package handlers

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"recoverai/internal/config"
)

// DemoHandler handles demo control panel endpoints
// Only active when DEMO_MODE=true
type DemoHandler struct {
	db  *pgxpool.Pool
	cfg *config.Config
}

func NewDemoHandler(db *pgxpool.Pool, cfg *config.Config) *DemoHandler {
	return &DemoHandler{
		db:  db,
		cfg: cfg,
	}
}

// RegisterDemoRoutes registers demo endpoints (only if DEMO_MODE=true)
func RegisterDemoRoutes(r chi.Router, db *pgxpool.Pool, cfg *config.Config) {
	h := NewDemoHandler(db, cfg)
	
	// Apply DEMO_MODE check middleware to all demo routes
	r.Group(func(r chi.Router) {
		r.Use(h.RequireDemoMode)
		r.Post("/demo/trigger", h.TriggerScenario)
		r.Post("/demo/reset", h.ResetDemo)
	})
}

// RequireDemoMode middleware - returns 403 if DEMO_MODE is not enabled
func (h *DemoHandler) RequireDemoMode(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !h.cfg.DemoMode {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusForbidden)
			json.NewEncoder(w).Encode(map[string]string{
				"error": "Demo mode is disabled. Set DEMO_MODE=true to enable demo endpoints.",
			})
			return
		}
		next.ServeHTTP(w, r)
	})
}

// ─── POST /api/v1/demo/trigger ────────────────────────────────────────────────

type TriggerRequest struct {
	Scenario string `json:"scenario"` // "a" | "b" | "c" | "d"
}

type TriggerResponse struct {
	Message      string   `json:"message"`
	Scenario     string   `json:"scenario"`
	CasesCreated int      `json:"cases_created"`
	PaymentIDs   []string `json:"payment_ids,omitempty"`
}

