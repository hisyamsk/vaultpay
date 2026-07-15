package worker

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/hisyamsk/vaultpay/internal/domain"
	"github.com/stretchr/testify/require"
)

type relayMarkedEvent struct {
	eventID     uuid.UUID
	publishedAt time.Time
}

type relayPublishFailure struct {
	eventID   uuid.UUID
	lastError string
}

type fakeRelayEventRepository struct {
	events           []domain.PaymentEvent
	claimResults     [][]domain.PaymentEvent
	claimErr         error
	claimErrors      []error
	markErr          error
	markErrors       []error
	recordFailureErr error
	claimCalls       int
	leaseCutoff      time.Time
	leaseCutoffs     []time.Time
	markedEvents     []relayMarkedEvent
	publishFailures  []relayPublishFailure
}

func (r *fakeRelayEventRepository) ClaimUnpublished(_ context.Context, leaseExpiredBefore time.Time) ([]domain.PaymentEvent, error) {
	r.claimCalls++
	r.leaseCutoff = leaseExpiredBefore
	r.leaseCutoffs = append(r.leaseCutoffs, leaseExpiredBefore)
	if r.claimErr != nil {
		return nil, r.claimErr
	}
	if len(r.claimErrors) >= r.claimCalls && r.claimErrors[r.claimCalls-1] != nil {
		return nil, r.claimErrors[r.claimCalls-1]
	}
	if len(r.claimResults) >= r.claimCalls {
		return r.claimResults[r.claimCalls-1], nil
	}
	return r.events, nil
}

func (r *fakeRelayEventRepository) MarkPublished(_ context.Context, eventID uuid.UUID, publishedAt time.Time) error {
	markCall := len(r.markedEvents)
	r.markedEvents = append(r.markedEvents, relayMarkedEvent{
		eventID:     eventID,
		publishedAt: publishedAt,
	})
	if len(r.markErrors) > markCall {
		return r.markErrors[markCall]
	}
	return r.markErr
}

func (r *fakeRelayEventRepository) RecordPublishFailure(_ context.Context, eventID uuid.UUID, lastError string) error {
	r.publishFailures = append(r.publishFailures, relayPublishFailure{
		eventID:   eventID,
		lastError: lastError,
	})
	return r.recordFailureErr
}

type fakeRelayPublisher struct {
	err       error
	published []domain.PaymentEvent
}

const testRelayIdleDelay = 500 * time.Millisecond

func (p *fakeRelayPublisher) Publish(_ context.Context, event domain.PaymentEvent) error {
	p.published = append(p.published, event)
	return p.err
}

func newRelayContract(t *testing.T, events PaymentEventRepository, publisher PaymentEventPublisher, claimLease time.Duration, now time.Time) *Relay {
	t.Helper()

	relay := NewRelay(events, publisher, claimLease, testRelayIdleDelay)
	relay.now = func() time.Time { return now }
	return relay
}

func TestRelayRunOnceMarksConfirmedEventPublished(t *testing.T) {
	now := time.Date(2026, time.July, 14, 8, 30, 0, 0, time.UTC)
	claimLease := 30 * time.Second
	event := domain.PaymentEvent{
		EventID:   uuid.MustParse("11111111-1111-1111-1111-111111111111"),
		EventType: domain.PaymentEventTypeCreated,
		Payload:   []byte(`{"event_id":"11111111-1111-1111-1111-111111111111"}`),
	}
	repository := &fakeRelayEventRepository{events: []domain.PaymentEvent{event}}
	publisher := &fakeRelayPublisher{}
	relay := newRelayContract(t, repository, publisher, claimLease, now)

	err := relay.RunOnce(context.Background())

	require.NoError(t, err)
	require.Equal(t, 1, repository.claimCalls)
	require.Equal(t, now.Add(-claimLease), repository.leaseCutoff)
	require.Equal(t, []domain.PaymentEvent{event}, publisher.published)
	require.Equal(t, []relayMarkedEvent{{eventID: event.EventID, publishedAt: now}}, repository.markedEvents)
	require.Empty(t, repository.publishFailures)
}

func TestRelayRunOnceRecordsPublisherFailureWithoutMarkingPublished(t *testing.T) {
	now := time.Date(2026, time.July, 14, 8, 30, 0, 0, time.UTC)
	publishErr := errors.New("RabbitMQ confirmation timed out")
	event := domain.PaymentEvent{
		EventID:   uuid.MustParse("22222222-2222-2222-2222-222222222222"),
		EventType: domain.PaymentEventTypeCreated,
		Payload:   []byte(`{"event_id":"22222222-2222-2222-2222-222222222222"}`),
	}
	repository := &fakeRelayEventRepository{events: []domain.PaymentEvent{event}}
	publisher := &fakeRelayPublisher{err: publishErr}
	relay := newRelayContract(t, repository, publisher, 30*time.Second, now)

	err := relay.RunOnce(context.Background())

	require.ErrorIs(t, err, publishErr)
	require.Equal(t, []domain.PaymentEvent{event}, publisher.published)
	require.Empty(t, repository.markedEvents)
	require.Equal(t, []relayPublishFailure{{eventID: event.EventID, lastError: publishErr.Error()}}, repository.publishFailures)
}

