package handlers

import (
	"encoding/json"
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"recoverai/internal/config"
)

func RegisterAnalyticsRoutes(r chi.Router, db *pgxpool.Pool, cfg *config.Config) {
	h := &analyticsHandler{db: db, cfg: cfg}
	r.Get("/analytics/overview", h.Overview)
	r.Get("/analytics/recovery-rate", h.RecoveryRate)
	r.Get("/analytics/revenue", h.Revenue)
	r.Get("/analytics/honest-exceptions", h.HonestExceptions)
	r.Get("/analytics/ai-performance", h.AIPerformance)
}

type analyticsHandler struct {
	db  *pgxpool.Pool
	cfg *config.Config
}

// ─── GET /api/v1/analytics/overview ───────────────────────────────────────────

type OverviewResponse struct {
	RevenueAtRiskPaise          int64   `json:"revenue_at_risk_paise"`
	RecoveredRevenuePaise       int64   `json:"recovered_revenue_paise"`
	RecoveryRatePercent         float64 `json:"recovery_rate_percent"`
	PartialRecoveryRatePercent  float64 `json:"partial_recovery_rate_percent"`
	TotalFailedPayments         int     `json:"total_failed_payments"`
	TotalRecoveredPayments      int     `json:"total_recovered_payments"`
	CustomerSelfRecoveredCount  int     `json:"customer_self_recovered_count"`
	OutageBatchedCount          int     `json:"outage_batched_count"`
	NotWorthRecoveringCount     int     `json:"not_worth_recovering_count"`
	AvgRecoveryTimeMinutes      float64 `json:"avg_recovery_time_minutes"`
	ActiveCases                 int     `json:"active_cases"`
	PendingHumanApprovalCount   int     `json:"pending_human_approval_count"`
	AIAccuracyRate              float64 `json:"ai_accuracy_rate"`
}

func (h *analyticsHandler) Overview(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	var resp OverviewResponse

	// Main aggregation query
	err := h.db.QueryRow(ctx, `
		SELECT
			COALESCE(SUM(revenue_at_risk), 0) AS revenue_at_risk,
			COALESCE(SUM(amount_recovered), 0) AS recovered_revenue,
			COUNT(*) FILTER (WHERE status IN ('recovered', 'partially_recovered', 'customer_self_recovered')) AS recovered_count,
			COUNT(*) FILTER (WHERE partial_recovery = TRUE) AS partial_count,
			COUNT(*) AS total_failed,
			COUNT(*) FILTER (WHERE status = 'customer_self_recovered') AS self_recovered_count,
			COUNT(*) FILTER (WHERE status = 'outage_batched') AS outage_batched_count,
			COUNT(*) FILTER (WHERE status = 'not_worth_recovering') AS not_worth_count,
			COALESCE(AVG(EXTRACT(EPOCH FROM (resolved_at - created_at)) / 60) FILTER (WHERE resolved_at IS NOT NULL), 0) AS avg_recovery_minutes,
			COUNT(*) FILTER (WHERE status IN ('open', 'in_progress')) AS active_count,
			COUNT(*) FILTER (WHERE status = 'pending_human_approval') AS pending_human_count
		FROM recovery_cases
		WHERE created_at >= NOW() - INTERVAL '30 days'
	`).Scan(
		&resp.RevenueAtRiskPaise,
		&resp.RecoveredRevenuePaise,
		&resp.TotalRecoveredPayments,
		&resp.PartialRecoveryRatePercent,
		&resp.TotalFailedPayments,
		&resp.CustomerSelfRecoveredCount,
		&resp.OutageBatchedCount,
		&resp.NotWorthRecoveringCount,
		&resp.AvgRecoveryTimeMinutes,
		&resp.ActiveCases,
		&resp.PendingHumanApprovalCount,
	)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	// Calculate rates
	if resp.TotalFailedPayments > 0 {
		resp.RecoveryRatePercent = float64(resp.TotalRecoveredPayments) / float64(resp.TotalFailedPayments) * 100
		resp.PartialRecoveryRatePercent = resp.PartialRecoveryRatePercent / float64(resp.TotalFailedPayments) * 100
	}

	// AI accuracy: high confidence (>0.8) cases that recovered successfully
	var aiHighConfSuccessCount, aiHighConfTotalCount int
	h.db.QueryRow(ctx, `
		SELECT
			COUNT(*) FILTER (WHERE rc.status IN ('recovered', 'customer_self_recovered')),
			COUNT(*)
		FROM recovery_cases rc
		WHERE rc.ai_strategy->>'confidence' IS NOT NULL
		  AND (rc.ai_strategy->>'confidence')::float > 0.8
		  AND rc.created_at >= NOW() - INTERVAL '30 days'
	`).Scan(&aiHighConfSuccessCount, &aiHighConfTotalCount)

	if aiHighConfTotalCount > 0 {
		resp.AIAccuracyRate = float64(aiHighConfSuccessCount) / float64(aiHighConfTotalCount) * 100
	}

	writeJSON(w, http.StatusOK, resp)
}

