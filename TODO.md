## Add Outbox Repository Operations

- [x] Add a small domain/repository type for a stored outbox event: database ID, event ID, event type, payment ID, payload, creation time, and publish metadata.
- [x] Add a repository method that atomically claims at most 10 unpublished events.
- [x] Order claims by `created_at`, then the database primary key for stable ordering.
- [x] Select candidates with `FOR UPDATE SKIP LOCKED` inside a short PostgreSQL transaction.
- [x] Use `last_attempted_at` as a short claim lease so a second relay does not immediately claim the same rows after the transaction commits.
- [x] Allow rows with an expired claim lease to be retried after a relay crash.
- [x] Increment `publish_attempts` and set `last_attempted_at` when a publish attempt is claimed.
- [x] Commit the claim before making any RabbitMQ call.
- [x] Add a method that sets `published_at` and clears `last_error` after confirmation.
- [x] Guard the success update with `published_at IS NULL` so repeated confirmation handling is harmless.
- [x] Add a method that records `last_error` after a failed or unconfirmed publish.
- [x] Keep failed rows unpublished so they remain retryable.
- [x] Wrap database errors with operation context using `%w`.

Tests:

- [x] Claim returns only unpublished events in stable order and respects the batch limit.
- [x] Two concurrent claims do not return the same fresh event.
- [x] An expired claim becomes available again.
- [x] Claim increments `publish_attempts` once per claim.
- [x] Mark-success sets `published_at` only for the requested event.
- [x] Mark-failure leaves `published_at` null and stores the error.
- [x] Repeating mark-success does not corrupt the event.

Gate: outbox lifecycle behavior is proven with real PostgreSQL without RabbitMQ.

## Add RabbitMQ And Declare The Topology

- [x] Add the RabbitMQ Go client dependency.
- [x] Add RabbitMQ to both Compose files with a persistent named volume and health check.
- [x] Add broker URL and bounded timeout settings to configuration.
- [x] Declare one durable topic exchange for payment events.
- [x] Declare a durable fraud queue bound to `payment.created`.
- [x] Declare a durable processor queue bound to `payment.processing`.
- [x] Declare one delayed retry path that dead-letters messages back to their work queue after a short delay.
- [x] Declare a durable DLQ for malformed messages and exhausted retries.
- [x] Make topology declaration idempotent so restarting the worker is safe.
- [x] Confirm Compose can start PostgreSQL, RabbitMQ, the API, and the worker dependencies from a clean volume.

Gate: queues, bindings, retry path, and DLQ can be inspected in a running local RabbitMQ instance.

## Implement The Confirming Publisher And Relay

- [x] Define a small publisher interface near the relay so relay tests do not require RabbitMQ.
- [x] Enable publisher confirms on the dedicated publisher channel before publishing.
- [x] Publish the stored JSON payload without rebuilding business state from memory.
- [x] Route by the stored event type.
- [x] Publish persistent messages with JSON content type and `event_id` as the message ID.
- [x] Use a bounded context for each publish and confirmation wait.
- [x] Treat broker rejection, timeout, channel closure, and connection errors as failed publication.
- [x] Mark an event published only after receiving a positive confirmation.
- [x] Record an error and leave the event unpublished for every failed or unknown confirmation result.
- [x] Poll in small batches with a bounded idle delay and stop promptly on context cancellation.
- [x] Log `event_id`, `payment_id`, `event_type`, `publish_attempts`, result, error, and duration.

Tests:

- [x] A confirmed publish marks the event published.
- [x] A rejected or timed-out publish never sets `published_at`.
- [x] A publisher error is stored and the event remains retryable.
- [x] A repeated relay pass does not republish an event already marked published.
- [x] A simulated crash after publish but before mark-success leaves a duplicate possible rather than losing the event.
- [x] One small RabbitMQ integration test proves a persistent message is confirmed and routed to the expected queue.

Gate: committed `payment.created` events reach the fraud queue and are marked published only after confirmation.

## Make Every Fraud Result Atomic With Its Next Event

- [x] Replace fraud rejection's separate read/update calls with one repository transaction.
- [x] Lock the payment row before deciding whether rejection may be applied.
- [x] For a pending payment, set `rejected` and insert `payment.rejected` in the same transaction.
- [x] Treat a non-pending payment as a successful no-op without inserting another event.
- [x] In `StartApprovedPaymentProcessing`, insert `payment.processing` in the same transaction as sender debit, debit ledger entry, and status update.
- [x] When funds are insufficient, insert `payment.failed` in the same transaction as the failed status and error code.
- [x] Ensure duplicate fraud work creates neither a second debit nor a second next event.
- [x] Return enough information for the worker to log the resulting status.

Tests:

