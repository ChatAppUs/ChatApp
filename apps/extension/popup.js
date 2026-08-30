// Popup: session status, unread badge, quick navigation, theme toggle.
(async function () {
  const s = await getSettings();
  applyTheme(s.theme);

  document.getElementById("themeBtn").addEventListener("click", toggleTheme);
  document.getElementById("optionsBtn").addEventListener("click", () => chrome.runtime.openOptionsPage());
  document.getElementById("openFull").addEventListener("click", () => {
    chrome.tabs.create({ url: chrome.runtime.getURL("fullpage.html") });
  });
  document.querySelectorAll("button.nav").forEach((b) => {
    b.addEventListener("click", () => {
      chrome.tabs.create({ url: s.webUrl.replace(/\/$/, "") + b.dataset.path });
    });
  });

  const status = document.getElementById("status");
  if (!s.accessToken) {
    status.textContent = "Not connected — sign in via the web app, then paste your token in Options to get badges.";
    return;
  }
  try {
    const r = await fetch(s.apiUrl.replace(/\/$/, "") + "/api/me", {
      headers: { Authorization: "Bearer " + s.accessToken },
    });
    if (!r.ok) throw new Error(String(r.status));
    const me = await r.json();
    status.textContent = "Signed in as @" + (me.username || "user");
    const n = await fetch(s.apiUrl.replace(/\/$/, "") + "/api/notifications", {
      headers: { Authorization: "Bearer " + s.accessToken },
    });
    if (n.ok) {
      const list = await n.json();
      const unread = (list.notifications || list || []).filter((x) => !x.read_at && !x.read).length;
      if (unread > 0) {
        const badge = document.getElementById("badge");
        badge.textContent = String(unread);
        badge.hidden = false;
      }
    }
  } catch (e) {
    status.textContent = "API unreachable or token expired — check Options.";
  }
})();
