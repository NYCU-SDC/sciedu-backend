package chat

import (
	"context"

	databaseutil "github.com/NYCU-SDC/summer/pkg/database"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"go.uber.org/zap"
)

type Repository struct {
	db     *pgxpool.Pool
	logger *zap.Logger
}

func NewRepository(db *pgxpool.Pool, logger *zap.Logger) *Repository {
	return &Repository{db: db, logger: logger}
}

func (r *Repository) CreateChat(ctx context.Context) (uuid.UUID, error) {
	var id uuid.UUID
	if err := r.db.QueryRow(ctx, `INSERT INTO chats DEFAULT VALUES RETURNING id`).Scan(&id); err != nil {
		return uuid.Nil, databaseutil.WrapDBError(err, r.logger, "create chat")
	}
	return id, nil
}

func (r *Repository) ListMessages(ctx context.Context, chatID uuid.UUID) ([]Message, error) {
	rows, err := r.db.Query(ctx, `
		SELECT id, content, role, previous_id, status, chat_id, created_at
		FROM messages
		WHERE chat_id = $1
		ORDER BY created_at ASC`, chatID)
	if err != nil {
		return nil, databaseutil.WrapDBError(err, r.logger, "list messages")
	}
	defer rows.Close()

	return scanMessages(rows, r.logger, "list messages")
}

func (r *Repository) CreateUserMessageWithReply(ctx context.Context, chatID uuid.UUID, req SendMessageRequest) (Message, uuid.UUID, error) {
	tx, err := r.db.Begin(ctx)
	if err != nil {
		return Message{}, uuid.Nil, databaseutil.WrapDBError(err, r.logger, "begin create message")
	}
	defer func() {
		_ = tx.Rollback(ctx)
	}()

	var userMessage Message
	err = tx.QueryRow(ctx, `
		INSERT INTO messages (content, role, status, previous_id, chat_id)
		VALUES ($1, 'user', 'created', $2, $3)
		RETURNING id, content, role, previous_id, status, chat_id, created_at`,
		req.Content, req.PreviousID, chatID).
		Scan(&userMessage.ID, &userMessage.Content, &userMessage.Role, &userMessage.PreviousID, &userMessage.Status, &userMessage.ChatID, &userMessage.CreatedAt)
	if err != nil {
		return Message{}, uuid.Nil, databaseutil.WrapDBError(err, r.logger, "create user message")
	}

	var replyID uuid.UUID
	err = tx.QueryRow(ctx, `
		INSERT INTO messages (content, role, status, previous_id, chat_id)
		VALUES ('', 'assistant', 'created', $1, $2)
		RETURNING id`, userMessage.ID, chatID).Scan(&replyID)
	if err != nil {
		return Message{}, uuid.Nil, databaseutil.WrapDBError(err, r.logger, "create assistant placeholder")
	}
	if err := tx.Commit(ctx); err != nil {
		return Message{}, uuid.Nil, databaseutil.WrapDBError(err, r.logger, "commit create message")
	}
	return userMessage, replyID, nil
}

func (r *Repository) ListMessagesForReply(ctx context.Context, replyID uuid.UUID) ([]Message, error) {
	rows, err := r.db.Query(ctx, `
		WITH RECURSIVE message_chain AS (
			SELECT id, content, role, previous_id, status, chat_id, created_at, 0 AS depth
			FROM messages
			WHERE id = $1
			UNION ALL
			SELECT m.id, m.content, m.role, m.previous_id, m.status, m.chat_id, m.created_at, message_chain.depth + 1
			FROM messages m
			JOIN message_chain ON message_chain.previous_id = m.id
		)
		SELECT id, content, role, previous_id, status, chat_id, created_at
		FROM message_chain
		WHERE id <> $1
		ORDER BY depth DESC`, replyID)
	if err != nil {
		return nil, databaseutil.WrapDBError(err, r.logger, "list message branch")
	}
	defer rows.Close()

	return scanMessages(rows, r.logger, "list message branch")
}

func (r *Repository) GetMessage(ctx context.Context, id uuid.UUID) (Message, error) {
	var m Message
	err := r.db.QueryRow(ctx, `
		SELECT id, content, role, previous_id, status, chat_id, created_at
		FROM messages
		WHERE id = $1`, id).
		Scan(&m.ID, &m.Content, &m.Role, &m.PreviousID, &m.Status, &m.ChatID, &m.CreatedAt)
	if err != nil {
		return Message{}, databaseutil.WrapDBErrorWithKeyValue(err, "messages", "id", id.String(), r.logger, "get message")
	}
	return m, nil
}

func (r *Repository) UpdateMessage(ctx context.Context, id uuid.UUID, content string, status Status) error {
	tag, err := r.db.Exec(ctx, `
		UPDATE messages
		SET content = $2, status = $3
		WHERE id = $1`, id, content, status)
	if err != nil {
		return databaseutil.WrapDBError(err, r.logger, "update message")
	}
	if tag.RowsAffected() == 0 {
		return databaseutil.WrapDBErrorWithKeyValue(pgx.ErrNoRows, "messages", "id", id.String(), r.logger, "update message")
	}
	return nil
}

func scanMessages(rows pgx.Rows, logger *zap.Logger, operation string) ([]Message, error) {
	var messages []Message
	for rows.Next() {
		var m Message
		if err := rows.Scan(&m.ID, &m.Content, &m.Role, &m.PreviousID, &m.Status, &m.ChatID, &m.CreatedAt); err != nil {
			return nil, databaseutil.WrapDBError(err, logger, operation)
		}
		messages = append(messages, m)
	}
	if err := rows.Err(); err != nil {
		return nil, databaseutil.WrapDBError(err, logger, operation)
	}
	return messages, nil
}
