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

type fakePaymentService struct {
	t *testing.T

	findPaymentByIDFn                func(ctx context.Context, id uuid.UUID) (*domain.Payment, error)
	rejectPendingPaymentFn           func(ctx context.Context, paymentID uuid.UUID) error
	startApprovedPaymentProcessingFn func(ctx context.Context, paymentID uuid.UUID) (*domain.Payment, error)
}

func (f *fakePaymentService) FindPaymentByID(ctx context.Context, id uuid.UUID) (*domain.Payment, error) {
	f.t.Helper()
	if f.findPaymentByIDFn == nil {
		f.t.Fatalf("unexpected FindPaymentByID call")
	}
	return f.findPaymentByIDFn(ctx, id)
}

func (f *fakePaymentService) RejectPendingPayment(ctx context.Context, paymentID uuid.UUID) error {
	f.t.Helper()
	if f.rejectPendingPaymentFn == nil {
		f.t.Fatalf("unexpected RejectPendingPayment call")
	}
	return f.rejectPendingPaymentFn(ctx, paymentID)
}

func (f *fakePaymentService) StartApprovedPaymentProcessing(ctx context.Context, paymentID uuid.UUID) (*domain.Payment, error) {
	f.t.Helper()
	if f.startApprovedPaymentProcessingFn == nil {
		f.t.Fatalf("unexpected StartApprovedPaymentProcessing call")
	}
	return f.startApprovedPaymentProcessingFn(ctx, paymentID)
}

type fakeFraudChecker struct {
	t *testing.T

	checkFn func(ctx context.Context, payment *domain.Payment) (FraudDecision, error)
}

func (f *fakeFraudChecker) Check(ctx context.Context, payment *domain.Payment) (FraudDecision, error) {
	f.t.Helper()
	if f.checkFn == nil {
		f.t.Fatalf("unexpected fraud checker call")
	}
	return f.checkFn(ctx, payment)
}

func newTestFraudWorker(t *testing.T, svc *fakePaymentService, checker *fakeFraudChecker) *FraudWorker {
	t.Helper()

	if svc == nil {
		svc = &fakePaymentService{t: t}
	}
	if checker == nil {
		checker = &fakeFraudChecker{t: t}
	}

	return NewFraudWorker(
		svc,
		checker,
		slog.New(slog.NewTextHandler(io.Discard, nil)),
	)
}

func TestFraudWorkerHandleMessageDropsInvalidPaymentID(t *testing.T) {
	worker := newTestFraudWorker(t, nil, nil)

	err := worker.HandleMessage(context.Background(), queue.PaymentMessage{
		PaymentID:     uuid.Nil,
		Attempt:       1,
		CorrelationID: "correlation-1",
	})

	require.NoError(t, err)
}

func TestFraudWorkerHandleMessageDropsMissingPayment(t *testing.T) {
	paymentID := uuid.New()
	worker := newTestFraudWorker(t, &fakePaymentService{
		t: t,
		findPaymentByIDFn: func(ctx context.Context, id uuid.UUID) (*domain.Payment, error) {
			require.Equal(t, paymentID, id)
			return nil, repository.ErrPaymentNotFound
		},
	}, nil)

	err := worker.HandleMessage(context.Background(), queue.PaymentMessage{
		PaymentID:     paymentID,
		Attempt:       1,
		CorrelationID: "correlation-1",
	})

	require.NoError(t, err)
}

func TestFraudWorkerHandleMessageReturnsFindPaymentErrorForRetry(t *testing.T) {
	paymentID := uuid.New()
	dbErr := errors.New("db unavailable")
	worker := newTestFraudWorker(t, &fakePaymentService{
		t: t,
		findPaymentByIDFn: func(ctx context.Context, id uuid.UUID) (*domain.Payment, error) {
			require.Equal(t, paymentID, id)
			return nil, dbErr
		},
	}, nil)

	err := worker.HandleMessage(context.Background(), queue.PaymentMessage{
		PaymentID:     paymentID,
		Attempt:       1,
		CorrelationID: "correlation-1",
	})

	require.ErrorIs(t, err, dbErr)
}

