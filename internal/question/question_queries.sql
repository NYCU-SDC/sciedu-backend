-- name: ListQuestion :many
SELECT id, type, content
FROM questions
ORDER BY id;

-- name: GetQuestion :one
SELECT id, type, content
FROM questions
WHERE id = $1;

-- name: CreateQuestion :one
INSERT INTO questions (type, content)
VALUES ($1, $2)
RETURNING id, type, content;

-- name: UpdateQuestion :one
UPDATE questions
SET type = $2,
    content = $3
WHERE id = $1
RETURNING id, type, content;

-- name: DeleteQuestion :exec
DELETE FROM questions
WHERE id = $1;
