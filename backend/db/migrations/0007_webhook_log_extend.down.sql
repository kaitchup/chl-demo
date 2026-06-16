ALTER TABLE webhook_raw_log
  DROP COLUMN IF EXISTS event_type,
  DROP COLUMN IF EXISTS resp_body;
