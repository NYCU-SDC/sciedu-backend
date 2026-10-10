-- Answers created before Experiment provenance existed cannot be assigned safely.
-- The maintainer approved clearing this legacy data instead of guessing a backfill.
DELETE FROM answers;

ALTER TABLE answers
    ADD COLUMN experiment_id UUID NOT NULL
        REFERENCES experiments(id) ON DELETE RESTRICT,
    ADD CONSTRAINT answers_experiment_user_question_unique
        UNIQUE (experiment_id, user_id, question_id);

CREATE INDEX answers_question_experiment_created_idx
    ON answers (question_id, experiment_id, created_at DESC, id DESC);
