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
	ID              int64            `db:"id"`
	EventID         uuid.UUID        `db:"event_id"`
	PaymentID       uuid.UUID        `db:"payment_id"`
	EventType       PaymentEventType `db:"event_type"`
	Payload         json.RawMessage  `db:"payload"`
	CreatedAt       time.Time        `db:"created_at"`
	PublishAttempts int              `db:"publish_attempts"`
	PublishedAt     *time.Time       `db:"published_at"`
	LastAttemptedAt *time.Time       `db:"last_attempted_at"`
	LastError       *string          `db:"last_error"`
}

type PaymentEventPayload struct {
	EventID    uuid.UUID        `db:"event_id" json:"event_id"`
	EventType  PaymentEventType `db:"event_type" json:"event_type"`
	PaymentID  uuid.UUID        `db:"payment_id" json:"payment_id"`
	Attempt    int              `db:"attempt" json:"attempt"`
	OccurredAt time.Time        `db:"occurred_at" json:"occurred_at"`
}
