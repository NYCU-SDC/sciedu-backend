package auth

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"time"

	handlerutil "github.com/NYCU-SDC/summer/pkg/handler"
	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
	"go.uber.org/zap"
)

type Repository interface {
	CreateRefreshSession(ctx context.Context, params CreateRefreshSessionParams) (RefreshTokenRecord, error)
	FindRefreshToken(ctx context.Context, tokenHash []byte) (RefreshTokenRecord, error)
	RotateRefreshToken(ctx context.Context, params RotateRefreshTokenParams) (RefreshTokenRecord, error)
	RevokeRefreshFamily(ctx context.Context, tokenHash []byte, now time.Time, reason string) error
	MarkReuseDetected(ctx context.Context, familyID uuid.UUID, now time.Time) error
}

type ServiceConfig struct {
	Secret      string
	Environment string
	Now         func() time.Time
}

type Service struct {
	repo   Repository
	config ServiceConfig
	logger *zap.Logger
}

type accessClaims struct {
	jwt.RegisteredClaims
}

func NewService(repo Repository, config ServiceConfig, logger *zap.Logger) *Service {
	if config.Secret == "" {
		config.Secret = defaultSecret
	}
	if config.Environment == "" {
		config.Environment = EnvironmentProd
	}
	if config.Now == nil {
		config.Now = time.Now
	}
	if logger == nil {
		logger = zap.NewNop()
	}
	return &Service{
		repo:   repo,
		config: config,
		logger: logger,
	}
}

func (s *Service) IssueSession(ctx context.Context, params IssueSessionParams) (Session, error) {
	now := s.now()
	refreshToken := uuid.NewString()
	familyExpiresAt := now.Add(refreshTokenLifetime)
	record, err := s.repo.CreateRefreshSession(ctx, CreateRefreshSessionParams{
		UserID:          params.UserID,
		OAuthAccountID:  params.OAuthAccountID,
		TokenHash:       refreshTokenHash(refreshToken),
		FamilyExpiresAt: familyExpiresAt,
		IPAddress:       params.IPAddress,
		UserAgent:       params.UserAgent,
		Now:             now,
	})
	if err != nil {
		return Session{}, err
	}

	accessToken, accessExpiresAt, err := s.issueAccessToken(record.UserID, now)
	if err != nil {
		return Session{}, err
	}

	return Session{
		UserID:                record.UserID,
		AccessToken:           accessToken,
		RefreshToken:          refreshToken,
		AccessTokenExpiresAt:  accessExpiresAt,
		RefreshTokenExpiresAt: record.FamilyExpiresAt,
	}, nil
}

func (s *Service) Session(ctx context.Context, accessToken, refreshToken string) (Session, error) {
	claims, err := s.VerifyAccessToken(accessToken)
	if err != nil {
		return Session{}, err
	}
	record, err := s.validRefreshToken(ctx, refreshToken)
	if err != nil {
		return Session{}, err
	}
	if claims.UserID != record.UserID {
		return Session{}, handlerutil.ErrUnauthorized
	}
	return Session{
		UserID:                claims.UserID,
		AccessTokenExpiresAt:  claims.ExpiresAt,
		RefreshTokenExpiresAt: record.FamilyExpiresAt,
	}, nil
}

func (s *Service) Refresh(ctx context.Context, refreshToken string) (Session, error) {
	record, err := s.findRefreshToken(ctx, refreshToken)
	if err != nil {
		return Session{}, err
	}

	now := s.now()
	if record.UsedAt != nil || !record.IsCurrent {
		if markErr := s.repo.MarkReuseDetected(ctx, record.FamilyID, now); markErr != nil {
			s.logger.Warn("failed to mark refresh token reuse", zap.Error(markErr))
		}
		return Session{}, fmt.Errorf("%w: %w", ErrRefreshReuseDetected, handlerutil.ErrUnauthorized)
	}
	if !record.isUsable(now) {
		return Session{}, handlerutil.ErrUnauthorized
	}

	newRefreshToken := uuid.NewString()
	rotated, err := s.repo.RotateRefreshToken(ctx, RotateRefreshTokenParams{
		OldToken:     record,
		NewTokenHash: refreshTokenHash(newRefreshToken),
		Now:          now,
	})
	if err != nil {
		return Session{}, err
	}

	accessToken, accessExpiresAt, err := s.issueAccessToken(rotated.UserID, now)
	if err != nil {
		return Session{}, err
	}
	return Session{
		UserID:                rotated.UserID,
		AccessToken:           accessToken,
		RefreshToken:          newRefreshToken,
		AccessTokenExpiresAt:  accessExpiresAt,
		RefreshTokenExpiresAt: rotated.FamilyExpiresAt,
	}, nil
}

