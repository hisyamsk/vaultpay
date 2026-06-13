package domain

import (
	"time"

	"github.com/google/uuid"
)

type PaymentStatus string

const (
	PaymentStatusPending    PaymentStatus = "pending"
	PaymentStatusProcessing PaymentStatus = "processing"
	PaymentStatusCompleted  PaymentStatus = "completed"
	PaymentStatusFailed     PaymentStatus = "failed"
	PaymentStatusRejected   PaymentStatus = "rejected"
)

type Payment struct {
	ID             uuid.UUID
	Amount         int64
	Currency       string
	SenderID       uuid.UUID
	ReceiverID     uuid.UUID
	IdempotencyKey string
	Status         PaymentStatus
	ErrorCode      *string
	Description    *string
	CreatedAt      time.Time
	UpdatedAt      time.Time
}
