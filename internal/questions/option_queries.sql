-- name: GetOption :one
SELECT id, question_id, content, label, created_at, updated_at
FROM options
WHERE id = $1;

-- name: ListOptionsByQuestion :many
SELECT id, question_id, content, label, created_at, updated_at
FROM options
WHERE question_id = $1
ORDER BY label;

-- name: CreateOption :one
INSERT INTO options (question_id, label, content)
VALUES ($1, $2, $3)
RETURNING id, question_id, content, label, created_at, updated_at;

-- name: UpdateOption :one
UPDATE options
SET label = $2,
    content = $3
WHERE id = $1
RETURNING id, question_id, content, label, created_at, updated_at;

-- name: DeleteOption :exec
DELETE FROM options
WHERE id = $1;