// ─── GET /api/v1/analytics/recovery-rate ──────────────────────────────────────

type RecoveryRateItem struct {
	Label        string  `json:"label"`
	Total        int     `json:"total"`
	Recovered    int     `json:"recovered"`
	RecoveryRate float64 `json:"recovery_rate"`
	AtRiskPaise  int64   `json:"at_risk_paise"`
	RecoveredPaise int64 `json:"recovered_paise"`
}

func (h *analyticsHandler) RecoveryRate(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	period := r.URL.Query().Get("period")
	groupBy := r.URL.Query().Get("group_by")

	if period == "" {
		period = "7d"
	}
	if groupBy == "" {
		groupBy = "failure_type"
	}

	// Parse period to interval
	var interval string
	switch period {
	case "24h":
		interval = "1 day"
	case "7d":
		interval = "7 days"
	case "30d":
		interval = "30 days"
	default:
		interval = "7 days"
	}

	// Build query based on groupBy
	var query string
	switch groupBy {
	case "failure_type":
		query = `
			SELECT
				COALESCE(failure_type, 'unknown') AS label,
				COUNT(*) AS total,
				COUNT(*) FILTER (WHERE status IN ('recovered', 'partially_recovered', 'customer_self_recovered')) AS recovered,
				COALESCE(SUM(revenue_at_risk), 0) AS at_risk,
				COALESCE(SUM(amount_recovered), 0) AS recovered_amount
			FROM recovery_cases
			WHERE created_at >= NOW() - INTERVAL '` + interval + `'
			GROUP BY failure_type
			ORDER BY total DESC
		`
	case "method":
		query = `
			SELECT
				COALESCE(p.method, 'unknown') AS label,
				COUNT(*) AS total,
				COUNT(*) FILTER (WHERE rc.status IN ('recovered', 'partially_recovered', 'customer_self_recovered')) AS recovered,
				COALESCE(SUM(rc.revenue_at_risk), 0) AS at_risk,
				COALESCE(SUM(rc.amount_recovered), 0) AS recovered_amount
			FROM recovery_cases rc
			JOIN payments p ON p.id = rc.payment_id
			WHERE rc.created_at >= NOW() - INTERVAL '` + interval + `'
			GROUP BY p.method
			ORDER BY total DESC
		`
	case "upi_error_code":
		query = `
			SELECT
				COALESCE(upi_error_code, 'unknown') AS label,
				COUNT(*) AS total,
				COUNT(*) FILTER (WHERE status IN ('recovered', 'partially_recovered', 'customer_self_recovered')) AS recovered,
				COALESCE(SUM(revenue_at_risk), 0) AS at_risk,
				COALESCE(SUM(amount_recovered), 0) AS recovered_amount
			FROM recovery_cases
			WHERE created_at >= NOW() - INTERVAL '` + interval + `'
			GROUP BY upi_error_code
			ORDER BY total DESC
		`
	default:
		http.Error(w, "invalid group_by parameter", http.StatusBadRequest)
		return
	}

	rows, err := h.db.Query(ctx, query)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	var items []RecoveryRateItem
	for rows.Next() {
		var item RecoveryRateItem
		if err := rows.Scan(&item.Label, &item.Total, &item.Recovered, &item.AtRiskPaise, &item.RecoveredPaise); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		if item.Total > 0 {
			item.RecoveryRate = float64(item.Recovered) / float64(item.Total) * 100
		}
		items = append(items, item)
	}

	writeJSON(w, http.StatusOK, items)
}

// ─── GET /api/v1/analytics/revenue ────────────────────────────────────────────

type RevenueDataPoint struct {
	Timestamp         time.Time `json:"timestamp"`
	AtRiskPaise       int64     `json:"at_risk_paise"`
	RecoveredPaise    int64     `json:"recovered_paise"`
}

