package service

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"
	"github.com/hisyamsk/vaultpay/internal/domain"
	"github.com/hisyamsk/vaultpay/internal/repository"
)

type fakePaymentRepository struct {
	createFn                         func(ctx context.Context, params repository.CreatePaymentParams) (*domain.Payment, error)
	findFn                           func(ctx context.Context, idempotencyKey string) (*domain.Payment, error)
	findByIDFn                       func(ctx context.Context, id uuid.UUID) (*domain.Payment, error)
	updateStatusFn                   func(ctx context.Context, id uuid.UUID, fromStatus domain.PaymentStatus, toStatus domain.PaymentStatus) error
	startApprovedPaymentProcessingFn func(ctx context.Context, paymentID uuid.UUID) (*domain.Payment, error)
	completeProcessedPaymentFn       func(ctx context.Context, paymentID uuid.UUID) (*domain.Payment, error)
	failProcessedPaymentFn           func(ctx context.Context, paymentID uuid.UUID, errorCode string) (*domain.Payment, error)
}

func (f fakePaymentRepository) Create(ctx context.Context, params repository.CreatePaymentParams) (*domain.Payment, error) {
	if f.createFn == nil {
		return nil, errors.New("unexpected create call")
	}
	return f.createFn(ctx, params)
}

func (f fakePaymentRepository) FindByIdempotencyKey(ctx context.Context, idempotencyKey string) (*domain.Payment, error) {
	if f.findFn == nil {
		return nil, errors.New("unexpected find by idempotency key call")
	}
	return f.findFn(ctx, idempotencyKey)
}

func (f fakePaymentRepository) FindById(ctx context.Context, id uuid.UUID) (*domain.Payment, error) {
	if f.findByIDFn == nil {
		return nil, errors.New("unexpected find by id call")
	}
	return f.findByIDFn(ctx, id)
}

func (f fakePaymentRepository) UpdateStatus(ctx context.Context, id uuid.UUID, fromStatus domain.PaymentStatus, toStatus domain.PaymentStatus) error {
	if f.updateStatusFn == nil {
		return errors.New("unexpected update status call")
	}
	return f.updateStatusFn(ctx, id, fromStatus, toStatus)
}

func (f fakePaymentRepository) StartApprovedPaymentProcessing(ctx context.Context, paymentID uuid.UUID) (*domain.Payment, error) {
	if f.startApprovedPaymentProcessingFn == nil {
		return nil, errors.New("unexpected start approved payment processing call")
	}
	return f.startApprovedPaymentProcessingFn(ctx, paymentID)
}

func (f fakePaymentRepository) CompleteProcessedPayment(ctx context.Context, paymentID uuid.UUID) (*domain.Payment, error) {
	if f.completeProcessedPaymentFn == nil {
		return nil, errors.New("unexpected complete processed payment call")
	}
	return f.completeProcessedPaymentFn(ctx, paymentID)
}

func (f fakePaymentRepository) FailProcessedPayment(ctx context.Context, paymentID uuid.UUID, errorCode string) (*domain.Payment, error) {
	if f.failProcessedPaymentFn == nil {
		return nil, errors.New("unexpected fail processed payment call")
	}
	return f.failProcessedPaymentFn(ctx, paymentID, errorCode)
}

func TestCreatePaymentValidation(t *testing.T) {
	validReq := CreatePaymentRequest{
		Amount:         1000,
		SenderID:       uuid.New(),
		ReceiverID:     uuid.New(),
		IdempotencyKey: "idem-1",
	}

	tests := []struct {
		name    string
		mutate  func(*CreatePaymentRequest)
		wantErr error
	}{
		{
			name: "invalid amount",
			mutate: func(req *CreatePaymentRequest) {
				req.Amount = 0
			},
			wantErr: ErrInvalidPaymentAmount,
		},
		{
			name: "invalid sender",
			mutate: func(req *CreatePaymentRequest) {
				req.SenderID = uuid.Nil
			},
			wantErr: ErrInvalidPaymentSender,
		},
		{
			name: "invalid receiver",
			mutate: func(req *CreatePaymentRequest) {
				req.ReceiverID = uuid.Nil
			},
			wantErr: ErrInvalidPaymentReceiver,
		},
		{
			name: "same sender and receiver",
			mutate: func(req *CreatePaymentRequest) {
				req.ReceiverID = req.SenderID
			},
			wantErr: ErrSameSenderAndReceiver,
		},
		{
			name: "missing idempotency key",
			mutate: func(req *CreatePaymentRequest) {
				req.IdempotencyKey = "   "
			},
			wantErr: ErrMissingIdempotencyKey,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := validReq
			tt.mutate(&req)

			calledCreate := false
			svc := NewPaymentService(fakePaymentRepository{
				createFn: func(ctx context.Context, params repository.CreatePaymentParams) (*domain.Payment, error) {
					calledCreate = true
					return nil, nil
				},
			})

			payment, err := svc.CreatePayment(context.Background(), req)
			if payment != nil {
				t.Fatalf("expected nil payment, got %#v", payment)
			}
			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("expected error %v, got %v", tt.wantErr, err)
			}
			if calledCreate {
				t.Fatal("expected repository create not to be called")
			}
		})
	}
}

