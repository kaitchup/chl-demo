ALTER TABLE orders
    DROP COLUMN IF EXISTS upay_uid,
    DROP COLUMN IF EXISTS payment_status,
    DROP COLUMN IF EXISTS kyc_status,
    DROP COLUMN IF EXISTS kyc_first_name,
    DROP COLUMN IF EXISTS kyc_last_name;
