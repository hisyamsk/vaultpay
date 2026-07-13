package queue

import (
	"os"
	"testing"
	"time"

	amqp "github.com/rabbitmq/amqp091-go"
	"github.com/stretchr/testify/require"
)

func TestDeclarePaymentEventsExchangeCanBeRepeated(t *testing.T) {
	rabbitMQURL := os.Getenv("RABBITMQ_URL")
	if rabbitMQURL == "" {
		t.Skip("RABBITMQ_URL is not set; skipping RabbitMQ integration test")
	}

	conn, err := amqp.DialConfig(rabbitMQURL, amqp.Config{
		Dial: amqp.DefaultDial(5 * time.Second),
	})
	require.NoError(t, err)
	t.Cleanup(func() {
		require.NoError(t, conn.Close())
	})

	ch, err := conn.Channel()
	require.NoError(t, err)
	t.Cleanup(func() {
		require.NoError(t, ch.Close())
	})

	require.NoError(t, DeclarePaymentEventsExchange(ch))
	require.NoError(t, DeclarePaymentEventsExchange(ch))
}
