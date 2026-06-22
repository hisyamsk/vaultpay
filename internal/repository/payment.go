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

func (r *PaymentRepository) ProcessPayment(ctx context.Context, params ProcessPaymentParams) error {
	tx, err := r.db.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)

	var currStatus domain.PaymentStatus
	err = tx.QueryRow(ctx, `
		SELECT status
		FROM payments
		WHERE id = $1
		FOR UPDATE
	`, params.Status, params.PaymentID).Scan(&currStatus)

	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrPaymentNotFound
		}
		return err
	}

	switch currStatus {
	case domain.PaymentStatusProcessing:
		return nil // return existing
	case domain.PaymentStatusCompleted, domain.PaymentStatusFailed, domain.PaymentStatusRejected:
		return nil // terminal return no - op
	case domain.PaymentStatusPending:
		_, err = tx.Exec(ctx, `
			SELECT balance 
			FROM accounts
			WHERE id = $1 
			FOR UPDATE
		`, params.AccountID)
		if err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return ErrAccountNotFound
			}
			return err
		}

		_, err = tx.Exec(ctx, `
			UPDATE accounts
			SET balance = balance - $1
			WHERE id = $2 AND balance >= $1
		`, params.Amount, params.AccountID)

		if err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return ErrInsufficientBalance
			}
			return err
		}

		_, err = tx.Exec(ctx, `
			INSERT INTO ledger_entries(payment_id, account_id, type, amount)
			VALUES($1, $2, $3, $4)
		`, params.PaymentID, params.AccountID, params.PaymentType, params.Amount)

		if err != nil {
			return err
		}

		_, err = tx.Exec(ctx, `
			UPDATE payments
			SET status = $1
			WHERE id = $2
		`, params.Status, params.PaymentID)

		return tx.Commit(ctx)
	}

	return nil // unrecognized payment status
}
