package queue

import (
	"fmt"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/hisyamsk/vaultpay/internal/domain"
	"github.com/stretchr/testify/require"
)

func TestDecodePaymentEventReturnsValidatedMessage(t *testing.T) {
	eventID := uuid.MustParse("11111111-1111-1111-1111-111111111111")
	paymentID := uuid.MustParse("22222222-2222-2222-2222-222222222222")
	occurredAt := time.Date(2026, time.July, 16, 10, 30, 0, 0, time.UTC)
	body := []byte(fmt.Sprintf(`{
		"event_id": %q,
		"event_type": "payment.created",
		"payment_id": %q,
		"attempt": 1,
		"occurred_at": %q
	}`, eventID, paymentID, occurredAt.Format(time.RFC3339)))

	message, err := DecodePaymentEvent(body)

	require.NoError(t, err)
	require.Equal(t, eventID, message.EventID)
	require.Equal(t, domain.PaymentEventTypeCreated, message.EventType)
	require.Equal(t, paymentID, message.PaymentID)
	require.Equal(t, 1, message.Attempt)
	require.Equal(t, occurredAt, message.OccurredAt)
}

func TestDecodePaymentEventRejectsMalformedOrInvalidMessage(t *testing.T) {
	validEventID := "11111111-1111-1111-1111-111111111111"
	validPaymentID := "22222222-2222-2222-2222-222222222222"
	validOccurredAt := "2026-07-16T10:30:00Z"

	tests := []struct {
		name string
		body string
	}{
		{name: "malformed JSON", body: `{`},
		{
			name: "invalid event ID",
			body: fmt.Sprintf(`{
				"event_id": "not-a-uuid",
				"event_type": "payment.created",
				"payment_id": %q,
				"attempt": 1,
				"occurred_at": %q
			}`, validPaymentID, validOccurredAt),
		},
		{
			name: "missing event ID",
			body: fmt.Sprintf(`{
				"event_type": "payment.created",
				"payment_id": %q,
				"attempt": 1,
				"occurred_at": %q
			}`, validPaymentID, validOccurredAt),
		},
		{
			name: "unknown event type",
			body: fmt.Sprintf(`{
				"event_id": %q,
				"event_type": "payment.unknown",
				"payment_id": %q,
				"attempt": 1,
				"occurred_at": %q
			}`, validEventID, validPaymentID, validOccurredAt),
		},
		{
			name: "missing payment ID",
			body: fmt.Sprintf(`{
				"event_id": %q,
				"event_type": "payment.created",
				"attempt": 1,
				"occurred_at": %q
			}`, validEventID, validOccurredAt),
		},
		{
			name: "attempt below one",
			body: fmt.Sprintf(`{
				"event_id": %q,
				"event_type": "payment.created",
				"payment_id": %q,
				"attempt": 0,
				"occurred_at": %q
			}`, validEventID, validPaymentID, validOccurredAt),
		},
		{
			name: "missing occurred at",
			body: fmt.Sprintf(`{
				"event_id": %q,
				"event_type": "payment.created",
				"payment_id": %q,
				"attempt": 1
			}`, validEventID, validPaymentID),
		},
		{
			name: "invalid occurred at",
			body: fmt.Sprintf(`{
				"event_id": %q,
				"event_type": "payment.created",
				"payment_id": %q,
				"attempt": 1,
				"occurred_at": "not-a-timestamp"
			}`, validEventID, validPaymentID),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			message, err := DecodePaymentEvent([]byte(tt.body))

			require.Error(t, err)
			require.Equal(t, PaymentEventMessage{}, message)
		})
	}
}
