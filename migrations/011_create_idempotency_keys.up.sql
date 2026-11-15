CREATE TABLE IF NOT EXISTS public.idempotency_keys (
    idempotency_key TEXT PRIMARY KEY CHECK (char_length(idempotency_key) <= 255),
    request_hash TEXT NOT NULL,
    order_id TEXT,
    response_status TEXT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
