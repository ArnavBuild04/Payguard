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

	coordinatorgranter "github.com/ArnavBuild04/payguard/core/internal/coordinator/granter"
	coordinatormodels "github.com/ArnavBuild04/payguard/core/internal/coordinator/models"
	coordinatorrepo "github.com/ArnavBuild04/payguard/core/internal/coordinator/repo"
	coordinatorservice "github.com/ArnavBuild04/payguard/core/internal/coordinator/service"

	"github.com/ArnavBuild04/payguard/core/internal/config"
	"github.com/ArnavBuild04/payguard/core/internal/outbox"
	outboxmodels "github.com/ArnavBuild04/payguard/core/internal/outbox/models"
	paymenthttpapi "github.com/ArnavBuild04/payguard/core/internal/payment/httpapi"
	paymentmodels "github.com/ArnavBuild04/payguard/core/internal/payment/models"
	paymentrepo "github.com/ArnavBuild04/payguard/core/internal/payment/repo"
	paymentservice "github.com/ArnavBuild04/payguard/core/internal/payment/service"
	"github.com/ArnavBuild04/payguard/core/internal/provider"
	"github.com/ArnavBuild04/payguard/core/internal/wallet/httpapi"
	"github.com/ArnavBuild04/payguard/core/internal/wallet/models"
	"github.com/ArnavBuild04/payguard/core/internal/wallet/repo"
	"github.com/ArnavBuild04/payguard/core/internal/wallet/service"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func main() {
	cfg, err := config.Load("config.json")
	if err != nil {
		log.Fatalf("failed to load config: %v", err)
	}

	slog.Info("starting service", "app", cfg.AppName, "version", cfg.Version)

	dsn := fmt.Sprintf("host=%s user=%s password=%s dbname=%s port=%d sslmode=disable TimeZone=UTC",
		cfg.Database.Host, cfg.Database.User, cfg.Database.Password, cfg.Database.Name, cfg.Database.Port)

	// GORM's default logger reports expected outcomes (idempotent replay, not-found) as ERROR; silenced in favor of httpapi's leveled logging.
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
	); err != nil {
		log.Fatalf("failed to auto-migrate models: %v", err)
	}

	svc := service.NewService(repo.NewRepo(db))

	providerClient := provider.NewHTTPProvider(cfg.Provider.BaseURL, &http.Client{Timeout: providerTimeout(cfg)})
	paymentSvc := paymentservice.NewService(paymentrepo.NewRepo(db), providerClient)

	assetGranter := coordinatorgranter.NewHTTPAssetGranter(cfg.Coordinator.AssetServiceURL, &http.Client{Timeout: 5 * time.Second})
	ticketGranter := coordinatorgranter.NewHTTPTicketGranter(cfg.Coordinator.TicketServiceURL, &http.Client{Timeout: 5 * time.Second})
	coordinatorSvc := coordinatorservice.NewService(coordinatorrepo.NewRepo(db), svc, assetGranter, ticketGranter)

	relayCtx, stopRelay := context.WithCancel(context.Background())
	defer stopRelay()
	go outbox.RunRelay(relayCtx, db, paymentSucceededHandler(coordinatorSvc), 2*time.Second)

	mux := http.NewServeMux()
	httpapi.RegisterRoutes(mux, svc)
	paymenthttpapi.RegisterRoutes(mux, paymentSvc)

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

// providerTimeout is deliberately overridable by PAYGUARD_PROVIDER_TIMEOUT_MS at runtime — hld.md
// §9.5's production knob for genuinely producing PROVIDER_AHEAD, not a test-only setting.
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

// paymentSucceededHandler is the outbox relay's dispatch function — the in-process stand-in for
// Kafka's payment.events topic in this phase (PLAN.md Phase B1). It decodes the event generically
// rather than importing payment's own payload type, so outbox and coordinator never need to know
// about payment's internals — only main.go glues the three together.
func paymentSucceededHandler(coordinatorSvc coordinatorservice.Service) outbox.Handler {
	return func(ctx context.Context, ev outboxmodels.Event) error {
		if ev.EventType != "PAYMENT_SUCCEEDED" {
			return nil
		}

		var payload struct {
			PaymentID   uint64 `json:"payment_id"`
			TenantID    string `json:"tenant_id"`
			UserID      int64  `json:"user_id"`
			SKU         string `json:"sku"`
			AmountMinor int64  `json:"amount_minor"`
		}
		if err := json.Unmarshal([]byte(ev.PayloadJSON), &payload); err != nil {
			return fmt.Errorf("decode PAYMENT_SUCCEEDED payload: %w", err)
		}

		return coordinatorSvc.HandlePaymentSucceeded(ctx, payload.PaymentID, payload.TenantID, payload.UserID, payload.SKU, payload.AmountMinor)
	}
}
