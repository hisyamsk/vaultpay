package worker

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

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
	FailProcessedPayment(ctx context.Context, paymentID uuid.UUID, errorCode string) (*domain.Payment, error)
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
	startedAt := time.Now()

	payment, err := w.paymentService.FindPaymentByID(ctx, msg.PaymentID)
	if err != nil {
		if errors.Is(err, repository.ErrPaymentNotFound) {
			w.logger.WarnContext(ctx, "dropping transfer finalization for missing payment",
				slog.String("worker", "transfer_finalizer"),
				slog.String("event_id", msg.EventID.String()),
				slog.String("payment_id", msg.PaymentID.String()),
				slog.Int("attempt", msg.Attempt),
				slog.Duration("duration", time.Since(startedAt)),
			)
			return nil
		}

		w.logger.ErrorContext(ctx, "failed to load payment for transfer finalization",
			slog.String("worker", "transfer_finalizer"),
			slog.String("event_id", msg.EventID.String()),
			slog.String("payment_id", msg.PaymentID.String()),
			slog.Int("attempt", msg.Attempt),
			slog.Any("error", err),
			slog.Duration("duration", time.Since(startedAt)),
		)
		return fmt.Errorf("finalize internal transfer find payment: %w", err)
	}

	if payment.Status != domain.PaymentStatusProcessing {
		w.logger.InfoContext(ctx, "skipping stale transfer finalization",
			slog.String("worker", "transfer_finalizer"),
			slog.String("event_id", msg.EventID.String()),
			slog.String("payment_id", payment.ID.String()),
			slog.Int("attempt", msg.Attempt),
			slog.String("status", string(payment.Status)),
			slog.Duration("duration", time.Since(startedAt)),
		)
		return nil
	}

	payment, err = w.paymentService.CompleteProcessedPayment(ctx, payment.ID)
	if err != nil {
		w.logger.ErrorContext(ctx, "failed to complete internal transfer",
			slog.String("worker", "transfer_finalizer"),
			slog.String("event_id", msg.EventID.String()),
			slog.String("payment_id", msg.PaymentID.String()),
			slog.Int("attempt", msg.Attempt),
			slog.String("status", string(domain.PaymentStatusProcessing)),
			slog.Any("error", err),
			slog.Duration("duration", time.Since(startedAt)),
		)
		return fmt.Errorf("finalize internal transfer complete processed payment: %w", err)
	}

	if payment == nil {
		w.logger.ErrorContext(ctx, "payment service returned nil after transfer finalization",
			slog.String("worker", "transfer_finalizer"),
			slog.String("event_id", msg.EventID.String()),
			slog.String("payment_id", msg.PaymentID.String()),
			slog.Int("attempt", msg.Attempt),
			slog.Duration("duration", time.Since(startedAt)),
		)
		return errors.New("finalize internal transfer: payment service returned nil payment")
	}

	w.logger.InfoContext(ctx, "finalized internal transfer",
		slog.String("worker", "transfer_finalizer"),
		slog.String("event_id", msg.EventID.String()),
		slog.String("payment_id", payment.ID.String()),
		slog.Int("attempt", msg.Attempt),
		slog.String("status", string(payment.Status)),
		slog.Duration("duration", time.Since(startedAt)),
	)

	return nil
}
