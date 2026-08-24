-- name: CreateCourse :one
INSERT INTO courses (code, title, description)
VALUES ($1, $2, $3)
RETURNING id, code, title, description, status, created_at, updated_at;

-- name: ListCourses :many
SELECT id, code, title, description, status, created_at, updated_at
FROM courses
WHERE (
        sqlc.narg('status')::text IS NULL
        OR status::text = sqlc.narg('status')::text
    )
  AND (
        sqlc.narg('search')::text IS NULL
        OR strpos(lower(code), lower(sqlc.narg('search')::text)) > 0
        OR strpos(lower(title), lower(sqlc.narg('search')::text)) > 0
    )
ORDER BY created_at DESC, id
LIMIT sqlc.arg('limit')::int OFFSET sqlc.arg('offset')::int;

-- name: CountCourses :one
SELECT count(*)
FROM courses
WHERE (
        sqlc.narg('status')::text IS NULL
        OR status::text = sqlc.narg('status')::text
    )
  AND (
        sqlc.narg('search')::text IS NULL
        OR strpos(lower(code), lower(sqlc.narg('search')::text)) > 0
        OR strpos(lower(title), lower(sqlc.narg('search')::text)) > 0
    );

-- name: GetCourseByID :one
SELECT id, code, title, description, status, created_at, updated_at
FROM courses
WHERE id = $1;

-- name: UpdateCourse :one
UPDATE courses
SET code = sqlc.arg('code'),
    title = sqlc.arg('title'),
    description = sqlc.narg('description'),
    updated_at = now()
WHERE id = sqlc.arg('id')
RETURNING id, code, title, description, status, created_at, updated_at;

-- name: UpdateCourseStatus :one
UPDATE courses
SET status = sqlc.arg('status'),
    updated_at = now()
WHERE id = sqlc.arg('id')
RETURNING id, code, title, description, status, created_at, updated_at;
