-- name: ResolveAnswerSubmissionContexts :many
SELECT e.id AS experiment_id, e.configuration
FROM experiment_participants AS ep
JOIN experiments AS e ON e.id = ep.experiment_id
JOIN experiment_courses AS ec ON ec.experiment_id = e.id
JOIN courses AS c ON c.id = ec.course_id
JOIN pages AS p ON p.course_id = c.id
JOIN page_blocks AS pb ON pb.page_id = p.id
WHERE ep.user_id = sqlc.arg(user_id)
  AND pb.question_id = sqlc.arg(question_id)
  AND e.status = 'ACTIVE'
  AND e.scheduled_start_at <= CURRENT_TIMESTAMP
  AND e.scheduled_end_at >= CURRENT_TIMESTAMP
  AND c.status = 'PUBLISHED'
GROUP BY e.id, e.configuration
ORDER BY e.id
LIMIT 2;

-- name: IsAnswerManagementScopeReachable :one
SELECT EXISTS (
    SELECT 1 FROM experiments AS e
    JOIN experiment_courses AS ec ON ec.experiment_id = e.id
    JOIN pages AS p ON p.course_id = ec.course_id
    JOIN page_blocks AS pb ON pb.page_id = p.id
    WHERE e.id = sqlc.arg(experiment_id)
      AND pb.question_id = sqlc.arg(question_id)
) AS reachable;

-- name: AnswerExperimentConfiguration :one
SELECT configuration FROM experiments WHERE id = $1;

-- name: LockAnswerQuestion :one
SELECT id FROM questions WHERE id = $1 FOR UPDATE;

-- name: ListAnswerSynchronizationCandidates :many
SELECT a.id, a.selected_option_id, ca.selected_option_id AS correct_option_id, ca.version
FROM answers AS a
JOIN questions AS q ON q.id = a.question_id
JOIN experiments AS e ON e.id = a.experiment_id
JOIN correct_answers AS ca ON ca.question_id = a.question_id
LEFT JOIN answer_results AS ar ON ar.answer_id = a.id
WHERE a.question_id = sqlc.arg(question_id) AND ca.version = sqlc.arg(target_version)
  AND q.type = 'CHOICE' AND ca.type = 'CHOICE' AND a.selected_option_id IS NOT NULL
  AND e.configuration->>'gradingMode' = 'AUTOMATIC'
  AND ar.method IS DISTINCT FROM 'MANUAL'
  AND (ar.answer_id IS NULL OR ar.correct_answer_version IS NULL
       OR ar.correct_answer_version < ca.version
       OR (ar.correct_answer_version = ca.version AND ar.status <> 'GRADED'))
ORDER BY a.id
LIMIT sqlc.arg(batch_size);

-- name: CreateAnswer :one
INSERT INTO answers (question_id, user_id, experiment_id, selected_option_id, text_answer)
VALUES ($1, $2, $3, $4, $5)
RETURNING id, question_id, user_id, experiment_id, selected_option_id, text_answer, created_at, updated_at;

-- name: CreateAnswerResult :one
INSERT INTO answer_results (answer_id, status, method, is_correct, graded_at, correct_answer_version)
VALUES ($1, $2, $3, $4, $5, $6)
RETURNING answer_id, status, method, is_correct, graded_at, correct_answer_version;

-- name: RegradeAnswerResultForVersion :one
WITH current_answer AS MATERIALIZED (
    SELECT ca.* FROM correct_answers AS ca
    JOIN answers AS a ON a.question_id = ca.question_id
    WHERE a.id = sqlc.arg(answer_id) AND ca.version = sqlc.arg(target_version)
    FOR SHARE OF ca
)
INSERT INTO answer_results AS ar (answer_id, status, method, is_correct, graded_at, correct_answer_version)
SELECT a.id, sqlc.arg(status)::text, sqlc.narg(method)::text, sqlc.narg(is_correct)::boolean,
       sqlc.narg(graded_at)::timestamptz, sqlc.arg(target_version)::bigint
FROM answers AS a
JOIN questions AS q ON q.id = a.question_id
JOIN experiments AS e ON e.id = a.experiment_id
JOIN current_answer AS ca ON ca.question_id = a.question_id
WHERE a.id = sqlc.arg(answer_id) AND ca.version = sqlc.arg(target_version)
  AND q.type = 'CHOICE' AND ca.type = 'CHOICE'
  AND a.selected_option_id IS NOT NULL
  AND e.configuration->>'gradingMode' = 'AUTOMATIC'
  AND sqlc.arg(status) = 'GRADED' AND sqlc.narg(method) = 'DETERMINISTIC'
  AND sqlc.narg(is_correct) = (a.selected_option_id = ca.selected_option_id)
ON CONFLICT (answer_id) DO UPDATE
SET status = EXCLUDED.status, method = EXCLUDED.method, is_correct = EXCLUDED.is_correct,
    graded_at = EXCLUDED.graded_at, correct_answer_version = EXCLUDED.correct_answer_version
WHERE ar.method IS DISTINCT FROM 'MANUAL'
  AND (ar.correct_answer_version IS NULL OR ar.correct_answer_version < EXCLUDED.correct_answer_version
       OR (ar.correct_answer_version = EXCLUDED.correct_answer_version AND ar.status <> 'GRADED'))
RETURNING ar.answer_id, ar.status, ar.method, ar.is_correct, ar.graded_at, ar.correct_answer_version;

-- name: ListAnswersByQuestionAndExperimentPage :many
SELECT a.id, a.question_id, a.user_id, a.experiment_id, a.selected_option_id, a.text_answer,
       a.created_at, ar.status AS grading_status, ar.method AS grading_method, ar.is_correct,
       ar.graded_at, ar.correct_answer_version, ca.version AS current_correct_answer_version
FROM answers AS a
LEFT JOIN answer_results AS ar ON ar.answer_id = a.id
LEFT JOIN correct_answers AS ca ON ca.question_id = a.question_id
WHERE a.question_id = sqlc.arg(question_id) AND a.experiment_id = sqlc.arg(experiment_id)
ORDER BY a.created_at DESC, a.id DESC
LIMIT sqlc.arg(page_size) OFFSET sqlc.arg(page_offset);

-- name: CountAnswersByQuestionAndExperiment :one
SELECT COUNT(*) FROM answers
WHERE question_id = sqlc.arg(question_id) AND experiment_id = sqlc.arg(experiment_id);

-- name: FindAnswerResultByQuestionAndID :one
SELECT a.id AS answer_id, a.question_id, a.user_id, a.experiment_id,
       ar.status AS grading_status, ar.method AS grading_method, ar.is_correct, ar.graded_at,
       ar.correct_answer_version, ca.version AS current_correct_answer_version
FROM answers AS a
LEFT JOIN answer_results AS ar ON ar.answer_id = a.id
LEFT JOIN correct_answers AS ca ON ca.question_id = a.question_id
WHERE a.question_id = sqlc.arg(question_id) AND a.id = sqlc.arg(answer_id);
