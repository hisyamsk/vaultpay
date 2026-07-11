package queue

import "github.com/google/uuid"

type PaymentMessage struct {
	PaymentID uuid.UUID `json:"payment_id"`
	Attempt   int       `json:"attempt"`
}
