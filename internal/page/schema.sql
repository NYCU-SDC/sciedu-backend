CREATE TABLE IF NOT EXISTS pages (
    id             UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    course_id      UUID NOT NULL REFERENCES courses(id) ON DELETE RESTRICT,
    title          VARCHAR(200) NOT NULL,
    display_order  INT NOT NULL CHECK (display_order >= 0),
    created_at     TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at     TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (course_id, display_order)
);

CREATE TABLE IF NOT EXISTS page_blocks (
    id             UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    page_id        UUID NOT NULL REFERENCES pages(id) ON DELETE CASCADE,
    content_id     UUID REFERENCES contents(id) ON DELETE RESTRICT,
    question_id    UUID REFERENCES questions(id) ON DELETE RESTRICT,
    display_order  INT NOT NULL CHECK (display_order >= 0),
    required       BOOL NOT NULL,
    created_at     TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at     TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (page_id, display_order),
    CHECK (
        (content_id IS NOT NULL AND question_id IS NULL) OR
        (content_id IS NULL AND question_id IS NOT NULL)
    )
);
