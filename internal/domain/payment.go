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
	Currency       Currency
	SenderID       uuid.UUID
	ReceiverID     uuid.UUID
	IdempotencyKey string
	Status         PaymentStatus
	ErrorCode      *string
	Description    *string
	CreatedAt      time.Time
	UpdatedAt      time.Time
}

type Currency string

const (
	CurrencyIDR Currency = "IDR"
	CurrencyUSD Currency = "USD"
	CurrencySGD Currency = "SGD"
)

func (c Currency) Valid() bool {
	switch c {
	case CurrencyIDR, CurrencyUSD, CurrencySGD:
		return true
	default:
		return false
	}
}
