package repository

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/hisyamsk/vaultpay/internal/domain"
	"github.com/stretchr/testify/require"
)

type paymentEventFixtureOptions struct {
	createdAt       time.Time
	publishAttempts int
	publishedAt     *time.Time
	lastAttemptedAt *time.Time
	lastError       *string
}

func createPaymentWithoutEvent(t *testing.T, ctx context.Context, db dbtx) (uuid.UUID, uuid.UUID, uuid.UUID) {
	t.Helper()

	senderID := createAccount(t, ctx, db, 2000)
	receiverID := createAccount(t, ctx, db, 1000)

	var paymentID uuid.UUID
	err := db.QueryRow(ctx, `
		INSERT INTO payments (amount, sender_id, receiver_id, idempotency_key)
		VALUES ($1, $2, $3, $4)
		RETURNING id
	`, 500, senderID, receiverID, "claim-events-"+uuid.NewString()).Scan(&paymentID)
	require.NoError(t, err)

	return paymentID, senderID, receiverID
}

func createPaymentEventFixture(t *testing.T, ctx context.Context, db dbtx, paymentID uuid.UUID, opts paymentEventFixtureOptions) domain.PaymentEvent {
	t.Helper()

	eventID := uuid.New()
	payload, err := json.Marshal(map[string]any{
		"event_id":    eventID,
		"event_type":  domain.PaymentEventTypeCreated,
		"payment_id":  paymentID,
		"attempt":     1,
		"occurred_at": opts.createdAt,
	})
	require.NoError(t, err)

	event := domain.PaymentEvent{
		EventID:         eventID,
		PaymentID:       paymentID,
		EventType:       domain.PaymentEventTypeCreated,
		Payload:         payload,
		CreatedAt:       opts.createdAt,
		PublishAttempts: opts.publishAttempts,
		PublishedAt:     opts.publishedAt,
		LastAttemptedAt: opts.lastAttemptedAt,
		LastError:       opts.lastError,
	}

	err = db.QueryRow(ctx, `
		INSERT INTO payment_events (
			event_id,
			payment_id,
			event_type,
			payload,
			created_at,
			publish_attempts,
			published_at,
			last_attempted_at,
			last_error
		)
		VALUES ($1, $2, $3, $4::jsonb, $5, $6, $7, $8, $9)
		RETURNING id
	`, event.EventID, event.PaymentID, event.EventType, string(event.Payload), event.CreatedAt,
		event.PublishAttempts, event.PublishedAt, event.LastAttemptedAt, event.LastError).Scan(&event.ID)
	require.NoError(t, err)

	return event
}

