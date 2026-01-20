CREATE TABLE IF NOT EXISTS answers (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    question_id UUID NOT NULL REFERENCES questions(id) ON DELETE CASCADE,
    selected_option_id UUID REFERENCES options(id) ON DELETE CASCADE,
    text_answer TEXT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);