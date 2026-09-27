CREATE TABLE answer_results (
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
