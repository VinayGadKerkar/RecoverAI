package handlers

import (
	"encoding/json"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"recoverai/internal/config"
	"recoverai/internal/redis"
)

func RegisterRecoveryRoutes(r chi.Router, db *pgxpool.Pool, redisClient *redis.Client, cfg *config.Config) {
	h := &recoveryHandler{db: db, redis: redisClient, cfg: cfg}
	r.Get("/recoveries", h.ListRecoveries)
	r.Get("/recoveries/{paymentID}", h.GetRecovery)
	r.Post("/recoveries/{paymentID}/retry", h.ManualRetry)
	r.Post("/recoveries/{paymentID}/approve", h.ApproveRecovery)
	r.Post("/recoveries/{paymentID}/abort", h.AbortRecovery)
}

type recoveryHandler struct {
	db    *pgxpool.Pool
	redis *redis.Client
	cfg   *config.Config
}

// GET /api/v1/recoveries
func (h *recoveryHandler) ListRecoveries(w http.ResponseWriter, r *http.Request) {
	// TODO: paginate, filter by status
	writeJSON(w, http.StatusOK, map[string]any{"recoveries": []any{}})
}

// GET /api/v1/recoveries/{paymentID}
func (h *recoveryHandler) GetRecovery(w http.ResponseWriter, r *http.Request) {
	paymentID := chi.URLParam(r, "paymentID")
	// TODO: fetch from db
	writeJSON(w, http.StatusOK, map[string]any{"payment_id": paymentID, "attempts": []any{}})
}

// POST /api/v1/recoveries/{paymentID}/retry
func (h *recoveryHandler) ManualRetry(w http.ResponseWriter, r *http.Request) {
	paymentID := chi.URLParam(r, "paymentID")
	// TODO: trigger manual recovery pipeline
	writeJSON(w, http.StatusAccepted, map[string]any{"message": "retry queued", "payment_id": paymentID})
}

// POST /api/v1/recoveries/{paymentID}/approve
func (h *recoveryHandler) ApproveRecovery(w http.ResponseWriter, r *http.Request) {
	paymentID := chi.URLParam(r, "paymentID")
	writeJSON(w, http.StatusOK, map[string]any{"message": "approved", "payment_id": paymentID})
}

// POST /api/v1/recoveries/{paymentID}/abort
func (h *recoveryHandler) AbortRecovery(w http.ResponseWriter, r *http.Request) {
	paymentID := chi.URLParam(r, "paymentID")
	writeJSON(w, http.StatusOK, map[string]any{"message": "aborted", "payment_id": paymentID})
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}