func (h *analyticsHandler) Revenue(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	period := r.URL.Query().Get("period")
	interval := r.URL.Query().Get("interval")

	if period == "" {
		period = "7d"
	}
	if interval == "" {
		interval = "day"
	}

	var periodDuration, truncFunc string
	switch period {
	case "24h":
		periodDuration = "1 day"
	case "7d":
		periodDuration = "7 days"
	case "30d":
		periodDuration = "30 days"
	default:
		periodDuration = "7 days"
	}

	switch interval {
	case "hour":
		truncFunc = "date_trunc('hour', created_at)"
	case "day":
		truncFunc = "date_trunc('day', created_at)"
	default:
		truncFunc = "date_trunc('day', created_at)"
	}

	query := `
		SELECT
			` + truncFunc + ` AS bucket,
			COALESCE(SUM(revenue_at_risk), 0) AS at_risk,
			COALESCE(SUM(amount_recovered), 0) AS recovered
		FROM recovery_cases
		WHERE created_at >= NOW() - INTERVAL '` + periodDuration + `'
		GROUP BY bucket
		ORDER BY bucket ASC
	`

	rows, err := h.db.Query(ctx, query)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	var data []RevenueDataPoint
	for rows.Next() {
		var dp RevenueDataPoint
		if err := rows.Scan(&dp.Timestamp, &dp.AtRiskPaise, &dp.RecoveredPaise); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		data = append(data, dp)
	}

	writeJSON(w, http.StatusOK, data)
}

// ─── GET /api/v1/analytics/honest-exceptions ──────────────────────────────────

type HonestException struct {
	CaseID                  string  `json:"case_id"`
	AmountPaise             int64   `json:"amount_paise"`
	UPIErrorCode            string  `json:"upi_error_code"`
	Reason                  string  `json:"reason"`
	ValidatorSkipReason     *string `json:"validator_skip_reason"`
	PolicyRuleTriggered     *string `json:"policy_rule_triggered"`
	CouldHumanHaveRecovered bool    `json:"could_human_have_recovered"`
}

func (h *analyticsHandler) HonestExceptions(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	limit := 100

	if limitStr := r.URL.Query().Get("limit"); limitStr != "" {
		if l, err := strconv.Atoi(limitStr); err == nil && l > 0 && l <= 500 {
			limit = l
		}
	}

	rows, err := h.db.Query(ctx, `
		SELECT
			rc.id,
			rc.revenue_at_risk,
			COALESCE(rc.upi_error_code, 'unknown'),
			rc.status,
			rc.validator_skip_reason,
			ra.policy_rule_triggered
		FROM recovery_cases rc
		LEFT JOIN LATERAL (
			SELECT policy_rule_triggered
			FROM recovery_actions
			WHERE case_id = rc.id AND policy_rule_triggered IS NOT NULL
			LIMIT 1
		) ra ON TRUE
		WHERE rc.status IN ('failed', 'stopped', 'not_worth_recovering', 'pending_human_approval')
		  AND rc.created_at >= NOW() - INTERVAL '30 days'
		ORDER BY rc.created_at DESC
		LIMIT $1
	`, limit)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	var exceptions []HonestException
	for rows.Next() {
		var ex HonestException
		var status string
		if err := rows.Scan(
			&ex.CaseID,
			&ex.AmountPaise,
			&ex.UPIErrorCode,
			&status,
			&ex.ValidatorSkipReason,
			&ex.PolicyRuleTriggered,
		); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		// Determine reason and recoverability
		switch status {
		case "failed":
			ex.Reason = "Max retries reached without success"
			ex.CouldHumanHaveRecovered = true
		case "stopped":
			ex.Reason = "Policy engine blocked execution"
			ex.CouldHumanHaveRecovered = ex.PolicyRuleTriggered != nil && (*ex.PolicyRuleTriggered == "rule1_non_retryable_upi" || *ex.PolicyRuleTriggered == "rule8_max_retries")
		case "not_worth_recovering":
			ex.Reason = "Negative ROI — recovery cost exceeds expected value"
			ex.CouldHumanHaveRecovered = false
		case "pending_human_approval":
			ex.Reason = "Requires human review (high value or RBI compliance)"
			ex.CouldHumanHaveRecovered = true
		}

		exceptions = append(exceptions, ex)
	}

	writeJSON(w, http.StatusOK, exceptions)
}

// ─── GET /api/v1/analytics/ai-performance ─────────────────────────────────────

