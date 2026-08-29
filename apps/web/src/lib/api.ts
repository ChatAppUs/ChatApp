export const API_URL =
  process.env.NEXT_PUBLIC_API_URL ?? "http://localhost:8080";
export const MEDIA_URL =
  process.env.NEXT_PUBLIC_MEDIA_URL ?? "http://localhost:8100";

export interface Tokens {
  access_token: string;
  refresh_token: string;
  expires_in: number;
  user_id?: string;
}

const ACCESS_KEY = "chatapp.access";
const REFRESH_KEY = "chatapp.refresh";
const USER_KEY = "chatapp.userId";

export function saveTokens(t: Tokens) {
  localStorage.setItem(ACCESS_KEY, t.access_token);
  localStorage.setItem(REFRESH_KEY, t.refresh_token);
  if (t.user_id) localStorage.setItem(USER_KEY, t.user_id);
}

export function clearTokens() {
  localStorage.removeItem(ACCESS_KEY);
  localStorage.removeItem(REFRESH_KEY);
  localStorage.removeItem(USER_KEY);
}

export function getAccessToken(): string | null {
  return localStorage.getItem(ACCESS_KEY);
}

export function getUserId(): string | null {
  return localStorage.getItem(USER_KEY);
}

export class ApiError extends Error {
  status: number;
  constructor(status: number, message: string) {
    super(message);
    this.status = status;
  }
}

async function rawRequest(
  path: string,
  options: RequestInit = {},
  auth = true
): Promise<Response> {
  const headers: Record<string, string> = {
    "Content-Type": "application/json",
    ...((options.headers as Record<string, string>) ?? {}),
  };
  if (auth) {
    const token = getAccessToken();
    if (token) headers["Authorization"] = `Bearer ${token}`;
  }
  return fetch(`${API_URL}${path}`, { ...options, headers });
}

async function refreshTokens(): Promise<boolean> {
  const refresh = localStorage.getItem(REFRESH_KEY);
  if (!refresh) return false;
  const res = await rawRequest(
    "/api/auth/refresh",
    { method: "POST", body: JSON.stringify({ refresh_token: refresh }) },
    false
  );
  if (!res.ok) {
    clearTokens();
    return false;
  }
  const t = (await res.json()) as Tokens;
  saveTokens(t);
  return true;
}

export async function api<T = unknown>(
  path: string,
  options: RequestInit = {},
  auth = true
): Promise<T> {
  let res = await rawRequest(path, options, auth);
  if (res.status === 401 && auth && (await refreshTokens())) {
    res = await rawRequest(path, options, auth);
  }
  const data = (await res.json().catch(() => ({}))) as Record<string, unknown>;
  if (!res.ok) {
    throw new ApiError(res.status, (data.error as string) ?? `HTTP ${res.status}`);
  }
  return data as T;
}

export async function uploadMedia(file: File): Promise<string> {
  // The media edge requires a short-lived signed grant when signed URLs are
  // enforced (production). Fetch one; if the signing service is not deployed
  // (local dev), upload unsigned — the edge accepts it only in that mode.
  let grant = "";
  try {
    const t = await api<{ expires: number; signature: string }>(
      "/api/media/upload-token",
      { method: "POST" }
    );
    grant = `&exp=${t.expires}&sig=${encodeURIComponent(t.signature)}`;
  } catch {
    // dev mode: media edge without SECURITY_SERVICE_URL accepts unsigned uploads
  }
  const res = await fetch(
    `${MEDIA_URL}/upload?filename=${encodeURIComponent(file.name)}${grant}`,
    { method: "POST", body: file }
  );
  if (!res.ok) throw new ApiError(res.status, "upload failed");
  const data = (await res.json()) as { url: string };
  return `${MEDIA_URL}${data.url}`;
}

export function wsURL(): string {
  const token = getAccessToken() ?? "";
  const base = API_URL.replace(/^http/, "ws");
  return `${base}/ws?token=${encodeURIComponent(token)}`;
}
