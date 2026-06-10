ALTER TABLE orders
    DROP COLUMN IF EXISTS reconcile_err_code,
    DROP COLUMN IF EXISTS reconcile_err_body;
