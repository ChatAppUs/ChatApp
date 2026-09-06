import type { Metadata, Viewport } from "next";
import "./globals.css";
import { I18nProvider } from "@/lib/i18n";
import Nav from "@/components/Nav";

export const metadata: Metadata = {
  title: { default: "ChatApp — Social, Chat, Calls, Wallet", template: "%s · ChatApp" },
  description: "Social, messaging, creator and payments platform",
  manifest: "/manifest.webmanifest",
  icons: {
    icon: [{ url: "/icons/32.png", sizes: "32x32", type: "image/png" }, { url: "/icons/192.png", sizes: "192x192", type: "image/png" }, { url: "/icons/512.png", sizes: "512x512", type: "image/png" }],
    apple: [{ url: "/icons/apple-touch-icon.png", sizes: "180x180", type: "image/png" }],
  },
  appleWebApp: {
    capable: true,
    statusBarStyle: "black-translucent",
    title: "ChatApp",
  },
};

export const viewport: Viewport = {
  themeColor: [
    { media: "(prefers-color-scheme: light)", color: "#f4f6fb" },
    { media: "(prefers-color-scheme: dark)", color: "#0b0f17" },
  ],
  width: "device-width",
  initialScale: 1,
};

const themeInit = `(function(){try{var t=localStorage.getItem("chatapp.theme");if(t!=="light")t="dark";document.documentElement.dataset.theme=t;}catch(e){}})();`;

export default function RootLayout({ children }: { children: React.ReactNode }) {
  return (
    <html lang="en">
      <head>
        <script dangerouslySetInnerHTML={{ __html: themeInit }} />
      </head>
      <body>
        <I18nProvider>
          <Nav />
          <main className="container">{children}</main>
        </I18nProvider>
      </body>
    </html>
  );
}
