package service

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/google/uuid"
	"github.com/hisyamsk/vaultpay/internal/domain"
	"github.com/hisyamsk/vaultpay/internal/repository"
)

func NewPaymentService(repo paymentRepository) *PaymentService {
	return &PaymentService{
		repo: repo,
	}
}

func (s *PaymentService) CreatePayment(ctx context.Context, req CreatePaymentRequest) (*domain.Payment, error) {
	if req.Amount <= 0 {
		return nil, ErrInvalidPaymentAmount
	}
	if req.SenderID == uuid.Nil {
		return nil, ErrInvalidPaymentSender
	}
	if req.ReceiverID == uuid.Nil {
		return nil, ErrInvalidPaymentReceiver
	}
	if req.SenderID == req.ReceiverID {
		return nil, ErrSameSenderAndReceiver
	}

	idempotencyKey := strings.TrimSpace(req.IdempotencyKey)
	if idempotencyKey == "" {
		return nil, ErrMissingIdempotencyKey
	}

	repoParams := repository.CreatePaymentParams{
		Amount:         req.Amount,
		SenderID:       req.SenderID,
		ReceiverID:     req.ReceiverID,
		IdempotencyKey: idempotencyKey,
		Description:    req.Description,
	}

	p, err := s.repo.Create(ctx, repoParams)
	if err == nil {
		return p, nil
	}

	if !errors.Is(err, repository.ErrDuplicateIdempotencyKey) {
		return nil, fmt.Errorf("create payment: %w", err)
	}

	existing, err := s.repo.FindByIdempotencyKey(ctx, idempotencyKey)
	if err != nil {
		return nil, fmt.Errorf("find payment by idempotency key: %w", err)
	}

	if req.samePaymentIntent(existing) {
		return existing, nil
	}
	return nil, ErrIdempotencyKeyConflict

}

func (s *PaymentService) UpdatePaymentStatus(ctx context.Context, paymentID uuid.UUID, nextStatus domain.PaymentStatus) error {
	if paymentID == uuid.Nil {
		return ErrInvalidPaymentID
	}

	payment, err := s.repo.FindById(ctx, paymentID)
	if err != nil {
		return fmt.Errorf("find payment by id: %w", err)
	}

	if !payment.Status.CanTransitionTo(nextStatus) {
		return ErrInvalidPaymentStatusTransition
	}

	if err := s.repo.UpdateStatus(ctx, paymentID, payment.Status, nextStatus); err != nil {
		return fmt.Errorf("update payment status: %w", err)
	}

	return nil
}

func (s *PaymentService) StartApprovedPaymentProcessing(ctx context.Context, paymentID uuid.UUID) (*domain.Payment, error) {
	if paymentID == uuid.Nil {
		return nil, ErrInvalidPaymentID
	}

	payment, err := s.repo.StartApprovedPaymentProcessing(ctx, paymentID)
	if err != nil {
		return nil, fmt.Errorf("start approved payment processing: %w", err)
	}

	return payment, nil
}
