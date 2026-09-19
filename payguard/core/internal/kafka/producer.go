package kafka

import (
	"context"
	"encoding/json"
	"fmt"

	segmentio "github.com/segmentio/kafka-go"
)

// Producer publishes Envelopes keyed by payment_id, so ordering holds per payment.
type Producer struct {
	writer *segmentio.Writer
}

func NewProducer(brokers []string, topic string) *Producer {
	return &Producer{
		writer: &segmentio.Writer{
			Addr:         segmentio.TCP(brokers...),
			Topic:        topic,
			Balancer:     &segmentio.Hash{},
			RequiredAcks: segmentio.RequireOne,
			// kafka-go's own client-side gate on topic auto-creation.
			AllowAutoTopicCreation: true,
		},
	}
}

func (p *Producer) Publish(ctx context.Context, key string, env Envelope) error {
	body, err := json.Marshal(env)
	if err != nil {
		return fmt.Errorf("kafka: encode envelope: %w", err)
	}
	return p.writer.WriteMessages(ctx, segmentio.Message{Key: []byte(key), Value: body})
}

func (p *Producer) Close() error {
	return p.writer.Close()
}
