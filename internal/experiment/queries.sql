-- name: ListExperiments :many
SELECT id,
       created_by,
       name,
       description,
       configuration,
       status,
       scheduled_start_at,
       scheduled_end_at,
       created_at,
       updated_at
FROM experiments
WHERE (sqlc.narg('status')::experiment_status IS NULL OR status = sqlc.narg('status')::experiment_status)
  AND (sqlc.narg('scheduled_from')::timestamptz IS NULL OR scheduled_end_at >= sqlc.narg('scheduled_from')::timestamptz)
  AND (sqlc.narg('scheduled_to')::timestamptz IS NULL OR scheduled_start_at <= sqlc.narg('scheduled_to')::timestamptz)
  AND (sqlc.narg('search')::text IS NULL OR strpos(lower(name), lower(sqlc.narg('search')::text)) > 0)
ORDER BY created_at DESC, id
LIMIT sqlc.arg('limit')::int OFFSET sqlc.arg('offset')::bigint;

-- name: CountExperiments :one
SELECT count(*)
FROM experiments
WHERE (sqlc.narg('status')::experiment_status IS NULL OR status = sqlc.narg('status')::experiment_status)
  AND (sqlc.narg('scheduled_from')::timestamptz IS NULL OR scheduled_end_at >= sqlc.narg('scheduled_from')::timestamptz)
  AND (sqlc.narg('scheduled_to')::timestamptz IS NULL OR scheduled_start_at <= sqlc.narg('scheduled_to')::timestamptz)
  AND (sqlc.narg('search')::text IS NULL OR strpos(lower(name), lower(sqlc.narg('search')::text)) > 0);

-- name: CreateExperiment :one
INSERT INTO experiments (
    created_by,
    name,
    description,
    configuration,
    scheduled_start_at,
    scheduled_end_at
) VALUES (
    sqlc.arg('created_by'),
    sqlc.arg('name'),
    sqlc.narg('description'),
    sqlc.arg('configuration'),
    sqlc.arg('scheduled_start_at'),
    sqlc.arg('scheduled_end_at')
)
RETURNING *;

-- name: ExperimentByID :one
SELECT *
FROM experiments
WHERE id = $1;

-- name: UpdateExperiment :one
UPDATE experiments
SET name = sqlc.arg('name'),
    description = sqlc.narg('description'),
    configuration = sqlc.arg('configuration'),
    scheduled_start_at = sqlc.arg('scheduled_start_at'),
    scheduled_end_at = sqlc.arg('scheduled_end_at'),
    updated_at = now()
WHERE id = sqlc.arg('id')
RETURNING *;

-- name: UpdateExperimentStatus :one
UPDATE experiments
SET status = sqlc.arg('status'),
    updated_at = now()
WHERE id = sqlc.arg('id')
RETURNING *;

-- name: CountExperimentParticipants :one
SELECT count(*)
FROM experiment_participants
WHERE experiment_id = $1;

-- name: CountExperimentCourses :one
SELECT count(*)
FROM experiment_courses
WHERE experiment_id = $1;

-- name: StudentCanAccessCourse :one
SELECT EXISTS (
    SELECT 1
    FROM experiment_participants ep
    JOIN experiments e ON e.id = ep.experiment_id
    JOIN experiment_courses ec ON ec.experiment_id = e.id
    JOIN courses c ON c.id = ec.course_id
    WHERE ep.user_id = sqlc.arg('student_id')
      AND ec.course_id = sqlc.arg('course_id')
      AND e.status = 'ACTIVE'
      AND e.scheduled_start_at <= CURRENT_TIMESTAMP
      AND e.scheduled_end_at >= CURRENT_TIMESTAMP
      AND c.status = 'PUBLISHED'
);
