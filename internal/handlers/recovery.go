package handlers

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"math"
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"recoverai/internal/config"
	"recoverai/internal/kafka"
	custommiddleware "recoverai/internal/middleware"
)

// ─── Route registration ───────────────────────────────────────────────────────

func RegisterRecoveryRoutes(r chi.Router, db *pgxpool.Pool, _ interface{}, cfg *config.Config) {
	producer, err := kafka.NewProducer(cfg.KafkaBrokers)
	if err != nil {
		slog.Error("recovery handler: failed to create kafka producer", "error", err)
		// handler is still registered; approve endpoint will fail loudly
	}

	h := &recoveryHandler{db: db, cfg: cfg, producer: producer}

	r.Get("/recovery-cases", h.ListCases)
	r.Get("/recovery-cases/{id}", h.GetCase)
	r.Post("/recovery-cases/{id}/approve", h.ApproveCases)
	r.Post("/recovery-cases/{id}/stop", h.StopCase)
}

type recoveryHandler struct {
	db       *pgxpool.Pool
	cfg      *config.Config
	producer *kafka.Producer
}

// ─── Shared response types ────────────────────────────────────────────────────

// CaseSummary is the per-row shape returned by the list endpoint.
type CaseSummary struct {
	ID                      string           `json:"id"`
	PaymentID               string           `json:"payment_id"`
	RazorpayPaymentID       string           `json:"razorpay_payment_id"`
	CustomerID              *string          `json:"customer_id"`
	CustomerEmail           *string          `json:"customer_email"`
	AmountPaise             int64            `json:"amount_paise"`
	AmountFormatted         string           `json:"amount_formatted"`
	Status                  string           `json:"status"`
	Priority                string           `json:"priority"`
	UPIErrorCode            *string          `json:"upi_error_code"`
	UPIErrorCategory        *string          `json:"upi_error_category"`
	FailureType             *string          `json:"failure_type"`
	RecoveryProbability     *float64         `json:"recovery_probability"`
	RecoveryROI             *float64         `json:"recovery_roi"`
	AmountRecoveredPaise    int64            `json:"amount_recovered_paise"`
	AmountRecoveredFormatted string          `json:"amount_recovered_formatted"`
	RetryCount              int              `json:"retry_count"`
	BankOutageDetected      bool             `json:"bank_outage_detected"`
	IsMandatePayment        bool             `json:"is_mandate_payment"`
	ValidatorSkipReason     *string          `json:"validator_skip_reason"`
	AIStrategy              *json.RawMessage `json:"ai_strategy"`
	CreatedAt               time.Time        `json:"created_at"`
	ResolvedAt              *time.Time       `json:"resolved_at"`
	RecoveryTimeMinutes     *float64         `json:"recovery_time_minutes"`
}

// CaseDetail embeds CaseSummary and adds the full expanded fields.
type CaseDetail struct {
	CaseSummary
	PartialRecovery   bool             `json:"partial_recovery"`
	CooldownUntil     *time.Time       `json:"cooldown_until"`
	RBIMinimumRetryAt *time.Time       `json:"rbi_minimum_retry_at"`
	AIDiagnosis       *json.RawMessage `json:"ai_diagnosis"`
}

// AuditEntry is a single row from audit_logs with a computed relative_time.
type AuditEntry struct {
	ID           string           `json:"id"`
	Actor        string           `json:"actor"`
	Action       string           `json:"action"`
	Metadata     *json.RawMessage `json:"metadata"`
	CreatedAt    time.Time        `json:"created_at"`
	RelativeTime string           `json:"relative_time"`
}

// RecoveryAction is a single row from recovery_actions.
type RecoveryAction struct {
	ID                 string           `json:"id"`
	ActionType         string           `json:"action_type"`
	Status             string           `json:"status"`
	AIConfidence       *float64         `json:"ai_confidence"`
	PolicyApproved     bool             `json:"policy_approved"`
	PolicyRuleTriggered *string         `json:"policy_rule_triggered"`
	PolicyReason       *string          `json:"policy_reason"`
	Result             *json.RawMessage `json:"result"`
	ExecutedBy         string           `json:"executed_by"`
	ExecutedAt         *time.Time       `json:"executed_at"`
	CreatedAt          time.Time        `json:"created_at"`
}

