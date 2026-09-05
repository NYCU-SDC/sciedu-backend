DROP INDEX IF EXISTS answers_question_experiment_created_idx;

ALTER TABLE answers
    DROP CONSTRAINT IF EXISTS answers_experiment_user_question_unique,
    DROP COLUMN IF EXISTS experiment_id;
