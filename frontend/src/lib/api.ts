import {
  OverviewResponse,
  RecoveryRateItem,
  RevenueDataPoint,
  RecoveryCase,
  AuditLogEntry,
  HonestException,
  AIPerformanceResponse,
} from "./types";
import { getToken, setToken, clearToken } from "./auth";

const API_BASE =
  process.env.NEXT_PUBLIC_API_URL || "http://localhost:8080";

class APIError extends Error {
  constructor(public status: number, message: string) {
    super(message);
    this.name = "APIError";
  }
}

async function fetchAPI<T>(endpoint: string): Promise<T> {
  const token = getToken();
  const headers: Record<string, string> = {
    "Content-Type": "application/json",
  };
  if (token) {
    headers["Authorization"] = `Bearer ${token}`;
  }

  const res = await fetch(`${API_BASE}/api/v1${endpoint}`, { headers });

  if (res.status === 401) {
    // Token missing or expired — clear it and send user to login.
    clearToken();
    if (typeof window !== "undefined") {
      window.location.href = "/login";
    }
    throw new APIError(401, "Unauthorized");
  }

  if (!res.ok) {
    throw new APIError(res.status, `API error: ${res.statusText}`);
  }
  return res.json();
}

// ─── Auth ─────────────────────────────────────────────────────────────────────

export async function login(
  merchantId: string,
  password: string
): Promise<string> {
  const res = await fetch(`${API_BASE}/auth/login`, {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify({ merchant_id: merchantId, password }),
  });

  if (!res.ok) {
    const body = await res.json().catch(() => ({}));
    throw new APIError(res.status, body.error ?? "Login failed");
  }

  const data = await res.json();
  setToken(data.token);
  return data.merchant_id as string;
}

// ─── Analytics Endpoints ──────────────────────────────────────────────────────

export async function getOverview(): Promise<OverviewResponse> {
  return fetchAPI<OverviewResponse>("/analytics/overview");
}

export async function getRecoveryRate(
  period: "24h" | "7d" | "30d" = "7d",
  groupBy: "failure_type" | "method" | "upi_error_code" = "failure_type"
): Promise<RecoveryRateItem[]> {
  return fetchAPI<RecoveryRateItem[]>(
    `/analytics/recovery-rate?period=${period}&group_by=${groupBy}`
  );
}

export async function getRevenue(
  period: "24h" | "7d" | "30d" = "7d",
  interval: "hour" | "day" = "day"
): Promise<RevenueDataPoint[]> {
  return fetchAPI<RevenueDataPoint[]>(
    `/analytics/revenue?period=${period}&interval=${interval}`
  );
}

export async function getHonestExceptions(
  limit = 100
): Promise<HonestException[]> {
  return fetchAPI<HonestException[]>(
    `/analytics/honest-exceptions?limit=${limit}`
  );
}

export async function getAIPerformance(): Promise<AIPerformanceResponse> {
  return fetchAPI<AIPerformanceResponse>("/analytics/ai-performance");
}

// ─── Recovery Cases Endpoints ─────────────────────────────────────────────────

export async function getRecoveryCases(filters?: {
  status?: string;
  priority?: string;
  upi_error_code?: string;
  bank_outage_detected?: boolean;
  date_from?: string;
  date_to?: string;
}): Promise<RecoveryCase[]> {
  const params = new URLSearchParams();
  if (filters) {
    Object.entries(filters).forEach(([key, value]) => {
      if (value !== undefined) {
        params.append(key, String(value));
      }
    });
  }
  const query = params.toString() ? `?${params.toString()}` : "";
  return fetchAPI<RecoveryCase[]>(`/recovery-cases${query}`);
}

export async function getRecoveryCase(id: string): Promise<RecoveryCase> {
  return fetchAPI<RecoveryCase>(`/recovery-cases/${id}`);
}

export async function getAuditLogs(caseId: string): Promise<AuditLogEntry[]> {
  return fetchAPI<AuditLogEntry[]>(`/recovery-cases/${caseId}/audit-logs`);
}

// ─── Recent Activity ──────────────────────────────────────────────────────────

export async function getRecentCases(limit = 10): Promise<RecoveryCase[]> {
  return fetchAPI<RecoveryCase[]>(
    `/recovery-cases?limit=${limit}&sort=created_at:desc`
  );
}
