-- name: ListBlocksByPage :many
SELECT id, page_id, content_id, question_id, display_order, required, created_at, updated_at
FROM page_blocks
WHERE page_id = $1
ORDER BY display_order;

-- name: GetBlockByID :one
SELECT id, page_id, content_id, question_id, display_order, required, created_at, updated_at
FROM page_blocks
WHERE id = $1;

-- name: CreateContentBlock :one
INSERT INTO page_blocks (page_id, content_id, display_order, required)
VALUES ($1, $2, $3, $4)
RETURNING id, page_id, content_id, question_id, display_order, required, created_at, updated_at;

-- name: CreateQuestionBlock :one
INSERT INTO page_blocks (page_id, question_id, display_order, required)
VALUES ($1, $2, $3, $4)
RETURNING id, page_id, content_id, question_id, display_order, required, created_at, updated_at;

-- name: UpdateContentBlock :one
UPDATE page_blocks
SET content_id = $2,
    question_id = NULL,
    display_order = $3,
    required = $4,
    updated_at = NOW()
WHERE id = $1
RETURNING id, page_id, content_id, question_id, display_order, required, created_at, updated_at;

-- name: UpdateQuestionBlock :one
UPDATE page_blocks
SET question_id = $2,
    content_id = NULL,
    display_order = $3,
    required = $4,
    updated_at = NOW()
WHERE id = $1
RETURNING id, page_id, content_id, question_id, display_order, required, created_at, updated_at;

-- name: DeleteBlock :execrows
DELETE FROM page_blocks
WHERE id = sqlc.arg(id)
  AND page_id = sqlc.arg(page_id);

-- name: LockPageForBlockReorder :one
-- The foreign key check for a concurrent PageBlock insert takes a key-share
-- lock on this Page row. FOR UPDATE freezes membership for the transaction.
SELECT id
FROM pages
WHERE id = $1
FOR UPDATE;

-- name: SetBlockOrders :many
-- Array position becomes the new display_order. The unique constraint is
-- deferred by the caller and rechecked when the transaction commits.
UPDATE page_blocks
SET display_order = data.ord - 1,
    updated_at = NOW()
FROM unnest(sqlc.arg(block_ids)::uuid[]) WITH ORDINALITY AS data(id, ord)
WHERE page_blocks.id = data.id
  AND page_blocks.page_id = sqlc.arg(page_id)
RETURNING page_blocks.id, page_blocks.page_id, page_blocks.content_id, page_blocks.question_id,
          page_blocks.display_order, page_blocks.required, page_blocks.created_at, page_blocks.updated_at;
