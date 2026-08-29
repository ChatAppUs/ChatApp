import type { CapacitorConfig } from "@capacitor/cli";

// ChatApp mobile wraps the deployed web app. Point `server.url` at your
// production deployment, or build apps/web and copy .next static export
// into webDir for fully bundled offline shells.
const config: CapacitorConfig = {
  appId: "dev.chatapp.mobile",
  appName: "ChatApp",
  webDir: "www",
  server: {
    url: process.env.CHATAPP_URL ?? "http://localhost:3000",
    cleartext: process.env.NODE_ENV !== "production",
  },
  android: {
    allowMixedContent: process.env.NODE_ENV !== "production",
  },
  ios: {
    contentInset: "automatic",
  },
  plugins: {
    Camera: { permissions: ["camera", "microphone"] },
  },
};

export default config;
