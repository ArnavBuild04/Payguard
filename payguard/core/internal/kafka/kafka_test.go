package kafka_test

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ArnavBuild04/payguard/core/internal/kafka"
	"github.com/ArnavBuild04/payguard/core/internal/kafka/models"
	segmentio "github.com/segmentio/kafka-go"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

const (
	testBroker = "localhost:9092"
	testDSN    = "host=localhost user=payguard password=payguard dbname=payguard port=5432 sslmode=disable TimeZone=UTC"
)

var (
	dbOnce sync.Once
	testDB *gorm.DB
	dbErr  error
)

func getDB(t *testing.T) *gorm.DB {
	t.Helper()
	dbOnce.Do(func() {
		testDB, dbErr = gorm.Open(postgres.Open(testDSN), &gorm.Config{
			Logger: logger.Default.LogMode(logger.Silent),
		})
		if dbErr == nil {
			dbErr = testDB.AutoMigrate(&models.ProcessedEvent{})
		}
	})
	if dbErr != nil {
		t.Skipf("postgres unavailable at %q, skipping integration test: %v", testDSN, dbErr)
	}
	return testDB
}

// brokerAvailable skips the test cleanly when Kafka isn't reachable.
func brokerAvailable(t *testing.T) {
	t.Helper()
	conn, err := segmentio.DialContext(context.Background(), "tcp", testBroker)
	if err != nil {
		t.Skipf("kafka broker unavailable at %q, skipping integration test: %v", testBroker, err)
	}
	conn.Close()
}

func uniqueTopic(prefix string) string {
	return fmt.Sprintf("%s.%d", prefix, time.Now().UnixNano())
}

func TestProducerConsumer_RoundTrip(t *testing.T) {
	brokerAvailable(t)
	db := getDB(t)
	topic := uniqueTopic("test.kafka.roundtrip")
	dlqTopic := uniqueTopic("test.kafka.roundtrip.dlq")
	groupID := uniqueTopic("test-group")

	producer := kafka.NewProducer([]string{testBroker}, topic)
	defer producer.Close()
	dlq := kafka.NewProducer([]string{testBroker}, dlqTopic)
	defer dlq.Close()

	env := kafka.Envelope{EventID: "evt-1", EventType: "TEST_EVENT", AggregateID: "agg-1", PayloadJSON: `{"k":"v"}`}
	if err := producer.Publish(context.Background(), env.AggregateID, env); err != nil {
		t.Fatalf("publish failed: %v", err)
	}

	var received int64
	var gotEnv kafka.Envelope
	var mu sync.Mutex
	handler := func(_ context.Context, e kafka.Envelope) error {
		mu.Lock()
		gotEnv = e
		mu.Unlock()
		atomic.AddInt64(&received, 1)
		return nil
	}

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	go kafka.RunConsumer(ctx, db, []string{testBroker}, topic, groupID, dlq, handler)
	<-ctx.Done()

	if atomic.LoadInt64(&received) != 1 {
		t.Fatalf("received = %d, want 1", received)
	}
	mu.Lock()
	defer mu.Unlock()
	if gotEnv.EventID != env.EventID || gotEnv.PayloadJSON != env.PayloadJSON {
		t.Fatalf("got envelope %+v, want %+v", gotEnv, env)
	}
}

// TestConsumer_HandlerFailure_RoutesToDLQAndStillCommits asserts a poison message still commits.
func TestConsumer_HandlerFailure_RoutesToDLQAndStillCommits(t *testing.T) {
	brokerAvailable(t)
	db := getDB(t)
	topic := uniqueTopic("test.kafka.failure")
	dlqTopic := uniqueTopic("test.kafka.failure.dlq")
	groupID := uniqueTopic("test-group")

	producer := kafka.NewProducer([]string{testBroker}, topic)
	defer producer.Close()
	dlq := kafka.NewProducer([]string{testBroker}, dlqTopic)
	defer dlq.Close()
	dlqReader := segmentio.NewReader(segmentio.ReaderConfig{Brokers: []string{testBroker}, Topic: dlqTopic, GroupID: uniqueTopic("dlq-reader")})
	defer dlqReader.Close()

	poisonEnv := kafka.Envelope{EventID: "evt-poison", EventType: "TEST_EVENT", AggregateID: "agg-poison", PayloadJSON: `{}`}
	if err := producer.Publish(context.Background(), poisonEnv.AggregateID, poisonEnv); err != nil {
		t.Fatalf("publish poison failed: %v", err)
	}
	goodEnv := kafka.Envelope{EventID: "evt-good", EventType: "TEST_EVENT", AggregateID: "agg-good", PayloadJSON: `{}`}
	if err := producer.Publish(context.Background(), goodEnv.AggregateID, goodEnv); err != nil {
		t.Fatalf("publish good failed: %v", err)
	}

	var goodReceived int64
	handler := func(_ context.Context, e kafka.Envelope) error {
		if e.EventID == poisonEnv.EventID {
			return fmt.Errorf("simulated permanent handler failure")
		}
		atomic.AddInt64(&goodReceived, 1)
		return nil
	}

	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Second)
	defer cancel()
	go kafka.RunConsumer(ctx, db, []string{testBroker}, topic, groupID, dlq, handler)

	dlqCtx, dlqCancel := context.WithTimeout(context.Background(), 4*time.Second)
	defer dlqCancel()
	dlqMsg, err := dlqReader.ReadMessage(dlqCtx)
	if err != nil {
		t.Fatalf("expected the poison message on the DLQ, got error: %v", err)
	}
	var dlqEnv kafka.Envelope
	if err := json.Unmarshal(dlqMsg.Value, &dlqEnv); err != nil {
		t.Fatalf("failed to decode DLQ message: %v", err)
	}
	if dlqEnv.EventID != poisonEnv.EventID {
		t.Fatalf("DLQ event id = %q, want %q", dlqEnv.EventID, poisonEnv.EventID)
	}
	if dlqEnv.OriginalTopic != topic {
		t.Fatalf("DLQ original_topic = %q, want %q", dlqEnv.OriginalTopic, topic)
	}

	<-ctx.Done()
	if atomic.LoadInt64(&goodReceived) != 1 {
		t.Fatalf("goodReceived = %d, want 1 — the partition must keep consuming after a poison message", goodReceived)
	}
}
