# Take-Home Project: Transaction Processing Pipeline

## Company Context

You're joining **VaultPay**, a fintech startup building payment infrastructure for marketplaces. Merchants integrate with VaultPay to accept payments from buyers, and VaultPay handles settlement to merchants' bank accounts.

Currently, the system has a synchronous payment flow that's causing problems:
- HTTP timeouts when the payment processor is slow
- No retry logic for transient failures
- Notification delivery is flaky and blocks payment response
- Can't handle end-of-month settlement spikes

Your task is to redesign the payment processing pipeline with proper async handling.

---

## Problem Statement

Build a **Payment Processing Service** that accepts payment requests via REST API and processes them through an asynchronous pipeline using message queues.

### The Core Flow

```
[Client] → [API Gateway] → [Payment Intent Created] → [MQ: payment.pending]
                                                        ↓
                                              [Fraud Screening Worker]
                                                        ↓
                                              [MQ: fraud.checked]
                                                        ↓
                                            [Payment Execution Worker]
                                            (calls external processor)
                                                        ↓
                                              [MQ: payment.completed]
                                                    ↓       ↓
                                        [Notification Worker]  [Webhook Worker]
```

---

## Functional Requirements

### FR1: Payment Intention
- `POST /api/v1/payments` - Initiate a payment
  - Request: `{ amount, sender_id, receiver_id, idempotency_key, description }`
  - Response: `201 Created` with `{ payment_id, status: "pending" }`
  - Must return **immediately** — do NOT wait for payment completion
  - Idempotency: Same `idempotency_key` returns the same `payment_id`

### FR2: Payment Status
- `GET /api/v1/payments/{payment_id}` - Get payment status
  - Response: `{ payment_id, amount, status, created_at, updated_at, error_code? }`
  - Statuses: `pending` → `processing` → `completed` | `failed` | `rejected`

### FR3: Fraud Screening (Queue Consumer #1)
- Consumes from `payment.pending` queue
- Simulate fraud check (random 5% rejection rate, 2% timeout simulation)
- If rejected: update status to `rejected`, publish to `payment.failed`
- If passed: publish to `fraud.checked`

### FR4: Payment Execution (Queue Consumer #2)
- Consumes from `fraud.checked` queue
- Simulate calling external payment processor (random 10% failure rate)
- On failure: retry up to 3 times with exponential backoff
- After all retries exhausted: mark as `failed`
- On success: publish to `payment.completed`

### FR5: Notification Delivery (Queue Consumer #3)
- Consumes from `payment.completed` and `payment.failed`
- Simulate sending email/push notification (log the notification)
- Should **never** block or fail the payment flow
- If notification fails, retry once then give up (log the failure)

### FR6: Account Ledger
- Maintain account balances for sender and receiver
- Deduct from sender on `pending`, credit receiver on `completed`
- Refund sender on `failed` or `rejected`
- `GET /api/v1/accounts/{account_id}` returns current balance and transaction history

---

## Non-Functional Requirements

### NFR1: Resilience
- API must respond within 200ms even if downstream is slow
- System must survive worker crashes without losing payments
- Failed messages must be retried automatically

### NFR2: Data Consistency
- No double-spending: idempotency must be enforced
- Ledger balances must always be accurate
- Payment state transitions must be valid (no `completed` → `pending`)

### NFR3: Observability
- Structured JSON logging with correlation IDs (payment_id)
- Metrics: payment count by status, processing duration, queue depth
- Health check endpoint: `GET /health`

### NFR4: Concurrency
- Multiple workers should be able to process different payments in parallel
- Same payment_id must never be processed concurrently by multiple workers

---

## Technical Constraints

| Aspect | Constraint |
|--------|------------|
| Language | Go 1.21+ |
| Database | PostgreSQL OR SQLite (your choice) |
| Message Queue | Redis Streams **OR** RabbitMQ (pick one) |
| External Dependencies | None — simulate external services with interfaces |
| Runtime | Must run locally with `docker-compose up` |
| Code Quality | Must have unit tests for core logic (aim for 60%+ coverage) |

---

## Why Message Queues Are Required Here

> **This section explains the architectural decision — understand it deeply.**

### Problem 1: External Service Latency
The fraud screening service and payment processor are external. They have:
- Variable latency (100ms to 5 seconds)
- Occasional timeouts
- No SLA guarantee

**Without MQ:** Your HTTP handler blocks for 5+ seconds, causing:
- Connection pool exhaustion
- Load balancer health check failures
- Terrible user experience

**With MQ:** HTTP returns in ~50ms. Processing happens asynchronously.

### Problem 2: Retry Logic
Payment processors fail ~10% of the time due to network issues, rate limits, or transient errors.

**Without MQ:** You'd need a cron job scanning for "stuck" payments — ugly, error-prone, delayed.

**With MQ:** Dead letter queues + retry policies handle this automatically with exponential backoff.

### Problem 3: Fan-Out (Multiple Consumers)
When a payment completes, multiple things need to happen:
- Update ledger
- Send notification to payer
- Send notification to payee
- Deliver webhook to merchant
- Update analytics

**Without MQ:** You'd call all of these synchronously. If webhook delivery is slow, everything waits. If notification fails, does the whole transaction roll back?

**With MQ:** Each consumer subscribes independently. Notification failure doesn't affect webhook delivery.

