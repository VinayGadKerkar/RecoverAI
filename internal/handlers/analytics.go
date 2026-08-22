package handlers

import (
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"recoverai/internal/config"
)

func RegisterAnalyticsRoutes(r chi.Router, db *pgxpool.Pool, cfg *config.Config) {
	h := &analyticsHandler{db: db, cfg: cfg}
	r.Get("/analytics/summary", h.Summary)
	r.Get("/analytics/recovery-rate", h.RecoveryRate)
	r.Get("/analytics/revenue-recovered", h.RevenueRecovered)
	r.Get("/analytics/error-distribution", h.ErrorDistribution)
}

type analyticsHandler struct {
	db  *pgxpool.Pool
	cfg *config.Config
}

// GET /api/v1/analytics/summary
// Returns high-level KPIs: total failed, recovered, recovery %, revenue recovered.
func (h *analyticsHandler) Summary(w http.ResponseWriter, r *http.Request) {
	// TODO: query aggregated metrics from DB
	writeJSON(w, http.StatusOK, map[string]any{
		"total_failed":      0,
		"total_recovered":   0,
		"recovery_rate_pct": 0.0,
		"revenue_recovered": 0,
	})
}

// GET /api/v1/analytics/recovery-rate
func (h *analyticsHandler) RecoveryRate(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"data": []any{}})
}

// GET /api/v1/analytics/revenue-recovered
func (h *analyticsHandler) RevenueRecovered(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"data": []any{}})
}

// GET /api/v1/analytics/error-distribution
// Returns breakdown by UPI error code (U16, U30, Z9, U68, RB, YG).
func (h *analyticsHandler) ErrorDistribution(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"data": []any{}})
}
