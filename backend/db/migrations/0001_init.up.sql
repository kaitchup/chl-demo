CREATE TABLE users (
    id            BIGSERIAL PRIMARY KEY,
    email         TEXT NOT NULL UNIQUE,
    password_hash TEXT NOT NULL,
    created_at    TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE membership_plans (
    code          TEXT PRIMARY KEY,
    name          TEXT NOT NULL,
    duration_days INT  NOT NULL,
    amount        NUMERIC(18,2) NOT NULL,
    currency      TEXT NOT NULL DEFAULT 'USD',
    active        BOOLEAN NOT NULL DEFAULT true
);

CREATE TABLE subscriptions (
    id         BIGSERIAL PRIMARY KEY,
    user_id    BIGINT NOT NULL UNIQUE REFERENCES users(id),
    tier       TEXT NOT NULL DEFAULT 'super',
    expires_at TIMESTAMPTZ,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE orders (
    id                      BIGSERIAL PRIMARY KEY,
    merchant_order_id       TEXT NOT NULL UNIQUE,
    upay_payment_id         TEXT UNIQUE,
    user_id                 BIGINT NOT NULL REFERENCES users(id),
    plan_code               TEXT NOT NULL REFERENCES membership_plans(code),
    amount                  NUMERIC(18,2) NOT NULL,
    currency                TEXT NOT NULL DEFAULT 'USD',
    status                  TEXT NOT NULL DEFAULT 'PENDING',
    received_amount         NUMERIC(18,2) NOT NULL DEFAULT 0,
    failure_code            TEXT,
    checkout_url            TEXT,
    idempotency_key         TEXT,
    applied_to_subscription BOOLEAN NOT NULL DEFAULT false,
    created_at              TIMESTAMPTZ NOT NULL DEFAULT now(),
    paid_at                 TIMESTAMPTZ,
    expires_at              TIMESTAMPTZ
);
CREATE INDEX idx_orders_user   ON orders(user_id, created_at DESC);
CREATE INDEX idx_orders_status ON orders(status) WHERE status = 'PENDING';

CREATE TABLE webhook_events (
    id              BIGSERIAL PRIMARY KEY,
    upay_event_id   TEXT NOT NULL UNIQUE,
    upay_payment_id TEXT,
    event_type      TEXT,
    payload         JSONB,
    signature_valid BOOLEAN,
    processed       BOOLEAN NOT NULL DEFAULT false,
    received_at     TIMESTAMPTZ NOT NULL DEFAULT now()
);

INSERT INTO membership_plans(code, name, duration_days, amount, currency) VALUES
  ('monthly',   'Super 月付', 30,  20.00,  'USD'),
  ('quarterly', 'Super 季付', 90,  54.00,  'USD'),
  ('yearly',    'Super 年付', 365, 192.00, 'USD');
