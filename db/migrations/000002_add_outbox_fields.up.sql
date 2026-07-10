ALTER TABLE payment_events
ADD COLUMN publish_attempts INTEGER NOT NULL,
ADD COLUMN published_at TIMESTAMPTZ,
ADD COLUMN last_attempted_at TIMESTAMPTZ,
ADD COLUMN last_error TEXT;
