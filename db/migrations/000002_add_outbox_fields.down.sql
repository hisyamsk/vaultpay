ALTER TABLE payment_events
DROP COLUMN publish_attempts,
DROP COLUMN published_at,
DROP COLUMN last_attempted_at,
DROP COLUMN last_error;
