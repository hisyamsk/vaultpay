package repository

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/hisyamsk/vaultpay/internal/domain"
	"github.com/stretchr/testify/require"
)

func rejectedEventCount(t *testing.T, ctx context.Context, tx dbtx, paymentID uuid.UUID) int {
	t.Helper()

	var count int
	err := tx.QueryRow(ctx, `
		SELECT COUNT(*)
		FROM payment_events
		WHERE payment_id = $1 AND event_type = $2
	`, paymentID, domain.PaymentEventTypeRejected).Scan(&count)
	require.NoError(t, err)
	return count
}

func TestPaymentRepositoryRejectPendingPaymentRejectsAndWritesOneOutboxEvent(t *testing.T) {
	repo, ctx := newTestRepo(t)
	senderID := createAccount(t, ctx, repo.db, 2000)
	receiverID := createAccount(t, ctx, repo.db, 1000)
	payment := createPayment(t, ctx, repo, 500, senderID, receiverID, "idem-reject-success")

	rejected, err := repo.RejectPendingPayment(ctx, payment.ID)

	require.NoError(t, err)
	require.NotNil(t, rejected)
	require.Equal(t, payment.ID, rejected.ID)
	require.Equal(t, domain.PaymentStatusRejected, rejected.Status)
	require.Equal(t, int64(2000), accountBalance(t, ctx, repo.db, senderID))
	require.Equal(t, int64(1000), accountBalance(t, ctx, repo.db, receiverID))
	require.Equal(t, 0, ledgerEntryCount(t, ctx, repo.db, payment.ID, senderID, domain.LedgerEntryTypeDebit))
	require.Equal(t, 1, rejectedEventCount(t, ctx, repo.db, payment.ID))
	require.Equal(t, 2, paymentEventCount(t, ctx, repo.db, payment.ID))

	stored, err := repo.FindByID(ctx, payment.ID)
	require.NoError(t, err)
	require.Equal(t, domain.PaymentStatusRejected, stored.Status)

	var (
		eventID         uuid.UUID
		eventPaymentID  uuid.UUID
		eventType       domain.PaymentEventType
		payload         []byte
		createdAt       time.Time
		publishAttempts int
		publishedAt     *time.Time
		lastAttemptedAt *time.Time
		lastError       *string
	)
	err = repo.db.QueryRow(ctx, `
		SELECT event_id, payment_id, event_type, payload, created_at,
		       publish_attempts, published_at, last_attempted_at, last_error
		FROM payment_events
		WHERE payment_id = $1 AND event_type = $2
	`, payment.ID, domain.PaymentEventTypeRejected).Scan(
		&eventID,
		&eventPaymentID,
		&eventType,
		&payload,
		&createdAt,
		&publishAttempts,
		&publishedAt,
		&lastAttemptedAt,
		&lastError,
	)
	require.NoError(t, err)
	require.NotEqual(t, uuid.Nil, eventID)
	require.Equal(t, payment.ID, eventPaymentID)
	require.Equal(t, domain.PaymentEventTypeRejected, eventType)
	require.Zero(t, publishAttempts)
	require.Nil(t, publishedAt)
	require.Nil(t, lastAttemptedAt)
	require.Nil(t, lastError)

	var eventPayload domain.PaymentEventPayload
	require.NoError(t, json.Unmarshal(payload, &eventPayload))
	require.Equal(t, eventID, eventPayload.EventID)
	require.Equal(t, eventPaymentID, eventPayload.PaymentID)
	require.Equal(t, eventType, eventPayload.EventType)
	require.Equal(t, 1, eventPayload.Attempt)
	require.True(t, eventPayload.OccurredAt.Equal(createdAt))
}

func TestPaymentRepositoryRejectPendingPaymentRepeatedCallIsNoOp(t *testing.T) {
	repo, ctx := newTestRepo(t)
	senderID := createAccount(t, ctx, repo.db, 2000)
	receiverID := createAccount(t, ctx, repo.db, 1000)
	payment := createPayment(t, ctx, repo, 500, senderID, receiverID, "idem-reject-repeated")

	first, err := repo.RejectPendingPayment(ctx, payment.ID)
	require.NoError(t, err)
	require.NotNil(t, first)

	var firstEventID uuid.UUID
	require.NoError(t, repo.db.QueryRow(ctx, `
		SELECT event_id
		FROM payment_events
		WHERE payment_id = $1 AND event_type = $2
	`, payment.ID, domain.PaymentEventTypeRejected).Scan(&firstEventID))

	second, err := repo.RejectPendingPayment(ctx, payment.ID)

	require.NoError(t, err)
	require.NotNil(t, second)
	require.Equal(t, domain.PaymentStatusRejected, second.Status)
	require.True(t, first.UpdatedAt.Equal(second.UpdatedAt))
	require.Equal(t, 1, rejectedEventCount(t, ctx, repo.db, payment.ID))
	require.Equal(t, int64(2000), accountBalance(t, ctx, repo.db, senderID))
	require.Equal(t, int64(1000), accountBalance(t, ctx, repo.db, receiverID))
	require.Equal(t, 0, ledgerEntryCount(t, ctx, repo.db, payment.ID, senderID, domain.LedgerEntryTypeDebit))

	var secondEventID uuid.UUID
	require.NoError(t, repo.db.QueryRow(ctx, `
		SELECT event_id
		FROM payment_events
		WHERE payment_id = $1 AND event_type = $2
	`, payment.ID, domain.PaymentEventTypeRejected).Scan(&secondEventID))
	require.Equal(t, firstEventID, secondEventID)
}

