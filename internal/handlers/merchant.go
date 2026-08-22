package handlers

import (
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"recoverai/internal/config"
)

func RegisterMerchantRoutes(r chi.Router, db *pgxpool.Pool, cfg *config.Config) {
	h := &merchantHandler{db: db, cfg: cfg}
	r.Get("/merchants/me", h.GetMe)
	r.Put("/merchants/me", h.UpdateMe)
	r.Get("/merchants/me/payments", h.ListPayments)
}

type merchantHandler struct {
	db  *pgxpool.Pool
	cfg *config.Config
}

func (h *merchantHandler) GetMe(w http.ResponseWriter, r *http.Request) {
	// TODO: extract merchant from JWT claims
	writeJSON(w, http.StatusOK, map[string]any{"merchant": nil})
}

func (h *merchantHandler) UpdateMe(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"message": "updated"})
}

func (h *merchantHandler) ListPayments(w http.ResponseWriter, r *http.Request) {
	// TODO: paginate with ?page=&limit=&status=
	writeJSON(w, http.StatusOK, map[string]any{"payments": []any{}, "total": 0})
}

// Ensure chi.Router is used (avoids unused import warning until routes expand)
var _ = chi.URLParam
