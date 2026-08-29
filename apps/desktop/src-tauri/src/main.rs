#![cfg_attr(not(debug_assertions), windows_subsystem = "windows")]

use tauri::Manager;

/// ChatApp desktop shell.
///
/// The window loads the ChatApp web app (PWA). The window URL is set in
/// tauri.conf.json for development; for production builds set the
/// CHATAPP_URL environment variable before `tauri build` and override the
/// window URL here — e.g. https://app.chatapp.example.
#[tauri::command]
fn app_version() -> &'static str {
    env!("CARGO_PKG_VERSION")
}

fn main() {
    tauri::Builder::default()
        .invoke_handler(tauri::generate_handler![app_version])
        .setup(|app| {
            if let Ok(url) = std::env::var("CHATAPP_URL") {
                if let Some(window) = app.get_webview_window("main") {
                    if let Ok(parsed) = url.parse() {
                        let _ = window.navigate(parsed);
                    }
                }
            }
            Ok(())
        })
        .run(tauri::generate_context!())
        .expect("error while running ChatApp desktop");
}
