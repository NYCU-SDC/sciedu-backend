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

-- name: LockExperimentByID :one
SELECT *
FROM experiments
WHERE id = $1
FOR UPDATE;

-- name: LockExperimentParticipantUsers :many
SELECT u.id
FROM users u
JOIN experiment_participants ep ON ep.user_id = u.id
WHERE ep.experiment_id = $1
ORDER BY u.id
FOR UPDATE OF u;

-- name: HasParticipantScheduleConflict :one
SELECT EXISTS (
    SELECT 1
    FROM experiment_participants ep
    JOIN experiments e ON e.id = ep.experiment_id
    WHERE ep.user_id = ANY(sqlc.arg('user_ids')::uuid[])
      AND e.id <> sqlc.arg('experiment_id')
      AND e.status <> 'ARCHIVED'
      AND e.scheduled_start_at < sqlc.arg('scheduled_end_at')::timestamptz
      AND sqlc.arg('scheduled_start_at')::timestamptz < e.scheduled_end_at
);

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

-- name: ListExperimentParticipants :many
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
ORDER BY ep.assigned_at DESC, u.id
LIMIT sqlc.arg('limit')::int OFFSET sqlc.arg('offset')::bigint;

-- name: LockParticipantCandidates :many
SELECT id,
       email,
       name,
       avatar_url,
       roles::text[] AS roles,
       disabled_at,
       created_at,
       updated_at
FROM users
WHERE id = ANY(sqlc.arg('user_ids')::uuid[])
ORDER BY id
FOR UPDATE;

-- name: AddExperimentParticipants :many
WITH inserted AS (
    INSERT INTO experiment_participants (experiment_id, user_id)
    SELECT sqlc.arg('experiment_id'), requested.user_id
    FROM unnest(sqlc.arg('user_ids')::uuid[]) AS requested(user_id)
    RETURNING user_id, assigned_at
)
SELECT u.id,
       u.email,
       u.name,
       u.avatar_url,
       u.roles::text[] AS roles,
       u.created_at,
       u.updated_at,
       inserted.assigned_at
FROM inserted
JOIN users u ON u.id = inserted.user_id
ORDER BY u.id;

-- name: RemoveExperimentParticipant :execrows
DELETE FROM experiment_participants
WHERE experiment_id = sqlc.arg('experiment_id')
  AND user_id = sqlc.arg('user_id');

-- name: CountExperimentCourses :one
SELECT count(*)
FROM experiment_courses
WHERE experiment_id = $1;
