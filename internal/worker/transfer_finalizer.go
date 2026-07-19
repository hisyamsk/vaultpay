package worker

import (
	"context"
	"errors"
	"fmt"
	"log/slog"

	"github.com/google/uuid"
	"github.com/hisyamsk/vaultpay/internal/domain"
	"github.com/hisyamsk/vaultpay/internal/queue"
	"github.com/hisyamsk/vaultpay/internal/repository"
)

type TransferFinalizer struct {
	paymentService transferFinalizerPaymentService
	logger         *slog.Logger
}

type transferFinalizerPaymentService interface {
	FindPaymentByID(ctx context.Context, id uuid.UUID) (*domain.Payment, error)
	CompleteProcessedPayment(ctx context.Context, paymentID uuid.UUID) (*domain.Payment, error)
}

func NewTransferFinalizer(s transferFinalizerPaymentService, logger *slog.Logger) *TransferFinalizer {
	if logger == nil {
		logger = slog.Default()
	}

	return &TransferFinalizer{
		paymentService: s,
		logger:         logger,
	}
}

func (w *TransferFinalizer) HandleEvent(ctx context.Context, msg queue.PaymentEventMessage) error {
	payment, err := w.paymentService.FindPaymentByID(ctx, msg.PaymentID)
	if err != nil {
		if errors.Is(err, repository.ErrPaymentNotFound) {
			return nil
		}

		return fmt.Errorf("finalize internal transfer find payment: %w", err)
	}

	if payment.Status != domain.PaymentStatusProcessing {
		return nil
	}

	payment, err = w.paymentService.CompleteProcessedPayment(ctx, payment.ID)
	if err != nil {
		return fmt.Errorf("finalize internal transfer complete processed payment: %w", err)
	}

	if payment == nil {
		return errors.New("finalize internal transfer: payment service returned nil payment")
	}

	return nil
}
