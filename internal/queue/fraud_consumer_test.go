package queue

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/hisyamsk/vaultpay/internal/domain"
	"github.com/stretchr/testify/require"
)

type fakeFraudEventHandler struct {
	messages []PaymentEventMessage
	err      error
}

func (f *fakeFraudEventHandler) HandleMessage(_ context.Context, message PaymentEventMessage) error {
	f.messages = append(f.messages, message)
	return f.err
}

func paymentEventBody(t *testing.T, message PaymentEventMessage) []byte {
	t.Helper()

	body, err := json.Marshal(message)
	require.NoError(t, err)
	return body
}

func TestFraudConsumerHandleMessagePassesCreatedEventToHandler(t *testing.T) {
	expected := PaymentEventMessage{
		EventID:    uuid.MustParse("11111111-1111-1111-1111-111111111111"),
		EventType:  domain.PaymentEventTypeCreated,
		PaymentID:  uuid.MustParse("22222222-2222-2222-2222-222222222222"),
		Attempt:    1,
		OccurredAt: time.Date(2026, time.July, 16, 10, 30, 0, 0, time.UTC),
	}
	handler := &fakeFraudEventHandler{}
	consumer := NewFraudConsumer(handler)

	err := consumer.HandleMessage(context.Background(), paymentEventBody(t, expected))

	require.NoError(t, err)
	require.Equal(t, []PaymentEventMessage{expected}, handler.messages)
}

func TestFraudConsumerHandleMessageRejectsInvalidBodyBeforeCallingHandler(t *testing.T) {
	handler := &fakeFraudEventHandler{}
	consumer := NewFraudConsumer(handler)

	err := consumer.HandleMessage(context.Background(), []byte(`{`))

	require.ErrorIs(t, err, ErrInvalidFraudMessage)
	require.Empty(t, handler.messages)
}

func TestFraudConsumerHandleMessageRejectsNonCreatedEventsBeforeCallingHandler(t *testing.T) {
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
			consumer := NewFraudConsumer(handler)

			err := consumer.HandleMessage(context.Background(), paymentEventBody(t, message))

			require.ErrorIs(t, err, ErrInvalidFraudMessage)
			require.Empty(t, handler.messages)
		})
	}
}

func TestFraudConsumerHandleMessageWrapsHandlerError(t *testing.T) {
	handlerErr := errors.New("database unavailable")
	handler := &fakeFraudEventHandler{err: handlerErr}
	consumer := NewFraudConsumer(handler)
	message := PaymentEventMessage{
		EventID:    uuid.MustParse("11111111-1111-1111-1111-111111111111"),
		EventType:  domain.PaymentEventTypeCreated,
		PaymentID:  uuid.MustParse("22222222-2222-2222-2222-222222222222"),
		Attempt:    1,
		OccurredAt: time.Date(2026, time.July, 16, 10, 30, 0, 0, time.UTC),
	}

	err := consumer.HandleMessage(context.Background(), paymentEventBody(t, message))

	require.ErrorIs(t, err, handlerErr)
	require.NotErrorIs(t, err, ErrInvalidFraudMessage)
	require.Equal(t, []PaymentEventMessage{message}, handler.messages)
}
