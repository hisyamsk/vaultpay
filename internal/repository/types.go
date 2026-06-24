package repository

import (
	"context"
	"errors"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

type PaymentRepository struct {
	db dbtx
}

type dbtx interface {
	Begin(ctx context.Context) (pgx.Tx, error)
	Exec(ctx context.Context, sql string, arguments ...any) (pgconn.CommandTag, error)
	QueryRow(ctx context.Context, sql string, arguments ...any) pgx.Row
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
var ErrInvalidStatusTransition = errors.New("invalid status transition")
