package queue

import (
	"fmt"

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
