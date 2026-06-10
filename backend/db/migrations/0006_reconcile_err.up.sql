ALTER TABLE orders
    ADD COLUMN reconcile_err_code SMALLINT,
    ADD COLUMN reconcile_err_body JSONB;
