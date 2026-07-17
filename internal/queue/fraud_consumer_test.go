package queue

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/hisyamsk/vaultpay/internal/domain"
	amqp "github.com/rabbitmq/amqp091-go"
	"github.com/stretchr/testify/require"
)

type fakeFraudEventHandler struct {
	messages []PaymentEventMessage
	err      error
	onHandle func()
}

func (f *fakeFraudEventHandler) HandleEvent(_ context.Context, event PaymentEventMessage) error {
	f.messages = append(f.messages, event)
	if f.onHandle != nil {
		f.onHandle()
	}
	return f.err
}

type fakeConsumerChannel struct {
	qosPrefetchCount int
	qosPrefetchSize  int
	qosGlobal        bool
	qosErr           error
	qosCalls         int

	consumeQueue     string
	consumeName      string
	consumeAutoAck   bool
	consumeExclusive bool
	consumeNoLocal   bool
	consumeNoWait    bool
	consumeArgs      amqp.Table
	consumeErr       error
	consumeCalls     int
	deliveries       <-chan amqp.Delivery
}

type fakeFailurePublisher struct{}

func (*fakeFailurePublisher) PublishDeadLetter(context.Context, []byte) error {
	return nil
}

func (*fakeFailurePublisher) PublishRetry(context.Context, PaymentEventMessage) error {
	return nil
}

func (f *fakeConsumerChannel) Qos(prefetchCount, prefetchSize int, global bool) error {
	f.qosCalls++
	f.qosPrefetchCount = prefetchCount
	f.qosPrefetchSize = prefetchSize
	f.qosGlobal = global
	return f.qosErr
}

func (f *fakeConsumerChannel) ConsumeWithContext(
	_ context.Context,
	queue, consumer string,
	autoAck, exclusive, noLocal, noWait bool,
	args amqp.Table,
) (<-chan amqp.Delivery, error) {
	f.consumeCalls++
	f.consumeQueue = queue
	f.consumeName = consumer
	f.consumeAutoAck = autoAck
	f.consumeExclusive = exclusive
	f.consumeNoLocal = noLocal
	f.consumeNoWait = noWait
	f.consumeArgs = args
	return f.deliveries, f.consumeErr
}

type fakeAcknowledger struct {
	order       *[]string
	ackErr      error
	ackCalls    int
	ackMultiple bool
	nackCalls   int
	rejectCalls int
}

func (f *fakeAcknowledger) Ack(_ uint64, multiple bool) error {
	f.ackCalls++
	f.ackMultiple = multiple
	if f.order != nil {
		*f.order = append(*f.order, "ack")
	}
	return f.ackErr
}

func (f *fakeAcknowledger) Nack(_ uint64, _, _ bool) error {
	f.nackCalls++
	return nil
}

func (f *fakeAcknowledger) Reject(_ uint64, _ bool) error {
	f.rejectCalls++
	return nil
}

func newTestFraudConsumer(t *testing.T, handler fraudEventHandler) *FraudConsumer {
	t.Helper()

	consumer, err := NewFraudConsumer(handler, &fakeConsumerChannel{}, 1, &fakeFailurePublisher{})
	require.NoError(t, err)
	return consumer
}

func paymentEventBody(t *testing.T, message PaymentEventMessage) []byte {
	t.Helper()

	body, err := json.Marshal(message)
	require.NoError(t, err)
	return body
}

func TestFraudConsumerHandleDeliveryPassesCreatedEventToHandler(t *testing.T) {
	expected := PaymentEventMessage{
		EventID:    uuid.MustParse("11111111-1111-1111-1111-111111111111"),
		EventType:  domain.PaymentEventTypeCreated,
		PaymentID:  uuid.MustParse("22222222-2222-2222-2222-222222222222"),
		Attempt:    1,
		OccurredAt: time.Date(2026, time.July, 16, 10, 30, 0, 0, time.UTC),
	}
	handler := &fakeFraudEventHandler{}
	consumer := newTestFraudConsumer(t, handler)

	err := consumer.HandleDelivery(context.Background(), paymentEventBody(t, expected))

	require.NoError(t, err)
	require.Equal(t, []PaymentEventMessage{expected}, handler.messages)
}