func TestPaymentEventRepository_ClaimUnpublishedClaimsOldestTenInStableOrder(t *testing.T) {
	paymentRepo, ctx := newTestRepo(t)
	eventRepo := NewPaymentEventRepository(paymentRepo.db)
	paymentID, _, _ := createPaymentWithoutEvent(t, ctx, paymentRepo.db)

	baseTime := time.Date(2026, time.July, 11, 10, 0, 0, 0, time.UTC)
	created := make([]domain.PaymentEvent, 0, 12)
	for i := range 12 {
		created = append(created, createPaymentEventFixture(t, ctx, paymentRepo.db, paymentID, paymentEventFixtureOptions{
			createdAt: baseTime.Add(time.Duration(i/2) * time.Second),
		}))
	}

	claimed, err := eventRepo.ClaimUnpublished(ctx, time.Now().Add(-time.Minute))
	require.NoError(t, err)
	require.Len(t, claimed, paymentEventClaimBatchSize)

	for i := range paymentEventClaimBatchSize {
		require.Equal(t, created[i].ID, claimed[i].ID)
		require.Equal(t, created[i].EventID, claimed[i].EventID)
		require.Equal(t, created[i].PaymentID, claimed[i].PaymentID)
		require.Equal(t, created[i].EventType, claimed[i].EventType)
		require.JSONEq(t, string(created[i].Payload), string(claimed[i].Payload))
		require.True(t, created[i].CreatedAt.Equal(claimed[i].CreatedAt))
		require.Equal(t, 1, claimed[i].PublishAttempts)
		require.NotNil(t, claimed[i].LastAttemptedAt)
		require.Nil(t, claimed[i].PublishedAt)
		require.Nil(t, claimed[i].LastError)

		var storedAttempts int
		var storedLastAttemptedAt *time.Time
		err := paymentRepo.db.QueryRow(ctx, `
			SELECT publish_attempts, last_attempted_at
			FROM payment_events
			WHERE id = $1
		`, claimed[i].ID).Scan(&storedAttempts, &storedLastAttemptedAt)
		require.NoError(t, err)
		require.Equal(t, claimed[i].PublishAttempts, storedAttempts)
		require.NotNil(t, storedLastAttemptedAt)
		require.WithinDuration(t, *claimed[i].LastAttemptedAt, *storedLastAttemptedAt, time.Microsecond)
	}

	for _, unclaimed := range created[paymentEventClaimBatchSize:] {
		var attempts int
		var lastAttemptedAt *time.Time
		err := paymentRepo.db.QueryRow(ctx, `
			SELECT publish_attempts, last_attempted_at
			FROM payment_events
			WHERE id = $1
		`, unclaimed.ID).Scan(&attempts, &lastAttemptedAt)
		require.NoError(t, err)
		require.Zero(t, attempts)
		require.Nil(t, lastAttemptedAt)
	}
}

func TestPaymentEventRepository_ClaimUnpublishedFiltersPublishedAndFreshClaims(t *testing.T) {
	paymentRepo, ctx := newTestRepo(t)
	eventRepo := NewPaymentEventRepository(paymentRepo.db)
	paymentID, _, _ := createPaymentWithoutEvent(t, ctx, paymentRepo.db)

	now := time.Now().UTC()
	claimCutoff := now.Add(-time.Minute)
	expiredAttempt := now.Add(-2 * time.Minute)
	freshAttempt := now.Add(-30 * time.Second)
	publishedAt := now.Add(-time.Second)
	lastError := "previous broker failure"

	neverClaimed := createPaymentEventFixture(t, ctx, paymentRepo.db, paymentID, paymentEventFixtureOptions{
		createdAt: now.Add(-4 * time.Minute),
	})
	expired := createPaymentEventFixture(t, ctx, paymentRepo.db, paymentID, paymentEventFixtureOptions{
		createdAt:       now.Add(-3 * time.Minute),
		publishAttempts: 2,
		lastAttemptedAt: &expiredAttempt,
		lastError:       &lastError,
	})
	fresh := createPaymentEventFixture(t, ctx, paymentRepo.db, paymentID, paymentEventFixtureOptions{
		createdAt:       now.Add(-2 * time.Minute),
		publishAttempts: 1,
		lastAttemptedAt: &freshAttempt,
	})
	published := createPaymentEventFixture(t, ctx, paymentRepo.db, paymentID, paymentEventFixtureOptions{
		createdAt:       now.Add(-time.Minute),
		publishAttempts: 1,
		publishedAt:     &publishedAt,
		lastAttemptedAt: &expiredAttempt,
	})

	claimed, err := eventRepo.ClaimUnpublished(ctx, claimCutoff)
	require.NoError(t, err)
	require.Len(t, claimed, 2)
	require.Equal(t, neverClaimed.EventID, claimed[0].EventID)
	require.Equal(t, 1, claimed[0].PublishAttempts)
	require.Equal(t, expired.EventID, claimed[1].EventID)
	require.Equal(t, 3, claimed[1].PublishAttempts)
	require.Equal(t, &lastError, claimed[1].LastError)

	for _, unchanged := range []domain.PaymentEvent{fresh, published} {
		var attempts int
		var lastAttemptedAt *time.Time
		err := paymentRepo.db.QueryRow(ctx, `
			SELECT publish_attempts, last_attempted_at
			FROM payment_events
			WHERE id = $1
		`, unchanged.ID).Scan(&attempts, &lastAttemptedAt)
		require.NoError(t, err)
		require.Equal(t, unchanged.PublishAttempts, attempts)
		require.NotNil(t, lastAttemptedAt)
		require.WithinDuration(t, *unchanged.LastAttemptedAt, *lastAttemptedAt, time.Microsecond)
	}
}

