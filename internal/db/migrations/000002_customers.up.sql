CREATE TABLE customers (
    id                     UUID         PRIMARY KEY DEFAULT gen_random_uuid(),
    merchant_id            UUID         NOT NULL REFERENCES merchants (id) ON DELETE CASCADE,
    razorpay_customer_id   VARCHAR(64)  UNIQUE,
    email                  VARCHAR(255),
    phone                  VARCHAR(20),
    -- All monetary values stored in paise (1 INR = 100 paise)
    lifetime_value         BIGINT       NOT NULL DEFAULT 0,
    successful_payments    INT          NOT NULL DEFAULT 0,
    failed_payments        INT          NOT NULL DEFAULT 0,
    -- Risk score: 0.0000 (safe) – 1.0000 (high risk)
    risk_score             DECIMAL(5,4) NOT NULL DEFAULT 0.5,
    created_at             TIMESTAMPTZ  NOT NULL DEFAULT NOW(),
    updated_at             TIMESTAMPTZ  NOT NULL DEFAULT NOW(),
    deleted_at             TIMESTAMPTZ
);

CREATE INDEX idx_customers_merchant_id  ON customers (merchant_id);
CREATE INDEX idx_customers_deleted_at   ON customers (deleted_at);
