package worker

import (
	"context"
	"time"

	"github.com/google/uuid"
	"github.com/hisyamsk/vaultpay/internal/domain"
)

type PaymentEventPublisher interface {
	Publish(ctx context.Context, event domain.PaymentEvent) error
}

type PaymentEventRepository interface {
	ClaimUnpublished(ctx context.Context, leaseExpiredBefore time.Time) ([]domain.PaymentEvent, error)
	MarkPublished(ctx context.Context, eventID uuid.UUID, publishedAt time.Time) error
	RecordPublishFailure(ctx context.Context, eventID uuid.UUID, lastError string) error
}
