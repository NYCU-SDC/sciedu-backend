-- name: GetCorrectAnswer :one
SELECT question_id, type, selected_option_id, reference_answer, version,
       answer_result_sync_status, updated_at
FROM correct_answers WHERE question_id = $1;

-- name: PendingAnswerSynchronizations :many
SELECT question_id, version FROM correct_answers
WHERE answer_result_sync_status = 'PENDING'
ORDER BY updated_at, question_id
LIMIT 100;

-- name: UpsertCorrectAnswer :one
WITH eligible AS (
    SELECT EXISTS (
        SELECT 1 FROM answers AS a
         JOIN experiments AS e ON e.id = a.experiment_id
         LEFT JOIN answer_results AS ar ON ar.answer_id = a.id
        WHERE a.question_id = sqlc.arg(question_id)
          AND sqlc.arg(type) = 'CHOICE'
           AND e.configuration->>'gradingMode' = 'AUTOMATIC'
           AND a.selected_option_id IS NOT NULL
           AND ar.method IS DISTINCT FROM 'MANUAL'
    ) AS present
)
INSERT INTO correct_answers (question_id, type, selected_option_id, reference_answer, answer_result_sync_status)
SELECT sqlc.arg(question_id), sqlc.arg(type), sqlc.narg(selected_option_id),
       sqlc.narg(reference_answer), CASE WHEN eligible.present THEN 'PENDING' ELSE 'SYNCED' END
FROM eligible
ON CONFLICT (question_id) DO UPDATE
SET type = EXCLUDED.type,
    selected_option_id = EXCLUDED.selected_option_id,
    reference_answer = EXCLUDED.reference_answer,
    version = CASE WHEN correct_answers.type = EXCLUDED.type
      AND correct_answers.selected_option_id IS NOT DISTINCT FROM EXCLUDED.selected_option_id
      AND correct_answers.reference_answer IS NOT DISTINCT FROM EXCLUDED.reference_answer
      THEN correct_answers.version ELSE correct_answers.version + 1 END,
    answer_result_sync_status = CASE WHEN correct_answers.type = EXCLUDED.type
      AND correct_answers.selected_option_id IS NOT DISTINCT FROM EXCLUDED.selected_option_id
      AND correct_answers.reference_answer IS NOT DISTINCT FROM EXCLUDED.reference_answer
      AND correct_answers.answer_result_sync_status <> 'FAILED'
      THEN correct_answers.answer_result_sync_status ELSE EXCLUDED.answer_result_sync_status END,
    updated_at = CASE WHEN correct_answers.type = EXCLUDED.type
      AND correct_answers.selected_option_id IS NOT DISTINCT FROM EXCLUDED.selected_option_id
      AND correct_answers.reference_answer IS NOT DISTINCT FROM EXCLUDED.reference_answer
      AND correct_answers.answer_result_sync_status <> 'FAILED'
      THEN correct_answers.updated_at ELSE NOW() END
RETURNING question_id, type, selected_option_id, reference_answer, version,
          answer_result_sync_status, updated_at;

-- name: UpdateAnswerResultSyncStatusForVersion :one
UPDATE correct_answers
SET answer_result_sync_status = sqlc.arg(status), updated_at = NOW()
WHERE question_id = sqlc.arg(question_id) AND version = sqlc.arg(target_version)
  AND (sqlc.arg(status) <> 'FAILED' OR answer_result_sync_status = 'PENDING')
RETURNING question_id, type, selected_option_id, reference_answer, version,
          answer_result_sync_status, updated_at;
