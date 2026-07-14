package queue

import (
	"context"
	"fmt"
	"time"

	"github.com/hisyamsk/vaultpay/internal/domain"
	amqp "github.com/rabbitmq/amqp091-go"
)

type RabbitMQPublisher struct {
	channel        *amqp.Channel
	publishTimeout time.Duration
}

func NewRabbitMQPublisher(channel *amqp.Channel, publishTimeout time.Duration) (*RabbitMQPublisher, error) {
	if publishTimeout <= 0 {
		return nil, fmt.Errorf("RabbitMQ publish timeout must be positive")
	}
	if err := channel.Confirm(false); err != nil {
		return nil, fmt.Errorf("enable RabbitMQ publisher confirms: %w", err)
	}

	return &RabbitMQPublisher{
		channel:        channel,
		publishTimeout: publishTimeout,
	}, nil
}

// Publish sends the stored event payload and returns nil only after RabbitMQ
// positively confirms it. The configured publish timeout bounds both sending
// the message and waiting for its confirmation. An earlier cancellation or
// deadline on ctx still takes precedence.
func (p *RabbitMQPublisher) Publish(ctx context.Context, event domain.PaymentEvent) error {
	publishCtx, cancel := context.WithTimeout(ctx, p.publishTimeout)
	defer cancel()

	confirmation, err := p.channel.PublishWithDeferredConfirmWithContext(
		publishCtx,
		PaymentEventsExchange,
		string(event.EventType),
		false,
		false,
		amqp.Publishing{
			DeliveryMode: amqp.Persistent,
			ContentType:  "application/json",
			MessageId:    event.EventID.String(),
			Body:         event.Payload,
		},
	)
	if err != nil {
		return fmt.Errorf("publish payment event: %w", err)
	}

	if confirmation == nil {
		return fmt.Errorf("publish payment event: confirmation unavailable")
	}

	confirmed, err := confirmation.WaitContext(publishCtx)
	if err != nil {
		return fmt.Errorf("wait for payment event confirmation: %w", err)
	}
	if !confirmed {
		return fmt.Errorf("publish payment event: RabbitMQ rejected message")
	}
	return nil
}
