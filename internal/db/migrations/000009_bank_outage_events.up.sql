CREATE TABLE bank_outage_events (
    id                UUID         PRIMARY KEY DEFAULT gen_random_uuid(),

    -- UPI error code that triggered the outage (e.g. 'RB', 'U30')
    upi_error_code    VARCHAR(10)  NOT NULL,

    -- NULL means system-wide outage (not scoped to one merchant)
    merchant_id       UUID         REFERENCES merchants (id) ON DELETE SET NULL,

    detected_at       TIMESTAMPTZ  NOT NULL DEFAULT NOW(),

    -- Number of failures counted in the detection window
    failure_count     INT          NOT NULL,
    -- Detection window used (matches outage_detection_threshold policy)
    window_minutes    INT          NOT NULL DEFAULT 5,

    resolved_at       TIMESTAMPTZ,

    -- UUIDs of recovery_cases that were batched/paused during this outage
    affected_case_ids UUID[]       NOT NULL DEFAULT '{}',

    created_at        TIMESTAMPTZ  NOT NULL DEFAULT NOW()
);

CREATE INDEX idx_boe_error_code_detected ON bank_outage_events (upi_error_code, detected_at DESC);
CREATE INDEX idx_boe_merchant_id         ON bank_outage_events (merchant_id);
-- Partial index for fast "active outages" lookup (resolved_at IS NULL)
CREATE INDEX idx_boe_active              ON bank_outage_events (upi_error_code, detected_at) WHERE resolved_at IS NULL;
