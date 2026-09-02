"use client";

import { useEffect, useState, useCallback } from "react";
import { useRouter, usePathname } from "next/navigation";
import useSWR from "swr";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import { Badge } from "@/components/ui/badge";
import { getRecoveryCase, getAuditLogs } from "@/lib/api";
import { formatCurrency, formatDate } from "@/lib/utils";
import { useWebSocket, type WSMessage } from "@/hooks/useWebSocket";

export default function CaseDetailPage() {
  const pathname = usePathname();
  const [id, setId] = useState<string | null>(null);
  const [realtimeAuditLogs, setRealtimeAuditLogs] = useState<any[]>([]);

  useEffect(() => {
    // Extract ID from pathname
    const parts = pathname?.split('/') || [];
    const caseId = parts[parts.length - 1];
    if (caseId && caseId !== '[id]') {
      setId(caseId);
    }
  }, [pathname]);

  const { data: caseData, isLoading: caseLoading, mutate: mutateCaseData } = useSWR(
    id ? `/recovery-cases/${id}` : null,
    () => (id ? getRecoveryCase(id) : null),
    { refreshInterval: 30000 } // Reduced from 5s to 30s - WebSocket handles real-time updates
  );

  const { data: auditLogs, isLoading: logsLoading } = useSWR(
    id ? `/recovery-cases/${id}/audit-logs` : null,
    () => (id ? getAuditLogs(id) : null),
    { refreshInterval: 0, revalidateOnFocus: false } // No polling - use WebSocket
  );

  // WebSocket connection for real-time updates
  const { isConnected } = useWebSocket(
    `ws://${typeof window !== 'undefined' ? window.location.hostname : 'localhost'}:8080/ws`,
    {
      onAuditEvent: useCallback((message: WSMessage) => {
        // Only process events for this case
        if (message.case_id === id) {
          const newLog = {
            actor: message.data.actor,
            action: message.data.action,
            message: formatAuditMessage(message.data.actor, message.data.action, message.data.metadata),
            metadata: message.data.metadata,
            created_at: message.timestamp,
          };
          
          setRealtimeAuditLogs(prev => [newLog, ...prev]);
          
          // Trigger case data refresh if status might have changed
          if (message.data.action === 'payment_captured' || 
              message.data.action === 'validator_blocked' ||
              message.data.action === 'self_recovered') {
            mutateCaseData();
          }
        }
      }, [id, mutateCaseData]),
      
      onCaseStatusChanged: useCallback((message: WSMessage) => {
        if (message.case_id === id) {
          mutateCaseData();
        }
      }, [id, mutateCaseData]),
    }
  );

  // Combine initial audit logs with real-time updates
  const allAuditLogs = [...realtimeAuditLogs, ...(auditLogs || [])];

  if (!id || caseLoading || logsLoading) {
    return (
      <div className="flex h-screen items-center justify-center">
        <div className="h-8 w-8 animate-spin rounded-full border-4 border-primary border-t-transparent"></div>
      </div>
    );
  }

  if (!caseData) {
    return (
      <div className="flex h-screen items-center justify-center">
        <p className="text-muted-foreground">Case not found</p>
      </div>
    );
  }

  const isCustomerSelfRecovered = caseData.status === "customer_self_recovered";

  return (
    <div className="min-h-screen bg-background p-8">
      <div className="mb-6">
        <h2 className="text-3xl font-bold text-foreground">Case Details</h2>
        <p className="text-sm text-muted-foreground">Case ID: {caseData.id}</p>
      </div>

      {isCustomerSelfRecovered && (
        <div className="mb-6 rounded-lg border border-slate-700 bg-slate-800/50 p-4">
          <div className="flex items-center gap-2">
            <svg
              xmlns="http://www.w3.org/2000/svg"
              viewBox="0 0 24 24"
              fill="none"
              stroke="currentColor"
              strokeLinecap="round"
              strokeLinejoin="round"
              strokeWidth="2"
              className="h-5 w-5 text-slate-400"
            >
              <path d="M22 11.08V12a10 10 0 1 1-5.93-9.14" />
              <polyline points="22 4 12 14.01 9 11.01" />
            </svg>
            <p className="text-sm font-medium text-slate-300">
              This payment was recovered by the customer themselves — no system action was
              needed.
            </p>
          </div>
        </div>
      )}

      <div className="flex flex-col gap-6 lg:flex-row">
        {/* Right Column First (Mobile) / Sidebar (Desktop) - 1/3 width on large screens */}
        <div className="w-full lg:w-1/3 lg:order-2 space-y-6">
          {/* Case Summary */}
          <Card>
            <CardHeader>
              <CardTitle>Case Summary</CardTitle>
            </CardHeader>
            <CardContent className="space-y-3">
              <div>
                <p className="text-xs text-muted-foreground">Status</p>
                <Badge className="mt-1">
                  {caseData.status ? caseData.status.replace(/_/g, " ") : "processing"}
                </Badge>
              </div>
              <div>
                <p className="text-xs text-muted-foreground">Revenue at Risk</p>
                <p className="text-lg font-bold text-foreground">
                  {formatCurrency(caseData.amount_paise)}
                </p>
              </div>
              <div>
                <p className="text-xs text-muted-foreground">Amount Recovered</p>
                <p className="text-lg font-bold text-green-400">
                  {formatCurrency(caseData.amount_recovered_paise)}
                </p>
              </div>
              <div>
                <p className="text-xs text-muted-foreground">Recovery Rate</p>
                <p className="text-sm font-bold text-foreground">
                  {caseData.amount_paise > 0 
                    ? `${((caseData.amount_recovered_paise / caseData.amount_paise) * 100).toFixed(1)}%`
                    : '0%'}
                </p>
              </div>
              <div>
                <p className="text-xs text-muted-foreground">Priority</p>
                <span
                  className={`text-sm font-bold uppercase ${
                    caseData.priority === "critical"
                      ? "text-red-400"
                      : caseData.priority === "high"
                      ? "text-orange-400"
                      : caseData.priority === "medium"
                      ? "text-yellow-400"
                      : "text-slate-400"
                  }`}
                >
                  {caseData.priority}
                </span>
              </div>
              <div>
                <p className="text-xs text-muted-foreground">Retry Count</p>
                <p className="text-sm font-medium text-foreground">{caseData.retry_count}</p>
              </div>
            </CardContent>
          </Card>

          {/* Why At Risk */}
          <Card>
            <CardHeader>
              <CardTitle>Why At Risk</CardTitle>
            </CardHeader>
            <CardContent className="space-y-2">
              <div>
                <p className="text-xs text-muted-foreground">Failure Type</p>
                <p className="text-sm font-medium text-foreground">
                  {caseData.failure_type || "Unknown"}
                </p>
              </div>
              {caseData.upi_error_code && (
                <div>
                  <p className="text-xs text-muted-foreground">UPI Error Code</p>
                  <p className="text-sm font-medium text-orange-400">
                    {caseData.upi_error_code}
                  </p>
                </div>
              )}
              {caseData.bank_outage_detected && (
                <div className="mt-2 rounded-md bg-purple-950/50 border border-purple-900/50 p-2">
                  <p className="text-xs text-purple-400">Bank outage detected</p>
                </div>
              )}
            </CardContent>
          </Card>

          {/* Validator Checks */}
          {caseData.validator_skip_reason ? (
            <Card>
              <CardHeader>
                <CardTitle>Validator Checks</CardTitle>
              </CardHeader>
              <CardContent>
                <div className="rounded-md bg-orange-950/50 border border-orange-900/50 p-3">
                  <p className="text-xs font-medium text-orange-400 mb-1">SKIPPED</p>
                  <p className="text-sm text-foreground">{caseData.validator_skip_reason}</p>
                </div>
              </CardContent>
            </Card>
          ) : (
            <Card>
              <CardHeader>
                <CardTitle>Validator Checks</CardTitle>
              </CardHeader>
              <CardContent className="space-y-2">
                <CheckItem label="Payment status" passed />
                <CheckItem label="Bank outage" passed={!caseData.bank_outage_detected} />
                <CheckItem label="RBI compliance" passed />
                <CheckItem label="Recovery ROI" passed />
                <CheckItem label="Error retryability" passed />
                <CheckItem label="Retry count" passed />
              </CardContent>
            </Card>
          )}

          {/* AI Decision */}
          {caseData.ai_strategy && (
            <Card className="border-l-4 border-l-cyan-500">
              <CardHeader>
                <div className="flex items-center gap-2">
                  <svg className="h-5 w-5 text-cyan-400" fill="none" stroke="currentColor" viewBox="0 0 24 24">
                    <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M9.663 17h4.673M12 3v1m6.364 1.636l-.707.707M21 12h-1M4 12H3m3.343-5.657l-.707-.707m2.828 9.9a5 5 0 117.072 0l-.548.547A3.374 3.374 0 0014 18.469V19a2 2 0 11-4 0v-.531c0-.895-.356-1.754-.988-2.386l-.548-.547z" />
                  </svg>
                  <CardTitle>AI Decision</CardTitle>
                </div>
              </CardHeader>
              <CardContent className="space-y-4">
                {/* Strategy */}
                <div className="rounded-lg bg-cyan-950/30 border border-cyan-900/50 p-3">
                  <p className="text-xs font-semibold text-cyan-400 mb-1">RECOMMENDED STRATEGY</p>
                  <p className="text-base font-bold text-foreground">
                    {caseData.ai_strategy.strategy?.replace(/_/g, " ").toUpperCase() || "—"}
                  </p>
                </div>

                {/* Confidence Bar */}
                <div>
                  <div className="flex items-center justify-between mb-2">
                    <p className="text-xs font-medium text-muted-foreground">Confidence Level</p>
                    <p className="text-sm font-bold text-foreground">
                      {((caseData.ai_strategy.confidence || 0) * 100).toFixed(1)}%
                    </p>
                  </div>
                  <div className="h-2 rounded-full bg-slate-800 overflow-hidden">
                    <div 
                      className={`h-full transition-all ${
                        (caseData.ai_strategy.confidence || 0) >= 0.8 
                          ? 'bg-green-500' 
                          : (caseData.ai_strategy.confidence || 0) >= 0.5 
                          ? 'bg-yellow-500' 
                          : 'bg-red-500'
                      }`}
                      style={{ width: `${((caseData.ai_strategy.confidence || 0) * 100)}%` }}
                    ></div>
                  </div>
                </div>

                {/* Reasoning */}
                {caseData.ai_strategy.reasoning && (
                  <div className="rounded-lg bg-slate-800/50 border border-slate-700 p-3">
                    <div className="flex items-start gap-2 mb-2">
                      <svg className="h-4 w-4 text-slate-400 mt-0.5 flex-shrink-0" fill="none" stroke="currentColor" viewBox="0 0 24 24">
                        <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M13 16h-1v-4h-1m1-4h.01M21 12a9 9 0 11-18 0 9 9 0 0118 0z" />
                      </svg>
                      <div>
                        <p className="text-xs font-semibold text-slate-300 mb-1">AI REASONING</p>
                        <p className="text-sm text-slate-300 leading-relaxed">
                          {caseData.ai_strategy.reasoning}
                        </p>
                      </div>
                    </div>
                  </div>
                )}

                {/* Key Factors (if available in metadata) */}
                {caseData.ai_strategy.factors && Array.isArray(caseData.ai_strategy.factors) && (
                  <div>
                    <p className="text-xs font-semibold text-muted-foreground mb-2">KEY FACTORS CONSIDERED</p>
                    <div className="space-y-1">
                      {caseData.ai_strategy.factors.map((factor: string, idx: number) => (
                        <div key={idx} className="flex items-start gap-2 text-xs">
                          <span className="text-cyan-400 mt-0.5">▸</span>
                          <span className="text-slate-300">{factor}</span>
                        </div>
                      ))}
                    </div>
                  </div>
                )}
              </CardContent>
            </Card>
          )}

          {/* Policy Rules */}
          {caseData.policy_decision && (
            <Card>
              <CardHeader>
                <CardTitle>Policy Rules</CardTitle>
              </CardHeader>
              <CardContent>
                <p className="text-sm text-foreground">{caseData.policy_decision}</p>
              </CardContent>
            </Card>
          )}

          {/* Result */}
          <Card>
            <CardHeader>
              <CardTitle>Result</CardTitle>
            </CardHeader>
            <CardContent>
              <div className="space-y-2">
                <div>
                  <p className="text-xs text-muted-foreground">Final Status</p>
                  <Badge className="mt-1">
                    {caseData.status ? caseData.status.replace(/_/g, " ") : "processing"}
                  </Badge>
                </div>
                {caseData.resolved_at && (
                  <div>
                    <p className="text-xs text-muted-foreground">Resolved At</p>
                    <p className="text-sm text-foreground">{formatDate(caseData.resolved_at)}</p>
                  </div>
                )}
                {caseData.partial_recovery && (
                  <div className="mt-2 rounded-md bg-teal-950/50 border border-teal-900/50 p-2">
                    <p className="text-xs text-teal-400">Partial recovery occurred</p>
                  </div>
                )}
              </div>
            </CardContent>
          </Card>
        </div>

        {/* Left Column: Full Audit Timeline - 2/3 width on large screens */}
        <div className="w-full lg:w-2/3 lg:order-1">
          <Card>
            <CardHeader>
              <CardTitle>Full Audit Timeline</CardTitle>
              <p className="text-sm text-muted-foreground">
                All actors and decisions in order
              </p>
            </CardHeader>
            <CardContent>
              <div className="relative space-y-6">
                {/* Timeline vertical line */}
                <div className="absolute left-[15px] top-0 h-full w-[2px] bg-border"></div>

                {/* WebSocket Connection Status */}
                {isConnected && (
                  <div className="mb-4 flex items-center gap-2 text-xs text-green-500">
                    <div className="h-2 w-2 rounded-full bg-green-500 animate-pulse"></div>
                    <span>Real-time updates active</span>
                  </div>
                )}

                {allAuditLogs && allAuditLogs.length > 0 ? (
                  allAuditLogs.map((log: any, idx: number) => (
                    <TimelineItem key={log.id || `rt-${idx}`} log={log} isLast={idx === allAuditLogs.length - 1} />
                  ))
                ) : (
                  <p className="text-sm text-muted-foreground">No audit logs available</p>
                )}
              </div>
            </CardContent>
          </Card>
        </div>
      </div>
    </div>
  );
}

