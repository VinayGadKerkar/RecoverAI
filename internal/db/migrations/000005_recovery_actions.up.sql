CREATE TABLE recovery_actions (
    id                     UUID         PRIMARY KEY DEFAULT gen_random_uuid(),
    case_id                UUID         NOT NULL REFERENCES recovery_cases (id) ON DELETE CASCADE,

    -- action_type: retry | payment_link | notify_customer | escalate | abort | wait
    action_type            VARCHAR(30)  NOT NULL,
    -- status: pending | executing | succeeded | failed | skipped
    status                 VARCHAR(20)  NOT NULL DEFAULT 'pending',

    -- AI confidence score for this action (0.0000 – 1.0000)
    ai_confidence          DECIMAL(5,4),

    -- Policy Engine decision
    policy_approved        BOOLEAN      NOT NULL DEFAULT FALSE,
    -- Name of the policy rule that blocked or approved this action
    policy_rule_triggered  VARCHAR(50),
    policy_reason          TEXT,

    -- Request payload sent to the execution layer
    payload                JSONB        NOT NULL DEFAULT '{}',
    -- Response / result from the execution layer
    result                 JSONB        NOT NULL DEFAULT '{}',

    -- actor: system | risk_engine | validator | ai_agent | policy_engine
    --        execution_worker | human | customer_self
    executed_by            VARCHAR(50)  NOT NULL DEFAULT 'system',
    executed_at            TIMESTAMPTZ,

    created_at             TIMESTAMPTZ  NOT NULL DEFAULT NOW()
);

CREATE INDEX idx_ra_case_id    ON recovery_actions (case_id);
CREATE INDEX idx_ra_status     ON recovery_actions (status);
CREATE INDEX idx_ra_created_at ON recovery_actions (created_at DESC);
