package queue

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/hisyamsk/vaultpay/internal/domain"
	amqp "github.com/rabbitmq/amqp091-go"
	"github.com/stretchr/testify/require"
)

func TestRabbitMQFailurePublisherPublishRetryIncrementsAttemptAndPublishesConfirmedPersistentEvent(t *testing.T) {
	tests := []struct {
		name      string
		eventType domain.PaymentEventType
	}{
		{name: "fraud retry", eventType: domain.PaymentEventTypeCreated},
		{name: "processor retry", eventType: domain.PaymentEventTypeProcessing},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ch := newTestRabbitMQChannel(t)
			require.NoError(t, DeclarePaymentRetryPath(ch))
			purgeQueue(t, ch, PaymentRetryQueue)

			publisher, err := NewRabbitMQFailurePublisher(ch, testPublishTimeout)
			require.NoError(t, err)
			event := PaymentEventMessage{
				EventID:    uuid.MustParse("11111111-1111-1111-1111-111111111111"),
				EventType:  tt.eventType,
				PaymentID:  uuid.MustParse("22222222-2222-2222-2222-222222222222"),
				Attempt:    2,
				OccurredAt: time.Date(2026, time.July, 18, 10, 30, 0, 0, time.UTC),
			}

			err = publisher.PublishRetry(context.Background(), event)

			require.NoError(t, err)
			delivery, ok, err := ch.Get(PaymentRetryQueue, false)
			require.NoError(t, err)
			require.True(t, ok, "PublishRetry returned success but no message reached the retry queue")
			require.Equal(t, PaymentRetryExchange, delivery.Exchange)
			require.Equal(t, string(event.EventType), delivery.RoutingKey)
			require.Equal(t, "application/json", delivery.ContentType)
			require.Equal(t, event.EventID.String(), delivery.MessageId)
			require.Equal(t, uint8(amqp.Persistent), delivery.DeliveryMode)

			retried, err := DecodePaymentEvent(delivery.Body)
			require.NoError(t, err)
			require.Equal(t, event.EventID, retried.EventID)
			require.Equal(t, event.EventType, retried.EventType)
			require.Equal(t, event.PaymentID, retried.PaymentID)
			require.Equal(t, event.Attempt+1, retried.Attempt)
			require.Equal(t, event.OccurredAt, retried.OccurredAt)
			require.NoError(t, delivery.Ack(false))
		})
	}
}

func TestRabbitMQFailurePublisherPublishRetryDoesNotPublishWithCanceledContext(t *testing.T) {
	ch := newTestRabbitMQChannel(t)
	require.NoError(t, DeclarePaymentRetryPath(ch))
	purgeQueue(t, ch, PaymentRetryQueue)

	publisher, err := NewRabbitMQFailurePublisher(ch, testPublishTimeout)
	require.NoError(t, err)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	err = publisher.PublishRetry(ctx, validRetryEvent())

	require.ErrorIs(t, err, context.Canceled)
	_, ok, err := ch.Get(PaymentRetryQueue, true)
	require.NoError(t, err)
	require.False(t, ok, "a canceled retry publish must not enqueue a message")
}

func TestRabbitMQFailurePublisherPublishRetryReturnsClosedChannelError(t *testing.T) {
	ch := newTestRabbitMQChannel(t)
	publisher, err := NewRabbitMQFailurePublisher(ch, testPublishTimeout)
	require.NoError(t, err)
	require.NoError(t, ch.Close())

	err = publisher.PublishRetry(context.Background(), validRetryEvent())

	require.ErrorIs(t, err, amqp.ErrClosed)
}

func validRetryEvent() PaymentEventMessage {
	return PaymentEventMessage{
		EventID:    uuid.MustParse("33333333-3333-3333-3333-333333333333"),
		EventType:  domain.PaymentEventTypeCreated,
		PaymentID:  uuid.MustParse("44444444-4444-4444-4444-444444444444"),
		Attempt:    1,
		OccurredAt: time.Date(2026, time.July, 18, 11, 0, 0, 0, time.UTC),
	}
}

func purgeQueue(t *testing.T, ch *amqp.Channel, queueName string) {
	t.Helper()

	_, err := ch.QueuePurge(queueName, false)
	require.NoError(t, err)
	t.Cleanup(func() {
		_, err := ch.QueuePurge(queueName, false)
		require.NoError(t, err)
	})
}
