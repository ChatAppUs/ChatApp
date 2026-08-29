// Admin-plane API client. Completely separate from the user app: it
// authenticates via POST /api/admin/login, stores tokens under admin-only
// keys, and only calls /api/admin/* endpoints. Admin-scoped tokens are
// rejected by every user-plane route on the API.

export const API_URL =
  process.env.NEXT_PUBLIC_API_URL ?? "http://localhost:8080";

const TOKEN_KEY = "chatapp.admin.access";
const ROLES_KEY = "chatapp.admin.roles";

export function saveAdminSession(token: string, roles: string[]) {
  localStorage.setItem(TOKEN_KEY, token);
  localStorage.setItem(ROLES_KEY, JSON.stringify(roles));
}

export function getAdminToken(): string | null {
  return localStorage.getItem(TOKEN_KEY);
}

export function getAdminRoles(): string[] {
  try {
    return JSON.parse(localStorage.getItem(ROLES_KEY) ?? "[]");
  } catch {
    return [];
  }
}

export function clearAdminSession() {
  localStorage.removeItem(TOKEN_KEY);
  localStorage.removeItem(ROLES_KEY);
}

export async function adminApi<T>(path: string, init?: RequestInit): Promise<T> {
  const res = await fetch(`${API_URL}${path}`, {
    ...init,
    headers: {
      "Content-Type": "application/json",
      ...(getAdminToken() ? { Authorization: `Bearer ${getAdminToken()}` } : {}),
      ...(init?.headers ?? {}),
    },
  });
  if (!res.ok) {
    let msg = res.statusText;
    try {
      msg = (await res.json()).error ?? msg;
    } catch { /* keep statusText */ }
    const err = new Error(msg) as Error & { status: number };
    err.status = res.status;
    throw err;
  }
  return res.json() as Promise<T>;
}

export async function adminLogin(
  identifier: string,
  password: string,
  totpCode?: string
): Promise<{ access_token: string; roles: string[] }> {
  const res = await fetch(`${API_URL}/api/admin/login`, {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify({ identifier, password, totp_code: totpCode }),
  });
  const data = await res.json();
  if (!res.ok) throw new Error(data.error ?? "login failed");
  saveAdminSession(data.access_token, data.roles ?? []);
  return data;
}