function TimelineItem({ log }: { log: any; isLast: boolean }) {
  const actorColors: Record<string, string> = {
    webhook: "text-blue-400",
    risk_engine: "text-purple-400",
    validator: "text-orange-400",
    validator_consumer: "text-orange-400",
    ai_risk_analyst: "text-pink-400",
    ai_strategist: "text-cyan-400",
    ai_executor: "text-emerald-400",
    policy_engine: "text-yellow-400",
    execution_worker: "text-green-400",
    result_processor: "text-teal-400",
    customer_self: "text-slate-400",
  };

  const actorColor = actorColors[log.actor] || "text-foreground";

  return (
    <div className="relative flex gap-4 pl-10">
      {/* Timeline dot */}
      <div className={`absolute left-[7px] top-1 h-4 w-4 rounded-full border-2 border-border bg-background flex items-center justify-center`}>
        <div className={`h-2 w-2 rounded-full ${actorColor.replace('text-', 'bg-')}`}></div>
      </div>

      <div className="flex-1">
        <div className="flex items-center gap-2">
          <span className={`text-xs font-bold uppercase ${actorColor}`}>
            [{log.actor.replace(/_/g, " ")}]
          </span>
          <span className="text-sm text-foreground">{log.action}</span>
        </div>
        {log.metadata && typeof log.metadata === "object" && (
          <div className="mt-1 rounded-md bg-muted/30 p-2 text-xs text-muted-foreground">
            <pre className="whitespace-pre-wrap break-words">
              {JSON.stringify(log.metadata, null, 2)}
            </pre>
          </div>
        )}
        <p className="mt-1 text-xs text-muted-foreground">{formatDate(log.created_at)}</p>
      </div>
    </div>
  );
}

