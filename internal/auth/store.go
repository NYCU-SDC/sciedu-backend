package auth

import (
	"context"
	"errors"
	"net/netip"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
)

type Store struct {
	pool    *pgxpool.Pool
	queries *Queries
}

func NewStore(pool *pgxpool.Pool) *Store {
	return &Store{
		pool:    pool,
		queries: New(pool),
	}
}

func (s *Store) CreateRefreshSession(ctx context.Context, params CreateRefreshSessionParams) (RefreshTokenRecord, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return RefreshTokenRecord{}, err
	}
	defer rollbackTx(ctx, tx)

	q := s.queries.WithTx(tx)
	family, err := q.CreateRefreshTokenFamily(ctx, CreateRefreshTokenFamilyParams{
		UserID:         params.UserID,
		OauthAccountID: nullableUUID(params.OAuthAccountID),
		ExpiresAt:      timestamptz(params.FamilyExpiresAt),
		IpAddress:      nullableIP(params.IPAddress),
		UserAgent:      nullableText(params.UserAgent),
		CreatedAt:      timestamptz(params.Now),
	})
	if err != nil {
		return RefreshTokenRecord{}, err
	}

	token, err := q.CreateRefreshToken(ctx, CreateRefreshTokenParams{
		FamilyID:           family.ID,
		UserID:             params.UserID,
		TokenHash:          params.TokenHash,
		RotatedFromTokenID: pgtype.UUID{},
		IssuedAt:           timestamptz(params.Now),
	})
	if err != nil {
		return RefreshTokenRecord{}, err
	}

	if err := tx.Commit(ctx); err != nil {
		return RefreshTokenRecord{}, err
	}
	return refreshTokenRecordFromToken(token, family.ExpiresAt, family.RevokedAt), nil
}

func (s *Store) FindRefreshToken(ctx context.Context, tokenHash []byte) (RefreshTokenRecord, error) {
	row, err := s.queries.GetRefreshTokenByHash(ctx, tokenHash)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return RefreshTokenRecord{}, errRefreshTokenNotFound
		}
		return RefreshTokenRecord{}, err
	}
	return refreshTokenRecordFromRow(row), nil
}

func (s *Store) RotateRefreshToken(ctx context.Context, params RotateRefreshTokenParams) (RefreshTokenRecord, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return RefreshTokenRecord{}, err
	}
	defer rollbackTx(ctx, tx)

	q := s.queries.WithTx(tx)
	rowsAffected, err := q.MarkRefreshTokenUsed(ctx, MarkRefreshTokenUsedParams{
		ID:     params.OldToken.ID,
		UsedAt: timestamptz(params.Now),
	})
	if err != nil {
		return RefreshTokenRecord{}, err
	}
	if rowsAffected != 1 {
		if err := q.MarkRefreshFamilyReuseDetected(ctx, MarkRefreshFamilyReuseDetectedParams{
			ID:        params.OldToken.FamilyID,
			RevokedAt: timestamptz(params.Now),
		}); err != nil {
			return RefreshTokenRecord{}, err
		}
		if err := tx.Commit(ctx); err != nil {
			return RefreshTokenRecord{}, err
		}
		return RefreshTokenRecord{}, ErrRefreshReuseDetected
	}

	token, err := q.InsertRotatedRefreshToken(ctx, InsertRotatedRefreshTokenParams{
		FamilyID:  params.OldToken.FamilyID,
		UserID:    params.OldToken.UserID,
		TokenHash: params.NewTokenHash,
		RotatedFromTokenID: pgtype.UUID{
			Bytes: params.OldToken.ID,
			Valid: true,
		},
		IssuedAt: timestamptz(params.Now),
	})
	if err != nil {
		return RefreshTokenRecord{}, err
	}

	if err := q.TouchRefreshTokenFamily(ctx, TouchRefreshTokenFamilyParams{
		ID:         params.OldToken.FamilyID,
		LastUsedAt: timestamptz(params.Now),
	}); err != nil {
		return RefreshTokenRecord{}, err
	}

	if err := tx.Commit(ctx); err != nil {
		return RefreshTokenRecord{}, err
	}
	return refreshTokenRecordFromToken(token, timestamptz(params.OldToken.FamilyExpiresAt), pgtype.Timestamptz{}), nil
}

