package repository

import (
	"context"
	"errors"

	"github.com/google/uuid"
	"github.com/hisyamsk/vaultpay/internal/domain"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

func NewPaymentRepository(db *pgxpool.Pool) *PaymentRepository {
	return &PaymentRepository{
		db: db,
	}
}

func (r *PaymentRepository) Create(ctx context.Context, params CreatePaymentParams) (*domain.Payment, error) {
	payment := &domain.Payment{}
	err := r.db.QueryRow(ctx, `
		INSERT INTO payments (amount, sender_id, receiver_id, idempotency_key, description)
		VALUES ($1, $2, $3, $4, $5)
		RETURNING id, amount, sender_id, receiver_id, idempotency_key, status, error_code, description, created_at, updated_at
	`, params.Amount, params.SenderID, params.ReceiverID, params.IdempotencyKey, params.Description).Scan(
		&payment.ID, &payment.Amount, &payment.SenderID, &payment.ReceiverID, &payment.IdempotencyKey, &payment.Status,
		&payment.ErrorCode, &payment.Description, &payment.CreatedAt, &payment.UpdatedAt,
	)

	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) {
			if pgErr.Code == "23505" && pgErr.ConstraintName == "payments_idempotency_key_unique" {
				return nil, ErrDuplicateIdempotencyKey
			}
		}
		return nil, err
	}

	return payment, nil
}

func (r *PaymentRepository) FindByIdempotencyKey(ctx context.Context, idempotencyKey string) (*domain.Payment, error) {
	payment := &domain.Payment{}
	err := r.db.QueryRow(ctx, `
		SELECT id, amount, sender_id, receiver_id, idempotency_key, status, error_code, description, created_at, updated_at
		FROM payments
		WHERE idempotency_key = $1`, idempotencyKey).Scan(
		&payment.ID, &payment.Amount, &payment.SenderID, &payment.ReceiverID, &payment.IdempotencyKey, &payment.Status,
		&payment.ErrorCode, &payment.Description, &payment.CreatedAt, &payment.UpdatedAt,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrPaymentNotFound
		}
		return nil, err
	}

	return payment, nil
}

func (r *PaymentRepository) FindById(ctx context.Context, id uuid.UUID) (*domain.Payment, error) {
	payment := &domain.Payment{}
	err := r.db.QueryRow(ctx, `
		SELECT id, amount, sender_id, receiver_id, idempotency_key, status, error_code, description, created_at, updated_at
		FROM payments
		WHERE id = $1`, id).Scan(
		&payment.ID, &payment.Amount, &payment.SenderID, &payment.ReceiverID, &payment.IdempotencyKey, &payment.Status,
		&payment.ErrorCode, &payment.Description, &payment.CreatedAt, &payment.UpdatedAt,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrPaymentNotFound
		}
		return nil, err
	}

	return payment, nil
}

func (r *PaymentRepository) UpdateStatus(ctx context.Context, id uuid.UUID, fromStatus domain.PaymentStatus, toStatus domain.PaymentStatus) error {
	tag, err := r.db.Exec(ctx, `
		UPDATE payments SET status = $1, updated_at = NOW()
		WHERE id = $2 AND status = $3;`, toStatus, id, fromStatus)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrPaymentStatusConflict
	}

	return nil
}

func (r *PaymentRepository) StartApprovedPaymentProcessing(ctx context.Context, paymentID uuid.UUID) (domain.PaymentStatus, error) {
	tx, err := r.db.Begin(ctx)
	if err != nil {
		return "", err
	}
	defer tx.Rollback(ctx)

	var status domain.PaymentStatus
	var senderID uuid.UUID
	var amount int64

	err = tx.QueryRow(ctx, `
		SELECT status, sender_id, amount
		FROM payments
		WHERE id = $1
		FOR UPDATE
	`, paymentID).Scan(&status, &senderID, &amount)

	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return "", ErrPaymentNotFound
		}
		return "", err
	}

	switch status {
	case domain.PaymentStatusProcessing, domain.PaymentStatusCompleted, domain.PaymentStatusFailed, domain.PaymentStatusRejected:
		return status, nil
	case domain.PaymentStatusPending:
		var balance int64
		err = tx.QueryRow(ctx, `
			SELECT balance 
			FROM accounts
			WHERE id = $1 
			FOR UPDATE
		`, senderID).Scan(&balance)
		if err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return "", ErrAccountNotFound
			}
			return "", err
		}

		err = tx.QueryRow(ctx, `
			UPDATE accounts
			SET balance = balance - $1, updated_at = NOW()
			WHERE id = $2 AND balance >= $1
			RETURNING balance
		`, amount, senderID).Scan(&balance)

		if err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				_, err = tx.Exec(ctx, `
					UPDATE payments
					SET status = $1, error_code = $2, updated_at = NOW()
					WHERE id = $3
					`, domain.PaymentStatusFailed, domain.ErrorCodeInsufficientFunds, paymentID)

				if err != nil {
					return "", err
				}

				return domain.PaymentStatusFailed, tx.Commit(ctx)
			}
			return "", err
		}

		_, err = tx.Exec(ctx, `
			INSERT INTO ledger_entries(payment_id, account_id, type, amount)
			VALUES($1, $2, $3, $4)
		`, paymentID, senderID, domain.LedgerEntryTypeDebit, amount)

		if err != nil {
			return "", err
		}

		_, err = tx.Exec(ctx, `
			UPDATE payments
			SET status = $1, updated_at = NOW()
			WHERE id = $2
		`, domain.PaymentStatusProcessing, paymentID)

		if err != nil {
			return "", err
		}

		return domain.PaymentStatusProcessing, tx.Commit(ctx)
	}

	return "", ErrUnrecognizedPaymentStatus
}

