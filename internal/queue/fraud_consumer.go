package queue

import (
	"context"
	"errors"
	"fmt"

	"github.com/hisyamsk/vaultpay/internal/domain"
)

var ErrInvalidFraudMessage = errors.New("invalid fraud message")

type fraudEventHandler interface {
	HandleEvent(ctx context.Context, event PaymentEventMessage) error
}

type FraudConsumer struct {
	handler fraudEventHandler
}

func NewFraudConsumer(handler fraudEventHandler) *FraudConsumer {
	return &FraudConsumer{handler: handler}
}

func (c *FraudConsumer) HandleDelivery(ctx context.Context, body []byte) error {
	payload, err := DecodePaymentEvent(body)
	if err != nil {
		return fmt.Errorf("%w: %v", ErrInvalidFraudMessage, err)
	}

	if payload.EventType != domain.PaymentEventTypeCreated {
		return fmt.Errorf(
			"%w: unexpected event type %q",
			ErrInvalidFraudMessage,
			payload.EventType,
		)
	}

	if err := c.handler.HandleEvent(ctx, payload); err != nil {
		return fmt.Errorf("handle fraud event: %w", err)
	}

	return nil
}
