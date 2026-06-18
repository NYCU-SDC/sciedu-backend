package auth

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
	"net/url"
	"strings"
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
	GetUserProfile(ctx context.Context, userID uuid.UUID) (UserProfile, error)
	CreateOAuthLoginState(ctx context.Context, params CreateOAuthStateParams) error
	ConsumeOAuthLoginState(ctx context.Context, stateHash []byte, now time.Time) (OAuthLoginStateRecord, error)
	FindOrCreateOAuthUser(ctx context.Context, identity OAuthIdentity) (OAuthUserRecord, error)
}

type ServiceConfig struct {
	Secret               string
	Environment          string
	Now                  func() time.Time
	OAuthProvider        OAuthProvider
	RedirectURLAllowlist []string
}

type Service struct {
	repo   Repository
	config ServiceConfig
	logger *zap.Logger
}

type accessClaims struct {
	jwt.RegisteredClaims
}

type OAuthProvider interface {
	Name() string
	AuthCodeURL(state, codeVerifier string) string
	ExchangeIDToken(ctx context.Context, code, codeVerifier string) (string, error)
	VerifyIDToken(ctx context.Context, rawToken string) (GoogleIDTokenClaims, error)
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

	session := Session{
		UserID:                record.UserID,
		AccessToken:           accessToken,
		RefreshToken:          refreshToken,
		AccessTokenExpiresAt:  accessExpiresAt,
		RefreshTokenExpiresAt: record.FamilyExpiresAt,
	}
	return s.attachUserProfile(ctx, session)
}

func (s *Service) BeginOAuth(ctx context.Context, params BeginOAuthParams) (BeginOAuthResult, error) {
	provider, err := s.oauthProvider(params.Provider)
	if err != nil {
		return BeginOAuthResult{}, err
	}
	if !s.isRedirectAllowed(params.RedirectURL) {
		return BeginOAuthResult{}, errInvalidRedirectURL
	}

	state, err := randomBase64URL(32)
	if err != nil {
		return BeginOAuthResult{}, err
	}
	codeVerifier, err := randomBase64URL(64)
	if err != nil {
		return BeginOAuthResult{}, err
	}
	now := s.now()
	// Persist only the hashed state; the raw state is sent to the provider and consumed once on callback.
	if err := s.repo.CreateOAuthLoginState(ctx, CreateOAuthStateParams{
		StateHash:    oauthStateHash(state),
		Provider:     provider.Name(),
		CodeVerifier: codeVerifier,
		RedirectURL:  params.RedirectURL,
		ExpiresAt:    now.Add(10 * time.Minute),
		IPAddress:    params.IPAddress,
		UserAgent:    params.UserAgent,
		Now:          now,
	}); err != nil {
		return BeginOAuthResult{}, err
	}
	return BeginOAuthResult{AuthURL: provider.AuthCodeURL(state, codeVerifier)}, nil
}

