package queue

import (
	"fmt"

	amqp "github.com/rabbitmq/amqp091-go"
)

const PaymentEventsExchange = "vaultpay.payment.events"

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
