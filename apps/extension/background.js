// Background service worker: polls the notification endpoint once a minute
// (chrome.alarms) and mirrors the unread count onto the toolbar badge.
// Only active when the user has pasted a token on the Options page.

const ALARM = "chatapp-notify-poll";

chrome.runtime.onInstalled.addListener(() => {
  chrome.alarms.create(ALARM, { periodInMinutes: 1 });
});

chrome.alarms.onAlarm.addListener((a) => {
  if (a.name === ALARM) poll();
});

async function poll() {
  const s = await chrome.storage.sync.get({
    apiUrl: "http://localhost:8080",
    accessToken: "",
  });
  if (!s.accessToken) {
    chrome.action.setBadgeText({ text: "" });
    return;
  }
  try {
    const r = await fetch(s.apiUrl.replace(/\/$/, "") + "/api/notifications", {
      headers: { Authorization: "Bearer " + s.accessToken },
    });
    if (!r.ok) {
      chrome.action.setBadgeText({ text: "!" });
      chrome.action.setBadgeBackgroundColor({ color: "#ef4d5e" });
      return;
    }
    const list = await r.json();
    const unread = (list.notifications || list || []).filter((x) => !x.read_at && !x.read).length;
    chrome.action.setBadgeText({ text: unread > 0 ? String(unread) : "" });
    chrome.action.setBadgeBackgroundColor({ color: "#4f7cff" });
  } catch (e) {
    chrome.action.setBadgeText({ text: "" });
  }
}
