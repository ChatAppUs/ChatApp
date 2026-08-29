// ChatApp desktop client: Electron shell around the deployed web app.
// Set CHATAPP_URL to point at your deployment (defaults to local dev).
const { app, BrowserWindow, shell } = require("electron");

const TARGET_URL = process.env.CHATAPP_URL || "http://localhost:3000";

function createWindow() {
  const win = new BrowserWindow({
    width: 1280,
    height: 840,
    minWidth: 480,
    minHeight: 640,
    backgroundColor: "#0b0f17",
    webPreferences: {
      contextIsolation: true,
      nodeIntegration: false,
      sandbox: true,
    },
  });

  // Open external links in the system browser, never in-app.
  win.webContents.setWindowOpenHandler(({ url }) => {
    if (url.startsWith(TARGET_URL)) return { action: "allow" };
    shell.openExternal(url);
    return { action: "deny" };
  });

  // Camera/mic for WebRTC calls.
  win.webContents.session.setPermissionRequestHandler((_wc, permission, callback) => {
    callback(["media", "mediaKeySystem", "notifications"].includes(permission));
  });

  win.loadURL(TARGET_URL);
}

app.whenReady().then(() => {
  createWindow();
  app.on("activate", () => {
    if (BrowserWindow.getAllWindows().length === 0) createWindow();
  });
});

app.on("window-all-closed", () => {
  if (process.platform !== "darwin") app.quit();
});
