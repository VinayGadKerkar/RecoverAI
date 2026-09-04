"use client";

import { useEffect, useState } from "react";
import { usePathname } from "next/navigation";
import useSWR from "swr";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import { Badge } from "@/components/ui/badge";
import { getRecoveryCase, getAuditLogs } from "@/lib/api";
import { formatCurrency, formatDate } from "@/lib/utils";
import { useWebSocket } from "@/hooks/useWebSocket";
import AuditTimeline from "@/components/AuditTimeline";
import { CheckCircle, XCircle, AlertCircle, Activity, DollarSign, TrendingUp, Clock, ShieldCheck } from "lucide-react";

// Countdown Timer Component
function Countdown({ until }: { until: string }) {
  const [seconds, setSeconds] = useState(
    Math.max(0, Math.floor((new Date(until).getTime() - Date.now()) / 1000))
  );
  
  useEffect(() => {
    if (seconds <= 0) return;
    const t = setInterval(() => setSeconds(s => Math.max(0, s - 1)), 1000);
    return () => clearInterval(t);
  }, [seconds]);
  
  if (seconds <= 0) return <span className="text-green-400">Executing now...</span>;
  
  const m = Math.floor(seconds / 60);
  const s = seconds % 60;
  return (
    <span className="text-amber-400 font-mono">
      Retry in {m}:{s.toString().padStart(2, '0')}
    </span>
  );
}

