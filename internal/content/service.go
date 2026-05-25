package content

import (
	"context"
	"errors"
	"fmt"
	"io"
	"math"
	"os"
	"path/filepath"
	"strings"

	databaseutil "github.com/NYCU-SDC/summer/pkg/database"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"go.uber.org/zap"
)

const defaultMediaDir = "contents"
const (
	defaultPage     int32 = 1
	defaultPageSize int32 = 20
	maxPageSize     int32 = 100
)

var errEmptyMediaContent = errors.New("media content is empty")
var errEmptyTextContent = errors.New("text content is empty")

type Querier interface {
	CreateMediaContent(ctx context.Context, content pgtype.Text) (Content, error)
	CreateTextContent(ctx context.Context, content pgtype.Text) (Content, error)
	GetMediaContent(ctx context.Context, id uuid.UUID) (Content, error)
	GetTextContent(ctx context.Context, id uuid.UUID) (Content, error)
	GetContent(ctx context.Context, id uuid.UUID) (Content, error)
	ListTextContents(ctx context.Context, arg ListTextContentsParams) ([]Content, error)
	CountTextContents(ctx context.Context) (int64, error)
	BatchGetTextContents(ctx context.Context, ids []uuid.UUID) ([]Content, error)
	DeleteContent(ctx context.Context, id uuid.UUID) error
}

type Service struct {
	logger  *zap.Logger
	querier Querier
}

type TextPage struct {
	Items       []Content
	TotalPages  int32
	TotalItems  int32
	CurrentPage int32
	PageSize    int32
	HasNextPage bool
}

func NewService(querier Querier, logger *zap.Logger) *Service {
	if logger == nil {
		logger = zap.NewNop()
	}

	return &Service{
		logger:  logger,
		querier: querier,
	}
}

func (s *Service) CreateTextContent(ctx context.Context, content string) (Content, error) {
	content = strings.TrimSpace(content)
	if content == "" {
		return Content{}, errEmptyTextContent
	}

	item, err := s.querier.CreateTextContent(ctx, pgtype.Text{
		String: content,
		Valid:  true,
	})
	if err != nil {
		return Content{}, databaseutil.WrapDBError(err, s.logger, "create text content")
	}

	return item, nil
}

func (s *Service) CreateMediaContent(ctx context.Context, raw []byte, filename string, dir string) (Content, error) {
	if len(raw) == 0 {
		return Content{}, errEmptyMediaContent
	}
	if dir == "" {
		dir = defaultMediaDir
	}

	ext := sanitizeExt(filepath.Ext(filename))
	mediaRoot := dir
	if !filepath.IsAbs(dir) {
		mediaRoot = filepath.Join(".", dir)
	}
	// Ensure the storage directory exists before creating a temp file.
	if err := os.MkdirAll(mediaRoot, 0o755); err != nil {
		return Content{}, fmt.Errorf("create media directory: %w", err)
	}

	// Write to a temp file first to avoid exposing partial writes.
	tmpFile, err := os.CreateTemp(mediaRoot, "upload-*"+ext)
	if err != nil {
		return Content{}, fmt.Errorf("create temp media file: %w", err)
	}

	tmpPath := tmpFile.Name()

	n, err := tmpFile.Write(raw)
	if err != nil {
		if cerr := tmpFile.Close(); cerr != nil {
			s.logger.Warn("failed to close temp media file after write error", zap.String("path", tmpPath), zap.Error(cerr))
		}
		_ = os.Remove(tmpPath)
		return Content{}, fmt.Errorf("write media file: %w", err)
	}
	if n != len(raw) {
		if cerr := tmpFile.Close(); cerr != nil {
			s.logger.Warn("failed to close temp media file after short write", zap.String("path", tmpPath), zap.Error(cerr))
		}
		_ = os.Remove(tmpPath)
		return Content{}, fmt.Errorf("write media file: %w", io.ErrShortWrite)
	}
	if err := tmpFile.Sync(); err != nil {
		if cerr := tmpFile.Close(); cerr != nil {
			s.logger.Warn("failed to close temp media file after sync error", zap.String("path", tmpPath), zap.Error(cerr))
		}
		_ = os.Remove(tmpPath)
		return Content{}, fmt.Errorf("sync media file: %w", err)
	}
	if err := tmpFile.Close(); err != nil {
		_ = os.Remove(tmpPath)
		return Content{}, fmt.Errorf("close media file: %w", err)
	}

	// Atomically move the temp file into its final name.
	finalName := uuid.NewString() + ext
	finalPath := filepath.Join(mediaRoot, finalName)
	if err := os.Rename(tmpPath, finalPath); err != nil {
		_ = os.Remove(tmpPath)
		return Content{}, fmt.Errorf("persist media file: %w", err)
	}

	storedPath := toStoredPath(finalPath)
	// Persist only the stored path in DB; rollback file if DB insert fails.
	item, err := s.querier.CreateMediaContent(ctx, pgtype.Text{
		String: storedPath,
		Valid:  true,
	})
	if err != nil {
		_ = os.Remove(finalPath)
		return Content{}, databaseutil.WrapDBError(err, s.logger, "create media content")
	}

	return item, nil
}

