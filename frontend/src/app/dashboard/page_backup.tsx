"use client";

import { useEffect, useState, useRef } from "react";
import useSWR from "swr";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import { Badge } from "@/components/ui/badge";
import { getOverview, getRecoveryRate, getRevenue, getRecentCases } from "@/lib/api";
import { formatCurrency, formatPercent, formatDate } from "@/lib/utils";
import { OverviewResponse } from "@/lib/types";
import {
  LineChart,
  Line,
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

export default function DashboardPage() {
  // Reduced polling - WebSocket handles real-time updates
  const { data: overview, mutate: mutateOverview } = useSWR("/analytics/overview", getOverview, {
    refreshInterval: 30000, // Reduced from 5s to 30s
  });

  const { data: recoveryRateData } = useSWR(
    "/analytics/recovery-rate",
    () => getRecoveryRate("7d", "failure_type"),
    { refreshInterval: 60000 } // Reduced from 30s to 60s
  );

  const { data: revenueData } = useSWR(
    "/analytics/revenue",
    () => getRevenue("24h", "hour"),
    { refreshInterval: 60000 } // Reduced from 30s to 60s
  );

  const { data: recentCases, mutate: mutateRecentCases } = useSWR(
    "/recent-cases",
    () => getRecentCases(10),
    { refreshInterval: 30000 } // Reduced from 5s to 30s
  );

  const [localOverview, setLocalOverview] = useState<OverviewResponse | undefined>(overview);
  const [flashCard, setFlashCard] = useState<string | null>(null);
  const prevRecoveredRef = useRef<number>(0);

  // WebSocket for real-time updates (no case filter = all events)
  const { events: liveEvents, connected, metrics } = useWebSocket();

  // Update local overview immediately when WebSocket sends metrics
  useEffect(() => {
    if (metrics && (localOverview || overview)) {
      const base = localOverview || overview!;
      setLocalOverview({
        ...base,
        // Only update fields that are present in metrics
        ...(metrics.revenue_at_risk !== undefined && { revenue_at_risk_paise: metrics.revenue_at_risk }),
        ...(metrics.revenue_recovered !== undefined && { recovered_revenue_paise: metrics.revenue_recovered }),
        ...(metrics.recovery_rate !== undefined && { recovery_rate_percent: metrics.recovery_rate }),
        ...(metrics.total_cases !== undefined && { total_failed_payments: metrics.total_cases }),
        ...(metrics.pending_human_approval !== undefined && { pending_human_approval_count: metrics.pending_human_approval }),
        ...(metrics.customer_self_recovered !== undefined && { customer_self_recovered_count: metrics.customer_self_recovered }),
        ...(metrics.not_worth_recovering !== undefined && { not_worth_recovering_count: metrics.not_worth_recovering }),
      });
      
      // Check if recovered revenue increased
      if (metrics.revenue_recovered && 
          metrics.revenue_recovered > (prevRecoveredRef.current || 0)) {
        // Flash the recovered revenue card
        setFlashCard('recovered');
        setTimeout(() => setFlashCard(null), 500);
        prevRecoveredRef.current = metrics.revenue_recovered;
      }
      
      // Also update the SWR cache
      mutateOverview();
    }
  }, [metrics, overview, localOverview, mutateOverview]);

  // Sync localOverview with SWR data
  useEffect(() => {
    if (overview && !localOverview) {
      setLocalOverview(overview);
      prevRecoveredRef.current = overview.recovered_revenue_paise || 0;
    }
  }, [overview, localOverview]);

  // Refresh recent cases when events arrive
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
    <div className="p-8 space-y-8">
      {/* Header with WebSocket status */}
      <div className="flex items-center justify-between">
        <div>
          <h2 className="text-3xl font-bold text-foreground">Recovery Overview</h2>
          <p className="text-muted-foreground">Real-time payment recovery dashboard</p>
        </div>
        
        {/* WebSocket Status Indicator */}
        <div className="flex items-center gap-2">
          {connected ? (
            <>
              <div className="w-2 h-2 bg-green-500 rounded-full animate-pulse" />
              <span className="text-sm text-green-400 font-medium">LIVE</span>
            </>
          ) : (
            <>
              <div className="w-2 h-2 bg-gray-500 rounded-full" />
              <span className="text-sm text-gray-500">OFFLINE</span>
            </>
          )}
        </div>
      </div>

      {/* Top Metric Cards (6 cards) */}
      <div className="grid grid-cols-1 gap-6 md:grid-cols-2 lg:grid-cols-3">
        {/* Revenue at Risk */}
        <Card className={flashCard === 'at_risk' ? 'animate-flashGreen' : ''}>
          <CardHeader className="flex flex-row items-center justify-between space-y-0 pb-2">
            <CardTitle className="text-sm font-medium">Revenue at Risk</CardTitle>
            <svg
              xmlns="http://www.w3.org/2000/svg"
              viewBox="0 0 24 24"
              fill="none"
              stroke="currentColor"
              strokeLinecap="round"
              strokeLinejoin="round"
              strokeWidth="2"
              className="h-4 w-4 text-red-500"
            >
              <path d="M12 2v20M2 12h20" />
            </svg>
          </CardHeader>
          <CardContent>
            <div className="text-2xl font-bold text-red-400">
              {formatCurrency(displayOverview.revenue_at_risk_paise)}
            </div>
            <p className="text-xs text-muted-foreground">
              {displayOverview.active_cases} active cases
            </p>
          </CardContent>
        </Card>

        {/* Recovered Revenue */}
        <Card className={flashCard === 'recovered' ? 'animate-flashGreen' : ''}>
          <CardHeader className="flex flex-row items-center justify-between space-y-0 pb-2">
            <CardTitle className="text-sm font-medium">Recovered Revenue</CardTitle>
            <svg
              xmlns="http://www.w3.org/2000/svg"
              viewBox="0 0 24 24"
              fill="none"
              stroke="currentColor"
              strokeLinecap="round"
              strokeLinejoin="round"
              strokeWidth="2"
              className="h-4 w-4 text-green-500"
            >
              <polyline points="22 12 18 12 15 21 9 3 6 12 2 12" />
            </svg>
          </CardHeader>
          <CardContent>
            <div className={`text-2xl font-bold text-green-400 ${flashCard === 'recovered' ? 'animate-countUp' : ''}`}>
              {formatCurrency(displayOverview.recovered_revenue_paise)}
            </div>
            <p className="text-xs text-muted-foreground">
              {displayOverview.total_recovered_payments} payments recovered
            </p>
          </CardContent>
        </Card>

        {/* Recovery Rate */}
        <Card>
          <CardHeader className="flex flex-row items-center justify-between space-y-0 pb-2">
            <CardTitle className="text-sm font-medium">Recovery Rate</CardTitle>
            <svg
              xmlns="http://www.w3.org/2000/svg"
              viewBox="0 0 24 24"
              fill="none"
              stroke="currentColor"
              strokeLinecap="round"
              strokeLinejoin="round"
              strokeWidth="2"
              className="h-4 w-4 text-blue-500"
            >
              <path d="M22 11.08V12a10 10 0 1 1-5.93-9.14" />
              <polyline points="22 4 12 14.01 9 11.01" />
            </svg>
          </CardHeader>
          <CardContent>
            <div className="text-2xl font-bold text-foreground">
              {formatPercent(displayOverview.recovery_rate_percent)}
            </div>
            <p className="text-xs text-muted-foreground">
              Partial: {formatPercent(displayOverview.partial_recovery_rate_percent)}
            </p>
          </CardContent>
        </Card>

        {/* Customer Self-Recovered */}
        <Card>
          <CardHeader className="flex flex-row items-center justify-between space-y-0 pb-2">
            <CardTitle className="text-sm font-medium">Customer Self-Recovered</CardTitle>
            <svg
              xmlns="http://www.w3.org/2000/svg"
              viewBox="0 0 24 24"
              fill="none"
              stroke="currentColor"
              strokeLinecap="round"
              strokeLinejoin="round"
              strokeWidth="2"
              className="h-4 w-4 text-slate-400"
            >
              <path d="M16 21v-2a4 4 0 0 0-4-4H6a4 4 0 0 0-4 4v2" />
              <circle cx="9" cy="7" r="4" />
              <polyline points="16 11 18 13 22 9" />
            </svg>
          </CardHeader>
          <CardContent>
            <div className="text-2xl font-bold text-slate-400">
              {displayOverview.customer_self_recovered_count}
            </div>
            <p className="text-xs text-muted-foreground">No system action needed</p>
          </CardContent>
        </Card>

        {/* Pending Human Approval */}
        <Card>
          <CardHeader className="flex flex-row items-center justify-between space-y-0 pb-2">
            <CardTitle className="text-sm font-medium">Pending Human Approval</CardTitle>
            <svg
              xmlns="http://www.w3.org/2000/svg"
              viewBox="0 0 24 24"
              fill="none"
              stroke="currentColor"
              strokeLinecap="round"
              strokeLinejoin="round"
              strokeWidth="2"
              className="h-4 w-4 text-orange-400"
            >
              <circle cx="12" cy="12" r="10" />
              <polyline points="12 6 12 12 16 14" />
            </svg>
          </CardHeader>
          <CardContent>
            <div className="text-2xl font-bold text-orange-400">
              {displayOverview.pending_human_approval_count}
            </div>
            <p className="text-xs text-muted-foreground">Requires attention</p>
          </CardContent>
        </Card>

        {/* Not Worth Recovering */}
        <Card>
          <CardHeader className="flex flex-row items-center justify-between space-y-0 pb-2">
            <CardTitle className="text-sm font-medium">Not Worth Recovering</CardTitle>
            <svg
              xmlns="http://www.w3.org/2000/svg"
              viewBox="0 0 24 24"
              fill="none"
              stroke="currentColor"
              strokeLinecap="round"
              strokeLinejoin="round"
              strokeWidth="2"
              className="h-4 w-4 text-slate-500"
            >
              <circle cx="12" cy="12" r="10" />
              <line x1="4.93" y1="4.93" x2="19.07" y2="19.07" />
            </svg>
          </CardHeader>
          <CardContent>
            <div className="text-2xl font-bold text-slate-500">
              {displayOverview.not_worth_recovering_count}
            </div>
            <p className="text-xs text-muted-foreground">System decided to stop</p>
          </CardContent>
        </Card>
      </div>

      {/* Charts Row */}
      <div className="grid grid-cols-1 gap-6 lg:grid-cols-2">
        {/* Line Chart: Revenue Over Time */}
        <Card>
          <CardHeader>
            <CardTitle>Revenue Over Time (24h)</CardTitle>
          </CardHeader>
          <CardContent>
            <ResponsiveContainer width="100%" height={300}>
              <LineChart data={revenueData || []}>
                <CartesianGrid strokeDasharray="3 3" stroke="#374151" />
                <XAxis
                  dataKey="timestamp"
                  tickFormatter={(val) => format(new Date(val), "HH:mm")}
                  stroke="#9CA3AF"
                />
                <YAxis
                  tickFormatter={(val) => `₹${(val / 100).toFixed(0)}`}
                  stroke="#9CA3AF"
                />
                <Tooltip
                  contentStyle={{
                    backgroundColor: "#1F2937",
                    border: "1px solid #374151",
                    borderRadius: "8px",
                  }}
                  formatter={(value: number) => formatCurrency(value)}
                  labelFormatter={(label) => format(new Date(label), "MMM d, HH:mm")}
                />
                <Legend />
                <Line
                  type="monotone"
                  dataKey="recovered_paise"
                  stroke="#10B981"
                  name="Recovered"
                  strokeWidth={2}
                />
                <Line
                  type="monotone"
                  dataKey="at_risk_paise"
                  stroke="#EF4444"
                  name="At Risk"
                  strokeWidth={2}
                />
              </LineChart>
            </ResponsiveContainer>
          </CardContent>
        </Card>

        {/* Bar Chart: Recovery Rate by Failure Type */}
        <Card>
          <CardHeader>
            <CardTitle>Recovery Rate by Failure Type</CardTitle>
          </CardHeader>
          <CardContent>
            <ResponsiveContainer width="100%" height={300}>
              <BarChart data={recoveryRateData || []}>
                <CartesianGrid strokeDasharray="3 3" stroke="#374151" />
                <XAxis dataKey="label" stroke="#9CA3AF" />
                <YAxis stroke="#9CA3AF" />
                <Tooltip
                  contentStyle={{
                    backgroundColor: "#1F2937",
                    border: "1px solid #374151",
                    borderRadius: "8px",
                  }}
                  formatter={(value: number, name: string) => {
                    if (name === "recovery_rate") return `${value.toFixed(1)}%`;
                    return value;
                  }}
                />
                <Legend />
                <Bar dataKey="total" fill="#6B7280" name="Total" />
                <Bar dataKey="recovered" fill="#10B981" name="Recovered" />
              </BarChart>
            </ResponsiveContainer>
          </CardContent>
        </Card>
      </div>

      {/* Live Feed */}
      <div className="grid grid-cols-1 gap-6 lg:grid-cols-3">
        {/* Recent Cases (Keep existing) */}
        <Card className="lg:col-span-2">
          <CardHeader>
            <CardTitle>Recent Cases</CardTitle>
            <p className="text-sm text-muted-foreground">Last 10 recovery cases</p>
          </CardHeader>
          <CardContent>
            <div className="space-y-4">
              {recentCases && recentCases.length > 0 ? (
                recentCases.map((c) => (
                  <div
                    key={c.id}
                    className="flex items-center justify-between border-b border-border pb-4 last:border-0 last:pb-0"
                  >
                    <div className="flex-1">
                      <div className="flex items-center gap-2">
                        <Badge variant={c.status as any}>{c.status.replace(/_/g, " ")}</Badge>
                        <span className="text-sm font-medium text-foreground">
                          {formatCurrency(c.amount_paise)}
                        </span>
                        {c.upi_error_code && (
                          <span className="text-xs text-muted-foreground">
                            UPI {c.upi_error_code}
                          </span>
                        )}
                      </div>
                      <p className="mt-1 text-xs text-muted-foreground">
                        {formatDate(c.created_at)} • {c.customer_email || c.customer_id || "Unknown customer"}
                      </p>
                    </div>
                    {c.validator_skip_reason && (
                      <div className="ml-4 text-xs text-orange-400">
                        Validator: {c.validator_skip_reason}
                      </div>
                    )}
                  </div>
                ))
              ) : (
                <p className="text-center text-sm text-muted-foreground">No recent cases</p>
              )}
            </div>
          </CardContent>
        </Card>

        {/* Live Activity Feed (NEW) */}
        <Card>
          <CardHeader>
            <div className="flex items-center justify-between">
              <CardTitle>Live Activity</CardTitle>
              {connected && (
                <div className="flex items-center gap-1">
                  <div className="w-1.5 h-1.5 bg-green-500 rounded-full animate-pulse" />
                  <span className="text-xs text-green-400">LIVE</span>
                </div>
              )}
            </div>
            <p className="text-sm text-muted-foreground">Real-time system events</p>
          </CardHeader>
          <CardContent>
            <div className="space-y-3 max-h-[400px] overflow-y-auto">
              {liveEvents.slice(0, 10).map((event, idx) => (
                <LiveActivityItem key={`${event.timestamp}-${idx}`} event={event} />
              ))}
              {liveEvents.length === 0 && (
                <p className="text-center text-sm text-muted-foreground py-4">
                  Waiting for activity...
                </p>
              )}
            </div>
          </CardContent>
        </Card>
      </div>
    </div>
  );
}

function LoadingState() {
  return (
    <div className="flex h-screen items-center justify-center">
      <div className="text-center">
        <div className="h-8 w-8 animate-spin rounded-full border-4 border-primary border-t-transparent"></div>
        <p className="mt-4 text-sm text-muted-foreground">Loading dashboard...</p>
      </div>
    </div>
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

  // Format message
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
    <div className="flex items-start gap-2 animate-slideIn">
      <span className="text-sm">{icon}</span>
      <div className="flex-1 min-w-0">
        <div className="flex items-center gap-2">
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
