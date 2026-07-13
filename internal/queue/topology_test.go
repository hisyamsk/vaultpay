package queue

import (
	"context"
	"os"
	"testing"
	"time"

	amqp "github.com/rabbitmq/amqp091-go"
	"github.com/stretchr/testify/require"
)

func newTestRabbitMQChannel(t *testing.T) *amqp.Channel {
	t.Helper()

	rabbitMQURL := os.Getenv("RABBITMQ_URL")
	if rabbitMQURL == "" {
		t.Skip("RABBITMQ_URL is not set; skipping RabbitMQ integration test")
	}

	conn, err := amqp.DialConfig(rabbitMQURL, amqp.Config{
		Dial: amqp.DefaultDial(5 * time.Second),
	})
	require.NoError(t, err)
	t.Cleanup(func() {
		require.NoError(t, conn.Close())
	})

	ch, err := conn.Channel()
	require.NoError(t, err)
	t.Cleanup(func() {
		require.NoError(t, ch.Close())
	})

	return ch
}

func TestDeclarePaymentEventsExchangeCanBeRepeated(t *testing.T) {
	ch := newTestRabbitMQChannel(t)

	require.NoError(t, DeclarePaymentEventsExchange(ch))
	require.NoError(t, DeclarePaymentEventsExchange(ch))
}

func TestDeclareFraudQueueCanBeRepeated(t *testing.T) {
	ch := newTestRabbitMQChannel(t)
	require.NoError(t, DeclarePaymentEventsExchange(ch))

	require.NoError(t, DeclareFraudQueue(ch))
	require.NoError(t, DeclareFraudQueue(ch))

	_, err := ch.QueueDeclarePassive(
		FraudQueue,
		true,
		false,
		false,
		false,
		nil,
	)
	require.NoError(t, err)
}

func TestDeclareFraudQueueRoutesOnlyPaymentCreated(t *testing.T) {
	ch := newTestRabbitMQChannel(t)
	require.NoError(t, DeclarePaymentEventsExchange(ch))
	require.NoError(t, DeclareFraudQueue(ch))

	_, err := ch.QueuePurge(FraudQueue, false)
	require.NoError(t, err)
	t.Cleanup(func() {
		_, err := ch.QueuePurge(FraudQueue, false)
		require.NoError(t, err)
	})

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	createdBody := []byte(`{"event_type":"payment.created"}`)
	err = ch.PublishWithContext(ctx, PaymentEventsExchange, "payment.created", false, false, amqp.Publishing{
		ContentType: "application/json",
		Body:        createdBody,
	})
	require.NoError(t, err)

	delivery, ok, err := ch.Get(FraudQueue, true)
	require.NoError(t, err)
	require.True(t, ok)
	require.Equal(t, createdBody, delivery.Body)

	err = ch.PublishWithContext(ctx, PaymentEventsExchange, "payment.processing", false, false, amqp.Publishing{
		ContentType: "application/json",
		Body:        []byte(`{"event_type":"payment.processing"}`),
	})
	require.NoError(t, err)

	_, ok, err = ch.Get(FraudQueue, true)
	require.NoError(t, err)
	require.False(t, ok)
}

func TestDeclareProcessorQueueCanBeRepeated(t *testing.T) {
	ch := newTestRabbitMQChannel(t)
	require.NoError(t, DeclarePaymentEventsExchange(ch))

	require.NoError(t, DeclareProcessorQueue(ch))
	require.NoError(t, DeclareProcessorQueue(ch))

	_, err := ch.QueueDeclarePassive(
		ProcessorQueue,
		true,
		false,
		false,
		false,
		nil,
	)
	require.NoError(t, err)
}

func TestDeclareProcessorQueueRoutesOnlyPaymentProcessing(t *testing.T) {
	ch := newTestRabbitMQChannel(t)
	require.NoError(t, DeclarePaymentEventsExchange(ch))
	require.NoError(t, DeclareProcessorQueue(ch))

	_, err := ch.QueuePurge(ProcessorQueue, false)
	require.NoError(t, err)
	t.Cleanup(func() {
		_, err := ch.QueuePurge(ProcessorQueue, false)
		require.NoError(t, err)
	})

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	processingBody := []byte(`{"event_type":"payment.processing"}`)
	err = ch.PublishWithContext(ctx, PaymentEventsExchange, "payment.processing", false, false, amqp.Publishing{
		ContentType: "application/json",
		Body:        processingBody,
	})
	require.NoError(t, err)

	delivery, ok, err := ch.Get(ProcessorQueue, true)
	require.NoError(t, err)
	require.True(t, ok)
	require.Equal(t, processingBody, delivery.Body)

	err = ch.PublishWithContext(ctx, PaymentEventsExchange, "payment.created", false, false, amqp.Publishing{
		ContentType: "application/json",
		Body:        []byte(`{"event_type":"payment.created"}`),
	})
	require.NoError(t, err)

	_, ok, err = ch.Get(ProcessorQueue, true)
	require.NoError(t, err)
	require.False(t, ok)
}
