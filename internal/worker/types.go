package worker

import (
	"context"
	"log/slog"

	"github.com/google/uuid"
	"github.com/hisyamsk/vaultpay/internal/domain"
)

type FraudWorker struct {
	paymentService paymentService
	fraudChecker   fraudChecker
	logger         *slog.Logger
}

type FraudDecision string

const (
	FraudDecisionApproved FraudDecision = "approve"
	FraudDecisionRejected FraudDecision = "rejected"
)

type paymentService interface {
	FindPaymentByID(ctx context.Context, id uuid.UUID) (*domain.Payment, error)
	RejectPendingPayment(ctx context.Context, paymentID uuid.UUID) (*domain.Payment, error)
	StartApprovedPaymentProcessing(ctx context.Context, paymentID uuid.UUID) (*domain.Payment, error)
}

type fraudChecker interface {
	Check(ctx context.Context, payment *domain.Payment) (FraudDecision, error)
}
