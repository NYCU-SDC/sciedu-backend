INSERT INTO users (id, email, name, roles)
VALUES ('00000000-0000-0000-0000-000000000001', 'mock@dev.local', 'Mock User', ARRAY['STUDENT']::user_role[])
ON CONFLICT (id) DO NOTHING;
