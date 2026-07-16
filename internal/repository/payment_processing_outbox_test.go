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

type storedPaymentEvent struct {
	eventID         uuid.UUID
	paymentID       uuid.UUID
	eventType       domain.PaymentEventType
	payload         []byte
	createdAt       time.Time
	publishAttempts int
	publishedAt     *time.Time
	lastAttemptedAt *time.Time
	lastError       *string
}

func paymentEventTypeCount(t *testing.T, ctx context.Context, tx dbtx, paymentID uuid.UUID, eventType domain.PaymentEventType) int {
	t.Helper()

	var count int
	err := tx.QueryRow(ctx, `
		SELECT COUNT(*)
		FROM payment_events
		WHERE payment_id = $1 AND event_type = $2
	`, paymentID, eventType).Scan(&count)
	require.NoError(t, err)
	return count
}

func loadStoredPaymentEvent(t *testing.T, ctx context.Context, tx dbtx, paymentID uuid.UUID, eventType domain.PaymentEventType) storedPaymentEvent {
	t.Helper()

	var event storedPaymentEvent
	err := tx.QueryRow(ctx, `
		SELECT event_id, payment_id, event_type, payload, created_at,
		       publish_attempts, published_at, last_attempted_at, last_error
		FROM payment_events
		WHERE payment_id = $1 AND event_type = $2
	`, paymentID, eventType).Scan(
		&event.eventID,
		&event.paymentID,
		&event.eventType,
		&event.payload,
		&event.createdAt,
		&event.publishAttempts,
		&event.publishedAt,
		&event.lastAttemptedAt,
		&event.lastError,
	)
	require.NoError(t, err)
	return event
}

func requirePaymentEventPayload(t *testing.T, event storedPaymentEvent) {
	t.Helper()

	require.NotEqual(t, uuid.Nil, event.eventID)
	require.Zero(t, event.publishAttempts)
	require.Nil(t, event.publishedAt)
	require.Nil(t, event.lastAttemptedAt)
	require.Nil(t, event.lastError)

	var payload domain.PaymentEventPayload
	require.NoError(t, json.Unmarshal(event.payload, &payload))
	require.Equal(t, event.eventID, payload.EventID)
	require.Equal(t, event.paymentID, payload.PaymentID)
	require.Equal(t, event.eventType, payload.EventType)
	require.Equal(t, 1, payload.Attempt)
	require.True(t, payload.OccurredAt.Equal(event.createdAt))
}

func TestPaymentRepositoryStartApprovedPaymentProcessingCommitsDebitLedgerStatusAndOneEvent(t *testing.T) {
	repo, ctx := newTestRepo(t)
	senderID := createAccount(t, ctx, repo.db, 2000)
	receiverID := createAccount(t, ctx, repo.db, 1000)
	payment := createPayment(t, ctx, repo, 500, senderID, receiverID, "idem-processing-event")

	first, err := repo.StartApprovedPaymentProcessing(ctx, payment.ID)

	require.NoError(t, err)
	require.NotNil(t, first)
	require.Equal(t, domain.PaymentStatusProcessing, first.Status)
	require.Nil(t, first.ErrorCode)
	require.Equal(t, int64(1500), accountBalance(t, ctx, repo.db, senderID))
	require.Equal(t, int64(1000), accountBalance(t, ctx, repo.db, receiverID))
	require.Equal(t, 1, ledgerEntryCount(t, ctx, repo.db, payment.ID, senderID, domain.LedgerEntryTypeDebit))
	require.Equal(t, 1, paymentEventTypeCount(t, ctx, repo.db, payment.ID, domain.PaymentEventTypeProcessing))
	require.Equal(t, 2, paymentEventCount(t, ctx, repo.db, payment.ID))

	event := loadStoredPaymentEvent(t, ctx, repo.db, payment.ID, domain.PaymentEventTypeProcessing)
	requirePaymentEventPayload(t, event)

	second, err := repo.StartApprovedPaymentProcessing(ctx, payment.ID)

	require.NoError(t, err)
	require.NotNil(t, second)
	require.Equal(t, domain.PaymentStatusProcessing, second.Status)
	require.True(t, first.UpdatedAt.Equal(second.UpdatedAt))
	require.Equal(t, int64(1500), accountBalance(t, ctx, repo.db, senderID))
	require.Equal(t, 1, ledgerEntryCount(t, ctx, repo.db, payment.ID, senderID, domain.LedgerEntryTypeDebit))
	require.Equal(t, 1, paymentEventTypeCount(t, ctx, repo.db, payment.ID, domain.PaymentEventTypeProcessing))
	require.Equal(t, event.eventID, loadStoredPaymentEvent(t, ctx, repo.db, payment.ID, domain.PaymentEventTypeProcessing).eventID)
}

