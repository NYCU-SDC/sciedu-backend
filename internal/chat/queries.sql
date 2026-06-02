-- name: GetChat :one
SELECT * FROM chats
WHERE id = $1;
-- name: CreateChat :one
INSERT INTO chats (user_id, title)
VALUES ($1, $2)
RETURNING *;
-- name: UpdateChat :one
UPDATE chats
SET title = $2, updated_at = now()
WHERE id = $1
RETURNING *;
-- name: GetMessage :one
SELECT * FROM messages
WHERE id = $1;
-- name: GetMessages :many
SELECT * FROM messages
WHERE chat_id = $1
ORDER BY created_at;
-- name: CreateMessage :one
INSERT INTO messages (chat_id, previous_id, content, role, status)
VALUES ($1, $2, $3, $4, $5)
RETURNING *;
-- name: UpdateMessage :one
UPDATE messages
SET content = $2, status = $3
WHERE id = $1
RETURNING *;
-- name: DeleteChat :exec
DELETE FROM chats
WHERE id = $1;
-- name: ListChatsByUser :many
SELECT * FROM chats
WHERE user_id = $1
ORDER BY updated_at DESC
LIMIT $2
OFFSET $3;
-- name: CountChatsByUser :one
SELECT COUNT(*) FROM chats
WHERE user_id = $1;