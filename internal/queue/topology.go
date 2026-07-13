package queue

import (
	"fmt"

	"github.com/hisyamsk/vaultpay/internal/domain"
	amqp "github.com/rabbitmq/amqp091-go"
)

const (
	PaymentEventsExchange = "vaultpay.payment.events"
	PaymentRetryExchange  = "vaultpay.payment.retry"
	PaymentDLQ            = "vaultpay.payment.dlq"

	FraudQueue        = "vaultpay.fraud"
	ProcessorQueue    = "vaultpay.processor"
	PaymentRetryQueue = "vaultpay.payment.retry.wait"

	retryDelayMilliseconds int32 = 5000
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
		return fmt.Errorf("bind processor queue to payment.processing: %w", err)
	}
	return nil
}

func DeclarePaymentRetryPath(ch *amqp.Channel) error {
	err := ch.ExchangeDeclare(
		PaymentRetryExchange,
		"topic",
		true,
		false,
		false,
		false,
		nil,
	)

	if err != nil {
		return fmt.Errorf("declare payment retry path exchange: %w", err)
	}

	args := amqp.Table{
		"x-message-ttl":          retryDelayMilliseconds,
		"x-dead-letter-exchange": PaymentEventsExchange,
	}

	q, err := ch.QueueDeclare(
		PaymentRetryQueue,
		true,
		false,
		false,
		false,
		args,
	)

	if err != nil {
		return fmt.Errorf("declare payment retry path queue: %w", err)
	}

	err = ch.QueueBind(
		q.Name,
		string(domain.PaymentEventTypeCreated),
		PaymentRetryExchange,
		false,
		nil,
	)
	if err != nil {
		return fmt.Errorf("bind payment retry path queue to payment.created: %w", err)
	}

	err = ch.QueueBind(
		q.Name,
		string(domain.PaymentEventTypeProcessing),
		PaymentRetryExchange,
		false,
		nil,
	)
	if err != nil {
		return fmt.Errorf("bind payment retry path queue to payment.processing: %w", err)
	}

	return nil
}

func DeclarePaymentDLQ(ch *amqp.Channel) error {
	_, err := ch.QueueDeclare(
		PaymentDLQ,
		true,
		false,
		false,
		false,
		nil,
	)

	if err != nil {
		return fmt.Errorf("declare payment DLQ: %w", err)
	}
	return nil
}

func DeclarePaymentTopology(ch *amqp.Channel) error {
	if err := DeclarePaymentEventsExchange(ch); err != nil {
		return fmt.Errorf("declare payment topology: %w", err)
	}

	if err := DeclareFraudQueue(ch); err != nil {
		return fmt.Errorf("declare payment topology: %w", err)
	}

	if err := DeclareProcessorQueue(ch); err != nil {
		return fmt.Errorf("declare payment topology: %w", err)
	}

	if err := DeclarePaymentRetryPath(ch); err != nil {
		return fmt.Errorf("declare payment topology: %w", err)
	}

	if err := DeclarePaymentDLQ(ch); err != nil {
		return fmt.Errorf("declare payment topology: %w", err)
	}

	return nil
}
