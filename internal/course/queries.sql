-- name: GetCourseByID :one
SELECT id, code, title, description, status, created_at, updated_at
FROM courses
WHERE id = $1;