func TestCreatePaymentCreatesPayment(t *testing.T) {
	req := CreatePaymentRequest{
		Amount:         1000,
		SenderID:       uuid.New(),
		ReceiverID:     uuid.New(),
		IdempotencyKey: " idem-1 ",
	}
	expected := &domain.Payment{ID: uuid.New()}
	expectedIdempotencyKey := "idem-1"

	svc := NewPaymentService(fakePaymentRepository{
		createFn: func(ctx context.Context, params repository.CreatePaymentParams) (*domain.Payment, error) {
			if params.Amount != req.Amount {
				t.Fatalf("expected amount %d, got %d", req.Amount, params.Amount)
			}
			if params.SenderID != req.SenderID {
				t.Fatalf("expected sender ID %s, got %s", req.SenderID, params.SenderID)
			}
			if params.ReceiverID != req.ReceiverID {
				t.Fatalf("expected receiver ID %s, got %s", req.ReceiverID, params.ReceiverID)
			}
			if params.IdempotencyKey != expectedIdempotencyKey {
				t.Fatalf("expected idempotency key %q, got %q", expectedIdempotencyKey, params.IdempotencyKey)
			}
			return expected, nil
		},
	})

	payment, err := svc.CreatePayment(context.Background(), req)
	if err != nil {
		t.Fatalf("expected nil error, got %v", err)
	}
	if payment != expected {
		t.Fatalf("expected payment %#v, got %#v", expected, payment)
	}
}

func TestCreatePaymentReturnsExistingPaymentForDuplicateIdempotencyKey(t *testing.T) {
	req := CreatePaymentRequest{
		Amount:         1000,
		SenderID:       uuid.New(),
		ReceiverID:     uuid.New(),
		IdempotencyKey: "idem-1",
	}
	existing := &domain.Payment{
		ID:             uuid.New(),
		Amount:         req.Amount,
		SenderID:       req.SenderID,
		ReceiverID:     req.ReceiverID,
		IdempotencyKey: req.IdempotencyKey,
	}

	svc := NewPaymentService(fakePaymentRepository{
		createFn: func(ctx context.Context, params repository.CreatePaymentParams) (*domain.Payment, error) {
			return nil, repository.ErrDuplicateIdempotencyKey
		},
		findFn: func(ctx context.Context, idempotencyKey string) (*domain.Payment, error) {
			if idempotencyKey != req.IdempotencyKey {
				t.Fatalf("expected idempotency key %q, got %q", req.IdempotencyKey, idempotencyKey)
			}
			return existing, nil
		},
	})

	payment, err := svc.CreatePayment(context.Background(), req)
	if err != nil {
		t.Fatalf("expected nil error, got %v", err)
	}
	if payment != existing {
		t.Fatalf("expected existing payment %#v, got %#v", existing, payment)
	}
}

func TestCreatePaymentReturnsConflictForDuplicateIdempotencyKeyWithDifferentIntent(t *testing.T) {
	req := CreatePaymentRequest{
		Amount:         1000,
		SenderID:       uuid.New(),
		ReceiverID:     uuid.New(),
		IdempotencyKey: "idem-1",
	}
	existing := &domain.Payment{
		ID:             uuid.New(),
		Amount:         req.Amount + 1,
		SenderID:       req.SenderID,
		ReceiverID:     req.ReceiverID,
		IdempotencyKey: req.IdempotencyKey,
	}

	svc := NewPaymentService(fakePaymentRepository{
		createFn: func(ctx context.Context, params repository.CreatePaymentParams) (*domain.Payment, error) {
			return nil, repository.ErrDuplicateIdempotencyKey
		},
		findFn: func(ctx context.Context, idempotencyKey string) (*domain.Payment, error) {
			if idempotencyKey != req.IdempotencyKey {
				t.Fatalf("expected idempotency key %q, got %q", req.IdempotencyKey, idempotencyKey)
			}
			return existing, nil
		},
	})

	payment, err := svc.CreatePayment(context.Background(), req)
	if payment != nil {
		t.Fatalf("expected nil payment, got %#v", payment)
	}
	if !errors.Is(err, ErrIdempotencyKeyConflict) {
		t.Fatalf("expected error %v, got %v", ErrIdempotencyKeyConflict, err)
	}
}

func TestCreatePaymentWrapsRepositoryErrors(t *testing.T) {
	req := CreatePaymentRequest{
		Amount:         1000,
		SenderID:       uuid.New(),
		ReceiverID:     uuid.New(),
		IdempotencyKey: "idem-1",
	}
	repoErr := errors.New("db unavailable")

	svc := NewPaymentService(fakePaymentRepository{
		createFn: func(ctx context.Context, params repository.CreatePaymentParams) (*domain.Payment, error) {
			return nil, repoErr
		},
	})

	payment, err := svc.CreatePayment(context.Background(), req)
	if payment != nil {
		t.Fatalf("expected nil payment, got %#v", payment)
	}
	if !errors.Is(err, repoErr) {
		t.Fatalf("expected error to wrap %v, got %v", repoErr, err)
	}
}

