ALTER TABLE payment_events
ADD COLUMN event_id UUID NOT NULL DEFAULT uuidv7(),
ADD CONSTRAINT payment_events_event_id_unique UNIQUE (event_id);
