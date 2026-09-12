// Options: persist web/API URLs and the optional badge token. A non-local
// API host requires an optional host permission — requested here at save time.
(async function () {
  const s = await getSettings();
  applyTheme(s.theme);
  document.getElementById("themeBtn").addEventListener("click", toggleTheme);

  const webUrl = document.getElementById("webUrl");
  const apiUrl = document.getElementById("apiUrl");
  const accessToken = document.getElementById("accessToken");
  webUrl.value = s.webUrl;
  apiUrl.value = s.apiUrl;
  accessToken.value = s.accessToken;

  document.getElementById("save").addEventListener("click", async () => {
    const api = apiUrl.value.trim().replace(/\/$/, "") || s.apiUrl;
    try {
      const origin = new URL(api).origin + "/*";
      const local = /^https?:\/\/(localhost|127\.0\.0\.1)(:\d+)?$/.test(new URL(api).origin);
      if (!local) {
        const granted = await chrome.permissions.request({ origins: [origin] });
        if (!granted) {
          document.getElementById("saved").textContent = "Permission denied for " + origin;
          return;
        }
      }
    } catch (e) {
      document.getElementById("saved").textContent = "Invalid API URL";
      return;
    }
    await chrome.storage.sync.set({
      webUrl: webUrl.value.trim().replace(/\/$/, "") || DEFAULTS.webUrl,
      apiUrl: api,
      accessToken: accessToken.value.trim(),
    });
    document.getElementById("saved").textContent = "Saved.";
    setTimeout(() => (document.getElementById("saved").textContent = ""), 2000);
  });

  const result = document.getElementById("securityResult");
  const security = (fn) => async () => {
    result.textContent = "";
    try { result.textContent = await fn() || "Done."; }
    catch (e) { result.textContent = e instanceof Error ? e.message : "Security request failed."; }
  };
  document.getElementById("sendSecurityOtp").addEventListener("click", security(async () => {
    const d = await ChatAppSecurity.sendCredentialChallenge(apiUrl.value.trim(), accessToken.value.trim(), "current_email");
    return d.dev_code ? `Development OTP: ${d.dev_code}` : "Verification code sent.";
  }));
  document.getElementById("changePassword").addEventListener("click", security(async () => {
    const api = apiUrl.value.trim(); const token = accessToken.value.trim();
    await ChatAppSecurity.verifyCredentialChallenge(api, token, "current_email", document.getElementById("securityOtp").value.trim());
    await ChatAppSecurity.attestCredentialChange(api, token, document.getElementById("securitySelfieUrl").value.trim());
    await ChatAppSecurity.changePassword(api, token, document.getElementById("securityCurrentPassword").value, document.getElementById("securityNewPassword").value);
    return "Password updated; other sessions revoked and withdrawals frozen for 48 hours.";
  }));
  document.getElementById("deletionStatus").addEventListener("click", security(async () => {
    const d = await ChatAppSecurity.deletionStatus(apiUrl.value.trim(), accessToken.value.trim());
    return `Deletion status: ${d.status || "unknown"}${d.pending ? " (pending)" : ""}`;
  }));
})();