func TestPaymentRepositoryRejectPendingPaymentNonPendingIsNoOp(t *testing.T) {
	statuses := []domain.PaymentStatus{
		domain.PaymentStatusProcessing,
		domain.PaymentStatusCompleted,
		domain.PaymentStatusFailed,
		domain.PaymentStatusRejected,
	}

	for _, status := range statuses {
		t.Run(string(status), func(t *testing.T) {
			repo, ctx := newTestRepo(t)
			senderID := createAccount(t, ctx, repo.db, 2000)
			receiverID := createAccount(t, ctx, repo.db, 1000)
			payment := createPayment(t, ctx, repo, 500, senderID, receiverID, "idem-reject-no-op-"+string(status))
			_, err := repo.db.Exec(ctx, `UPDATE payments SET status = $1 WHERE id = $2`, status, payment.ID)
			require.NoError(t, err)

			var updatedAtBefore time.Time
			require.NoError(t, repo.db.QueryRow(ctx, `SELECT updated_at FROM payments WHERE id = $1`, payment.ID).Scan(&updatedAtBefore))

			unchanged, err := repo.RejectPendingPayment(ctx, payment.ID)

			require.NoError(t, err)
			require.NotNil(t, unchanged)
			require.Equal(t, status, unchanged.Status)
			require.True(t, updatedAtBefore.Equal(unchanged.UpdatedAt))
			require.Equal(t, 0, rejectedEventCount(t, ctx, repo.db, payment.ID))
			require.Equal(t, 1, paymentEventCount(t, ctx, repo.db, payment.ID))
			require.Equal(t, int64(2000), accountBalance(t, ctx, repo.db, senderID))
			require.Equal(t, int64(1000), accountBalance(t, ctx, repo.db, receiverID))
		})
	}
}

func TestPaymentRepositoryRejectPendingPaymentRollsBackStatusWhenEventInsertFails(t *testing.T) {
	repo, ctx := newTestRepo(t)
	senderID := createAccount(t, ctx, repo.db, 2000)
	receiverID := createAccount(t, ctx, repo.db, 1000)
	payment := createPayment(t, ctx, repo, 500, senderID, receiverID, "idem-reject-event-failure")

	_, err := repo.db.Exec(ctx, `
		ALTER TABLE payment_events
		ADD CONSTRAINT payment_events_reject_rejected_for_test
		CHECK (event_type <> 'payment.rejected')
	`)
	require.NoError(t, err)

	rejected, err := repo.RejectPendingPayment(ctx, payment.ID)

	require.Error(t, err)
	require.Nil(t, rejected)
	stored, findErr := repo.FindByID(ctx, payment.ID)
	require.NoError(t, findErr)
	require.Equal(t, domain.PaymentStatusPending, stored.Status)
	require.True(t, payment.UpdatedAt.Equal(stored.UpdatedAt))
	require.Equal(t, 0, rejectedEventCount(t, ctx, repo.db, payment.ID))
	require.Equal(t, 1, paymentEventCount(t, ctx, repo.db, payment.ID))
}

func TestPaymentRepositoryRejectPendingPaymentDoesNotInsertEventWhenStatusUpdateFails(t *testing.T) {
	repo, ctx := newTestRepo(t)
	senderID := createAccount(t, ctx, repo.db, 2000)
	receiverID := createAccount(t, ctx, repo.db, 1000)
	payment := createPayment(t, ctx, repo, 500, senderID, receiverID, "idem-reject-update-failure")

	_, err := repo.db.Exec(ctx, `
		ALTER TABLE payments
		ADD CONSTRAINT payments_reject_rejected_for_test
		CHECK (status <> 'rejected')
	`)
	require.NoError(t, err)

	rejected, err := repo.RejectPendingPayment(ctx, payment.ID)

	require.Error(t, err)
	require.Nil(t, rejected)
	stored, findErr := repo.FindByID(ctx, payment.ID)
	require.NoError(t, findErr)
	require.Equal(t, domain.PaymentStatusPending, stored.Status)
	require.True(t, payment.UpdatedAt.Equal(stored.UpdatedAt))
	require.Equal(t, 0, rejectedEventCount(t, ctx, repo.db, payment.ID))
	require.Equal(t, 1, paymentEventCount(t, ctx, repo.db, payment.ID))
}

func TestPaymentRepositoryRejectPendingPaymentMissingPayment(t *testing.T) {
	repo, ctx := newTestRepo(t)

	rejected, err := repo.RejectPendingPayment(ctx, uuid.MustParse("99999999-9999-9999-9999-999999999999"))

	require.ErrorIs(t, err, ErrPaymentNotFound)
	require.Nil(t, rejected)
}

func TestPaymentRepositoryRejectPendingPaymentHonorsCanceledContext(t *testing.T) {
	repo, ctx := newTestRepo(t)
	senderID := createAccount(t, ctx, repo.db, 2000)
	receiverID := createAccount(t, ctx, repo.db, 1000)
	payment := createPayment(t, ctx, repo, 500, senderID, receiverID, "idem-reject-canceled")
	canceledCtx, cancel := context.WithCancel(ctx)
	cancel()

	rejected, err := repo.RejectPendingPayment(canceledCtx, payment.ID)

	require.ErrorIs(t, err, context.Canceled)
	require.Nil(t, rejected)
	stored, findErr := repo.FindByID(ctx, payment.ID)
	require.NoError(t, findErr)
	require.Equal(t, domain.PaymentStatusPending, stored.Status)
	require.Equal(t, 0, rejectedEventCount(t, ctx, repo.db, payment.ID))
}