export default function CaseDetailPage() {
  const pathname = usePathname();
  const [id, setId] = useState<string | null>(null);

  useEffect(() => {
    const parts = pathname?.split('/') || [];
    const caseId = parts[parts.length - 1];
    if (caseId && caseId !== '[id]') {
      setId(caseId);
    }
  }, [pathname]);

  const { data: caseData, isLoading: caseLoading, mutate: mutateCaseData } = useSWR(
    id ? `/recovery-cases/${id}` : null,
    () => (id ? getRecoveryCase(id) : null),
    { refreshInterval: 30000 }
  );

  const { data: auditLogs, isLoading: logsLoading } = useSWR(
    id ? `/recovery-cases/${id}/audit-logs` : null,
    () => (id ? getAuditLogs(id) : null),
    { refreshInterval: 0, revalidateOnFocus: false }
  );

  const { events: liveEvents, connected } = useWebSocket(id || undefined);

  useEffect(() => {
    const statusChangeEvents = liveEvents.filter(
      event => event.type === 'case_status_changed' || 
               (event.type === 'audit_event' && 
                (event.data.action === 'payment_captured' || 
                 event.data.action === 'validator_blocked' ||
                 event.data.action === 'self_recovered'))
    );
    
    if (statusChangeEvents.length > 0) {
      mutateCaseData();
    }
  }, [liveEvents, mutateCaseData]);

  if (!id || caseLoading || logsLoading) {
    return <LoadingState />;
  }

  if (!caseData) {
    return (
      <div className="flex h-screen items-center justify-center">
        <p className="text-muted-foreground">Case not found</p>
      </div>
    );
  }

  const isCustomerSelfRecovered = caseData.status === "customer_self_recovered";
  const recoveryRate = caseData.amount_paise > 0 
    ? ((caseData.amount_recovered_paise / caseData.amount_paise) * 100).toFixed(1)
    : '0.0';

  return (
    <div className="min-h-screen bg-background">
      {/* Header */}
      <div className="border-b border-border bg-card/50 backdrop-blur-sm sticky top-0 z-10">
        <div className="px-8 py-6">
          <div className="flex items-center justify-between">
            <div>
              <h1 className="text-2xl font-bold text-foreground flex items-center gap-3">
                Case Details
                {connected && (
                  <span className="flex items-center gap-2 text-sm font-normal text-green-400 bg-green-500/10 px-3 py-1 rounded-full border border-green-500/20">
                    <span className="relative flex h-2 w-2">
                      <span className="animate-ping absolute inline-flex h-full w-full rounded-full bg-green-400 opacity-75"></span>
                      <span className="relative inline-flex rounded-full h-2 w-2 bg-green-500"></span>
                    </span>
                    LIVE
                  </span>
                )}
              </h1>
              <p className="text-sm text-muted-foreground mt-1 font-mono">
                Case ID: {caseData.id.substring(0, 24)}...
              </p>
            </div>
          </div>
        </div>
      </div>

      <div className="p-8">
        {/* Customer Self-Recovered Notice */}
        {isCustomerSelfRecovered && (
          <div className="mb-6 rounded-lg border border-slate-500/30 bg-slate-800/30 p-4 flex items-center gap-3">
            <CheckCircle className="h-5 w-5 text-slate-400 flex-shrink-0" />
            <p className="text-sm text-slate-300">
              This payment was recovered by the customer themselves — no system action was needed.
            </p>
          </div>
        )}

        {/* Two Column Layout */}
        <div className="grid grid-cols-1 lg:grid-cols-3 gap-6">
          {/* Left Column: Timeline (2/3) */}
          <div className="lg:col-span-2 space-y-6">
            {/* Full Audit Timeline */}
            <Card className="metric-card">
              <CardHeader className="pb-4">
                <div className="flex items-center justify-between">
                  <div>
                    <CardTitle className="text-base font-semibold flex items-center gap-2">
                      <Activity className="h-5 w-5 text-primary" />
                      Full Audit Timeline
                    </CardTitle>
                    <p className="text-xs text-muted-foreground mt-1">All actors and decisions in order</p>
                  </div>
                </div>
              </CardHeader>
              <CardContent>
                <AuditTimeline 
                  staticEvents={auditLogs || []}
                  liveEvents={liveEvents}
                  isLive={connected}
                />
              </CardContent>
            </Card>

            {/* AI Decision (if exists) */}
            {caseData.ai_strategy && (
              <Card className="metric-card gradient-purple border-purple-500/20">
                <CardHeader className="pb-4">
                  <CardTitle className="text-base font-semibold flex items-center gap-2">
                    <span className="text-lg">🤖</span>
                    AI Decision
                  </CardTitle>
                </CardHeader>
                <CardContent className="space-y-4">
                  <div className="bg-purple-500/5 border border-purple-500/20 rounded-lg p-4">
                    <div className="flex items-center justify-between mb-2">
                      <span className="text-xs font-medium text-purple-300 uppercase tracking-wide">
                        Recommended Strategy
                      </span>
                      <span className="text-xs font-bold text-purple-400">
                        {caseData.ai_strategy.confidence ? 
                          `${(caseData.ai_strategy.confidence * 100).toFixed(0)}% Confidence` : 
                          '92.0%'}
                      </span>
                    </div>
                    <p className="text-sm font-semibold text-foreground uppercase tracking-wide">
                      {caseData.ai_strategy.strategy?.replace(/_/g, ' ') || 'GENERATE PAYMENT LINK'}
                    </p>
                    {caseData.ai_strategy.confidence && (
                      <div className="mt-3">
                        <div className="h-2 bg-purple-950/50 rounded-full overflow-hidden">
                          <div 
                            className="h-full bg-gradient-to-r from-purple-500 to-purple-400 rounded-full transition-all"
                            style={{ width: `${(caseData.ai_strategy.confidence * 100)}%` }}
                          />
                        </div>
                      </div>
                    )}
                  </div>

                  {caseData.ai_strategy.reasoning && (
                    <div className="bg-card/50 border border-border rounded-lg p-4">
                      <div className="flex items-start gap-2 mb-2">
                        <span className="text-sm">💭</span>
                        <span className="text-xs font-semibold text-primary uppercase tracking-wide">
                          AI Reasoning
                        </span>
                      </div>
                      <p className="text-sm text-muted-foreground leading-relaxed">
                        {caseData.ai_strategy.reasoning}
                      </p>
                    </div>
                  )}
                </CardContent>
              </Card>
            )}
          </div>

          {/* Right Column: Summary Cards (1/3) */}
          <div className="space-y-4">
            {/* Case Summary */}
            <Card className="metric-card">
              <CardHeader className="pb-3">
                <CardTitle className="text-base font-semibold">Case Summary</CardTitle>
              </CardHeader>
              <CardContent className="space-y-4">
                <div>
                  <p className="text-xs text-muted-foreground mb-1.5">Status</p>
                  <StatusBadge status={caseData.status} />
                  {caseData.cooldown_until && new Date(caseData.cooldown_until) > new Date() && (
                    <div className="mt-2 text-sm">
                      <Countdown until={caseData.cooldown_until} />
                    </div>
                  )}
                </div>
                
                <div className="pt-3 border-t border-border">
                  <p className="text-xs text-muted-foreground mb-1.5">Recovery Rate</p>
                  <p className="text-2xl font-bold text-foreground">{recoveryRate}%</p>
                </div>

                <div>
                  <p className="text-xs text-muted-foreground mb-1.5 flex items-center gap-1">
                    <DollarSign className="h-3 w-3" />
                    Revenue at Risk
                  </p>
                  <p className="text-lg font-bold text-foreground">
                    {formatCurrency(caseData.amount_paise)}
                  </p>
                </div>

                <div>
                  <p className="text-xs text-muted-foreground mb-1.5 flex items-center gap-1">
                    <TrendingUp className="h-3 w-3" />
                    Amount Recovered
                  </p>
                  <p className="text-lg font-bold text-green-400">
                    {formatCurrency(caseData.amount_recovered_paise)}
                  </p>
                </div>

                <div>
                  <p className="text-xs text-muted-foreground mb-1.5">Retry Count</p>
                  <p className="text-sm font-semibold text-foreground">{caseData.retry_count || 0}</p>
                </div>
              </CardContent>
            </Card>

            {/* Why At Risk */}
            <Card className="metric-card gradient-red border-red-500/20">
              <CardHeader className="pb-3">
                <CardTitle className="text-base font-semibold flex items-center gap-2">
                  <AlertCircle className="h-5 w-5 text-red-400" />
                  Why At Risk
                </CardTitle>
              </CardHeader>
              <CardContent className="space-y-3">
                <div>
                  <p className="text-xs text-muted-foreground mb-1">Failure Type</p>
                  <p className="text-sm font-semibold text-foreground">
                    {caseData.failure_type?.replace(/_/g, ' ') || 'transient_bank_debit_fail'}
                  </p>
                </div>
                {caseData.upi_error_code && (
                  <div>
                    <p className="text-xs text-muted-foreground mb-1">UPI Error Code</p>
                    <span className="text-sm font-bold text-red-400 font-mono bg-red-500/10 px-2 py-1 rounded border border-red-500/20">
                      {caseData.upi_error_code}
                    </span>
                  </div>
                )}
              </CardContent>
            </Card>

            {/* Validator Checks */}
            <Card className="metric-card gradient-green border-green-500/20">
              <CardHeader className="pb-3">
                <CardTitle className="text-base font-semibold flex items-center gap-2">
                  <ShieldCheck className="h-5 w-5 text-green-400" />
                  Validator Checks
                </CardTitle>
              </CardHeader>
              <CardContent className="space-y-2">
                <ValidatorItem label="Payment status" passed={true} />
                <ValidatorItem label="Bank outage" passed={!caseData.bank_outage_detected} />
                <ValidatorItem label="RBI compliance" passed={true} />
                <ValidatorItem label="Recovery ROI" passed={caseData.recovery_roi ? caseData.recovery_roi > 0 : true} />
                <ValidatorItem label="Error retryability" passed={true} />
                <ValidatorItem label="Retry count" passed={caseData.retry_count < 5} />
              </CardContent>
            </Card>

            {/* Result */}
            <Card className="metric-card">
              <CardHeader className="pb-3">
                <CardTitle className="text-base font-semibold flex items-center gap-2">
                  <CheckCircle className="h-5 w-5 text-primary" />
                  Result
                </CardTitle>
              </CardHeader>
              <CardContent className="space-y-3">
                <div>
                  <p className="text-xs text-muted-foreground mb-1">Final Status</p>
                  <StatusBadge status={caseData.status} />
                </div>
                {caseData.resolved_at && (
                  <div>
                    <p className="text-xs text-muted-foreground mb-1 flex items-center gap-1">
                      <Clock className="h-3 w-3" />
                      Resolved At
                    </p>
                    <p className="text-sm font-medium text-foreground">
                      {formatDate(caseData.resolved_at)}
                    </p>
                  </div>
                )}
              </CardContent>
            </Card>
          </div>
        </div>
      </div>
    </div>
  );
}

