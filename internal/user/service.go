package user

import (
	"context"
	"fmt"

	databaseutil "github.com/NYCU-SDC/summer/pkg/database"
	"github.com/google/uuid"
	"go.uber.org/zap"
)

type Repository interface {
	ListUsers(ctx context.Context, params ListParams) ([]Profile, error)
	CountUsers(ctx context.Context, filter ListFilter) (int64, error)
	GetActiveUser(ctx context.Context, id uuid.UUID) (Profile, error)
	ReplaceRoles(ctx context.Context, id uuid.UUID, roles []string) (Profile, error)
	SoftDelete(ctx context.Context, id uuid.UUID) error
}

type ListInput struct {
	Page     int32
	PageSize int32
	Search   *string
	Role     *string
}

type ProfilePage struct {
	Items       []Profile
	TotalItems  int32
	TotalPages  int32
	CurrentPage int32
	PageSize    int32
	HasNextPage bool
}

type Service struct {
	logger *zap.Logger
	repo   Repository
}

func NewService(repo Repository, logger *zap.Logger) *Service {
	if logger == nil {
		logger = zap.NewNop()
	}
	return &Service{
		logger: logger,
		repo:   repo,
	}
}

func (s *Service) List(ctx context.Context, in ListInput) (ProfilePage, error) {
	filter := ListFilter{Search: in.Search, Role: in.Role}

	items, err := s.repo.ListUsers(ctx, ListParams{
		ListFilter: filter,
		Limit:      in.PageSize,
		Offset:     (in.Page - 1) * in.PageSize,
	})
	if err != nil {
		return ProfilePage{}, databaseutil.WrapDBError(err, s.logger, "list users")
	}

	total, err := s.repo.CountUsers(ctx, filter)
	if err != nil {
		return ProfilePage{}, databaseutil.WrapDBError(err, s.logger, "count users")
	}

	var totalPages int32
	if total > 0 {
		totalPages = int32((total + int64(in.PageSize) - 1) / int64(in.PageSize))
	}

	return ProfilePage{
		Items:       items,
		TotalItems:  int32(total),
		TotalPages:  totalPages,
		CurrentPage: in.Page,
		PageSize:    in.PageSize,
		HasNextPage: in.Page < totalPages,
	}, nil
}

func (s *Service) Get(ctx context.Context, id uuid.UUID) (Profile, error) {
	u, err := s.repo.GetActiveUser(ctx, id)
	if err != nil {
		return Profile{}, databaseutil.WrapDBErrorWithKeyValue(err, "users", "id", id.String(), s.logger, "get user")
	}
	return u, nil
}

func (s *Service) Delete(ctx context.Context, actorID, targetID uuid.UUID) error {
	if actorID == targetID {
		return errSelfOperation
	}
	if err := s.repo.SoftDelete(ctx, targetID); err != nil {
		return databaseutil.WrapDBErrorWithKeyValue(err, "users", "id", targetID.String(), s.logger, "soft delete user")
	}
	return nil
}

func (s *Service) ReplaceRoles(ctx context.Context, actorID, targetID uuid.UUID, roles []string) (Profile, error) {
	if actorID == targetID {
		return Profile{}, errSelfOperation
	}
	for _, role := range roles {
		if !isKnownRole(role) {
			return Profile{}, fmt.Errorf("%w: unknown role %q", errInvalidUserPayload, role)
		}
	}
	u, err := s.repo.ReplaceRoles(ctx, targetID, roles)
	if err != nil {
		return Profile{}, databaseutil.WrapDBErrorWithKeyValue(err, "users", "id", targetID.String(), s.logger, "replace user roles")
	}
	return u, nil
}
