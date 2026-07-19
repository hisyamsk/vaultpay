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

type fakeFailurePublisher struct {
	deadLetterBodies [][]byte
	deadLetterErr    error
	retryEvents      []PaymentEventMessage
	retryErr         error
	order            *[]string
}

func (f *fakeFailurePublisher) PublishDeadLetter(_ context.Context, body []byte) error {
	f.deadLetterBodies = append(f.deadLetterBodies, append([]byte(nil), body...))
	if f.order != nil {
		*f.order = append(*f.order, "publish dead letter")
	}
	return f.deadLetterErr
}

func (f *fakeFailurePublisher) PublishRetry(_ context.Context, event PaymentEventMessage) error {
	f.retryEvents = append(f.retryEvents, event)
	if f.order != nil {
		*f.order = append(*f.order, "publish retry")
	}
	return f.retryErr
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

	consumer, err := NewFraudConsumer(handler, &fakeConsumerChannel{}, 1, 3, &fakeFailurePublisher{})
	require.NoError(t, err)
	return consumer
}

func paymentEventBody(t *testing.T, message PaymentEventMessage) []byte {
	t.Helper()

	body, err := json.Marshal(message)
	require.NoError(t, err)
	return body
}

func validFraudEventMessage() PaymentEventMessage {
	return PaymentEventMessage{
		EventID:    uuid.MustParse("33333333-3333-3333-3333-333333333333"),
		EventType:  domain.PaymentEventTypeCreated,
		PaymentID:  uuid.MustParse("44444444-4444-4444-4444-444444444444"),
		Attempt:    1,
		OccurredAt: time.Date(2026, time.July, 18, 11, 0, 0, 0, time.UTC),
	}
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
	consumer, err := NewFraudConsumer(handler, channel, 7, 3, &fakeFailurePublisher{})
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

func TestFraudConsumerConsumePublishesRetryBeforeAcknowledgingTransientHandlerFailure(t *testing.T) {
	handlerErr := errors.New("database unavailable")
	deliveries := make(chan amqp.Delivery, 1)
	channel := &fakeConsumerChannel{deliveries: deliveries}
	order := []string{}
	handler := &fakeFraudEventHandler{
		err: handlerErr,
		onHandle: func() {
			order = append(order, "handle")
		},
	}
	publisher := &fakeFailurePublisher{order: &order}
	acknowledger := &fakeAcknowledger{order: &order}
	message := PaymentEventMessage{
		EventID:    uuid.MustParse("11111111-1111-1111-1111-111111111111"),
		EventType:  domain.PaymentEventTypeCreated,
		PaymentID:  uuid.MustParse("22222222-2222-2222-2222-222222222222"),
		Attempt:    2,
		OccurredAt: time.Date(2026, time.July, 16, 10, 30, 0, 0, time.UTC),
	}
	consumer, err := NewFraudConsumer(handler, channel, 1, 3, publisher)
	require.NoError(t, err)
	deliveries <- amqp.Delivery{
		Acknowledger: acknowledger,
		DeliveryTag:  42,
		Body:         paymentEventBody(t, message),
	}
	close(deliveries)

	err = consumer.Consume(context.Background())

	require.ErrorContains(t, err, "fraud delivery channel closed")
	require.Equal(t, []PaymentEventMessage{message}, handler.messages)
	require.Equal(t, []PaymentEventMessage{message}, publisher.retryEvents)
	require.Equal(t, []string{"handle", "publish retry", "ack"}, order)
	require.Equal(t, 1, acknowledger.ackCalls)
	require.False(t, acknowledger.ackMultiple)
	require.Zero(t, acknowledger.nackCalls)
	require.Zero(t, acknowledger.rejectCalls)
}

func TestFraudConsumerConsumeDoesNotAcknowledgeWhenRetryPublicationFails(t *testing.T) {
	handlerErr := errors.New("database unavailable")
	publishErr := errors.New("retry publisher unavailable")
	message := validFraudEventMessage()
	handler := &fakeFraudEventHandler{err: handlerErr}
	deliveries := make(chan amqp.Delivery, 1)
	channel := &fakeConsumerChannel{deliveries: deliveries}
	publisher := &fakeFailurePublisher{retryErr: publishErr}
	acknowledger := &fakeAcknowledger{}
	consumer, err := NewFraudConsumer(handler, channel, 1, 3, publisher)
	require.NoError(t, err)
	deliveries <- amqp.Delivery{
		Acknowledger: acknowledger,
		DeliveryTag:  42,
		Body:         paymentEventBody(t, message),
	}
	close(deliveries)

	err = consumer.Consume(context.Background())

	require.ErrorIs(t, err, publishErr)
	require.Equal(t, []PaymentEventMessage{message}, handler.messages)
	require.Equal(t, []PaymentEventMessage{message}, publisher.retryEvents)
	require.Zero(t, acknowledger.ackCalls)
	require.Zero(t, acknowledger.nackCalls)
	require.Zero(t, acknowledger.rejectCalls)
}

func TestFraudConsumerConsumeReturnsAckErrorAfterConfirmedRetry(t *testing.T) {
	handlerErr := errors.New("database unavailable")
	ackErr := errors.New("acknowledgement failed")
	message := validFraudEventMessage()
	handler := &fakeFraudEventHandler{err: handlerErr}
	deliveries := make(chan amqp.Delivery, 1)
	channel := &fakeConsumerChannel{deliveries: deliveries}
	publisher := &fakeFailurePublisher{}
	acknowledger := &fakeAcknowledger{ackErr: ackErr}
	consumer, err := NewFraudConsumer(handler, channel, 1, 3, publisher)
	require.NoError(t, err)
	deliveries <- amqp.Delivery{
		Acknowledger: acknowledger,
		DeliveryTag:  42,
		Body:         paymentEventBody(t, message),
	}
	close(deliveries)

	err = consumer.Consume(context.Background())

	require.ErrorIs(t, err, ackErr)
	require.Equal(t, []PaymentEventMessage{message}, publisher.retryEvents)
	require.Equal(t, 1, acknowledger.ackCalls)
	require.False(t, acknowledger.ackMultiple)
	require.Zero(t, acknowledger.nackCalls)
	require.Zero(t, acknowledger.rejectCalls)
}

func TestFraudConsumerConsumeDeadLettersExhaustedTransientFailureBeforeAcknowledging(t *testing.T) {
	tests := []struct {
		name    string
		attempt int
	}{
		{name: "at maximum", attempt: 3},
		{name: "above maximum", attempt: 4},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			message := validFraudEventMessage()
			message.Attempt = tt.attempt
			body := paymentEventBody(t, message)
			handlerErr := errors.New("database unavailable")
			deliveries := make(chan amqp.Delivery, 1)
			channel := &fakeConsumerChannel{deliveries: deliveries}
			order := []string{}
			handler := &fakeFraudEventHandler{
				err: handlerErr,
				onHandle: func() {
					order = append(order, "handle")
				},
			}
			publisher := &fakeFailurePublisher{order: &order}
			acknowledger := &fakeAcknowledger{order: &order}
			consumer, err := NewFraudConsumer(handler, channel, 1, 3, publisher)
			require.NoError(t, err)
			deliveries <- amqp.Delivery{
				Acknowledger: acknowledger,
				DeliveryTag:  42,
				Body:         body,
			}
			close(deliveries)

			err = consumer.Consume(context.Background())

			require.ErrorContains(t, err, "fraud delivery channel closed")
			require.Equal(t, []PaymentEventMessage{message}, handler.messages)
			require.Equal(t, [][]byte{body}, publisher.deadLetterBodies)
			require.Empty(t, publisher.retryEvents)
			require.Equal(t, []string{"handle", "publish dead letter", "ack"}, order)
			require.Equal(t, 1, acknowledger.ackCalls)
			require.False(t, acknowledger.ackMultiple)
			require.Zero(t, acknowledger.nackCalls)
			require.Zero(t, acknowledger.rejectCalls)
		})
	}
}

