// Package httpapi is the thin HTTP layer over the reconcile service.
package httpapi

import (
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"strconv"

	"github.com/ArnavBuild04/payguard/core/internal/reconcile/models"
	"github.com/ArnavBuild04/payguard/core/internal/reconcile/reconcileerr"
	"github.com/ArnavBuild04/payguard/core/internal/reconcile/service"
)

func RegisterRoutes(mux *http.ServeMux, svc service.Service) {
	mux.HandleFunc("GET /v1/reconciliation-cases", handleList(svc))
	mux.HandleFunc("GET /v1/reconciliation-cases/{id}", handleGet(svc))
	mux.HandleFunc("POST /v1/reconciliation-cases/{id}/approve", handleApprove(svc))
	mux.HandleFunc("POST /v1/reconciliation-cases/{id}/dismiss", handleDismiss(svc))
}

func handleList(svc service.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		cases, err := svc.ListOpen(r.Context())
		if err != nil {
			slog.Error("reconcile: list open failed", "err", err)
			writeError(w, http.StatusInternalServerError, "internal error")
			return
		}
		writeJSON(w, http.StatusOK, cases)
	}
}

func handleGet(svc service.Service) http.HandlerFunc {
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
			slog.Error("reconcile: get case failed", "err", err)
			writeError(w, http.StatusInternalServerError, "internal error")
		}
	}
}

type approveRequest struct {
	Actor  string        `json:"actor"`
	Action models.Action `json:"action,omitempty"`
}

func handleApprove(svc service.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id, err := strconv.ParseUint(r.PathValue("id"), 10, 64)
		if err != nil {
			writeError(w, http.StatusBadRequest, "invalid case id")
			return
		}
		var req approveRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeError(w, http.StatusBadRequest, "invalid request body")
			return
		}
		if req.Actor == "" {
			writeError(w, http.StatusBadRequest, "actor is required")
			return
		}

		err = svc.Approve(r.Context(), id, req.Actor, req.Action)
		switch {
		case err == nil:
			slog.Info("reconcile: case approved", "case_id", id, "actor", req.Actor)
			writeJSON(w, http.StatusOK, map[string]string{"status": "resolved"})
		case errors.Is(err, reconcileerr.ErrCaseNotFound):
			writeError(w, http.StatusNotFound, err.Error())
		case errors.Is(err, reconcileerr.ErrCaseNotOpen):
			writeError(w, http.StatusConflict, err.Error())
		case errors.Is(err, reconcileerr.ErrUnknownAction):
			writeError(w, http.StatusBadRequest, err.Error())
		default:
			slog.Error("reconcile: approve failed", "case_id", id, "err", err)
			writeError(w, http.StatusInternalServerError, "internal error")
		}
	}
}

type dismissRequest struct {
	Actor string `json:"actor"`
	Note  string `json:"note"`
}

func handleDismiss(svc service.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id, err := strconv.ParseUint(r.PathValue("id"), 10, 64)
		if err != nil {
			writeError(w, http.StatusBadRequest, "invalid case id")
			return
		}
		var req dismissRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeError(w, http.StatusBadRequest, "invalid request body")
			return
		}
		if req.Actor == "" {
			writeError(w, http.StatusBadRequest, "actor is required")
			return
		}

		err = svc.Dismiss(r.Context(), id, req.Actor, req.Note)
		switch {
		case err == nil:
			slog.Info("reconcile: case dismissed", "case_id", id, "actor", req.Actor)
			writeJSON(w, http.StatusOK, map[string]string{"status": "dismissed"})
		case errors.Is(err, reconcileerr.ErrNoteRequired):
			writeError(w, http.StatusBadRequest, err.Error())
		case errors.Is(err, reconcileerr.ErrCaseNotFound):
			writeError(w, http.StatusNotFound, err.Error())
		case errors.Is(err, reconcileerr.ErrCaseNotOpen):
			writeError(w, http.StatusConflict, err.Error())
		default:
			slog.Error("reconcile: dismiss failed", "case_id", id, "err", err)
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