func (s *Store) RevokeRefreshFamily(ctx context.Context, tokenHash []byte, now time.Time, reason string) error {
	return s.queries.RevokeRefreshFamilyByTokenHash(ctx, RevokeRefreshFamilyByTokenHashParams{
		TokenHash:     tokenHash,
		RevokedAt:     timestamptz(now),
		RevokedReason: nullableText(reason),
	})
}

func (s *Store) MarkReuseDetected(ctx context.Context, familyID uuid.UUID, now time.Time) error {
	return s.queries.MarkRefreshFamilyReuseDetected(ctx, MarkRefreshFamilyReuseDetectedParams{
		ID:        familyID,
		RevokedAt: timestamptz(now),
	})
}

func (s *Store) CreateOAuthLoginState(ctx context.Context, params CreateOAuthStateParams) error {
	return s.queries.CreateOAuthLoginState(ctx, CreateOAuthLoginStateParams{
		StateHash:    params.StateHash,
		Provider:     params.Provider,
		CodeVerifier: params.CodeVerifier,
		RedirectUrl:  params.RedirectURL,
		ExpiresAt:    timestamptz(params.ExpiresAt),
		IpAddress:    nullableIP(params.IPAddress),
		UserAgent:    nullableText(params.UserAgent),
		CreatedAt:    timestamptz(params.Now),
	})
}

func (s *Store) ConsumeOAuthLoginState(ctx context.Context, stateHash []byte, now time.Time) (OAuthLoginStateRecord, error) {
	row, err := s.queries.ConsumeOAuthLoginState(ctx, ConsumeOAuthLoginStateParams{
		StateHash: stateHash,
		UsedAt:    timestamptz(now),
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return OAuthLoginStateRecord{}, errOAuthStateNotFound
		}
		return OAuthLoginStateRecord{}, err
	}
	return OAuthLoginStateRecord{
		StateHash:    row.StateHash,
		Provider:     row.Provider,
		CodeVerifier: row.CodeVerifier,
		RedirectURL:  row.RedirectUrl,
		ExpiresAt:    row.ExpiresAt.Time,
		UsedAt:       timePtr(row.UsedAt),
	}, nil
}

func (s *Store) FindOrCreateOAuthUser(ctx context.Context, identity OAuthIdentity) (OAuthUserRecord, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return OAuthUserRecord{}, err
	}
	defer rollbackTx(ctx, tx)

	// Link by the provider subject, not email, because provider emails can change or be unverified.
	q := s.queries.WithTx(tx)
	account, err := q.GetOAuthAccountByProviderUserID(ctx, GetOAuthAccountByProviderUserIDParams{
		Provider:       identity.Provider,
		ProviderUserID: identity.ProviderUserID,
	})
	if err == nil {
		if err := q.TouchOAuthAccount(ctx, TouchOAuthAccountParams{
			ID:            account.ID,
			UserID:        account.UserID,
			ProviderEmail: identity.Email,
			EmailVerified: identity.EmailVerified,
			LastLoginAt:   timestamptz(identity.Now),
		}); err != nil {
			return OAuthUserRecord{}, err
		}
		if err := q.TouchUserLogin(ctx, TouchUserLoginParams{
			ID:          account.UserID,
			LastLoginAt: timestamptz(identity.Now),
		}); err != nil {
			return OAuthUserRecord{}, err
		}
		if err := tx.Commit(ctx); err != nil {
			return OAuthUserRecord{}, err
		}
		return OAuthUserRecord{UserID: account.UserID, OAuthAccountID: account.ID}, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return OAuthUserRecord{}, err
	}

	user, err := q.CreateOAuthUser(ctx, CreateOAuthUserParams{
		Email:       identity.Email,
		Name:        nonEmptyName(identity.Name, identity.Email),
		AvatarUrl:   nullableText(identity.AvatarURL),
		LastLoginAt: timestamptz(identity.Now),
	})
	if err != nil {
		return OAuthUserRecord{}, err
	}
	account, err = q.CreateOAuthAccount(ctx, CreateOAuthAccountParams{
		UserID:         user.ID,
		Provider:       identity.Provider,
		ProviderUserID: identity.ProviderUserID,
		ProviderEmail:  identity.Email,
		EmailVerified:  identity.EmailVerified,
		LastLoginAt:    timestamptz(identity.Now),
	})
	if err != nil {
		return OAuthUserRecord{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return OAuthUserRecord{}, err
	}
	return OAuthUserRecord{UserID: user.ID, OAuthAccountID: account.ID}, nil
}

