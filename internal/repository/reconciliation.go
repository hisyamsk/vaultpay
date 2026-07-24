package repository

import (
	"context"
	"fmt"
	"time"

	"github.com/hisyamsk/vaultpay/internal/domain"
	"github.com/jackc/pgx/v5"
)

type reconciliationDB interface {
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
}

type ReconciliationRepository struct {
	db reconciliationDB
}

func NewReconciliationRepository(db reconciliationDB) *ReconciliationRepository {
	return &ReconciliationRepository{db: db}
}

// FindLedgerDiscrepancies returns one result per broken payment/ledger rule,
// ordered by payment ID and discrepancy kind.
func (r *ReconciliationRepository) FindLedgerDiscrepancies(ctx context.Context) ([]domain.ReconciliationDiscrepancy, error) {
	rows, err := r.db.Query(ctx, `
		SELECT
			'completed_missing_sender_debit' AS kind,
			p.id AS payment_id,
			NULL::uuid AS event_id
		FROM payments p
		WHERE p.status = $1
			AND NOT EXISTS (
			SELECT 1
			FROM ledger_entries le
			WHERE le.payment_id = p.id
				AND le.account_id = p.sender_id
				AND le.type = $2
				AND le.amount = p.amount
		)

		UNION ALL

		SELECT
			'completed_missing_receiver_credit' AS kind,
			p.id AS payment_id,
			NULL::uuid AS event_id
		FROM payments p
		WHERE p.status = $1
			AND NOT EXISTS (
			SELECT 1
			FROM ledger_entries le
			WHERE le.payment_id = p.id
				AND le.account_id = p.receiver_id
				AND le.type = $3
				AND le.amount = p.amount
		)

		UNION ALL

		SELECT
			'failed_missing_sender_debit' AS kind,
			p.id AS payment_id,
			NULL::uuid AS event_id
		FROM payments p
		WHERE p.status = $4
			AND p.error_code IS DISTINCT FROM $5
			AND NOT EXISTS (
			SELECT 1
			FROM ledger_entries le
			WHERE le.payment_id = p.id
				AND le.account_id = p.sender_id
				AND le.type = $2
				AND le.amount = p.amount
		)

		UNION ALL

		SELECT
			'failed_missing_sender_refund' AS kind,
			p.id AS payment_id,
			NULL::uuid AS event_id
		FROM payments p
		WHERE p.status = $4
			AND p.error_code IS DISTINCT FROM $5
			AND NOT EXISTS (
			SELECT 1
			FROM ledger_entries le
			WHERE le.payment_id = p.id
				AND le.account_id = p.sender_id
				AND le.type = $6
				AND le.amount = p.amount
		)

		UNION ALL

		SELECT
			'pending_unexpected_ledger_movement' as kind,
			p.id AS payment_id,
			NULL::uuid AS event_id
		FROM payments p
		WHERE p.status = $7
			AND EXISTS (
			SELECT 1
			FROM ledger_entries le
			WHERE le.payment_id = p.id
		)

		UNION ALL

		SELECT
			'rejected_unexpected_ledger_movement' as kind,
			p.id AS payment_id,
			NULL::uuid AS event_id
		FROM payments p
		WHERE p.status = $8
			AND EXISTS (
			SELECT 1
			FROM ledger_entries le
			WHERE le.payment_id = p.id
		)

		ORDER BY payment_id, kind
	`,
		domain.PaymentStatusCompleted,     // 1
		domain.LedgerEntryTypeDebit,       // 2
		domain.LedgerEntryTypeCredit,      // 3
		domain.PaymentStatusFailed,        // 4
		domain.ErrorCodeInsufficientFunds, // 5
		domain.LedgerEntryTypeRefund,      // 6
		domain.PaymentStatusPending,       // 7
		domain.PaymentStatusRejected,      // 8
	)
	if err != nil {
		return nil, fmt.Errorf("find ledger discrepancies: query: %w", err)
	}

	discrepancies, err := pgx.CollectRows(rows, pgx.RowToStructByName[domain.ReconciliationDiscrepancy])
	if err != nil {
		return nil, fmt.Errorf("find ledger discrepancies: collect rows: %w", err)
	}

	return discrepancies, nil
}

// FindStalePayments returns pending or processing payments updated strictly
// before cutoff, ordered by payment ID.
func (r *ReconciliationRepository) FindStalePayments(ctx context.Context, cutoff time.Time) ([]domain.ReconciliationDiscrepancy, error) {
	return nil, nil
}

// FindStaleUnpublishedEvents returns unpublished events created strictly before
// cutoff, ordered by event ID.
func (r *ReconciliationRepository) FindStaleUnpublishedEvents(ctx context.Context, cutoff time.Time) ([]domain.ReconciliationDiscrepancy, error) {
	return nil, nil
}
