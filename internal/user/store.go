package user

import (
	"context"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
)

type Profile struct {
	ID        uuid.UUID
	Email     string
	Name      string
	AvatarURL *string
	Roles     []string
	CreatedAt time.Time
	UpdatedAt time.Time
}

type ListFilter struct {
	Search *string
	Role   *string
}

type ListParams struct {
	ListFilter
	Limit  int32
	Offset int32
}

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

func (s *Store) ListUsers(ctx context.Context, params ListParams) ([]Profile, error) {
	rows, err := s.queries.ListUsers(ctx, ListUsersParams{
		Search: pgTextFromPtr(params.Search),
		Role:   pgTextFromPtr(params.Role),
		Limit:  params.Limit,
		Offset: params.Offset,
	})
	if err != nil {
		return nil, err
	}

	profiles := make([]Profile, 0, len(rows))
	for _, row := range rows {
		profiles = append(profiles, toProfile(row.ID, row.Email, row.Name, row.AvatarUrl, row.Roles, row.CreatedAt, row.UpdatedAt))
	}
	return profiles, nil
}

func (s *Store) CountUsers(ctx context.Context, filter ListFilter) (int64, error) {
	return s.queries.CountUsers(ctx, CountUsersParams{
		Search: pgTextFromPtr(filter.Search),
		Role:   pgTextFromPtr(filter.Role),
	})
}

func (s *Store) GetActiveUser(ctx context.Context, id uuid.UUID) (Profile, error) {
	row, err := s.queries.GetActiveUserByID(ctx, id)
	if err != nil {
		return Profile{}, err
	}
	return toProfile(row.ID, row.Email, row.Name, row.AvatarUrl, row.Roles, row.CreatedAt, row.UpdatedAt), nil
}

func (s *Store) ReplaceRoles(ctx context.Context, id uuid.UUID, roles []string) (Profile, error) {
	row, err := s.queries.ReplaceUserRoles(ctx, ReplaceUserRolesParams{
		Roles: roles,
		ID:    id,
	})
	if err != nil {
		return Profile{}, err
	}
	return toProfile(row.ID, row.Email, row.Name, row.AvatarUrl, row.Roles, row.CreatedAt, row.UpdatedAt), nil
}

// SoftDelete disables the user and revokes their active refresh token families in a
// single transaction. A user that does not exist or is already disabled yields
// pgx.ErrNoRows so callers can map it to 404.
func (s *Store) SoftDelete(ctx context.Context, id uuid.UUID) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	q := s.queries.WithTx(tx)
	rows, err := q.SoftDeleteUser(ctx, id)
	if err != nil {
		return err
	}
	if rows == 0 {
		return pgx.ErrNoRows
	}

	if err := q.RevokeUserRefreshFamilies(ctx, id); err != nil {
		return err
	}

	return tx.Commit(ctx)
}

func toProfile(id uuid.UUID, email, name string, avatar pgtype.Text, roles []string, createdAt, updatedAt pgtype.Timestamptz) Profile {
	return Profile{
		ID:        id,
		Email:     email,
		Name:      name,
		AvatarURL: textPtr(avatar),
		Roles:     roles,
		CreatedAt: createdAt.Time,
		UpdatedAt: updatedAt.Time,
	}
}

func textPtr(t pgtype.Text) *string {
	if !t.Valid {
		return nil
	}
	v := t.String
	return &v
}

func pgTextFromPtr(s *string) pgtype.Text {
	if s == nil {
		return pgtype.Text{}
	}
	return pgtype.Text{String: *s, Valid: true}
}
