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
})();
