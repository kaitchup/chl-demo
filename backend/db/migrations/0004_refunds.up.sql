CREATE TABLE refunds (
    id                 BIGSERIAL PRIMARY KEY,
    merchant_refund_id TEXT          NOT NULL UNIQUE,
    upay_refund_id     TEXT          UNIQUE,
    order_id           BIGINT        NOT NULL REFERENCES orders(id),
    user_id            BIGINT        NOT NULL REFERENCES users(id),
    amount             NUMERIC(18,2) NOT NULL,
    coin               TEXT          NOT NULL,
    chain              TEXT          NOT NULL,
    to_address         TEXT          NOT NULL,
    status             TEXT          NOT NULL DEFAULT 'PENDING',
    reason             TEXT,
    created_at         TIMESTAMPTZ   NOT NULL DEFAULT now()
);
CREATE INDEX idx_refunds_order ON refunds(order_id);
CREATE INDEX idx_refunds_user  ON refunds(user_id, created_at DESC);
