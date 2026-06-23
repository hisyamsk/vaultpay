package repository

import (
	"errors"

	"github.com/google/uuid"
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

var ErrDuplicateIdempotencyKey = errors.New("duplicate idempotency key")
var ErrPaymentNotFound = errors.New("payment not found")
var ErrAccountNotFound = errors.New("account not found")
var ErrPaymentStatusConflict = errors.New("payment status conflict")
var ErrInsufficientBalance = errors.New("insufficient balance")
var ErrUnrecognizedPaymentStatus = errors.New("unrecognized payment status")
