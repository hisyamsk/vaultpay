package repository

import (
	"context"
	"sort"
	"testing"

	"github.com/google/uuid"
	"github.com/hisyamsk/vaultpay/internal/domain"
	"github.com/stretchr/testify/require"
)

type reconciliationPaymentFixture struct {
	payment    *domain.Payment
	senderID   uuid.UUID
	receiverID uuid.UUID
}

func newReconciliationTestRepositories(
	t *testing.T,
) (*PaymentRepository, *ReconciliationRepository, context.Context) {
	t.Helper()

	ctx := context.Background()
	tx, err := testPool.Begin(ctx)
	require.NoError(t, err)
	t.Cleanup(func() {
		require.NoError(t, tx.Rollback(ctx))
	})

	return NewPaymentRepository(tx), NewReconciliationRepository(tx), ctx
}

func seedReconciliationPayment(
	t *testing.T,
	ctx context.Context,
	repo *PaymentRepository,
	idempotencyKey string,
	senderBalance int64,
	amount int64,
) reconciliationPaymentFixture {
	t.Helper()

	senderID := createAccount(t, ctx, repo.db, senderBalance)
	receiverID := createAccount(t, ctx, repo.db, 1_000)
	payment := createPayment(t, ctx, repo, amount, senderID, receiverID, idempotencyKey)
	return reconciliationPaymentFixture{
		payment:    payment,
		senderID:   senderID,
		receiverID: receiverID,
	}
}

func seedCompletedReconciliationPayment(
	t *testing.T,
	ctx context.Context,
	repo *PaymentRepository,
	idempotencyKey string,
) reconciliationPaymentFixture {
	t.Helper()

	fixture := seedReconciliationPayment(t, ctx, repo, idempotencyKey, 2_000, 500)
	_, err := repo.StartApprovedPaymentProcessing(ctx, fixture.payment.ID)
	require.NoError(t, err)
	fixture.payment, err = repo.CompleteProcessedPayment(ctx, fixture.payment.ID)
	require.NoError(t, err)
	return fixture
}

func seedProcessedFailedReconciliationPayment(
	t *testing.T,
	ctx context.Context,
	repo *PaymentRepository,
	idempotencyKey string,
) reconciliationPaymentFixture {
	t.Helper()

	fixture := seedReconciliationPayment(t, ctx, repo, idempotencyKey, 2_000, 500)
	_, err := repo.StartApprovedPaymentProcessing(ctx, fixture.payment.ID)
	require.NoError(t, err)
	fixture.payment, err = repo.FailProcessedPayment(
		ctx,
		fixture.payment.ID,
		string(domain.ErrorCodeReceiverAccountNotFound),
	)
	require.NoError(t, err)
	return fixture
}

func deleteReconciliationLedgerEntry(
	t *testing.T,
	ctx context.Context,
	repo *PaymentRepository,
	paymentID uuid.UUID,
	accountID uuid.UUID,
	entryType domain.LedgerEntryType,
) {
	t.Helper()

	tag, err := repo.db.Exec(ctx, `
		DELETE FROM ledger_entries
		WHERE payment_id = $1 AND account_id = $2 AND type = $3
	`, paymentID, accountID, entryType)
	require.NoError(t, err)
	require.Equal(t, int64(1), tag.RowsAffected())
}

func insertUnexpectedReconciliationLedgerEntry(
	t *testing.T,
	ctx context.Context,
	repo *PaymentRepository,
	fixture reconciliationPaymentFixture,
) {
	t.Helper()

	_, err := repo.db.Exec(ctx, `
		INSERT INTO ledger_entries (payment_id, account_id, type, amount)
		VALUES ($1, $2, $3, $4)
	`, fixture.payment.ID, fixture.senderID, domain.LedgerEntryTypeDebit, fixture.payment.Amount)
	require.NoError(t, err)
}

