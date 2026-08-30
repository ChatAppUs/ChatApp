// ChatApp extension — shared settings + theme helpers.
// The extension is a shell over the same backend/web app as every other
// client: the popup offers quick navigation and session status, and the
// full-page view embeds the complete web app (all features, same code).

const DEFAULTS = {
  webUrl: "http://localhost:3000",
  apiUrl: "http://localhost:8080",
  theme: "dark",
  accessToken: "",
};

async function getSettings() {
  const s = await chrome.storage.sync.get(DEFAULTS);
  return { ...DEFAULTS, ...s };
}

function applyTheme(theme) {
  document.documentElement.dataset.theme = theme === "light" ? "light" : "dark";
}

async function toggleTheme() {
  const s = await getSettings();
  const next = s.theme === "light" ? "dark" : "light";
  await chrome.storage.sync.set({ theme: next });
  applyTheme(next);
  return next;
}
