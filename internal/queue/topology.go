package queue

import (
	"fmt"

	"github.com/hisyamsk/vaultpay/internal/domain"
	amqp "github.com/rabbitmq/amqp091-go"
)

const (
	PaymentEventsExchange = "vaultpay.payment.events"
	FraudQueue            = "vaultpay.fraud"
	ProcessorQueue        = "vaultpay.processor"
)

func DeclarePaymentEventsExchange(ch *amqp.Channel) error {
	err := ch.ExchangeDeclare(
		PaymentEventsExchange,
		"topic",
		true,
		false,
		false,
		false,
		nil,
	)
	if err != nil {
		return fmt.Errorf("declare payment events exchange: %w", err)
	}

	return nil
}

func DeclareFraudQueue(ch *amqp.Channel) error {
	q, err := ch.QueueDeclare(
		FraudQueue,
		true,
		false,
		false,
		false,
		nil,
	)

	if err != nil {
		return fmt.Errorf("declare fraud queue: %w", err)
	}

	err = ch.QueueBind(
		q.Name,
		string(domain.PaymentEventTypeCreated),
		PaymentEventsExchange,
		false,
		nil,
	)
	if err != nil {
		return fmt.Errorf("bind fraud queue to payment.created: %w", err)
	}

	return nil
}

func DeclareProcessorQueue(ch *amqp.Channel) error {
	q, err := ch.QueueDeclare(
		ProcessorQueue,
		true,
		false,
		false,
		false,
		nil,
	)

	if err != nil {
		return fmt.Errorf("declare processor queue: %w", err)
	}

	err = ch.QueueBind(
		q.Name,
		string(domain.PaymentEventTypeProcessing),
		PaymentEventsExchange,
		false,
		nil,
	)

	if err != nil {
		return fmt.Errorf("bind fraud queue to payment.created: %w", err)
	}
	return nil
}
