# Fraud Worker Flow Plan

## Goal

Build the fraud worker slice.

The fraud worker consumes payment messages for newly created pending payments, runs fraud screening, then moves the payment into the correct next state:

```txt
pending -> rejected
pending -> processing
```

If fraud fails, reject the payment before money moves.
If fraud passes, call `StartApprovedPaymentProcessing`, which atomically debits the sender and moves the payment to `processing`.

Core pattern: **idempotent worker**.

The queue may deliver the same message more than once, so the worker must always treat the database as the source of truth and make duplicate delivery safe.

## Proposed Structure

Add only the packages/files needed for this slice:

```txt
internal/
  worker/
    fraud_worker.go
    types.go

  queue/
    message.go

  external/
    fraud_checker.go

cmd/
  worker/
    main.go
```

Keep the first version simple. RabbitMQ wiring can be added after the worker behavior is tested with fakes.

## Message Shape

Use small, stable messages:

```json
{
  "payment_id": "uuid",
  "attempt": 1,
  "correlation_id": "uuid-or-string"
}
```

Do not put the full payment object in the queue.

Why: queue messages can be delayed or redelivered. The payment row in Postgres is the current truth.

## Interfaces

Define interfaces near the worker package:

```go
type paymentService interface {
    FindPaymentByID(ctx context.Context, id uuid.UUID) (*domain.Payment, error) // add only if needed
    RejectPendingPayment(ctx context.Context, paymentID uuid.UUID) error
    StartApprovedPaymentProcessing(ctx context.Context, paymentID uuid.UUID) (*domain.Payment, error)
}

type fraudChecker interface {
    Check(ctx context.Context, payment *domain.Payment) (FraudDecision, error)
}
```

You may need to add a service method to load a payment by ID.

Why: the worker should inspect current DB state before doing work. The queue message only tells it which payment to process.

## Fraud Decision

Keep fraud results explicit:

```txt
approved
rejected
```

Avoid boolean-only naming like `true/false` because it becomes unclear whether `true` means safe, risky, allowed, or blocked.

For now, the fake fraud checker can be deterministic:

```txt
amount over threshold -> rejected
otherwise -> approved
```

Do not use randomness in tests.

## Worker Flow

For each message:

1. Decode and validate message.
2. Load payment from DB by `payment_id`.
3. If payment is not `pending`, treat message as stale/duplicate and ack.
4. Run fraud checker.
5. If fraud rejected, call `RejectPendingPayment`.
6. If fraud approved, call `StartApprovedPaymentProcessing`.
7. Ack only after the service call succeeds.

Pseudo-flow:

```txt
message received
  -> validate payment_id
  -> load current payment
  -> if status != pending: ack/no-op
  -> fraud check
  -> rejected: RejectPendingPayment
  -> approved: StartApprovedPaymentProcessing
  -> ack
```

Why: this keeps external fraud work outside database transactions, while the service/repository still owns the atomic state change.

## Status Handling

Worker behavior by current payment status:

```txt
pending     -> run fraud check
processing  -> no-op, ack
completed   -> no-op, ack
failed      -> no-op, ack
rejected    -> no-op, ack
```

Pattern: **stale message guard**.

If a message is redelivered after another worker already processed it, the current DB status decides whether the message is still relevant.

## Error Handling

Classify errors simply:

```txt
invalid message       -> log and ack or send to DLQ
payment not found     -> log and ack or DLQ, depending on desired strictness
fraud checker error   -> retry/nack
service repo error    -> retry/nack
context canceled      -> stop/return
```

For the take-home, a reasonable first version:

- invalid JSON / invalid UUID: log and ack
- payment not found: log and ack
- fraud checker error: return error so caller can retry
- service error: return error so caller can retry

Why: malformed messages will not become valid by retrying. Temporary infrastructure errors might.

## Ack/Nack Rule

Ack only after the database state change succeeds.

```txt
fraud rejected + RejectPendingPayment success -> ack
fraud approved + StartApprovedPaymentProcessing success -> ack
service error -> do not ack
```

Why: if the worker crashes after the DB commit but before ack, redelivery is safe because the service/repo methods are idempotent.

## Logging

Use `slog` with stable fields:

```txt
worker
payment_id
correlation_id
attempt
decision
status
error
duration_ms
```

Keep logs useful but not noisy. Log one meaningful line per processed message outcome.

Do not log secrets or full raw messages.

## Tests

Start with worker unit tests using fake service and fake fraud checker.

Cover:

- invalid payment ID is handled without calling fraud checker
- missing payment returns/handles error according to your chosen policy
- non-pending payment is no-op and does not call fraud checker
- pending + fraud approved calls `StartApprovedPaymentProcessing`
- pending + fraud rejected calls `RejectPendingPayment`
- fraud checker error is returned for retry
- service error is returned for retry
- duplicate delivery is safe because non-pending status no-ops

Do not mock RabbitMQ first. Test the worker behavior around a plain message struct.

Pattern:

```txt
worker unit tests -> fake service + fake fraud checker
queue integration -> later
repository integration -> already covers money invariants
```

## Implementation Order

1. Add message type and validation.
2. Add service method to find payment by ID, if the worker needs it.
3. Add fraud checker interface and deterministic fake implementation.
4. Add fraud worker with a `HandleMessage(ctx, msg)` method.
5. Unit test `HandleMessage`.
6. Add RabbitMQ consumer wrapper after the worker behavior is correct.
7. Add `cmd/worker` wiring last.

Why this order: build the business behavior before transport wiring. RabbitMQ should deliver messages, not define your core worker logic.

## Out of Scope For This Step

Do not build these yet unless needed:

- payment processor worker
- notification worker
- complex retry backoff
- DLQ management UI
- metrics dashboards
- outbox pattern

Keep this slice focused: fraud decision -> payment state transition.

## Definition Of Done

This slice is done when:

- a pending payment message can be handled by the fraud worker
- approved fraud result starts processing and debits sender through the existing service/repo path
- rejected fraud result rejects the pending payment
- duplicate/stale messages are safe no-ops
- worker behavior is covered by unit tests
- message handling logs useful fields
- README or notes explain the fraud worker flow and idempotency behavior
