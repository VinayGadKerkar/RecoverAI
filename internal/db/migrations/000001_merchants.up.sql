CREATE EXTENSION IF NOT EXISTS "pgcrypto";

CREATE TABLE merchants (
    id                UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
    razorpay_key_id   VARCHAR(64) UNIQUE NOT NULL,
    name              VARCHAR(255) NOT NULL,
    webhook_secret    VARCHAR(255) NOT NULL,
    settings          JSONB       NOT NULL DEFAULT '{}',
    created_at        TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at        TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    deleted_at        TIMESTAMPTZ
);

CREATE INDEX idx_merchants_deleted_at ON merchants (deleted_at);
