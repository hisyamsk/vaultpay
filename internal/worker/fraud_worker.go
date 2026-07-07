package worker

import (
	"context"

	"github.com/hisyamsk/vaultpay/internal/queue"
)

func NewFraudWorker(s paymentService, f fraudChecker) *FraudWorker {
	return &FraudWorker{
		paymentService: s,
		fraudChecker:   f,
	}
}

func (w *FraudWorker) HandleMessage(ctx context.Context, msg queue.PaymentMessage) error {
	return nil
}
