package kafka

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"math/rand"
	"time"

	segmentio "github.com/segmentio/kafka-go"
	"gorm.io/gorm"
)

// Handler processes one decoded event; exhausting retries sends it to the DLQ.
type Handler func(ctx context.Context, env Envelope) error

// RunConsumer reads topic with consumer group groupID until ctx is cancelled.
func RunConsumer(ctx context.Context, db *gorm.DB, brokers []string, topic, groupID string, dlq *Producer, handler Handler) {
	reader := segmentio.NewReader(segmentio.ReaderConfig{
		Brokers:        brokers,
		Topic:          topic,
		GroupID:        groupID,
		CommitInterval: 0, // manual commit only
	})
	defer reader.Close()

	dedup := newDedupStore(db)

	for {
		msg, err := reader.FetchMessage(ctx)
		if err != nil {
			if ctx.Err() != nil {
				return
			}
			slog.Error("kafka: fetch failed", "topic", topic, "err", err)
			continue
		}

		var env Envelope
		if err := json.Unmarshal(msg.Value, &env); err != nil {
			// A message that will never parse must still be committed, or it blocks the partition forever.
			slog.Error("kafka: malformed message, routing to DLQ", "topic", topic, "partition", msg.Partition, "offset", msg.Offset, "err", err)
			publishToDLQ(ctx, dlq, topic, string(msg.Key), Envelope{
				EventID:       fmt.Sprintf("malformed-%s-%d-%d", topic, msg.Partition, msg.Offset),
				EventType:     "MALFORMED",
				PayloadJSON:   string(msg.Value),
				OriginalTopic: topic,
			})
			commit(ctx, reader, msg)
			continue
		}

		processed, err := dedup.alreadyProcessed(ctx, groupID, env.EventID)
		if err != nil {
			slog.Error("kafka: dedup check failed, will retry", "event_id", env.EventID, "err", err)
			continue // no commit — try again on the next poll
		}
		if processed {
			slog.Info("kafka: event already processed, skipping redelivery", "event_id", env.EventID)
			commit(ctx, reader, msg)
			continue
		}

		hErr := callWithRetry(ctx, func() error { return handler(ctx, env) })
		if hErr != nil {
			slog.Error("kafka: handler failed after retries, routing to DLQ", "event_id", env.EventID, "event_type", env.EventType, "err", hErr)
			env.OriginalTopic = topic
			publishToDLQ(ctx, dlq, topic, string(msg.Key), env)
		}

		// Marked processed either way, so a DLQ'd message isn't retried forever either.
		if err := dedup.markProcessed(ctx, groupID, env.EventID); err != nil {
			slog.Error("kafka: failed to record processed event, will retry", "event_id", env.EventID, "err", err)
			continue
		}
		commit(ctx, reader, msg)
	}
}

func publishToDLQ(ctx context.Context, dlq *Producer, sourceTopic, key string, env Envelope) {
	if dlq == nil {
		return
	}
	if err := dlq.Publish(ctx, key, env); err != nil {
		slog.Error("kafka: failed to publish to DLQ", "source_topic", sourceTopic, "event_id", env.EventID, "err", err)
	}
}

func commit(ctx context.Context, reader *segmentio.Reader, msg segmentio.Message) {
	if err := reader.CommitMessages(ctx, msg); err != nil {
		slog.Error("kafka: commit failed", "topic", msg.Topic, "partition", msg.Partition, "offset", msg.Offset, "err", err)
	}
}

// callWithRetry is a small bounded retry with full jitter.
func callWithRetry(ctx context.Context, fn func() error) error {
	const maxAttempts = 3
	base := 100 * time.Millisecond

	var err error
	for attempt := 1; attempt <= maxAttempts; attempt++ {
		err = fn()
		if err == nil {
			return nil
		}
		if attempt == maxAttempts {
			break
		}
		delay := time.Duration(1<<attempt) * base
		jitter := time.Duration(rand.Int63n(int64(delay)))
		select {
		case <-time.After(jitter):
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	return err
}
