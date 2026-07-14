package worker

import (
	"context"

	"github.com/hisyamsk/vaultpay/internal/domain"
)

type PaymentEventPublisher interface {
	Publish(ctx context.Context, event domain.PaymentEvent) error
}
