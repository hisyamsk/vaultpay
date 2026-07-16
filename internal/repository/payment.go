package repository

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/hisyamsk/vaultpay/internal/domain"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

func NewPaymentRepository(db dbtx) *PaymentRepository {
	return &PaymentRepository{
		db: db,
	}
}

func (r *PaymentRepository) Create(ctx context.Context, params CreatePaymentParams) (*domain.Payment, error) {
	payment := &domain.Payment{}
	tx, err := r.db.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx)

	err = tx.QueryRow(ctx, `
		INSERT INTO payments (amount, sender_id, receiver_id, idempotency_key, description)
		VALUES ($1, $2, $3, $4, $5)
		RETURNING id, amount, sender_id, receiver_id, idempotency_key, status, error_code, description, created_at, updated_at
	`, params.Amount, params.SenderID, params.ReceiverID, params.IdempotencyKey, params.Description).Scan(
		&payment.ID, &payment.Amount, &payment.SenderID, &payment.ReceiverID, &payment.IdempotencyKey, &payment.Status,
		&payment.ErrorCode, &payment.Description, &payment.CreatedAt, &payment.UpdatedAt,
	)

	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) {
			if pgErr.Code == "23505" && pgErr.ConstraintName == "payments_idempotency_key_unique" {
				return nil, ErrDuplicateIdempotencyKey
			}
		}
		return nil, err
	}

	eventID, err := uuid.NewV7()
	if err != nil {
		return nil, err
	}
	payload, err := json.Marshal(domain.PaymentEventPayload{
		EventID:    eventID,
		EventType:  domain.PaymentEventTypeCreated,
		PaymentID:  payment.ID,
		Attempt:    1,
		OccurredAt: payment.CreatedAt,
	})
	if err != nil {
		return nil, err
	}

	_, err = tx.Exec(ctx, `
		INSERT INTO payment_events (event_id, payment_id, event_type, payload)
		VALUES ($1, $2, $3, $4)
	`, eventID, payment.ID, domain.PaymentEventTypeCreated, payload)

	if err != nil {
		return nil, err
	}

	err = tx.Commit(ctx)
	if err != nil {
		return nil, err
	}

	return payment, nil
}

func (r *PaymentRepository) FindByIdempotencyKey(ctx context.Context, idempotencyKey string) (*domain.Payment, error) {
	payment := &domain.Payment{}
	err := r.db.QueryRow(ctx, `
		SELECT id, amount, sender_id, receiver_id, idempotency_key, status, error_code, description, created_at, updated_at
		FROM payments
		WHERE idempotency_key = $1`, idempotencyKey).Scan(
		&payment.ID, &payment.Amount, &payment.SenderID, &payment.ReceiverID, &payment.IdempotencyKey, &payment.Status,
		&payment.ErrorCode, &payment.Description, &payment.CreatedAt, &payment.UpdatedAt,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrPaymentNotFound
		}
		return nil, err
	}

	return payment, nil
}

func (r *PaymentRepository) FindByID(ctx context.Context, id uuid.UUID) (*domain.Payment, error) {
	payment := &domain.Payment{}
	err := r.db.QueryRow(ctx, `
		SELECT id, amount, sender_id, receiver_id, idempotency_key, status, error_code, description, created_at, updated_at
		FROM payments
		WHERE id = $1`, id).Scan(
		&payment.ID, &payment.Amount, &payment.SenderID, &payment.ReceiverID, &payment.IdempotencyKey, &payment.Status,
		&payment.ErrorCode, &payment.Description, &payment.CreatedAt, &payment.UpdatedAt,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrPaymentNotFound
		}
		return nil, err
	}

	return payment, nil
}

func (r *PaymentRepository) UpdateStatus(ctx context.Context, id uuid.UUID, fromStatus domain.PaymentStatus, toStatus domain.PaymentStatus) error {
	tag, err := r.db.Exec(ctx, `
		UPDATE payments SET status = $1, updated_at = NOW()
		WHERE id = $2 AND status = $3;`, toStatus, id, fromStatus)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrPaymentStatusConflict
	}

	return nil
}

func scanPayment(row pgx.Row) (*domain.Payment, error) {
	payment := &domain.Payment{}
	err := row.Scan(
		&payment.ID, &payment.Amount, &payment.SenderID, &payment.ReceiverID, &payment.IdempotencyKey, &payment.Status,
		&payment.ErrorCode, &payment.Description, &payment.CreatedAt, &payment.UpdatedAt,
	)
	if err != nil {
		return nil, err
	}

	return payment, nil
}

