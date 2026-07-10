package domain

type PaymentEventType string

const (
	PaymentEventTypeCreated    PaymentEventType = "payment.created"
	PaymentEventTypeProcessing PaymentEventType = "payment.processing"
	PaymentEventTypeCompleted  PaymentEventType = "payment.completed"
	PaymentEventTypeFailed     PaymentEventType = "payment.failed"
	PaymentEventTypeRejected   PaymentEventType = "payment.rejected"
)
