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
	ID             uuid.UUID
	Amount         int64
	SenderID       uuid.UUID
	ReceiverID     uuid.UUID
	IdempotencyKey string
	Status         PaymentStatus
	ErrorCode      *string
	Description    *string
	CreatedAt      time.Time
	UpdatedAt      time.Time
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
