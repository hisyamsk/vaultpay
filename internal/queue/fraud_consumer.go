package queue

import (
	"context"
	"errors"
)

var ErrInvalidFraudMessage = errors.New("invalid fraud message")

type fraudEventHandler interface {
	HandleMessage(ctx context.Context, message PaymentEventMessage) error
}

type FraudConsumer struct {
	handler fraudEventHandler
}

func NewFraudConsumer(handler fraudEventHandler) *FraudConsumer {
	return &FraudConsumer{handler: handler}
}

// HandleMessage must decode and validate the body, accept only payment.created,
// and pass the decoded event to the fraud handler. Invalid bodies and unexpected
// event types must wrap ErrInvalidFraudMessage. Handler errors must be wrapped
// without being classified as invalid messages.
func (c *FraudConsumer) HandleMessage(ctx context.Context, body []byte) error {
	return nil
}
