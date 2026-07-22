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
			w.logOutcome(ctx, slog.LevelWarn, "dropping transfer finalization for missing payment", msg, msg.PaymentID, "", startedAt)
			return nil
		}

		return fmt.Errorf("finalize internal transfer find payment: %w", err)
	}

	if payment.Status != domain.PaymentStatusProcessing {
		w.logOutcome(ctx, slog.LevelInfo, "skipping stale transfer finalization", msg, payment.ID, payment.Status, startedAt)
		return nil
	}

	completedPayment, err := w.paymentService.CompleteProcessedPayment(ctx, payment.ID)
	if err != nil {
		if errors.Is(err, repository.ErrAccountNotFound) {
			return w.failAndRefund(ctx, msg, payment.ID, startedAt)
		}

		return fmt.Errorf("finalize internal transfer complete processed payment: %w", err)
	}

	if completedPayment == nil {
		return errors.New("finalize internal transfer: payment service returned nil payment")
	}

	w.logOutcome(ctx, slog.LevelInfo, "finalized internal transfer", msg, completedPayment.ID, completedPayment.Status, startedAt)

	return nil
}

func (w *TransferFinalizer) failAndRefund(ctx context.Context, msg queue.PaymentEventMessage, paymentID uuid.UUID, startedAt time.Time) error {
	failedPayment, err := w.paymentService.FailProcessedPayment(
		ctx,
		paymentID,
		string(domain.ErrorCodeReceiverAccountNotFound),
	)
	if err != nil {
		return fmt.Errorf("finalize internal transfer fail processed payment receiver not found: %w", err)
	}
	if failedPayment == nil {
		return errors.New("finalize internal transfer failure/refund: payment service returned nil payment")
	}

	w.logOutcome(
		ctx,
		slog.LevelInfo,
		"failed internal transfer and refunded sender",
		msg,
		failedPayment.ID,
		failedPayment.Status,
		startedAt,
		slog.String("error_code", string(domain.ErrorCodeReceiverAccountNotFound)),
	)
	return nil
}

func (w *TransferFinalizer) logOutcome(
	ctx context.Context,
	level slog.Level,
	message string,
	msg queue.PaymentEventMessage,
	paymentID uuid.UUID,
	status domain.PaymentStatus,
	startedAt time.Time,
	extra ...slog.Attr,
) {
	attrs := []slog.Attr{
		slog.String("worker", "transfer_finalizer"),
		slog.String("event_id", msg.EventID.String()),
		slog.String("payment_id", paymentID.String()),
		slog.Int("attempt", msg.Attempt),
		slog.Duration("duration", time.Since(startedAt)),
	}
	if status != "" {
		attrs = append(attrs, slog.String("status", string(status)))
	}
	attrs = append(attrs, extra...)
	w.logger.LogAttrs(ctx, level, message, attrs...)
}
