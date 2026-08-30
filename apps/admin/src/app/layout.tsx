import "./globals.css";
import type { Metadata } from "next";
import ThemeToggle from "@/components/ThemeToggle";

export const metadata: Metadata = {
  title: "ChatApp Admin",
  description: "ChatApp administration console — separate from the user platform",
};

// Applied before first paint; same storage key and attribute as the user
// web app so the theme follows the operator across both consoles.
const themeInit = `(function(){try{var t=localStorage.getItem("chatapp.theme");if(t!=="light")t="dark";document.documentElement.dataset.theme=t;}catch(e){}})();`;

export default function RootLayout({ children }: { children: React.ReactNode }) {
  return (
    <html lang="en">
      <head>
        <script dangerouslySetInnerHTML={{ __html: themeInit }} />
      </head>
      <body>
        <main style={{ maxWidth: 1100, margin: "0 auto", padding: 16 }}>
          <h2 style={{ marginBottom: 4 }}>
            ChatApp Admin
            <ThemeToggle />
          </h2>
          <p className="muted" style={{ marginTop: 0 }}>
            Restricted operations console. Not part of the user platform.
          </p>
          {children}
        </main>
      </body>
    </html>
  );
}
