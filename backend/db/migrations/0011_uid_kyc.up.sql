-- UPay 2026-05-04: optional uid on create + KYC/payment_status snapshot on query.
-- upay_uid NULL = order created without uid (KYC keyed per merchant_order_id).
ALTER TABLE orders
    ADD COLUMN upay_uid       TEXT,
    ADD COLUMN payment_status TEXT,
    ADD COLUMN kyc_status     TEXT,
    ADD COLUMN kyc_first_name TEXT,
    ADD COLUMN kyc_last_name  TEXT;
