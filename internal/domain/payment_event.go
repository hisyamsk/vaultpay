package domain

import (
	"encoding/json"
	"time"

	"github.com/google/uuid"
)

type PaymentEventType string

const (
	PaymentEventTypeCreated    PaymentEventType = "payment.created"
	PaymentEventTypeProcessing PaymentEventType = "payment.processing"
	PaymentEventTypeCompleted  PaymentEventType = "payment.completed"
	PaymentEventTypeFailed     PaymentEventType = "payment.failed"
	PaymentEventTypeRejected   PaymentEventType = "payment.rejected"
)

type PaymentEvent struct {
	ID              int64
	EventID         uuid.UUID
	PaymentID       uuid.UUID
	EventType       PaymentEventType
	Payload         json.RawMessage
	CreatedAt       time.Time
	PublishAttempts int
	PublishedAt     *time.Time
	LastAttemptedAt *time.Time
	LastError       *string
}
