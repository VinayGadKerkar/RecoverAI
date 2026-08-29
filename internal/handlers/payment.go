package handlers

import (
	"encoding/json"
	"log/slog"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"recoverai/internal/config"
	"recoverai/internal/services"
)

// RegisterPaymentRoutes registers payment-related routes
func RegisterPaymentRoutes(r chi.Router, db *pgxpool.Pool, cfg *config.Config) {
	h := &paymentHandler{
		db:       db,
		cfg:      cfg,
		razorpay: services.NewRazorpayService(cfg),
	}

	r.Post("/api/v1/create-order", h.CreateOrder)
}

type paymentHandler struct {
	db       *pgxpool.Pool
	cfg      *config.Config
	razorpay *services.RazorpayService
}

// CreateOrderRequest is the request body for creating a Razorpay order
type CreateOrderRequest struct {
	Amount   int64             `json:"amount"`   // in paise
	Currency string            `json:"currency"` // INR
	Notes    map[string]string `json:"notes"`
}

// CreateOrder creates a Razorpay order for test payments
func (h *paymentHandler) CreateOrder(w http.ResponseWriter, r *http.Request) {
	var req CreateOrderRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request body"})
		return
	}

	// Validate amount
	if req.Amount < 100 {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "amount must be at least ₹1 (100 paise)"})
		return
	}

	// Default currency to INR
	if req.Currency == "" {
		req.Currency = "INR"
	}

	// Create Razorpay order
	order, err := h.razorpay.CreateOrder(r.Context(), req.Amount, req.Currency, req.Notes)
	if err != nil {
		slog.Error("create order failed", "error", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to create order"})
		return
	}

	orderID, _ := order["id"].(string)
	slog.Info("order created", "order_id", orderID, "amount", req.Amount)
	writeJSON(w, http.StatusOK, order)
}
