// Package httpapi is the thin HTTP layer over the payment (Purchase) service.
package httpapi

import (
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"strconv"

	"github.com/ArnavBuild04/payguard/core/internal/payment/paymenterr"
	"github.com/ArnavBuild04/payguard/core/internal/payment/service"
)

func RegisterRoutes(mux *http.ServeMux, svc service.Service) {
	mux.HandleFunc("POST /v1/payments", handleCreate(svc))
	mux.HandleFunc("GET /v1/payments/{id}", handleGet(svc))
	mux.HandleFunc("POST /v1/payments/webhooks", handleWebhook(svc))
}

type createRequest struct {
	TenantID string `json:"tenant_id"`
	UserID   int64  `json:"user_id"`
	SKU      string `json:"sku"`
}

func handleCreate(svc service.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req createRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeError(w, http.StatusBadRequest, "invalid request body")
			return
		}
		idemKey := r.Header.Get("Idempotency-Key")

		payment, err := svc.CreatePayment(r.Context(), req.TenantID, req.UserID, req.SKU, idemKey)
		switch {
		case err == nil:
			slog.Info("payment created", "payment_id", payment.ID)
			writeJSON(w, http.StatusAccepted, payment)
		case errors.Is(err, paymenterr.ErrAlreadyProcessed):
			slog.Info("payment create replayed", "payment_id", payment.ID)
			writeJSON(w, http.StatusOK, payment)
		case errors.Is(err, paymenterr.ErrFingerprintMismatch):
			slog.Info("payment create rejected", "reason", "fingerprint_mismatch")
			writeError(w, http.StatusConflict, err.Error())
		case errors.Is(err, paymenterr.ErrInvalidSKU):
			slog.Info("payment create rejected", "reason", "invalid_sku")
			writeError(w, http.StatusBadRequest, err.Error())
		case errors.Is(err, paymenterr.ErrMissingIdempotencyKey):
			slog.Info("payment create rejected", "reason", "missing_idempotency_key")
			writeError(w, http.StatusBadRequest, err.Error())
		default:
			slog.Error("payment create failed unexpectedly", "err", err)
			writeError(w, http.StatusInternalServerError, "internal error")
		}
	}
}

func handleGet(svc service.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id, err := strconv.ParseUint(r.PathValue("id"), 10, 64)
		if err != nil {
			writeError(w, http.StatusBadRequest, "invalid payment id")
			return
		}

		payment, err := svc.GetPayment(r.Context(), id)
		switch {
		case err == nil:
			writeJSON(w, http.StatusOK, payment)
		case errors.Is(err, paymenterr.ErrPaymentNotFound):
			writeError(w, http.StatusNotFound, err.Error())
		default:
			slog.Error("payment lookup failed unexpectedly", "err", err)
			writeError(w, http.StatusInternalServerError, "internal error")
		}
	}
}

// webhookWire matches the mock provider's outbound delivery shape.
type webhookWire struct {
	EventID   string `json:"event_id"`
	EventType string `json:"event_type"`
	Payment   struct {
		ID     string `json:"id"`
		Status string `json:"status"`
	} `json:"payment"`
}

// webhookSource identifies every inbound provider webhook uniformly.
const webhookSource = "provider"

func handleWebhook(svc service.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		if err != nil {
			writeError(w, http.StatusBadRequest, "failed to read request body")
			return
		}

		var wire webhookWire
		if err := json.Unmarshal(body, &wire); err != nil {
			writeError(w, http.StatusBadRequest, "invalid request body")
			return
		}
		if wire.EventID == "" || wire.Payment.ID == "" {
			writeError(w, http.StatusBadRequest, "event_id and payment.id are required")
			return
		}

		err = svc.HandleWebhook(r.Context(), webhookSource, wire.EventID, wire.EventType, wire.Payment.ID, wire.Payment.Status, string(body))
		if err != nil {
			slog.Error("payment: webhook handling failed unexpectedly", "err", err)
			writeError(w, http.StatusInternalServerError, "internal error")
			return
		}
		writeJSON(w, http.StatusOK, map[string]string{"status": "received"})
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
