package handlers

import (
	"encoding/json"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/golang-jwt/jwt/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"recoverai/internal/config"
	"recoverai/internal/middleware"
)

// RegisterAuthRoutes mounts the public (no-JWT) auth routes.
func RegisterAuthRoutes(r chi.Router, db *pgxpool.Pool, cfg *config.Config) {
	h := &authHandler{db: db, cfg: cfg}
	r.Post("/auth/login", h.Login)
}

type authHandler struct {
	db  *pgxpool.Pool
	cfg *config.Config
}

type loginRequest struct {
	MerchantID string `json:"merchant_id"`
	Password   string `json:"password"`
}

type loginResponse struct {
	Token      string `json:"token"`
	MerchantID string `json:"merchant_id"`
	ExpiresIn  int    `json:"expires_in"` // seconds
}

// Login validates the demo password and returns a signed JWT containing the
// merchant's real UUID from the merchants table.
//
// POST /auth/login
// Body: {"merchant_id":"demo","password":"<DEMO_PASSWORD>"}
// 200:  {"token":"<jwt>","merchant_id":"<uuid>","expires_in":86400}
// 400:  missing fields
// 401:  wrong password
// 404:  merchant name not found in merchants table
func (h *authHandler) Login(w http.ResponseWriter, r *http.Request) {
	var req loginRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, `{"error":"invalid request body"}`, http.StatusBadRequest)
		return
	}

	if req.MerchantID == "" || req.Password == "" {
		http.Error(w, `{"error":"merchant_id and password are required"}`, http.StatusBadRequest)
		return
	}

	if req.Password != h.cfg.DemoPassword {
		http.Error(w, `{"error":"invalid credentials"}`, http.StatusUnauthorized)
		return
	}

	// Look up the merchant UUID by name so the JWT carries a real UUID.
	var merchantUUID string
	err := h.db.QueryRow(r.Context(),
		`SELECT id FROM merchants WHERE name = $1 AND deleted_at IS NULL LIMIT 1`,
		req.MerchantID,
	).Scan(&merchantUUID)
	if err != nil {
		http.Error(w, `{"error":"merchant not found — create a merchant row first"}`, http.StatusNotFound)
		return
	}

	now := time.Now()
	claims := &middleware.JWTClaims{
		MerchantID: merchantUUID, // real UUID — required by recovery_cases FK
		Role:       "merchant",
		RegisteredClaims: jwt.RegisteredClaims{
			Subject:   merchantUUID,
			IssuedAt:  jwt.NewNumericDate(now),
			ExpiresAt: jwt.NewNumericDate(now.Add(24 * time.Hour)),
		},
	}

	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	signed, err := token.SignedString([]byte(h.cfg.JWTSecret))
	if err != nil {
		http.Error(w, `{"error":"failed to generate token"}`, http.StatusInternalServerError)
		return
	}

	writeJSON(w, http.StatusOK, loginResponse{
		Token:      signed,
		MerchantID: merchantUUID,
		ExpiresIn:  86400,
	})
}
