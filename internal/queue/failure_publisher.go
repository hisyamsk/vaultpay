package queue

import (
	"context"
	"fmt"
	"time"

	amqp "github.com/rabbitmq/amqp091-go"
)

type RabbitMQFailurePublisher struct {
	channel        *amqp.Channel
	publishTimeout time.Duration
}

func NewRabbitMQFailurePublisher(channel *amqp.Channel, publishTimeout time.Duration) (*RabbitMQFailurePublisher, error) {
	if publishTimeout <= 0 {
		return nil, fmt.Errorf("RabbitMQ publish timeout must be positive")
	}
	if err := channel.Confirm(false); err != nil {
		return nil, fmt.Errorf("enable RabbitMQ failure publisher confirms: %w", err)
	}

	return &RabbitMQFailurePublisher{
		channel:        channel,
		publishTimeout: publishTimeout,
	}, nil
}

func (p *RabbitMQFailurePublisher) PublishDeadLetter(ctx context.Context, body []byte) error {
	publishCtx, cancel := context.WithTimeout(ctx, p.publishTimeout)
	defer cancel()

	confirmation, err := p.channel.PublishWithDeferredConfirmWithContext(
		publishCtx,
		"",
		PaymentDeadLetterQueue,
		false,
		false,
		amqp.Publishing{
			DeliveryMode: amqp.Persistent,
			ContentType:  "application/json",
			Body:         body,
		},
	)

	if err != nil {
		return fmt.Errorf("publish payment dlq: %w", err)
	}

	if confirmation == nil {
		return fmt.Errorf("publish payment dlq: confirmation unavailable")
	}

	confirmed, err := confirmation.WaitContext(publishCtx)
	if err != nil {
		return fmt.Errorf("wait for payment dlq confirmation: %w", err)
	}
	if !confirmed {
		return fmt.Errorf("publish payment dlq: RabbitMQ rejected message")
	}
	return nil
}

func (p *RabbitMQFailurePublisher) PublishRetry(ctx context.Context, event PaymentEventMessage) error {
	return nil
}
