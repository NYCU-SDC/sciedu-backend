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
	if err := q.MarkRefreshTokenUsed(ctx, MarkRefreshTokenUsedParams{
		ID:     params.OldToken.ID,
		UsedAt: timestamptz(params.Now),
	}); err != nil {
		return RefreshTokenRecord{}, err
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