func TestRelayRunOnceReturnsClaimErrorWithoutPublishing(t *testing.T) {
	claimErr := errors.New("claim unavailable")
	repository := &fakeRelayEventRepository{claimErr: claimErr}
	publisher := &fakeRelayPublisher{}
	relay := newRelayContract(t, repository, publisher, 30*time.Second, time.Date(2026, time.July, 14, 8, 30, 0, 0, time.UTC))

	err := relay.RunOnce(context.Background())

	require.ErrorIs(t, err, claimErr)
	require.Empty(t, publisher.published)
	require.Empty(t, repository.markedEvents)
	require.Empty(t, repository.publishFailures)
}

func TestRelayRunOnceReturnsMarkErrorAfterConfirmedPublish(t *testing.T) {
	now := time.Date(2026, time.July, 14, 8, 30, 0, 0, time.UTC)
	markErr := errors.New("database unavailable while marking published")
	event := domain.PaymentEvent{
		EventID:   uuid.MustParse("33333333-3333-3333-3333-333333333333"),
		EventType: domain.PaymentEventTypeCreated,
		Payload:   []byte(`{"event_id":"33333333-3333-3333-3333-333333333333"}`),
	}
	repository := &fakeRelayEventRepository{
		events:  []domain.PaymentEvent{event},
		markErr: markErr,
	}
	publisher := &fakeRelayPublisher{}
	relay := newRelayContract(t, repository, publisher, 30*time.Second, now)

	err := relay.RunOnce(context.Background())

	require.ErrorIs(t, err, markErr)
	require.Equal(t, []domain.PaymentEvent{event}, publisher.published)
	require.Equal(t, []relayMarkedEvent{{eventID: event.EventID, publishedAt: now}}, repository.markedEvents)
	require.Empty(t, repository.publishFailures)
}

func TestRelayRunOnceReturnsRecordFailureErrorWithoutMarkingPublished(t *testing.T) {
	now := time.Date(2026, time.July, 14, 8, 30, 0, 0, time.UTC)
	publishErr := errors.New("publisher channel closed")
	recordErr := errors.New("database unavailable while recording publish failure")
	event := domain.PaymentEvent{
		EventID:   uuid.MustParse("44444444-4444-4444-4444-444444444444"),
		EventType: domain.PaymentEventTypeCreated,
		Payload:   []byte(`{"event_id":"44444444-4444-4444-4444-444444444444"}`),
	}
	repository := &fakeRelayEventRepository{
		events:           []domain.PaymentEvent{event},
		recordFailureErr: recordErr,
	}
	publisher := &fakeRelayPublisher{err: publishErr}
	relay := newRelayContract(t, repository, publisher, 30*time.Second, now)

	err := relay.RunOnce(context.Background())

	require.ErrorIs(t, err, publishErr)
	require.ErrorIs(t, err, recordErr)
	require.Equal(t, []domain.PaymentEvent{event}, publisher.published)
	require.Empty(t, repository.markedEvents)
	require.Equal(t, []relayPublishFailure{{eventID: event.EventID, lastError: publishErr.Error()}}, repository.publishFailures)
}

func TestRelayRunOnceDoesNotRepublishEventMarkedPublishedByPreviousPass(t *testing.T) {
	now := time.Date(2026, time.July, 14, 8, 30, 0, 0, time.UTC)
	event := domain.PaymentEvent{
		EventID:   uuid.MustParse("55555555-5555-5555-5555-555555555555"),
		EventType: domain.PaymentEventTypeCreated,
		Payload:   []byte(`{"event_id":"55555555-5555-5555-5555-555555555555"}`),
	}
	repository := &fakeRelayEventRepository{
		claimResults: [][]domain.PaymentEvent{
			{event},
			nil,
		},
	}
	publisher := &fakeRelayPublisher{}
	relay := newRelayContract(t, repository, publisher, 30*time.Second, now)

	require.NoError(t, relay.RunOnce(context.Background()))
	require.NoError(t, relay.RunOnce(context.Background()))

	require.Equal(t, 2, repository.claimCalls)
	require.Equal(t, []domain.PaymentEvent{event}, publisher.published)
	require.Equal(t, []relayMarkedEvent{{eventID: event.EventID, publishedAt: now}}, repository.markedEvents)
	require.Empty(t, repository.publishFailures)
}

