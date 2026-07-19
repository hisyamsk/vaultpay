package worker

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"testing"

	"github.com/google/uuid"
	"github.com/hisyamsk/vaultpay/internal/domain"
	"github.com/hisyamsk/vaultpay/internal/queue"
	"github.com/hisyamsk/vaultpay/internal/repository"
	"github.com/stretchr/testify/require"
)

const transferFinalizerPaymentID = "0198be7e-9a2a-7000-8000-000000000001"

type fakeTransferFinalizerPaymentService struct {
	t *testing.T

	findPaymentByIDFn          func(ctx context.Context, id uuid.UUID) (*domain.Payment, error)
	completeProcessedPaymentFn func(ctx context.Context, paymentID uuid.UUID) (*domain.Payment, error)
}

func (f *fakeTransferFinalizerPaymentService) FindPaymentByID(ctx context.Context, id uuid.UUID) (*domain.Payment, error) {
	f.t.Helper()
	if f.findPaymentByIDFn == nil {
		f.t.Fatalf("unexpected FindPaymentByID call")
	}
	return f.findPaymentByIDFn(ctx, id)
}

func (f *fakeTransferFinalizerPaymentService) CompleteProcessedPayment(ctx context.Context, paymentID uuid.UUID) (*domain.Payment, error) {
	f.t.Helper()
	if f.completeProcessedPaymentFn == nil {
		f.t.Fatalf("unexpected CompleteProcessedPayment call")
	}
	return f.completeProcessedPaymentFn(ctx, paymentID)
}

func newTestTransferFinalizer(t *testing.T, service *fakeTransferFinalizerPaymentService) *TransferFinalizer {
	t.Helper()

	return NewTransferFinalizer(
		service,
		slog.New(slog.NewTextHandler(io.Discard, nil)),
	)
}

func TestTransferFinalizerHandleEventDropsMissingPayment(t *testing.T) {
	paymentID := uuid.MustParse(transferFinalizerPaymentID)
	findCalls := 0
	finalizer := newTestTransferFinalizer(t, &fakeTransferFinalizerPaymentService{
		t: t,
		findPaymentByIDFn: func(ctx context.Context, id uuid.UUID) (*domain.Payment, error) {
			findCalls++
			require.Equal(t, paymentID, id)
			return nil, repository.ErrPaymentNotFound
		},
	})

	err := finalizer.HandleEvent(context.Background(), queue.PaymentEventMessage{
		PaymentID: paymentID,
		Attempt:   1,
	})

	require.NoError(t, err)
	require.Equal(t, 1, findCalls)
}

func TestTransferFinalizerHandleEventReturnsFindPaymentErrorForRetry(t *testing.T) {
	paymentID := uuid.MustParse(transferFinalizerPaymentID)
	dbErr := errors.New("database unavailable")
	findCalls := 0
	finalizer := newTestTransferFinalizer(t, &fakeTransferFinalizerPaymentService{
		t: t,
		findPaymentByIDFn: func(ctx context.Context, id uuid.UUID) (*domain.Payment, error) {
			findCalls++
			require.Equal(t, paymentID, id)
			return nil, dbErr
		},
	})

	err := finalizer.HandleEvent(context.Background(), queue.PaymentEventMessage{
		PaymentID: paymentID,
		Attempt:   1,
	})

	require.ErrorIs(t, err, dbErr)
	require.ErrorContains(t, err, "finalize internal transfer")
	require.Equal(t, 1, findCalls)
}

func TestTransferFinalizerHandleEventSkipsNonProcessingPayment(t *testing.T) {
	paymentID := uuid.MustParse(transferFinalizerPaymentID)
	tests := []struct {
		name   string
		status domain.PaymentStatus
	}{
		{name: "pending", status: domain.PaymentStatusPending},
		{name: "completed", status: domain.PaymentStatusCompleted},
		{name: "failed", status: domain.PaymentStatusFailed},
		{name: "rejected", status: domain.PaymentStatusRejected},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			payment := &domain.Payment{
				ID:     paymentID,
				Amount: 10_000,
				Status: tt.status,
			}
			original := *payment
			findCalls := 0
			finalizer := newTestTransferFinalizer(t, &fakeTransferFinalizerPaymentService{
				t: t,
				findPaymentByIDFn: func(ctx context.Context, id uuid.UUID) (*domain.Payment, error) {
					findCalls++
					require.Equal(t, paymentID, id)
					return payment, nil
				},
			})

			err := finalizer.HandleEvent(context.Background(), queue.PaymentEventMessage{
				PaymentID: paymentID,
				Attempt:   1,
			})

			require.NoError(t, err)
			require.Equal(t, 1, findCalls)
			require.Equal(t, original, *payment)
		})
	}
}

func TestTransferFinalizerHandleEventCompletesProcessingPaymentOnce(t *testing.T) {
	paymentID := uuid.MustParse(transferFinalizerPaymentID)
	payment := &domain.Payment{
		ID:     paymentID,
		Status: domain.PaymentStatusProcessing,
	}
	findCalls := 0
	completeCalls := 0
	finalizer := newTestTransferFinalizer(t, &fakeTransferFinalizerPaymentService{
		t: t,
		findPaymentByIDFn: func(ctx context.Context, id uuid.UUID) (*domain.Payment, error) {
			findCalls++
			require.Equal(t, paymentID, id)
			return payment, nil
		},
		completeProcessedPaymentFn: func(ctx context.Context, gotPaymentID uuid.UUID) (*domain.Payment, error) {
			completeCalls++
			require.Equal(t, paymentID, gotPaymentID)
			return &domain.Payment{
				ID:     gotPaymentID,
				Status: domain.PaymentStatusCompleted,
			}, nil
		},
	})

	err := finalizer.HandleEvent(context.Background(), queue.PaymentEventMessage{
		PaymentID: paymentID,
		Attempt:   1,
	})

	require.NoError(t, err)
	require.Equal(t, 1, findCalls)
	require.Equal(t, 1, completeCalls)
}

func TestTransferFinalizerHandleEventReturnsCompletionErrorForRetry(t *testing.T) {
	paymentID := uuid.MustParse(transferFinalizerPaymentID)
	dbErr := errors.New("complete payment failed")
	findCalls := 0
	completeCalls := 0
	finalizer := newTestTransferFinalizer(t, &fakeTransferFinalizerPaymentService{
		t: t,
		findPaymentByIDFn: func(ctx context.Context, id uuid.UUID) (*domain.Payment, error) {
			findCalls++
			require.Equal(t, paymentID, id)
			return &domain.Payment{
				ID:     id,
				Status: domain.PaymentStatusProcessing,
			}, nil
		},
		completeProcessedPaymentFn: func(ctx context.Context, gotPaymentID uuid.UUID) (*domain.Payment, error) {
			completeCalls++
			require.Equal(t, paymentID, gotPaymentID)
			return nil, dbErr
		},
	})

	err := finalizer.HandleEvent(context.Background(), queue.PaymentEventMessage{
		PaymentID: paymentID,
		Attempt:   1,
	})

	require.ErrorIs(t, err, dbErr)
	require.ErrorContains(t, err, "finalize internal transfer")
	require.Equal(t, 1, findCalls)
	require.Equal(t, 1, completeCalls)
}