func TestPaymentEventRepository_ClaimUnpublishedReclaimsOnlyAfterLeaseExpires(t *testing.T) {
	paymentRepo, ctx := newTestRepo(t)
	eventRepo := NewPaymentEventRepository(paymentRepo.db)
	paymentID, _, _ := createPaymentWithoutEvent(t, ctx, paymentRepo.db)

	now := time.Now().UTC()
	previousAttempt := now.Add(-30 * time.Second)
	event := createPaymentEventFixture(t, ctx, paymentRepo.db, paymentID, paymentEventFixtureOptions{
		createdAt:       now.Add(-time.Minute),
		publishAttempts: 1,
		lastAttemptedAt: &previousAttempt,
	})

	claimed, err := eventRepo.ClaimUnpublished(ctx, now.Add(-time.Minute))
	require.NoError(t, err)
	require.Empty(t, claimed)

	claimed, err = eventRepo.ClaimUnpublished(ctx, now)
	require.NoError(t, err)
	require.Len(t, claimed, 1)
	require.Equal(t, event.EventID, claimed[0].EventID)
	require.Equal(t, 2, claimed[0].PublishAttempts)
	require.NotNil(t, claimed[0].LastAttemptedAt)
	require.True(t, claimed[0].LastAttemptedAt.After(previousAttempt))
}

func TestPaymentEventRepository_ClaimUnpublishedUsesExclusiveLeaseCutoff(t *testing.T) {
	paymentRepo, ctx := newTestRepo(t)
	eventRepo := NewPaymentEventRepository(paymentRepo.db)
	paymentID, _, _ := createPaymentWithoutEvent(t, ctx, paymentRepo.db)

	cutoff := time.Now().UTC().Add(-time.Minute)
	justExpired := cutoff.Add(-time.Microsecond)
	atCutoff := createPaymentEventFixture(t, ctx, paymentRepo.db, paymentID, paymentEventFixtureOptions{
		createdAt:       cutoff.Add(-2 * time.Minute),
		publishAttempts: 1,
		lastAttemptedAt: &cutoff,
	})
	expired := createPaymentEventFixture(t, ctx, paymentRepo.db, paymentID, paymentEventFixtureOptions{
		createdAt:       cutoff.Add(-time.Minute),
		publishAttempts: 1,
		lastAttemptedAt: &justExpired,
	})

	claimed, err := eventRepo.ClaimUnpublished(ctx, cutoff)
	require.NoError(t, err)
	require.Len(t, claimed, 1)
	require.Equal(t, expired.EventID, claimed[0].EventID)
	require.Equal(t, 2, claimed[0].PublishAttempts)

	var attempts int
	err = paymentRepo.db.QueryRow(ctx, `
		SELECT publish_attempts
		FROM payment_events
		WHERE id = $1
	`, atCutoff.ID).Scan(&attempts)
	require.NoError(t, err)
	require.Equal(t, 1, attempts)
}

func TestPaymentEventRepository_ClaimUnpublishedHonorsCanceledContext(t *testing.T) {
	paymentRepo, ctx := newTestRepo(t)
	eventRepo := NewPaymentEventRepository(paymentRepo.db)
	paymentID, _, _ := createPaymentWithoutEvent(t, ctx, paymentRepo.db)
	event := createPaymentEventFixture(t, ctx, paymentRepo.db, paymentID, paymentEventFixtureOptions{
		createdAt: time.Now().UTC().Add(-time.Minute),
	})

	canceledCtx, cancel := context.WithCancel(ctx)
	cancel()

	claimed, err := eventRepo.ClaimUnpublished(canceledCtx, time.Now())
	require.Error(t, err)
	require.True(t, errors.Is(err, context.Canceled))
	require.Nil(t, claimed)

	var attempts int
	var lastAttemptedAt *time.Time
	err = paymentRepo.db.QueryRow(ctx, `
		SELECT publish_attempts, last_attempted_at
		FROM payment_events
		WHERE id = $1
	`, event.ID).Scan(&attempts, &lastAttemptedAt)
	require.NoError(t, err)
	require.Zero(t, attempts)
	require.Nil(t, lastAttemptedAt)
}

