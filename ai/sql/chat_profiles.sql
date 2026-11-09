CREATE TABLE IF NOT EXISTS ai_chat_profiles (
    conversation_id text NOT NULL,
    user_id text NOT NULL,
    team text NOT NULL,
    role text NOT NULL,
    PRIMARY KEY (conversation_id, user_id)
);
