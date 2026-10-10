-- name: UpsertReach :one
INSERT INTO page_progress (student_id, page_id, course_id, reached_at)
SELECT sqlc.arg('student_id'), p.id, p.course_id, sqlc.arg('reached_at')
FROM pages p
WHERE p.id = sqlc.arg('page_id')
ON CONFLICT (student_id, page_id) DO UPDATE
    SET updated_at = now()
RETURNING student_id, page_id, course_id, reached_at, completed_at, created_at, updated_at;

-- name: UpsertComplete :one
INSERT INTO page_progress (student_id, page_id, course_id, reached_at, completed_at)
SELECT sqlc.arg('student_id'), p.id, p.course_id, sqlc.arg('completed_at'), sqlc.arg('completed_at')
FROM pages p
WHERE p.id = sqlc.arg('page_id')
ON CONFLICT (student_id, page_id) DO UPDATE
    SET completed_at = COALESCE(page_progress.completed_at, EXCLUDED.completed_at),
        updated_at = now()
RETURNING student_id, page_id, course_id, reached_at, completed_at, created_at, updated_at;

-- name: PageByID :one
SELECT id, course_id, title, display_order
FROM pages
WHERE id = sqlc.arg('page_id');

-- name: PagesByCourse :many
SELECT id, course_id, title, display_order
FROM pages
WHERE course_id = sqlc.arg('course_id')
ORDER BY display_order, id;

-- name: ProgressByStudentCourse :many
SELECT pp.student_id,
       pp.page_id,
       pp.course_id,
       pp.reached_at,
       pp.completed_at,
       pp.created_at,
       pp.updated_at
FROM page_progress pp
JOIN pages p ON p.id = pp.page_id
WHERE pp.student_id = sqlc.arg('student_id')
  AND pp.course_id = sqlc.arg('course_id')
ORDER BY p.display_order, p.id;

-- name: ListCourseParticipants :many
SELECT u.id,
       u.email,
       u.name,
       u.avatar_url,
       u.roles::text[] AS roles,
       u.created_at,
       u.updated_at,
       ep.assigned_at
FROM experiment_participants ep
JOIN users u ON u.id = ep.user_id
WHERE ep.experiment_id = sqlc.arg('experiment_id')
ORDER BY ep.assigned_at, u.id
LIMIT sqlc.arg('limit')::int OFFSET sqlc.arg('offset')::bigint;

-- name: CountCourseParticipants :one
SELECT count(*)
FROM experiment_participants
WHERE experiment_id = sqlc.arg('experiment_id');

-- name: ProgressForParticipants :many
SELECT pp.student_id,
       pp.page_id,
       pp.reached_at,
       pp.completed_at
FROM page_progress pp
WHERE pp.course_id = sqlc.arg('course_id')
  AND pp.student_id = ANY(sqlc.arg('student_ids')::uuid[]);

-- name: ExperimentCourseExists :one
SELECT EXISTS (
    SELECT 1
    FROM experiment_courses
    WHERE experiment_id = sqlc.arg('experiment_id')
      AND course_id = sqlc.arg('course_id')
);

-- name: ParticipantInExperiment :one
SELECT EXISTS (
    SELECT 1
    FROM experiment_participants
    WHERE experiment_id = sqlc.arg('experiment_id')
      AND user_id = sqlc.arg('student_id')
);
