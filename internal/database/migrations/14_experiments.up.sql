CREATE TYPE experiment_status AS ENUM (
    'DRAFT',
    'SCHEDULED',
    'ACTIVE',
    'COMPLETED',
    'ARCHIVED'
);

CREATE TABLE experiments (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    created_by UUID NOT NULL REFERENCES users (id),
    name VARCHAR(200) NOT NULL CHECK (btrim(name) <> ''),
    description VARCHAR(4000),
    configuration JSONB NOT NULL CHECK (
        jsonb_typeof(configuration) = 'object'
        AND configuration ?& ARRAY[
            'maxAttempts',
            'allowRetry',
            'showScore',
            'showExplanations',
            'gradingMode',
            'correctAnswerReleaseMode'
        ]
        AND configuration
            - 'maxAttempts'
            - 'allowRetry'
            - 'showScore'
            - 'showExplanations'
            - 'gradingMode'
            - 'correctAnswerReleaseMode' = '{}'::jsonb
    ),
    status experiment_status NOT NULL DEFAULT 'DRAFT',
    scheduled_start_at TIMESTAMPTZ NOT NULL,
    scheduled_end_at TIMESTAMPTZ NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    CHECK (scheduled_end_at > scheduled_start_at)
);

CREATE TABLE experiment_participants (
    experiment_id UUID NOT NULL REFERENCES experiments (id) ON DELETE CASCADE,
    user_id UUID NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    assigned_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (experiment_id, user_id)
);

CREATE INDEX experiment_participants_user_id_idx ON experiment_participants (user_id);

-- The courses table is owned by the course domain. Its migration must add the
-- course_id foreign key after that table exists.
CREATE TABLE experiment_courses (
    experiment_id UUID NOT NULL REFERENCES experiments (id) ON DELETE CASCADE,
    course_id UUID NOT NULL,
    linked_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (experiment_id, course_id)
);

CREATE INDEX experiment_courses_course_id_idx ON experiment_courses (course_id);
