// Package httpapi is the thin HTTP layer over the wallet service.
package httpapi

import (
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"strconv"

	"github.com/ArnavBuild04/payguard/core/internal/wallet/models"
	"github.com/ArnavBuild04/payguard/core/internal/wallet/service"
	"github.com/ArnavBuild04/payguard/core/internal/wallet/walleterr"
)

func RegisterRoutes(mux *http.ServeMux, svc service.Service) {
	mux.HandleFunc("GET /health", handleHealth)
	mux.HandleFunc("POST /v1/wallet/debit", handleDebit(svc))
	mux.HandleFunc("POST /v1/wallet/credit", handleCredit(svc))
	mux.HandleFunc("GET /v1/wallet/{tenantID}/{userID}/balance", handleBalance(svc))
}

func handleHealth(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

type walletRequest struct {
	TenantID    string        `json:"tenant_id"`
	UserID      int64         `json:"user_id"`
	Amount      int64         `json:"amount"`
	Source      models.Source `json:"source"`
	ReferenceID string        `json:"reference_id"`
}

func handleDebit(svc service.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req walletRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeError(w, http.StatusBadRequest, "invalid request body")
			return
		}
		err := svc.Debit(r.Context(), req.TenantID, req.UserID, req.Amount, req.Source, req.ReferenceID)
		respondMutation(w, err)
	}
}

func handleCredit(svc service.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req walletRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeError(w, http.StatusBadRequest, "invalid request body")
			return
		}
		err := svc.Credit(r.Context(), req.TenantID, req.UserID, req.Amount, req.Source, req.ReferenceID)
		respondMutation(w, err)
	}
}

// respondMutation logs expected outcomes at Info and unclassified failures at Error; already-processed is HTTP 200, not an error.
func respondMutation(w http.ResponseWriter, err error) {
	switch {
	case err == nil:
		writeJSON(w, http.StatusOK, map[string]string{"status": "applied"})
	case errors.Is(err, walleterr.ErrAlreadyProcessed):
		slog.Info("wallet mutation replayed", "reason", "already_processed")
		writeJSON(w, http.StatusOK, map[string]string{"status": "already_processed"})
	case errors.Is(err, walleterr.ErrAccountNotFound):
		slog.Info("wallet mutation rejected", "reason", "account_not_found")
		writeError(w, http.StatusNotFound, err.Error())
	case errors.Is(err, walleterr.ErrInsufficientBalance):
		slog.Info("wallet mutation rejected", "reason", "insufficient_balance")
		writeError(w, http.StatusConflict, err.Error())
	case errors.Is(err, walleterr.ErrInvalidAmount):
		slog.Info("wallet mutation rejected", "reason", "invalid_amount")
		writeError(w, http.StatusBadRequest, err.Error())
	default:
		slog.Error("wallet mutation failed unexpectedly", "err", err)
		writeError(w, http.StatusInternalServerError, "internal error")
	}
}

func handleBalance(svc service.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		tenantID := r.PathValue("tenantID")
		userID, err := strconv.ParseInt(r.PathValue("userID"), 10, 64)
		if err != nil {
			writeError(w, http.StatusBadRequest, "invalid user id")
			return
		}

		account, err := svc.GetAccount(r.Context(), tenantID, userID)
		switch {
		case err == nil:
			writeJSON(w, http.StatusOK, account)
		case errors.Is(err, walleterr.ErrAccountNotFound):
			slog.Info("balance lookup rejected", "reason", "account_not_found")
			writeError(w, http.StatusNotFound, err.Error())
		default:
			slog.Error("balance lookup failed unexpectedly", "err", err)
			writeError(w, http.StatusInternalServerError, "internal error")
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
