ALTER TABLE webhook_raw_log
  ADD COLUMN event_type TEXT,
  ADD COLUMN resp_body  TEXT;
