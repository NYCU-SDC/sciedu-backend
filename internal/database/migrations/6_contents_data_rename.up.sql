DO $$
BEGIN
    IF EXISTS (
        SELECT 1
        FROM information_schema.columns
        WHERE table_schema = 'public'
          AND table_name = 'contents'
          AND column_name = 'content'
    ) AND NOT EXISTS (
        SELECT 1
        FROM information_schema.columns
        WHERE table_schema = 'public'
          AND table_name = 'contents'
          AND column_name = 'data'
    ) THEN
        ALTER TABLE contents RENAME COLUMN content TO data;
    END IF;
END $$;
