package repository

import (
	"context"
	"encoding/json"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/hisyamsk/vaultpay/internal/domain"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"
)

var testPool *pgxpool.Pool

func TestMain(m *testing.M) {
	ctx := context.Background()

	dsn := "postgres://vaultpay:vaultpay_dev@localhost:5432/vaultpay_test?sslmode=disable"

	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		panic(err)
	}

	testPool = pool
	code := m.Run()

	pool.Close()
	os.Exit(code)
}

func newTestRepo(t *testing.T) (*PaymentRepository, context.Context) {
	t.Helper()

	ctx := context.Background()

	tx, err := testPool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}

	t.Cleanup(func() {
		tx.Rollback(ctx)
	})

	return NewPaymentRepository(tx), ctx
}

func createAccount(t *testing.T, ctx context.Context, tx dbtx, balance int64) uuid.UUID {
	t.Helper()

	var id uuid.UUID
	err := tx.QueryRow(ctx, `
		INSERT INTO accounts (balance)
		VALUES ($1)
		RETURNING id
	`, balance).Scan(&id)
	if err != nil {
		t.Fatal(err)
	}

	return id
}

func createPayment(t *testing.T, ctx context.Context, repo *PaymentRepository, amount int64, senderID uuid.UUID, receiverID uuid.UUID, idempotencyKey string) *domain.Payment {
	t.Helper()

	payment, err := repo.Create(ctx, CreatePaymentParams{
		Amount:         amount,
		SenderID:       senderID,
		ReceiverID:     receiverID,
		IdempotencyKey: idempotencyKey,
	})
	require.NoError(t, err)

	return payment
}

func accountBalance(t *testing.T, ctx context.Context, tx dbtx, accountID uuid.UUID) int64 {
	t.Helper()

	var balance int64
	err := tx.QueryRow(ctx, `
		SELECT balance
		FROM accounts
		WHERE id = $1
	`, accountID).Scan(&balance)
	require.NoError(t, err)

	return balance
}

func ledgerEntryCount(t *testing.T, ctx context.Context, tx dbtx, paymentID uuid.UUID, accountID uuid.UUID, entryType domain.LedgerEntryType) int {
	t.Helper()

	var count int
	err := tx.QueryRow(ctx, `
		SELECT COUNT(*)
		FROM ledger_entries
		WHERE payment_id = $1
			AND account_id = $2
			AND type = $3
	`, paymentID, accountID, entryType).Scan(&count)
	require.NoError(t, err)

	return count
}

func paymentEventCount(t *testing.T, ctx context.Context, tx dbtx, paymentID uuid.UUID) int {
	t.Helper()

	var count int
	err := tx.QueryRow(ctx, `
		SELECT COUNT(*)
		FROM payment_events
		WHERE payment_id = $1
	`, paymentID).Scan(&count)
	require.NoError(t, err)

	return count
}