// Pagination wraps list response metadata.
type Pagination struct {
	Page       int `json:"page"`
	Limit      int `json:"limit"`
	Total      int `json:"total"`
	TotalPages int `json:"total_pages"`
}

// ─── formatPaise converts an int64 paise value to a human-readable ₹ string. ─

func formatPaise(paise int64) string {
	rupees := float64(paise) / 100
	// Format with Indian grouping: no library needed for simple display
	if rupees == math.Trunc(rupees) {
		return fmt.Sprintf("₹%s", formatINR(int64(rupees)))
	}
	return fmt.Sprintf("₹%.2f", rupees)
}

func formatINR(n int64) string {
	s := strconv.FormatInt(n, 10)
	if len(s) <= 3 {
		return s
	}
	// Indian numbering: last 3 digits, then groups of 2
	result := s[len(s)-3:]
	s = s[:len(s)-3]
	for len(s) > 2 {
		result = s[len(s)-2:] + "," + result
		s = s[:len(s)-2]
	}
	if len(s) > 0 {
		result = s + "," + result
	}
	return result
}

// formatRelative converts a duration since case.created_at into a readable string.
func formatRelative(d time.Duration) string {
	secs := int(d.Seconds())
	if secs < 0 {
		secs = 0
	}
	if d < time.Minute {
		return fmt.Sprintf("%ds", secs)
	}
	if d < time.Hour {
		m := secs / 60
		s := secs % 60
		if s == 0 {
			return fmt.Sprintf("%dm", m)
		}
		return fmt.Sprintf("%dm %ds", m, s)
	}
	h := secs / 3600
	m := (secs % 3600) / 60
	if m == 0 {
		return fmt.Sprintf("%dh", h)
	}
	return fmt.Sprintf("%dh %dm", h, m)
}

// ─── ENDPOINT 1: GET /api/v1/recovery/cases ──────────────────────────────────

