CREATE TABLE IF NOT EXISTS public.failed_pubsub_messages (
    subscription TEXT NOT NULL,
    message_id TEXT NOT NULL,
    data BYTEA NOT NULL,
    attributes JSONB NOT NULL DEFAULT '{}'::JSONB,
    attempts INTEGER NOT NULL CHECK (attempts > 0),
    last_error TEXT NOT NULL,
    failed_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (subscription, message_id)
);
