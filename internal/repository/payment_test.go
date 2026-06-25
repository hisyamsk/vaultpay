package repository

import (
	"context"
	"os"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/assert"
)

var testPool *pgxpool.Pool

func TestMain(m *testing.M) {
	ctx := context.Background()

	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		os.Exit(0)
	}

	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		panic(err)
	}

	testPool = pool
	code := m.Run()

	pool.Close()
	os.Exit(code)
}

func newTestRepo(t *testing.T) (*PaymentRepository, context.Context) {
	t.Helper()

	ctx := context.Background()

	tx, err := testPool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}

	t.Cleanup(func() {
		tx.Rollback(ctx)
	})

	return NewPaymentRepository(testPool), ctx
}

// seed data
func createAccount(t *testing.T, ctx context.Context, tx dbtx, balance int64) uuid.UUID {
	t.Helper()

	var id uuid.UUID
	err := tx.QueryRow(ctx, `
		INSERT INTO accounts (balance)
		VALUES ($1)
		RETURNING id
	`, balance).Scan(&id)
	if err != nil {
		t.Fatal(err)
	}

	return id
}

func TestFindById(t *testing.T) {
	repo, ctx := newTestRepo(t)

	senderID := createAccount(t, ctx, repo.db, 2000)
	receiverID := createAccount(t, ctx, repo.db, 1000)

	payment, err := repo.Create(ctx, CreatePaymentParams{
		Amount:         500,
		SenderID:       senderID,
		ReceiverID:     receiverID,
		IdempotencyKey: "idem-1",
	})

	assert.Nil(t, err)
	assert.Equal(t, payment.SenderID, senderID)
	assert.Equal(t, payment.ReceiverID, receiverID)
	assert.Equal(t, payment.IdempotencyKey, "idem-1")
	assert.Equal(t, payment.Amount, 500)
}
