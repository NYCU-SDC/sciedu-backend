CREATE EXTENSION IF NOT EXISTS pgcrypto;

CREATE TABLE IF NOT EXISTS questions (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    content TEXT NOT NULL,
    type TEXT NOT NULL CHECK (type IN ('CHOICE', 'TEXT')),
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE TABLE IF NOT EXISTS options (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    question_id UUID NOT NULL REFERENCES questions(id) ON DELETE CASCADE,
    content TEXT NOT NULL,
    label TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (question_id, label)
);

CREATE TABLE IF NOT EXISTS correct_answers (
    question_id UUID PRIMARY KEY REFERENCES questions(id) ON DELETE CASCADE,
    type TEXT NOT NULL CHECK (type IN ('CHOICE', 'TEXT')),
    selected_option_id UUID REFERENCES options(id) ON DELETE CASCADE,
    reference_answer TEXT,
    version BIGINT NOT NULL DEFAULT 1 CHECK (version >= 1),
    answer_result_sync_status TEXT NOT NULL DEFAULT 'SYNCED'
        CHECK (answer_result_sync_status IN ('PENDING', 'SYNCED', 'FAILED')),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT correct_answers_payload_check CHECK (
        (type = 'CHOICE' AND selected_option_id IS NOT NULL AND reference_answer IS NULL)
        OR
        (type = 'TEXT' AND selected_option_id IS NULL AND reference_answer IS NOT NULL
            AND char_length(reference_answer) BETWEEN 1 AND 2000)
    )
);

CREATE TABLE IF NOT EXISTS answers (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    question_id UUID NOT NULL REFERENCES questions(id) ON DELETE CASCADE,
    user_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    experiment_id UUID NOT NULL REFERENCES experiments(id) ON DELETE RESTRICT,
    selected_option_id UUID REFERENCES options(id) ON DELETE SET NULL,
    text_answer TEXT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (experiment_id, user_id, question_id)
);

CREATE INDEX answers_question_experiment_created_idx
    ON answers (question_id, experiment_id, created_at DESC, id DESC);

CREATE TABLE IF NOT EXISTS answer_results (
    answer_id UUID PRIMARY KEY REFERENCES answers(id) ON DELETE CASCADE,
    status TEXT NOT NULL CHECK (status IN ('PENDING', 'GRADED', 'FAILED')),
    method TEXT CHECK (method IN ('DETERMINISTIC', 'MANUAL', 'LLM')),
    is_correct BOOLEAN,
    graded_at TIMESTAMPTZ,
    correct_answer_version BIGINT CHECK (correct_answer_version >= 1),
    CONSTRAINT answer_results_correctness_check CHECK (
        (status = 'GRADED' AND is_correct IS NOT NULL AND graded_at IS NOT NULL)
        OR
        (status IN ('PENDING', 'FAILED') AND is_correct IS NULL)
    ),
    CONSTRAINT answer_results_deterministic_version_check CHECK (
        NOT (status = 'GRADED' AND method = 'DETERMINISTIC')
        OR correct_answer_version IS NOT NULL
    )
);

CREATE FUNCTION invalidate_deleted_correct_answer_results() RETURNS trigger
LANGUAGE plpgsql AS $$
BEGIN
    UPDATE answer_results AS ar
    SET status = 'PENDING', is_correct = NULL, graded_at = NULL,
        correct_answer_version = NULL
    FROM answers AS a
    WHERE ar.answer_id = a.id AND a.question_id = OLD.question_id
      AND ar.method = 'DETERMINISTIC';
    RETURN OLD;
END;
$$;

CREATE TRIGGER correct_answer_delete_invalidation
AFTER DELETE ON correct_answers
FOR EACH ROW EXECUTE FUNCTION invalidate_deleted_correct_answer_results();
