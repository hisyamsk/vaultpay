# VaultPay

VaultPay is an in-progress payment processing pipeline written in Go. It models an
asynchronous account-to-account payment with PostgreSQL, RabbitMQ, idempotent
workers, atomic ledger updates, a transactional outbox, and basic reconciliation.

The immediate goal is a small, working portfolio project that can be submitted
with job applications. Production-oriented improvements will be added
incrementally after the first complete version.

> **Status:** payment creation, state transitions, atomic balance movements, and
> transport-independent fraud behavior are implemented. RabbitMQ wiring, the
> outbox relay, the processor worker, and reconciliation are the remaining work
> for the initial version.

See [REQUIREMENTS.md](REQUIREMENTS.md) for the initial-version acceptance criteria.

## Initial Scope

The first portfolio version will demonstrate:

- asynchronous payment creation
- idempotent API requests and worker processing
- guarded payment state transitions
- atomic balance and append-only ledger updates
- transactional outbox publication
- RabbitMQ manual acknowledgements, bounded retry, and a DLQ
- deterministic fraud behavior and internal transfer finalization
- read-only internal reconciliation
- structured logs and focused correctness tests

This version uses the existing simple ledger model. It does not attempt to model a
complete banking ledger or a real payment service provider.

## Architecture

```mermaid
flowchart LR
    Client --> API[HTTP API]
    API -->|payment + event| DB[(PostgreSQL)]
    DB --> Relay[Outbox relay]
    Relay -->|publisher confirm| MQ[(RabbitMQ)]
    MQ --> Fraud[Fraud worker]
    MQ --> Processor[Processor worker]
    MQ --> Notification[Notification worker]
    Fraud --> DB
    Processor --> DB
    Recon[Reconciliation command] --> DB
```

PostgreSQL is the source of truth. A payment change and its next outbox event are
committed in one database transaction. The relay publishes committed events to
RabbitMQ and marks them published only after broker confirmation.

RabbitMQ delivery is at least once. Workers reload the payment from PostgreSQL,
treat stale messages as successful no-ops, commit database work before
acknowledging, and remain safe when the same message is delivered again.

## Payment Flow

```txt
POST /payments
  -> pending + payment.created outbox event
  -> fraud worker
       -> rejected
       -> processing + sender debit + payment.processing event
  -> processor worker
       -> completed + receiver credit
       -> failed + sender refund
  -> terminal event
  -> notification worker logs the result
```

The fraud checker is deterministic so tests and demos produce repeatable results.
Money movement is internal and no external payment processor is called.

## Reconciliation V1

The first reconciliation command is deliberately read-only. It reports:

- terminal payment states that do not match their expected ledger entries
- pending or rejected payments that unexpectedly moved money
- payments stuck in `pending` or `processing` beyond a threshold
- outbox events that remain unpublished beyond a threshold

It does not repair data automatically. This keeps reconciliation observable and
safe while still demonstrating the control used to detect pipeline gaps.

## Current Status

| Area | Status |
|---|---|
| `POST /api/v1/payments` | Implemented |
| Idempotency conflict handling | Implemented |
| Payment state machine | Implemented |
| Atomic debit, credit, and refund operations | Implemented |
| Append-only movement entries and duplicate guards | Implemented |
| Fraud worker core behavior | Implemented |
| Basic `/health` endpoint | Implemented |
| `GET /api/v1/payments/{id}` | Initial-version work |
| Transactional outbox writes | Initial-version work |
| RabbitMQ relay and topology | Initial-version work |
| Fraud RabbitMQ consumer wiring | Initial-version work |
| Internal transfer finalizer worker | Initial-version work |
| Notification logging worker | Optional initial-version work |
| Read-only reconciliation command | Initial-version work |

## Initial Implementation Order

1. Insert `payment.created` into the outbox in the payment creation transaction.
2. Add the RabbitMQ exchange, queues, relay, publisher confirms, and manual ack.
3. Wire the existing fraud worker and emit its next outbox event atomically.
4. Add the internal transfer finalizer using the existing completion service.
5. Add payment status lookup and the read-only reconciliation command.
6. Add one end-to-end duplicate-delivery test and update this status table.
7. Add the notification logging worker only if the critical flow is already solid.

## Repository Layout

```txt
cmd/
  api/                 HTTP API
  worker/              relay and selected queue consumer
  reconcile/           one-shot reconciliation command
db/
  migrations/          PostgreSQL schema
internal/
  app/                 dependency wiring
  config/              environment configuration
  domain/              payment and ledger types
  external/            deterministic fraud simulation
  handler/             HTTP transport
  queue/               messages and RabbitMQ adapter
  repository/          SQL and transaction boundaries
  service/             payment use cases
  worker/              transport-independent worker behavior
  reconciliation/      read-only consistency checks
```

Directories are added only when the corresponding slice is implemented.

## Running The Current API

### Prerequisites

- Go 1.25+
- Docker with Compose
- [`golang-migrate`](https://github.com/golang-migrate/migrate) CLI

```bash
docker compose up -d postgres

export DATABASE_URL='postgres://vaultpay:vaultpay_dev@localhost:5432/vaultpay?sslmode=disable'
make migrate-up

HTTP_ADDR=:8080 DATABASE_URL="$DATABASE_URL" go run ./cmd/api
```

Current endpoints:

```txt
GET  /health
POST /api/v1/payments
```

The API has no authentication and is for local demonstration only. Do not expose
it publicly or use real customer or payment data.

## Tests

Tests that do not require PostgreSQL:

```bash
go test ./internal/domain ./internal/service ./internal/handler ./internal/worker
```

Repository tests expect a local `vaultpay_test` database:

```bash
docker compose exec postgres createdb -U vaultpay vaultpay_test

export TEST_DATABASE_URL='postgres://vaultpay:vaultpay_dev@localhost:5432/vaultpay_test?sslmode=disable'
make migrate-test-up
go test ./...
```

## Deferred Improvements

These are valuable follow-up improvements, not initial-version requirements:

1. Convert movement entries into balanced double-entry journals with a clearing
   account and currency-aware accounts.
2. Integrate a real external payment rail and model ambiguous outcomes with a
   `requires_reconciliation` state.
3. Compare local payments with an external settlement report and persist
   reconciliation runs and discrepancies.
4. Add safe, narrowly scoped automatic reconciliation repair.
5. Move idempotency from the JSON body to an `Idempotency-Key` header and store a
   canonical request fingerprint.
6. Add notification delivery records, stronger deduplication, readiness checks,
   graceful shutdown, and a few operational metrics.
7. Add paginated account ledger reads and broader crash-boundary integration tests.

Authentication, card data, PCI compliance, KYC/AML, FX, fees, chargebacks,
merchant settlement, Kubernetes, and multi-region operation remain out of scope.

## Reliability Model

VaultPay does not claim exactly-once delivery. It uses at-least-once delivery with
idempotent business operations, database constraints, row locks, guarded status
transitions, and append-only ledger entries.

## Safety Notice

VaultPay is a personal educational project. It is not a production payment system
and must not be used for real funds or customer data.
