INSERT INTO users (id, email, name, roles)
VALUES ('00000000-0000-0000-0000-000000000001', 'mock@dev.local', 'Mock User', ARRAY['STUDENT']::user_role[])
ON CONFLICT (id) DO NOTHING;

ALTER TABLE chats
    ADD COLUMN user_id UUID,
    ADD COLUMN title VARCHAR(255) NOT NULL DEFAULT '',
    ADD COLUMN updated_at TIMESTAMPTZ NOT NULL DEFAULT now();

UPDATE chats
SET user_id = '00000000-0000-0000-0000-000000000001'
WHERE user_id IS NULL;

ALTER TABLE chats
    ALTER COLUMN user_id SET NOT NULL,
    ADD CONSTRAINT chats_user_id_fkey
        FOREIGN KEY (user_id) REFERENCES users(id);

ALTER TABLE messages
    DROP CONSTRAINT IF EXISTS messages_chat_id_fkey,
    ADD CONSTRAINT messages_chat_id_fkey
        FOREIGN KEY (chat_id) REFERENCES chats(id) ON DELETE CASCADE;