func TestPaymentRepositoryStartApprovedPaymentProcessingRollsBackWhenProcessingEventInsertFails(t *testing.T) {
	repo, ctx := newTestRepo(t)
	senderID := createAccount(t, ctx, repo.db, 2000)
	receiverID := createAccount(t, ctx, repo.db, 1000)
	payment := createPayment(t, ctx, repo, 500, senderID, receiverID, "idem-processing-event-failure")

	_, err := repo.db.Exec(ctx, `
		ALTER TABLE payment_events
		ADD CONSTRAINT payment_events_reject_processing_for_test
		CHECK (event_type <> 'payment.processing')
	`)
	require.NoError(t, err)

	processed, err := repo.StartApprovedPaymentProcessing(ctx, payment.ID)

	require.Error(t, err)
	require.Nil(t, processed)
	stored, findErr := repo.FindByID(ctx, payment.ID)
	require.NoError(t, findErr)
	require.Equal(t, domain.PaymentStatusPending, stored.Status)
	require.Nil(t, stored.ErrorCode)
	require.True(t, payment.UpdatedAt.Equal(stored.UpdatedAt))
	require.Equal(t, int64(2000), accountBalance(t, ctx, repo.db, senderID))
	require.Equal(t, int64(1000), accountBalance(t, ctx, repo.db, receiverID))
	require.Equal(t, 0, ledgerEntryCount(t, ctx, repo.db, payment.ID, senderID, domain.LedgerEntryTypeDebit))
	require.Equal(t, 0, paymentEventTypeCount(t, ctx, repo.db, payment.ID, domain.PaymentEventTypeProcessing))
	require.Equal(t, 1, paymentEventCount(t, ctx, repo.db, payment.ID))
}

func TestPaymentRepositoryStartApprovedPaymentProcessingRollsBackDebitWhenLedgerInsertFails(t *testing.T) {
	repo, ctx := newTestRepo(t)
	senderID := createAccount(t, ctx, repo.db, 2000)
	receiverID := createAccount(t, ctx, repo.db, 1000)
	payment := createPayment(t, ctx, repo, 500, senderID, receiverID, "idem-processing-ledger-failure")

	_, err := repo.db.Exec(ctx, `
		ALTER TABLE ledger_entries
		ADD CONSTRAINT ledger_entries_reject_debit_for_test
		CHECK (type <> 'debit')
	`)
	require.NoError(t, err)

	processed, err := repo.StartApprovedPaymentProcessing(ctx, payment.ID)

	require.Error(t, err)
	require.Nil(t, processed)
	stored, findErr := repo.FindByID(ctx, payment.ID)
	require.NoError(t, findErr)
	require.Equal(t, domain.PaymentStatusPending, stored.Status)
	require.Nil(t, stored.ErrorCode)
	require.True(t, payment.UpdatedAt.Equal(stored.UpdatedAt))
	require.Equal(t, int64(2000), accountBalance(t, ctx, repo.db, senderID))
	require.Equal(t, 0, ledgerEntryCount(t, ctx, repo.db, payment.ID, senderID, domain.LedgerEntryTypeDebit))
	require.Equal(t, 0, paymentEventTypeCount(t, ctx, repo.db, payment.ID, domain.PaymentEventTypeProcessing))
	require.Equal(t, 1, paymentEventCount(t, ctx, repo.db, payment.ID))
}

