DO $$
BEGIN
    IF EXISTS (
        SELECT 1
        FROM information_schema.columns
        WHERE table_schema = 'public'
          AND table_name = 'contents'
          AND column_name = 'data'
    ) AND NOT EXISTS (
        SELECT 1
        FROM information_schema.columns
        WHERE table_schema = 'public'
          AND table_name = 'contents'
          AND column_name = 'content'
    ) THEN
        ALTER TABLE contents RENAME COLUMN data TO content;
    END IF;
END $$;
