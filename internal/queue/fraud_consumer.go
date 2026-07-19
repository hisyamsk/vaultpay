package queue

import (
	"context"
	"errors"
	"fmt"

	"github.com/hisyamsk/vaultpay/internal/domain"
	amqp "github.com/rabbitmq/amqp091-go"
)

var ErrInvalidFraudMessage = errors.New("invalid fraud message")

type fraudEventHandler interface {
	HandleEvent(ctx context.Context, event PaymentEventMessage) error
}

type FraudConsumer struct {
	handler          fraudEventHandler
	channel          consumerChannel
	prefetchCount    int
	maxAttempts      int
	failurePublisher failurePublisher
}

func NewFraudConsumer(handler fraudEventHandler, channel consumerChannel, prefetchCount, maxAttempts int, failurePublisher failurePublisher) (*FraudConsumer, error) {
	if prefetchCount <= 0 {
		return nil, errors.New("prefetch count must be greater than zero")
	}
	if maxAttempts <= 0 {
		return nil, errors.New("maximum attempts must be greater than zero")
	}

	return &FraudConsumer{
		channel:          channel,
		handler:          handler,
		prefetchCount:    prefetchCount,
		maxAttempts:      maxAttempts,
		failurePublisher: failurePublisher,
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
				if !errors.Is(err, ErrInvalidFraudMessage) {
					event, decodeErr := DecodePaymentEvent(delivery.Body)
					if decodeErr != nil {
						return fmt.Errorf("decode transient fraud delivery: %w", decodeErr)
					}

					if event.Attempt >= c.maxAttempts {
						if exhaustedErr := c.deadLetterExhaustedDelivery(ctx, delivery); exhaustedErr != nil {
							return fmt.Errorf("dead-letter exhausted fraud delivery: %w", exhaustedErr)
						}
						continue
					}

					if retryErr := c.retryTransientDelivery(ctx, delivery, event); retryErr != nil {
						return fmt.Errorf("retry transient fraud delivery: %w", retryErr)
					}
					continue
				}

				if publishErr := c.failurePublisher.PublishDeadLetter(
					ctx,
					delivery.Body,
				); publishErr != nil {
					return fmt.Errorf(
						"publish invalid fraud message to dead-letter queue: %w",
						publishErr,
					)
				}

				if ackErr := delivery.Ack(false); ackErr != nil {
					return fmt.Errorf(
						"acknowledge dead-lettered fraud message: %w",
						ackErr,
					)
				}

				continue
			}

			if err := delivery.Ack(false); err != nil {
				return fmt.Errorf("acknowledge fraud delivery: %w", err)
			}
		}
	}
}

func (c *FraudConsumer) deadLetterExhaustedDelivery(ctx context.Context, delivery amqp.Delivery) error {
	if err := c.failurePublisher.PublishDeadLetter(ctx, delivery.Body); err != nil {
		return fmt.Errorf("exhaust delivery publish dead letter: %w", err)
	}
	if err := delivery.Ack(false); err != nil {
		return fmt.Errorf(
			"acknowledge exhausted delivery: %w",
			err,
		)
	}
	return nil
}

func (c *FraudConsumer) retryTransientDelivery(ctx context.Context, delivery amqp.Delivery, event PaymentEventMessage) error {
	if err := c.failurePublisher.PublishRetry(ctx, event); err != nil {
		return fmt.Errorf(
			"publish retry fraud message to retry queue: %w",
			err,
		)
	}

	if err := delivery.Ack(false); err != nil {
		return fmt.Errorf(
			"acknowledge retry fraud message: %w",
			err,
		)
	}
	return nil
}
