// TODO: typed API client wrapping fetch against the Go API
// All routes prefixed /api/v1, proxied by next.config.ts

export const API_BASE = "/api/v1";

export async function apiFetch<T>(path: string, init?: RequestInit): Promise<T> {
  const res = await fetch(`${API_BASE}${path}`, {
    headers: { "Content-Type": "application/json" },
    ...init,
  });
  if (!res.ok) throw new Error(`API error ${res.status}: ${await res.text()}`);
  return res.json() as Promise<T>;
}