func TestPaymentRepository_Create_WritesOneUnpublishedCreatedEvent(t *testing.T) {
	repo, ctx := newTestRepo(t)

	senderID := createAccount(t, ctx, repo.db, 2000)
	receiverID := createAccount(t, ctx, repo.db, 1000)
	params := CreatePaymentParams{
		Amount:         500,
		SenderID:       senderID,
		ReceiverID:     receiverID,
		IdempotencyKey: "idem-created-event",
	}

	payment, err := repo.Create(ctx, params)
	require.NoError(t, err)

	var (
		eventID         uuid.UUID
		eventPaymentID  uuid.UUID
		eventType       domain.PaymentEventType
		payload         []byte
		createdAt       time.Time
		publishAttempts int
		published       bool
		attempted       bool
		lastErrorSet    bool
	)
	err = repo.db.QueryRow(ctx, `
			SELECT event_id,
			       payment_id,
			       event_type,
			       payload,
			       created_at,
			       publish_attempts,
		       published_at IS NOT NULL,
		       last_attempted_at IS NOT NULL,
		       last_error IS NOT NULL
		FROM payment_events
			WHERE payment_id = $1
		`, payment.ID).Scan(
		&eventID,
		&eventPaymentID,
		&eventType,
		&payload,
		&createdAt,
		&publishAttempts,
		&published,
		&attempted,
		&lastErrorSet,
	)
	require.NoError(t, err)
	require.NotEqual(t, uuid.Nil, eventID)
	require.Equal(t, payment.ID, eventPaymentID)
	require.Equal(t, domain.PaymentEventTypeCreated, eventType)

	var eventPayload struct {
		EventID    uuid.UUID               `json:"event_id"`
		EventType  domain.PaymentEventType `json:"event_type"`
		PaymentID  uuid.UUID               `json:"payment_id"`
		Attempt    int                     `json:"attempt"`
		OccurredAt time.Time               `json:"occurred_at"`
	}
	require.NoError(t, json.Unmarshal(payload, &eventPayload))
	require.Equal(t, eventID, eventPayload.EventID)
	require.Equal(t, eventType, eventPayload.EventType)
	require.Equal(t, eventPaymentID, eventPayload.PaymentID)
	require.Equal(t, 1, eventPayload.Attempt)
	require.False(t, eventPayload.OccurredAt.IsZero())
	require.True(t, eventPayload.OccurredAt.Equal(createdAt))

	var payloadFields map[string]json.RawMessage
	require.NoError(t, json.Unmarshal(payload, &payloadFields))
	require.Len(t, payloadFields, 5)
	require.NotContains(t, payloadFields, "correlation_id")

	require.Zero(t, publishAttempts)
	require.False(t, published)
	require.False(t, attempted)
	require.False(t, lastErrorSet)
	require.Equal(t, 1, paymentEventCount(t, ctx, repo.db, payment.ID))

	_, err = repo.Create(ctx, params)
	require.ErrorIs(t, err, ErrDuplicateIdempotencyKey)
	require.Equal(t, 1, paymentEventCount(t, ctx, repo.db, payment.ID))
}

func TestPaymentRepository_Create_RollsBackPaymentWhenEventInsertFails(t *testing.T) {
	repo, ctx := newTestRepo(t)
	var eventCountBefore int
	require.NoError(t, repo.db.QueryRow(ctx, `SELECT COUNT(*) FROM payment_events`).Scan(&eventCountBefore))

	_, err := repo.db.Exec(ctx, `
		ALTER TABLE payment_events
		ADD CONSTRAINT payment_events_reject_created_for_test
		CHECK (event_type <> 'payment.created')
	`)
	require.NoError(t, err)

	senderID := createAccount(t, ctx, repo.db, 2000)
	receiverID := createAccount(t, ctx, repo.db, 1000)
	idempotencyKey := "idem-event-insert-failure"

	payment, err := repo.Create(ctx, CreatePaymentParams{
		Amount:         500,
		SenderID:       senderID,
		ReceiverID:     receiverID,
		IdempotencyKey: idempotencyKey,
	})
	require.Error(t, err)
	require.Nil(t, payment)

	var paymentCount int
	err = repo.db.QueryRow(ctx, `
		SELECT COUNT(*)
		FROM payments
		WHERE idempotency_key = $1
	`, idempotencyKey).Scan(&paymentCount)
	require.NoError(t, err)
	require.Zero(t, paymentCount)

	var eventCountAfter int
	require.NoError(t, repo.db.QueryRow(ctx, `SELECT COUNT(*) FROM payment_events`).Scan(&eventCountAfter))
	require.Equal(t, eventCountBefore, eventCountAfter)
}

func TestPaymentRepository_CreateFindAndUpdateStatus(t *testing.T) {
	repo, ctx := newTestRepo(t)

	senderID := createAccount(t, ctx, repo.db, 2000)
	receiverID := createAccount(t, ctx, repo.db, 1000)

	payment, err := repo.Create(ctx, CreatePaymentParams{
		Amount:         500,
		SenderID:       senderID,
		ReceiverID:     receiverID,
		IdempotencyKey: "idem-1",
	})

	require.NoError(t, err)
	require.Equal(t, senderID, payment.SenderID)
	require.Equal(t, receiverID, payment.ReceiverID)
	require.Equal(t, "idem-1", payment.IdempotencyKey)
	require.Equal(t, int64(500), payment.Amount)
	require.Equal(t, domain.PaymentStatusPending, payment.Status)

	found, err := repo.FindByID(ctx, payment.ID)
	require.NoError(t, err)
	require.Equal(t, payment.ID, found.ID)
	require.Equal(t, domain.PaymentStatusPending, found.Status)

	err = repo.UpdateStatus(ctx, payment.ID, domain.PaymentStatusPending, domain.PaymentStatusProcessing)
	require.NoError(t, err)

	updated, err := repo.FindByID(ctx, payment.ID)
	require.NoError(t, err)
	require.Equal(t, domain.PaymentStatusProcessing, updated.Status)
}

