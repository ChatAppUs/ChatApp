// Identity-security bindings shared by extension pages.
// Uses the same bearer token and server-owned verification protocol as web and
// native clients; no client boolean bypasses server checks.
async function securityRequest(apiUrl, path, accessToken, method, body) {
  const response = await fetch(apiUrl.replace(/\/$/, "") + path, {
    method,
    headers: { "Content-Type": "application/json", Authorization: `Bearer ${accessToken}` },
    body: method === "GET" ? undefined : JSON.stringify(body || {}),
  });
  const data = await response.json().catch(() => ({}));
  if (!response.ok) throw new Error(data.error || `HTTP ${response.status}`);
  return data;
}
self.ChatAppSecurity = {
  sendCredentialChallenge: (api, token, kind, destination = "") => securityRequest(api, "/api/me/security/challenges", token, "POST", { kind, destination }),
  verifyCredentialChallenge: (api, token, kind, code) => securityRequest(api, `/api/me/security/challenges/${encodeURIComponent(kind)}/verify`, token, "POST", { code }),
  attestCredentialChange: (api, token, selfieUrl) => securityRequest(api, "/api/me/security/attestation", token, "POST", { selfie_url: selfieUrl }),
  changePassword: (api, token, currentPassword, newPassword) => securityRequest(api, "/api/me/security", token, "PUT", { operation: "password", current_password: currentPassword, new_password: newPassword }),
  deletionStatus: (api, token) => securityRequest(api, "/api/me/deletion", token, "GET"),
  requestDeletion: (api, token, password, assetsWithdrawn) => securityRequest(api, "/api/me/deletion", token, "POST", { password, assets_withdrawn: assetsWithdrawn }),
};
