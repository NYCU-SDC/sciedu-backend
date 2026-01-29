-- name: CreateQuestion :one
INSERT INTO questions (type, content)
VALUES ($1, $2)
RETURNING *;

-- name: CreateCorrespondOption :one
INSERT INTO options(question_id, label, content)
VALUES ($1, $2, $3)
RETURNING *;

-- name: ListQuestion :many
SELECT * FROM questions;

-- name: GetQuestion :one
SELECT * FROM questions WHERE id = $1;

-- name: ListCorrespondOption :many
SELECT * FROM options WHERE question_id = $1;

-- name: UpdateQuestion :one
UPDATE questions SET type = $2, content = $3 WHERE id = $1 RETURNING *;

-- name: UpdateCorrespondingOption :one
UPDATE options SET label = $2, content = $3 WHERE question_id = $1 RETURNING *;

-- name: DeleteQuestion :exec
DELETE FROM questions WHERE id = $1;
/* ON DELETE CASCADE property on corresponding options, no need to delete them manually */

-- name: DeleteCorrespondingOption :exec
DELETE FROM options WHERE question_id = $1;

-- name: SubmitAnswer :one
INSERT INTO answers(question_id, selected_option_id, text_answer)
VALUES ($1, $2, $3)
RETURNING *;

-- name: ListAnswer :many
SELECT * FROM answers WHERE question_id = $1;