// RejectPendingPayment locks paymentID and, when it is pending, changes it to
// rejected and inserts one payment.rejected outbox event in the same
// transaction. A payment already in any non-pending state is returned as a
// successful no-op without another event. Missing payments return
// ErrPaymentNotFound. Any failure rolls back both the status and event writes.
func (r *PaymentRepository) RejectPendingPayment(ctx context.Context, paymentID uuid.UUID) (*domain.Payment, error) {
	tx, err := r.db.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx)

	payment, err := scanPayment(tx.QueryRow(ctx, `
		SELECT id, amount, sender_id, receiver_id, idempotency_key,
			status, error_code, description, created_at, updated_at
		FROM payments
		WHERE id = $1
		FOR UPDATE
  `, paymentID))

	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrPaymentNotFound
		}

		return nil, err
	}

	switch payment.Status {
	case domain.PaymentStatusCompleted, domain.PaymentStatusFailed, domain.PaymentStatusProcessing, domain.PaymentStatusRejected:
		return payment, nil
	case domain.PaymentStatusPending:
		var updatedAt time.Time
		err = tx.QueryRow(ctx, `
			UPDATE payments
			SET status = $1, updated_at = NOW()
			WHERE id = $2
			RETURNING updated_at
		`, domain.PaymentStatusRejected, paymentID).Scan(&updatedAt)
		if err != nil {
			return nil, err
		}

		eventID, err := uuid.NewV7()
		if err != nil {
			return nil, err
		}
		payload, err := json.Marshal(domain.PaymentEventPayload{
			EventID:    eventID,
			EventType:  domain.PaymentEventTypeRejected,
			PaymentID:  payment.ID,
			Attempt:    1,
			OccurredAt: updatedAt,
		})
		if err != nil {
			return nil, err
		}

		_, err = tx.Exec(ctx, `
			INSERT INTO payment_events(event_id, payment_id, event_type, payload)
			VALUES($1, $2, $3, $4)
		`, eventID, paymentID, domain.PaymentEventTypeRejected, payload)
		if err != nil {
			return nil, err
		}

		err = tx.Commit(ctx)
		if err != nil {
			return nil, err
		}

		payment.UpdatedAt = updatedAt
		payment.Status = domain.PaymentStatusRejected
		return payment, nil
	}

	return nil, ErrUnrecognizedPaymentStatus
}

func (r *PaymentRepository) StartApprovedPaymentProcessing(ctx context.Context, paymentID uuid.UUID) (*domain.Payment, error) {
	tx, err := r.db.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx)

	payment, err := scanPayment(tx.QueryRow(ctx, `
		SELECT id, amount, sender_id, receiver_id, idempotency_key, status, error_code, description, created_at, updated_at
		FROM payments
		WHERE id = $1
		FOR UPDATE
	`, paymentID))

	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrPaymentNotFound
		}
		return nil, err
	}

	eventID, err := uuid.NewV7()
	if err != nil {
		return nil, err
	}

	payload := domain.PaymentEventPayload{
		EventID:   eventID,
		PaymentID: payment.ID,
		Attempt:   1,
	}

	switch payment.Status {
	case domain.PaymentStatusProcessing, domain.PaymentStatusCompleted, domain.PaymentStatusFailed, domain.PaymentStatusRejected:
		return payment, nil
	case domain.PaymentStatusPending:
		var balance int64
		err = tx.QueryRow(ctx, `
			SELECT balance 
			FROM accounts
			WHERE id = $1 
			FOR UPDATE
		`, payment.SenderID).Scan(&balance)
		if err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return nil, ErrAccountNotFound
			}
			return nil, err
		}

		err = tx.QueryRow(ctx, `
			UPDATE accounts
			SET balance = balance - $1, updated_at = NOW()
			WHERE id = $2 AND balance >= $1
			RETURNING balance
		`, payment.Amount, payment.SenderID).Scan(&balance)

		if err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				payment, err = scanPayment(tx.QueryRow(ctx, `
					UPDATE payments
					SET status = $1, error_code = $2, updated_at = NOW()
					WHERE id = $3
					RETURNING id, amount, sender_id, receiver_id, idempotency_key, status, error_code, description, created_at, updated_at
				`, domain.PaymentStatusFailed, domain.ErrorCodeInsufficientFunds, paymentID))

				if err != nil {
					return nil, err
				}

				payload.OccurredAt = payment.UpdatedAt
				payload.EventType = domain.PaymentEventTypeFailed

				jsonPayload, err := json.Marshal(payload)
				if err != nil {
					return nil, err
				}

				_, err = tx.Exec(ctx, `
					INSERT INTO payment_events (event_id, payment_id, event_type, payload)
					VALUES ($1, $2, $3, $4)
				`, eventID, paymentID, domain.PaymentEventTypeFailed, jsonPayload)
				if err != nil {
					return nil, err
				}

				if err := tx.Commit(ctx); err != nil {
					return nil, err
				}
				return payment, nil
			}
			return nil, err
		}

		_, err = tx.Exec(ctx, `
			INSERT INTO ledger_entries(payment_id, account_id, type, amount)
			VALUES($1, $2, $3, $4)
		`, paymentID, payment.SenderID, domain.LedgerEntryTypeDebit, payment.Amount)

		if err != nil {
			return nil, err
		}

		payment, err = scanPayment(tx.QueryRow(ctx, `
			UPDATE payments
			SET status = $1, updated_at = NOW()
			WHERE id = $2
			RETURNING id, amount, sender_id, receiver_id, idempotency_key, status, error_code, description, created_at, updated_at
		`, domain.PaymentStatusProcessing, paymentID))

		if err != nil {
			return nil, err
		}

		payload.OccurredAt = payment.UpdatedAt
		payload.EventType = domain.PaymentEventTypeProcessing
		jsonPayload, err := json.Marshal(payload)
		if err != nil {
			return nil, err
		}

		_, err = tx.Exec(ctx, `
			INSERT INTO payment_events (event_id, payment_id, event_type, payload)
			VALUES ($1, $2, $3, $4)
		`, eventID, paymentID, domain.PaymentEventTypeProcessing, jsonPayload)
		if err != nil {
			return nil, err
		}

		if err := tx.Commit(ctx); err != nil {
			return nil, err
		}
		return payment, nil
	}

	return nil, ErrUnrecognizedPaymentStatus
}

