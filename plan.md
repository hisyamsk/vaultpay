# Payment Processing Service/Repository Plan

  ## Summary

  Build the next slice as worker-facing service methods, not an HTTP update handler.

  Chosen design: deduct sender after fraud approval. So POST /payments still only creates a pending payment. After fraud passes, the future payment worker calls a service method that
  atomically moves the payment into processing and debits the sender.

  Keep the current status model:

  pending -> processing -> completed | failed
  pending -> rejected

  Use processing to mean: fraud passed and sender funds have been debited/reserved.

  ## Key Changes

  Add three worker-facing service methods:

  StartApprovedPaymentProcessing(ctx, paymentID) (*domain.Payment, error)
  CompleteProcessedPayment(ctx, paymentID) (*domain.Payment, error)
  FailProcessedPayment(ctx, paymentID, errorCode) (*domain.Payment, error)

  Why three methods: money movement is not one generic status update. Debit, credit, refund, ledger insert, error code handling, and status transition each have different invariants.

  Expected behavior:

  - StartApprovedPaymentProcessing
      - Validate paymentID is not empty.
      - Call repository method that owns the atomic transition and sender debit.
      - Reload the payment from the repository and return current state.
      - If repository returns an error, wrap and return it.
      - If sender has insufficient balance, repository marks payment failed with error_code = "insufficient_funds"; service returns the reloaded failed payment.

  - CompleteProcessedPayment
      - Validate paymentID is not empty.
      - Call repository method that owns the atomic receiver credit and completion.
      - Reload the payment from the repository and return current state.
      - If repository returns an error, wrap and return it.

  - FailProcessedPayment
      - Validate paymentID is not empty.
      - Validate errorCode is an allowed processor failure code.
      - Call repository method that owns the atomic refund and failure transition.
      - Reload the payment from the repository and return current state.
      - If repository returns an error, wrap and return it.
      - If payment is already failed, do not overwrite the existing error_code.

  Keep UpdatePaymentStatus only for simple non-money transitions for now, such as future fraud rejection pending -> rejected. Do not expose it through HTTP.

  ## Interface Boundaries

  Keep interfaces small and define them near the consumer.

  Service package:

  - PaymentService depends on a paymentRepository interface.
  - This interface can include repository methods needed by payment service use cases.
  - It is okay for this interface to grow with worker-facing payment operations because the service owns those use cases.

  Handler package:

  - HTTP handlers should depend on a handler-local service interface, not directly on repository behavior.
  - The handler-local interface should include only methods the handler calls, such as CreatePayment and future GetPayment.
  - Handler tests should fake the service directly.
  - Handler tests should not need to know about StartApprovedPaymentProcessing, CompleteProcessedPayment, or FailProcessedPayment.

  Why: worker-only service methods should not leak into HTTP handler tests. The handler is responsible for HTTP decoding, validation, service call, error mapping, and response encoding. It should not care how the service talks to the database.

  ## Repository Practices

  Implement each money-moving repository method as one database transaction.

  Apply these practices:

  - Use SELECT ... FOR UPDATE on the payment row before deciding state.
  - Lock the account row before balance mutation.
  - Keep lock order consistent: payment first, then account.
  - Update account balance and insert ledger entry in the same transaction.
  - Do not call external fraud/processor services inside a DB transaction.
  - Treat duplicate or terminal-state messages as idempotent no-ops.
  - Rely on both status checks and the existing unique ledger constraint for duplicate protection.
  - Return safe domain/repository errors such as payment not found, account not found, invalid transition.

  Repository method behavior:

  - StartApprovedPaymentProcessing
      - If payment is pending: lock payment, lock sender account, check balance, debit sender, insert debit ledger, set status processing.
      - If payment is already processing: return it without debiting again.
      - If payment is terminal: return it as a no-op.
      - If sender has insufficient balance: mark payment failed with error_code = "insufficient_funds", do not write ledger, do not debit.

  - CompleteProcessedPayment
      - Only money-moves from processing.
      - Lock payment, lock receiver account, credit receiver, insert credit ledger, set status completed.
      - If already completed: no-op.
      - If failed or rejected: no-op, never credit.
      - If still pending: return invalid transition.

  - FailProcessedPayment
      - Only refunds from processing.
      - Lock payment, lock sender account, credit sender, insert refund ledger, set status failed with the provided processor error code.
      - If already failed: no-op and keep the existing error_code.
      - If completed or rejected: no-op, never refund.
      - If still pending: return invalid transition.

  Why: queue workers are at-least-once. A redelivered message must be safe even if the previous attempt committed right before crashing.

  ## Test Plan

  Add service unit tests with fake repositories.

  Cover:

  - StartApprovedPaymentProcessing
      - rejects empty paymentID
      - calls repository method
      - reloads and returns current payment
      - returns current payment for idempotent no-op statuses
      - wraps repository errors
      - wraps reload errors

  - CompleteProcessedPayment
      - rejects empty paymentID
      - calls repository method
      - reloads and returns current payment
      - returns current payment for idempotent no-op statuses
      - wraps repository errors
      - wraps reload errors

  - FailProcessedPayment
      - rejects empty paymentID
      - rejects empty or unsupported errorCode
      - calls repository method with the provided errorCode
      - reloads and returns current payment
      - returns current payment for idempotent no-op statuses
      - wraps repository errors
      - wraps reload errors

  Add handler unit tests with fake service, not fake repository.

  Cover:

  - handler decodes JSON correctly
  - handler validates request shape
  - handler maps service errors to HTTP status codes
  - handler response does not expose internal errors

  Add repository integration tests with real Postgres if possible, because fakes cannot prove transaction/locking/constraint behavior.

  Cover:

  - Start processing succeeds:
      - pending payment becomes processing
      - sender balance decreases once
      - one debit ledger row is inserted

  - Start processing duplicate:
      - second call does not debit again
      - no second debit ledger row

  - Insufficient funds:
      - payment becomes failed
      - payment error_code is insufficient_funds
      - sender balance unchanged
      - no debit ledger row

  - Complete succeeds:
      - processing payment becomes completed
      - receiver balance increases once
      - one credit ledger row is inserted

  - Complete duplicate:
      - no double credit

  - Fail succeeds:
      - processing payment becomes failed
      - payment error_code is the processor failure code
      - sender is refunded once
      - one refund ledger row is inserted

  - Fail duplicate:
      - no double refund
      - existing error_code is not overwritten

  - Invalid/terminal transitions:
      - pending cannot complete directly
      - pending cannot fail through FailProcessedPayment
      - completed cannot fail
      - rejected cannot process
      - terminal states stay terminal

  - Optional but strong:
      - two concurrent StartApprovedPaymentProcessing calls for the same payment result in one debit only.

  ## Assumptions

  - Amounts remain int64 minor units.
  - Fraud rejection happens before sender debit, so fraud rejection does not need a refund.
  - Insufficient funds is handled by StartApprovedPaymentProcessing because it happens before processing starts.
  - Processor failures are handled by FailProcessedPayment because they happen after processing starts.
  - The future worker will call the external payment processor only after StartApprovedPaymentProcessing returns a payment with status processing.
  - The future worker will call CompleteProcessedPayment only after processor success, and FailProcessedPayment only after final processor failure.
  - Queue ack/retry behavior is out of scope for this step, but these methods are designed so duplicate/redelivered messages are safe.