- [x] Rejection and `payment.rejected` roll back together when either write fails.
- [x] Approval, debit, ledger entry, processing status, and `payment.processing` roll back together.
- [x] Insufficient-funds status and `payment.failed` roll back together.
- [x] Duplicate approval keeps one debit entry, one balance deduction, and one processing event.
- [x] Duplicate rejection keeps one rejected event.

Gate: every fraud-caused payment mutation has exactly one matching committed outbox event.

## Wire The Fraud RabbitMQ Consumer

- [x] Keep the existing fraud handler independent from RabbitMQ delivery types.
- [x] Add a positive maximum-attempt setting shared by the fraud and processor consumers.
- [x] Decode and validate `event_id`, `event_type`, `payment_id`, `attempt`, and `occurred_at` at the adapter boundary.
- [ ] Accept only `payment.created` on the fraud consumer.
- [ ] Configure the fraud consumer channel with bounded prefetch and consume with automatic acknowledgements disabled.
- [x] Load current payment state from PostgreSQL before running the fraud checker.
- [x] Treat an already-applied or stale payment as a successful no-op.
- [ ] Acknowledge only after the handler's database work commits or returns a stale no-op.
- [ ] Send malformed/permanent messages to the DLQ without retrying.
- [ ] On a transient error, publish a copy to the retry path with `attempt + 1`, wait for confirmation, then acknowledge the original.
- [ ] Do not acknowledge the original when retry publication is unconfirmed.
- [ ] Stop retrying after the configured maximum attempt and send the message to the DLQ.
- [ ] Never use immediate `Nack(requeue=true)` for transient failures.

Tests:

- [ ] Successful database handling happens before acknowledgement.
- [ ] Database failure does not acknowledge the original message.
- [ ] Stale duplicate delivery is acknowledged as a no-op.
- [ ] Malformed input reaches the DLQ path.
- [ ] Transient failure increments `attempt` and uses the delayed retry path.
- [ ] Exhausted retry reaches the DLQ.

Gate: a duplicated `payment.created` delivery cannot debit the sender twice.

## Make Processor Results Atomic With Their Events

- [ ] In completion, lock the payment and receiver account rows.
- [ ] Commit receiver credit, credit ledger entry, `completed` status, and `payment.completed` together.
- [ ] In definitive failure, lock the payment and sender account rows.
- [ ] Commit sender refund, refund ledger entry, `failed` status/error code, and `payment.failed` together.
- [ ] Treat terminal or already-applied payments as successful no-ops without another ledger entry or event.
- [ ] Keep unique ledger constraints as the final duplicate guard.

Tests:

- [ ] Completion and `payment.completed` roll back together on any write failure.
- [ ] Failure/refund and `payment.failed` roll back together on any write failure.
- [ ] Duplicate success keeps one receiver credit, one credit entry, and one completed event.
- [ ] Duplicate failure keeps one sender refund, one refund entry, and one failed event.
- [ ] A terminal payment cannot switch to another terminal outcome.

Gate: each processor outcome changes money and emits its terminal event exactly once.

## Implement The Deterministic Processor Core

- [ ] Add a small processor interface near the worker.
- [ ] Implement a deterministic fake with explicit success and definitive-failure outcomes.
- [ ] Do not use randomness, sleeping, or hidden retries.
- [ ] Add a transport-independent processor handler for `payment.processing` messages.
- [ ] Load the payment from PostgreSQL and skip non-processing states as successful stale work.
- [ ] Call the fake processor outside every database transaction.
- [ ] On success, call the atomic complete-payment operation.
- [ ] On definitive failure, call the atomic fail-and-refund operation with a safe error code.
- [ ] Return transient fake or database errors to the RabbitMQ adapter for bounded retry.

Tests:

- [ ] Invalid and missing payment inputs are classified correctly.
- [ ] Non-processing states do not call the fake processor or mutate money.
- [ ] Deterministic success calls completion once.
- [ ] Deterministic failure calls refund/failure once with the expected error code.
- [ ] Fake and database errors are returned for retry.

Gate: processor decisions are deterministic and testable without RabbitMQ.

## Wire The Processor RabbitMQ Consumer

- [ ] Consume only from the processor queue bound to `payment.processing`.
- [ ] Configure the processor consumer channel with bounded prefetch and consume with automatic acknowledgements disabled.
- [ ] Reuse the same validation, manual-acknowledgement, bounded-retry, and DLQ rules as the fraud consumer.
- [ ] Keep RabbitMQ types out of the processor handler.
- [ ] Acknowledge only after completion/refund commits or stale work returns successfully.
- [ ] Add structured logs with `event_id`, `payment_id`, `attempt`, outcome, status, error, and duration.

Tests:

