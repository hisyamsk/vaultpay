package external

import (
	"context"

	"github.com/hisyamsk/vaultpay/internal/domain"
	"github.com/hisyamsk/vaultpay/internal/worker"
)

type FraudChecker struct {
	RejectAmountAbove int64
}

func (f FraudChecker) Check(ctx context.Context, payment *domain.Payment) (worker.FraudDecision, error) {
	if payment.Amount > f.RejectAmountAbove {
		return worker.FraudDecisionRejected, nil
	}

	return worker.FraudDecisionApproved, nil
}
