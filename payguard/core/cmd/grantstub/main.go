// Command grantstub is the standalone stand-in for the asset and ticket grant services — "their own
// grant tables, stubs for now, real interface" per hld.md §1. It is deliberately a separate process
// (not mounted inside cmd/server) so the partial-bundle chaos scenario in hld.md §9.5 — "stop one
// grant service; chips land, the ticket does not" — is produced by genuinely killing this process,
// not by a rigged failure flag.
package main

import (
	"context"
	"encoding/json"
	"errors"
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

// store dedups grant/revoke calls by order_id — a redelivered fan-out event (hld.md edge case F1)
// must be a no-op here, not a second grant.
type store struct {
	mu      sync.Mutex
	granted map[uint64]bool
}

func newStore() *store {
	return &store{granted: make(map[uint64]bool)}
}

func (s *store) grant(orderID uint64) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.granted[orderID] = true
}

func (s *store) revoke(orderID uint64) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.granted, orderID)
}

type grantRequestWire struct {
	TenantID string `json:"tenant_id"`
	UserID   int64  `json:"user_id"`
	OrderID  uint64 `json:"order_id"`
}

func registerRoutes(mux *http.ServeMux, name string, s *store) {
	mux.HandleFunc("GET /health", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok", "service": name})
	})

	for _, resource := range []string{"assets", "tickets"} {
		mux.HandleFunc("POST /v1/"+resource+"/grant", func(w http.ResponseWriter, r *http.Request) {
			var req grantRequestWire
			if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
				writeError(w, http.StatusBadRequest, "invalid request body")
				return
			}
			s.grant(req.OrderID)
			slog.Info("grantstub: granted", "resource", resource, "order_id", req.OrderID)
			writeJSON(w, http.StatusOK, map[string]string{"status": "granted"})
		})

		mux.HandleFunc("POST /v1/"+resource+"/revoke", func(w http.ResponseWriter, r *http.Request) {
			var req grantRequestWire
			if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
				writeError(w, http.StatusBadRequest, "invalid request body")
				return
			}
			s.revoke(req.OrderID)
			slog.Info("grantstub: revoked", "resource", resource, "order_id", req.OrderID)
			writeJSON(w, http.StatusOK, map[string]string{"status": "revoked"})
		})
	}
}

func main() {
	name := envString("PAYGUARD_GRANTSTUB_NAME", "grantstub")
	port := envInt("PAYGUARD_GRANTSTUB_PORT", 9100)
	addr := ":" + strconv.Itoa(port)

	s := newStore()
	mux := http.NewServeMux()
	registerRoutes(mux, name, s)

	srv := &http.Server{
		Addr:              addr,
		Handler:           mux,
		ReadHeaderTimeout: 5 * time.Second,
	}

	errCh := make(chan error, 1)
	go func() {
		slog.Info("grantstub listening", "name", name, "addr", addr)
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

func envString(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
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
