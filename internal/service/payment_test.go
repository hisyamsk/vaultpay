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
	createFn func(ctx context.Context, params repository.CreatePaymentParams) (*domain.Payment, error)
	findFn   func(ctx context.Context, idempotencyKey string) (*domain.Payment, error)
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
		IdempotencyKey: "idem-1",
	}
	expected := &domain.Payment{ID: uuid.New()}

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
			if params.IdempotencyKey != req.IdempotencyKey {
				t.Fatalf("expected idempotency key %q, got %q", req.IdempotencyKey, params.IdempotencyKey)
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
	existing := &domain.Payment{ID: uuid.New(), IdempotencyKey: req.IdempotencyKey}

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