func (r *PaymentRepository) CompleteProcessedPayment(ctx context.Context, paymentID uuid.UUID) (domain.PaymentStatus, error) {
	tx, err := r.db.Begin(ctx)
	if err != nil {
		return "", err
	}
	defer tx.Rollback(ctx)

	var status domain.PaymentStatus
	var receiverID uuid.UUID
	var amount int64

	err = tx.QueryRow(ctx, `
		SELECT status, receiver_id, amount
		FROM payments
		WHERE id = $1
		FOR UPDATE
	`, paymentID).Scan(&status, &receiverID, &amount)

	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return "", ErrPaymentNotFound
		}
		return "", err
	}

	switch status {
	case domain.PaymentStatusPending:
		return "", ErrInvalidStatusTransition
	case domain.PaymentStatusCompleted, domain.PaymentStatusFailed, domain.PaymentStatusRejected:
		return status, nil
	case domain.PaymentStatusProcessing:
		var balance int64
		err = tx.QueryRow(ctx, `
			UPDATE accounts
			SET balance = balance + $1, updated_at = NOW()
			WHERE id = $2
			RETURNING balance
		`, amount, receiverID).Scan(&balance)

		if err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return "", ErrAccountNotFound
			}
			return "", err
		}

		_, err = tx.Exec(ctx, `
			INSERT INTO ledger_entries(payment_id, account_id, type, amount)
			VALUES($1, $2, $3, $4)
		`, paymentID, receiverID, domain.LedgerEntryTypeCredit, amount)

		if err != nil {
			return "", err
		}

		_, err = tx.Exec(ctx, `
			UPDATE payments
			SET status = $1, updated_at = NOW()
			WHERE id = $2
		`, domain.PaymentStatusCompleted, paymentID)

		if err != nil {
			return "", err
		}

		return domain.PaymentStatusCompleted, tx.Commit(ctx)
	}

	return "", ErrUnrecognizedPaymentStatus
}

func (r *PaymentRepository) FailProcessedPayment(ctx context.Context, paymentID uuid.UUID) (domain.PaymentStatus, error) {
	tx, err := r.db.Begin(ctx)
	if err != nil {
		return "", err
	}
	defer tx.Rollback(ctx)

	var status domain.PaymentStatus
	var senderID uuid.UUID
	var amount int64

	err = tx.QueryRow(ctx, `
		SELECT status, sender_id, amount
		FROM payments
		WHERE id = $1
		FOR UPDATE
	`, paymentID).Scan(&status, &senderID, &amount)

	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return "", ErrPaymentNotFound
		}
	}

	switch status {
	case domain.PaymentStatusPending, domain.PaymentStatusCompleted, domain.PaymentStatusFailed, domain.PaymentStatusRejected:
		return status, nil
	case domain.PaymentStatusProcessing:
		var balance int64
		err = tx.QueryRow(ctx, `
			UPDATE accounts
			SET balance = balance + $1, updated_at = NOW()
			WHERE id = $2
			RETURNING balance
		`, amount, senderID).Scan(&balance)

		if err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return "", ErrAccountNotFound
			}

			return "", err
		}

		_, err = tx.Exec(ctx, `
			INSERT INTO ledger_entries(payment_id, account_id, type, amount)
			VALUES($1, $2, $3, $4)
		`, paymentID, senderID, domain.LedgerEntryTypeRefund, amount)

		if err != nil {
			return "", err
		}

		_, err = tx.Exec(ctx, `
			UPDATE payments
			SET status = $1, updated_at = NOW()
			WHERE id = $2
		`, domain.PaymentStatusFailed, paymentID)

		if err != nil {
			return "", err
		}

		return domain.PaymentStatusFailed, tx.Commit(ctx)
	}

	return "", ErrUnrecognizedPaymentStatus
}
