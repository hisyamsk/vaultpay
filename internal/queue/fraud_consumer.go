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
	handler       fraudEventHandler
	channel       consumerChannel
	prefetchCount int
}

func NewFraudConsumer(handler fraudEventHandler, channel consumerChannel, prefetchCount int) (*FraudConsumer, error) {
	if prefetchCount <= 0 {
		return nil, errors.New("prefetch count must be greater than zero")
	}

	return &FraudConsumer{
		channel:       channel,
		handler:       handler,
		prefetchCount: prefetchCount,
	}, nil
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

func (c *FraudConsumer) Consume(ctx context.Context) error {
	err := c.channel.Qos(c.prefetchCount, 0, false)
	if err != nil {
		return fmt.Errorf("fraud consumer set Qos: %w", err)
	}

	deliveries, err := c.channel.ConsumeWithContext(
		ctx,
		FraudQueue,
		"",
		false,
		false,
		false,
		false,
		nil,
	)

	if err != nil {
		return fmt.Errorf("fraud consume with context: %w", err)
	}

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()

		case delivery, ok := <-deliveries:
			if !ok {
				return errors.New("fraud delivery channel closed")
			}

			if err := c.HandleDelivery(ctx, delivery.Body); err != nil {
				return fmt.Errorf("handle fraud delivery: %w", err)
			}

			if err := delivery.Ack(false); err != nil {
				return fmt.Errorf("acknowledge fraud delivery: %w", err)
			}
		}
	}
}