func (h *recoveryHandler) ListCases(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	merchantID := custommiddleware.MerchantIDFromContext(ctx)
	if merchantID == "" {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "merchant not authenticated"})
		return
	}

	// ── Parse query params ────────────────────────────────────────────────────
	q := r.URL.Query()

	statusFilter := nullableString(q.Get("status"))
	priorityFilter := nullableString(q.Get("priority"))
	upiCodeFilter := nullableString(q.Get("upi_error_code"))

	outageOnly := false
	if q.Get("outage_only") == "true" {
		outageOnly = true
	}

	page := 1
	if p, err := strconv.Atoi(q.Get("page")); err == nil && p > 0 {
		page = p
	}

	limit := 20
	if l, err := strconv.Atoi(q.Get("limit")); err == nil && l > 0 {
		if l > 100 {
			l = 100
		}
		limit = l
	}

	offset := (page - 1) * limit

	// ── Count query (same filters, no pagination) ─────────────────────────────
	var total int
	err := h.db.QueryRow(ctx, `
		SELECT COUNT(*)
		FROM recovery_cases rc
		JOIN payments p ON p.id = rc.payment_id
		WHERE rc.merchant_id = $1
		  AND ($2::text IS NULL OR rc.status = $2)
		  AND ($3::text IS NULL OR rc.priority = $3)
		  AND ($4::text IS NULL OR rc.upi_error_code = $4)
		  AND ($5::bool IS FALSE OR rc.bank_outage_detected = TRUE)
	`, merchantID, statusFilter, priorityFilter, upiCodeFilter, outageOnly).Scan(&total)
	if err != nil {
		slog.Error("recovery/list: count query failed", "error", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "database error"})
		return
	}

	// ── Main list query ───────────────────────────────────────────────────────
	rows, err := h.db.Query(ctx, `
		SELECT
			rc.id,
			rc.payment_id,
			p.razorpay_payment_id,
			rc.customer_id,
			c.email            AS customer_email,
			rc.revenue_at_risk,
			rc.status,
			rc.priority,
			rc.upi_error_code,
			rc.upi_error_category,
			rc.failure_type,
			rc.recovery_probability,
			rc.recovery_roi,
			rc.amount_recovered,
			rc.retry_count,
			rc.bank_outage_detected,
			rc.is_mandate_payment,
			rc.validator_skip_reason,
			rc.ai_strategy,
			rc.created_at,
			rc.resolved_at
		FROM recovery_cases rc
		JOIN payments p ON p.id = rc.payment_id
		LEFT JOIN customers c ON c.id = rc.customer_id
		WHERE rc.merchant_id = $1
		  AND ($2::text IS NULL OR rc.status = $2)
		  AND ($3::text IS NULL OR rc.priority = $3)
		  AND ($4::text IS NULL OR rc.upi_error_code = $4)
		  AND ($5::bool IS FALSE OR rc.bank_outage_detected = TRUE)
		ORDER BY rc.created_at DESC
		LIMIT $6 OFFSET $7
	`, merchantID, statusFilter, priorityFilter, upiCodeFilter, outageOnly, limit, offset)
	if err != nil {
		slog.Error("recovery/list: query failed", "error", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "database error"})
		return
	}
	defer rows.Close()

	cases := make([]CaseSummary, 0, limit)
	for rows.Next() {
		var cs CaseSummary
		var aiStrategyRaw []byte

		err := rows.Scan(
			&cs.ID,
			&cs.PaymentID,
			&cs.RazorpayPaymentID,
			&cs.CustomerID,
			&cs.CustomerEmail,
			&cs.AmountPaise,
			&cs.Status,
			&cs.Priority,
			&cs.UPIErrorCode,
			&cs.UPIErrorCategory,
			&cs.FailureType,
			&cs.RecoveryProbability,
			&cs.RecoveryROI,
			&cs.AmountRecoveredPaise,
			&cs.RetryCount,
			&cs.BankOutageDetected,
			&cs.IsMandatePayment,
			&cs.ValidatorSkipReason,
			&aiStrategyRaw,
			&cs.CreatedAt,
			&cs.ResolvedAt,
		)
		if err != nil {
			slog.Error("recovery/list: scan failed", "error", err)
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "scan error"})
			return
		}

		cs.AmountFormatted = formatPaise(cs.AmountPaise)
		cs.AmountRecoveredFormatted = formatPaise(cs.AmountRecoveredPaise)

		if len(aiStrategyRaw) > 0 {
			raw := json.RawMessage(aiStrategyRaw)
			cs.AIStrategy = &raw
		}

		// Recovery time in minutes
		if cs.ResolvedAt != nil {
			mins := cs.ResolvedAt.Sub(cs.CreatedAt).Minutes()
			cs.RecoveryTimeMinutes = &mins
		}

		cases = append(cases, cs)
	}

	if err := rows.Err(); err != nil {
		slog.Error("recovery/list: rows error", "error", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "database error"})
		return
	}

	totalPages := 1
	if total > 0 {
		totalPages = int(math.Ceil(float64(total) / float64(limit)))
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"cases": cases,
		"pagination": Pagination{
			Page:       page,
			Limit:      limit,
			Total:      total,
			TotalPages: totalPages,
		},
	})
}

// ─── ENDPOINT 2: GET /api/v1/recovery/cases/:id ──────────────────────────────

