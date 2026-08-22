-- Migration 001: Initial schema for RecoverAI
-- PostgreSQL 16

BEGIN;

-- ─── Extensions ──────────────────────────────────────────────────────────────
CREATE EXTENSION IF NOT EXISTS "pgcrypto";
CREATE EXTENSION IF NOT EXISTS "pg_trgm";

-- ─── Merchants ────────────────────────────────────────────────────────────────
CREATE TABLE IF NOT EXISTS merchants (
    id            TEXT PRIMARY KEY,
    name          TEXT NOT NULL,
    email         TEXT NOT NULL UNIQUE,
    razorpay_id   TEXT NOT NULL UNIQUE,
    is_active     BOOLEAN NOT NULL DEFAULT true,
    created_at    TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at    TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- ─── Payments ────────────────────────────────────────────────────────────────
CREATE TABLE IF NOT EXISTS payments (
    id              TEXT PRIMARY KEY,
    merchant_id     TEXT NOT NULL REFERENCES merchants(id),
    order_id        TEXT,
    amount          BIGINT NOT NULL,          -- in paise
    currency        TEXT NOT NULL DEFAULT 'INR',
    status          TEXT NOT NULL,            -- created | authorized | captured | failed | refunded
    method          TEXT,                     -- upi | card | netbanking | wallet
    error_code      TEXT,
    error_description TEXT,
    error_source    TEXT,
    error_step      TEXT,
    error_reason    TEXT,
    bank            TEXT,
    vpa             TEXT,
    card_id         TEXT,
    email           TEXT,
    contact         TEXT,
    description     TEXT,
    razorpay_created_at TIMESTAMPTZ,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_payments_merchant_id ON payments(merchant_id);
CREATE INDEX IF NOT EXISTS idx_payments_status ON payments(status);
CREATE INDEX IF NOT EXISTS idx_payments_error_code ON payments(error_code);
CREATE INDEX IF NOT EXISTS idx_payments_created_at ON payments(created_at DESC);

-- ─── Webhook Events (idempotency store) ──────────────────────────────────────
CREATE TABLE IF NOT EXISTS webhook_events (
    id            TEXT PRIMARY KEY,           -- Razorpay event ID
    payment_id    TEXT,
    merchant_id   TEXT,
    event_type    TEXT NOT NULL,
    raw_payload   JSONB NOT NULL,
    processed     BOOLEAN NOT NULL DEFAULT false,
    received_at   TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    processed_at  TIMESTAMPTZ
);

CREATE INDEX IF NOT EXISTS idx_webhook_events_payment_id ON webhook_events(payment_id);
CREATE INDEX IF NOT EXISTS idx_webhook_events_processed ON webhook_events(processed);

-- ─── Risk Scores ─────────────────────────────────────────────────────────────
CREATE TABLE IF NOT EXISTS risk_scores (
    id               BIGSERIAL PRIMARY KEY,
    payment_id       TEXT NOT NULL REFERENCES payments(id),
    score            NUMERIC(5,4) NOT NULL,   -- 0.0000 – 1.0000
    level            TEXT NOT NULL,           -- low | medium | high | critical
    recovery_chance  NUMERIC(5,4) NOT NULL,
    upi_error_code   TEXT,
    bank_outage      BOOLEAN NOT NULL DEFAULT false,
    outage_bank      TEXT,
    factors          JSONB NOT NULL DEFAULT '[]',
    scored_at        TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_risk_scores_payment_id ON risk_scores(payment_id);

-- ─── Recovery Attempts ────────────────────────────────────────────────────────
CREATE TABLE IF NOT EXISTS recovery_attempts (
    id                BIGSERIAL PRIMARY KEY,
    payment_id        TEXT NOT NULL REFERENCES payments(id),
    merchant_id       TEXT NOT NULL REFERENCES merchants(id),
    attempt_number    INT NOT NULL DEFAULT 1,
    status            TEXT NOT NULL DEFAULT 'pending',
    action            TEXT,                   -- retry | payment_link | notify_customer | escalate | abort
    ai_command        JSONB,                  -- structured JSON from AI service
    policy_decision   TEXT,                  -- approved | overridden | rejected
    policy_reason     TEXT,
    executed_at       TIMESTAMPTZ,
    result            JSONB,
    created_at        TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at        TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE(payment_id, attempt_number)
);

CREATE INDEX IF NOT EXISTS idx_recovery_attempts_payment_id ON recovery_attempts(payment_id);
CREATE INDEX IF NOT EXISTS idx_recovery_attempts_status ON recovery_attempts(status);
CREATE INDEX IF NOT EXISTS idx_recovery_attempts_merchant_id ON recovery_attempts(merchant_id);

-- ─── Validation Results ───────────────────────────────────────────────────────
CREATE TABLE IF NOT EXISTS validation_results (
    id           BIGSERIAL PRIMARY KEY,
    payment_id   TEXT NOT NULL REFERENCES payments(id),
    passed       BOOLEAN NOT NULL,
    checks       JSONB NOT NULL DEFAULT '[]',
    blocked_by   TEXT,
    checked_at   TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_validation_results_payment_id ON validation_results(payment_id);

-- ─── Audit Log ────────────────────────────────────────────────────────────────
CREATE TABLE IF NOT EXISTS audit_log (
    id           BIGSERIAL PRIMARY KEY,
    payment_id   TEXT,
    merchant_id  TEXT,
    stage        TEXT NOT NULL,
    action       TEXT NOT NULL,
    actor        TEXT NOT NULL DEFAULT 'system',
    decision     TEXT,
    metadata     JSONB,
    created_at   TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_audit_log_payment_id ON audit_log(payment_id);
CREATE INDEX IF NOT EXISTS idx_audit_log_merchant_id ON audit_log(merchant_id);
CREATE INDEX IF NOT EXISTS idx_audit_log_created_at ON audit_log(created_at DESC);

COMMIT;
