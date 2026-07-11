ALTER TABLE payment_events
DROP CONSTRAINT payment_events_event_id_unique,
DROP COLUMN event_id;
