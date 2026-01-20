CREATE TABLE IF NOT EXISTS questions (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    type TEXT NOT NULL,
    content TEXT NOT NULL
    /* get options from 2_options.up.sql */
    /* get answers from 3_answers.up.sql */
);

CREATE TABLE IF NOT EXISTS options (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    question_id UUID NOT NULL REFERENCES questions(id) ON DELETE CASCADE,
    label TEXT NOT NULL,
    content TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS answers (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    question_id UUID NOT NULL REFERENCES questions(id) ON DELETE CASCADE,
    selected_option_id UUID REFERENCES options(id),
    text_answer TEXT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);