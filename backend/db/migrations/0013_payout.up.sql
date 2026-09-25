-- Payout (汇款) module. See docs/TECH-DESIGN-汇款功能.md §4.

-- Whitelist + trade password
ALTER TABLE users
    ADD COLUMN payout_enabled              BOOLEAN     NOT NULL DEFAULT false,
    ADD COLUMN trade_password_hash         TEXT,
    ADD COLUMN trade_password_fail_count   INT         NOT NULL DEFAULT 0,
    ADD COLUMN trade_password_locked_until TIMESTAMPTZ;

-- One user = one UPay payer
CREATE TABLE payout_payers (
    user_id    BIGINT      PRIMARY KEY REFERENCES users(id),
    payer_no   TEXT        NOT NULL UNIQUE,
    name       TEXT        NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- A UI "recipient" = one UPay beneficiary + one bank account.
-- bank_account_no NULL means step 2 failed and can be retried.
CREATE TABLE payout_recipients (
    id              BIGSERIAL   PRIMARY KEY,
    user_id         BIGINT      NOT NULL REFERENCES users(id),
    given_name      TEXT        NOT NULL,
    family_name     TEXT        NOT NULL,
    beneficiary_no  TEXT        UNIQUE,
    bank_account_no TEXT        UNIQUE,
    bank_name       TEXT        NOT NULL,
    swift_code      TEXT        NOT NULL,
    account_last4   TEXT        NOT NULL,
    currency        TEXT        NOT NULL,
    bank_country    TEXT        NOT NULL,
    last_error      TEXT,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX idx_payout_recipients_user ON payout_recipients(user_id, created_at DESC);

CREATE TABLE payout_orders (
    id             BIGSERIAL   PRIMARY KEY,
    user_id        BIGINT      NOT NULL REFERENCES users(id),
    recipient_id   BIGINT      NOT NULL REFERENCES payout_recipients(id),
    third_order_no TEXT        NOT NULL UNIQUE,
    order_no       TEXT        UNIQUE,
    amount         TEXT        NOT NULL,
    currency       TEXT        NOT NULL,
    debit_coin     TEXT        NOT NULL,
    usage          TEXT        NOT NULL,
    note           TEXT        NOT NULL,
    status         INT         NOT NULL DEFAULT 0,
    quote          JSONB,
    last_info      JSONB,
    last_error     TEXT,
    confirmed_at   TIMESTAMPTZ,
    synced_at      TIMESTAMPTZ,
    created_at     TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX idx_payout_orders_user ON payout_orders(user_id, id DESC);
CREATE INDEX idx_payout_orders_active ON payout_orders(status) WHERE status IN (3, 5, 6, 10);

CREATE TABLE payout_order_events (
    id          BIGSERIAL   PRIMARY KEY,
    order_id    BIGINT      NOT NULL REFERENCES payout_orders(id),
    status      INT         NOT NULL,
    observed_at TIMESTAMPTZ NOT NULL,
    source      TEXT        NOT NULL, -- api | poll | webhook
    UNIQUE (order_id, status)
);

-- Webhook dedupe on the outer orderNo (= notification id, not the payout order).
CREATE TABLE payout_webhook_events (
    notify_no    TEXT        PRIMARY KEY,
    raw_log_id   BIGINT      REFERENCES webhook_raw_log(id),
    order_no     TEXT,
    status       INT,
    pushed_at    TIMESTAMPTZ,
    processed_at TIMESTAMPTZ,
    received_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);
