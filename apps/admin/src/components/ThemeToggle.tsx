"use client";

// Light/dark toggle — same storage key + html[data-theme] attribute as the
// user web app, so an operator's theme follows them across both consoles.
export default function ThemeToggle() {
  return (
    <button
      className="secondary"
      style={{ float: "right", padding: "4px 10px", fontSize: 13 }}
      aria-label="toggle theme"
      onClick={() => {
        const next = document.documentElement.dataset.theme === "light" ? "dark" : "light";
        document.documentElement.dataset.theme = next;
        try {
          localStorage.setItem("chatapp.theme", next);
        } catch {
          /* ignore */
        }
      }}
    >
      ◐ Theme
    </button>
  );
}
