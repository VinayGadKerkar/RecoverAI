"use client";

import { useEffect, useState, useRef } from "react";
import useSWR from "swr";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import { Badge } from "@/components/ui/badge";
import { getOverview, getRecoveryRate, getRevenue, getRecentCases } from "@/lib/api";
import { formatCurrency, formatPercent, formatDate } from "@/lib/utils";
import { OverviewResponse } from "@/lib/types";
import {
  AreaChart,
  Area,
  BarChart,
  Bar,
  XAxis,
  YAxis,
  CartesianGrid,
  Tooltip,
  Legend,
  ResponsiveContainer,
} from "recharts";
import { format } from "date-fns";
import { useWebSocket, type WSMessage } from "@/hooks/useWebSocket";
import { TrendingDown, TrendingUp, Activity, AlertTriangle, Users, XCircle, CheckCircle } from "lucide-react";

export default function DashboardPage() {
  // Reduced polling - WebSocket handles real-time updates
  const { data: overview, mutate: mutateOverview } = useSWR("/analytics/overview", getOverview, {
    refreshInterval: 30000,
  });

  const { data: recoveryRateData } = useSWR(
    "/analytics/recovery-rate",
    () => getRecoveryRate("7d", "failure_type"),
    { refreshInterval: 60000 }
  );

  const { data: revenueData } = useSWR(
    "/analytics/revenue",
    () => getRevenue("24h", "hour"),
    { refreshInterval: 60000 }
  );

  const { data: recentCases, mutate: mutateRecentCases } = useSWR(
    "/recent-cases",
    () => getRecentCases(10),
    { refreshInterval: 30000 }
  );

  const [localOverview, setLocalOverview] = useState<OverviewResponse | undefined>(overview);
  const [flashCard, setFlashCard] = useState<string | null>(null);
  const prevRecoveredRef = useRef<number>(0);

  // WebSocket for real-time updates (no case filter = all events)
  const { events: liveEvents, connected, metrics } = useWebSocket();

  // Update local overview immediately when WebSocket sends metrics
  useEffect(() => {
    if (!metrics) return;
    const base = localOverview || overview;
    if (!base) return;

    setLocalOverview({
      ...base,
      ...(metrics.revenue_at_risk !== undefined && { revenue_at_risk_paise: metrics.revenue_at_risk }),
      ...(metrics.revenue_recovered !== undefined && { recovered_revenue_paise: metrics.revenue_recovered }),
      ...(metrics.recovery_rate !== undefined && { recovery_rate_percent: metrics.recovery_rate }),
      ...(metrics.total_cases !== undefined && { total_failed_payments: metrics.total_cases }),
      ...(metrics.pending_human_approval !== undefined && { pending_human_approval_count: metrics.pending_human_approval }),
      ...(metrics.customer_self_recovered !== undefined && { customer_self_recovered_count: metrics.customer_self_recovered }),
      ...(metrics.not_worth_recovering !== undefined && { not_worth_recovering_count: metrics.not_worth_recovering }),
    });

    // Only flash + trigger a real re-fetch when recovered amount increases
    if (metrics.revenue_recovered && metrics.revenue_recovered > prevRecoveredRef.current) {
      setFlashCard('recovered');
      setTimeout(() => setFlashCard(null), 500);
      prevRecoveredRef.current = metrics.revenue_recovered;
      mutateOverview();  // one real fetch per actual recovery event
    }
  // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [metrics]); // ← only re-run when metrics changes, not when localOverview/overview change

  useEffect(() => {
    if (overview && !localOverview) {
      setLocalOverview(overview);
      prevRecoveredRef.current = overview.recovered_revenue_paise || 0;
    }
  }, [overview, localOverview]);

  useEffect(() => {
    if (liveEvents.length > 0) {
      mutateRecentCases();
    }
  }, [liveEvents.length, mutateRecentCases]);

  const displayOverview = localOverview || overview;

  if (!displayOverview) {
    return <LoadingState />;
  }

  return (
    <div className="min-h-screen bg-background">
      {/* Header */}
      <div className="border-b border-border bg-card/50 backdrop-blur-sm sticky top-0 z-10">
        <div className="px-8 py-6">
          <div className="flex items-center justify-between">
            <div>
              <h1 className="text-2xl font-bold text-foreground flex items-center gap-3">
                Recovery Overview
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
              <p className="text-sm text-muted-foreground mt-1">Real-time payment recovery dashboard</p>
            </div>
          </div>
        </div>
      </div>

      <div className="p-8 space-y-6">
        {/* Top Metric Cards */}
        <div className="grid grid-cols-1 gap-4 md:grid-cols-2 lg:grid-cols-3 xl:grid-cols-6">
          {/* Revenue at Risk */}
          <Card className={`metric-card gradient-red border-red-500/20 ${flashCard === 'at_risk' ? 'animate-flashGreen' : ''}`}>
            <CardContent className="p-6">
              <div className="flex items-center justify-between mb-4">
                <div className="p-2 bg-red-500/10 rounded-lg">
                  <TrendingDown className="h-5 w-5 text-red-400" />
                </div>
                <span className="text-xs font-medium text-red-400">
                  {displayOverview.active_cases} active
                </span>
              </div>
              <div className="space-y-1">
                <p className="text-xs font-medium text-muted-foreground">Revenue at Risk</p>
                <p className="text-2xl font-bold text-red-400">
                  {formatCurrency(displayOverview.revenue_at_risk_paise)}
                </p>
              </div>
            </CardContent>
          </Card>

          {/* Recovered Revenue */}
          <Card className={`metric-card gradient-green border-green-500/20 ${flashCard === 'recovered' ? 'animate-flashGreen' : ''}`}>
            <CardContent className="p-6">
              <div className="flex items-center justify-between mb-4">
                <div className="p-2 bg-green-500/10 rounded-lg">
                  <TrendingUp className="h-5 w-5 text-green-400" />
                </div>
                <span className="text-xs font-medium text-green-400">
                  {displayOverview.total_recovered_payments} recovered
                </span>
              </div>
              <div className="space-y-1">
                <p className="text-xs font-medium text-muted-foreground">Recovered Revenue</p>
                <p className={`text-2xl font-bold text-green-400 ${flashCard === 'recovered' ? 'animate-countUp' : ''}`}>
                  {formatCurrency(displayOverview.recovered_revenue_paise)}
                </p>
              </div>
            </CardContent>
          </Card>

          {/* Recovery Rate */}
          <Card className="metric-card gradient-blue border-blue-500/20">
            <CardContent className="p-6">
              <div className="flex items-center justify-between mb-4">
                <div className="p-2 bg-blue-500/10 rounded-lg">
                  <Activity className="h-5 w-5 text-blue-400" />
                </div>
                <span className="text-xs font-medium text-muted-foreground">
                  {formatPercent(displayOverview.partial_recovery_rate_percent)} partial
                </span>
              </div>
              <div className="space-y-1">
                <p className="text-xs font-medium text-muted-foreground">Recovery Rate</p>
                <p className="text-2xl font-bold text-foreground">
                  {formatPercent(displayOverview.recovery_rate_percent)}
                </p>
              </div>
            </CardContent>
          </Card>

          {/* Customer Self-Recovered */}
          <Card className="metric-card border-slate-500/20">
            <CardContent className="p-6">
              <div className="flex items-center justify-between mb-4">
                <div className="p-2 bg-slate-500/10 rounded-lg">
                  <Users className="h-5 w-5 text-slate-400" />
                </div>
              </div>
              <div className="space-y-1">
                <p className="text-xs font-medium text-muted-foreground">Customer Self-Recovered</p>
                <p className="text-2xl font-bold text-slate-300">
                  {displayOverview.customer_self_recovered_count}
                </p>
              </div>
            </CardContent>
          </Card>

          {/* Pending Human Approval */}
          <Card className="metric-card gradient-orange border-orange-500/20">
            <CardContent className="p-6">
              <div className="flex items-center justify-between mb-4">
                <div className="p-2 bg-orange-500/10 rounded-lg">
                  <AlertTriangle className="h-5 w-5 text-orange-400" />
                </div>
              </div>
              <div className="space-y-1">
                <p className="text-xs font-medium text-muted-foreground">Pending Approval</p>
                <p className="text-2xl font-bold text-orange-400">
                  {displayOverview.pending_human_approval_count}
                </p>
              </div>
            </CardContent>
          </Card>

          {/* Not Worth Recovering */}
          <Card className="metric-card border-slate-500/20">
            <CardContent className="p-6">
              <div className="flex items-center justify-between mb-4">
                <div className="p-2 bg-slate-500/10 rounded-lg">
                  <XCircle className="h-5 w-5 text-slate-400" />
                </div>
              </div>
              <div className="space-y-1">
                <p className="text-xs font-medium text-muted-foreground">Not Worth Recovering</p>
                <p className="text-2xl font-bold text-slate-400">
                  {displayOverview.not_worth_recovering_count}
                </p>
              </div>
            </CardContent>
          </Card>
        </div>

        {/* Charts Row */}
        <div className="grid grid-cols-1 gap-6 lg:grid-cols-2">
          {/* Area Chart: Revenue Over Time */}
          <Card className="metric-card">
            <CardHeader className="pb-4">
              <CardTitle className="text-base font-semibold">Revenue Over Time (24h)</CardTitle>
              <p className="text-xs text-muted-foreground">Tracking recovered vs at-risk revenue</p>
            </CardHeader>
            <CardContent>
              <ResponsiveContainer width="100%" height={280}>
                <AreaChart data={revenueData || []}>
                  <defs>
                    <linearGradient id="colorRecovered" x1="0" y1="0" x2="0" y2="1">
                      <stop offset="5%" stopColor="#10B981" stopOpacity={0.3}/>
                      <stop offset="95%" stopColor="#10B981" stopOpacity={0}/>
                    </linearGradient>
                    <linearGradient id="colorAtRisk" x1="0" y1="0" x2="0" y2="1">
                      <stop offset="5%" stopColor="#EF4444" stopOpacity={0.3}/>
                      <stop offset="95%" stopColor="#EF4444" stopOpacity={0}/>
                    </linearGradient>
                  </defs>
                  <CartesianGrid strokeDasharray="3 3" stroke="#1E293B" opacity={0.5} />
                  <XAxis
                    dataKey="timestamp"
                    tickFormatter={(val) => format(new Date(val), "HH:mm")}
                    stroke="#64748B"
                    style={{ fontSize: '12px' }}
                  />
                  <YAxis
                    tickFormatter={(val) => `₹${(val / 100).toFixed(0)}`}
                    stroke="#64748B"
                    style={{ fontSize: '12px' }}
                  />
                  <Tooltip
                    contentStyle={{
                      backgroundColor: "#0F172A",
                      border: "1px solid #1E293B",
                      borderRadius: "8px",
                      fontSize: '12px',
                    }}
                    formatter={(value: number) => formatCurrency(value)}
                    labelFormatter={(label) => format(new Date(label), "MMM d, HH:mm")}
                  />
                  <Legend 
                    wrapperStyle={{ fontSize: '12px' }}
                  />
                  <Area
                    type="monotone"
                    dataKey="recovered_paise"
                    stroke="#10B981"
                    fill="url(#colorRecovered)"
                    name="Recovered"
                    strokeWidth={2}
                  />
                  <Area
                    type="monotone"
                    dataKey="at_risk_paise"
                    stroke="#EF4444"
                    fill="url(#colorAtRisk)"
                    name="At Risk"
                    strokeWidth={2}
                  />
                </AreaChart>
              </ResponsiveContainer>
            </CardContent>
          </Card>

          {/* Bar Chart: Recovery Rate by Failure Type */}
          <Card className="metric-card">
            <CardHeader className="pb-4">
              <CardTitle className="text-base font-semibold">Recovery Rate by Failure Type</CardTitle>
              <p className="text-xs text-muted-foreground">Performance across different error categories</p>
            </CardHeader>
            <CardContent>
              <ResponsiveContainer width="100%" height={280}>
                <BarChart data={recoveryRateData || []}>
                  <CartesianGrid strokeDasharray="3 3" stroke="#1E293B" opacity={0.5} />
                  <XAxis 
                    dataKey="label" 
                    stroke="#64748B" 
                    style={{ fontSize: '11px' }}
                    angle={-15}
                    textAnchor="end"
                    height={60}
                  />
                  <YAxis 
                    stroke="#64748B" 
                    style={{ fontSize: '12px' }}
                  />
                  <Tooltip
                    contentStyle={{
                      backgroundColor: "#0F172A",
                      border: "1px solid #1E293B",
                      borderRadius: "8px",
                      fontSize: '12px',
                    }}
                    formatter={(value: number, name: string) => {
                      if (name === "recovery_rate") return `${value.toFixed(1)}%`;
                      return value;
                    }}
                  />
                  <Legend wrapperStyle={{ fontSize: '12px' }} />
                  <Bar dataKey="total" fill="#475569" name="Total" radius={[4, 4, 0, 0]} />
                  <Bar dataKey="recovered" fill="#10B981" name="Recovered" radius={[4, 4, 0, 0]} />
                </BarChart>
              </ResponsiveContainer>
            </CardContent>
          </Card>
        </div>

        {/* Recent Cases & Live Activity */}
        <div className="grid grid-cols-1 gap-6 lg:grid-cols-3">
          {/* Recent Cases (2/3 width) */}
          <Card className="metric-card lg:col-span-2">
            <CardHeader className="pb-4">
              <div className="flex items-center justify-between">
                <div>
                  <CardTitle className="text-base font-semibold">Recent Cases</CardTitle>
                  <p className="text-xs text-muted-foreground mt-1">Last 10 recovery cases</p>
                </div>
              </div>
            </CardHeader>
            <CardContent>
              <div className="space-y-3">
                {recentCases && recentCases.length > 0 ? (
                  recentCases.map((c) => (
                    <div
                      key={c.id}
                      className="flex items-center justify-between p-3 rounded-lg bg-muted/30 border border-border hover:border-primary/30 transition-colors"
                    >
                      <div className="flex-1 min-w-0">
                        <div className="flex items-center gap-2 mb-1">
                          <StatusBadge status={c.status} />
                          <span className="text-sm font-semibold text-foreground">
                            {formatCurrency(c.amount_paise)}
                          </span>
                          {c.upi_error_code && (
                            <span className="text-xs text-muted-foreground font-mono bg-muted px-2 py-0.5 rounded">
                              {c.upi_error_code}
                            </span>
                          )}
                        </div>
                        <p className="text-xs text-muted-foreground truncate">
                          {formatDate(c.created_at)} • {c.customer_email || c.customer_id || "Unknown customer"}
                        </p>
                      </div>
                      {c.validator_skip_reason && (
                        <div className="ml-4 text-xs text-orange-400 bg-orange-500/10 px-2 py-1 rounded border border-orange-500/20">
                          {c.validator_skip_reason}
                        </div>
                      )}
                    </div>
                  ))
                ) : (
                  <p className="text-center text-sm text-muted-foreground py-8">No recent cases</p>
                )}
              </div>
            </CardContent>
          </Card>

          {/* Live Activity Feed (1/3 width) */}
          <Card className="metric-card">
            <CardHeader className="pb-4">
              <div className="flex items-center justify-between">
                <div>
                  <CardTitle className="text-base font-semibold">Live Activity</CardTitle>
                  <p className="text-xs text-muted-foreground mt-1">Real-time system events</p>
                </div>
                {connected && (
                  <div className="flex items-center gap-1.5">
                    <div className="w-1.5 h-1.5 bg-green-500 rounded-full animate-pulse" />
                    <span className="text-xs text-green-400 font-medium">LIVE</span>
                  </div>
                )}
              </div>
            </CardHeader>
            <CardContent>
              <div className="space-y-2 max-h-[400px] overflow-y-auto pr-2">
                {liveEvents.slice(0, 10).map((event, idx) => (
                  <LiveActivityItem key={`${event.timestamp}-${idx}`} event={event} />
                ))}
                {liveEvents.length === 0 && (
                  <p className="text-center text-sm text-muted-foreground py-8">
                    Waiting for activity...
                  </p>
                )}
              </div>
            </CardContent>
          </Card>
        </div>
      </div>
    </div>
  );
}

function StatusBadge({ status }: { status: string }) {
  const variants: Record<string, string> = {
    recovered: "status-success",
    partially_recovered: "status-success",
    customer_self_recovered: "status-success",
    failed: "status-danger",
    in_progress: "status-info",
    pending_human_approval: "status-warning",
    not_worth_recovering: "status-muted",
    stopped: "status-muted",
  };

  const label = status.replace(/_/g, " ");
  const className = variants[status] || "status-muted";

  return (
    <span className={`text-xs font-medium px-2 py-0.5 rounded ${className}`}>
      {label}
    </span>
  );
}

function LiveActivityItem({ event }: { event: WSMessage }) {
  const actorIcons: Record<string, string> = {
    system: '⚙️',
    risk_engine: '📊',
    validator: '🛡️',
    ai_agent: '🤖',
    policy_engine: '⚖️',
    execution_worker: '⚡',
    human: '👤',
    customer_self: '👤',
  };

  const actorColors: Record<string, string> = {
    system: '#6B7280',
    risk_engine: '#3B82F6',
    validator: '#10B981',
    ai_agent: '#8B5CF6',
    policy_engine: '#F59E0B',
    execution_worker: '#F97316',
    human: '#6366F1',
    customer_self: '#10B981',
  };

  const actor = (event.data.actor as string) || 'system';
  const action = (event.data.action as string) || 'activity';
  const icon = actorIcons[actor] || '⚙️';
  const color = actorColors[actor] || '#6B7280';
  
  const time = new Date(event.timestamp).toLocaleTimeString('en-US', {
    hour: '2-digit',
    minute: '2-digit',
  });

  let message = action.replace(/_/g, ' ');
  if (event.data.metadata) {
    const metadata = event.data.metadata as Record<string, unknown>;
    if (action === 'payment_captured' && metadata.amount_paise) {
      const amount = (metadata.amount_paise as number) / 100;
      message = `✅ ₹${amount.toFixed(2)} recovered`;
    } else if (action === 'risk_scored' && metadata.priority) {
      message = `Risk: ${metadata.priority}`;
    } else if (action === 'ai_recommendation' && metadata.strategy) {
      message = `AI: ${(metadata.strategy as string).replace(/_/g, ' ')}`;
    }
  }

  return (
    <div className="flex items-start gap-2 p-2 rounded-lg bg-muted/20 animate-slideIn">
      <span className="text-sm mt-0.5">{icon}</span>
      <div className="flex-1 min-w-0">
        <div className="flex items-center gap-2 mb-0.5">
          <span className="text-xs font-mono text-muted-foreground">{time}</span>
          <span className="text-xs font-medium truncate" style={{ color }}>
            {actor.replace(/_/g, ' ')}
          </span>
        </div>
        <p className="text-xs text-gray-300 truncate">{message}</p>
      </div>
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
        <p className="text-sm text-muted-foreground">Loading dashboard...</p>
      </div>
    </div>
  );
}
