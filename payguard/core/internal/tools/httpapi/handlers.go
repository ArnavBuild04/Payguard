// Package httpapi exposes read-only evidence-gathering endpoints for the Python agent
// (Phase D). Every route here is GET-only and calls the same service layer the customer-facing
// APIs use — there is no separate write path, and no route under /internal/tools/ can move
// money or change any record. The agent investigates through this surface and this surface
// alone; it never touches /v1/reconciliation-cases/{id}/approve.
package httpapi

import (
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"strconv"

	paymenterr "github.com/ArnavBuild04/payguard/core/internal/payment/paymenterr"
	paymentservice "github.com/ArnavBuild04/payguard/core/internal/payment/service"
	"github.com/ArnavBuild04/payguard/core/internal/provider"
	"github.com/ArnavBuild04/payguard/core/internal/provider/providererr"
	"github.com/ArnavBuild04/payguard/core/internal/reconcile/reconcileerr"
	reconcileservice "github.com/ArnavBuild04/payguard/core/internal/reconcile/service"
	walletservice "github.com/ArnavBuild04/payguard/core/internal/wallet/service"
	walleterr "github.com/ArnavBuild04/payguard/core/internal/wallet/walleterr"
)

func RegisterRoutes(mux *http.ServeMux, reconcileSvc reconcileservice.Service, paymentSvc paymentservice.Service, walletSvc walletservice.Service, providerClient provider.Provider) {
	mux.HandleFunc("GET /internal/tools/cases/{id}", handleCase(reconcileSvc))
	mux.HandleFunc("GET /internal/tools/payments/{id}", handlePayment(paymentSvc))
	mux.HandleFunc("GET /internal/tools/wallet/{tenantID}/{userID}/balance", handleBalance(walletSvc))
	mux.HandleFunc("GET /internal/tools/provider/payments/{providerPaymentID}", handleProviderPayment(providerClient))
}

func handleCase(svc reconcileservice.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id, err := strconv.ParseUint(r.PathValue("id"), 10, 64)
		if err != nil {
			writeError(w, http.StatusBadRequest, "invalid case id")
			return
		}
		c, err := svc.GetCase(r.Context(), id)
		switch {
		case err == nil:
			writeJSON(w, http.StatusOK, c)
		case errors.Is(err, reconcileerr.ErrCaseNotFound):
			writeError(w, http.StatusNotFound, err.Error())
		default:
			slog.Error("tools: get case failed", "err", err)
			writeError(w, http.StatusInternalServerError, "internal error")
		}
	}
}

func handlePayment(svc paymentservice.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id, err := strconv.ParseUint(r.PathValue("id"), 10, 64)
		if err != nil {
			writeError(w, http.StatusBadRequest, "invalid payment id")
			return
		}
		p, err := svc.GetPayment(r.Context(), id)
		switch {
		case err == nil:
			writeJSON(w, http.StatusOK, p)
		case errors.Is(err, paymenterr.ErrPaymentNotFound):
			writeError(w, http.StatusNotFound, err.Error())
		default:
			slog.Error("tools: get payment failed", "err", err)
			writeError(w, http.StatusInternalServerError, "internal error")
		}
	}
}

func handleBalance(svc walletservice.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		tenantID := r.PathValue("tenantID")
		userID, err := strconv.ParseInt(r.PathValue("userID"), 10, 64)
		if err != nil {
			writeError(w, http.StatusBadRequest, "invalid user id")
			return
		}
		acct, err := svc.GetAccount(r.Context(), tenantID, userID)
		switch {
		case err == nil:
			writeJSON(w, http.StatusOK, acct)
		case errors.Is(err, walleterr.ErrAccountNotFound):
			writeError(w, http.StatusNotFound, err.Error())
		default:
			slog.Error("tools: get balance failed", "err", err)
			writeError(w, http.StatusInternalServerError, "internal error")
		}
	}
}

// handleProviderPayment is a live, authoritative lookup straight from the provider — unlike the
// evidence snapshot baked into a case at detection time, this reflects the provider's current
// state, which is exactly what the agent needs to check for staleness.
func handleProviderPayment(providerClient provider.Provider) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id := r.PathValue("providerPaymentID")
		if id == "" {
			writeError(w, http.StatusBadRequest, "provider payment id is required")
			return
		}
		p, err := providerClient.GetPayment(r.Context(), id)
		if err == nil {
			writeJSON(w, http.StatusOK, p)
			return
		}
		var use *providererr.UnknownStatusError
		switch {
		case errors.As(err, &use):
			writeJSON(w, http.StatusOK, map[string]string{"raw_status": use.Raw, "note": "unrecognized provider status"})
		case errors.Is(err, providererr.ErrNotFound):
			writeError(w, http.StatusNotFound, err.Error())
		default:
			writeError(w, http.StatusBadGateway, "provider lookup failed: "+err.Error())
		}
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
