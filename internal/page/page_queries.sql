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

-- name: DeletePage :exec
DELETE FROM pages
WHERE id = $1;

-- name: OffsetPageOrders :exec
-- Step 1 of the temp-offset reorder: bump every page in the course out of the
-- way so step 2 can't collide with UNIQUE(course_id, display_order).
UPDATE pages
SET display_order = display_order + 100000
WHERE course_id = $1;

-- name: SetPageOrders :many
-- Step 2 of the temp-offset reorder: write final display_order values from
-- the caller-supplied order of page IDs (position in the array = new order).
-- Must run in the same transaction as OffsetPageOrders.
UPDATE pages
SET display_order = data.ord - 1,
    updated_at = NOW()
FROM unnest($2::uuid[]) WITH ORDINALITY AS data(id, ord)
WHERE pages.id = data.id
  AND pages.course_id = $1
RETURNING pages.id, pages.course_id, pages.title, pages.display_order, pages.created_at, pages.updated_at;
