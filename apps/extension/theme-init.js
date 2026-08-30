// Applied before first paint in every extension page to avoid a theme flash.
// Mirrors apps/web/src/app/layout.tsx (same storage key, same attribute).
(function () {
  try {
    chrome.storage.sync.get({ theme: "dark" }, function (s) {
      document.documentElement.dataset.theme = s.theme === "light" ? "light" : "dark";
    });
  } catch (e) { /* storage unavailable — stay on the dark default */ }
})();
