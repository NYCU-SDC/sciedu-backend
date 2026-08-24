-- name: ListPagesByCourse :many
SELECT id, course_id, title, display_order, created_at, updated_at
FROM pages
WHERE course_id = $1
ORDER BY display_order;

-- name: GetPageByID :one
SELECT id, course_id, title, display_order, created_at, updated_at
FROM pages
WHERE id = $1;

-- name: CreatePage :one
INSERT INTO pages (course_id, title, display_order)
VALUES ($1, $2, $3)
RETURNING id, course_id, title, display_order, created_at, updated_at;

-- name: UpdatePage :one
UPDATE pages
SET title = $2,
    display_order = $3,
    updated_at = NOW()
WHERE id = $1
RETURNING id, course_id, title, display_order, created_at, updated_at;

-- name: DeletePage :execrows
DELETE FROM pages
WHERE id = $1;

-- name: DeferOrderConstraints :exec
-- Both deferrable constraints in this schema are display-order uniqueness
-- constraints. Defer them until commit so reorder cycles can move directly to
-- their final positions.
SET CONSTRAINTS ALL DEFERRED;

-- name: SetPageOrders :many
-- Array position becomes the new display_order. The unique constraint is
-- deferred by the caller and rechecked when the transaction commits.
UPDATE pages
SET display_order = data.ord - 1,
    updated_at = NOW()
FROM unnest(sqlc.arg(page_ids)::uuid[]) WITH ORDINALITY AS data(id, ord)
WHERE pages.id = data.id
  AND pages.course_id = sqlc.arg(course_id)
RETURNING pages.id, pages.course_id, pages.title, pages.display_order, pages.created_at, pages.updated_at;
