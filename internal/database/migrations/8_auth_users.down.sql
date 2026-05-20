DROP TABLE IF EXISTS oauth_login_states;
DROP TABLE IF EXISTS refresh_tokens;
DROP TABLE IF EXISTS refresh_token_families;
DROP TABLE IF EXISTS oauth_accounts;

ALTER TABLE users
    DROP CONSTRAINT IF EXISTS users_roles_not_empty,
    DROP CONSTRAINT IF EXISTS users_name_not_empty,
    DROP COLUMN IF EXISTS disabled_at,
    DROP COLUMN IF EXISTS last_login_at,
    ALTER COLUMN roles TYPE TEXT[] USING roles::TEXT[],
    ALTER COLUMN name TYPE VARCHAR(255),
    ALTER COLUMN email TYPE VARCHAR(255),
    ADD CONSTRAINT users_roles_check CHECK (roles <@ ARRAY['STUDENT', 'EXPERIMENTER', 'ADMIN']);

DROP TYPE IF EXISTS user_role;
