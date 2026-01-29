CREATE TABLE IF NOT EXISTS questions (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    type TEXT NOT NULL,
    content TEXT NOT NULL
    /* get options from 2_options.up.sql */
    /* get answers from 3_answers.up.sql */
);