func TestFraudConsumerConsumeDoesNotAcknowledgeWhenExhaustedDeadLetterPublicationFails(t *testing.T) {
	message := validFraudEventMessage()
	message.Attempt = 3
	body := paymentEventBody(t, message)
	handlerErr := errors.New("database unavailable")
	publishErr := errors.New("dead-letter publisher unavailable")
	handler := &fakeFraudEventHandler{err: handlerErr}
	deliveries := make(chan amqp.Delivery, 1)
	channel := &fakeConsumerChannel{deliveries: deliveries}
	publisher := &fakeFailurePublisher{deadLetterErr: publishErr}
	acknowledger := &fakeAcknowledger{}
	consumer, err := NewFraudConsumer(handler, channel, 1, 3, publisher)
	require.NoError(t, err)
	deliveries <- amqp.Delivery{
		Acknowledger: acknowledger,
		DeliveryTag:  42,
		Body:         body,
	}
	close(deliveries)

	err = consumer.Consume(context.Background())

	require.ErrorIs(t, err, publishErr)
	require.Equal(t, []PaymentEventMessage{message}, handler.messages)
	require.Equal(t, [][]byte{body}, publisher.deadLetterBodies)
	require.Empty(t, publisher.retryEvents)
	require.Zero(t, acknowledger.ackCalls)
	require.Zero(t, acknowledger.nackCalls)
	require.Zero(t, acknowledger.rejectCalls)
}

