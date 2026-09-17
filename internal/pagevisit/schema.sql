CREATE TABLE page_visits (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    student_id UUID NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    course_id UUID NOT NULL REFERENCES courses (id) ON DELETE CASCADE,
    page_id UUID NOT NULL REFERENCES pages (id) ON DELETE CASCADE,
    client_session_id UUID NOT NULL,
    idempotency_key UUID NOT NULL,
    entered_at TIMESTAMPTZ NOT NULL,
    left_at TIMESTAMPTZ,
    CONSTRAINT page_visits_student_idempotency_key_unique
        UNIQUE (student_id, idempotency_key),
    CONSTRAINT page_visits_left_at_not_before_entered_at
        CHECK (left_at IS NULL OR left_at >= entered_at)
);

CREATE UNIQUE INDEX page_visits_one_open_per_student_session_idx
    ON page_visits (student_id, client_session_id)
    WHERE left_at IS NULL;

CREATE INDEX page_visits_entered_at_idx
    ON page_visits (entered_at DESC);

CREATE INDEX page_visits_course_id_idx
    ON page_visits (course_id);

CREATE INDEX page_visits_page_id_idx
    ON page_visits (page_id);

CREATE INDEX page_visits_client_session_id_idx
    ON page_visits (client_session_id);