func TestCreatePaymentWrapsFindByIdempotencyKeyErrors(t *testing.T) {
	req := CreatePaymentRequest{
		Amount:         1000,
		SenderID:       uuid.New(),
		ReceiverID:     uuid.New(),
		IdempotencyKey: "idem-1",
	}
	findErr := errors.New("lookup failed")

	svc := NewPaymentService(fakePaymentRepository{
		createFn: func(ctx context.Context, params repository.CreatePaymentParams) (*domain.Payment, error) {
			return nil, repository.ErrDuplicateIdempotencyKey
		},
		findFn: func(ctx context.Context, idempotencyKey string) (*domain.Payment, error) {
			return nil, findErr
		},
	})

	payment, err := svc.CreatePayment(context.Background(), req)
	if payment != nil {
		t.Fatalf("expected nil payment, got %#v", payment)
	}
	if !errors.Is(err, findErr) {
		t.Fatalf("expected error to wrap %v, got %v", findErr, err)
	}
}

func TestUpdatePaymentStatusUpdatesValidTransition(t *testing.T) {
	paymentID := uuid.New()

	svc := NewPaymentService(fakePaymentRepository{
		findByIDFn: func(ctx context.Context, id uuid.UUID) (*domain.Payment, error) {
			if id != paymentID {
				t.Fatalf("expected payment ID %s, got %s", paymentID, id)
			}
			return &domain.Payment{ID: paymentID, Status: domain.PaymentStatusPending}, nil
		},
		updateStatusFn: func(ctx context.Context, id uuid.UUID, fromStatus domain.PaymentStatus, toStatus domain.PaymentStatus) error {
			if id != paymentID {
				t.Fatalf("expected payment ID %s, got %s", paymentID, id)
			}
			if fromStatus != domain.PaymentStatusPending {
				t.Fatalf("expected from status %s, got %s", domain.PaymentStatusPending, fromStatus)
			}
			if toStatus != domain.PaymentStatusProcessing {
				t.Fatalf("expected to status %s, got %s", domain.PaymentStatusProcessing, toStatus)
			}
			return nil
		},
	})

	if err := svc.UpdatePaymentStatus(context.Background(), paymentID, domain.PaymentStatusProcessing); err != nil {
		t.Fatalf("expected nil error, got %v", err)
	}
}

func TestUpdatePaymentStatusRejectsInvalidTransition(t *testing.T) {
	paymentID := uuid.New()
	calledUpdate := false

	svc := NewPaymentService(fakePaymentRepository{
		findByIDFn: func(ctx context.Context, id uuid.UUID) (*domain.Payment, error) {
			return &domain.Payment{ID: paymentID, Status: domain.PaymentStatusCompleted}, nil
		},
		updateStatusFn: func(ctx context.Context, id uuid.UUID, fromStatus domain.PaymentStatus, toStatus domain.PaymentStatus) error {
			calledUpdate = true
			return nil
		},
	})

	err := svc.UpdatePaymentStatus(context.Background(), paymentID, domain.PaymentStatusFailed)
	if !errors.Is(err, ErrInvalidPaymentStatusTransition) {
		t.Fatalf("expected error %v, got %v", ErrInvalidPaymentStatusTransition, err)
	}
	if calledUpdate {
		t.Fatal("expected repository update not to be called")
	}
}

func TestUpdatePaymentStatusWrapsFindByIDErrors(t *testing.T) {
	paymentID := uuid.New()
	findErr := errors.New("lookup failed")

	svc := NewPaymentService(fakePaymentRepository{
		findByIDFn: func(ctx context.Context, id uuid.UUID) (*domain.Payment, error) {
			return nil, findErr
		},
	})

	err := svc.UpdatePaymentStatus(context.Background(), paymentID, domain.PaymentStatusProcessing)
	if !errors.Is(err, findErr) {
		t.Fatalf("expected error to wrap %v, got %v", findErr, err)
	}
}

func TestUpdatePaymentStatusWrapsUpdateStatusErrors(t *testing.T) {
	paymentID := uuid.New()
	updateErr := errors.New("update failed")

	svc := NewPaymentService(fakePaymentRepository{
		findByIDFn: func(ctx context.Context, id uuid.UUID) (*domain.Payment, error) {
			return &domain.Payment{ID: paymentID, Status: domain.PaymentStatusPending}, nil
		},
		updateStatusFn: func(ctx context.Context, id uuid.UUID, fromStatus domain.PaymentStatus, toStatus domain.PaymentStatus) error {
			return updateErr
		},
	})

	err := svc.UpdatePaymentStatus(context.Background(), paymentID, domain.PaymentStatusProcessing)
	if !errors.Is(err, updateErr) {
		t.Fatalf("expected error to wrap %v, got %v", updateErr, err)
	}
}
