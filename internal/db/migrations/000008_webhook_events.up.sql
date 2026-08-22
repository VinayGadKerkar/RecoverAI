CREATE TABLE webhook_events (
    id                  UUID         PRIMARY KEY DEFAULT gen_random_uuid(),
    -- Razorpay's own event ID — used as the idempotency key
    razorpay_event_id   VARCHAR(64)  UNIQUE NOT NULL,
    merchant_id         UUID         REFERENCES merchants (id) ON DELETE SET NULL,
    event_type          VARCHAR(50)  NOT NULL,
    payload             JSONB        NOT NULL,
    processed           BOOLEAN      NOT NULL DEFAULT FALSE,
    processed_at        TIMESTAMPTZ,
    created_at          TIMESTAMPTZ  NOT NULL DEFAULT NOW()
);

-- The UNIQUE constraint on razorpay_event_id already creates an index;
-- add a separate index on (processed, created_at) for the consumer poll query.
CREATE INDEX idx_we_unprocessed ON webhook_events (processed, created_at) WHERE processed = FALSE;
CREATE INDEX idx_we_merchant_id ON webhook_events (merchant_id);
