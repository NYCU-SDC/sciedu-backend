CREATE TABLE correct_answers (
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
