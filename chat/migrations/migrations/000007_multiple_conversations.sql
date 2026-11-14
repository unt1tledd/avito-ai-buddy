-- +goose Up
ALTER TABLE chat.conversation DROP CONSTRAINT conversation_user_id_key;
ALTER TABLE chat.conversation ADD COLUMN title text NOT NULL DEFAULT 'Новый чат';
ALTER TABLE chat.conversation ADD COLUMN deleted_at timestamptz;
ALTER TABLE chat.conversation ADD COLUMN is_default boolean NOT NULL DEFAULT true;
CREATE UNIQUE INDEX conversation_default_user ON chat.conversation(user_id) WHERE is_default AND deleted_at IS NULL;
-- +goose Down
-- The unique constraint deliberately rejects rollback if multiple histories exist.
-- Goose rolls back this transaction without deleting any user data.
DROP INDEX chat.conversation_default_user;
ALTER TABLE chat.conversation DROP COLUMN title, DROP COLUMN deleted_at, DROP COLUMN is_default;
ALTER TABLE chat.conversation ADD CONSTRAINT conversation_user_id_key UNIQUE(user_id);
