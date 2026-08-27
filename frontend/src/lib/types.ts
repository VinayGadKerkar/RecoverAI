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
  razorpay_payment_id: string;
  customer_id?: string;
  customer_email?: string;
  amount_paise: number;
  amount_formatted: string;
  status: RecoveryCaseStatus;
  priority: "low" | "medium" | "high" | "critical";
  upi_error_code?: string;
  upi_error_category?: string;
  failure_type?: string;
  recovery_probability?: number;
  recovery_roi?: number;
  amount_recovered_paise: number;
  amount_recovered_formatted: string;
  retry_count: number;
  bank_outage_detected: boolean;
  is_mandate_payment: boolean;
  validator_skip_reason?: string;
  ai_strategy?: any;
  ai_diagnosis?: any;
  created_at: string;
  resolved_at?: string;
  recovery_time_minutes?: number;
  partial_recovery?: boolean;
  cooldown_until?: string;
  rbi_minimum_retry_at?: string;
  policy_decision?: string;
  // Alias for backwards compatibility
  revenue_at_risk?: number;
  amount_recovered?: number;
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
