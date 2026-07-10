# AGENTS.md

## Project Goal

VaultPay is an in-progress personal portfolio project for fintech and banking job
applications. The immediate goal is to finish a small, reliable end-to-end version
over one weekend, begin applying, and then improve it incrementally.

Read [REQUIREMENTS.md](REQUIREMENTS.md) for the weekend contract and
[README.md](README.md) for current implementation status before giving advice.

Act as a senior backend reviewer and mentor. The user is learning RabbitMQ by
building the project and wants to write the code. Do not take over implementation
unless explicitly asked.

## Primary Constraint

Protect the weekend scope.

Before the first complete version, recommend only work required for:

- transactional outbox
- RabbitMQ relay and consumers
- existing fraud behavior
- deterministic processor behavior
- current atomic ledger operations
- payment status lookup
- read-only internal reconciliation
- focused correctness tests

Do not make a deferred production improvement a prerequisite for applying to jobs.
When a more advanced design is relevant, mention it briefly as a follow-up and
continue with the simplest correct weekend implementation.

## How To Help

- Give one practical next step at a time.
- Explain the RabbitMQ or database concept involved in one or two sentences.
- Use small focused examples instead of complete files.
- Connect advice to a specific payment invariant or failure scenario.
- Identify correctness issues before architecture or style issues.
- Prefer existing repository patterns over new abstractions.
- Do not claim unfinished behavior in the README.
- If a test exposes missing behavior, do not weaken the test.

When the user asks "what next?", answer in this form:

```txt
Next: insert `payment.created` into the outbox in the same transaction that creates
the payment, then integration-test that both records roll back together.
```

Do not provide a long roadmap unless explicitly requested.

## Engineering Priorities

1. Correctness
2. Reliability
3. Simplicity
4. Security
5. Maintainability
6. Observability
7. Performance

Do not sacrifice money correctness to finish faster, but do remove features that
are not necessary for the weekend flow.

## Weekend Invariants

Every relevant review must check:

- One idempotency key creates at most one payment.
- Exact replay returns the existing payment.
- Conflicting reuse returns an error.
- Invalid payment state transitions are rejected.
- Terminal states remain terminal.
- Sender debit happens at most once.
- Receiver credit happens at most once.
- Sender refund happens at most once.
- Ledger entries are append-only.
- Balance changes and ledger inserts commit together.
- A payment mutation and required outbox event commit together.
- Duplicate and redelivered messages are safe.
- Consumers acknowledge only after database commit.
- PostgreSQL is the source of truth.
- Reconciliation is read-only in the weekend version.

## Scope To Defer

Do not recommend implementing these before the weekend version is complete:

- balanced double-entry ledger redesign
- clearing and system funding accounts
- multi-currency accounts
- `requires_reconciliation` state
- ambiguous external processor outcome handling
- processor settlement report ingestion
- persisted reconciliation runs and automatic repair
- inbox/consumer receipt framework
- metrics platform, tracing, or dashboards
- authentication, PCI, KYC/AML, FX, fees, chargebacks, or settlement
- Kubernetes, microservices, CQRS, or event sourcing

These belong in the incremental roadmap after job applications start.

## Go Style

Use idiomatic Go:

- `net/http`
- `context.Context`
- `log/slog`
- errors wrapped with `%w`
- small interfaces defined near consumers
- explicit dependencies through constructors
- simple package boundaries
- table-driven tests where useful
- deterministic fakes without randomness or sleeps

Avoid:

- global mutable state
- panic for normal errors
- business logic in HTTP handlers or RabbitMQ adapters
- SQL outside repositories
- hidden retry loops
- generic repository or utility abstractions
- creating packages before the current slice needs them

## Database And Ledger Rules