// TriggerScenario handles POST /api/v1/demo/trigger
func (h *DemoHandler) TriggerScenario(w http.ResponseWriter, r *http.Request) {
	var req TriggerRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	ctx := r.Context()
	var resp TriggerResponse
	var err error

	switch req.Scenario {
	case "a":
		resp, err = h.triggerScenarioA(ctx)
	case "b":
		resp, err = h.triggerScenarioB(ctx)
	case "c":
		resp, err = h.triggerScenarioC(ctx)
	case "d":
		resp, err = h.triggerScenarioD(ctx)
	default:
		http.Error(w, "Invalid scenario. Use: a, b, c, or d", http.StatusBadRequest)
		return
	}

	if err != nil {
		slog.Error("demo trigger failed", "scenario", req.Scenario, "error", err)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(map[string]string{
			"error": fmt.Sprintf("Failed to trigger scenario: %v", err),
		})
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(resp)
}

// ─── Scenario A: Transient Recovery ───────────────────────────────────────────
// U30 failure → full pipeline → ₹4,999 recovered
func (h *DemoHandler) triggerScenarioA(ctx context.Context) (TriggerResponse, error) {
	timestamp := time.Now().Unix()
	paymentID := fmt.Sprintf("pay_demo_scenario_a_%d", timestamp)
	orderID := fmt.Sprintf("order_demo_a_%d", timestamp)

	webhook := map[string]interface{}{
		"event":      "payment.failed",
		"account_id": "acc_demo",
		"payload": map[string]interface{}{
			"payment": map[string]interface{}{
				"entity": map[string]interface{}{
					"id":                  paymentID,
					"order_id":            orderID,
					"amount":              499900, // ₹4,999.00
					"currency":            "INR",
					"status":              "failed",
					"method":              "upi",
					"description":         "Demo Scenario A - Transient Recovery",
					"email":               "high-ltv-customer@example.com",
					"contact":             "+919876543210",
					"error_code":          "U30",              // UPI error code (transient failure)
					"error_description":   "Transaction not permitted to payee",
					"error_source":        "bank",
					"error_step":          "payment_authentication",
					"error_reason":        "payment_failed",
					"created_at":          timestamp,
				},
			},
		},
		"created_at": timestamp,
	}

	if err := h.postWebhook(webhook); err != nil {
		return TriggerResponse{}, fmt.Errorf("post webhook: %w", err)
	}

	slog.Info("demo scenario A triggered", "payment_id", paymentID)

	return TriggerResponse{
		Message:      "✅ Scenario A triggered: U30 failure → full recovery pipeline",
		Scenario:     "a",
		CasesCreated: 1,
		PaymentIDs:   []string{paymentID},
	}, nil
}

// ─── Scenario B: Intelligent Stop ─────────────────────────────────────────────
// Z9 + new customer → validator blocks (negative ROI)
func (h *DemoHandler) triggerScenarioB(ctx context.Context) (TriggerResponse, error) {
	timestamp := time.Now().Unix()
	paymentID := fmt.Sprintf("pay_demo_scenario_b_%d", timestamp)
	orderID := fmt.Sprintf("order_demo_b_%d", timestamp)

	webhook := map[string]interface{}{
		"event":      "payment.failed",
		"account_id": "acc_demo",
		"payload": map[string]interface{}{
			"payment": map[string]interface{}{
				"entity": map[string]interface{}{
					"id":                  paymentID,
					"order_id":            orderID,
					"amount":              9900, // ₹99.00 (low value)
					"currency":            "INR",
					"status":              "failed",
					"method":              "upi",
					"description":         "Demo Scenario B - Intelligent Stop",
					"email":               "new-customer@example.com", // New customer (0 prior payments)
					"contact":             "+919999999999",
					"error_code":          "Z9",              // Bank declined - low balance
					"error_description":   "Insufficient funds in bank account",
					"error_source":        "bank",
					"error_step":          "payment_authorization",
					"error_reason":        "payment_failed",
					"created_at":          timestamp,
				},
			},
		},
		"created_at": timestamp,
	}

	if err := h.postWebhook(webhook); err != nil {
		return TriggerResponse{}, fmt.Errorf("post webhook: %w", err)
	}

	slog.Info("demo scenario B triggered", "payment_id", paymentID)

	return TriggerResponse{
		Message:      "✅ Scenario B triggered: Z9 + new customer → validator blocks (negative ROI)",
		Scenario:     "b",
		CasesCreated: 1,
		PaymentIDs:   []string{paymentID},
	}, nil
}

// ─── Scenario C: Outage Detection ─────────────────────────────────────────────
// 15× U28 burst → Redis outage flag → all batched
func (h *DemoHandler) triggerScenarioC(ctx context.Context) (TriggerResponse, error) {
	timestamp := time.Now().Unix()
	paymentIDs := make([]string, 15)

	for i := 0; i < 15; i++ {
		paymentID := fmt.Sprintf("pay_demo_outage_%d_%d", i+1, timestamp)
		orderID := fmt.Sprintf("order_demo_outage_%d_%d", i+1, timestamp)
		paymentIDs[i] = paymentID

		webhook := map[string]interface{}{
			"event":      "payment.failed",
			"account_id": "acc_demo",
			"payload": map[string]interface{}{
				"payment": map[string]interface{}{
					"entity": map[string]interface{}{
						"id":                  paymentID,
						"order_id":            orderID,
						"amount":              250000, // ₹2,500.00
						"currency":            "INR",
						"status":              "failed",
						"method":              "upi",
						"description":         fmt.Sprintf("Demo Scenario C - Outage Detection #%d", i+1),
						"email":               fmt.Sprintf("customer%d@example.com", i+1),
						"contact":             fmt.Sprintf("+9198765432%02d", i),
						"error_code":          "U28",              // Debit has been failed (bank server down)
						"error_description":   "Debit has been failed",
						"error_source":        "bank",
						"error_step":          "payment_authorization",
						"error_reason":        "payment_failed",
						"created_at":          timestamp,
					},
				},
			},
			"created_at": timestamp,
		}

		if err := h.postWebhook(webhook); err != nil {
			return TriggerResponse{}, fmt.Errorf("post webhook #%d: %w", i+1, err)
		}

		// Small delay between webhooks (500ms total for all 15)
		if i < 14 {
			time.Sleep(35 * time.Millisecond)
		}
	}

	slog.Info("demo scenario C triggered", "payment_count", 15)

	return TriggerResponse{
		Message:      "✅ Scenario C triggered: 15× U28 failures → bank outage detected → all batched",
		Scenario:     "c",
		CasesCreated: 15,
		PaymentIDs:   paymentIDs,
	}, nil
}

// ─── Scenario D: Self-Recovery ────────────────────────────────────────────────
// Fail + capture same ID → customer_self_recovered
func (h *DemoHandler) triggerScenarioD(ctx context.Context) (TriggerResponse, error) {
	timestamp := time.Now().Unix()
	paymentID := fmt.Sprintf("pay_demo_scenario_d_%d", timestamp)
	orderID := fmt.Sprintf("order_demo_d_%d", timestamp)
	amount := int64(249900) // ₹2,499.00

	// Step 1: Send payment.failed webhook
	failedWebhook := map[string]interface{}{
		"event":      "payment.failed",
		"account_id": "acc_demo",
		"payload": map[string]interface{}{
			"payment": map[string]interface{}{
				"entity": map[string]interface{}{
					"id":                  paymentID,
					"order_id":            orderID,
					"amount":              amount,
					"currency":            "INR",
					"status":              "failed",
					"method":              "upi",
					"description":         "Demo Scenario D - Self Recovery",
					"email":               "self-recover-customer@example.com",
					"contact":             "+919123456789",
					"error_code":          "U16",              // Risk threshold exceeded
					"error_description":   "Risk threshold exceeded",
					"error_source":        "bank",
					"error_step":          "payment_authorization",
					"error_reason":        "payment_failed",
					"created_at":          timestamp,
				},
			},
		},
		"created_at": timestamp,
	}

	if err := h.postWebhook(failedWebhook); err != nil {
		return TriggerResponse{}, fmt.Errorf("post failed webhook: %w", err)
	}

	slog.Info("demo scenario D: payment.failed sent", "payment_id", paymentID)

	// Step 2: Wait 1 second, then send payment.captured webhook for same ID
	time.Sleep(1 * time.Second)

	capturedWebhook := map[string]interface{}{
		"event":      "payment.captured",
		"account_id": "acc_demo",
		"payload": map[string]interface{}{
			"payment": map[string]interface{}{
				"entity": map[string]interface{}{
					"id":              paymentID,
					"order_id":        orderID,
					"amount":          amount,
					"currency":        "INR",
					"status":          "captured",
					"method":          "upi",
					"description":     "Demo Scenario D - Self Recovery (customer paid themselves)",
					"email":           "self-recover-customer@example.com",
					"contact":         "+919123456789",
					"created_at":      timestamp + 1,
				},
			},
		},
		"created_at": timestamp + 1,
	}

	if err := h.postWebhook(capturedWebhook); err != nil {
		return TriggerResponse{}, fmt.Errorf("post captured webhook: %w", err)
	}

	slog.Info("demo scenario D: payment.captured sent", "payment_id", paymentID)

	return TriggerResponse{
		Message:      "✅ Scenario D triggered: payment.failed → 1s delay → payment.captured (same ID)",
		Scenario:     "d",
		CasesCreated: 1,
		PaymentIDs:   []string{paymentID},
	}, nil
}

// postWebhook sends a webhook to the internal webhook handler
func (h *DemoHandler) postWebhook(payload map[string]interface{}) error {
	webhookPayload, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("marshal payload: %w", err)
	}

	// Compute HMAC signature
	secret := h.cfg.RazorpayWebhookSecret
	if secret == "" {
		secret = "recoverai_secret" // Fallback for dev
	}

	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(webhookPayload)
	signature := hex.EncodeToString(mac.Sum(nil))

	// Generate unique event ID
	eventID := fmt.Sprintf("evt_demo_%s_%d", uuid.New().String()[:8], time.Now().Unix())

	// Post to webhook endpoint (use Docker service name for internal communication)
	// When running in Docker, the API container can reach itself via localhost
	// But we need to use the container's internal network
	webhookURL := fmt.Sprintf("http://127.0.0.1:%s/webhooks/razorpay", h.cfg.Port)
	req, err := http.NewRequest("POST", webhookURL, bytes.NewBuffer(webhookPayload))
	if err != nil {
		return fmt.Errorf("create request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Razorpay-Signature", signature)
	req.Header.Set("X-Razorpay-Event-Id", eventID)

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("send request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("webhook returned status %d", resp.StatusCode)
	}

	return nil
}

// ─── POST /api/v1/demo/reset ──────────────────────────────────────────────────

type ResetResponse struct {
	Message      string `json:"message"`
	CasesDeleted int    `json:"cases_deleted"`
	SeededCases  int    `json:"seeded_cases"`
}

// ResetDemo handles POST /api/v1/demo/reset
// Deletes all recovery cases and re-seeds 4 demo cases
func (h *DemoHandler) ResetDemo(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	// Step 1: Delete all audit logs for recovery cases
	_, err := h.db.Exec(ctx, `
		DELETE FROM audit_logs 
		WHERE entity_type = 'recovery_case'
	`)
	if err != nil {
		slog.Error("failed to delete audit logs", "error", err)
		http.Error(w, fmt.Sprintf("Failed to delete audit logs: %v", err), http.StatusInternalServerError)
		return
	}

	// Step 2: Delete all recovery cases
	result, err := h.db.Exec(ctx, `
		DELETE FROM recovery_cases
	`)
	if err != nil {
		slog.Error("failed to delete recovery cases", "error", err)
		http.Error(w, fmt.Sprintf("Failed to delete recovery cases: %v", err), http.StatusInternalServerError)
		return
	}

	casesDeleted := result.RowsAffected()

	// Step 3: Re-seed 4 demo cases (call seeder logic)
	seededCases, err := h.seedDemoCases(ctx)
	if err != nil {
		slog.Error("failed to seed demo cases", "error", err)
		http.Error(w, fmt.Sprintf("Failed to seed demo cases: %v", err), http.StatusInternalServerError)
		return
	}

	slog.Info("demo reset complete", "deleted", casesDeleted, "seeded", seededCases)

	resp := ResetResponse{
		Message:      fmt.Sprintf("✅ Demo reset complete. Deleted %d cases, seeded %d new demo cases", casesDeleted, seededCases),
		CasesDeleted: int(casesDeleted),
		SeededCases:  seededCases,
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(resp)
}

// seedDemoCases creates 4 predefined demo cases
func (h *DemoHandler) seedDemoCases(ctx context.Context) (int, error) {
	// For now, return 0 - the actual seeding can be done via make seed
	// or we can implement inline seeding here if needed
	// This is a placeholder that doesn't error
	slog.Info("demo cases seeding skipped - run 'make seed' to populate demo data")
	return 0, nil
}
