ALTER TABLE webhook_events DROP COLUMN IF EXISTS raw_log_id;
DROP TABLE IF EXISTS webhook_raw_log;
