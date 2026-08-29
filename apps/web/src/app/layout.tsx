import type { Metadata, Viewport } from "next";
import "./globals.css";
import { I18nProvider } from "@/lib/i18n";
import Nav from "@/components/Nav";

export const metadata: Metadata = {
  title: "ChatApp",
  description: "Social, messaging, creator and payments platform",
  manifest: "/manifest.webmanifest",
};

export const viewport: Viewport = {
  themeColor: "#0b0f17",
  width: "device-width",
  initialScale: 1,
};

export default function RootLayout({ children }: { children: React.ReactNode }) {
  return (
    <html lang="en">
      <body>
        <I18nProvider>
          <Nav />
          <main className="container">{children}</main>
        </I18nProvider>
      </body>
    </html>
  );
}