func (s *Service) CompleteOAuth(ctx context.Context, params CompleteOAuthParams) (CompleteOAuthResult, error) {
	provider, err := s.oauthProvider(params.Provider)
	if err != nil {
		return CompleteOAuthResult{}, err
	}
	if params.Code == "" || params.State == "" {
		return CompleteOAuthResult{}, errInvalidOAuthState
	}

	// Consuming the state also loads the PKCE verifier needed to exchange the authorization code.
	state, err := s.repo.ConsumeOAuthLoginState(ctx, oauthStateHash(params.State), s.now())
	if err != nil {
		if errors.Is(err, errOAuthStateNotFound) {
			return CompleteOAuthResult{}, errInvalidOAuthState
		}
		return CompleteOAuthResult{}, err
	}
	if state.Provider != provider.Name() {
		return CompleteOAuthResult{}, errInvalidOAuthState
	}

	idToken, err := provider.ExchangeIDToken(ctx, params.Code, state.CodeVerifier)
	if err != nil {
		return CompleteOAuthResult{}, fmt.Errorf("%w: %w", errOAuthCodeExchange, err)
	}
	claims, err := provider.VerifyIDToken(ctx, idToken)
	if err != nil {
		return CompleteOAuthResult{}, fmt.Errorf("verify oauth id token: %w", err)
	}

	user, err := s.repo.FindOrCreateOAuthUser(ctx, OAuthIdentity{
		Provider:       provider.Name(),
		ProviderUserID: claims.Subject,
		Email:          claims.Email,
		EmailVerified:  claims.EmailVerified,
		Name:           claims.Name,
		AvatarURL:      claims.Picture,
		Now:            s.now(),
	})
	if err != nil {
		return CompleteOAuthResult{}, fmt.Errorf("find or create oauth user: %w", err)
	}

	session, err := s.IssueSession(ctx, IssueSessionParams{
		UserID:         user.UserID,
		OAuthAccountID: &user.OAuthAccountID,
		IPAddress:      params.IPAddress,
		UserAgent:      params.UserAgent,
	})
	if err != nil {
		return CompleteOAuthResult{}, fmt.Errorf("issue oauth session: %w", err)
	}
	return CompleteOAuthResult{Session: session, RedirectURL: state.RedirectURL}, nil
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
	session := Session{
		UserID:                claims.UserID,
		AccessTokenExpiresAt:  claims.ExpiresAt,
		RefreshTokenExpiresAt: record.FamilyExpiresAt,
	}
	return s.attachUserProfile(ctx, session)
}

func (s *Service) Refresh(ctx context.Context, refreshToken string) (Session, error) {
	record, err := s.findRefreshToken(ctx, refreshToken)
	if err != nil {
		return Session{}, err
	}

	now := s.now()
	if record.UsedAt != nil || !record.IsCurrent {
		// A non-current refresh token means the token chain may have been replayed, so revoke the whole family.
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
	session := Session{
		UserID:                rotated.UserID,
		AccessToken:           accessToken,
		RefreshToken:          newRefreshToken,
		AccessTokenExpiresAt:  accessExpiresAt,
		RefreshTokenExpiresAt: rotated.FamilyExpiresAt,
	}
	return s.attachUserProfile(ctx, session)
}

func (s *Service) attachUserProfile(ctx context.Context, session Session) (Session, error) {
	profile, err := s.repo.GetUserProfile(ctx, session.UserID)
	if err != nil {
		return Session{}, err
	}
	session.Username = profile.Username
	session.Email = profile.Email
	return session, nil
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

func oauthStateHash(raw string) []byte {
	sum := sha256.Sum256([]byte(raw))
	return sum[:]
}

func randomBase64URL(size int) (string, error) {
	buf := make([]byte, size)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(buf), nil
}

func (s *Service) oauthProvider(name string) (OAuthProvider, error) {
	if s.config.OAuthProvider == nil {
		return nil, errOAuthNotConfigured
	}
	if name == "" || name == s.config.OAuthProvider.Name() {
		return s.config.OAuthProvider, nil
	}
	return nil, errOAuthNotConfigured
}

func (s *Service) isRedirectAllowed(raw string) bool {
	parsed, err := url.Parse(raw)
	if err != nil || parsed.Scheme == "" || parsed.Host == "" {
		return false
	}

	allowlist := s.config.RedirectURLAllowlist
	if len(allowlist) == 0 {
		if s.config.Environment == EnvironmentDev {
			allowlist = []string{"http://localhost", "http://127.0.0.1"}
		} else {
			allowlist = []string{"https://sciedu.sdc.nycu.club"}
		}
	}

	for _, allowed := range allowlist {
		allowed = strings.TrimSpace(allowed)
		if allowed == "" {
			continue
		}
		if raw == allowed || strings.HasPrefix(raw, strings.TrimRight(allowed, "/")+"/") {
			return true
		}
	}
	return false
}
