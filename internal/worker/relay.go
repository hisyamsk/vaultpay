package worker

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
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
	logger     *slog.Logger
	now        func() time.Time
	wait       func(context.Context, time.Duration) error
}

func NewRelay(events PaymentEventRepository, publisher PaymentEventPublisher, claimLease, idleDelay time.Duration, logger *slog.Logger) (*Relay, error) {
	if claimLease <= 0 {
		return nil, fmt.Errorf("relay claim lease must be positive")
	}
	if idleDelay <= 0 {
		return nil, fmt.Errorf("relay idle delay must be positive")
	}
	if logger == nil {
		logger = slog.Default()
	}

	return &Relay{
		eventsRepo: events,
		publisher:  publisher,
		claimLease: claimLease,
		idleDelay:  idleDelay,
		logger:     logger,
		now:        func() time.Time { return time.Now().UTC() },
		wait:       waitForRelayIdle,
	}, nil
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
		r.logger.ErrorContext(ctx, "relay failed to claim unpublished events",
			slog.String("result", "failed"),
			slog.Any("error", err),
			slog.Duration("duration", r.now().Sub(now)),
		)
		return fmt.Errorf("relay claim unpublished events: %w", err)
	}

	for _, event := range unpublishedEvents {
		startedAt := r.now()
		err := r.publisher.Publish(ctx, event)
		if err != nil {
			dbErr := r.eventsRepo.RecordPublishFailure(ctx, event.EventID, err.Error())

			if dbErr != nil {
				combinedErr := errors.Join(dbErr, err)
				r.logEventResult(ctx, event, "failed", combinedErr, startedAt)
				return fmt.Errorf("relay publish and record failure: %w", combinedErr)
			}

			r.logEventResult(ctx, event, "failed", err, startedAt)
			return fmt.Errorf("relay publish event: %w", err)
		}

		err = r.eventsRepo.MarkPublished(ctx, event.EventID, r.now())
		if err != nil {
			r.logEventResult(ctx, event, "failed", err, startedAt)
			return fmt.Errorf("relay mark published event: %w", err)
		}

		r.logEventResult(ctx, event, "published", nil, startedAt)
	}

	return nil
}

func (r *Relay) logEventResult(ctx context.Context, event domain.PaymentEvent, result string, err error, startedAt time.Time) {
	attributes := []any{
		slog.String("event_id", event.EventID.String()),
		slog.String("payment_id", event.PaymentID.String()),
		slog.String("event_type", string(event.EventType)),
		slog.Int("publish_attempts", event.PublishAttempts),
		slog.String("result", result),
		slog.Any("error", err),
		slog.Duration("duration", r.now().Sub(startedAt)),
	}

	if err != nil {
		r.logger.ErrorContext(ctx, "relay payment event publish failed", attributes...)
		return
	}
	r.logger.InfoContext(ctx, "relay payment event published", attributes...)
}

// Run immediately attempts one relay pass, then waits idleDelay before each
// later pass. A pass error does not permanently stop polling. Run stops without
// starting more work when ctx is canceled and returns the context error.
func (r *Relay) Run(ctx context.Context) error {
	for {
		if err := ctx.Err(); err != nil {
			return err
		}

		if err := r.RunOnce(ctx); err != nil {
			if ctxErr := ctx.Err(); ctxErr != nil {
				return ctxErr
			}
		}

		if err := r.wait(ctx, r.idleDelay); err != nil {
			return err
		}
	}
}