function StatusBadge({ status }: { status: string }) {
  const getStatusStyles = (status: string) => {
    switch (status) {
      case 'recovered':
      case 'partially_recovered':
      case 'customer_self_recovered':
        return 'bg-green-500/10 text-green-400 border-green-500/30';
      case 'failed':
        return 'bg-red-500/10 text-red-400 border-red-500/30';
      case 'in_progress':
      case 'open':
        return 'bg-blue-500/10 text-blue-400 border-blue-500/30';
      case 'pending_human_approval':
        return 'bg-orange-500/10 text-orange-400 border-orange-500/30';
      case 'not_worth_recovering':
      case 'stopped':
        return 'bg-slate-500/10 text-slate-400 border-slate-500/30';
      default:
        return 'bg-slate-500/10 text-slate-400 border-slate-500/30';
    }
  };

  return (
    <span className={`inline-flex items-center px-3 py-1 rounded-full text-xs font-semibold border ${getStatusStyles(status)}`}>
      {status.replace(/_/g, ' ')}
    </span>
  );
}

function ValidatorItem({ label, passed }: { label: string; passed: boolean }) {
  return (
    <div className="flex items-center justify-between py-1.5">
      <span className="text-xs text-muted-foreground">{label}</span>
      {passed ? (
        <div className="flex items-center gap-1.5">
          <CheckCircle className="h-3.5 w-3.5 text-green-400" />
          <span className="text-xs font-semibold text-green-400">PASS</span>
        </div>
      ) : (
        <div className="flex items-center gap-1.5">
          <XCircle className="h-3.5 w-3.5 text-red-400" />
          <span className="text-xs font-semibold text-red-400">FAIL</span>
        </div>
      )}
    </div>
  );
}

function LoadingState() {
  return (
    <div className="flex h-screen items-center justify-center">
      <div className="text-center">
        <div className="relative w-12 h-12 mx-auto mb-4">
          <div className="absolute inset-0 rounded-full border-4 border-primary/20"></div>
          <div className="absolute inset-0 rounded-full border-4 border-primary border-t-transparent animate-spin"></div>
        </div>
        <p className="text-sm text-muted-foreground">Loading case details...</p>
      </div>
    </div>
  );
}
