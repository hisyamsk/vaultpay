package repository

import (
	"errors"

	"github.com/google/uuid"
	"github.com/hisyamsk/vaultpay/internal/domain"
	"github.com/jackc/pgx/v5/pgxpool"
)

type PaymentRepository struct {
	db *pgxpool.Pool
}

type CreatePaymentParams struct {
	Amount         int64
	SenderID       uuid.UUID
	ReceiverID     uuid.UUID
	IdempotencyKey string
	Description    *string
}

type ProcessPaymentParams struct {
	PaymentID   uuid.UUID
	AccountID   uuid.UUID
	Amount      int64
	Status      domain.PaymentStatus
	PaymentType domain.LedgerEntryType
}

var ErrDuplicateIdempotencyKey = errors.New("duplicate idempotency key")
var ErrPaymentNotFound = errors.New("payment not found")
var ErrPaymentStatusConflict = errors.New("payment status conflict")
var ErrInsufficientBalance = errors.New("insufficient balance")
