# VaultPay Initial Version Requirements

## 1. Goal

Finish a small, working payment pipeline that is credible in a fintech job
application and can be explained end to end.

The initial version prioritizes a complete, correct, and reliable flow over broad
production features. Anything under **Deferred Improvements** is explicitly not
required before applying for jobs, but correctness requirements are never relaxed
to meet a schedule.

## 2. System Boundary

VaultPay models a payment between two internal accounts using:

- Go and `net/http`
- PostgreSQL as the source of truth
- RabbitMQ for asynchronous delivery
- deterministic fake fraud, processor, and notification integrations

Amounts are positive integer minor units in one implicit demo currency. The
system does not process real money or card data.

## 3. Required Invariants

The initial implementation must preserve:

1. An idempotency key creates at most one payment.
2. Replaying the same request returns the existing payment.
3. Reusing a key for a different sender, receiver, or amount returns a conflict.
4. Only valid payment state transitions are accepted.
5. Terminal states remain terminal.
6. Sender balance is deducted at most once per payment.
7. Receiver balance is credited at most once per payment.
8. Sender balance is refunded at most once per payment.
9. Balance changes and ledger inserts commit in the same transaction.
10. Ledger entries are append-only.
11. A payment mutation and its required outbox event commit in one transaction.
12. Duplicate or redelivered messages do not duplicate money movement.
13. PostgreSQL, not RabbitMQ or memory, decides the current payment state.

## 4. Payment States

Use the existing state model:

```txt
pending -> processing -> completed
    |           `------> failed
    `------------------> rejected
```

- `completed`, `failed`, and `rejected` are terminal.
- Insufficient funds may move a pending payment to `failed` without a ledger entry.
- Fraud rejection moves a pending payment to `rejected` without moving money.

## 5. HTTP API

### `POST /api/v1/payments`

Request:

```json
{
  "amount": 1250,
  "sender_id": "uuid",
  "receiver_id": "uuid",
  "idempotency_key": "client-generated-key",
  "description": "optional"
}
```

Required behavior:

- Validate IDs, amount, sender/receiver difference, and idempotency key.
- Limit the request body and reject unknown JSON fields.
- Insert the payment and `payment.created` outbox event atomically.
- Return immediately without waiting for fraud or processing.
- Return the existing payment for an exact idempotent replay.
- Return `409 Conflict` when the key is reused for a different payment intent.

The JSON idempotency key is retained for the initial version to avoid unnecessary API
rework. Moving it to an HTTP header is deferred.

### `GET /api/v1/payments/{payment_id}`

Return payment ID, amount, sender, receiver, status, safe error code, and
timestamps. Return `404` when the payment does not exist.

### `GET /health`

Keep the existing endpoint. A database-aware readiness endpoint is deferred.

## 6. Transactional Outbox

Whenever a committed payment change requires another worker to run, insert an
outbox event in the same PostgreSQL transaction.

Minimum event fields:

```json
{
  "event_id": "uuid",
  "event_type": "payment.created",
  "payment_id": "uuid",
  "attempt": 1,
  "occurred_at": "timestamp"
}
```

Minimum stored outbox metadata:

- unique event ID
- event type and payment ID
- JSON payload
- creation time
- published time, nullable
- publish attempt count
- last publish error, nullable

Relay behavior:

1. Read a small ordered batch of unpublished events.
2. Use `FOR UPDATE SKIP LOCKED` so multiple relays cannot claim one row together.
3. Publish a persistent RabbitMQ message with a bounded timeout.
4. Require a publisher confirmation.
5. Mark the event published only after confirmation.
6. Leave failed events available for a later retry.

A crash after publish but before marking the row can publish a duplicate. This is
expected and is handled by idempotent workers.

## 7. RabbitMQ

The minimum topology is:

- one durable topic exchange
- a fraud queue
- a processor queue
- a notification queue if the optional worker is completed
- one bounded retry path and a DLQ for exhausted or malformed messages

Use durable queues, persistent messages, publisher confirms, manual consumer
acknowledgements, and bounded prefetch.

Consumers acknowledge only after their required database transaction commits.
Malformed messages are not retried forever. Transient errors receive a small,
bounded number of retries without immediate hot-loop requeueing.

## 8. Fraud Worker

- Consume `payment.created` events.
- Load the current payment from PostgreSQL.
- If it is no longer `pending`, acknowledge as a stale successful no-op.
- Use the existing deterministic fraud checker.
- On rejection, atomically mark `rejected` and insert a terminal outbox event.
- On approval, atomically debit the sender, insert the debit ledger entry, move to
  `processing`, and insert `payment.processing` into the outbox.
- A duplicate event must not deduct funds twice.

## 9. Processor Worker

