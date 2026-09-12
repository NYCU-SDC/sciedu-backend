-- name: LockPageVisitStudent :one
SELECT id
FROM users
WHERE id = sqlc.arg('student_id')
FOR UPDATE;

-- name: PageVisitByIdempotencyKey :one
SELECT *
FROM page_visits
WHERE student_id = sqlc.arg('student_id')
  AND idempotency_key = sqlc.arg('idempotency_key');

-- name: PageVisitByIDForStudent :one
SELECT *
FROM page_visits
WHERE id = sqlc.arg('id')
  AND student_id = sqlc.arg('student_id');

-- name: LockPageVisitTarget :one
SELECT pages.id
FROM pages
JOIN courses ON courses.id = pages.course_id
WHERE pages.id = sqlc.arg('page_id')
  AND courses.id = sqlc.arg('course_id')
FOR KEY SHARE OF pages, courses;

-- name: OpenPageVisitForStudentSession :one
SELECT *
FROM page_visits
WHERE student_id = sqlc.arg('student_id')
  AND client_session_id = sqlc.arg('client_session_id')
  AND left_at IS NULL
FOR UPDATE;

-- name: CloseOpenPageVisit :one
UPDATE page_visits
SET left_at = sqlc.arg('left_at')
WHERE id = sqlc.arg('id')
  AND student_id = sqlc.arg('student_id')
  AND left_at IS NULL
RETURNING *;

-- name: CreatePageVisit :one
INSERT INTO page_visits (
    student_id,
    course_id,
    page_id,
    client_session_id,
    idempotency_key,
    entered_at
) VALUES (
    sqlc.arg('student_id'),
    sqlc.arg('course_id'),
    sqlc.arg('page_id'),
    sqlc.arg('client_session_id'),
    sqlc.arg('idempotency_key'),
    sqlc.arg('entered_at')
)
RETURNING *;

-- name: LeavePageVisitIfOpen :one
UPDATE page_visits
SET left_at = sqlc.arg('left_at')
WHERE id = sqlc.arg('id')
  AND student_id = sqlc.arg('student_id')
  AND left_at IS NULL
RETURNING *;

-- name: ListPageVisits :many
SELECT *
FROM page_visits
WHERE (sqlc.narg('student_id')::uuid IS NULL OR student_id = sqlc.narg('student_id')::uuid)
  AND (sqlc.narg('course_id')::uuid IS NULL OR course_id = sqlc.narg('course_id')::uuid)
  AND (sqlc.narg('page_id')::uuid IS NULL OR page_id = sqlc.narg('page_id')::uuid)
  AND (sqlc.narg('client_session_id')::uuid IS NULL OR client_session_id = sqlc.narg('client_session_id')::uuid)
  AND (
      sqlc.narg('status')::text IS NULL
      OR (sqlc.narg('status')::text = 'OPEN' AND left_at IS NULL)
      OR (sqlc.narg('status')::text = 'CLOSED' AND left_at IS NOT NULL)
  )
  AND (sqlc.narg('entered_from')::timestamptz IS NULL OR entered_at >= sqlc.narg('entered_from')::timestamptz)
  AND (sqlc.narg('entered_before')::timestamptz IS NULL OR entered_at < sqlc.narg('entered_before')::timestamptz)
ORDER BY entered_at DESC, id
LIMIT sqlc.arg('limit')::int OFFSET sqlc.arg('offset')::bigint;

-- name: CountPageVisits :one
SELECT count(*)
FROM page_visits
WHERE (sqlc.narg('student_id')::uuid IS NULL OR student_id = sqlc.narg('student_id')::uuid)
  AND (sqlc.narg('course_id')::uuid IS NULL OR course_id = sqlc.narg('course_id')::uuid)
  AND (sqlc.narg('page_id')::uuid IS NULL OR page_id = sqlc.narg('page_id')::uuid)
  AND (sqlc.narg('client_session_id')::uuid IS NULL OR client_session_id = sqlc.narg('client_session_id')::uuid)
  AND (
      sqlc.narg('status')::text IS NULL
      OR (sqlc.narg('status')::text = 'OPEN' AND left_at IS NULL)
      OR (sqlc.narg('status')::text = 'CLOSED' AND left_at IS NOT NULL)
  )
  AND (sqlc.narg('entered_from')::timestamptz IS NULL OR entered_at >= sqlc.narg('entered_from')::timestamptz)
  AND (sqlc.narg('entered_before')::timestamptz IS NULL OR entered_at < sqlc.narg('entered_before')::timestamptz);
