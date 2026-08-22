CREATE TABLE recovery_cases (
    id                    UUID          PRIMARY KEY DEFAULT gen_random_uuid(),
    merchant_id           UUID          NOT NULL REFERENCES merchants (id) ON DELETE CASCADE,
    payment_id            UUID          NOT NULL REFERENCES payments  (id) ON DELETE CASCADE,
    customer_id           UUID                   REFERENCES customers (id) ON DELETE SET NULL,

    -- Status lifecycle:
    -- open → in_progress → recovered | partially_recovered | failed | stopped
    -- Special statuses: customer_self_recovered, outage_batched,
    --                   not_worth_recovering, pending_human_approval
    status                VARCHAR(30)   NOT NULL DEFAULT 'open',

    -- All monetary values in paise
    revenue_at_risk       BIGINT        NOT NULL,
    recovery_probability  DECIMAL(5,4),
    -- ROI = (revenue_at_risk × recovery_probability) - estimated_cost
    recovery_roi          DECIMAL(10,2),

    priority              VARCHAR(10)   NOT NULL DEFAULT 'medium', -- low | medium | high | critical
    failure_type          VARCHAR(30),
    upi_error_code        VARCHAR(10),
    -- TD = Technical Decline, BD = Business Decline
    upi_error_category    VARCHAR(20),

    -- Outage detection flag — set by the Risk Engine via Redis counter
    bank_outage_detected  BOOLEAN       NOT NULL DEFAULT FALSE,

    -- RBI e-mandate compliance
    is_mandate_payment    BOOLEAN       NOT NULL DEFAULT FALSE,
    -- Earliest timestamp this payment may be retried under RBI 24h mandate rule
    rbi_minimum_retry_at  TIMESTAMPTZ,

    -- Pre-Recovery Validator gate: populated when AI is NOT called
    validator_skip_reason TEXT,

    -- Structured AI outputs (Stage 4)
    ai_diagnosis          JSONB,
    ai_strategy           JSONB,

    -- Recovery tracking
    amount_recovered      BIGINT        NOT NULL DEFAULT 0,
    partial_recovery      BOOLEAN       NOT NULL DEFAULT FALSE,
    retry_count           INT           NOT NULL DEFAULT 0,
    max_retries           INT           NOT NULL DEFAULT 2,
    cooldown_until        TIMESTAMPTZ,
    resolved_at           TIMESTAMPTZ,

    created_at            TIMESTAMPTZ   NOT NULL DEFAULT NOW(),
    updated_at            TIMESTAMPTZ   NOT NULL DEFAULT NOW()
);

CREATE INDEX idx_rc_merchant_status     ON recovery_cases (merchant_id, status);
CREATE INDEX idx_rc_priority_created    ON recovery_cases (priority, created_at);
CREATE INDEX idx_rc_bank_outage         ON recovery_cases (bank_outage_detected) WHERE bank_outage_detected = TRUE;
CREATE INDEX idx_rc_status_created      ON recovery_cases (status, created_at);
CREATE INDEX idx_rc_payment_id          ON recovery_cases (payment_id);