func TestPaymentRepositoryStartApprovedPaymentProcessingInsufficientFundsCommitsFailedStatusAndOneEvent(t *testing.T) {
	repo, ctx := newTestRepo(t)
	senderID := createAccount(t, ctx, repo.db, 300)
	receiverID := createAccount(t, ctx, repo.db, 1000)
	payment := createPayment(t, ctx, repo, 500, senderID, receiverID, "idem-insufficient-event")

	first, err := repo.StartApprovedPaymentProcessing(ctx, payment.ID)

	require.NoError(t, err)
	require.NotNil(t, first)
	require.Equal(t, domain.PaymentStatusFailed, first.Status)
	require.NotNil(t, first.ErrorCode)
	require.Equal(t, string(domain.ErrorCodeInsufficientFunds), *first.ErrorCode)
	require.Equal(t, int64(300), accountBalance(t, ctx, repo.db, senderID))
	require.Equal(t, int64(1000), accountBalance(t, ctx, repo.db, receiverID))
	require.Equal(t, 0, ledgerEntryCount(t, ctx, repo.db, payment.ID, senderID, domain.LedgerEntryTypeDebit))
	require.Equal(t, 1, paymentEventTypeCount(t, ctx, repo.db, payment.ID, domain.PaymentEventTypeFailed))
	require.Equal(t, 2, paymentEventCount(t, ctx, repo.db, payment.ID))

	event := loadStoredPaymentEvent(t, ctx, repo.db, payment.ID, domain.PaymentEventTypeFailed)
	requirePaymentEventPayload(t, event)

	second, err := repo.StartApprovedPaymentProcessing(ctx, payment.ID)

	require.NoError(t, err)
	require.NotNil(t, second)
	require.Equal(t, domain.PaymentStatusFailed, second.Status)
	require.True(t, first.UpdatedAt.Equal(second.UpdatedAt))
	require.Equal(t, int64(300), accountBalance(t, ctx, repo.db, senderID))
	require.Equal(t, 0, ledgerEntryCount(t, ctx, repo.db, payment.ID, senderID, domain.LedgerEntryTypeDebit))
	require.Equal(t, 1, paymentEventTypeCount(t, ctx, repo.db, payment.ID, domain.PaymentEventTypeFailed))
	require.Equal(t, event.eventID, loadStoredPaymentEvent(t, ctx, repo.db, payment.ID, domain.PaymentEventTypeFailed).eventID)
}

func TestPaymentRepositoryStartApprovedPaymentProcessingRollsBackInsufficientFundsWhenFailedEventInsertFails(t *testing.T) {
	repo, ctx := newTestRepo(t)
	senderID := createAccount(t, ctx, repo.db, 300)
	receiverID := createAccount(t, ctx, repo.db, 1000)
	payment := createPayment(t, ctx, repo, 500, senderID, receiverID, "idem-insufficient-event-failure")

	_, err := repo.db.Exec(ctx, `
		ALTER TABLE payment_events
		ADD CONSTRAINT payment_events_reject_failed_for_test
		CHECK (event_type <> 'payment.failed')
	`)
	require.NoError(t, err)

	failed, err := repo.StartApprovedPaymentProcessing(ctx, payment.ID)

	require.Error(t, err)
	require.Nil(t, failed)
	stored, findErr := repo.FindByID(ctx, payment.ID)
	require.NoError(t, findErr)
	require.Equal(t, domain.PaymentStatusPending, stored.Status)
	require.Nil(t, stored.ErrorCode)
	require.True(t, payment.UpdatedAt.Equal(stored.UpdatedAt))
	require.Equal(t, int64(300), accountBalance(t, ctx, repo.db, senderID))
	require.Equal(t, int64(1000), accountBalance(t, ctx, repo.db, receiverID))
	require.Equal(t, 0, ledgerEntryCount(t, ctx, repo.db, payment.ID, senderID, domain.LedgerEntryTypeDebit))
	require.Equal(t, 0, paymentEventTypeCount(t, ctx, repo.db, payment.ID, domain.PaymentEventTypeFailed))
	require.Equal(t, 1, paymentEventCount(t, ctx, repo.db, payment.ID))
}
