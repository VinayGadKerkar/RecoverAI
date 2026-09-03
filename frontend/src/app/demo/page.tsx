"use client";

import { useState, useEffect } from "react";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import { useWebSocket } from "@/hooks/useWebSocket";
import { Activity, AlertTriangle, CheckCircle, Loader2, PlayCircle, RefreshCw, XCircle } from "lucide-react";

interface SystemStatus {
  ai_mode: string;
  demo_mode: boolean;
  mock_ai_available: boolean;
  real_call_count: number;
  test_limit_enabled: boolean;
}

interface ServiceStatus {
  name: string;
  url: string;
  status: "checking" | "online" | "offline";
  responseTime?: number;
}

export default function DemoPage() {
  const [systemStatus, setSystemStatus] = useState<SystemStatus | null>(null);
  const [services, setServices] = useState<ServiceStatus[]>([
    { name: "API Server", url: `${process.env.NEXT_PUBLIC_API_URL}/health`, status: "checking" },
  ]);
  
  const [loadingScenario, setLoadingScenario] = useState<string | null>(null);
  const [statusMessage, setStatusMessage] = useState<{ type: "success" | "error"; message: string } | null>(null);
  const [resetting, setResetting] = useState(false);

  const { events: liveEvents, connected: wsConnected } = useWebSocket();

  // Poll system status every 5 seconds
  useEffect(() => {
    const checkStatus = async () => {
      try {
        const res = await fetch(`${process.env.NEXT_PUBLIC_API_URL}/api/v1/status`);
        if (res.ok) {
          const data = await res.json();
          setSystemStatus(data);
        }
      } catch (err) {
        console.error("Failed to fetch status:", err);
      }
    };

    checkStatus();
    const interval = setInterval(checkStatus, 5000);
    return () => clearInterval(interval);
  }, []);

  // Check service health on mount
  useEffect(() => {
    const checkServices = async () => {
      const updatedServices = await Promise.all(
        services.map(async (service) => {
          const start = Date.now();
          try {
            const res = await fetch(service.url, { 
              method: "GET",
              signal: AbortSignal.timeout(3000)
            });
            const responseTime = Date.now() - start;
            return {
              ...service,
              status: res.ok ? "online" as const : "offline" as const,
              responseTime,
            };
          } catch (err) {
            return { ...service, status: "offline" as const };
          }
        })
      );
      setServices(updatedServices);
    };

    checkServices();
  }, []);

  const triggerScenario = async (scenario: string) => {
    setLoadingScenario(scenario);
    setStatusMessage(null);

    try {
      const res = await fetch(`${process.env.NEXT_PUBLIC_API_URL}/api/v1/demo/trigger`, {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ scenario }),
      });

      const data = await res.json();

      if (!res.ok) {
        throw new Error(data.error || "Failed to trigger scenario");
      }

      setStatusMessage({
        type: "success",
        message: `✅ ${data.message}`,
      });
    } catch (err: any) {
      setStatusMessage({
        type: "error",
        message: `❌ Error: ${err.message}`,
      });
    } finally {
      setLoadingScenario(null);
    }
  };

  const resetDemo = async () => {
    if (!confirm("This will delete all current recovery cases. Continue?")) {
      return;
    }

    setResetting(true);
    setStatusMessage(null);

    try {
      const res = await fetch(`${process.env.NEXT_PUBLIC_API_URL}/api/v1/demo/reset`, {
        method: "POST",
        headers: { "Content-Type": "application/json" },
      });

      const data = await res.json();

      if (!res.ok) {
        throw new Error(data.error || "Failed to reset demo");
      }

      setStatusMessage({
        type: "success",
        message: data.message,
      });
    } catch (err: any) {
      setStatusMessage({
        type: "error",
        message: `❌ Error: ${err.message}`,
      });
    } finally {
      setResetting(false);
    }
  };

  return (
    <div className="min-h-screen bg-background p-8">
      <div className="max-w-7xl mx-auto">
        {/* Header */}
        <div className="mb-8">
          <h1 className="text-3xl font-bold text-foreground mb-2">🎬 Demo Control Panel</h1>
          <p className="text-muted-foreground">
            Trigger recovery scenarios and monitor system status in real-time
          </p>
        </div>

        {/* Status Message */}
        {statusMessage && (
          <div
            className={`mb-6 p-4 rounded-lg border ${
              statusMessage.type === "success"
                ? "bg-green-500/10 border-green-500/30 text-green-400"
                : "bg-red-500/10 border-red-500/30 text-red-400"
            }`}
          >
            {statusMessage.message}
          </div>
        )}

        <div className="grid grid-cols-1 lg:grid-cols-2 gap-6">
          {/* Left Column: System Status */}
          <div className="space-y-6">
            <Card className="metric-card">
              <CardHeader className="pb-4">
                <CardTitle className="text-base font-semibold">System Status</CardTitle>
              </CardHeader>
              <CardContent className="space-y-4">
                {/* Service Health Checks */}
                <div className="space-y-2">
                  {services.map((service) => (
                    <div key={service.name} className="flex items-center justify-between py-2">
                      <span className="text-sm text-muted-foreground">{service.name}</span>
                      <div className="flex items-center gap-2">
                        {service.status === "checking" && (
                          <Loader2 className="h-4 w-4 animate-spin text-blue-400" />
                        )}
                        {service.status === "online" && (
                          <>
                            <div className="w-2 h-2 bg-green-500 rounded-full" />
                            <span className="text-xs text-green-400 font-medium">
                              ONLINE {service.responseTime && `(${service.responseTime}ms)`}
                            </span>
                          </>
                        )}
                        {service.status === "offline" && (
                          <>
                            <div className="w-2 h-2 bg-red-500 rounded-full" />
                            <span className="text-xs text-red-400 font-medium">OFFLINE</span>
                          </>
                        )}
                      </div>
                    </div>
                  ))}

                  {/* WebSocket Status */}
                  <div className="flex items-center justify-between py-2">
                    <span className="text-sm text-muted-foreground">WebSocket</span>
                    <div className="flex items-center gap-2">
                      {wsConnected ? (
                        <>
                          <div className="w-2 h-2 bg-green-500 rounded-full animate-pulse" />
                          <span className="text-xs text-green-400 font-medium">CONNECTED</span>
                        </>
                      ) : (
                        <>
                          <div className="w-2 h-2 bg-red-500 rounded-full" />
                          <span className="text-xs text-red-400 font-medium">DISCONNECTED</span>
                        </>
                      )}
                    </div>
                  </div>
                </div>

                {/* AI Mode */}
                {systemStatus && (
                  <>
                    <div className="pt-4 border-t border-border">
                      <p className="text-xs text-muted-foreground mb-2">AI Mode</p>
                      {systemStatus.ai_mode === "real" ? (
                        <div className="flex items-center gap-2">
                          <div className="w-2 h-2 bg-green-500 rounded-full" />
                          <span className="text-sm font-semibold text-green-400">🟢 LIVE AI (Groq)</span>
                        </div>
                      ) : (
                        <div className="flex items-center gap-2">
                          <div className="w-2 h-2 bg-orange-500 rounded-full" />
                          <span className="text-sm font-semibold text-orange-400">🟡 Mock AI</span>
                        </div>
                      )}
                      <p className="text-xs text-muted-foreground mt-1">
                        {systemStatus.mock_ai_available ? "Mock AI available" : "Mock AI unavailable"}
                      </p>
                    </div>

                    {/* Demo Mode */}
                    <div className="pt-4 border-t border-border">
                      <p className="text-xs text-muted-foreground mb-2">Demo Mode</p>
                      {systemStatus.demo_mode ? (
                        <div className="flex items-center gap-2">
                          <div className="w-2 h-2 bg-green-500 rounded-full animate-pulse" />
                          <span className="text-sm font-semibold text-green-400">
                            ⚡ Demo Mode ON (1-min delays)
                          </span>
                        </div>
                      ) : (
                        <div className="space-y-2">
                          <div className="flex items-center gap-2">
                            <div className="w-2 h-2 bg-slate-500 rounded-full" />
                            <span className="text-sm font-semibold text-slate-400">Demo Mode OFF</span>
                          </div>
                          <p className="text-xs text-orange-400">
                            ⚠️ Delays are real! Set DEMO_MODE=true in .env
                          </p>
                        </div>
                      )}
                    </div>
                  </>
                )}
              </CardContent>
            </Card>

            {/* Live Activity Feed */}
            <Card className="metric-card">
              <CardHeader className="pb-4">
                <div className="flex items-center justify-between">
                  <CardTitle className="text-base font-semibold flex items-center gap-2">
                    <Activity className="h-5 w-5 text-primary" />
                    Live Activity Feed
                  </CardTitle>
                  {wsConnected && (
                    <div className="flex items-center gap-1.5">
                      <div className="w-1.5 h-1.5 bg-green-500 rounded-full animate-pulse" />
                      <span className="text-xs text-green-400 font-medium">LIVE</span>
                    </div>
                  )}
                </div>
              </CardHeader>
              <CardContent>
                <div className="space-y-2 max-h-[400px] overflow-y-auto">
                  {liveEvents.slice(0, 10).map((event, idx) => {
                    const actor = (event.data.actor as string) || "system";
                    const action = (event.data.action as string) || "activity";
                    const time = new Date(event.timestamp).toLocaleTimeString("en-US", {
                      hour: "2-digit",
                      minute: "2-digit",
                      second: "2-digit",
                    });

                    const actorIcons: Record<string, string> = {
                      system: "⚙️",
                      risk_engine: "📊",
                      validator: "🛡️",
                      ai_agent: "🤖",
                      policy_engine: "⚖️",
                      execution_worker: "⚡",
                      human: "👤",
                      customer_self: "👤",
                    };

                    const actorColors: Record<string, string> = {
                      system: "#6B7280",
                      risk_engine: "#3B82F6",
                      validator: "#10B981",
                      ai_agent: "#8B5CF6",
                      policy_engine: "#F59E0B",
                      execution_worker: "#F97316",
                      human: "#6366F1",
                      customer_self: "#10B981",
                    };

                    const icon = actorIcons[actor] || "⚙️";
                    const color = actorColors[actor] || "#6B7280";

                    let message = action.replace(/_/g, " ");
                    if (event.data.metadata) {
                      const metadata = event.data.metadata as Record<string, unknown>;
                      if (action === "payment_captured" && metadata.amount_paise) {
                        const amount = (metadata.amount_paise as number) / 100;
                        message = `✅ ₹${amount.toFixed(2)} recovered`;
                      }
                    }

                    return (
                      <div
                        key={`${event.timestamp}-${idx}`}
                        className="flex items-start gap-2 p-2 rounded-lg bg-muted/20 animate-slideIn text-xs"
                      >
                        <span className="text-sm mt-0.5">{icon}</span>
                        <div className="flex-1 min-w-0">
                          <div className="flex items-center gap-2 mb-0.5">
                            <span className="font-mono text-muted-foreground">{time}</span>
                            <span className="font-medium truncate" style={{ color }}>
                              {actor.replace(/_/g, " ")}
                            </span>
                          </div>
                          <p className="text-gray-300 truncate">{message}</p>
                        </div>
                      </div>
                    );
                  })}
                  {liveEvents.length === 0 && (
                    <p className="text-center text-sm text-muted-foreground py-8">
                      Waiting for activity...
                    </p>
                  )}
                </div>
              </CardContent>
            </Card>
          </div>

          {/* Right Column: Scenario Triggers */}
          <div className="space-y-6">
            <Card className="metric-card">
              <CardHeader className="pb-4">
                <CardTitle className="text-base font-semibold">Scenario Triggers</CardTitle>
                <p className="text-xs text-muted-foreground mt-1">
                  Run complete recovery flows without touching the terminal
                </p>
              </CardHeader>
              <CardContent className="space-y-4">
                {/* Scenario A */}
                <button
                  onClick={() => triggerScenario("a")}
                  disabled={loadingScenario === "a"}
                  className="w-full p-4 rounded-lg border-2 border-green-500/30 bg-green-500/5 hover:bg-green-500/10 transition-colors text-left disabled:opacity-50 disabled:cursor-not-allowed"
                >
                  <div className="flex items-center justify-between mb-2">
                    <div className="flex items-center gap-2">
                      {loadingScenario === "a" ? (
                        <Loader2 className="h-5 w-5 animate-spin text-green-400" />
                      ) : (
                        <PlayCircle className="h-5 w-5 text-green-400" />
                      )}
                      <span className="font-semibold text-green-400">Scenario A — Transient Recovery</span>
                    </div>
                  </div>
                  <p className="text-xs text-muted-foreground">
                    U30 failure → full pipeline → ₹4,999 recovered
                  </p>
                </button>

                {/* Scenario B */}
                <button
                  onClick={() => triggerScenario("b")}
                  disabled={loadingScenario === "b"}
                  className="w-full p-4 rounded-lg border-2 border-red-500/30 bg-red-500/5 hover:bg-red-500/10 transition-colors text-left disabled:opacity-50 disabled:cursor-not-allowed"
                >
                  <div className="flex items-center justify-between mb-2">
                    <div className="flex items-center gap-2">
                      {loadingScenario === "b" ? (
                        <Loader2 className="h-5 w-5 animate-spin text-red-400" />
                      ) : (
                        <XCircle className="h-5 w-5 text-red-400" />
                      )}
                      <span className="font-semibold text-red-400">Scenario B — Intelligent Stop</span>
                    </div>
                  </div>
                  <p className="text-xs text-muted-foreground">
                    Z9 + new customer → validator blocks (negative ROI)
                  </p>
                </button>

                {/* Scenario C */}
                <button
                  onClick={() => triggerScenario("c")}
                  disabled={loadingScenario === "c"}
                  className="w-full p-4 rounded-lg border-2 border-purple-500/30 bg-purple-500/5 hover:bg-purple-500/10 transition-colors text-left disabled:opacity-50 disabled:cursor-not-allowed"
                >
                  <div className="flex items-center justify-between mb-2">
                    <div className="flex items-center gap-2">
                      {loadingScenario === "c" ? (
                        <Loader2 className="h-5 w-5 animate-spin text-purple-400" />
                      ) : (
                        <AlertTriangle className="h-5 w-5 text-purple-400" />
                      )}
                      <span className="font-semibold text-purple-400">Scenario C — Outage Detection</span>
                    </div>
                  </div>
                  <p className="text-xs text-muted-foreground">
                    15× U28 burst → Redis outage flag → all batched
                  </p>
                </button>

                {/* Scenario D */}
                <button
                  onClick={() => triggerScenario("d")}
                  disabled={loadingScenario === "d"}
                  className="w-full p-4 rounded-lg border-2 border-blue-500/30 bg-blue-500/5 hover:bg-blue-500/10 transition-colors text-left disabled:opacity-50 disabled:cursor-not-allowed"
                >
                  <div className="flex items-center justify-between mb-2">
                    <div className="flex items-center gap-2">
                      {loadingScenario === "d" ? (
                        <Loader2 className="h-5 w-5 animate-spin text-blue-400" />
                      ) : (
                        <CheckCircle className="h-5 w-5 text-blue-400" />
                      )}
                      <span className="font-semibold text-blue-400">Scenario D — Self-Recovery</span>
                    </div>
                  </div>
                  <p className="text-xs text-muted-foreground">
                    Fail + capture same ID → customer_self_recovered
                  </p>
                </button>
              </CardContent>
            </Card>

            {/* Reset Button */}
            <Card className="metric-card">
              <CardHeader className="pb-4">
                <CardTitle className="text-base font-semibold">Reset Demo Data</CardTitle>
                <p className="text-xs text-muted-foreground mt-1">
                  Clear all cases and start fresh
                </p>
              </CardHeader>
              <CardContent>
                <button
                  onClick={resetDemo}
                  disabled={resetting}
                  className="w-full p-4 rounded-lg border-2 border-orange-500/30 bg-orange-500/5 hover:bg-orange-500/10 transition-colors disabled:opacity-50 disabled:cursor-not-allowed"
                >
                  <div className="flex items-center justify-center gap-2">
                    {resetting ? (
                      <Loader2 className="h-5 w-5 animate-spin text-orange-400" />
                    ) : (
                      <RefreshCw className="h-5 w-5 text-orange-400" />
                    )}
                    <span className="font-semibold text-orange-400">
                      {resetting ? "Resetting..." : "Reset Demo Data"}
                    </span>
                  </div>
                  <p className="text-xs text-muted-foreground mt-2 text-center">
                    Deletes all current recovery cases
                  </p>
                </button>
              </CardContent>
            </Card>

            {/* Quick Links */}
            <Card className="metric-card border-primary/20">
              <CardHeader className="pb-4">
                <CardTitle className="text-base font-semibold">Quick Links</CardTitle>
              </CardHeader>
              <CardContent className="space-y-2">
                <a
                  href="/dashboard"
                  target="_blank"
                  rel="noopener noreferrer"
                  className="block p-3 rounded-lg bg-primary/10 hover:bg-primary/20 transition-colors border border-primary/30"
                >
                  <div className="flex items-center justify-between">
                    <span className="text-sm font-medium text-primary">📊 Dashboard</span>
                    <span className="text-xs text-muted-foreground">Watch metrics update</span>
                  </div>
                </a>
                <a
                  href={`${process.env.NEXT_PUBLIC_API_URL}/api/v1/status`}
                  target="_blank"
                  rel="noopener noreferrer"
                  className="block p-3 rounded-lg bg-muted/30 hover:bg-muted/50 transition-colors border border-border"
                >
                  <div className="flex items-center justify-between">
                    <span className="text-sm font-medium text-foreground">🔧 API Status</span>
                    <span className="text-xs text-muted-foreground">View raw JSON</span>
                  </div>
                </a>
              </CardContent>
            </Card>
          </div>
        </div>
      </div>
    </div>
  );
}
