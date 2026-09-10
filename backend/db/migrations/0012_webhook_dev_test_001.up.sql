CREATE TABLE webhook_dev_test_001 (
    id          BIGSERIAL   PRIMARY KEY,
    event_id    TEXT        NOT NULL,
    req_headers TEXT        NOT NULL DEFAULT '',
    raw_body    TEXT        NOT NULL,
    event_type  TEXT,
    received_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT uq_webhook_dev_test_001_event_id UNIQUE (event_id)
);
