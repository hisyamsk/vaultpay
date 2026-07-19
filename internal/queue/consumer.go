package queue

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/hisyamsk/vaultpay/internal/domain"
	amqp "github.com/rabbitmq/amqp091-go"
)

type consumerChannel interface {
	Qos(prefetchCount, prefetchSize int, global bool) error

	ConsumeWithContext(
		ctx context.Context,
		queue, consumer string,
		autoAck, exclusive, noLocal, noWait bool,
		args amqp.Table,
	) (<-chan amqp.Delivery, error)
}

type failurePublisher interface {
	PublishDeadLetter(ctx context.Context, body []byte) error
	PublishRetry(ctx context.Context, event PaymentEventMessage) error
}

var ErrInvalidConsumerMessage = errors.New("invalid consumer message")

type eventHandler interface {
	HandleEvent(ctx context.Context, event PaymentEventMessage) error
}

type ConsumerConfig struct {
	Name          string
	Queue         string
	EventType     domain.PaymentEventType
	PrefetchCount int
	MaxAttempts   int
}

type RabbitMQConsumer struct {
	handler          eventHandler
	channel          consumerChannel
	failurePublisher failurePublisher
	config           ConsumerConfig
}

func NewRabbitMQConsumer(handler eventHandler, channel consumerChannel, failurePublisher failurePublisher, config ConsumerConfig) (*RabbitMQConsumer, error) {
	config.Name = strings.TrimSpace(config.Name)
	if config.Name == "" {
		return nil, errors.New("consumer name must not be empty")
	}
	if config.Queue == "" {
		return nil, errors.New("consumer queue must not be empty")
	}
	if !config.EventType.IsValid() {
		return nil, errors.New("consumer event type must be valid")
	}
	if config.PrefetchCount <= 0 {
		return nil, errors.New("prefetch count must be greater than zero")
	}
	if config.MaxAttempts <= 0 {
		return nil, errors.New("maximum attempts must be greater than zero")
	}

	return &RabbitMQConsumer{
		handler:          handler,
		channel:          channel,
		failurePublisher: failurePublisher,
		config:           config,
	}, nil
}

func (c *RabbitMQConsumer) HandleDelivery(ctx context.Context, body []byte) error {
	payload, err := DecodePaymentEvent(body)
	if err != nil {
		return fmt.Errorf("%w: %v", ErrInvalidConsumerMessage, err)
	}

	if payload.EventType != c.config.EventType {
		return fmt.Errorf(
			"%w: unexpected event type %q",
			ErrInvalidConsumerMessage,
			payload.EventType,
		)
	}

	if err := c.handler.HandleEvent(ctx, payload); err != nil {
		return fmt.Errorf("handle %s event: %w", c.config.Name, err)
	}

	return nil
}

func (c *RabbitMQConsumer) Consume(ctx context.Context) error {
	if err := c.channel.Qos(c.config.PrefetchCount, 0, false); err != nil {
		return fmt.Errorf("%s consumer set QoS: %w", c.config.Name, err)
	}

	deliveries, err := c.channel.ConsumeWithContext(
		ctx,
		c.config.Queue,
		"",
		false,
		false,
		false,
		false,
		nil,
	)
	if err != nil {
		return fmt.Errorf("%s consume with context: %w", c.config.Name, err)
	}

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()

		case delivery, ok := <-deliveries:
			if !ok {
				return fmt.Errorf("%s delivery channel closed", c.config.Name)
			}

			if err := c.HandleDelivery(ctx, delivery.Body); err != nil {
				if !errors.Is(err, ErrInvalidConsumerMessage) {
					event, decodeErr := DecodePaymentEvent(delivery.Body)
					if decodeErr != nil {
						return fmt.Errorf("decode transient %s delivery: %w", c.config.Name, decodeErr)
					}

					if event.Attempt >= c.config.MaxAttempts {
						if exhaustedErr := c.deadLetterExhaustedDelivery(ctx, delivery); exhaustedErr != nil {
							return fmt.Errorf("dead-letter exhausted %s delivery: %w", c.config.Name, exhaustedErr)
						}
						continue
					}

					if retryErr := c.retryTransientDelivery(ctx, delivery, event); retryErr != nil {
						return fmt.Errorf("retry transient %s delivery: %w", c.config.Name, retryErr)
					}
					continue
				}

				if publishErr := c.failurePublisher.PublishDeadLetter(ctx, delivery.Body); publishErr != nil {
					return fmt.Errorf("publish invalid %s message to dead-letter queue: %w", c.config.Name, publishErr)
				}

				if ackErr := delivery.Ack(false); ackErr != nil {
					return fmt.Errorf("acknowledge dead-lettered %s message: %w", c.config.Name, ackErr)
				}

				continue
			}

			if err := delivery.Ack(false); err != nil {
				return fmt.Errorf("acknowledge %s delivery: %w", c.config.Name, err)
			}
		}
	}
}

func (c *RabbitMQConsumer) deadLetterExhaustedDelivery(ctx context.Context, delivery amqp.Delivery) error {
	if err := c.failurePublisher.PublishDeadLetter(ctx, delivery.Body); err != nil {
		return fmt.Errorf("exhaust delivery publish dead letter: %w", err)
	}
	if err := delivery.Ack(false); err != nil {
		return fmt.Errorf("acknowledge exhausted delivery: %w", err)
	}
	return nil
}

func (c *RabbitMQConsumer) retryTransientDelivery(ctx context.Context, delivery amqp.Delivery, event PaymentEventMessage) error {
	if err := c.failurePublisher.PublishRetry(ctx, event); err != nil {
		return fmt.Errorf("publish retry %s message to retry queue: %w", c.config.Name, err)
	}

	if err := delivery.Ack(false); err != nil {
		return fmt.Errorf("acknowledge retry %s message: %w", c.config.Name, err)
	}
	return nil
}
