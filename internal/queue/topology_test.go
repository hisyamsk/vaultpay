package queue

import (
	"context"
	"errors"
	"testing"
	"time"

	amqp "github.com/rabbitmq/amqp091-go"
	"github.com/stretchr/testify/require"
)

const testRabbitMQURL = "amqp://vaultpay:vaultpay_dev@localhost:5672/"

func newTestRabbitMQChannel(t *testing.T) *amqp.Channel {
	t.Helper()

	conn, err := amqp.DialConfig(testRabbitMQURL, amqp.Config{
		Dial: amqp.DefaultDial(5 * time.Second),
	})
	require.NoError(t, err)
	t.Cleanup(func() {
		require.NoError(t, conn.Close())
	})

	ch, err := conn.Channel()
	require.NoError(t, err)
	t.Cleanup(func() {
		err := ch.Close()
		if !errors.Is(err, amqp.ErrClosed) {
			require.NoError(t, err)
		}
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

func TestDeclarePaymentRetryPathCanBeRepeated(t *testing.T) {
	ch := newTestRabbitMQChannel(t)
	require.NoError(t, DeclarePaymentEventsExchange(ch))

	require.NoError(t, DeclarePaymentRetryPath(ch))
	require.NoError(t, DeclarePaymentRetryPath(ch))

	err := ch.ExchangeDeclarePassive(
		PaymentRetryExchange,
		"topic",
		true,
		false,
		false,
		false,
		nil,
	)
	require.NoError(t, err)

	_, err = ch.QueueDeclarePassive(
		PaymentRetryQueue,
		true,
		false,
		false,
		false,
		amqp.Table{
			"x-message-ttl":          retryDelayMilliseconds,
			"x-dead-letter-exchange": PaymentEventsExchange,
		},
	)
	require.NoError(t, err)
}

func TestDeclarePaymentRetryPathReturnsMessagesToTheirWorkQueueAfterDelay(t *testing.T) {
	tests := []struct {
		name             string
		routingKey       string
		destinationQueue string
		otherQueue       string
	}{
		{
			name:             "created returns to fraud queue",
			routingKey:       "payment.created",
			destinationQueue: FraudQueue,
			otherQueue:       ProcessorQueue,
		},
		{
			name:             "processing returns to processor queue",
			routingKey:       "payment.processing",
			destinationQueue: ProcessorQueue,
			otherQueue:       FraudQueue,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ch := newTestRabbitMQChannel(t)
			require.NoError(t, DeclarePaymentEventsExchange(ch))
			require.NoError(t, DeclareFraudQueue(ch))
			require.NoError(t, DeclareProcessorQueue(ch))
			require.NoError(t, DeclarePaymentRetryPath(ch))

			for _, queueName := range []string{PaymentRetryQueue, FraudQueue, ProcessorQueue} {
				_, err := ch.QueuePurge(queueName, false)
				require.NoError(t, err)
			}
			t.Cleanup(func() {
				for _, queueName := range []string{PaymentRetryQueue, FraudQueue, ProcessorQueue} {
					_, err := ch.QueuePurge(queueName, false)
					require.NoError(t, err)
				}
			})

			body := []byte(`{"event_type":"` + tt.routingKey + `"}`)
			publishCtx, cancelPublish := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancelPublish()

			require.NoError(t, ch.Confirm(false))
			confirmation, err := ch.PublishWithDeferredConfirmWithContext(publishCtx, PaymentRetryExchange, tt.routingKey, false, false, amqp.Publishing{
				ContentType: "application/json",
				Body:        body,
			})
			require.NoError(t, err)
			require.NotNil(t, confirmation)

			confirmed, err := confirmation.WaitContext(publishCtx)
			require.NoError(t, err)
			require.True(t, confirmed)

			retryQueue, err := ch.QueueDeclarePassive(
				PaymentRetryQueue,
				true,
				false,
				false,
				false,
				amqp.Table{
					"x-message-ttl":          retryDelayMilliseconds,
					"x-dead-letter-exchange": PaymentEventsExchange,
				},
			)
			require.NoError(t, err)
			require.Equal(t, 1, retryQueue.Messages)

			consumeTimeout := time.Duration(retryDelayMilliseconds)*time.Millisecond + 5*time.Second
			consumeCtx, cancelConsume := context.WithTimeout(context.Background(), consumeTimeout)
			defer cancelConsume()

			deliveries, err := ch.ConsumeWithContext(
				consumeCtx,
				tt.destinationQueue,
				"",
				true,
				false,
				false,
				false,
				nil,
			)
			require.NoError(t, err)

			select {
			case delivery, ok := <-deliveries:
				require.True(t, ok)
				require.Equal(t, body, delivery.Body)
			case <-consumeCtx.Done():
				t.Fatalf("message was not returned to %s: %v", tt.destinationQueue, consumeCtx.Err())
			}

			otherQueue, err := ch.QueueDeclarePassive(
				tt.otherQueue,
				true,
				false,
				false,
				false,
				nil,
			)
			require.NoError(t, err)
			require.Zero(t, otherQueue.Messages)
		})
	}
}

func TestDeclarePaymentDLQCanBeRepeated(t *testing.T) {
	ch := newTestRabbitMQChannel(t)

	require.NoError(t, DeclarePaymentDLQ(ch))
	require.NoError(t, DeclarePaymentDLQ(ch))

	_, err := ch.QueueDeclarePassive(
		PaymentDLQ,
		true,
		false,
		false,
		false,
		nil,
	)
	require.NoError(t, err)
}

func TestDeclarePaymentDLQRetainsMessageUntilConsumed(t *testing.T) {
	ch := newTestRabbitMQChannel(t)
	require.NoError(t, DeclarePaymentDLQ(ch))

	_, err := ch.QueuePurge(PaymentDLQ, false)
	require.NoError(t, err)
	t.Cleanup(func() {
		_, err := ch.QueuePurge(PaymentDLQ, false)
		require.NoError(t, err)
	})

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	require.NoError(t, ch.Confirm(false))
	body := []byte(`{"reason":"malformed"}`)
	confirmation, err := ch.PublishWithDeferredConfirmWithContext(ctx, "", PaymentDLQ, false, false, amqp.Publishing{
		ContentType:  "application/json",
		DeliveryMode: amqp.Persistent,
		Body:         body,
	})
	require.NoError(t, err)
	require.NotNil(t, confirmation)

	confirmed, err := confirmation.WaitContext(ctx)
	require.NoError(t, err)
	require.True(t, confirmed)

	dlq, err := ch.QueueDeclarePassive(
		PaymentDLQ,
		true,
		false,
		false,
		false,
		nil,
	)
	require.NoError(t, err)
	require.Equal(t, 1, dlq.Messages)

	delivery, ok, err := ch.Get(PaymentDLQ, false)
	require.NoError(t, err)
	require.True(t, ok)
	require.Equal(t, body, delivery.Body)
	require.NoError(t, delivery.Ack(false))

	dlq, err = ch.QueueDeclarePassive(
		PaymentDLQ,
		true,
		false,
		false,
		false,
		nil,
	)
	require.NoError(t, err)
	require.Zero(t, dlq.Messages)
}

func TestDeclarePaymentTopologyCanBeRedeclaredFromNewConnectionWithoutLosingQueuedMessage(t *testing.T) {
	firstWorkerChannel := newTestRabbitMQChannel(t)

	// Remove one topology component so this test cannot pass only because a
	// previous test or worker already declared everything.
	require.NoError(t, DeclarePaymentDLQ(firstWorkerChannel))
	_, err := firstWorkerChannel.QueuePurge(PaymentDLQ, false)
	require.NoError(t, err)
	_, err = firstWorkerChannel.QueueDelete(PaymentDLQ, false, false, false)
	require.NoError(t, err)

	t.Cleanup(func() {
		require.NoError(t, DeclarePaymentDLQ(firstWorkerChannel))
		_, err := firstWorkerChannel.QueuePurge(PaymentDLQ, false)
		require.NoError(t, err)
	})

	require.NoError(t, DeclarePaymentTopology(firstWorkerChannel))

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	require.NoError(t, firstWorkerChannel.Confirm(false))
	body := []byte(`{"reason":"exhausted_retries"}`)
	confirmation, err := firstWorkerChannel.PublishWithDeferredConfirmWithContext(ctx, "", PaymentDLQ, false, false, amqp.Publishing{
		ContentType:  "application/json",
		DeliveryMode: amqp.Persistent,
		Body:         body,
	})
	require.NoError(t, err)
	require.NotNil(t, confirmation)

	confirmed, err := confirmation.WaitContext(ctx)
	require.NoError(t, err)
	require.True(t, confirmed)

	// A separate connection represents a newly started worker process.
	restartedWorkerChannel := newTestRabbitMQChannel(t)
	require.NoError(t, DeclarePaymentTopology(restartedWorkerChannel))

	delivery, ok, err := restartedWorkerChannel.Get(PaymentDLQ, false)
	require.NoError(t, err)
	require.True(t, ok)
	require.Equal(t, body, delivery.Body)
	require.NoError(t, delivery.Ack(false))
}
