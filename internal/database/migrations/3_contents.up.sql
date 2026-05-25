DO $$
BEGIN
    CREATE TYPE content_type AS ENUM ('TEXT', 'MEDIA');
EXCEPTION
    WHEN duplicate_object THEN NULL;
END $$;

CREATE TABLE IF NOT EXISTS contents (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    type content_type NOT NULL,
    content TEXT NOT NULL        /* string if text, filepath if media */
);
