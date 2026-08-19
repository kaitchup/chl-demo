-- Rollback: convert webhook_raw_log.raw_body back to BYTEA
ALTER TABLE webhook_raw_log ALTER COLUMN raw_body SET DATA TYPE BYTEA USING convert_to(raw_body, 'utf8');