func TestFraudConsumerHandleDeliveryRejectsInvalidBodyBeforeCallingHandler(t *testing.T) {
	tests := []struct {
		name string
		body []byte
	}{
		{
			name: "malformed JSON",
			body: []byte(`{`),
		},
		{
			name: "semantically invalid event",
			body: paymentEventBody(t, PaymentEventMessage{
				EventID:    uuid.MustParse("11111111-1111-1111-1111-111111111111"),
				EventType:  domain.PaymentEventTypeCreated,
				PaymentID:  uuid.MustParse("22222222-2222-2222-2222-222222222222"),
				Attempt:    0,
				OccurredAt: time.Date(2026, time.July, 16, 10, 30, 0, 0, time.UTC),
			}),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			handler := &fakeFraudEventHandler{}
			consumer := newTestFraudConsumer(t, handler)

			err := consumer.HandleDelivery(context.Background(), tt.body)

			require.ErrorIs(t, err, ErrInvalidFraudMessage)
			require.Empty(t, handler.messages)
		})
	}
}

func TestFraudConsumerHandleDeliveryRejectsNonCreatedEventsBeforeCallingHandler(t *testing.T) {
	tests := []domain.PaymentEventType{
		domain.PaymentEventTypeProcessing,
		domain.PaymentEventTypeCompleted,
		domain.PaymentEventTypeFailed,
		domain.PaymentEventTypeRejected,
	}

	for _, eventType := range tests {
		t.Run(string(eventType), func(t *testing.T) {
			message := PaymentEventMessage{
				EventID:    uuid.MustParse("11111111-1111-1111-1111-111111111111"),
				EventType:  eventType,
				PaymentID:  uuid.MustParse("22222222-2222-2222-2222-222222222222"),
				Attempt:    1,
				OccurredAt: time.Date(2026, time.July, 16, 10, 30, 0, 0, time.UTC),
			}
			handler := &fakeFraudEventHandler{}
			consumer := newTestFraudConsumer(t, handler)

			err := consumer.HandleDelivery(context.Background(), paymentEventBody(t, message))

			require.ErrorIs(t, err, ErrInvalidFraudMessage)
			require.Empty(t, handler.messages)
		})
	}
}

func TestFraudConsumerHandleDeliveryWrapsHandlerError(t *testing.T) {
	handlerErr := errors.New("database unavailable")
	handler := &fakeFraudEventHandler{err: handlerErr}
	consumer := newTestFraudConsumer(t, handler)
	message := PaymentEventMessage{
		EventID:    uuid.MustParse("11111111-1111-1111-1111-111111111111"),
		EventType:  domain.PaymentEventTypeCreated,
		PaymentID:  uuid.MustParse("22222222-2222-2222-2222-222222222222"),
		Attempt:    1,
		OccurredAt: time.Date(2026, time.July, 16, 10, 30, 0, 0, time.UTC),
	}

	err := consumer.HandleDelivery(context.Background(), paymentEventBody(t, message))

	require.ErrorIs(t, err, handlerErr)
	require.NotErrorIs(t, err, ErrInvalidFraudMessage)
	require.Equal(t, []PaymentEventMessage{message}, handler.messages)
}