- [ ] Success is acknowledged only after the database commit.
- [ ] Transient failure follows the delayed retry path.
- [ ] Malformed and exhausted messages reach the DLQ.
- [ ] Duplicate delivery does not credit or refund twice.

Gate: a created payment can travel asynchronously to `completed` or `failed` without duplicate money movement.

## Add Payment Status Lookup

- [ ] Add `GET /api/v1/payments/{payment_id}` to the existing payment handler.
- [ ] Validate the path UUID before calling the service.
- [ ] Return payment ID, amount, sender ID, receiver ID, status, safe error code, description, and timestamps.
- [ ] Return `404` for a missing payment and a safe `500` response for internal errors.
- [ ] Keep database errors out of client responses.

Tests:

- [ ] Valid payment lookup returns the current asynchronous status.
- [ ] Invalid UUID returns `400`.
- [ ] Missing payment returns `404`.
- [ ] Repository failure returns a safe `500`.

Gate: clients can poll a created payment until it reaches a terminal state.

## Add Read-Only Reconciliation

- [ ] Add a reconciliation repository with query-only methods.
- [ ] Detect completed payments missing sender debit or receiver credit entries.
- [ ] Detect processed failures missing their debit or refund entries without flagging legitimate insufficient-funds failures.
- [ ] Detect pending or rejected payments with any ledger movement.
- [ ] Detect pending or processing payments older than a configurable threshold.
- [ ] Detect unpublished outbox events older than a configurable threshold.
- [ ] Return useful payment/event identifiers and discrepancy kinds.
- [ ] Add a one-shot `cmd/reconcile` command that prints structured output and a summary.
- [ ] Do not update payments, balances, ledger entries, or outbox rows.

Tests:

- [ ] Seed one example of every required mismatch and verify it is reported.
- [ ] Seed a consistent lifecycle and verify it is not reported.
- [ ] Verify threshold boundaries for stale payments and outbox events.
- [ ] Verify insufficient-funds failures without ledger entries are not false positives.

Gate: reconciliation reports deliberately seeded discrepancies and changes no data.

## Wire Processes, Shutdown, And Logs

- [ ] Add explicit constructors for database, RabbitMQ, relay, fraud, and processor dependencies.
- [ ] Add a worker entry point that runs the relay and required consumers without putting business logic in `main`.
- [ ] Add the worker service and required environment variables to Compose.
- [ ] Handle `SIGINT` and `SIGTERM` with context cancellation.
- [ ] Stop accepting new HTTP work and call `http.Server.Shutdown` with a timeout.
- [ ] Stop relay polling and consumer intake before closing RabbitMQ channels/connections.
- [ ] Close PostgreSQL after workers stop using it.
- [ ] Add structured logs that make one payment traceable through API, relay, fraud, and processor stages.

Gate: Compose starts the complete required system and all processes stop cleanly.

## Prove The Complete Flow

- [ ] Add one integration test for `create -> outbox -> fraud -> outbox -> processor -> completed`.
- [ ] Deliver at least one fraud or processor event twice in that test.
- [ ] Assert the final payment is `completed`.
- [ ] Assert the sender was debited exactly once.
- [ ] Assert the receiver was credited exactly once.
- [ ] Assert exactly one debit and one credit ledger entry exist.
- [ ] Assert required outbox events exist once per applied state change.
- [ ] Add a focused RabbitMQ test for manual acknowledgement after database success.
- [ ] Add a focused RabbitMQ test proving bounded retry exhaustion reaches the DLQ.
- [ ] Run the full suite repeatedly and remove all timing randomness and sleeps from business tests.

Gate: core unit and integration tests pass deterministically, including duplicate delivery.

## Final Definition-Of-Done Check

- [ ] From clean volumes, start the complete project with Docker Compose.
- [ ] Apply migrations successfully.
- [ ] Create sender and receiver demo accounts.
- [ ] Create a payment through the API and receive a pending response immediately.
- [ ] Retrieve the payment until the asynchronous flow reaches a terminal state.
- [ ] Inspect logs and trace the same payment through relay, fraud, and processor work.
- [ ] Confirm the relay uses publisher confirms.
- [ ] Confirm consumers use manual acknowledgement after database success.
- [ ] Confirm retries are bounded and exhausted/malformed messages reach the DLQ.
- [ ] Run reconciliation against deliberately seeded discrepancies and confirm it is read-only.
- [ ] Run all required tests from the documented commands.
- [ ] Update the README status table so only implemented behavior is marked implemented.
- [ ] Update README Compose, migration, API, worker, reconciliation, and test commands to match reality.
- [ ] Remove optional notification work from the critical path; implement it only after every item above passes.

Done means the initial-version checklist in `REQUIREMENTS.md` is true in a clean local environment, not only in isolated unit tests.