func TestPaymentEventRepository_ClaimUnpublishedSeparateClaimsAreDisjoint(t *testing.T) {
	ctx := context.Background()
	tx, err := testPool.Begin(ctx)
	require.NoError(t, err)

	paymentID, senderID, receiverID := createPaymentWithoutEvent(t, ctx, tx)
	fixtureIDs := make(map[uuid.UUID]struct{}, paymentEventClaimBatchSize*2)
	baseTime := time.Date(2000, time.January, 1, 0, 0, 0, 0, time.UTC)
	for i := range paymentEventClaimBatchSize * 2 {
		event := createPaymentEventFixture(t, ctx, tx, paymentID, paymentEventFixtureOptions{
			createdAt: baseTime.Add(time.Duration(i) * time.Second),
		})
		fixtureIDs[event.EventID] = struct{}{}
	}
	require.NoError(t, tx.Commit(ctx))

	t.Cleanup(func() {
		cleanupCtx := context.Background()
		_, _ = testPool.Exec(cleanupCtx, `DELETE FROM payment_events WHERE payment_id = $1`, paymentID)
		_, _ = testPool.Exec(cleanupCtx, `DELETE FROM payments WHERE id = $1`, paymentID)
		_, _ = testPool.Exec(cleanupCtx, `DELETE FROM accounts WHERE id = ANY($1)`, []uuid.UUID{senderID, receiverID})
	})

	type claimResult struct {
		events []domain.PaymentEvent
		err    error
	}

	start := make(chan struct{})
	results := make(chan claimResult, 2)
	claim := func() {
		<-start
		events, err := NewPaymentEventRepository(testPool).ClaimUnpublished(ctx, time.Now().Add(-time.Minute))
		results <- claimResult{events: events, err: err}
	}

	go claim()
	go claim()
	close(start)

	first := <-results
	second := <-results
	require.NoError(t, first.err)
	require.NoError(t, second.err)
	require.Len(t, first.events, paymentEventClaimBatchSize)
	require.Len(t, second.events, paymentEventClaimBatchSize)

	claimedIDs := make(map[uuid.UUID]struct{}, paymentEventClaimBatchSize*2)
	for _, event := range append(first.events, second.events...) {
		_, isFixture := fixtureIDs[event.EventID]
		require.True(t, isFixture, "claimed unexpected event %s", event.EventID)
		_, duplicate := claimedIDs[event.EventID]
		require.False(t, duplicate, "event %s was claimed twice", event.EventID)
		claimedIDs[event.EventID] = struct{}{}
	}
	require.Equal(t, fixtureIDs, claimedIDs)
}

func TestPaymentEventRepository_ClaimUnpublishedReturnsEmptyWhenNothingIsEligible(t *testing.T) {
	paymentRepo, ctx := newTestRepo(t)
	eventRepo := NewPaymentEventRepository(paymentRepo.db)
	paymentID, _, _ := createPaymentWithoutEvent(t, ctx, paymentRepo.db)
	now := time.Now().UTC()

	createPaymentEventFixture(t, ctx, paymentRepo.db, paymentID, paymentEventFixtureOptions{
		createdAt:       now.Add(-time.Minute),
		publishAttempts: 1,
		publishedAt:     &now,
		lastAttemptedAt: &now,
	})

	claimed, err := eventRepo.ClaimUnpublished(ctx, now.Add(-time.Minute))
	require.NoError(t, err)
	require.Empty(t, claimed)
}