func (s *Service) GetTextContent(ctx context.Context, id uuid.UUID) (Content, error) {
	item, err := s.querier.GetTextContent(ctx, id)
	if err != nil {
		return Content{}, databaseutil.WrapDBErrorWithKeyValue(err, "contents", "id", id.String(),
			s.logger, "get text content")
	}
	return item, nil
}

func (s *Service) GetMediaContent(ctx context.Context, id uuid.UUID) (Content, error) {
	item, err := s.querier.GetMediaContent(ctx, id)
	if err != nil {
		return Content{}, databaseutil.WrapDBErrorWithKeyValue(err, "contents", "id", id.String(),
			s.logger, "get media content")
	}
	return item, nil
}

func (s *Service) GetContent(ctx context.Context, id uuid.UUID) (Content, error) {
	item, err := s.querier.GetContent(ctx, id)
	if err != nil {
		return Content{}, databaseutil.WrapDBErrorWithKeyValue(err, "contents", "id", id.String(),
			s.logger, "get content")
	}
	return item, nil
}

func (s *Service) BatchGetTextContents(ctx context.Context, ids []uuid.UUID) ([]Content, error) {
	if len(ids) == 0 {
		return []Content{}, nil
	}

	items, err := s.querier.BatchGetTextContents(ctx, ids)
	if err != nil {
		return nil, databaseutil.WrapDBError(err, s.logger, "batch get text contents")
	}
	return items, nil
}

func (s *Service) ListTextContents(ctx context.Context, page, pageSize int32) (TextPage, error) {
	page, pageSize = normalizePagination(page, pageSize)

	totalItems, err := s.querier.CountTextContents(ctx)
	if err != nil {
		return TextPage{}, databaseutil.WrapDBError(err, s.logger, "count text contents")
	}

	offset := (page - 1) * pageSize
	items, err := s.querier.ListTextContents(ctx, ListTextContentsParams{
		Limit:  pageSize,
		Offset: offset,
	})
	if err != nil {
		return TextPage{}, databaseutil.WrapDBError(err, s.logger, "list text contents")
	}

	totalPages := int32(0)
	if totalItems > 0 {
		totalPages = int32(math.Ceil(float64(totalItems) / float64(pageSize)))
	}

	return TextPage{
		Items:       items,
		TotalPages:  totalPages,
		TotalItems:  int32(totalItems),
		CurrentPage: page,
		PageSize:    pageSize,
		HasNextPage: page < totalPages,
	}, nil
}

func (s *Service) DeleteContent(ctx context.Context, id uuid.UUID) error {
	return databaseutil.WrapDBErrorWithKeyValue(s.querier.DeleteContent(ctx, id), "contents", "id", id.String(),
		s.logger, "delete content")
}

func normalizePagination(page, pageSize int32) (int32, int32) {
	if page < 1 {
		page = defaultPage
	}
	if pageSize < 1 {
		pageSize = defaultPageSize
	}
	if pageSize > maxPageSize {
		pageSize = maxPageSize
	}
	return page, pageSize
}

func sanitizeExt(ext string) string {
	ext = strings.TrimSpace(strings.ToLower(ext))
	if ext == "" {
		return ".bin"
	}
	if strings.Contains(ext, string(filepath.Separator)) {
		return ".bin"
	}
	if !strings.HasPrefix(ext, ".") {
		return ".bin"
	}
	return ext
}

func toStoredPath(path string) string {
	slashPath := filepath.ToSlash(path)
	if filepath.IsAbs(path) {
		return slashPath
	}
	return "./" + strings.TrimPrefix(slashPath, "./")
}
