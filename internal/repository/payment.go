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

type PaymentRepository struct {
	db *pgxpool.Pool
}

type CreatePaymentParams struct {
	Amount         int64
	Currency       string
	SenderID       uuid.UUID
	ReceiverID     uuid.UUID
	IdempotencyKey string
	Description    *string
}

var ErrDuplicateIdempotencyKey = errors.New("duplicate idempotency key")
var ErrPaymentNotFound = errors.New("payment not found")

var pgErr *pgconn.PgError

func NewPaymentRepository(db *pgxpool.Pool) *PaymentRepository {
	return &PaymentRepository{
		db: db,
	}
}

func (r *PaymentRepository) Create(ctx context.Context, params CreatePaymentParams) (*domain.Payment, error) {
	payment := &domain.Payment{}
	err := r.db.QueryRow(ctx, `
		INSERT INTO payments (amount, currency, sender_id, receiver_id, idempotency_key, description)
		VALUES ($1, $2, $3, $4, $5, $6)
		RETURNING id, amount, currency, sender_id, receiver_id, idempotency_key, status, error_code, description, created_at, updated_at
	`, params.Amount, params.Currency, params.SenderID, params.ReceiverID, params.IdempotencyKey, params.Description).Scan(
		&payment.ID, &payment.Amount, &payment.Currency, &payment.SenderID, &payment.ReceiverID, &payment.IdempotencyKey, &payment.Status,
		&payment.ErrorCode, &payment.Description, &payment.CreatedAt, &payment.UpdatedAt,
	)

	if err != nil {
		if errors.As(err, &pgErr) {
			if pgErr.Code == "23505" && pgErr.ConstraintName == "payments_idempotency_key_key" {
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
		SELECT id, amount, currency, sender_id, receiver_id, idempotency_key, status, error_code, description, created_at, updated_at
		FROM payments
		WHERE idempotency_key = $1`, idempotencyKey).Scan(
		&payment.ID, &payment.Amount, &payment.Currency, &payment.SenderID, &payment.ReceiverID, &payment.IdempotencyKey, &payment.Status,
		&payment.ErrorCode, &payment.Description, &payment.CreatedAt, &payment.UpdatedAt,
	)
	if err != nil {
		if errors.As(err, pgx.ErrNoRows) {
			return nil, ErrPaymentNotFound
		}
		return nil, err
	}

	return payment, nil
}
