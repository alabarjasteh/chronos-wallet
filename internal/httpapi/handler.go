package httpapi

import (
	"encoding/json"
	"errors"
	"net/http"
	"time"

	"github.com/alibaba/chronos-wallet/internal/service"
	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
)

type Handler struct {
	service *service.Service
}

func New(service *service.Service) http.Handler {
	h := &Handler{service: service}

	r := chi.NewRouter()
	r.Use(middleware.RequestID)
	r.Use(middleware.RealIP)
	r.Use(middleware.Recoverer)
	r.Use(middleware.Timeout(30 * time.Second))

	r.Get("/healthz", h.health)
	r.Get("/swagger", redirectSwagger)
	r.Get("/swagger/", swaggerUI)
	r.Get("/swagger/openapi.yaml", openAPISpec)
	r.Post("/wallets", h.createWallet)
	r.Get("/wallets/{walletID}/balance", h.balance)
	r.Post("/wallets/{walletID}/deposits", h.deposit)
	r.Post("/wallets/{walletID}/withdrawals", h.scheduleWithdrawal)
	r.Get("/withdrawals/{withdrawalID}", h.withdrawal)

	return r
}

func (h *Handler) health(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (h *Handler) createWallet(w http.ResponseWriter, r *http.Request) {
	wallet, err := h.service.CreateWallet(r.Context())
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, wallet)
}

func (h *Handler) balance(w http.ResponseWriter, r *http.Request) {
	balance, err := h.service.Balance(r.Context(), chi.URLParam(r, "walletID"))
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, balance)
}

func (h *Handler) deposit(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Amount int64 `json:"amount"`
	}
	if !decodeJSON(w, r, &req) {
		return
	}
	balance, err := h.service.Deposit(r.Context(), chi.URLParam(r, "walletID"), req.Amount)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, balance)
}

func (h *Handler) scheduleWithdrawal(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Amount    int64  `json:"amount"`
		ExecuteAt string `json:"execute_at"`
	}
	if !decodeJSON(w, r, &req) {
		return
	}
	executeAt, err := time.Parse(time.RFC3339, req.ExecuteAt)
	if err != nil {
		writeProblem(w, http.StatusBadRequest, "execute_at must be an RFC3339 timestamp")
		return
	}
	withdrawal, err := h.service.ScheduleWithdrawal(r.Context(), chi.URLParam(r, "walletID"), req.Amount, executeAt)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusAccepted, withdrawal)
}

func (h *Handler) withdrawal(w http.ResponseWriter, r *http.Request) {
	withdrawal, err := h.service.Withdrawal(r.Context(), chi.URLParam(r, "withdrawalID"))
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, withdrawal)
}

func decodeJSON(w http.ResponseWriter, r *http.Request, dst any) bool {
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(dst); err != nil {
		writeProblem(w, http.StatusBadRequest, "invalid JSON request body")
		return false
	}
	return true
}

func writeError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, service.ErrInvalidAmount),
		errors.Is(err, service.ErrExecuteAtNotFuture):
		writeProblem(w, http.StatusBadRequest, err.Error())
	case errors.Is(err, service.ErrWalletNotFound),
		errors.Is(err, service.ErrWithdrawalNotFound):
		writeProblem(w, http.StatusNotFound, err.Error())
	default:
		writeProblem(w, http.StatusInternalServerError, "internal server error")
	}
}

func writeProblem(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, map[string]string{"error": message})
}

func writeJSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}