func TestFraudConsumerConsumeConfiguresManualConsumptionAndAcknowledgesAfterHandling(t *testing.T) {
	deliveries := make(chan amqp.Delivery, 1)
	channel := &fakeConsumerChannel{deliveries: deliveries}
	order := []string{}
	handler := &fakeFraudEventHandler{
		onHandle: func() {
			order = append(order, "handle")
		},
	}
	acknowledger := &fakeAcknowledger{order: &order}
	message := PaymentEventMessage{
		EventID:    uuid.MustParse("11111111-1111-1111-1111-111111111111"),
		EventType:  domain.PaymentEventTypeCreated,
		PaymentID:  uuid.MustParse("22222222-2222-2222-2222-222222222222"),
		Attempt:    1,
		OccurredAt: time.Date(2026, time.July, 16, 10, 30, 0, 0, time.UTC),
	}
	consumer, err := NewFraudConsumer(handler, channel, 7, &fakeFailurePublisher{})
	require.NoError(t, err)
	deliveries <- amqp.Delivery{
		Acknowledger: acknowledger,
		DeliveryTag:  42,
		Body:         paymentEventBody(t, message),
	}
	close(deliveries)

	err = consumer.Consume(context.Background())

	require.ErrorContains(t, err, "fraud delivery channel closed")
	require.Equal(t, 1, channel.qosCalls)
	require.Equal(t, 7, channel.qosPrefetchCount)
	require.Zero(t, channel.qosPrefetchSize)
	require.False(t, channel.qosGlobal)
	require.Equal(t, 1, channel.consumeCalls)
	require.Equal(t, FraudQueue, channel.consumeQueue)
	require.Empty(t, channel.consumeName)
	require.False(t, channel.consumeAutoAck)
	require.False(t, channel.consumeExclusive)
	require.False(t, channel.consumeNoLocal)
	require.False(t, channel.consumeNoWait)
	require.Nil(t, channel.consumeArgs)
	require.Equal(t, []PaymentEventMessage{message}, handler.messages)
	require.Equal(t, []string{"handle", "ack"}, order)
	require.Equal(t, 1, acknowledger.ackCalls)
	require.False(t, acknowledger.ackMultiple)
	require.Zero(t, acknowledger.nackCalls)
	require.Zero(t, acknowledger.rejectCalls)
}

func TestFraudConsumerConsumeDoesNotAcknowledgeHandlerFailure(t *testing.T) {
	handlerErr := errors.New("database unavailable")
	handler := &fakeFraudEventHandler{err: handlerErr}
	deliveries := make(chan amqp.Delivery, 1)
	channel := &fakeConsumerChannel{deliveries: deliveries}
	acknowledger := &fakeAcknowledger{}
	message := PaymentEventMessage{
		EventID:    uuid.MustParse("11111111-1111-1111-1111-111111111111"),
		EventType:  domain.PaymentEventTypeCreated,
		PaymentID:  uuid.MustParse("22222222-2222-2222-2222-222222222222"),
		Attempt:    1,
		OccurredAt: time.Date(2026, time.July, 16, 10, 30, 0, 0, time.UTC),
	}
	consumer, err := NewFraudConsumer(handler, channel, 1, &fakeFailurePublisher{})
	require.NoError(t, err)
	deliveries <- amqp.Delivery{
		Acknowledger: acknowledger,
		DeliveryTag:  42,
		Body:         paymentEventBody(t, message),
	}

	err = consumer.Consume(context.Background())

	require.ErrorIs(t, err, handlerErr)
	require.Equal(t, []PaymentEventMessage{message}, handler.messages)
	require.Zero(t, acknowledger.ackCalls)
	require.Zero(t, acknowledger.nackCalls)
	require.Zero(t, acknowledger.rejectCalls)
}

func TestFraudConsumerConsumeStopsWhenQoSConfigurationFails(t *testing.T) {
	qosErr := errors.New("channel closed")
	channel := &fakeConsumerChannel{qosErr: qosErr}
	consumer, err := NewFraudConsumer(&fakeFraudEventHandler{}, channel, 1, &fakeFailurePublisher{})
	require.NoError(t, err)

	err = consumer.Consume(context.Background())

	require.ErrorIs(t, err, qosErr)
	require.Equal(t, 1, channel.qosCalls)
	require.Zero(t, channel.consumeCalls)
}

func TestFraudConsumerConsumeReturnsConsumptionSetupFailure(t *testing.T) {
	consumeErr := errors.New("consumer registration failed")
	channel := &fakeConsumerChannel{consumeErr: consumeErr}
	consumer, err := NewFraudConsumer(&fakeFraudEventHandler{}, channel, 1, &fakeFailurePublisher{})
	require.NoError(t, err)

	err = consumer.Consume(context.Background())

	require.ErrorIs(t, err, consumeErr)
	require.Equal(t, 1, channel.qosCalls)
	require.Equal(t, 1, channel.consumeCalls)
}