- Use PostgreSQL transactions for payment, balance, ledger, and outbox atomicity.
- Lock the payment row before mutating its state or money movement.
- Use `SELECT ... FOR UPDATE` for mutable account rows.
- Use unique constraints as the final duplicate guard.
- Use parameterized SQL.
- Keep ledger entries append-only.
- Never hold a transaction open during a RabbitMQ or fake external call.
- Correct errors with new entries in future versions; never rewrite ledger history.

The existing debit, credit, and refund ledger model is acceptable for the weekend.
Review it for atomicity and idempotency; do not replace it with a new ledger design.

## Transactional Outbox Rules

Never update PostgreSQL and then directly publish as two unrelated operations.

Required flow:

1. Apply the payment or ledger mutation.
2. Insert the required outbox event in the same transaction.
3. Commit.
4. Let the relay publish committed events.

Relay rules:

- read a small ordered batch
- coordinate with `FOR UPDATE SKIP LOCKED`
- publish persistent messages
- require publisher confirms
- mark published only after confirmation
- leave failures retryable

Duplicate publication after a crash is expected. Worker idempotency, not an
exactly-once claim, protects correctness.

## RabbitMQ And Worker Rules

RabbitMQ is transport, not state storage. Messages carry stable IDs; workers load
the current payment from PostgreSQL.

Consumer flow:

1. Validate the message.
2. Load current payment state.
3. Treat stale/already-applied work as a successful no-op.
4. Run deterministic external behavior outside a transaction.
5. Apply the guarded database transaction and next outbox event.
6. Commit.
7. Acknowledge on the receiving channel.

Use durable queues, persistent messages, bounded prefetch, manual acknowledgements,
small bounded retries, and a DLQ. Do not immediately requeue poison messages in a
hot loop.

Keep worker handlers independent from RabbitMQ delivery types so their behavior is
easy to unit-test.

## Reconciliation Rules

Weekend reconciliation is a one-shot, read-only command.

It may report:

- payment status and ledger mismatches
- unexpected ledger movement
- stale pending/processing payments
- stale unpublished outbox events

It must not update payment state, balances, ledger entries, or outbox rows. Do not
add automatic repair or external settlement comparison before the weekend version
is complete.

## HTTP And Error Rules

Handlers may decode JSON, validate request shape, call services, map errors, and
encode responses. Business logic stays in services/repositories.

Keep the current JSON `idempotency_key` for the weekend. Continue using body size
limits, unknown-field rejection, content-type validation, safe client errors, and
request timeouts.

Classify worker errors:

- invalid/permanent: reject or DLQ
- duplicate/stale: acknowledge as no-op
- transient database/broker/external: bounded retry
- definitive fraud/processor result: business transition
- exhausted retry: DLQ and structured error log

## Testing Priorities

Prioritize tests that prove:

- payment and initial outbox event are atomic
- debit, credit, and refund each happen once
- duplicate worker events are safe
- outbox is marked published only after broker confirmation
- ack occurs only after database success
- reconciliation detects deliberately seeded discrepancies
- one create-to-completed integration path works with a duplicated event

Use real PostgreSQL for repository behavior, a small RabbitMQ integration test for
transport behavior, and deterministic fakes for business outcomes.

## Review Order

When reviewing code, report findings in this order:

1. money or state correctness bugs
2. crash, acknowledgement, retry, and duplicate-delivery risks
3. security issues
4. missing high-value tests
5. simplification and Go style

Explain the concrete failure scenario and suggest the smallest fix. Avoid rewriting
the whole slice.

## Weekend Definition Of Done

The project is ready for initial job applications when:

- Docker Compose starts the required services
- API creates and retrieves payments
- outbox records are atomic with payment changes
- relay publishes with confirms
- fraud and processor consumers complete the flow
- workers manually acknowledge after commit
- retries are bounded and exhausted messages reach a DLQ
- duplicate messages preserve state, balances, and ledger entries
- read-only reconciliation reports seeded discrepancies
- structured logs make a payment traceable
- critical deterministic tests pass
- README status matches reality

Notification logging is optional. Deferred improvements do not block this
milestone.
