// TODO: mirror Go models.types as TypeScript interfaces
export interface Payment {
  id: string;
  merchant_id: string;
  amount: number; // paise
  currency: string;
  status: "created" | "authorized" | "captured" | "failed" | "refunded";
  method: "upi" | "card" | "netbanking" | "wallet";
  error_code?: string;
  bank?: string;
  created_at: string;
}

export interface RecoveryAttempt {
  id: number;
  payment_id: string;
  attempt_number: number;
  status: string;
  action?: string;
  ai_command?: AICommand;
  policy_decision?: string;
  policy_reason?: string;
  executed_at?: string;
  created_at: string;
}

export interface AICommand {
  payment_id: string;
  recommended_action: string;
  rationale: string;
  confidence: number;
  diagnosis: string;
  requires_approval: boolean;
  generated_at: string;
}

export interface AnalyticsSummary {
  total_failed: number;
  total_recovered: number;
  recovery_rate_pct: number;
  revenue_recovered: number; // paise
}