func TestRelayRunOnceCanRepublishAfterConfirmationSucceededButMarkFailed(t *testing.T) {
	firstPassTime := time.Date(2026, time.July, 14, 8, 30, 0, 0, time.UTC)
	claimLease := 30 * time.Second
	markErr := errors.New("database unavailable after RabbitMQ confirmation")
	event := domain.PaymentEvent{
		EventID:   uuid.MustParse("66666666-6666-6666-6666-666666666666"),
		EventType: domain.PaymentEventTypeCreated,
		Payload:   []byte(`{"event_id":"66666666-6666-6666-6666-666666666666"}`),
	}
	repository := &fakeRelayEventRepository{
		claimResults: [][]domain.PaymentEvent{
			{event},
			{event},
		},
		markErrors: []error{markErr, nil},
	}
	publisher := &fakeRelayPublisher{}
	relay := NewRelay(repository, publisher, claimLease, testRelayIdleDelay)
	currentTime := firstPassTime
	relay.now = func() time.Time { return currentTime }

	err := relay.RunOnce(context.Background())
	require.ErrorIs(t, err, markErr)

	currentTime = firstPassTime.Add(claimLease + time.Second)
	require.NoError(t, relay.RunOnce(context.Background()))

	require.Equal(t, []time.Time{
		firstPassTime.Add(-claimLease),
		currentTime.Add(-claimLease),
	}, repository.leaseCutoffs)
	require.Equal(t, []domain.PaymentEvent{event, event}, publisher.published)
	require.Equal(t, []relayMarkedEvent{
		{eventID: event.EventID, publishedAt: firstPassTime},
		{eventID: event.EventID, publishedAt: currentTime},
	}, repository.markedEvents)
	require.Empty(t, repository.publishFailures)
}

func TestRelayRunAttemptsImmediatelyThenWaitsConfiguredIdleDelay(t *testing.T) {
	repository := &fakeRelayEventRepository{}
	publisher := &fakeRelayPublisher{}
	relay := newRelayContract(t, repository, publisher, 30*time.Second, time.Date(2026, time.July, 15, 8, 30, 0, 0, time.UTC))
	ctx, cancel := context.WithCancel(context.Background())
	var waited []time.Duration
	relay.wait = func(ctx context.Context, delay time.Duration) error {
		waited = append(waited, delay)
		cancel()
		return ctx.Err()
	}

	err := relay.Run(ctx)

	require.ErrorIs(t, err, context.Canceled)
	require.Equal(t, 1, repository.claimCalls)
	require.Equal(t, []time.Duration{testRelayIdleDelay}, waited)
}

func TestRelayRunContinuesAfterPassError(t *testing.T) {
	claimErr := errors.New("temporary claim failure")
	repository := &fakeRelayEventRepository{
		claimErrors: []error{claimErr, nil},
	}
	publisher := &fakeRelayPublisher{}
	relay := newRelayContract(t, repository, publisher, 30*time.Second, time.Date(2026, time.July, 15, 8, 30, 0, 0, time.UTC))
	ctx, cancel := context.WithCancel(context.Background())
	waitCalls := 0
	relay.wait = func(ctx context.Context, delay time.Duration) error {
		require.Equal(t, testRelayIdleDelay, delay)
		waitCalls++
		if waitCalls == 2 {
			cancel()
			return ctx.Err()
		}
		return nil
	}

	err := relay.Run(ctx)

	require.ErrorIs(t, err, context.Canceled)
	require.Equal(t, 2, repository.claimCalls)
	require.Equal(t, 2, waitCalls)
	require.Empty(t, publisher.published)
}

func TestRelayRunDoesNoWorkWhenContextIsAlreadyCanceled(t *testing.T) {
	repository := &fakeRelayEventRepository{}
	publisher := &fakeRelayPublisher{}
	relay := newRelayContract(t, repository, publisher, 30*time.Second, time.Date(2026, time.July, 15, 8, 30, 0, 0, time.UTC))
	waitCalls := 0
	relay.wait = func(context.Context, time.Duration) error {
		waitCalls++
		return nil
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	err := relay.Run(ctx)

	require.ErrorIs(t, err, context.Canceled)
	require.Equal(t, 0, repository.claimCalls)
	require.Equal(t, 0, waitCalls)
	require.Empty(t, publisher.published)
}

func TestRelayRunRejectsNonPositiveIdleDelayWithoutPolling(t *testing.T) {
	tests := []struct {
		name      string
		idleDelay time.Duration
	}{
		{name: "zero", idleDelay: 0},
		{name: "negative", idleDelay: -time.Second},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			repository := &fakeRelayEventRepository{}
			publisher := &fakeRelayPublisher{}
			relay := NewRelay(repository, publisher, 30*time.Second, tt.idleDelay)

			err := relay.Run(context.Background())

			require.Error(t, err)
			require.Equal(t, 0, repository.claimCalls)
			require.Empty(t, publisher.published)
		})
	}
}