func TestFraudWorkerHandleMessageSkipsNonPendingPayment(t *testing.T) {
	tests := []struct {
		name   string
		status domain.PaymentStatus
	}{
		{name: "processing", status: domain.PaymentStatusProcessing},
		{name: "completed", status: domain.PaymentStatusCompleted},
		{name: "failed", status: domain.PaymentStatusFailed},
		{name: "rejected", status: domain.PaymentStatusRejected},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			paymentID := uuid.New()
			worker := newTestFraudWorker(t, &fakePaymentService{
				t: t,
				findPaymentByIDFn: func(ctx context.Context, id uuid.UUID) (*domain.Payment, error) {
					require.Equal(t, paymentID, id)
					return &domain.Payment{
						ID:     paymentID,
						Status: tt.status,
					}, nil
				},
			}, nil)

			err := worker.HandleMessage(context.Background(), queue.PaymentMessage{
				PaymentID:     paymentID,
				Attempt:       1,
				CorrelationID: "correlation-1",
			})

			require.NoError(t, err)
		})
	}
}

func TestFraudWorkerHandleMessageApprovedPaymentStartsProcessing(t *testing.T) {
	paymentID := uuid.New()
	payment := &domain.Payment{
		ID:     paymentID,
		Status: domain.PaymentStatusPending,
	}

	var checkedPaymentID uuid.UUID
	var startedPaymentID uuid.UUID
	worker := newTestFraudWorker(t, &fakePaymentService{
		t: t,
		findPaymentByIDFn: func(ctx context.Context, id uuid.UUID) (*domain.Payment, error) {
			require.Equal(t, paymentID, id)
			return payment, nil
		},
		startApprovedPaymentProcessingFn: func(ctx context.Context, paymentID uuid.UUID) (*domain.Payment, error) {
			startedPaymentID = paymentID
			return &domain.Payment{
				ID:     paymentID,
				Status: domain.PaymentStatusProcessing,
			}, nil
		},
	}, &fakeFraudChecker{
		t: t,
		checkFn: func(ctx context.Context, payment *domain.Payment) (FraudDecision, error) {
			checkedPaymentID = payment.ID
			return FraudDecisionApproved, nil
		},
	})

	err := worker.HandleMessage(context.Background(), queue.PaymentMessage{
		PaymentID:     paymentID,
		Attempt:       1,
		CorrelationID: "correlation-1",
	})

	require.NoError(t, err)
	require.Equal(t, paymentID, checkedPaymentID)
	require.Equal(t, paymentID, startedPaymentID)
}

func TestFraudWorkerHandleMessageRejectedPaymentRejectsPendingPayment(t *testing.T) {
	paymentID := uuid.New()
	payment := &domain.Payment{
		ID:     paymentID,
		Status: domain.PaymentStatusPending,
	}

	var checkedPaymentID uuid.UUID
	var rejectedPaymentID uuid.UUID
	worker := newTestFraudWorker(t, &fakePaymentService{
		t: t,
		findPaymentByIDFn: func(ctx context.Context, id uuid.UUID) (*domain.Payment, error) {
			require.Equal(t, paymentID, id)
			return payment, nil
		},
		rejectPendingPaymentFn: func(ctx context.Context, paymentID uuid.UUID) error {
			rejectedPaymentID = paymentID
			return nil
		},
	}, &fakeFraudChecker{
		t: t,
		checkFn: func(ctx context.Context, payment *domain.Payment) (FraudDecision, error) {
			checkedPaymentID = payment.ID
			return FraudDecisionRejected, nil
		},
	})

	err := worker.HandleMessage(context.Background(), queue.PaymentMessage{
		PaymentID:     paymentID,
		Attempt:       1,
		CorrelationID: "correlation-1",
	})

	require.NoError(t, err)
	require.Equal(t, paymentID, checkedPaymentID)
	require.Equal(t, paymentID, rejectedPaymentID)
}

