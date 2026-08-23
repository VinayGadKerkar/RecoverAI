"use client";

import { use } from "react";
import useSWR from "swr";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import { Badge } from "@/components/ui/badge";
import { getRecoveryCase, getAuditLogs } from "@/lib/api";
import { formatCurrency, formatDate } from "@/lib/utils";

export default function CaseDetailPage({ params }: { params: Promise<{ id: string }> }) {
  const { id } = use(params);
  const { data: caseData, isLoading: caseLoading } = useSWR(
    `/recovery-cases/${id}`,
    () => getRecoveryCase(id),
    { refreshInterval: 5000 }
  );

  const { data: auditLogs, isLoading: logsLoading } = useSWR(
    `/recovery-cases/${id}/audit-logs`,
    () => getAuditLogs(id),
    { refreshInterval: 5000 }
  );

  if (caseLoading || logsLoading) {
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
    <div className="p-8">
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

      <div className="grid grid-cols-1 gap-6 lg:grid-cols-3">
        {/* Left Column: Full Audit Timeline (2/3 width) */}
        <div className="lg:col-span-2">
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

                {auditLogs && auditLogs.length > 0 ? (
                  auditLogs.map((log, idx) => (
                    <TimelineItem key={log.id} log={log} isLast={idx === auditLogs.length - 1} />
                  ))
                ) : (
                  <p className="text-sm text-muted-foreground">No audit logs available</p>
                )}
              </div>
            </CardContent>
          </Card>
        </div>

        {/* Right Column: Breakdown Panels (1/3 width) */}
        <div className="space-y-6">
          {/* Case Summary */}
          <Card>
            <CardHeader>
              <CardTitle>Case Summary</CardTitle>
            </CardHeader>
            <CardContent className="space-y-3">
              <div>
                <p className="text-xs text-muted-foreground">Status</p>
                <Badge variant={caseData.status as any} className="mt-1">
                  {caseData.status.replace(/_/g, " ")}
                </Badge>
              </div>
              <div>
                <p className="text-xs text-muted-foreground">Revenue at Risk</p>
                <p className="text-lg font-bold text-foreground">
                  {formatCurrency(caseData.revenue_at_risk)}
                </p>
              </div>
              <div>
                <p className="text-xs text-muted-foreground">Amount Recovered</p>
                <p className="text-lg font-bold text-green-400">
                  {formatCurrency(caseData.amount_recovered)}
                </p>
              </div>
              <div>
                <p className="text-xs text-muted-foreground">Priority</p>
                <p className="text-sm font-medium text-foreground uppercase">
                  {caseData.priority}
                </p>
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
            <Card>
              <CardHeader>
                <CardTitle>AI Decision</CardTitle>
              </CardHeader>
              <CardContent className="space-y-2">
                <div>
                  <p className="text-xs text-muted-foreground">Strategy</p>
                  <p className="text-sm font-medium text-foreground">
                    {caseData.ai_strategy.strategy || "—"}
                  </p>
                </div>
                <div>
                  <p className="text-xs text-muted-foreground">Confidence</p>
                  <p className="text-sm font-medium text-foreground">
                    {((caseData.ai_strategy.confidence || 0) * 100).toFixed(1)}%
                  </p>
                </div>
                {caseData.ai_strategy.reasoning && (
                  <div>
                    <p className="text-xs text-muted-foreground">Reasoning</p>
                    <p className="text-xs text-muted-foreground mt-1">
                      {caseData.ai_strategy.reasoning}
                    </p>
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
                  <Badge variant={caseData.status as any} className="mt-1">
                    {caseData.status.replace(/_/g, " ")}
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
      </div>
    </div>
  );
}

function TimelineItem({ log, isLast }: { log: any; isLast: boolean }) {
  const actorColors: Record<string, string> = {
    webhook: "text-blue-400",
    risk_engine: "text-purple-400",
    validator: "text-orange-400",
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
        {log.details && typeof log.details === "object" && (
          <div className="mt-1 rounded-md bg-muted/30 p-2 text-xs text-muted-foreground">
            <pre className="whitespace-pre-wrap break-words">
              {JSON.stringify(log.details, null, 2)}
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
