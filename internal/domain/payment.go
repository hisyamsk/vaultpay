package domain

import (
	"time"

	"github.com/google/uuid"
)

type PaymentStatus string
type ErrorCode string

const (
	PaymentStatusPending    PaymentStatus = "pending"
	PaymentStatusProcessing PaymentStatus = "processing"
	PaymentStatusCompleted  PaymentStatus = "completed"
	PaymentStatusFailed     PaymentStatus = "failed"
	PaymentStatusRejected   PaymentStatus = "rejected"

	ErrorCodeInsufficientFunds ErrorCode = "insufficient_funds"
)

type Payment struct {
	ID             uuid.UUID     `db:"id"`
	Amount         int64         `db:"amount"`
	SenderID       uuid.UUID     `db:"sender_id"`
	ReceiverID     uuid.UUID     `db:"receiver_id"`
	IdempotencyKey string        `db:"idempotency_key"`
	Status         PaymentStatus `db:"status"`
	ErrorCode      *string       `db:"error_code"`
	Description    *string       `db:"description"`
	CreatedAt      time.Time     `db:"created_at"`
	UpdatedAt      time.Time     `db:"updated_at"`
}

func (s PaymentStatus) CanTransitionTo(next PaymentStatus) bool {
	switch s {
	case PaymentStatusPending:
		return next == PaymentStatusProcessing || next == PaymentStatusRejected
	case PaymentStatusProcessing:
		return next == PaymentStatusCompleted || next == PaymentStatusFailed
	case PaymentStatusCompleted, PaymentStatusFailed, PaymentStatusRejected:
		return false
	default:
		return false
	}
}
