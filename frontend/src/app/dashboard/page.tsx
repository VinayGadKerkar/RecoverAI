"use client";

import { useEffect, useState } from "react";
import useSWR from "swr";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import { Badge } from "@/components/ui/badge";
import { getOverview, getRecoveryRate, getRevenue, getRecentCases } from "@/lib/api";
import { formatCurrency, formatPercent, formatDate, formatDuration } from "@/lib/utils";
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

export default function DashboardPage() {
  // Poll overview every 5 seconds for live updates
  const { data: overview } = useSWR("/analytics/overview", getOverview, {
    refreshInterval: 5000,
  });

  const { data: recoveryRateData } = useSWR(
    "/analytics/recovery-rate",
    () => getRecoveryRate("7d", "failure_type"),
    { refreshInterval: 30000 }
  );

  const { data: revenueData } = useSWR(
    "/analytics/revenue",
    () => getRevenue("24h", "hour"),
    { refreshInterval: 30000 }
  );

  const { data: recentCases } = useSWR(
    "/recent-cases",
    () => getRecentCases(10),
    { refreshInterval: 5000 }
  );

  const [prevRecovered, setPrevRecovered] = useState<number>(0);
  const [animateRecovered, setAnimateRecovered] = useState(false);

  // Animate recovered revenue on change
  useEffect(() => {
    if (overview && overview.recovered_revenue_paise !== prevRecovered) {
      setAnimateRecovered(true);
      setPrevRecovered(overview.recovered_revenue_paise);
      setTimeout(() => setAnimateRecovered(false), 500);
    }
  }, [overview, prevRecovered]);

  if (!overview) {
    return <LoadingState />;
  }

  return (
    <div className="p-8 space-y-8">
      {/* Header */}
      <div>
        <h2 className="text-3xl font-bold text-foreground">Recovery Overview</h2>
        <p className="text-muted-foreground">Real-time payment recovery dashboard</p>
      </div>

      {/* Top Metric Cards (6 cards) */}
      <div className="grid grid-cols-1 gap-6 md:grid-cols-2 lg:grid-cols-3">
        {/* Revenue at Risk */}
        <Card>
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
              {formatCurrency(overview.revenue_at_risk_paise)}
            </div>
            <p className="text-xs text-muted-foreground">
              {overview.active_cases} active cases
            </p>
          </CardContent>
        </Card>

        {/* Recovered Revenue */}
        <Card>
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
            <div
              className={`text-2xl font-bold text-green-400 ${
                animateRecovered ? "animate-tick-up" : ""
              }`}
            >
              {formatCurrency(overview.recovered_revenue_paise)}
            </div>
            <p className="text-xs text-muted-foreground">
              {overview.total_recovered_payments} payments recovered
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
              {formatPercent(overview.recovery_rate_percent)}
            </div>
            <p className="text-xs text-muted-foreground">
              Partial: {formatPercent(overview.partial_recovery_rate_percent)}
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
              {overview.customer_self_recovered_count}
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
              {overview.pending_human_approval_count}
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
              {overview.not_worth_recovering_count}
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
      <Card>
        <CardHeader>
          <CardTitle>Recent Cases (Live)</CardTitle>
          <p className="text-sm text-muted-foreground">Updates every 5 seconds</p>
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
                        {formatCurrency(c.revenue_at_risk)}
                      </span>
                      {c.upi_error_code && (
                        <span className="text-xs text-muted-foreground">
                          UPI {c.upi_error_code}
                        </span>
                      )}
                    </div>
                    <p className="mt-1 text-xs text-muted-foreground">
                      {formatDate(c.created_at)} • {c.customer_name || "Unknown customer"}
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