func (h *recoveryHandler) GetCase(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	merchantID := custommiddleware.MerchantIDFromContext(ctx)
	if merchantID == "" {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "merchant not authenticated"})
		return
	}

	caseID := chi.URLParam(r, "id")

	// ── Fetch the case itself ─────────────────────────────────────────────────
	var cd CaseDetail
	var aiStrategyRaw, aiDiagnosisRaw []byte

	err := h.db.QueryRow(ctx, `
		SELECT
			rc.id,
			rc.payment_id,
			p.razorpay_payment_id,
			rc.customer_id,
			c.email            AS customer_email,
			rc.revenue_at_risk,
			rc.status,
			rc.priority,
			rc.upi_error_code,
			rc.upi_error_category,
			rc.failure_type,
			rc.recovery_probability,
			rc.recovery_roi,
			rc.amount_recovered,
			rc.retry_count,
			rc.bank_outage_detected,
			rc.is_mandate_payment,
			rc.validator_skip_reason,
			rc.ai_strategy,
			rc.ai_diagnosis,
			rc.partial_recovery,
			rc.cooldown_until,
			rc.rbi_minimum_retry_at,
			rc.created_at,
			rc.resolved_at
		FROM recovery_cases rc
		JOIN payments p ON p.id = rc.payment_id
		LEFT JOIN customers c ON c.id = rc.customer_id
		WHERE rc.id = $1
		  AND rc.merchant_id = $2
	`, caseID, merchantID).Scan(
		&cd.ID,
		&cd.PaymentID,
		&cd.RazorpayPaymentID,
		&cd.CustomerID,
		&cd.CustomerEmail,
		&cd.AmountPaise,
		&cd.Status,
		&cd.Priority,
		&cd.UPIErrorCode,
		&cd.UPIErrorCategory,
		&cd.FailureType,
		&cd.RecoveryProbability,
		&cd.RecoveryROI,
		&cd.AmountRecoveredPaise,
		&cd.RetryCount,
		&cd.BankOutageDetected,
		&cd.IsMandatePayment,
		&cd.ValidatorSkipReason,
		&aiStrategyRaw,
		&aiDiagnosisRaw,
		&cd.PartialRecovery,
		&cd.CooldownUntil,
		&cd.RBIMinimumRetryAt,
		&cd.CreatedAt,
		&cd.ResolvedAt,
	)
	if err != nil {
		slog.Error("recovery/get: query failed", "case_id", caseID, "error", err)
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "case not found"})
		return
	}

	cd.AmountFormatted = formatPaise(cd.AmountPaise)
	cd.AmountRecoveredFormatted = formatPaise(cd.AmountRecoveredPaise)

	if len(aiStrategyRaw) > 0 {
		raw := json.RawMessage(aiStrategyRaw)
		cd.AIStrategy = &raw
	}
	if len(aiDiagnosisRaw) > 0 {
		raw := json.RawMessage(aiDiagnosisRaw)
		cd.AIDiagnosis = &raw
	}

	if cd.ResolvedAt != nil {
		mins := cd.ResolvedAt.Sub(cd.CreatedAt).Minutes()
		cd.RecoveryTimeMinutes = &mins
	}

	// ── Fetch audit trail ─────────────────────────────────────────────────────
	auditRows, err := h.db.Query(ctx, `
		SELECT id, actor, action, metadata, created_at
		FROM audit_logs
		WHERE entity_type = 'recovery_case'
		  AND entity_id = $1
		ORDER BY created_at ASC
	`, caseID)
	if err != nil {
		slog.Error("recovery/get: audit query failed", "case_id", caseID, "error", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "database error"})
		return
	}
	defer auditRows.Close()

	auditTrail := make([]AuditEntry, 0)
	for auditRows.Next() {
		var ae AuditEntry
		var metaRaw []byte

		if err := auditRows.Scan(&ae.ID, &ae.Actor, &ae.Action, &metaRaw, &ae.CreatedAt); err != nil {
			slog.Error("recovery/get: audit scan failed", "error", err)
			continue
		}

		if len(metaRaw) > 0 {
			raw := json.RawMessage(metaRaw)
			ae.Metadata = &raw
		}

		ae.RelativeTime = formatRelative(ae.CreatedAt.Sub(cd.CreatedAt))
		auditTrail = append(auditTrail, ae)
	}

	// ── Fetch recovery actions ────────────────────────────────────────────────
	actionRows, err := h.db.Query(ctx, `
		SELECT
			id,
			action_type,
			status,
			ai_confidence,
			policy_approved,
			policy_rule_triggered,
			policy_reason,
			result,
			executed_by,
			executed_at,
			created_at
		FROM recovery_actions
		WHERE case_id = $1
		ORDER BY created_at ASC
	`, caseID)
	if err != nil {
		slog.Error("recovery/get: actions query failed", "case_id", caseID, "error", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "database error"})
		return
	}
	defer actionRows.Close()

	actions := make([]RecoveryAction, 0)
	for actionRows.Next() {
		var ra RecoveryAction
		var resultRaw []byte

		if err := actionRows.Scan(
			&ra.ID,
			&ra.ActionType,
			&ra.Status,
			&ra.AIConfidence,
			&ra.PolicyApproved,
			&ra.PolicyRuleTriggered,
			&ra.PolicyReason,
			&resultRaw,
			&ra.ExecutedBy,
			&ra.ExecutedAt,
			&ra.CreatedAt,
		); err != nil {
			slog.Error("recovery/get: action scan failed", "error", err)
			continue
		}

		if len(resultRaw) > 0 {
			raw := json.RawMessage(resultRaw)
			ra.Result = &raw
		}

		actions = append(actions, ra)
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"case":             cd,
		"audit_trail":      auditTrail,
		"recovery_actions": actions,
	})
}