- Consume `payment.processing` events.
- Load the current payment from PostgreSQL.
- If it is no longer `processing`, acknowledge as a stale successful no-op.
- Use a deterministic fake outcome; do not use randomness in tests.
- On success, atomically credit the receiver, insert the credit ledger entry, move
  to `completed`, and insert `payment.completed` into the outbox.
- On definitive failure, atomically refund the sender, insert the refund ledger
  entry, move to `failed`, and insert `payment.failed` into the outbox.
- Duplicate delivery must not credit or refund twice.

The initial fake does not model a processor accepting a request while its response
is lost. Unknown external outcomes are an important deferred improvement.

## 10. Notification Worker

This worker is optional for the initial version and must not delay the critical flow.

If implemented:

- consume completed, failed, and rejected events
- log a structured notification rather than calling a real provider
- never modify payment or ledger outcome
- acknowledge after successful logging/recording
- cap retries and dead-letter final failures

## 11. Ledger Behavior

Keep the current append-only movement model:

- fraud approval: sender debit entry
- processor success: receiver credit entry
- processor failure after debit: sender refund entry

Use unique constraints and payment row locking to make each movement happen at
most once. Account balance updates and ledger inserts must remain in the same
transaction.

Balanced double-entry journals and a clearing account are deferred. Do not redesign
the ledger before the initial version works end to end.

## 12. Reconciliation V1

Add a one-shot `cmd/reconcile` command. It is read-only and may be run manually.

It reports at least:

- `completed` payment missing its sender debit or receiver credit
- processed `failed` payment missing its debit or refund
- `pending` or `rejected` payment with ledger movement
- payment stuck in `pending` or `processing` beyond a configurable threshold
- unpublished outbox event older than a configurable threshold

Output a structured summary and useful identifiers for each discrepancy. Do not
automatically change payments, account balances, ledger rows, or outbox events.

Persisted reconciliation runs, processor-report comparison, and automatic repair
are deferred.

## 13. Error And Retry Rules

Classify errors simply:

- malformed message or invalid input: reject or DLQ; no repeated retry
- duplicate or stale work: successful no-op
- temporary database, broker, or fake external error: bounded retry
- definitive fraud/processor outcome: apply the business transition
- exhausted retry: DLQ and structured error log

Wrap internal errors with `%w`. Do not return database or broker errors directly
to API clients.

## 14. Observability And Shutdown

Use `log/slog` structured logs with applicable fields:

```txt
payment_id event_id worker attempt status error duration_ms
```

API, relay, and worker binaries should stop on context cancellation, stop taking
new work, and close database and RabbitMQ connections. Advanced metrics are
deferred; useful logs are sufficient for the initial version.

## 15. Required Tests

Keep existing state, service, handler, and repository tests.

Add focused tests for:

- payment creation and outbox insertion commit or roll back together
- duplicate fraud delivery does not debit twice
- duplicate processor success does not credit twice
- duplicate processor failure does not refund twice
- outbox events are marked published only after confirmation
- reconciliation detects seeded lifecycle/ledger and stale-outbox discrepancies

Add one small integration test for the successful flow:

```txt
create -> outbox -> fraud -> outbox -> processor -> completed
```

Deliver at least one event twice in that test and verify final balances and ledger
entry counts remain correct.

## 16. Initial Version Definition Of Done

The first application-ready version is done when:

- the project starts locally with Docker Compose
- the API creates and retrieves a payment
- payment creation atomically creates its outbox event
- the relay publishes through RabbitMQ with confirmation
- fraud and processor consumers complete the asynchronous flow
- RabbitMQ messages are manually acknowledged after database commit
- duplicate messages preserve balances, ledger entries, and payment state
- bounded retry and a DLQ exist
- read-only reconciliation reports deliberately seeded discrepancies
- structured logs show the payment flow
- the README status and run instructions match the actual implementation
- core unit and integration tests pass deterministically

Notification delivery is not required if the critical payment flow, outbox, and
reconciliation are complete and tested.

## 17. Deferred Improvements

Add these incrementally after beginning job applications:

1. Balanced double-entry journals, clearing account, currency, and opening balance
   journals.
2. Processor idempotency records, status lookup, unknown outcomes, and a
   `requires_reconciliation` state.
3. Simulated processor settlement reports and persisted reconciliation runs/items.
4. Guarded automatic repair through existing payment services.
5. `Idempotency-Key` header and canonical request fingerprint.
6. Notification delivery records and consumer receipts.
7. Readiness checks, Prometheus metrics, paginated ledger history, and broader
   failure-boundary tests.

Out of scope for this project: real payments, card data, PCI compliance,
authentication, KYC/AML, FX, fees, chargebacks, merchant settlement, Kubernetes,
and multi-region architecture.
