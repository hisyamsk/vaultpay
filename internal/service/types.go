package service

import (
	"context"
	"errors"

	"github.com/google/uuid"
	"github.com/hisyamsk/vaultpay/internal/domain"
	"github.com/hisyamsk/vaultpay/internal/repository"
)

type paymentRepository interface {
	Create(ctx context.Context, params repository.CreatePaymentParams) (*domain.Payment, error)
	FindByIdempotencyKey(ctx context.Context, idempotencyKey string) (*domain.Payment, error)
	FindById(ctx context.Context, id uuid.UUID) (*domain.Payment, error)
	UpdateStatus(ctx context.Context, id uuid.UUID, fromStatus domain.PaymentStatus, toStatus domain.PaymentStatus) error
}

var (
	ErrInvalidPaymentAmount           = errors.New("invalid payment amount")
	ErrInvalidPaymentSender           = errors.New("invalid payment sender")
	ErrInvalidPaymentReceiver         = errors.New("invalid payment receiver")
	ErrSameSenderAndReceiver          = errors.New("sender and receiver must differ")
	ErrMissingIdempotencyKey          = errors.New("missing idempotency key")
	ErrIdempotencyKeyConflict         = errors.New("idempotency key conflict")
	ErrInvalidPaymentStatusTransition = errors.New("invalid payment status transition")
)

type CreatePaymentRequest struct {
	Amount         int64
	SenderID       uuid.UUID
	ReceiverID     uuid.UUID
	IdempotencyKey string
	Description    *string
}

type PaymentService struct {
	repo paymentRepository
}

func (req *CreatePaymentRequest) samePaymentIntent(payment *domain.Payment) bool {
	return (req.SenderID == payment.SenderID &&
		req.ReceiverID == payment.ReceiverID &&
		req.Amount == payment.Amount)
}