func (s *Service) Logout(ctx context.Context, refreshToken string) error {
	if refreshToken == "" {
		return nil
	}
	return s.repo.RevokeRefreshFamily(ctx, refreshTokenHash(refreshToken), s.now(), "logout")
}

type AccessTokenClaims struct {
	UserID    uuid.UUID
	ExpiresAt time.Time
}

func (s *Service) VerifyAccessToken(rawToken string) (AccessTokenClaims, error) {
	if rawToken == "" {
		return AccessTokenClaims{}, handlerutil.ErrUnauthorized
	}

	claims := &accessClaims{}
	token, err := jwt.ParseWithClaims(rawToken, claims, func(token *jwt.Token) (interface{}, error) {
		if token.Method != jwt.SigningMethodHS256 {
			return nil, fmt.Errorf("unexpected signing method: %s", token.Header["alg"])
		}
		return []byte(s.config.Secret), nil
	}, jwt.WithLeeway(clockSkew), jwt.WithTimeFunc(s.now))
	if err != nil || !token.Valid {
		return AccessTokenClaims{}, handlerutil.ErrUnauthorized
	}

	userID, err := uuid.Parse(claims.Subject)
	if err != nil {
		return AccessTokenClaims{}, handlerutil.ErrUnauthorized
	}
	if claims.ExpiresAt == nil {
		return AccessTokenClaims{}, handlerutil.ErrUnauthorized
	}
	return AccessTokenClaims{
		UserID:    userID,
		ExpiresAt: claims.ExpiresAt.UTC(),
	}, nil
}

func (s *Service) issueAccessToken(userID uuid.UUID, now time.Time) (string, time.Time, error) {
	expiresAt := now.Add(accessTokenLifetime)
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, accessClaims{
		RegisteredClaims: jwt.RegisteredClaims{
			Subject:   userID.String(),
			IssuedAt:  jwt.NewNumericDate(now),
			ExpiresAt: jwt.NewNumericDate(expiresAt),
		},
	})
	raw, err := token.SignedString([]byte(s.config.Secret))
	if err != nil {
		return "", time.Time{}, err
	}
	return raw, expiresAt, nil
}

func (s *Service) validRefreshToken(ctx context.Context, refreshToken string) (RefreshTokenRecord, error) {
	record, err := s.findRefreshToken(ctx, refreshToken)
	if err != nil {
		return RefreshTokenRecord{}, err
	}
	if !record.isUsable(s.now()) {
		return RefreshTokenRecord{}, handlerutil.ErrUnauthorized
	}
	return record, nil
}

func (s *Service) findRefreshToken(ctx context.Context, refreshToken string) (RefreshTokenRecord, error) {
	if refreshToken == "" {
		return RefreshTokenRecord{}, handlerutil.ErrUnauthorized
	}
	record, err := s.repo.FindRefreshToken(ctx, refreshTokenHash(refreshToken))
	if err != nil {
		if errors.Is(err, errRefreshTokenNotFound) {
			return RefreshTokenRecord{}, handlerutil.ErrUnauthorized
		}
		return RefreshTokenRecord{}, err
	}
	return record, nil
}

func (r RefreshTokenRecord) isUsable(now time.Time) bool {
	if r.RevokedAt != nil || r.FamilyRevokedAt != nil {
		return false
	}
	if !r.FamilyExpiresAt.After(now) {
		return false
	}
	return r.IsCurrent
}

func (s *Service) now() time.Time {
	return s.config.Now().UTC()
}

func refreshTokenHash(raw string) []byte {
	sum := sha256.Sum256([]byte(raw))
	return sum[:]
}
