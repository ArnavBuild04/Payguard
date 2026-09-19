package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"syscall"
	"time"

	"github.com/ArnavBuild04/payguard/core/internal/cache"
	coordinatorgranter "github.com/ArnavBuild04/payguard/core/internal/coordinator/granter"
	coordinatormodels "github.com/ArnavBuild04/payguard/core/internal/coordinator/models"
	coordinatorrepo "github.com/ArnavBuild04/payguard/core/internal/coordinator/repo"
	coordinatorservice "github.com/ArnavBuild04/payguard/core/internal/coordinator/service"

	"github.com/ArnavBuild04/payguard/core/internal/config"
	pgkafka "github.com/ArnavBuild04/payguard/core/internal/kafka"
	kafkamodels "github.com/ArnavBuild04/payguard/core/internal/kafka/models"
	"github.com/ArnavBuild04/payguard/core/internal/outbox"
	outboxmodels "github.com/ArnavBuild04/payguard/core/internal/outbox/models"
	paymenthttpapi "github.com/ArnavBuild04/payguard/core/internal/payment/httpapi"
	paymentmodels "github.com/ArnavBuild04/payguard/core/internal/payment/models"
	paymentrepo "github.com/ArnavBuild04/payguard/core/internal/payment/repo"
	paymentservice "github.com/ArnavBuild04/payguard/core/internal/payment/service"
	"github.com/ArnavBuild04/payguard/core/internal/provider"
	"github.com/ArnavBuild04/payguard/core/internal/reconcile/detector"
	reconcilehttpapi "github.com/ArnavBuild04/payguard/core/internal/reconcile/httpapi"
	reconcilemodels "github.com/ArnavBuild04/payguard/core/internal/reconcile/models"
	reconcilerepo "github.com/ArnavBuild04/payguard/core/internal/reconcile/repo"
	reconcileservice "github.com/ArnavBuild04/payguard/core/internal/reconcile/service"
	unmatchedwebhookmodels "github.com/ArnavBuild04/payguard/core/internal/unmatchedwebhook/models"
	"github.com/ArnavBuild04/payguard/core/internal/wallet/httpapi"
	"github.com/ArnavBuild04/payguard/core/internal/wallet/models"
	"github.com/ArnavBuild04/payguard/core/internal/wallet/repo"
	"github.com/ArnavBuild04/payguard/core/internal/wallet/service"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

const (
	topicPaymentEvents = "payment.events"
	topicPaymentDLQ    = "payment.dlq"
	dtcConsumerGroup   = "dtc-consumer"

	detectorSweepInterval    = 5 * time.Second
	autoResolveSweepInterval = 5 * time.Second
)

func main() {
	cfg, err := config.Load("config.json")
	if err != nil {
		log.Fatalf("failed to load config: %v", err)
	}

	slog.Info("starting service", "app", cfg.AppName, "version", cfg.Version)

	dsn := fmt.Sprintf("host=%s user=%s password=%s dbname=%s port=%d sslmode=disable TimeZone=UTC",
		cfg.Database.Host, cfg.Database.User, cfg.Database.Password, cfg.Database.Name, cfg.Database.Port)

	// GORM's default logger reports expected outcomes as ERROR; silenced in favor of httpapi's own logging.
	db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Silent),
	})
	if err != nil {
		log.Fatalf("failed to connect to database: %v", err)
	}

	if err := db.AutoMigrate(
		&models.Account{}, &models.LedgerEntry{},
		&paymentmodels.Payment{}, &paymentmodels.PaymentEvent{},
		&outboxmodels.Event{},
		&coordinatormodels.Transaction{},
		&kafkamodels.ProcessedEvent{},
		&reconcilemodels.Case{},
		&unmatchedwebhookmodels.UnmatchedWebhook{},
	); err != nil {
		log.Fatalf("failed to auto-migrate models: %v", err)
	}
	if err := reconcilerepo.Migrate(db); err != nil {
		log.Fatalf("failed to create reconciliation indexes: %v", err)
	}

	cacheClient := cache.New(cfg.Redis.Addr)
	defer cacheClient.Close()

	svc := service.NewService(repo.NewRepo(db))

	providerClient := provider.NewHTTPProvider(cfg.Provider.BaseURL, &http.Client{Timeout: providerTimeout(cfg)})
	webhookURL := fmt.Sprintf("http://localhost:%d/v1/payments/webhooks", cfg.HTTPPort)
	paymentSvc := paymentservice.NewService(paymentrepo.NewRepo(db), providerClient, webhookURL, cacheClient)

	assetGranter := coordinatorgranter.NewHTTPAssetGranter(cfg.Coordinator.AssetServiceURL, &http.Client{Timeout: 5 * time.Second})
	ticketGranter := coordinatorgranter.NewHTTPTicketGranter(cfg.Coordinator.TicketServiceURL, &http.Client{Timeout: 5 * time.Second})
	coordinatorSvc := coordinatorservice.NewService(coordinatorrepo.NewRepo(db), svc, assetGranter, ticketGranter)

	// The relay publishes to Kafka; the consumer below does the actual fan-out.
	eventsProducer := pgkafka.NewProducer(cfg.Kafka.Brokers, topicPaymentEvents)
	defer eventsProducer.Close()
	dlqProducer := pgkafka.NewProducer(cfg.Kafka.Brokers, topicPaymentDLQ)
	defer dlqProducer.Close()

	relayCtx, stopRelay := context.WithCancel(context.Background())
	defer stopRelay()
	go outbox.RunRelay(relayCtx, db, cacheClient, kafkaPublishHandler(eventsProducer), 2*time.Second)
	go pgkafka.RunConsumer(relayCtx, db, cfg.Kafka.Brokers, topicPaymentEvents, dtcConsumerGroup, dlqProducer, dtcEventHandler(coordinatorSvc, paymentSvc))

	reconcileRepo := reconcilerepo.NewRepo(db)
	det := detector.New(db, providerClient, reconcileRepo)
	go det.Run(relayCtx, detectorSweepInterval)

	reconcileSvc := reconcileservice.NewService(reconcileRepo, paymentSvc, coordinatorSvc, cfg.Reconcile.AutoResolveCeilingMinor, cfg.Reconcile.MaxAutoAttempts)
	go reconcileSvc.RunAutoResolver(relayCtx, autoResolveSweepInterval)

	mux := http.NewServeMux()
	httpapi.RegisterRoutes(mux, svc)
	paymenthttpapi.RegisterRoutes(mux, paymentSvc)
	reconcilehttpapi.RegisterRoutes(mux, reconcileSvc)

	srv := &http.Server{
		Addr:              httpAddr(cfg),
		Handler:           mux,
		ReadHeaderTimeout: 5 * time.Second,
	}

	errCh := make(chan error, 1)
	go func() {
		slog.Info("listening", "addr", srv.Addr)
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			errCh <- err
		}
	}()

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, os.Interrupt, syscall.SIGTERM)

	select {
	case err := <-errCh:
		log.Fatalf("server error: %v", err)
	case <-stop:
		slog.Info("shutdown signal received")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := srv.Shutdown(ctx); err != nil {
		slog.Error("graceful shutdown failed", "err", err)
	} else {
		slog.Info("shutdown complete")
	}
}

