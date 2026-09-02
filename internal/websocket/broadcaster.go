package websocket

import (
	"context"
	"fmt"
	"log"

	"github.com/jackc/pgx/v5/pgxpool"
)

// Broadcaster handles broadcasting WebSocket messages based on pipeline events
type Broadcaster struct {
	hub *Hub
	db  *pgxpool.Pool
}

// NewBroadcaster creates a new Broadcaster instance
func NewBroadcaster(hub *Hub, db *pgxpool.Pool) *Broadcaster {
	return &Broadcaster{
		hub: hub,
		db:  db,
	}
}

// AuditEvent broadcasts an audit event to all connected clients
func (b *Broadcaster) AuditEvent(caseID, paymentID, actor, action string, metadata map[string]interface{}) {
	message := formatAuditMessage(actor, action, metadata)
	
	msg := NewAuditEvent(caseID, paymentID, actor, action, message, metadata)
	data, err := msg.ToJSON()
	if err != nil {
		log.Printf("Failed to marshal audit event: %v", err)
		return
	}
	
	b.hub.Broadcast(data)
}

// CaseStatusChanged broadcasts a case status change event
func (b *Broadcaster) CaseStatusChanged(caseID, oldStatus, newStatus string, amountPaise int64, upiErrorCode string) {
	msg := NewCaseStatusChanged(caseID, oldStatus, newStatus, amountPaise, upiErrorCode)
	data, err := msg.ToJSON()
	if err != nil {
		log.Printf("Failed to marshal case status changed: %v", err)
		return
	}
	
	b.hub.Broadcast(data)
}

// MetricUpdate fetches current metrics from DB and broadcasts them
func (b *Broadcaster) MetricUpdate() {
	ctx := context.Background()
	
	var metrics MetricUpdateData
	err := b.db.QueryRow(ctx, `
		SELECT
			COALESCE(SUM(amount_recovered), 0) AS recovered_revenue,
			COALESCE(SUM(revenue_at_risk), 0) AS revenue_at_risk,
			COUNT(*) FILTER (WHERE status IN ('recovered', 'partially_recovered', 'customer_self_recovered')) AS recovered_count,
			COUNT(*) FILTER (WHERE status = 'customer_self_recovered') AS self_recovered_count,
			COUNT(*) FILTER (WHERE status = 'outage_batched') AS outage_batched_count,
			COUNT(*) FILTER (WHERE status = 'not_worth_recovering') AS not_worth_count,
			COUNT(*) FILTER (WHERE status = 'pending_human_approval') AS pending_human_count,
			COUNT(*) FILTER (WHERE status IN ('open', 'in_progress')) AS active_count,
			COUNT(*) AS total_failed
		FROM recovery_cases
		WHERE created_at >= NOW() - INTERVAL '30 days'
	`).Scan(
		&metrics.RecoveredRevenuePaise,
		&metrics.RevenueAtRiskPaise,
		&metrics.TotalRecoveredPayments,
		&metrics.CustomerSelfRecoveredCount,
		&metrics.OutageBatchedCount,
		&metrics.NotWorthRecoveringCount,
		&metrics.PendingHumanApprovalCount,
		&metrics.ActiveCases,
		new(int), // total_failed (not needed in struct but needed for scan)
	)
	
	if err != nil {
		log.Printf("Failed to fetch metrics: %v", err)
		return
	}
	
	// Calculate recovery rate
	if metrics.TotalRecoveredPayments > 0 {
		totalFailed := metrics.ActiveCases + metrics.TotalRecoveredPayments + 
			metrics.NotWorthRecoveringCount + metrics.OutageBatchedCount
		if totalFailed > 0 {
			metrics.RecoveryRatePercent = float64(metrics.TotalRecoveredPayments) / float64(totalFailed) * 100
		}
	}
	
	msg := NewMetricUpdate(metrics)
	data, err := msg.ToJSON()
	if err != nil {
		log.Printf("Failed to marshal metric update: %v", err)
		return
	}
	
	b.hub.Broadcast(data)
}

// OutageDetected broadcasts a bank outage detection event
func (b *Broadcaster) OutageDetected(code string, failureCount, windowMinutes, affectedCount int) {
	msg := NewOutageDetected(code, failureCount, windowMinutes, affectedCount)
	data, err := msg.ToJSON()
	if err != nil {
		log.Printf("Failed to marshal outage detected: %v", err)
		return
	}
	
	b.hub.Broadcast(data)
}