// ─── ENDPOINT 3: POST /api/v1/recovery/cases/:id/approve ─────────────────────

type ApproveRequest struct {
	ApprovedBy string `json:"approved_by"`
	Notes      string `json:"notes"`
}

func (h *recoveryHandler) ApproveCases(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	merchantID := custommiddleware.MerchantIDFromContext(ctx)
	if merchantID == "" {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "merchant not authenticated"})
		return
	}

	caseID := chi.URLParam(r, "id")

	var req ApproveRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request body"})
		return
	}
	if req.ApprovedBy == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "approved_by is required"})
		return
	}

	// ── Verify case exists, belongs to this merchant, and is in pending_human_approval ──
	var currentStatus, paymentID string
	err := h.db.QueryRow(ctx,
		`SELECT status, payment_id::text FROM recovery_cases WHERE id = $1 AND merchant_id = $2`,
		caseID, merchantID,
	).Scan(&currentStatus, &paymentID)
	if err != nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "case not found"})
		return
	}

	if currentStatus != "pending_human_approval" {
		writeJSON(w, http.StatusConflict, map[string]string{
			"error": fmt.Sprintf("case is in status '%s', expected 'pending_human_approval'", currentStatus),
		})
		return
	}

	// ── Transition to in_progress ─────────────────────────────────────────────
	_, err = h.db.Exec(ctx, `
		UPDATE recovery_cases
		SET status = 'in_progress', updated_at = NOW()
		WHERE id = $1
	`, caseID)
	if err != nil {
		slog.Error("recovery/approve: update failed", "case_id", caseID, "error", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "database error"})
		return
	}

	// ── Write audit log ───────────────────────────────────────────────────────
	meta, _ := json.Marshal(map[string]string{
		"approved_by": req.ApprovedBy,
		"notes":       req.Notes,
	})
	_, err = h.db.Exec(ctx, `
		INSERT INTO audit_logs (entity_type, entity_id, actor, action, metadata)
		VALUES ('recovery_case', $1, 'human', 'human_approved', $2)
	`, caseID, meta)
	if err != nil {
		slog.Error("recovery/approve: audit log failed", "case_id", caseID, "error", err)
		// Non-fatal — don't fail the request
	}

	// ── Re-publish to payment.ai_commands with human_approved flag ─────────────
	if h.producer != nil {
		commandPayload, _ := json.Marshal(map[string]any{
			"case_id":        caseID,
			"payment_id":     paymentID,
			"merchant_id":    merchantID,
			"human_approved": true,
			"approved_by":    req.ApprovedBy,
			"queued_at":      time.Now().UTC().Format(time.RFC3339),
		})

		publishCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
		defer cancel()

		if err := h.producer.Publish(publishCtx, kafkapkg.TopicAICommands, caseID, commandPayload); err != nil {
			slog.Error("recovery/approve: kafka publish failed", "case_id", caseID, "error", err)
			// Status already updated — still return 200, log the failure
		}
	}

	slog.Info("recovery/approve: case approved",
		"case_id", caseID,
		"merchant_id", merchantID,
		"approved_by", req.ApprovedBy,
	)

	writeJSON(w, http.StatusOK, map[string]string{
		"case_id": caseID,
		"status":  "in_progress",
		"message": "Recovery approved and re-queued",
	})
}

