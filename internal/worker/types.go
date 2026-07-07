package worker

import (
	"context"
	"errors"

	"github.com/google/uuid"
	"github.com/hisyamsk/vaultpay/internal/domain"
)

type FraudWorker struct {
	paymentService paymentService
	fraudChecker   fraudChecker
}

type FraudDecision string

const (
	FraudDecisionApproved FraudDecision = "approve"
	FraudDecisionRejected FraudDecision = "rejected"
)

type paymentService interface {
	FindPaymentByID(ctx context.Context, id uuid.UUID) (*domain.Payment, error) // add only if needed
	RejectPendingPayment(ctx context.Context, paymentID uuid.UUID) error
	StartApprovedPaymentProcessing(ctx context.Context, paymentID uuid.UUID) (*domain.Payment, error)
}

type fraudChecker interface {
	Check(ctx context.Context, payment *domain.Payment) (FraudDecision, error)
}

var ErrInvalidPaymentID = errors.New("invalid payment id")