func (s *Store) GetUserProfile(ctx context.Context, userID uuid.UUID) (UserProfile, error) {
	row, err := s.queries.GetUserProfile(ctx, userID)
	if err != nil {
		return UserProfile{}, err
	}
	return UserProfile{
		ID:       row.ID,
		Username: row.Name,
		Email:    string(row.Email),
	}, nil
}

func refreshTokenRecordFromRow(row GetRefreshTokenByHashRow) RefreshTokenRecord {
	return RefreshTokenRecord{
		ID:              row.ID,
		FamilyID:        row.FamilyID,
		UserID:          row.UserID,
		Hash:            row.TokenHash,
		IsCurrent:       row.IsCurrent,
		IssuedAt:        row.IssuedAt.Time,
		UsedAt:          timePtr(row.UsedAt),
		RevokedAt:       timePtr(row.RevokedAt),
		FamilyExpiresAt: row.FamilyExpiresAt.Time,
		FamilyRevokedAt: timePtr(row.FamilyRevokedAt),
	}
}

func refreshTokenRecordFromToken(token RefreshToken, familyExpiresAt, familyRevokedAt pgtype.Timestamptz) RefreshTokenRecord {
	return RefreshTokenRecord{
		ID:              token.ID,
		FamilyID:        token.FamilyID,
		UserID:          token.UserID,
		Hash:            token.TokenHash,
		IsCurrent:       token.IsCurrent,
		IssuedAt:        token.IssuedAt.Time,
		UsedAt:          timePtr(token.UsedAt),
		RevokedAt:       timePtr(token.RevokedAt),
		FamilyExpiresAt: familyExpiresAt.Time,
		FamilyRevokedAt: timePtr(familyRevokedAt),
	}
}

func nullableUUID(id *uuid.UUID) pgtype.UUID {
	if id == nil {
		return pgtype.UUID{}
	}
	return pgtype.UUID{Bytes: *id, Valid: true}
}

func nullableText(value string) pgtype.Text {
	if value == "" {
		return pgtype.Text{}
	}
	return pgtype.Text{String: value, Valid: true}
}

func nullableIP(value string) *netip.Addr {
	if value == "" {
		return nil
	}
	addr, err := netip.ParseAddr(value)
	if err != nil {
		return nil
	}
	return &addr
}

func timestamptz(value time.Time) pgtype.Timestamptz {
	return pgtype.Timestamptz{Time: value.UTC(), Valid: true}
}

func timePtr(value pgtype.Timestamptz) *time.Time {
	if !value.Valid {
		return nil
	}
	t := value.Time
	return &t
}

func rollbackTx(ctx context.Context, tx pgx.Tx) {
	_ = tx.Rollback(ctx)
}

func nonEmptyName(name, fallback string) string {
	if name != "" {
		return name
	}
	return fallback
}
