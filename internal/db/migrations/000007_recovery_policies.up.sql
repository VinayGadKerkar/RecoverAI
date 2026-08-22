CREATE TABLE recovery_policies (
    id                          UUID          PRIMARY KEY DEFAULT gen_random_uuid(),
    -- One policy row per merchant
    merchant_id                 UUID          NOT NULL UNIQUE REFERENCES merchants (id) ON DELETE CASCADE,

    -- Maximum payment amount (paise) eligible for automated retry
    max_retry_amount_paise      BIGINT        NOT NULL DEFAULT 1000000,   -- ₹10,000
    max_retries                 INT           NOT NULL DEFAULT 2,
    retry_cooldown_minutes      INT           NOT NULL DEFAULT 5,

    -- Payments above this amount require human approval before any action
    require_human_above         BIGINT        NOT NULL DEFAULT 5000000,   -- ₹50,000

    -- Actions the merchant has opted into; executor rejects anything not in this list
    allowed_actions             VARCHAR[]     NOT NULL DEFAULT ARRAY['retry','payment_link','notify'],

    -- RBI e-mandate compliance: minimum hours between retries for mandate payments
    mandate_min_retry_hours     INT           NOT NULL DEFAULT 24,

    -- High-value threshold: payments above this need human approval even if below require_human_above
    high_value_threshold_paise  BIGINT        NOT NULL DEFAULT 1500000,   -- ₹15,000

    -- ROI floor: skip recovery if projected ROI is below this value (paise or ratio)
    min_recovery_roi            DECIMAL(10,2) NOT NULL DEFAULT 0,

    -- Outage detection: number of failures within 5-minute window to flag an outage
    outage_detection_threshold  INT           NOT NULL DEFAULT 10,

    created_at                  TIMESTAMPTZ   NOT NULL DEFAULT NOW(),
    updated_at                  TIMESTAMPTZ   NOT NULL DEFAULT NOW()
);

CREATE INDEX idx_rp_merchant_id ON recovery_policies (merchant_id);