func TestFraudConsumerConsumePublishesMalformedInputBeforeAcknowledging(t *testing.T) {
	body := []byte(`{`)
	deliveries := make(chan amqp.Delivery, 1)
	channel := &fakeConsumerChannel{deliveries: deliveries}
	order := []string{}
	publisher := &fakeFailurePublisher{order: &order}
	acknowledger := &fakeAcknowledger{order: &order}
	handler := &fakeFraudEventHandler{}
	consumer, err := NewFraudConsumer(handler, channel, 1, 3, publisher)
	require.NoError(t, err)
	deliveries <- amqp.Delivery{
		Acknowledger: acknowledger,
		DeliveryTag:  42,
		Body:         body,
	}
	close(deliveries)

	err = consumer.Consume(context.Background())

	require.ErrorContains(t, err, "fraud delivery channel closed")
	require.Empty(t, handler.messages)
	require.Equal(t, [][]byte{body}, publisher.deadLetterBodies)
	require.Equal(t, []string{"publish dead letter", "ack"}, order)
	require.Equal(t, 1, acknowledger.ackCalls)
	require.False(t, acknowledger.ackMultiple)
	require.Zero(t, acknowledger.nackCalls)
	require.Zero(t, acknowledger.rejectCalls)
}

func TestFraudConsumerConsumeDoesNotAcknowledgeWhenDeadLetterPublicationFails(t *testing.T) {
	body := []byte(`{`)
	publishErr := errors.New("dead-letter publisher unavailable")
	deliveries := make(chan amqp.Delivery, 1)
	channel := &fakeConsumerChannel{deliveries: deliveries}
	publisher := &fakeFailurePublisher{deadLetterErr: publishErr}
	acknowledger := &fakeAcknowledger{}
	handler := &fakeFraudEventHandler{}
	consumer, err := NewFraudConsumer(handler, channel, 1, 3, publisher)
	require.NoError(t, err)
	deliveries <- amqp.Delivery{
		Acknowledger: acknowledger,
		DeliveryTag:  42,
		Body:         body,
	}

	err = consumer.Consume(context.Background())

	require.ErrorIs(t, err, publishErr)
	require.Empty(t, handler.messages)
	require.Equal(t, [][]byte{body}, publisher.deadLetterBodies)
	require.Zero(t, acknowledger.ackCalls)
	require.Zero(t, acknowledger.nackCalls)
	require.Zero(t, acknowledger.rejectCalls)
}

func TestFraudConsumerConsumeStopsWhenQoSConfigurationFails(t *testing.T) {
	qosErr := errors.New("channel closed")
	channel := &fakeConsumerChannel{qosErr: qosErr}
	consumer, err := NewFraudConsumer(&fakeFraudEventHandler{}, channel, 1, 3, &fakeFailurePublisher{})
	require.NoError(t, err)

	err = consumer.Consume(context.Background())

	require.ErrorIs(t, err, qosErr)
	require.Equal(t, 1, channel.qosCalls)
	require.Zero(t, channel.consumeCalls)
}

func TestFraudConsumerConsumeReturnsConsumptionSetupFailure(t *testing.T) {
	consumeErr := errors.New("consumer registration failed")
	channel := &fakeConsumerChannel{consumeErr: consumeErr}
	consumer, err := NewFraudConsumer(&fakeFraudEventHandler{}, channel, 1, 3, &fakeFailurePublisher{})
	require.NoError(t, err)

	err = consumer.Consume(context.Background())

	require.ErrorIs(t, err, consumeErr)
	require.Equal(t, 1, channel.qosCalls)
	require.Equal(t, 1, channel.consumeCalls)
}
