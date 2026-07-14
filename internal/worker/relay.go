package worker

import (
	"context"
	"errors"
	"fmt"
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

type Relay struct {
	eventsRepo PaymentEventRepository
	publisher  PaymentEventPublisher
	claimLease time.Duration
	now        func() time.Time
}

func NewRelay(events PaymentEventRepository, publisher PaymentEventPublisher, claimLease time.Duration) *Relay {
	return &Relay{
		eventsRepo: events,
		publisher:  publisher,
		claimLease: claimLease,
		now:        func() time.Time { return time.Now().UTC() },
	}
}

func (r *Relay) RunOnce(ctx context.Context) error {
	now := r.now()
	leaseExpiredBefore := now.Add(-r.claimLease)

	unpublishedEvents, err := r.eventsRepo.ClaimUnpublished(ctx, leaseExpiredBefore)
	if err != nil {
		return fmt.Errorf("relay claim unpublished events: %w", err)
	}

	for _, event := range unpublishedEvents {
		err := r.publisher.Publish(ctx, event)
		if err != nil {
			dbErr := r.eventsRepo.RecordPublishFailure(ctx, event.EventID, err.Error())

			if dbErr != nil {
				return fmt.Errorf("relay publish and record failure: %w", errors.Join(dbErr, err))
			}

			return fmt.Errorf("relay publish event: %w", err)
		}

		err = r.eventsRepo.MarkPublished(ctx, event.EventID, r.now())
		if err != nil {
			return fmt.Errorf("relay mark published event: %w", err)
		}
	}

	return nil
}
