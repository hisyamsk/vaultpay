package queue

import (
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/hisyamsk/vaultpay/internal/domain"
)

type PaymentEventMessage struct {
	EventID    uuid.UUID               `json:"event_id"`
	EventType  domain.PaymentEventType `json:"event_type"`
	PaymentID  uuid.UUID               `json:"payment_id"`
	Attempt    int                     `json:"attempt"`
	OccurredAt time.Time               `json:"occurred_at"`
}

func DecodePaymentEvent(body []byte) (PaymentEventMessage, error) {
	var event PaymentEventMessage
	err := json.Unmarshal(body, &event)
	if err != nil {
		return PaymentEventMessage{}, fmt.Errorf("decode payment event unmarshal body: %w", err)
	}

	if event.EventID == uuid.Nil {
		return PaymentEventMessage{}, errors.New("invalid event_id")
	}

	if event.PaymentID == uuid.Nil {
		return PaymentEventMessage{}, errors.New("invalid payment_id")
	}

	if !event.EventType.IsValid() {
		return PaymentEventMessage{}, errors.New("invalid event_type")
	}

	if event.Attempt < 1 {
		return PaymentEventMessage{}, errors.New("invalid attempt")
	}

	if event.OccurredAt.IsZero() {
		return PaymentEventMessage{}, errors.New("invalid occurred_at")
	}

	return event, nil
}