### Problem 4: Load Leveling
End-of-month settlement creates 10x normal traffic for 2 hours.

**Without MQ:** You need 10x infrastructure always, or your system crashes during peaks.

**With MQ:** Queue absorbs the spike. Workers process at sustainable rate. Payments complete slower but don't fail.

### Problem 5: Decoupling
Fraud screening team wants to deploy their service independently. Notification team wants to add SMS. Webhook team needs to add retry logic.

**With MQ:** These are separate workers. Deploy independently. Add new consumers without touching payment service.

---

## Expected Deliverables

### 1. Working System
```
├── cmd/
│   ├── api/           # HTTP server
│   └── worker/        # Queue consumers (single binary, flag to select worker type)
├── internal/
│   ├── domain/        # Entities, value objects
│   ├── repository/    # Database access
│   ├── service/       # Business logic
│   ├── handler/       # HTTP handlers
│   ├── worker/        # Queue consumers
│   ├── queue/         # Queue abstraction (interface + implementation)
│   └── external/      # Simulated external services
├── migrations/        # SQL migrations
├── docker-compose.yml
├── Makefile
└── README.md
```

### 2. Documentation (README.md)
- Architecture diagram (ASCII or image)
- How to run locally
- API documentation
- Queue topology explanation
- Design decisions and trade-offs

### 3. Tests
- Unit tests for: payment state machine, idempotency logic, ledger operations
- Integration test: submit payment → verify it reaches `completed` status

---

## Evaluation Criteria

| Category | Weight | What We Look For |
|----------|--------|------------------|
| **Correctness** | 25% | Payment lifecycle works, no data loss, idempotency enforced |
| **Queue Usage** | 25% | MQ is used correctly, retries work, dead letter handling, proper acknowledgment |
| **Code Quality** | 20% | Clean architecture, proper error handling, no global state |
| **Testing** | 15% | Meaningful tests, not just happy path |
| **Observability** | 10% | Logging, metrics, health checks |
| **Documentation** | 5% | Clear README, design rationale |

---

## Bonus Points (Not Required)

- [ ] Add a simple CLI tool to submit payments and check status
- [ ] Implement distributed tracing with OpenTelemetry
- [ ] Add a dashboard showing queue depths and payment stats
- [ ] Add webhook delivery with signature verification

---

## Getting Started Template

```bash
# Suggested project structure
mkdir vaultpay && cd vaultpay
go mod init github.com/yourname/vaultpay

# Create directories
mkdir -p cmd/api cmd/worker internal/{domain,repository,service,handler,worker,queue,external} migrations
```

### Suggested Interfaces to Define

```go
// internal/queue/queue.go
type PaymentQueue interface {
    PublishPaymentPending(ctx context.Context, payment *domain.Payment) error
    PublishFraudChecked(ctx context.Context, payment *domain.Payment) error
    PublishPaymentCompleted(ctx context.Context, payment *domain.Payment) error
    PublishPaymentFailed(ctx context.Context, payment *domain.Payment) error
    
    ConsumePaymentPending(ctx context.Context, handler func(*domain.Payment) error) error
    ConsumeFraudChecked(ctx context.Context, handler func(*domain.Payment) error) error
    // ...
}

// internal/external/fraud.go
type FraudChecker interface {
    Check(ctx context.Context, payment *domain.Payment) (*FraudResult, error)
}

// internal/external/processor.go
type PaymentProcessor interface {
    Process(ctx context.Context, payment *domain.Payment) (*ProcessorResult, error)
}
```

### Suggested State Machine

```go
// internal/domain/payment.go
type PaymentStatus string

const (
    StatusPending   PaymentStatus = "pending"
    StatusProcessing PaymentStatus = "processing"
    StatusCompleted PaymentStatus = "completed"
    StatusFailed    PaymentStatus = "failed"
    StatusRejected  PaymentStatus = "rejected"
)

var ValidTransitions = map[PaymentStatus][]PaymentStatus{
    StatusPending:   {StatusProcessing, StatusRejected},
    StatusProcessing: {StatusCompleted, StatusFailed},
    // Terminal states have no transitions
}
```

---

## Sample docker-compose.yml

```yaml
version: '3.8'
services:
  postgres:
    image: postgres:15-alpine
    environment:
      POSTGRES_DB: vaultpay
      POSTGRES_USER: vaultpay
      POSTGRES_PASSWORD: vaultpay_dev
    ports:
      - "5432:5432"
    volumes:
      - pgdata:/var/lib/postgresql/data

  redis:
    image: redis:7-alpine
    ports:
      - "6379:6379"
    
  # Uncomment if using RabbitMQ instead of Redis
  # rabbitmq:
  #   image: rabbitmq:3-management-alpine
  #   ports:
  #     - "5672:5672"
  #     - "15672:15672"

volumes:
  pgdata:
```

---

## Questions to Think About (Not Required to Submit)

1. What happens if a worker crashes mid-processing? How do you ensure exactly-once semantics?
2. How would you handle the "double refund" problem if both fraud rejection and payment failure happen?
3. What if the queue itself loses messages? How do you recover?
4. How would this design change if you needed real-time payment status updates via WebSocket?
5. What's the tradeoff between using Redis Streams vs RabbitMQ for this use case?

---

*Estimated time: 16-24 hours*