// ─── ENDPOINT 4: POST /api/v1/recovery/cases/:id/stop ────────────────────────

type StopRequest struct {
	Reason string `json:"reason"`
}

func (h *recoveryHandler) StopCase(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	merchantID := custommiddleware.MerchantIDFromContext(ctx)
	if merchantID == "" {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "merchant not authenticated"})
		return
	}

	caseID := chi.URLParam(r, "id")

	var req StopRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request body"})
		return
	}

	// ── Verify case exists, belongs to merchant, and is in a stoppable status ──
	var currentStatus string
	err := h.db.QueryRow(ctx,
		`SELECT status FROM recovery_cases WHERE id = $1 AND merchant_id = $2`,
		caseID, merchantID,
	).Scan(&currentStatus)
	if err != nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "case not found"})
		return
	}

	stoppable := map[string]bool{
		"open":                   true,
		"in_progress":            true,
		"pending_human_approval": true,
	}
	if !stoppable[currentStatus] {
		writeJSON(w, http.StatusConflict, map[string]string{
			"error": fmt.Sprintf("case cannot be stopped from status '%s'", currentStatus),
		})
		return
	}

	// ── Transition to stopped ─────────────────────────────────────────────────
	_, err = h.db.Exec(ctx, `
		UPDATE recovery_cases
		SET status = 'stopped',
		    resolved_at = NOW(),
		    updated_at  = NOW()
		WHERE id = $1
	`, caseID)
	if err != nil {
		slog.Error("recovery/stop: update failed", "case_id", caseID, "error", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "database error"})
		return
	}

	// ── Cancel all pending recovery actions ───────────────────────────────────
	_, err = h.db.Exec(ctx, `
		UPDATE recovery_actions
		SET status = 'skipped'
		WHERE case_id = $1 AND status = 'pending'
	`, caseID)
	if err != nil {
		slog.Error("recovery/stop: cancel actions failed", "case_id", caseID, "error", err)
		// Non-fatal
	}

	// ── Write audit log ───────────────────────────────────────────────────────
	reason := req.Reason
	if reason == "" {
		reason = "merchant stopped recovery"
	}
	meta, _ := json.Marshal(map[string]string{
		"reason":           reason,
		"previous_status":  currentStatus,
	})
	_, err = h.db.Exec(ctx, `
		INSERT INTO audit_logs (entity_type, entity_id, actor, action, metadata)
		VALUES ('recovery_case', $1, 'human', 'merchant_stopped', $2)
	`, caseID, meta)
	if err != nil {
		slog.Error("recovery/stop: audit log failed", "case_id", caseID, "error", err)
	}

	slog.Info("recovery/stop: case stopped",
		"case_id", caseID,
		"merchant_id", merchantID,
		"reason", reason,
	)

	writeJSON(w, http.StatusOK, map[string]string{
		"case_id": caseID,
		"status":  "stopped",
		"message": "Recovery stopped",
	})
}

// ─── Helpers ─────────────────────────────────────────────────────────────────

// nullableString returns nil when s is empty, otherwise &s.
// Used to pass optional SQL parameters that must be NULL when unset.
func nullableString(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}