func httpAddr(cfg *config.Config) string {
	if cfg.HTTPPort == 0 {
		return ":8080"
	}
	return fmt.Sprintf(":%d", cfg.HTTPPort)
}

// providerTimeout is overridable by PAYGUARD_PROVIDER_TIMEOUT_MS at runtime.
func providerTimeout(cfg *config.Config) time.Duration {
	if v := os.Getenv("PAYGUARD_PROVIDER_TIMEOUT_MS"); v != "" {
		if ms, err := strconv.Atoi(v); err == nil {
			return time.Duration(ms) * time.Millisecond
		}
	}
	if cfg.Provider.TimeoutMS > 0 {
		return time.Duration(cfg.Provider.TimeoutMS) * time.Millisecond
	}
	return 5 * time.Second
}

// kafkaPublishHandler is the outbox relay's dispatch function: a real Kafka publish.
func kafkaPublishHandler(producer *pgkafka.Producer) outbox.Handler {
	return func(ctx context.Context, ev outboxmodels.Event) error {
		return producer.Publish(ctx, ev.AggregateID, pgkafka.Envelope{
			EventID:     strconv.FormatUint(ev.ID, 10),
			EventType:   ev.EventType,
			AggregateID: ev.AggregateID,
			PayloadJSON: ev.PayloadJSON,
		})
	}
}

// dtcEventHandler is the consumer side of payment.events.
func dtcEventHandler(coordinatorSvc coordinatorservice.Service, paymentSvc paymentservice.Service) pgkafka.Handler {
	return func(ctx context.Context, env pgkafka.Envelope) error {
		switch env.EventType {
		case "PAYMENT_SUCCEEDED":
			var payload struct {
				PaymentID   uint64 `json:"payment_id"`
				TenantID    string `json:"tenant_id"`
				UserID      int64  `json:"user_id"`
				SKU         string `json:"sku"`
				AmountMinor int64  `json:"amount_minor"`
			}
			if err := json.Unmarshal([]byte(env.PayloadJSON), &payload); err != nil {
				return fmt.Errorf("decode PAYMENT_SUCCEEDED payload: %w", err)
			}
			return coordinatorSvc.HandlePaymentSucceeded(ctx, payload.PaymentID, payload.TenantID, payload.UserID, payload.SKU, payload.AmountMinor)

		case "PAYMENT_REFUND_PENDING":
			// The only place that ever calls Compensate.
			var payload struct {
				PaymentID uint64 `json:"payment_id"`
			}
			if err := json.Unmarshal([]byte(env.PayloadJSON), &payload); err != nil {
				return fmt.Errorf("decode PAYMENT_REFUND_PENDING payload: %w", err)
			}
			if err := coordinatorSvc.Compensate(ctx, payload.PaymentID); err != nil {
				return err
			}
			return paymentSvc.MarkRefunded(ctx, payload.PaymentID)

		default:
			return nil
		}
	}
}
