CREATE TABLE audit_logs (
    id            UUID         PRIMARY KEY DEFAULT gen_random_uuid(),

    -- entity_type: payment | recovery_case | recovery_action | merchant | customer
    entity_type   VARCHAR(30)  NOT NULL,
    entity_id     UUID         NOT NULL,

    -- actor: system | risk_engine | validator | ai_agent | policy_engine
    --        execution_worker | human | customer_self
    actor         VARCHAR(50)  NOT NULL DEFAULT 'system',
    action        VARCHAR(50)  NOT NULL,

    before_state  JSONB,
    after_state   JSONB,
    metadata      JSONB        NOT NULL DEFAULT '{}',

    created_at    TIMESTAMPTZ  NOT NULL DEFAULT NOW()
);

-- Partial index for fast entity-scoped lookups (most common query pattern)
CREATE INDEX idx_al_entity        ON audit_logs (entity_type, entity_id);
CREATE INDEX idx_al_created_at    ON audit_logs (created_at DESC);
