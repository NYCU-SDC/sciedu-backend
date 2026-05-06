-- name: ListQuestion :many
SELECT id, content, type, created_at, updated_at
FROM questions
ORDER BY id;

-- name: GetQuestion :one
SELECT id, content, type, created_at, updated_at
FROM questions
WHERE id = $1;

-- name: CreateQuestion :one
INSERT INTO questions (type, content)
VALUES ($1, $2)
RETURNING id, content, type, created_at, updated_at;

-- name: UpdateQuestion :one
UPDATE questions
SET type = $2,
    content = $3
WHERE id = $1
RETURNING id, content, type, created_at, updated_at;

-- name: DeleteQuestion :exec
DELETE FROM questions
WHERE id = $1;
