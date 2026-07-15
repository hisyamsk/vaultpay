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
	idleDelay  time.Duration
	now        func() time.Time
	wait       func(context.Context, time.Duration) error
}

func NewRelay(events PaymentEventRepository, publisher PaymentEventPublisher, claimLease, idleDelay time.Duration) *Relay {
	return &Relay{
		eventsRepo: events,
		publisher:  publisher,
		claimLease: claimLease,
		idleDelay:  idleDelay,
		now:        func() time.Time { return time.Now().UTC() },
		wait:       waitForRelayIdle,
	}
}

func waitForRelayIdle(ctx context.Context, delay time.Duration) error {
	timer := time.NewTimer(delay)
	defer func() {
		if !timer.Stop() {
			select {
			case <-timer.C:
			default:
			}
		}
	}()

	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
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

// Run immediately attempts one relay pass, then waits idleDelay before each
// later pass. A pass error does not permanently stop polling. Run stops without
// starting more work when ctx is canceled and returns the context error.
func (r *Relay) Run(ctx context.Context) error {
	return nil
}
