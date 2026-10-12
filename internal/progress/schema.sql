CREATE TABLE page_progress (
    student_id    UUID NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    experiment_id UUID NOT NULL REFERENCES experiments (id) ON DELETE CASCADE,
    page_id       UUID NOT NULL REFERENCES pages (id) ON DELETE CASCADE,
    course_id     UUID NOT NULL REFERENCES courses (id) ON DELETE CASCADE,
    reached_at    TIMESTAMPTZ NOT NULL,
    completed_at  TIMESTAMPTZ,
    created_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (student_id, experiment_id, page_id),
    CONSTRAINT page_progress_completed_at_not_before_reached_at
        CHECK (completed_at IS NULL OR completed_at >= reached_at)
);

CREATE INDEX page_progress_course_id_idx
    ON page_progress (course_id);
