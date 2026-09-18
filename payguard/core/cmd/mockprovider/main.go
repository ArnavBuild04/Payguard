// Command mockprovider is a standalone, always-honest payment provider — the "ideal mock" from
// hld.md §9.5. It has a real PSP's API surface (create, get, refund, outbound webhooks) and no
// failure-injection mechanism: every failure the running system produces at the provider boundary
// comes from genuinely breaking something (a short client timeout, stopping this process, a real
// refund call), never from this service being asked to lie. See hld.md §9.5 and PLAN.md Phase B1.
package main

import (
	"bytes"
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
	"sync"
	"syscall"
	"time"
)

// confirmDelay is how long a created payment honestly stays "processing" before this service
// confirms it — real card networks take real time. A client whose own timeout is shorter than this
// genuinely misses the confirmation, which is what produces PROVIDER_AHEAD without rigging anything.
var confirmDelay = envDuration("PAYGUARD_MOCK_CONFIRM_DELAY_MS", 800*time.Millisecond)

type paymentRecord struct {
	ID             string
	IdempotencyKey string
	Status         string // "processing" | "succeeded" | "failed" | "refunded"
	AmountMinor    int64
	Currency       string
	WebhookURL     string
	CreatedAt      time.Time
}

type store struct {
	mu          sync.Mutex
	payments    map[string]*paymentRecord
	byIdemKey   map[string]string // idempotency key -> payment id, so a retried create is a no-op
	nextID      int64
	nextEventID int64
	startedAt   int64
}

func newStore() *store {
	return &store{
		payments:  make(map[string]*paymentRecord),
		byIdemKey: make(map[string]string),
		startedAt: time.Now().UnixNano(),
	}
}

// createOrReplay honors provider-side idempotency: a create with a key we've already seen returns
// the original payment as it currently stands, exactly like a real PSP — this is the Purchase →
// Provider layer from hld.md §3 that stops a client retry from double-charging. The bool reports
// whether this call actually created the row (false means a replay).
func (s *store) createOrReplay(idemKey string, amountMinor int64, currency, webhookURL string) (*paymentRecord, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if id, ok := s.byIdemKey[idemKey]; ok {
		return s.payments[id], false
	}

	s.nextID++
	rec := &paymentRecord{
		// Seeded with the process start time so a restart never reissues an ID a prior run already
		// gave out — a real PSP's IDs are never reused either, and our own uniqueness constraint on
		// provider_payment_id is enforced durably forever, long after this in-memory store resets.
		ID:             fmt.Sprintf("pay_%d_%d", s.startedAt, s.nextID),
		IdempotencyKey: idemKey,
		Status:         "processing",
		AmountMinor:    amountMinor,
		Currency:       currency,
		WebhookURL:     webhookURL,
		CreatedAt:      time.Now().UTC(),
	}
	s.payments[rec.ID] = rec
	s.byIdemKey[idemKey] = rec.ID
	return rec, true
}

func (s *store) get(id string) (*paymentRecord, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	rec, ok := s.payments[id]
	return rec, ok
}

// confirmSync blocks for confirmDelay — a real card network round trip — then flips the payment to
// succeeded and fires a webhook if one was registered, returning a snapshot of the final state.
// Blocking here (not a detached goroutine) is what makes hld.md §6.1's happy path work with no
// webhook receiver or poller needed: the caller's own CreatePayment response already carries the
// terminal status. A client whose own timeout is shorter than confirmDelay gives up on the response
// before this returns — a genuine PROVIDER_AHEAD, since this goroutine (and the record it's about
// to update) keeps running server-side regardless of whether anyone is still listening.
func (s *store) confirmSync(rec *paymentRecord) *paymentRecord {
	time.Sleep(confirmDelay)

	s.mu.Lock()
	rec.Status = "succeeded"
	webhookURL := rec.WebhookURL
	s.nextEventID++
	eventID := fmt.Sprintf("evt_%d", s.nextEventID)
	snapshot := *rec
	s.mu.Unlock()

	if webhookURL != "" {
		deliverWebhook(webhookURL, eventID, &snapshot)
	}
	return &snapshot
}

func (s *store) refund(id string, amountMinor int64) (*paymentRecord, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	rec, ok := s.payments[id]
	if !ok {
		return nil, errNotFound
	}
	rec.Status = "refunded"
	return rec, nil
}

var errNotFound = errors.New("not found")

