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
const ACCOUNTS_KEY = "chatapp.accounts";

export interface Account {
  userId: string;
  username?: string;
  access: string;
  refresh: string;
}

export function listAccounts(): Account[] {
  try {
    return JSON.parse(localStorage.getItem(ACCOUNTS_KEY) ?? "[]");
  } catch {
    return [];
  }
}

function archiveAccount(a: Account) {
  const all = listAccounts().filter((x) => x.userId !== a.userId);
  localStorage.setItem(ACCOUNTS_KEY, JSON.stringify([...all, a]));
}

export function saveTokens(t: Tokens, username?: string) {
  localStorage.setItem(ACCESS_KEY, t.access_token);
  localStorage.setItem(REFRESH_KEY, t.refresh_token);
  if (t.user_id) {
    localStorage.setItem(USER_KEY, t.user_id);
    archiveAccount({
      userId: t.user_id,
      username,
      access: t.access_token,
      refresh: t.refresh_token,
    });
  }
}

export function switchAccount(userId: string): boolean {
  const target = listAccounts().find((a) => a.userId === userId);
  if (!target) return false;
  localStorage.setItem(ACCESS_KEY, target.access);
  localStorage.setItem(REFRESH_KEY, target.refresh);
  localStorage.setItem(USER_KEY, target.userId);
  return true;
}

export function clearTokens() {
  const userId = localStorage.getItem(USER_KEY);
  if (userId) {
    localStorage.setItem(
      ACCOUNTS_KEY,
      JSON.stringify(listAccounts().filter((a) => a.userId !== userId))
    );
  }
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

// Chunked resumable upload for large media (Telegram-style 2 GiB files):
// the Go API opens a session and the Rust security service signs a grant
// bound to it; bytes then flow straight to the C++ media edge in 4 MiB
// chunks, with resume-on-failure via the edge's received-byte probe.
export async function uploadMediaChunked(
  file: File,
  onProgress?: (sent: number, total: number) => void
): Promise<string> {
  const CHUNK = 4 * 1024 * 1024;
  const session = await api<{
    upload_id: string;
    expires: number;
    signature: string;
    media_base: string;
  }>("/api/uploads", {
    method: "POST",
    body: JSON.stringify({ filename: file.name, total_bytes: file.size }),
  });
  const base = session.media_base || MEDIA_URL;
  const grant = `sig=${encodeURIComponent(session.signature)}&exp=${session.expires}`;
  const id = session.upload_id;

  await fetch(`${base}/upload/init?id=${id}&filename=${encodeURIComponent(file.name)}&total=${file.size}&${grant}`, { method: "POST" });

  const probe = async (): Promise<number> => {
    const res = await fetch(`${base}/upload/${id}/status`);
    if (!res.ok) return 0;
    const data = (await res.json()) as { received: number };
    return data.received;
  };

  let sent = await probe();
  onProgress?.(sent, file.size);
  while (sent < file.size) {
    const slice = file.slice(sent, sent + CHUNK);
    const res = await fetch(`${base}/upload/${id}/chunk?${grant}`, {
      method: "PUT",
      body: slice,
    });
    if (!res.ok) {
      sent = await probe(); // resume from what the edge actually holds
      continue;
    }
    sent += slice.size;
    onProgress?.(sent, file.size);
  }

  const done = await fetch(`${base}/upload/${id}/complete?${grant}`, { method: "POST" });
  if (!done.ok) throw new ApiError(done.status, "upload finalize failed");
  const fin = (await done.json()) as { url: string; bytes: number };
  await api(`/api/uploads/${id}/complete`, {
    method: "POST",
    body: JSON.stringify({ media_url: fin.url, received_bytes: fin.bytes }),
  });
  return `${base}${fin.url}`;
}

export function wsURL(): string {
  const token = getAccessToken() ?? "";
  const base = API_URL.replace(/^http/, "ws");
  return `${base}/ws?token=${encodeURIComponent(token)}`;
}
