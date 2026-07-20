-- name: CreateAnswer :one
INSERT INTO answers (question_id, user_id, selected_option_id, text_answer)
VALUES ($1, $2, $3, $4)
RETURNING id, question_id, user_id, selected_option_id, text_answer, created_at, updated_at;

-- name: ListAnswersByQuestionForUser :many
SELECT id, question_id, user_id, selected_option_id, text_answer, created_at, updated_at
FROM answers
WHERE question_id = $1 AND user_id = $2
ORDER BY created_at DESC;
