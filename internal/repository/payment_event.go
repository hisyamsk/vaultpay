package repository

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/hisyamsk/vaultpay/internal/domain"
	"github.com/jackc/pgx/v5"
)

const paymentEventClaimBatchSize = 10

type PaymentEventRepository struct {
	db dbtx
}

func NewPaymentEventRepository(db dbtx) *PaymentEventRepository {
	return &PaymentEventRepository{db: db}
}

func (r *PaymentEventRepository) ClaimUnpublished(ctx context.Context, leaseExpiredBefore time.Time) ([]domain.PaymentEvent, error) {
	tx, err := r.db.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("claim unpublished payment events: begin transaction: %w", err)
	}
	defer tx.Rollback(ctx)

	rows, err := tx.Query(ctx, `
		SELECT id, event_id, payment_id, event_type, payload, created_at, publish_attempts,
			published_at, last_attempted_at, last_error
		FROM payment_events
		WHERE published_at IS NULL AND (last_attempted_at IS NULL OR last_attempted_at < $1)
		ORDER BY created_at ASC, id ASC
		LIMIT 10
		FOR UPDATE SKIP LOCKED
	`, leaseExpiredBefore)

	if err != nil {
		return nil, fmt.Errorf("claim unpublished payment events: query events: %w", err)
	}
	defer rows.Close()

	events, err := pgx.CollectRows(rows, pgx.RowToStructByName[domain.PaymentEvent])
	if err != nil {
		return nil, fmt.Errorf("claim unpublished payment events: collect events: %w", err)
	}

	now := time.Now().UTC()
	ids := make([]int64, len(events))
	for i := range events {
		ids[i] = events[i].ID

		events[i].PublishAttempts += 1
		events[i].LastAttemptedAt = &now
	}

	if len(ids) > 0 {
		_, err := tx.Exec(ctx, `
			UPDATE payment_events
			SET publish_attempts = publish_attempts + 1,
				last_attempted_at = $1
			WHERE id = ANY($2)
		`, now, ids)

		if err != nil {
			return nil, fmt.Errorf("claim unpublished payment events: update claim metadata: %w", err)
		}
	}

	err = tx.Commit(ctx)
	if err != nil {
		return nil, fmt.Errorf("claim unpublished payment events: commit transaction: %w", err)
	}
	return events, nil
}

func (r *PaymentEventRepository) MarkPublished(ctx context.Context, eventID uuid.UUID, publishedAt time.Time) error {
	_, err := r.db.Exec(ctx, `
		UPDATE payment_events
		SET published_at = $1, last_error = NULL
		WHERE event_id = $2 AND published_at IS NULL
	`, publishedAt, eventID)

	if err != nil {
		return fmt.Errorf("mark payment event published: update event: %w", err)
	}

	return nil
}

func (r *PaymentEventRepository) RecordPublishFailure(ctx context.Context, eventID uuid.UUID, lastError string) error {
	_, err := r.db.Exec(ctx, `
		UPDATE payment_events
		SET last_error = $1
		WHERE event_id = $2 AND published_at IS NULL
	`, lastError, eventID)

	if err != nil {
		return fmt.Errorf("record payment event publish failure: %w", err)
	}
	return nil
}
