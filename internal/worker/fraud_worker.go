package worker

import (
	"context"
	"errors"
	"fmt"
	"log/slog"

	"github.com/hisyamsk/vaultpay/internal/domain"
	"github.com/hisyamsk/vaultpay/internal/queue"
	"github.com/hisyamsk/vaultpay/internal/repository"
)

func NewFraudWorker(s paymentService, f fraudChecker, logger *slog.Logger) *FraudWorker {
	if logger == nil {
		logger = slog.Default()
	}

	return &FraudWorker{
		paymentService: s,
		fraudChecker:   f,
		logger:         logger,
	}
}

func (w *FraudWorker) HandleEvent(ctx context.Context, msg queue.PaymentEventMessage) error {
	payment, err := w.paymentService.FindPaymentByID(ctx, msg.PaymentID)
	if err != nil {
		if errors.Is(err, repository.ErrPaymentNotFound) {
			w.logger.WarnContext(ctx, "dropping fraud message for missing payment",
				slog.String("worker", "fraud"),
				slog.String("payment_id", msg.PaymentID.String()),
				slog.Int("attempt", msg.Attempt),
			)
			return nil
		}

		w.logger.ErrorContext(ctx, "failed to load payment for fraud check",
			slog.String("worker", "fraud"),
			slog.String("payment_id", msg.PaymentID.String()),
			slog.Int("attempt", msg.Attempt),
			slog.Any("error", err),
		)
		return fmt.Errorf("fraud worker handle message: %w", err)
	}

	if payment.Status != domain.PaymentStatusPending {
		w.logger.InfoContext(ctx, "skipping stale fraud message",
			slog.String("worker", "fraud"),
			slog.String("payment_id", payment.ID.String()),
			slog.Int("attempt", msg.Attempt),
			slog.String("status", string(payment.Status)),
		)
		return nil
	}

	fraudDecision, err := w.fraudChecker.Check(ctx, payment)
	if err != nil {
		w.logger.ErrorContext(ctx, "fraud check failed",
			slog.String("worker", "fraud"),
			slog.String("payment_id", payment.ID.String()),
			slog.Int("attempt", msg.Attempt),
			slog.Any("error", err),
		)
		return fmt.Errorf("fraud worker handle message: %w", err)
	}

	resultPayment := payment
	switch fraudDecision {
	case FraudDecisionRejected:
		resultPayment, err = w.paymentService.RejectPendingPayment(ctx, payment.ID)
		if err != nil {
			w.logger.ErrorContext(ctx, "failed to reject fraud-flagged payment",
				slog.String("worker", "fraud"),
				slog.String("payment_id", payment.ID.String()),
				slog.Int("attempt", msg.Attempt),
				slog.String("decision", string(fraudDecision)),
				slog.Any("error", err),
			)
			return fmt.Errorf("fraud worker handle message: %w", err)
		}
	case FraudDecisionApproved:
		resultPayment, err = w.paymentService.StartApprovedPaymentProcessing(ctx, payment.ID)
		if err != nil {
			w.logger.ErrorContext(ctx, "failed to start approved payment processing",
				slog.String("worker", "fraud"),
				slog.String("payment_id", payment.ID.String()),
				slog.Int("attempt", msg.Attempt),
				slog.String("decision", string(fraudDecision)),
				slog.Any("error", err),
			)
			return fmt.Errorf("fraud worker handle message: %w", err)
		}
	default:
		w.logger.ErrorContext(ctx, "dropping fraud message with unrecognized decision",
			slog.String("worker", "fraud"),
			slog.String("payment_id", payment.ID.String()),
			slog.Int("attempt", msg.Attempt),
			slog.String("decision", string(fraudDecision)),
		)
		return nil
	}

	if resultPayment == nil {
		return fmt.Errorf("fraud worker handle message: payment service returned nil payment")
	}

	w.logger.InfoContext(ctx, "handled fraud message",
		slog.String("worker", "fraud"),
		slog.String("payment_id", resultPayment.ID.String()),
		slog.Int("attempt", msg.Attempt),
		slog.String("decision", string(fraudDecision)),
		slog.String("status", string(resultPayment.Status)),
	)
	return nil
}
