package websocket

import (
	"encoding/json"
	"time"
)

// WSMessage represents a WebSocket message sent to clients
type WSMessage struct {
	Type      string      `json:"type"`
	Timestamp time.Time   `json:"timestamp"`
	CaseID    string      `json:"case_id,omitempty"`
	PaymentID string      `json:"payment_id,omitempty"`
	Data      interface{} `json:"data"`
}

// AuditEventData represents an audit event in the pipeline
type AuditEventData struct {
	Actor    string                 `json:"actor"`
	Action   string                 `json:"action"`
	Message  string                 `json:"message"`
	Metadata map[string]interface{} `json:"metadata,omitempty"`
}

// CaseStatusChangedData represents a case status transition
type CaseStatusChangedData struct {
	CaseID       string `json:"case_id"`
	OldStatus    string `json:"old_status"`
	NewStatus    string `json:"new_status"`
	AmountPaise  int64  `json:"amount_paise"`
	UPIErrorCode string `json:"upi_error_code,omitempty"`
}

// MetricUpdateData represents dashboard metric changes
type MetricUpdateData struct {
	RecoveredRevenuePaise        int64   `json:"recovered_revenue_paise"`
	RevenueAtRiskPaise           int64   `json:"revenue_at_risk_paise"`
	RecoveryRatePercent          float64 `json:"recovery_rate_percent"`
	TotalRecoveredPayments       int     `json:"total_recovered_payments"`
	CustomerSelfRecoveredCount   int     `json:"customer_self_recovered_count"`
	OutageBatchedCount           int     `json:"outage_batched_count"`
	NotWorthRecoveringCount      int     `json:"not_worth_recovering_count"`
	PendingHumanApprovalCount    int     `json:"pending_human_approval_count"`
	ActiveCases                  int     `json:"active_cases"`
}

// OutageDetectedData represents a bank outage detection
type OutageDetectedData struct {
	UPIErrorCode   string `json:"upi_error_code"`
	FailureCount   int    `json:"failure_count"`
	WindowMinutes  int    `json:"window_minutes"`
	AffectedCount  int    `json:"affected_count"`
}

// PipelineHeartbeatData represents a keepalive message
type PipelineHeartbeatData struct {
	ActiveCases int `json:"active_cases"`
	Processing  int `json:"processing"`
}

// NewWSMessage creates a new WebSocket message
func NewWSMessage(msgType string, data interface{}) *WSMessage {
	return &WSMessage{
		Type:      msgType,
		Timestamp: time.Now(),
		Data:      data,
	}
}

// NewAuditEvent creates an audit event message
func NewAuditEvent(caseID, paymentID, actor, action, message string, metadata map[string]interface{}) *WSMessage {
	return &WSMessage{
		Type:      "audit_event",
		Timestamp: time.Now(),
		CaseID:    caseID,
		PaymentID: paymentID,
		Data: AuditEventData{
			Actor:    actor,
			Action:   action,
			Message:  message,
			Metadata: metadata,
		},
	}
}

// NewCaseStatusChanged creates a case status change message
func NewCaseStatusChanged(caseID, oldStatus, newStatus string, amountPaise int64, upiErrorCode string) *WSMessage {
	return &WSMessage{
		Type:      "case_status_changed",
		Timestamp: time.Now(),
		CaseID:    caseID,
		Data: CaseStatusChangedData{
			CaseID:       caseID,
			OldStatus:    oldStatus,
			NewStatus:    newStatus,
			AmountPaise:  amountPaise,
			UPIErrorCode: upiErrorCode,
		},
	}
}

// NewMetricUpdate creates a metric update message
func NewMetricUpdate(data MetricUpdateData) *WSMessage {
	return &WSMessage{
		Type:      "metric_update",
		Timestamp: time.Now(),
		Data:      data,
	}
}

// NewOutageDetected creates an outage detection message
func NewOutageDetected(code string, failureCount, windowMinutes, affectedCount int) *WSMessage {
	return &WSMessage{
		Type:      "outage_detected",
		Timestamp: time.Now(),
		Data: OutageDetectedData{
			UPIErrorCode:  code,
			FailureCount:  failureCount,
			WindowMinutes: windowMinutes,
			AffectedCount: affectedCount,
		},
	}
}

// NewPipelineHeartbeat creates a heartbeat message
func NewPipelineHeartbeat(activeCases, processing int) *WSMessage {
	return &WSMessage{
		Type:      "pipeline_heartbeat",
		Timestamp: time.Now(),
		Data: PipelineHeartbeatData{
			ActiveCases: activeCases,
			Processing:  processing,
		},
	}
}

// ToJSON converts a WSMessage to JSON bytes
func (m *WSMessage) ToJSON() ([]byte, error) {
	return json.Marshal(m)
}
