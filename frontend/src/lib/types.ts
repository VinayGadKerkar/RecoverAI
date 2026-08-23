// RecoverAI TypeScript types — mirrors Go backend models

export type RecoveryCaseStatus =
  | "open"
  | "in_progress"
  | "recovered"
  | "partially_recovered"
  | "failed"
  | "customer_self_recovered"
  | "outage_batched"
  | "not_worth_recovering"
  | "pending_human_approval"
  | "stopped";

export interface OverviewResponse {
  revenue_at_risk_paise: number;
  recovered_revenue_paise: number;
  recovery_rate_percent: number;
  partial_recovery_rate_percent: number;
  total_failed_payments: number;
  total_recovered_payments: number;
  customer_self_recovered_count: number;
  outage_batched_count: number;
  not_worth_recovering_count: number;
  avg_recovery_time_minutes: number;
  active_cases: number;
  pending_human_approval_count: number;
  ai_accuracy_rate: number;
}

export interface RecoveryRateItem {
  label: string;
  total: number;
  recovered: number;
  recovery_rate: number;
  at_risk_paise: number;
  recovered_paise: number;
}

export interface RevenueDataPoint {
  timestamp: string;
  at_risk_paise: number;
  recovered_paise: number;
}

export interface RecoveryCase {
  id: string;
  payment_id: string;
  merchant_id: string;
  customer_id: string;
  revenue_at_risk: number; // paise
  amount_recovered: number; // paise
  status: RecoveryCaseStatus;
  priority: "low" | "medium" | "high" | "critical";
  recovery_probability: number;
  failure_type: string;
  upi_error_code?: string;
  bank_outage_detected: boolean;
  validator_skip_reason?: string;
  ai_risk_assessment?: any;
  ai_strategy?: any;
  policy_decision?: string;
  retry_count: number;
  partial_recovery: boolean;
  created_at: string;
  resolved_at?: string;
  customer_name?: string;
  customer_phone?: string;
}

export interface AuditLogEntry {
  id: string;
  case_id: string;
  actor: string;
  action: string;
  details: any;
  created_at: string;
}

export interface HonestException {
  case_id: string;
  amount_paise: number;
  upi_error_code: string;
  reason: string;
  validator_skip_reason?: string;
  policy_rule_triggered?: string;
  could_human_have_recovered: boolean;
}

export interface AIPerformanceResponse {
  total_ai_calls: number;
  avg_confidence: number;
  high_confidence_recovery_rate: number;
  low_confidence_recovery_rate: number;
  strategy_breakdown: StrategyBreakdownItem[];
  cases_blocked_before_ai: number;
  cases_ai_would_have_been_wrong: number;
}

export interface StrategyBreakdownItem {
  strategy: string;
  count: number;
  recovery_rate: number;
}
