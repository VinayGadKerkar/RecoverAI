"use client";

import { useState } from "react";
import Link from "next/link";
import useSWR from "swr";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import { Badge } from "@/components/ui/badge";
import { getRecoveryCases } from "@/lib/api";
import { formatCurrency, formatDate } from "@/lib/utils";
import { RecoveryCaseStatus } from "@/lib/types";

export default function RecoveryCasesPage() {
  const [filters, setFilters] = useState({
    status: "",
    priority: "",
    upi_error_code: "",
    bank_outage_detected: undefined as boolean | undefined,
  });

  const { data: cases, isLoading } = useSWR(
    ["/recovery-cases", filters],
    () => getRecoveryCases(filters),
    { refreshInterval: 10000 }
  );

  const handleFilterChange = (key: string, value: string) => {
    setFilters((prev) => ({
      ...prev,
      [key]: value === "all" ? "" : value,
    }));
  };

  return (
    <div className="p-8 space-y-6">
      {/* Header */}
      <div>
        <h2 className="text-3xl font-bold text-foreground">Recovery Cases</h2>
        <p className="text-muted-foreground">View and filter all recovery cases</p>
      </div>

      {/* Filters */}
      <Card>
        <CardHeader>
          <CardTitle>Filters</CardTitle>
        </CardHeader>
        <CardContent>
          <div className="grid grid-cols-1 gap-4 md:grid-cols-4">
            {/* Status Filter */}
            <div>
              <label className="mb-2 block text-sm font-medium text-foreground">
                Status
              </label>
              <select
                className="w-full rounded-md border border-border bg-background px-3 py-2 text-sm text-foreground"
                value={filters.status}
                onChange={(e) => handleFilterChange("status", e.target.value)}
              >
                <option value="all">All</option>
                <option value="open">Open</option>
                <option value="in_progress">In Progress</option>
                <option value="recovered">Recovered</option>
                <option value="partially_recovered">Partially Recovered</option>
                <option value="failed">Failed</option>
                <option value="customer_self_recovered">Customer Self-Recovered</option>
                <option value="pending_human_approval">Pending Human Approval</option>
                <option value="outage_batched">Outage Batched</option>
                <option value="not_worth_recovering">Not Worth Recovering</option>
                <option value="stopped">Stopped</option>
              </select>
            </div>

            {/* Priority Filter */}
            <div>
              <label className="mb-2 block text-sm font-medium text-foreground">
                Priority
              </label>
              <select
                className="w-full rounded-md border border-border bg-background px-3 py-2 text-sm text-foreground"
                value={filters.priority}
                onChange={(e) => handleFilterChange("priority", e.target.value)}
              >
                <option value="all">All</option>
                <option value="low">Low</option>
                <option value="medium">Medium</option>
                <option value="high">High</option>
                <option value="critical">Critical</option>
              </select>
            </div>

            {/* UPI Error Code Filter */}
            <div>
              <label className="mb-2 block text-sm font-medium text-foreground">
                UPI Error Code
              </label>
              <select
                className="w-full rounded-md border border-border bg-background px-3 py-2 text-sm text-foreground"
                value={filters.upi_error_code}
                onChange={(e) => handleFilterChange("upi_error_code", e.target.value)}
              >
                <option value="all">All</option>
                <option value="U30">U30 - Debit timeout</option>
                <option value="U28">U28 - Credit pending</option>
                <option value="RB">RB - Insufficient funds</option>
                <option value="BT">BT - Bank timeout</option>
                <option value="U16">U16 - Risk threshold</option>
                <option value="Z9">Z9 - Bank declined</option>
                <option value="Z8">Z8 - Invalid VPA</option>
                <option value="Z7">Z7 - Transaction failure</option>
                <option value="U68">U68 - Collect expired</option>
                <option value="YG">YG - PSP declined</option>
                <option value="U69">U69 - Frequency limit</option>
              </select>
            </div>

            {/* Bank Outage Filter */}
            <div>
              <label className="mb-2 block text-sm font-medium text-foreground">
                Bank Outage
              </label>
              <select
                className="w-full rounded-md border border-border bg-background px-3 py-2 text-sm text-foreground"
                value={
                  filters.bank_outage_detected === undefined
                    ? "all"
                    : filters.bank_outage_detected
                    ? "true"
                    : "false"
                }
                onChange={(e) =>
                  setFilters((prev) => ({
                    ...prev,
                    bank_outage_detected:
                      e.target.value === "all"
                        ? undefined
                        : e.target.value === "true",
                  }))
                }
              >
                <option value="all">All</option>
                <option value="true">Yes</option>
                <option value="false">No</option>
              </select>
            </div>
          </div>
        </CardContent>
      </Card>

      {/* Cases Table */}
      <Card>
        <CardContent className="p-0">
          {isLoading ? (
            <div className="flex items-center justify-center py-12">
              <div className="h-8 w-8 animate-spin rounded-full border-4 border-primary border-t-transparent"></div>
            </div>
          ) : cases && cases.length > 0 ? (
            <div className="overflow-x-auto">
              <table className="w-full">
                <thead className="border-b border-border bg-muted/50">
                  <tr>
                    <th className="px-6 py-3 text-left text-xs font-medium uppercase tracking-wider text-muted-foreground">
                      Case ID
                    </th>
                    <th className="px-6 py-3 text-left text-xs font-medium uppercase tracking-wider text-muted-foreground">
                      Status
                    </th>
                    <th className="px-6 py-3 text-left text-xs font-medium uppercase tracking-wider text-muted-foreground">
                      Amount
                    </th>
                    <th className="px-6 py-3 text-left text-xs font-medium uppercase tracking-wider text-muted-foreground">
                      Priority
                    </th>
                    <th className="px-6 py-3 text-left text-xs font-medium uppercase tracking-wider text-muted-foreground">
                      Error Code
                    </th>
                    <th className="px-6 py-3 text-left text-xs font-medium uppercase tracking-wider text-muted-foreground">
                      Validator Decision
                    </th>
                    <th className="px-6 py-3 text-left text-xs font-medium uppercase tracking-wider text-muted-foreground">
                      Outage
                    </th>
                    <th className="px-6 py-3 text-left text-xs font-medium uppercase tracking-wider text-muted-foreground">
                      Created
                    </th>
                    <th className="px-6 py-3 text-left text-xs font-medium uppercase tracking-wider text-muted-foreground">
                      Actions
                    </th>
                  </tr>
                </thead>
                <tbody className="divide-y divide-border">
                  {cases.map((c) => (
                    <tr
                      key={c.id}
                      className="hover:bg-accent/50 transition-colors cursor-pointer"
                      onClick={() => (window.location.href = `/dashboard/cases/${c.id}`)}
                    >
                      <td className="whitespace-nowrap px-6 py-4 text-sm font-mono text-foreground">
                        {c.id.substring(0, 8)}...
                      </td>
                      <td className="whitespace-nowrap px-6 py-4">
                        <Badge variant={c.status as any}>
                          {c.status ? c.status.replace(/_/g, " ") : "Processing"}
                        </Badge>
                      </td>
                      <td className="whitespace-nowrap px-6 py-4 text-sm font-medium text-foreground">
                        {formatCurrency(c.amount_paise)}
                      </td>
                      <td className="whitespace-nowrap px-6 py-4">
                        <span
                          className={`text-xs font-medium uppercase ${
                            c.priority === "critical"
                              ? "text-red-400"
                              : c.priority === "high"
                              ? "text-orange-400"
                              : c.priority === "medium"
                              ? "text-yellow-400"
                              : "text-slate-400"
                          }`}
                        >
                          {c.priority}
                        </span>
                      </td>
                      <td className="whitespace-nowrap px-6 py-4 text-sm text-muted-foreground">
                        {c.upi_error_code || "—"}
                      </td>
                      <td className="px-6 py-4 text-sm">
                        {c.validator_skip_reason ? (
                          <span
                            className="text-orange-400 cursor-help"
                            title={c.validator_skip_reason}
                          >
                            Skipped
                          </span>
                        ) : (
                          <span className="text-green-400">Passed</span>
                        )}
                      </td>
                      <td className="whitespace-nowrap px-6 py-4 text-center">
                        {c.bank_outage_detected ? (
                          <span className="text-purple-400">●</span>
                        ) : (
                          <span className="text-slate-600">○</span>
                        )}
                      </td>
                      <td className="whitespace-nowrap px-6 py-4 text-sm text-muted-foreground">
                        {formatDate(c.created_at)}
                      </td>
                      <td className="whitespace-nowrap px-6 py-4 text-sm">
                        <Link
                          href={`/dashboard/cases/${c.id}`}
                          className="text-primary hover:underline"
                          onClick={(e) => e.stopPropagation()}
                        >
                          View Details
                        </Link>
                      </td>
                    </tr>
                  ))}
                </tbody>
              </table>
            </div>
          ) : (
            <div className="py-12 text-center">
              <p className="text-sm text-muted-foreground">No cases found</p>
            </div>
          )}
        </CardContent>
      </Card>
    </div>
  );
}