function CheckItem({ label, passed }: { label: string; passed: boolean }) {
  return (
    <div className="flex items-center justify-between text-sm">
      <span className="text-muted-foreground">{label}</span>
      {passed ? (
        <span className="text-green-400 text-xs font-medium">✓ PASS</span>
      ) : (
        <span className="text-red-400 text-xs font-medium">✗ FAIL</span>
      )}
    </div>
  );
}


// Format audit message based on actor/action
function formatAuditMessage(actor: string, action: string, metadata: any = {}): string {
  switch (actor) {
    case "risk_engine":
      if (action === "risk_scored") {
        const priority = metadata.priority || "unknown";
        const prob = (metadata.recovery_probability || 0) * 100;
        return `Risk scored: ${priority} priority, ${prob.toFixed(0)}% recovery probability`;
      }
      break;
    
    case "validator":
      switch (action) {
        case "check_1_passed":
        case "check1_pass":
          return "✓ Check 1: Payment not already captured";
        case "check_2_passed":
        case "check2_pass":
          return "✓ Check 2: No active bank outage";
        case "check_3_passed":
        case "check3_pass":
          return "✓ Check 3: RBI compliant";
        case "check_4_passed":
        case "check4_pass":
          const roi = (metadata.roi_paise || 0) / 100;
          return `✓ Check 4: ROI positive (₹${roi.toFixed(2)})`;
        case "check_5_passed":
        case "check5_pass":
          return "✓ Check 5: Error is retryable";
        case "check_6_passed":
        case "check6_pass":
          const n = metadata.retry_count || 0;
          const max = metadata.max_retries || 2;
          return `✓ Check 6: Retries available (${n} of ${max} used)`;
        case "validator_passed":
          return "All 6 checks passed — calling AI";
        case "validator_blocked":
          return `Blocked: ${metadata.reason || "validation failed"}`;
      }
      break;
    
    case "ai_agent":
      switch (action) {
        case "ai_analyzed":
          const failureType = metadata.failure_type || "unknown";
          const prob = (metadata.recovery_probability || 0) * 100;
          return `AI: ${failureType}, ${prob.toFixed(0)}% recovery probability`;
        case "ai_strategy_selected":
          const strategy = metadata.strategy || "unknown";
          const confidence = (metadata.confidence || 0) * 100;
          return `Strategy: ${strategy} — ${confidence.toFixed(0)}% confidence`;
      }
      break;
    
    case "execution_worker":
      switch (action) {
        case "action_executed":
          return `Action: ${metadata.action_type || "action"} executed`;
        case "payment_captured":
          const amount = (metadata.amount_paise || 0) / 100;
          return `✅ ₹${amount.toFixed(2)} recovered`;
      }
      break;
  }
  
  // Fallback: return action as-is
  return action.replace(/_/g, " ");
}
