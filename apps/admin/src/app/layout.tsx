import "./globals.css";
import type { Metadata } from "next";

export const metadata: Metadata = {
  title: "ChatApp Admin",
  description: "ChatApp administration console — separate from the user platform",
};

export default function RootLayout({ children }: { children: React.ReactNode }) {
  return (
    <html lang="en">
      <body>
        <main style={{ maxWidth: 1100, margin: "0 auto", padding: 16 }}>
          <h2 style={{ marginBottom: 4 }}>ChatApp Admin</h2>
          <p className="muted" style={{ marginTop: 0 }}>
            Restricted operations console. Not part of the user platform.
          </p>
          {children}
        </main>
      </body>
    </html>
  );
}
