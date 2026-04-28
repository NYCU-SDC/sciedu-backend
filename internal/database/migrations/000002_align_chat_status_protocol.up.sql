ALTER TYPE message_status ADD VALUE IF NOT EXISTS 'created';
ALTER TYPE message_status RENAME VALUE 'done' TO 'completed';
ALTER TYPE message_status RENAME VALUE 'error' TO 'failed';
