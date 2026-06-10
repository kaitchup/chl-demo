CREATE TABLE webhook_raw_log (
    id          BIGSERIAL    PRIMARY KEY,
    req_headers JSONB        NOT NULL DEFAULT '{}',
    raw_body    BYTEA        NOT NULL,
    received_at TIMESTAMPTZ  NOT NULL DEFAULT now()
);

ALTER TABLE webhook_events ADD COLUMN raw_log_id BIGINT REFERENCES webhook_raw_log(id);