func TestPaymentRepository_CreateAndFindEdges(t *testing.T) {
	repo, ctx := newTestRepo(t)

	senderID := createAccount(t, ctx, repo.db, 2000)
	receiverID := createAccount(t, ctx, repo.db, 1000)

	createPayment(t, ctx, repo, 500, senderID, receiverID, "idem-duplicate")

	found, err := repo.FindByIdempotencyKey(ctx, "idem-duplicate")
	require.NoError(t, err)
	require.Equal(t, "idem-duplicate", found.IdempotencyKey)

	_, err = repo.FindByID(ctx, uuid.New())
	require.ErrorIs(t, err, ErrPaymentNotFound)

	_, err = repo.FindByIdempotencyKey(ctx, "missing-idempotency-key")
	require.ErrorIs(t, err, ErrPaymentNotFound)

	_, err = repo.Create(ctx, CreatePaymentParams{
		Amount:         500,
		SenderID:       senderID,
		ReceiverID:     receiverID,
		IdempotencyKey: "idem-duplicate",
	})
	require.ErrorIs(t, err, ErrDuplicateIdempotencyKey)
}

func TestPaymentRepository_UpdateStatusConflict(t *testing.T) {
	repo, ctx := newTestRepo(t)

	senderID := createAccount(t, ctx, repo.db, 2000)
	receiverID := createAccount(t, ctx, repo.db, 1000)
	payment := createPayment(t, ctx, repo, 500, senderID, receiverID, "idem-status-conflict")

	err := repo.UpdateStatus(ctx, payment.ID, domain.PaymentStatusProcessing, domain.PaymentStatusCompleted)
	require.ErrorIs(t, err, ErrPaymentStatusConflict)

	unchanged, err := repo.FindByID(ctx, payment.ID)
	require.NoError(t, err)
	require.Equal(t, domain.PaymentStatusPending, unchanged.Status)
}

func TestPaymentRepository_StartApprovedPaymentProcessing_DebitsSenderOnce(t *testing.T) {
	repo, ctx := newTestRepo(t)

	senderID := createAccount(t, ctx, repo.db, 2000)
	receiverID := createAccount(t, ctx, repo.db, 1000)
	payment := createPayment(t, ctx, repo, 500, senderID, receiverID, "idem-start-success")

	processed, err := repo.StartApprovedPaymentProcessing(ctx, payment.ID)
	require.NoError(t, err)
	require.Equal(t, domain.PaymentStatusProcessing, processed.Status)
	require.Equal(t, int64(1500), accountBalance(t, ctx, repo.db, senderID))
	require.Equal(t, 1, ledgerEntryCount(t, ctx, repo.db, payment.ID, senderID, domain.LedgerEntryTypeDebit))

	processed, err = repo.StartApprovedPaymentProcessing(ctx, payment.ID)
	require.NoError(t, err)
	require.Equal(t, domain.PaymentStatusProcessing, processed.Status)
	require.Equal(t, int64(1500), accountBalance(t, ctx, repo.db, senderID))
	require.Equal(t, 1, ledgerEntryCount(t, ctx, repo.db, payment.ID, senderID, domain.LedgerEntryTypeDebit))
}

func TestPaymentRepository_StartApprovedPaymentProcessing_InsufficientFundsFailsWithoutDebit(t *testing.T) {
	repo, ctx := newTestRepo(t)

	senderID := createAccount(t, ctx, repo.db, 300)
	receiverID := createAccount(t, ctx, repo.db, 1000)
	payment := createPayment(t, ctx, repo, 500, senderID, receiverID, "idem-start-insufficient")

	failed, err := repo.StartApprovedPaymentProcessing(ctx, payment.ID)
	require.NoError(t, err)
	require.Equal(t, domain.PaymentStatusFailed, failed.Status)
	require.Equal(t, int64(300), accountBalance(t, ctx, repo.db, senderID))
	require.Equal(t, 0, ledgerEntryCount(t, ctx, repo.db, payment.ID, senderID, domain.LedgerEntryTypeDebit))

	failed, err = repo.FindByID(ctx, payment.ID)
	require.NoError(t, err)
	require.Equal(t, domain.PaymentStatusFailed, failed.Status)
	require.NotNil(t, failed.ErrorCode)
	require.Equal(t, string(domain.ErrorCodeInsufficientFunds), *failed.ErrorCode)
}