func TestFraudWorkerHandleMessageReturnsFraudCheckerErrorForRetry(t *testing.T) {
	paymentID := uuid.New()
	checkErr := errors.New("fraud checker timeout")
	worker := newTestFraudWorker(t, &fakePaymentService{
		t: t,
		findPaymentByIDFn: func(ctx context.Context, id uuid.UUID) (*domain.Payment, error) {
			require.Equal(t, paymentID, id)
			return &domain.Payment{
				ID:     paymentID,
				Status: domain.PaymentStatusPending,
			}, nil
		},
	}, &fakeFraudChecker{
		t: t,
		checkFn: func(ctx context.Context, payment *domain.Payment) (FraudDecision, error) {
			require.Equal(t, paymentID, payment.ID)
			return "", checkErr
		},
	})

	err := worker.HandleMessage(context.Background(), queue.PaymentMessage{
		PaymentID:     paymentID,
		Attempt:       1,
		CorrelationID: "correlation-1",
	})

	require.ErrorIs(t, err, checkErr)
}

func TestFraudWorkerHandleMessageReturnsRejectErrorForRetry(t *testing.T) {
	paymentID := uuid.New()
	rejectErr := errors.New("reject failed")
	worker := newTestFraudWorker(t, &fakePaymentService{
		t: t,
		findPaymentByIDFn: func(ctx context.Context, id uuid.UUID) (*domain.Payment, error) {
			require.Equal(t, paymentID, id)
			return &domain.Payment{
				ID:     paymentID,
				Status: domain.PaymentStatusPending,
			}, nil
		},
		rejectPendingPaymentFn: func(ctx context.Context, gotPaymentID uuid.UUID) error {
			require.Equal(t, paymentID, gotPaymentID)
			return rejectErr
		},
	}, &fakeFraudChecker{
		t: t,
		checkFn: func(ctx context.Context, payment *domain.Payment) (FraudDecision, error) {
			require.Equal(t, paymentID, payment.ID)
			return FraudDecisionRejected, nil
		},
	})

	err := worker.HandleMessage(context.Background(), queue.PaymentMessage{
		PaymentID:     paymentID,
		Attempt:       1,
		CorrelationID: "correlation-1",
	})

	require.ErrorIs(t, err, rejectErr)
}

func TestFraudWorkerHandleMessageReturnsStartProcessingErrorForRetry(t *testing.T) {
	paymentID := uuid.New()
	startErr := errors.New("start processing failed")
	worker := newTestFraudWorker(t, &fakePaymentService{
		t: t,
		findPaymentByIDFn: func(ctx context.Context, id uuid.UUID) (*domain.Payment, error) {
			require.Equal(t, paymentID, id)
			return &domain.Payment{
				ID:     paymentID,
				Status: domain.PaymentStatusPending,
			}, nil
		},
		startApprovedPaymentProcessingFn: func(ctx context.Context, gotPaymentID uuid.UUID) (*domain.Payment, error) {
			require.Equal(t, paymentID, gotPaymentID)
			return nil, startErr
		},
	}, &fakeFraudChecker{
		t: t,
		checkFn: func(ctx context.Context, payment *domain.Payment) (FraudDecision, error) {
			require.Equal(t, paymentID, payment.ID)
			return FraudDecisionApproved, nil
		},
	})

	err := worker.HandleMessage(context.Background(), queue.PaymentMessage{
		PaymentID:     paymentID,
		Attempt:       1,
		CorrelationID: "correlation-1",
	})

	require.ErrorIs(t, err, startErr)
}

func TestFraudWorkerHandleMessageDropsUnrecognizedFraudDecision(t *testing.T) {
	paymentID := uuid.New()
	worker := newTestFraudWorker(t, &fakePaymentService{
		t: t,
		findPaymentByIDFn: func(ctx context.Context, id uuid.UUID) (*domain.Payment, error) {
			require.Equal(t, paymentID, id)
			return &domain.Payment{
				ID:     paymentID,
				Status: domain.PaymentStatusPending,
			}, nil
		},
	}, &fakeFraudChecker{
		t: t,
		checkFn: func(ctx context.Context, payment *domain.Payment) (FraudDecision, error) {
			require.Equal(t, paymentID, payment.ID)
			return FraudDecision("unknown"), nil
		},
	})

	err := worker.HandleMessage(context.Background(), queue.PaymentMessage{
		PaymentID:     paymentID,
		Attempt:       1,
		CorrelationID: "correlation-1",
	})

	require.NoError(t, err)
}
