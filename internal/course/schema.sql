CREATE TYPE course_status AS ENUM ('DRAFT', 'PUBLISHED', 'ARCHIVED');

CREATE TABLE courses (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    code VARCHAR(100) NOT NULL,
    title VARCHAR(200) NOT NULL,
    description VARCHAR(4000),
    status course_status NOT NULL DEFAULT 'DRAFT',
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT courses_code_not_empty CHECK (btrim(code) <> ''),
    CONSTRAINT courses_title_not_empty CHECK (btrim(title) <> '')
);

CREATE UNIQUE INDEX courses_code_lower_unique
ON courses (lower(code));
