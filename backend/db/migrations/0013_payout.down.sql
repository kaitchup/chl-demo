DROP TABLE IF EXISTS payout_webhook_events;
DROP TABLE IF EXISTS payout_order_events;
DROP TABLE IF EXISTS payout_orders;
DROP TABLE IF EXISTS payout_recipients;
DROP TABLE IF EXISTS payout_payers;
ALTER TABLE users
    DROP COLUMN IF EXISTS trade_password_locked_until,
    DROP COLUMN IF EXISTS trade_password_fail_count,
    DROP COLUMN IF EXISTS trade_password_hash,
    DROP COLUMN IF EXISTS payout_enabled;
