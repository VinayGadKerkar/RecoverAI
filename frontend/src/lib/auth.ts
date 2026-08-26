const TOKEN_KEY = "recoverai_token";

/** Read the stored JWT. Returns null if not in a browser or not set. */
export function getToken(): string | null {
  if (typeof window === "undefined") return null;
  return localStorage.getItem(TOKEN_KEY);
}

/** Persist a JWT to localStorage. */
export function setToken(token: string): void {
  localStorage.setItem(TOKEN_KEY, token);
}

/** Remove the stored JWT (logout). */
export function clearToken(): void {
  if (typeof window === "undefined") return;
  localStorage.removeItem(TOKEN_KEY);
}

/** True when a token is present in localStorage. */
export function isLoggedIn(): boolean {
  return !!getToken();
}
