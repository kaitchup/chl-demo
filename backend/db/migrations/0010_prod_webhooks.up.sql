CREATE TABLE webhook_test_merchant (
    id          BIGSERIAL   PRIMARY KEY,
    event_id    TEXT        NOT NULL,
    req_headers TEXT        NOT NULL DEFAULT '',
    raw_body    TEXT        NOT NULL,
    event_type  TEXT,
    received_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT uq_webhook_test_merchant_event_id UNIQUE (event_id)
);

CREATE TABLE webhook_prod_dummy (
    id          BIGSERIAL   PRIMARY KEY,
    event_id    TEXT        NOT NULL,
    req_headers TEXT        NOT NULL DEFAULT '',
    raw_body    TEXT        NOT NULL,
    event_type  TEXT,
    received_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT uq_webhook_prod_dummy_event_id UNIQUE (event_id)
);