func TestReconciliationRepositoryFindLedgerDiscrepanciesReportsOnlyBrokenRulesInStableOrder(t *testing.T) {
	repo, reconciliationRepo, ctx := newReconciliationTestRepositories(t)

	completedMissingDebit := seedCompletedReconciliationPayment(t, ctx, repo, "reconcile-completed-missing-debit")
	deleteReconciliationLedgerEntry(
		t, ctx, repo,
		completedMissingDebit.payment.ID,
		completedMissingDebit.senderID,
		domain.LedgerEntryTypeDebit,
	)

	completedMissingCredit := seedCompletedReconciliationPayment(t, ctx, repo, "reconcile-completed-missing-credit")
	deleteReconciliationLedgerEntry(
		t, ctx, repo,
		completedMissingCredit.payment.ID,
		completedMissingCredit.receiverID,
		domain.LedgerEntryTypeCredit,
	)

	failedMissingDebit := seedProcessedFailedReconciliationPayment(t, ctx, repo, "reconcile-failed-missing-debit")
	deleteReconciliationLedgerEntry(
		t, ctx, repo,
		failedMissingDebit.payment.ID,
		failedMissingDebit.senderID,
		domain.LedgerEntryTypeDebit,
	)

	failedMissingRefund := seedProcessedFailedReconciliationPayment(t, ctx, repo, "reconcile-failed-missing-refund")
	deleteReconciliationLedgerEntry(
		t, ctx, repo,
		failedMissingRefund.payment.ID,
		failedMissingRefund.senderID,
		domain.LedgerEntryTypeRefund,
	)

	pendingWithMovement := seedReconciliationPayment(t, ctx, repo, "reconcile-pending-movement", 2_000, 500)
	insertUnexpectedReconciliationLedgerEntry(t, ctx, repo, pendingWithMovement)

	rejectedWithMovement := seedReconciliationPayment(t, ctx, repo, "reconcile-rejected-movement", 2_000, 500)
	rejectedPayment, err := repo.RejectPendingPayment(ctx, rejectedWithMovement.payment.ID)
	require.NoError(t, err)
	rejectedWithMovement.payment = rejectedPayment
	insertUnexpectedReconciliationLedgerEntry(t, ctx, repo, rejectedWithMovement)

	// These consistent lifecycles must not be reported.
	seedCompletedReconciliationPayment(t, ctx, repo, "reconcile-consistent-completed")
	seedProcessedFailedReconciliationPayment(t, ctx, repo, "reconcile-consistent-failed")
	seedReconciliationPayment(t, ctx, repo, "reconcile-consistent-pending", 2_000, 500)
	cleanRejected := seedReconciliationPayment(t, ctx, repo, "reconcile-consistent-rejected", 2_000, 500)
	_, err = repo.RejectPendingPayment(ctx, cleanRejected.payment.ID)
	require.NoError(t, err)
	insufficientFunds := seedReconciliationPayment(t, ctx, repo, "reconcile-insufficient-funds", 100, 500)
	insufficientFunds.payment, err = repo.StartApprovedPaymentProcessing(ctx, insufficientFunds.payment.ID)
	require.NoError(t, err)
	require.Equal(t, domain.PaymentStatusFailed, insufficientFunds.payment.Status)
	require.NotNil(t, insufficientFunds.payment.ErrorCode)
	require.Equal(t, string(domain.ErrorCodeInsufficientFunds), *insufficientFunds.payment.ErrorCode)

	expected := []domain.ReconciliationDiscrepancy{
		{Kind: domain.ReconciliationCompletedMissingSenderDebit, PaymentID: completedMissingDebit.payment.ID},
		{Kind: domain.ReconciliationCompletedMissingReceiverCredit, PaymentID: completedMissingCredit.payment.ID},
		{Kind: domain.ReconciliationFailedMissingSenderDebit, PaymentID: failedMissingDebit.payment.ID},
		{Kind: domain.ReconciliationFailedMissingSenderRefund, PaymentID: failedMissingRefund.payment.ID},
		{Kind: domain.ReconciliationPendingUnexpectedLedgerMovement, PaymentID: pendingWithMovement.payment.ID},
		{Kind: domain.ReconciliationRejectedUnexpectedLedgerMovement, PaymentID: rejectedWithMovement.payment.ID},
	}
	sort.Slice(expected, func(i, j int) bool {
		if expected[i].PaymentID != expected[j].PaymentID {
			return expected[i].PaymentID.String() < expected[j].PaymentID.String()
		}
		return expected[i].Kind < expected[j].Kind
	})

	discrepancies, err := reconciliationRepo.FindLedgerDiscrepancies(ctx)

	require.NoError(t, err)
	require.Equal(t, expected, discrepancies)
}

func TestReconciliationRepositoryFindLedgerDiscrepanciesHonorsCanceledContext(t *testing.T) {
	_, reconciliationRepo, _ := newReconciliationTestRepositories(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	discrepancies, err := reconciliationRepo.FindLedgerDiscrepancies(ctx)

	require.Nil(t, discrepancies)
	require.ErrorIs(t, err, context.Canceled)
}
