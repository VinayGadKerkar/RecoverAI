CREATE TABLE payments (
    id                   UUID         PRIMARY KEY DEFAULT gen_random_uuid(),
    merchant_id          UUID         NOT NULL REFERENCES merchants  (id) ON DELETE CASCADE,
    customer_id          UUID                  REFERENCES customers  (id) ON DELETE SET NULL,
    razorpay_payment_id  VARCHAR(64)  UNIQUE NOT NULL,
    razorpay_order_id    VARCHAR(64),
    -- amount in paise; BIGINT handles values up to ~₹92 trillion safely
    amount               BIGINT       NOT NULL,
    currency             CHAR(3)      NOT NULL DEFAULT 'INR',
    method               VARCHAR(20),
    -- status: created | authorized | captured | failed | refunded
    status               VARCHAR(20)  NOT NULL,
    upi_error_code       VARCHAR(10),
    failure_reason       TEXT,
    -- RBI e-mandate / subscription payment flag
    is_mandate_payment   BOOLEAN      NOT NULL DEFAULT FALSE,
    mandate_id           VARCHAR(64),
    metadata             JSONB        NOT NULL DEFAULT '{}',
    created_at           TIMESTAMPTZ  NOT NULL DEFAULT NOW(),
    updated_at           TIMESTAMPTZ  NOT NULL DEFAULT NOW()
);

CREATE INDEX idx_payments_merchant_status  ON payments (merchant_id, status);
CREATE INDEX idx_payments_customer_id      ON payments (customer_id);
CREATE INDEX idx_payments_razorpay_order   ON payments (razorpay_order_id);
CREATE INDEX idx_payments_upi_error_code   ON payments (upi_error_code);
