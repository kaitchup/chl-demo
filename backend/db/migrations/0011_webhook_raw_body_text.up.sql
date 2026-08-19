-- Convert webhook_raw_log.raw_body from BYTEA to TEXT
ALTER TABLE webhook_raw_log ALTER COLUMN raw_body SET DATA TYPE TEXT USING convert_from(raw_body, 'utf8');