// deliverWebhook is fire-and-forget with a short timeout — a stopped listener or unreachable URL
// just fails quietly, exactly like a real misconfigured callback. Polling (GetPayment) is the
// system's actual source of truth per hld.md §9.5; this is an optimization only.
func deliverWebhook(url, eventID string, rec *paymentRecord) {
	body, err := json.Marshal(map[string]any{
		"event_id":   eventID,
		"event_type": "payment." + rec.Status,
		"payment": paymentWire{
			ID:          rec.ID,
			Status:      rec.Status,
			AmountMinor: rec.AmountMinor,
			Currency:    rec.Currency,
			CreatedAt:   rec.CreatedAt,
		},
	})
	if err != nil {
		slog.Error("webhook: encode failed", "err", err)
		return
	}

	client := &http.Client{Timeout: 3 * time.Second}
	resp, err := client.Post(url, "application/json", bytes.NewReader(body))
	if err != nil {
		slog.Warn("webhook: delivery failed", "url", url, "err", err)
		return
	}
	defer resp.Body.Close()
	slog.Info("webhook: delivered", "url", url, "status", resp.StatusCode)
}

type paymentWire struct {
	ID          string    `json:"id"`
	Status      string    `json:"status"`
	AmountMinor int64     `json:"amount_minor"`
	Currency    string    `json:"currency"`
	CreatedAt   time.Time `json:"created_at"`
}

type refundWire struct {
	ID          string `json:"id"`
	PaymentID   string `json:"payment_id"`
	Status      string `json:"status"`
	AmountMinor int64  `json:"amount_minor"`
}

type createRequestWire struct {
	AmountMinor int64  `json:"amount_minor"`
	Currency    string `json:"currency"`
	WebhookURL  string `json:"webhook_url,omitempty"`
}

type refundRequestWire struct {
	AmountMinor int64 `json:"amount_minor"`
}

func toPaymentWire(rec *paymentRecord) paymentWire {
	return paymentWire{
		ID:          rec.ID,
		Status:      rec.Status,
		AmountMinor: rec.AmountMinor,
		Currency:    rec.Currency,
		CreatedAt:   rec.CreatedAt,
	}
}

func registerRoutes(mux *http.ServeMux, s *store) {
	mux.HandleFunc("GET /health", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
	})

	mux.HandleFunc("POST /v1/payments", func(w http.ResponseWriter, r *http.Request) {
		idemKey := r.Header.Get("Idempotency-Key")
		if idemKey == "" {
			writeError(w, http.StatusBadRequest, "Idempotency-Key header is required")
			return
		}

		var req createRequestWire
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeError(w, http.StatusBadRequest, "invalid request body")
			return
		}
		if req.AmountMinor <= 0 {
			writeError(w, http.StatusBadRequest, "amount_minor must be positive")
			return
		}

		rec, isNew := s.createOrReplay(idemKey, req.AmountMinor, req.Currency, req.WebhookURL)
		if isNew {
			// Block until settlement, like a real synchronous card-auth call — see confirmSync.
			rec = s.confirmSync(rec)
		}
		writeJSON(w, http.StatusCreated, toPaymentWire(rec))
	})

	mux.HandleFunc("GET /v1/payments/{id}", func(w http.ResponseWriter, r *http.Request) {
		rec, ok := s.get(r.PathValue("id"))
		if !ok {
			writeError(w, http.StatusNotFound, "payment not found")
			return
		}
		writeJSON(w, http.StatusOK, toPaymentWire(rec))
	})

	mux.HandleFunc("POST /v1/payments/{id}/refund", func(w http.ResponseWriter, r *http.Request) {
		var req refundRequestWire
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeError(w, http.StatusBadRequest, "invalid request body")
			return
		}

		rec, err := s.refund(r.PathValue("id"), req.AmountMinor)
		if errors.Is(err, errNotFound) {
			writeError(w, http.StatusNotFound, "payment not found")
			return
		}
		writeJSON(w, http.StatusOK, refundWire{
			ID:          "re_" + rec.ID,
			PaymentID:   rec.ID,
			Status:      rec.Status,
			AmountMinor: req.AmountMinor,
		})
	})
}

func main() {
	port := envInt("PAYGUARD_MOCK_PORT", 9090)
	addr := ":" + strconv.Itoa(port)

	s := newStore()
	mux := http.NewServeMux()
	registerRoutes(mux, s)

	srv := &http.Server{
		Addr:              addr,
		Handler:           mux,
		ReadHeaderTimeout: 5 * time.Second,
	}

	errCh := make(chan error, 1)
	go func() {
		slog.Info("mockprovider listening", "addr", addr, "confirm_delay", confirmDelay)
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

func writeJSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}

func writeError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}

func envDuration(key string, fallback time.Duration) time.Duration {
	v := os.Getenv(key)
	if v == "" {
		return fallback
	}
	ms, err := strconv.Atoi(v)
	if err != nil {
		return fallback
	}
	return time.Duration(ms) * time.Millisecond
}

func envInt(key string, fallback int) int {
	v := os.Getenv(key)
	if v == "" {
		return fallback
	}
	n, err := strconv.Atoi(v)
	if err != nil {
		return fallback
	}
	return n
}
