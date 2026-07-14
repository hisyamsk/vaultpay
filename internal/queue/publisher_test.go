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

func TestRabbitMQPublisherPublishesStoredEventWithConfirmationAndMetadata(t *testing.T) {
	ch := newTestRabbitMQChannel(t)
	require.NoError(t, DeclarePaymentEventsExchange(ch))
	require.NoError(t, DeclareFraudQueue(ch))

	_, err := ch.QueuePurge(FraudQueue, false)
	require.NoError(t, err)
	t.Cleanup(func() {
		_, err := ch.QueuePurge(FraudQueue, false)
		require.NoError(t, err)
	})

	publisher, err := NewRabbitMQPublisher(ch)
	require.NoError(t, err)

	storedPayload := []byte(`{"event_id":"11111111-1111-1111-1111-111111111111","committed_marker":"publish-this-exact-json"}`)
	event := domain.PaymentEvent{
		EventID:   uuid.MustParse("11111111-1111-1111-1111-111111111111"),
		EventType: domain.PaymentEventTypeCreated,
		Payload:   storedPayload,
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	require.NoError(t, publisher.Publish(ctx, event))

	delivery, ok, err := ch.Get(FraudQueue, false)
	require.NoError(t, err)
	require.True(t, ok, "Publish returned success but no message was routed to the fraud queue")
	require.Equal(t, storedPayload, delivery.Body)
	require.Equal(t, PaymentEventsExchange, delivery.Exchange)
	require.Equal(t, string(event.EventType), delivery.RoutingKey)
	require.Equal(t, "application/json", delivery.ContentType)
	require.Equal(t, event.EventID.String(), delivery.MessageId)
	require.Equal(t, uint8(amqp.Persistent), delivery.DeliveryMode)
	require.NoError(t, delivery.Ack(false))
}

func TestRabbitMQPublisherDoesNotPublishWithCanceledContext(t *testing.T) {
	ch := newTestRabbitMQChannel(t)
	require.NoError(t, DeclarePaymentEventsExchange(ch))
	require.NoError(t, DeclareFraudQueue(ch))

	_, err := ch.QueuePurge(FraudQueue, false)
	require.NoError(t, err)
	t.Cleanup(func() {
		_, err := ch.QueuePurge(FraudQueue, false)
		require.NoError(t, err)
	})

	publisher, err := NewRabbitMQPublisher(ch)
	require.NoError(t, err)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	event := domain.PaymentEvent{
		EventID:   uuid.MustParse("22222222-2222-2222-2222-222222222222"),
		EventType: domain.PaymentEventTypeCreated,
		Payload:   []byte(`{"event_id":"22222222-2222-2222-2222-222222222222"}`),
	}

	err = publisher.Publish(ctx, event)
	require.ErrorIs(t, err, context.Canceled)

	_, ok, err := ch.Get(FraudQueue, true)
	require.NoError(t, err)
	require.False(t, ok, "a canceled publish must not route a message")
}

func TestRabbitMQPublisherReturnsErrorWhenChannelIsClosed(t *testing.T) {
	ch := newTestRabbitMQChannel(t)
	publisher, err := NewRabbitMQPublisher(ch)
	require.NoError(t, err)
	require.NoError(t, ch.Close())

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	event := domain.PaymentEvent{
		EventID:   uuid.MustParse("33333333-3333-3333-3333-333333333333"),
		EventType: domain.PaymentEventTypeCreated,
		Payload:   []byte(`{"event_id":"33333333-3333-3333-3333-333333333333"}`),
	}

	err = publisher.Publish(ctx, event)
	require.ErrorIs(t, err, amqp.ErrClosed)
}
