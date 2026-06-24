ALTER TABLE chats
    DROP COLUMN IF EXISTS user_id,
    DROP COLUMN IF EXISTS title,
    DROP COLUMN IF EXISTS updated_at;

ALTER TABLE messages
    DROP CONSTRAINT messages_chat_id_fkey,
    ADD CONSTRAINT messages_chat_id_fkey
        FOREIGN KEY (chat_id) REFERENCES chats(id);

