CREATE TABLE sandbox_simulations (
    id                 BIGSERIAL PRIMARY KEY,
    merchant_id        TEXT        NOT NULL,
    payment_request_id TEXT        NOT NULL,
    scenario           TEXT        NOT NULL, -- NORMAL | BATCH | OVERPAID | RISK
    to_address         TEXT,                 -- RISK scenario: USDT deposit address
    amount             TEXT        NOT NULL,
    coin               TEXT,
    sim_events         JSONB       NOT NULL DEFAULT '[]',
    status             TEXT        NOT NULL DEFAULT 'SIMULATING',
    -- SIMULATING → AML_PENDING → AML_APPROVED → DONE | FAILED
    retry_count        INT         NOT NULL DEFAULT 0,
    max_retries        INT         NOT NULL DEFAULT 20,
    error_detail       TEXT,
    created_at         TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at         TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX idx_sandbox_simulations_merchant ON sandbox_simulations (merchant_id);
CREATE INDEX idx_sandbox_simulations_status   ON sandbox_simulations (status)
    WHERE status IN ('SIMULATING', 'AML_PENDING');

CREATE TABLE sandbox_aml_tickets (
    id            BIGSERIAL PRIMARY KEY,
    simulation_id BIGINT      NOT NULL REFERENCES sandbox_simulations (id),
    inspection_id TEXT        NOT NULL UNIQUE,
    ticket_uuid   TEXT,
    approved_at   TIMESTAMPTZ,
    created_at    TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX idx_sandbox_aml_tickets_sim ON sandbox_aml_tickets (simulation_id);
