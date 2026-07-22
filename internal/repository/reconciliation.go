package repository

import (
	"context"
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
			p.id AS payment.id
			NULL:uuid AS event_id
		FROM payments p
		WHERE p.status = $1
		AND NOT EXISTS (
			SELECT 1
			FROM ledger_entries le
			WHERE le.payment_id = p.id
				AND le.account_id = p.sender_id
				AND type = $2
		)
	`)
	if err != nil {
		return []domain.ReconciliationDiscrepancy{}, err
	}

	defer rows.Close()

	discrepancies, err := pgx.CollectRows(rows, pgx.RowToStructByName[domain.ReconciliationDiscrepancy])
	if err != nil {
		return []domain.ReconciliationDiscrepancy{}, err
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
