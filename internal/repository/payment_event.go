package repository

import (
	"context"
	"time"

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

// ClaimUnpublished claims at most paymentEventClaimBatchSize events ordered by
// created_at and then id. An event is eligible when it is unpublished and has
// either never been attempted or was last attempted before leaseExpiredBefore.
//
// Claiming increments publish_attempts and sets last_attempted_at for every
// returned event in the same short transaction. Concurrent callers must receive
// disjoint fresh claims by selecting with FOR UPDATE SKIP LOCKED. A canceled
// context returns its error without changing any event. This method commits the
// database claim before returning and never calls RabbitMQ.
func (r *PaymentEventRepository) ClaimUnpublished(ctx context.Context, leaseExpiredBefore time.Time) ([]domain.PaymentEvent, error) {
	tx, err := r.db.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx)

	rows, err := tx.Query(ctx, `
		SELECT id, event_id, payment_id, event_type, payload, created_at, publish_attempts,
			published_at, last_attempted_at, last_error
		FROM payment_events
		WHERE published_at is NULL AND (last_attempted_at IS NULL OR last_attempted_at < $1)
		ORDER BY created_at ASC, id ASC
		LIMIT 10
		FOR UPDATE SKIP LOCKED
	`, leaseExpiredBefore)

	if err != nil {
		return nil, err
	}
	defer rows.Close()

	events, err := pgx.CollectRows(rows, pgx.RowToStructByName[domain.PaymentEvent])
	if err != nil {
		return nil, err
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
			return nil, err
		}
	}

	err = tx.Commit(ctx)
	if err != nil {
		return nil, err
	}
	return events, nil
}