func (r *PaymentRepository) CompleteProcessedPayment(ctx context.Context, paymentID uuid.UUID) (*domain.Payment, error) {
	tx, err := r.db.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx)

	payment, err := scanPayment(tx.QueryRow(ctx, `
		SELECT id, amount, sender_id, receiver_id, idempotency_key, status, error_code, description, created_at, updated_at
		FROM payments
		WHERE id = $1
		FOR UPDATE
	`, paymentID))

	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrPaymentNotFound
		}
		return nil, err
	}

	switch payment.Status {
	case domain.PaymentStatusPending:
		return nil, ErrInvalidStatusTransition
	case domain.PaymentStatusCompleted, domain.PaymentStatusFailed, domain.PaymentStatusRejected:
		return payment, nil
	case domain.PaymentStatusProcessing:
		var balance int64
		err = tx.QueryRow(ctx, `
			UPDATE accounts
			SET balance = balance + $1, updated_at = NOW()
			WHERE id = $2
			RETURNING balance
		`, payment.Amount, payment.ReceiverID).Scan(&balance)

		if err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return nil, ErrAccountNotFound
			}
			return nil, err
		}

		_, err = tx.Exec(ctx, `
			INSERT INTO ledger_entries(payment_id, account_id, type, amount)
			VALUES($1, $2, $3, $4)
		`, paymentID, payment.ReceiverID, domain.LedgerEntryTypeCredit, payment.Amount)

		if err != nil {
			return nil, err
		}

		payment, err = scanPayment(tx.QueryRow(ctx, `
			UPDATE payments
			SET status = $1, updated_at = NOW()
			WHERE id = $2
			RETURNING id, amount, sender_id, receiver_id, idempotency_key, status, error_code, description, created_at, updated_at
		`, domain.PaymentStatusCompleted, paymentID))

		if err != nil {
			return nil, err
		}

		if err := tx.Commit(ctx); err != nil {
			return nil, err
		}
		return payment, nil
	}

	return nil, ErrUnrecognizedPaymentStatus
}

func (r *PaymentRepository) FailProcessedPayment(ctx context.Context, paymentID uuid.UUID, errorCode string) (*domain.Payment, error) {
	tx, err := r.db.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx)

	payment, err := scanPayment(tx.QueryRow(ctx, `
		SELECT id, amount, sender_id, receiver_id, idempotency_key, status, error_code, description, created_at, updated_at
		FROM payments
		WHERE id = $1
		FOR UPDATE
	`, paymentID))

	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrPaymentNotFound
		}

		return nil, err
	}

	switch payment.Status {
	case domain.PaymentStatusPending:
		return nil, ErrInvalidStatusTransition
	case domain.PaymentStatusCompleted, domain.PaymentStatusFailed, domain.PaymentStatusRejected:
		return payment, nil
	case domain.PaymentStatusProcessing:
		var balance int64
		err = tx.QueryRow(ctx, `
			UPDATE accounts
			SET balance = balance + $1, updated_at = NOW()
			WHERE id = $2
			RETURNING balance
		`, payment.Amount, payment.SenderID).Scan(&balance)

		if err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return nil, ErrAccountNotFound
			}

			return nil, err
		}

		_, err = tx.Exec(ctx, `
			INSERT INTO ledger_entries(payment_id, account_id, type, amount)
			VALUES($1, $2, $3, $4)
		`, paymentID, payment.SenderID, domain.LedgerEntryTypeRefund, payment.Amount)

		if err != nil {
			return nil, err
		}

		payment, err = scanPayment(tx.QueryRow(ctx, `
			UPDATE payments
			SET status = $1, error_code = $2, updated_at = NOW()
			WHERE id = $3
			RETURNING id, amount, sender_id, receiver_id, idempotency_key, status, error_code, description, created_at, updated_at
		`, domain.PaymentStatusFailed, errorCode, paymentID))

		if err != nil {
			return nil, err
		}

		if err := tx.Commit(ctx); err != nil {
			return nil, err
		}
		return payment, nil
	}

	return nil, ErrUnrecognizedPaymentStatus
}
