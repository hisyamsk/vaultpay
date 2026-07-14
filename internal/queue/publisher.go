package queue

import (
	"context"
	"fmt"

	"github.com/hisyamsk/vaultpay/internal/domain"
	amqp "github.com/rabbitmq/amqp091-go"
)

type RabbitMQPublisher struct {
	channel *amqp.Channel
}

func NewRabbitMQPublisher(channel *amqp.Channel) (*RabbitMQPublisher, error) {
	if err := channel.Confirm(false); err != nil {
		return nil, fmt.Errorf("enable RabbitMQ publisher confirms: %w", err)
	}

	return &RabbitMQPublisher{
		channel: channel,
	}, nil
}

func (p *RabbitMQPublisher) Publish(ctx context.Context, event domain.PaymentEvent) error {
	confirmation, err := p.channel.PublishWithDeferredConfirmWithContext(
		ctx,
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

	confirmed, err := confirmation.WaitContext(ctx)
	if err != nil {
		return fmt.Errorf("wait for payment event confirmation: %w", err)
	}
	if !confirmed {
		return fmt.Errorf("publish payment event: RabbitMQ rejected message")
	}
	return nil
}
