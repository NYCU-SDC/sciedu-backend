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

-- name: CourseForStudent :one
SELECT c.id,
       c.code,
       c.title,
       c.description,
       c.status,
       c.created_at,
       c.updated_at,
       EXISTS (
           SELECT 1
           FROM experiment_participants ep
           JOIN experiments e ON e.id = ep.experiment_id
           JOIN experiment_courses ec ON ec.experiment_id = e.id
           WHERE ep.user_id = sqlc.arg('student_id')
             AND ec.course_id = c.id
             AND e.status = 'ACTIVE'
             AND e.scheduled_start_at <= CURRENT_TIMESTAMP
             AND e.scheduled_end_at >= CURRENT_TIMESTAMP
             AND c.status = 'PUBLISHED'
       ) AS allowed
FROM courses c
WHERE c.id = sqlc.arg('course_id');

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
