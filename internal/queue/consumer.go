package queue

import (
	"context"

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