type AIPerformanceResponse struct {
	TotalAICalls                 int                     `json:"total_ai_calls"`
	AvgConfidence                float64                 `json:"avg_confidence"`
	HighConfidenceRecoveryRate   float64                 `json:"high_confidence_recovery_rate"`
	LowConfidenceRecoveryRate    float64                 `json:"low_confidence_recovery_rate"`
	StrategyBreakdown            []StrategyBreakdownItem `json:"strategy_breakdown"`
	CasesBlockedBeforeAI         int                     `json:"cases_blocked_before_ai"`
	CasesAIWouldHaveBeenWrong    int                     `json:"cases_ai_would_have_been_wrong"`
}

type StrategyBreakdownItem struct {
	Strategy     string  `json:"strategy"`
	Count        int     `json:"count"`
	RecoveryRate float64 `json:"recovery_rate"`
}

func (h *analyticsHandler) AIPerformance(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	var resp AIPerformanceResponse

	// Total AI calls and avg confidence
	h.db.QueryRow(ctx, `
		SELECT
			COUNT(*),
			COALESCE(AVG((ai_strategy->>'confidence')::float), 0)
		FROM recovery_cases
		WHERE ai_strategy IS NOT NULL
		  AND created_at >= NOW() - INTERVAL '30 days'
	`).Scan(&resp.TotalAICalls, &resp.AvgConfidence)

	// High confidence recovery rate (confidence > 0.8)
	var highConfRecovered, highConfTotal int
	h.db.QueryRow(ctx, `
		SELECT
			COUNT(*) FILTER (WHERE status IN ('recovered', 'customer_self_recovered')),
			COUNT(*)
		FROM recovery_cases
		WHERE ai_strategy IS NOT NULL
		  AND (ai_strategy->>'confidence')::float > 0.8
		  AND created_at >= NOW() - INTERVAL '30 days'
	`).Scan(&highConfRecovered, &highConfTotal)
	if highConfTotal > 0 {
		resp.HighConfidenceRecoveryRate = float64(highConfRecovered) / float64(highConfTotal) * 100
	}

	// Low confidence recovery rate (confidence < 0.5)
	var lowConfRecovered, lowConfTotal int
	h.db.QueryRow(ctx, `
		SELECT
			COUNT(*) FILTER (WHERE status IN ('recovered', 'customer_self_recovered')),
			COUNT(*)
		FROM recovery_cases
		WHERE ai_strategy IS NOT NULL
		  AND (ai_strategy->>'confidence')::float < 0.5
		  AND created_at >= NOW() - INTERVAL '30 days'
	`).Scan(&lowConfRecovered, &lowConfTotal)
	if lowConfTotal > 0 {
		resp.LowConfidenceRecoveryRate = float64(lowConfRecovered) / float64(lowConfTotal) * 100
	}

	// Strategy breakdown
	rows, err := h.db.Query(ctx, `
		SELECT
			ai_strategy->>'strategy' AS strategy,
			COUNT(*) AS count,
			COUNT(*) FILTER (WHERE status IN ('recovered', 'customer_self_recovered')) AS recovered
		FROM recovery_cases
		WHERE ai_strategy IS NOT NULL
		  AND ai_strategy->>'strategy' IS NOT NULL
		  AND created_at >= NOW() - INTERVAL '30 days'
		GROUP BY ai_strategy->>'strategy'
		ORDER BY count DESC
	`)
	if err == nil {
		defer rows.Close()
		for rows.Next() {
			var item StrategyBreakdownItem
			var recovered int
			rows.Scan(&item.Strategy, &item.Count, &recovered)
			if item.Count > 0 {
				item.RecoveryRate = float64(recovered) / float64(item.Count) * 100
			}
			resp.StrategyBreakdown = append(resp.StrategyBreakdown, item)
		}
	}

	// Cases blocked before AI (validator stopped them)
	h.db.QueryRow(ctx, `
		SELECT COUNT(*)
		FROM recovery_cases
		WHERE validator_skip_reason IS NOT NULL
		  AND ai_strategy IS NULL
		  AND created_at >= NOW() - INTERVAL '30 days'
	`).Scan(&resp.CasesBlockedBeforeAI)

	// Cases where AI would have been wrong (policy overrode and recovery happened)
	h.db.QueryRow(ctx, `
		SELECT COUNT(*)
		FROM recovery_actions ra
		JOIN recovery_cases rc ON rc.id = ra.case_id
		WHERE ra.policy_approved = FALSE
		  AND rc.status IN ('recovered', 'customer_self_recovered')
		  AND rc.created_at >= NOW() - INTERVAL '30 days'
	`).Scan(&resp.CasesAIWouldHaveBeenWrong)

	writeJSON(w, http.StatusOK, resp)
}

// ─── Helper ───────────────────────────────────────────────────────────────────

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}
