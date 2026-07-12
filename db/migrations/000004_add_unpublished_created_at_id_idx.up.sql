CREATE INDEX payment_events_unpublished_order_idx
ON payment_events (created_at, id)
WHERE published_at IS NULL;
