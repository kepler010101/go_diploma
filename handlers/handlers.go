package handlers

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"time"

	"gophermart/repo"
	"gophermart/service"
)

const statusProcessed = "PROCESSED"

type Handler struct {
	auth     service.AuthService
	orders   service.OrderService
	balance  service.BalanceService
	withdraw service.WithdrawService
}

type orderResponse struct {
	Number     string   `json:"number"`
	Status     string   `json:"status"`
	Accrual    *float64 `json:"accrual,omitempty"`
	UploadedAt string   `json:"uploaded_at"`
}

type withdrawRequest struct {
	Order string  `json:"order"`
	Sum   float64 `json:"sum"`
}

type withdrawalResponse struct {
	Order       string  `json:"order"`
	Sum         float64 `json:"sum"`
	ProcessedAt string  `json:"processed_at"`
}

func NewRouter(auth service.AuthService, orders service.OrderService, balance service.BalanceService, withdraw service.WithdrawService) http.Handler {
	h := &Handler{auth: auth, orders: orders, balance: balance, withdraw: withdraw}
	mux := http.NewServeMux()
	mux.HandleFunc("/", h.handleRoot)
	mux.HandleFunc("/ping", h.handlePing)
	mux.Handle("/api/user/register", decompressGzip(http.HandlerFunc(h.handleRegister)))
	mux.Handle("/api/user/login", decompressGzip(http.HandlerFunc(h.handleLogin)))
	ordersHandler := wrapHandlerForGzip(http.HandlerFunc(h.authFromCookie(h.handleOrders)))
	mux.Handle("/api/user/orders", decompressGzip(ordersHandler))
	mux.HandleFunc("/api/user/balance", h.authFromCookie(h.handleBalance))
	mux.Handle("/api/user/balance/withdraw", decompressGzip(http.HandlerFunc(h.authFromCookie(h.handleWithdraw))))
	mux.Handle("/api/user/withdrawals", wrapHandlerForGzip(http.HandlerFunc(h.authFromCookie(h.handleWithdrawals))))
	return mux
}

type authRequest struct {
	Login    string `json:"login"`
	Password string `json:"password"`
}

func (h *Handler) handleRegister(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}

	var req authRequest
	if err := decodeAuthRequest(r, &req); err != nil {
		w.WriteHeader(http.StatusBadRequest)
		return
	}

	token, err := h.auth.Register(r.Context(), req.Login, req.Password)
	if err != nil {
		switch {
		case errors.Is(err, service.ErrLoginTaken):
			w.WriteHeader(http.StatusConflict)
		case errors.Is(err, service.ErrInvalidCredentials):
			w.WriteHeader(http.StatusBadRequest)
		default:
			w.WriteHeader(http.StatusInternalServerError)
		}
		return
	}

	setAuthCookie(w, token)
	w.WriteHeader(http.StatusOK)
}

func (h *Handler) handleLogin(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}

	var req authRequest
	if err := decodeAuthRequest(r, &req); err != nil {
		w.WriteHeader(http.StatusBadRequest)
		return
	}

	token, err := h.auth.Login(r.Context(), req.Login, req.Password)
	if err != nil {
		switch {
		case errors.Is(err, service.ErrInvalidCredentials):
			w.WriteHeader(http.StatusUnauthorized)
		default:
			w.WriteHeader(http.StatusInternalServerError)
		}
		return
	}

	setAuthCookie(w, token)
	w.WriteHeader(http.StatusOK)
}

func (h *Handler) handleOrders(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodPost:
		h.handleCreateOrder(w, r)
	case http.MethodGet:
		h.handleListOrders(w, r)
	default:
		w.WriteHeader(http.StatusMethodNotAllowed)
	}
}

func (h *Handler) handleListOrders(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}

	val := r.Context().Value(userIDContextKey)
	userID, ok := val.(int64)
	if !ok {
		w.WriteHeader(http.StatusUnauthorized)
		return
	}

	orders, err := h.orders.ListUserOrders(r.Context(), userID)
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		return
	}

	if len(orders) == 0 {
		w.WriteHeader(http.StatusNoContent)
		return
	}

	responses := make([]orderResponse, 0, len(orders))
	for _, order := range orders {
		item := orderResponse{
			Number:     order.Number,
			Status:     order.Status,
			UploadedAt: order.UploadedAt.Format(time.RFC3339),
		}
		if order.Status == statusProcessed && order.Accrual != nil {
			item.Accrual = order.Accrual
		}
		responses = append(responses, item)
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(responses)
}

func (h *Handler) handleBalance(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}

	val := r.Context().Value(userIDContextKey)
	userID, ok := val.(int64)
	if !ok {
		w.WriteHeader(http.StatusUnauthorized)
		return
	}

	current, withdrawn, err := h.balance.GetBalance(r.Context(), userID)
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		return
	}

	resp := map[string]float64{
		"current":   current,
		"withdrawn": withdrawn,
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(resp)
}

