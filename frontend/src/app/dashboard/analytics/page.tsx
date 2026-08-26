"use client";

import useSWR from "swr";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import { Badge } from "@/components/ui/badge";
import { getHonestExceptions, getAIPerformance } from "@/lib/api";
import { formatCurrency, formatPercent } from "@/lib/utils";

export default function AnalyticsPage() {
  const { data: exceptions } = useSWR("/analytics/honest-exceptions", () =>
    getHonestExceptions(50)
  );

  const { data: aiPerf } = useSWR("/analytics/ai-performance", getAIPerformance);

  return (
    <div className="p-8 space-y-8">
      {/* Header */}
      <div>
        <h2 className="text-3xl font-bold text-foreground">Analytics</h2>
        <p className="text-muted-foreground">Deep insights into recovery performance</p>
      </div>

      {/* AI Performance Section */}
      {aiPerf && (
        <div>
          <h3 className="text-xl font-bold text-foreground mb-4">AI Performance</h3>
          <div className="grid grid-cols-1 gap-6 md:grid-cols-2 lg:grid-cols-4">
            <Card>
              <CardHeader className="pb-2">
                <CardTitle className="text-sm font-medium">Total AI Calls</CardTitle>
              </CardHeader>
              <CardContent>
                <div className="text-2xl font-bold text-foreground">
                  {aiPerf.total_ai_calls.toLocaleString()}
                </div>
              </CardContent>
            </Card>

            <Card>
              <CardHeader className="pb-2">
                <CardTitle className="text-sm font-medium">Avg Confidence</CardTitle>
              </CardHeader>
              <CardContent>
                <div className="text-2xl font-bold text-foreground">
                  {formatPercent(aiPerf.avg_confidence * 100)}
                </div>
              </CardContent>
            </Card>

            <Card>
              <CardHeader className="pb-2">
                <CardTitle className="text-sm font-medium">
                  High Confidence Recovery Rate
                </CardTitle>
              </CardHeader>
              <CardContent>
                <div className="text-2xl font-bold text-green-400">
                  {formatPercent(aiPerf.high_confidence_recovery_rate)}
                </div>
                <p className="text-xs text-muted-foreground">Confidence &gt; 80%</p>
              </CardContent>
            </Card>

            <Card>
              <CardHeader className="pb-2">
                <CardTitle className="text-sm font-medium">
                  Low Confidence Recovery Rate
                </CardTitle>
              </CardHeader>
              <CardContent>
                <div className="text-2xl font-bold text-orange-400">
                  {formatPercent(aiPerf.low_confidence_recovery_rate)}
                </div>
                <p className="text-xs text-muted-foreground">Confidence &lt; 50%</p>
              </CardContent>
            </Card>
          </div>

          {/* Strategy Breakdown */}
          <Card className="mt-6">
            <CardHeader>
              <CardTitle>Strategy Breakdown</CardTitle>
            </CardHeader>
            <CardContent>
              <div className="space-y-4">
                {(aiPerf.strategy_breakdown ?? []).map((item) => (
                  <div
                    key={item.strategy}
                    className="flex items-center justify-between border-b border-border pb-3 last:border-0 last:pb-0"
                  >
                    <div className="flex-1">
                      <p className="text-sm font-medium text-foreground">{item.strategy}</p>
                      <p className="text-xs text-muted-foreground">
                        {item.count} cases • {formatPercent(item.recovery_rate)} recovery rate
                      </p>
                    </div>
                    <div className="ml-4">
                      <div className="h-2 w-32 overflow-hidden rounded-full bg-muted">
                        <div
                          className="h-full bg-green-500"
                          style={{ width: `${item.recovery_rate}%` }}
                        ></div>
                      </div>
                    </div>
                  </div>
                ))}
              </div>
            </CardContent>
          </Card>

          {/* AI Gate Metrics */}
          <div className="mt-6 grid grid-cols-1 gap-6 md:grid-cols-2">
            <Card>
              <CardHeader className="pb-2">
                <CardTitle className="text-sm font-medium">
                  Cases Blocked Before AI
                </CardTitle>
              </CardHeader>
              <CardContent>
                <div className="text-2xl font-bold text-orange-400">
                  {aiPerf.cases_blocked_before_ai.toLocaleString()}
                </div>
                <p className="text-xs text-muted-foreground">
                  Validator stopped before reaching AI
                </p>
              </CardContent>
            </Card>

            <Card>
              <CardHeader className="pb-2">
                <CardTitle className="text-sm font-medium">
                  Cases AI Would Have Been Wrong
                </CardTitle>
              </CardHeader>
              <CardContent>
                <div className="text-2xl font-bold text-yellow-400">
                  {aiPerf.cases_ai_would_have_been_wrong.toLocaleString()}
                </div>
                <p className="text-xs text-muted-foreground">
                  Policy overrode AI but recovery happened
                </p>
              </CardContent>
            </Card>
          </div>
        </div>
      )}

      {/* Honest Exceptions Section */}
      <div>
        <h3 className="text-xl font-bold text-foreground mb-4">Honest Exceptions</h3>
        <p className="text-sm text-muted-foreground mb-4">
          Cases that could not be recovered with detailed reasons
        </p>

        <Card>
          <CardContent className="p-0">
            {exceptions && exceptions.length > 0 ? (
              <div className="overflow-x-auto">
                <table className="w-full">
                  <thead className="border-b border-border bg-muted/50">
                    <tr>
                      <th className="px-6 py-3 text-left text-xs font-medium uppercase tracking-wider text-muted-foreground">
                        Case ID
                      </th>
                      <th className="px-6 py-3 text-left text-xs font-medium uppercase tracking-wider text-muted-foreground">
                        Amount
                      </th>
                      <th className="px-6 py-3 text-left text-xs font-medium uppercase tracking-wider text-muted-foreground">
                        UPI Error
                      </th>
                      <th className="px-6 py-3 text-left text-xs font-medium uppercase tracking-wider text-muted-foreground">
                        Reason
                      </th>
                      <th className="px-6 py-3 text-left text-xs font-medium uppercase tracking-wider text-muted-foreground">
                        Validator Skip
                      </th>
                      <th className="px-6 py-3 text-left text-xs font-medium uppercase tracking-wider text-muted-foreground">
                        Policy Rule
                      </th>
                      <th className="px-6 py-3 text-left text-xs font-medium uppercase tracking-wider text-muted-foreground">
                        Human Could Recover?
                      </th>
                    </tr>
                  </thead>
                  <tbody className="divide-y divide-border">
                    {exceptions.map((ex) => (
                      <tr key={ex.case_id} className="hover:bg-accent/50 transition-colors">
                        <td className="whitespace-nowrap px-6 py-4 text-sm font-mono text-foreground">
                          {ex.case_id.substring(0, 8)}...
                        </td>
                        <td className="whitespace-nowrap px-6 py-4 text-sm font-medium text-foreground">
                          {formatCurrency(ex.amount_paise)}
                        </td>
                        <td className="whitespace-nowrap px-6 py-4 text-sm text-orange-400">
                          {ex.upi_error_code}
                        </td>
                        <td className="px-6 py-4 text-sm text-muted-foreground max-w-xs">
                          {ex.reason}
                        </td>
                        <td className="px-6 py-4 text-sm text-muted-foreground max-w-xs">
                          {ex.validator_skip_reason || "—"}
                        </td>
                        <td className="px-6 py-4 text-sm text-muted-foreground">
                          {ex.policy_rule_triggered || "—"}
                        </td>
                        <td className="whitespace-nowrap px-6 py-4 text-center">
                          {ex.could_human_have_recovered ? (
                            <Badge variant="outline" className="text-green-400 border-green-400">
                              Yes
                            </Badge>
                          ) : (
                            <Badge variant="outline" className="text-red-400 border-red-400">
                              No
                            </Badge>
                          )}
                        </td>
                      </tr>
                    ))}
                  </tbody>
                </table>
              </div>
            ) : (
              <div className="py-12 text-center">
                <p className="text-sm text-muted-foreground">No exceptions found</p>
              </div>
            )}
          </CardContent>
        </Card>
      </div>
    </div>
  );
}
