-- name: GetChat :one
SELECT * FROM chats
WHERE id = $1;
-- name: CreateChat :one
INSERT INTO chats DEFAULT VALUES
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