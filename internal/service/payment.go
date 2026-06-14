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
	if !req.Currency.Valid() {
		return nil, ErrInvalidPaymentCurrency
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
	if strings.TrimSpace(req.IdempotencyKey) == "" {
		return nil, ErrMissingIdempotencyKey
	}

	repoParams := repository.CreatePaymentParams{
		Amount:         req.Amount,
		Currency:       req.Currency,
		SenderID:       req.SenderID,
		ReceiverID:     req.ReceiverID,
		IdempotencyKey: req.IdempotencyKey,
		Description:    req.Description,
	}

	p, err := s.repo.Create(ctx, repoParams)
	if err != nil {
		if errors.Is(err, repository.ErrDuplicateIdempotencyKey) {
			existing, err := s.repo.FindByIdempotencyKey(ctx, req.IdempotencyKey)
			if err != nil {
				return nil, fmt.Errorf("find payment by idempotency key: %w", err)
			}
			return existing, nil
		}
		return nil, fmt.Errorf("create payment: %w", err)
	}

	return p, nil
}