func (h *Handler) handleWithdrawals(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}

	val := r.Context().Value(userIDContextKey)
	userID, ok := val.(int64)
	if !ok {
		w.WriteHeader(http.StatusUnauthorized)
		return
	}

	withdrawals, err := h.withdraw.ListUserWithdrawals(r.Context(), userID)
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		return
	}

	if len(withdrawals) == 0 {
		w.WriteHeader(http.StatusNoContent)
		return
	}

	responses := make([]withdrawalResponse, 0, len(withdrawals))
	for _, wdr := range withdrawals {
		responses = append(responses, withdrawalResponse{
			Order:       wdr.OrderNumber,
			Sum:         wdr.Sum,
			ProcessedAt: wdr.ProcessedAt.Format(time.RFC3339),
		})
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(responses)
}

func (h *Handler) handleWithdraw(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}

	val := r.Context().Value(userIDContextKey)
	userID, ok := val.(int64)
	if !ok {
		w.WriteHeader(http.StatusUnauthorized)
		return
	}

	var req withdrawRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		w.WriteHeader(http.StatusBadRequest)
		return
	}

	order := strings.TrimSpace(req.Order)
	if err := service.ValidateOrderNumber(order); err != nil {
		w.WriteHeader(http.StatusUnprocessableEntity)
		return
	}

	if req.Sum <= 0 {
		w.WriteHeader(http.StatusBadRequest)
		return
	}

	err := h.withdraw.Withdraw(r.Context(), userID, order, req.Sum)
	if err != nil {
		switch {
		case errors.Is(err, service.ErrInvalidOrderNumber):
			w.WriteHeader(http.StatusUnprocessableEntity)
		case errors.Is(err, service.ErrInvalidWithdrawAmount):
			w.WriteHeader(http.StatusBadRequest)
		case errors.Is(err, repo.ErrInsufficientFunds):
			w.WriteHeader(http.StatusPaymentRequired)
		default:
			w.WriteHeader(http.StatusInternalServerError)
		}
		return
	}

	w.WriteHeader(http.StatusOK)
}

func (h *Handler) handleCreateOrder(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}

	if ct := r.Header.Get("Content-Type"); ct == "" || !strings.HasPrefix(ct, "text/plain") {
		w.WriteHeader(http.StatusBadRequest)
		return
	}

	if r.Body == nil {
		w.WriteHeader(http.StatusBadRequest)
		return
	}

	body, err := io.ReadAll(r.Body)
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		return
	}
	defer r.Body.Close()

	number := strings.TrimSpace(string(body))
	if err := service.ValidateOrderNumber(number); err != nil {
		w.WriteHeader(http.StatusUnprocessableEntity)
		return
	}

	val := r.Context().Value(userIDContextKey)
	userID, ok := val.(int64)
	if !ok {
		w.WriteHeader(http.StatusUnauthorized)
		return
	}

	created, err := h.orders.SubmitOrder(r.Context(), userID, number)
	if err != nil {
		switch {
		case errors.Is(err, service.ErrInvalidOrderNumber):
			w.WriteHeader(http.StatusUnprocessableEntity)
		case errors.Is(err, service.ErrOrderOwnedByAnother):
			w.WriteHeader(http.StatusConflict)
		default:
			w.WriteHeader(http.StatusInternalServerError)
		}
		return
	}

	if created {
		w.WriteHeader(http.StatusAccepted)
		return
	}
	w.WriteHeader(http.StatusOK)
}

func decodeAuthRequest(r *http.Request, dst *authRequest) error {
	if r.Body == nil {
		return errors.New("empty body")
	}
	defer r.Body.Close()

	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(dst); err != nil {
		return err
	}
	if strings.TrimSpace(dst.Login) == "" || strings.TrimSpace(dst.Password) == "" {
		return errors.New("missing fields")
	}
	return nil
}

func setAuthCookie(w http.ResponseWriter, token string) {
	http.SetCookie(w, &http.Cookie{
		Name:     "token",
		Value:    token,
		Path:     "/",
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
	})
}

func (h *Handler) handleRoot(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte("GoFirmart placeholder"))
}

func (h *Handler) handlePing(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte("ok"))
}

func (h *Handler) authFromCookie(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		cookie, err := r.Cookie("token")
		if err != nil {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		userID, err := h.auth.ValidateToken(r.Context(), cookie.Value)
		if err != nil {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		ctx := context.WithValue(r.Context(), userIDContextKey, userID)
		next(w, r.WithContext(ctx))
	}
}

type contextKey string

const userIDContextKey contextKey = "userID"