func TestPaymentRepository_CompleteProcessedPayment_CreditsReceiverOnce(t *testing.T) {
	repo, ctx := newTestRepo(t)

	senderID := createAccount(t, ctx, repo.db, 2000)
	receiverID := createAccount(t, ctx, repo.db, 1000)
	payment := createPayment(t, ctx, repo, 500, senderID, receiverID, "idem-complete-success")

	_, err := repo.StartApprovedPaymentProcessing(ctx, payment.ID)
	require.NoError(t, err)

	completed, err := repo.CompleteProcessedPayment(ctx, payment.ID)
	require.NoError(t, err)
	require.Equal(t, domain.PaymentStatusCompleted, completed.Status)
	require.Equal(t, int64(1500), accountBalance(t, ctx, repo.db, receiverID))
	require.Equal(t, 1, ledgerEntryCount(t, ctx, repo.db, payment.ID, receiverID, domain.LedgerEntryTypeCredit))

	completed, err = repo.CompleteProcessedPayment(ctx, payment.ID)
	require.NoError(t, err)
	require.Equal(t, domain.PaymentStatusCompleted, completed.Status)
	require.Equal(t, int64(1500), accountBalance(t, ctx, repo.db, receiverID))
	require.Equal(t, 1, ledgerEntryCount(t, ctx, repo.db, payment.ID, receiverID, domain.LedgerEntryTypeCredit))
}

func TestPaymentRepository_CompleteProcessedPayment_RejectsPendingPayment(t *testing.T) {
	repo, ctx := newTestRepo(t)

	senderID := createAccount(t, ctx, repo.db, 2000)
	receiverID := createAccount(t, ctx, repo.db, 1000)
	payment := createPayment(t, ctx, repo, 500, senderID, receiverID, "idem-complete-pending")

	completed, err := repo.CompleteProcessedPayment(ctx, payment.ID)
	require.ErrorIs(t, err, ErrInvalidStatusTransition)
	require.Nil(t, completed)

	unchanged, err := repo.FindByID(ctx, payment.ID)
	require.NoError(t, err)
	require.Equal(t, domain.PaymentStatusPending, unchanged.Status)
	require.Equal(t, int64(1000), accountBalance(t, ctx, repo.db, receiverID))
	require.Equal(t, 0, ledgerEntryCount(t, ctx, repo.db, payment.ID, receiverID, domain.LedgerEntryTypeCredit))
}

func TestPaymentRepository_FailProcessedPayment_RefundsSenderOnce(t *testing.T) {
	repo, ctx := newTestRepo(t)

	senderID := createAccount(t, ctx, repo.db, 2000)
	receiverID := createAccount(t, ctx, repo.db, 1000)
	payment := createPayment(t, ctx, repo, 500, senderID, receiverID, "idem-fail-success")

	_, err := repo.StartApprovedPaymentProcessing(ctx, payment.ID)
	require.NoError(t, err)
	require.Equal(t, int64(1500), accountBalance(t, ctx, repo.db, senderID))

	failed, err := repo.FailProcessedPayment(ctx, payment.ID, "processor_declined")
	require.NoError(t, err)
	require.Equal(t, domain.PaymentStatusFailed, failed.Status)
	require.Equal(t, int64(2000), accountBalance(t, ctx, repo.db, senderID))
	require.Equal(t, 1, ledgerEntryCount(t, ctx, repo.db, payment.ID, senderID, domain.LedgerEntryTypeRefund))

	failed, err = repo.FailProcessedPayment(ctx, payment.ID, "processor_declined")
	require.NoError(t, err)
	require.Equal(t, domain.PaymentStatusFailed, failed.Status)
	require.Equal(t, int64(2000), accountBalance(t, ctx, repo.db, senderID))
	require.Equal(t, 1, ledgerEntryCount(t, ctx, repo.db, payment.ID, senderID, domain.LedgerEntryTypeRefund))

	failed, err = repo.FindByID(ctx, payment.ID)
	require.NoError(t, err)
	require.NotNil(t, failed.ErrorCode)
	require.Equal(t, "processor_declined", *failed.ErrorCode)
}
