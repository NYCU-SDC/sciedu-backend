CREATE TABLE IF NOT EXISTS courses (
    id             UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    code           VARCHAR(100) NOT NULL,
    title          VARCHAR(200) NOT NULL,
    description    VARCHAR(4000),
    status         TEXT NOT NULL DEFAULT 'DRAFT' CHECK (status IN ('DRAFT', 'PUBLISHED', 'ARCHIVED')),
    created_at     TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at     TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE UNIQUE INDEX IF NOT EXISTS courses_code_unique_idx ON courses (LOWER(code));
