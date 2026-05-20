-- name: CreateTextContent :one
INSERT INTO contents (type, content)
VALUES ('TEXT', $1)
RETURNING id, type, content;

-- name: CreateMediaContent :one
INSERT INTO contents (type, content)
VALUES ('MEDIA', $1)
RETURNING id, type, content;

-- name: GetTextContent :one
SELECT id, type, content
FROM contents
WHERE id = $1
  AND type = 'TEXT';

-- name: GetMediaContent :one
SELECT id, type, content
FROM contents
WHERE id = $1
  AND type = 'MEDIA';

-- name: GetContent :one
SELECT id, type, content
FROM contents
WHERE id = $1;

-- name: ListTextContents :many
SELECT id, type, content
FROM contents
WHERE type = 'TEXT'
ORDER BY id
LIMIT $1
OFFSET $2;

-- name: CountTextContents :one
SELECT COUNT(*)
FROM contents
WHERE type = 'TEXT';

-- name: BatchGetTextContents :many
SELECT id, type, content
FROM contents
WHERE type = 'TEXT'
  AND id = ANY($1::uuid[])
ORDER BY array_position($1::uuid[], id);

-- name: DeleteContent :exec
DELETE FROM contents
WHERE id = $1;
