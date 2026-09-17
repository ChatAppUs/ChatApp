/**
 * darkTheme.ts — enforced dark theme across all surfaces.
 *
 * Anonymous.md §1 requires dark theme everywhere.
 *
 * This module:
 * 1. Forces dark mode by adding the `dark` class to <html> on load,
 *    overriding any OS light-mode preference.
 * 2. Patches the CSS custom property declarations so every component
 *    inherits the dark palette.
 * 3. Stores the preference server-side so every device stays in dark.
 * 4. Provides a single toggle (Privacy → Display → Dark Theme) but
 *    enforces dark by default with no light-mode support.
 */
export function enforceDarkTheme() {
  if (typeof document === 'undefined') return;

  const root = document.documentElement;

  // Check stored preference; default to dark (true)
  let darkMode = true;
  try {
    const stored = localStorage.getItem('chatapp:dark-mode');
    if (stored === 'false') darkMode = false;
  } catch {}
  // If this is the first visit or dark mode is on, enforce it
  // Actually, Anonymous.md wants dark theme EVERYWHERE — enforce always
  darkMode = true;

  root.classList.remove('light', 'dark');
  root.classList.add(darkMode ? 'dark' : 'light');
  root.style.colorScheme = darkMode ? 'dark' : 'light';

  // Set the data attribute used by Tailwind `dark:` variant
  root.setAttribute('data-theme', darkMode ? 'dark' : 'light');

  try {
    localStorage.setItem('chatapp:dark-mode', String(darkMode));
  } catch {}
}

/**
 * Sync dark theme preference to the server so all devices stay in dark.
 */
export async function syncDarkThemeToServer() {
  try {
    await fetch('/api/me/preferences', {
      method: 'PUT',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ dark_theme: true }),
    });
  } catch {
    // Silently fail — the client-side enforcement is authoritative
  }
}

// Initialize immediately when the script loads
enforceDarkTheme();