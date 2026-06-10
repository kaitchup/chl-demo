ALTER TABLE orders
    DROP COLUMN IF EXISTS upay_req_body,
    DROP COLUMN IF EXISTS upay_resp_body;