// formatAuditMessage formats actor/action into a human-readable message
func formatAuditMessage(actor, action string, metadata map[string]interface{}) string {
	switch actor {
	case "system":
		if action == "webhook_received" {
			return "Webhook received"
		}
	
	case "risk_engine":
		if action == "risk_scored" {
			priority := getMetadataString(metadata, "priority", "unknown")
			prob := getMetadataFloat(metadata, "recovery_probability", 0.0)
			return fmt.Sprintf("Risk scored: %s priority, %.0f%% recovery probability", priority, prob*100)
		}
	
	case "validator":
		switch action {
		case "check_1_passed", "check1_pass":
			return "✓ Check 1: Payment not already captured"
		case "check_2_passed", "check2_pass":
			return "✓ Check 2: No active bank outage"
		case "check_3_passed", "check3_pass":
			return "✓ Check 3: RBI compliant"
		case "check_4_passed", "check4_pass":
			roi := getMetadataFloat(metadata, "roi_paise", 0.0)
			return fmt.Sprintf("✓ Check 4: ROI positive (₹%.2f)", roi/100)
		case "check_5_passed", "check5_pass":
			return "✓ Check 5: Error is retryable"
		case "check_6_passed", "check6_pass":
			n := getMetadataInt(metadata, "retry_count", 0)
			max := getMetadataInt(metadata, "max_retries", 2)
			return fmt.Sprintf("✓ Check 6: Retries available (%d of %d used)", n, max)
		case "check_1_failed", "check_2_failed", "check_3_failed", 
		     "check_4_failed", "check_5_failed", "check_6_failed",
		     "check1_blocked", "check2_blocked", "check3_blocked",
		     "check4_blocked", "check5_blocked", "check6_blocked":
			reason := getMetadataString(metadata, "reason", "validation failed")
			checkNum := string(action[6]) // extract check number
			return fmt.Sprintf("✗ Check %s: %s", checkNum, reason)
		case "validator_passed":
			return "All 6 checks passed — calling AI"
		case "validator_blocked":
			reason := getMetadataString(metadata, "reason", "blocked")
			return fmt.Sprintf("Blocked: %s", reason)
		}
	
	case "ai_agent":
		switch action {
		case "ai_analyzed":
			failureType := getMetadataString(metadata, "failure_type", "unknown")
			prob := getMetadataFloat(metadata, "recovery_probability", 0.0)
			return fmt.Sprintf("AI: %s, %.0f%% recovery probability", failureType, prob*100)
		case "ai_strategy_selected":
			strategy := getMetadataString(metadata, "strategy", "unknown")
			confidence := getMetadataFloat(metadata, "confidence", 0.0)
			return fmt.Sprintf("Strategy: %s — %.0f%% confidence", strategy, confidence*100)
		}
	
	case "policy_engine":
		switch action {
		case "policy_approved":
			return "Policy engine: APPROVED — executing"
		case "policy_blocked":
			rule := getMetadataString(metadata, "rule", "policy violation")
			return fmt.Sprintf("Policy engine: BLOCKED — %s", rule)
		}
	
	case "execution_worker":
		switch action {
		case "action_executed":
			actionType := getMetadataString(metadata, "action_type", "action")
			return fmt.Sprintf("Action: %s executed", actionType)
		case "payment_captured":
			amount := getMetadataInt64(metadata, "amount_paise", 0)
			return fmt.Sprintf("✅ ₹%.2f recovered", float64(amount)/100)
		}
	
	case "customer_self":
		if action == "self_recovered" {
			return "Customer paid themselves — case closed"
		}
	}
	
	// Fallback: return action as-is
	return action
}

// Helper functions to safely extract metadata values
func getMetadataString(metadata map[string]interface{}, key, defaultVal string) string {
	if val, ok := metadata[key]; ok {
		if str, ok := val.(string); ok {
			return str
		}
	}
	return defaultVal
}

func getMetadataInt(metadata map[string]interface{}, key string, defaultVal int) int {
	if val, ok := metadata[key]; ok {
		switch v := val.(type) {
		case int:
			return v
		case int64:
			return int(v)
		case float64:
			return int(v)
		}
	}
	return defaultVal
}

func getMetadataInt64(metadata map[string]interface{}, key string, defaultVal int64) int64 {
	if val, ok := metadata[key]; ok {
		switch v := val.(type) {
		case int64:
			return v
		case int:
			return int64(v)
		case float64:
			return int64(v)
		}
	}
	return defaultVal
}

func getMetadataFloat(metadata map[string]interface{}, key string, defaultVal float64) float64 {
	if val, ok := metadata[key]; ok {
		if f, ok := val.(float64); ok {
			return f
		}
	}
	return defaultVal
}
