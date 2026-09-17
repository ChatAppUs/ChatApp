// screen_security.rs — screenshot prevention for Tauri desktop.
// Anonymous.md §1 requires screenshot protection.

use tauri::Manager;

#[cfg(target_os = "macos")]
pub fn prevent_capture(window: &tauri::Window) -> Result<(), Box<dyn std::error::Error>> {
    use objc::{class, msg_send, sel, sel_impl};
    let ns_window = window.ns_window().ok_or("no NSWindow")?;
    unsafe { let _: () = msg_send![ns_window as *mut objc::runtime::Object, setSharingType: 0u64]; }
    Ok(())
}

#[cfg(target_os = "windows")]
pub fn prevent_capture(window: &tauri::Window) -> Result<(), Box<dyn std::error::Error>> {
    use windows::Win32::UI::WindowsAndMessaging::SetWindowDisplayAffinity;
    let hwnd = window.hwnd().ok_or("no HWND")?;
    unsafe { SetWindowDisplayAffinity(hwnd, windows::Win32::UI::WindowsAndMessaging::WDA_MONITOR)?; }
    Ok(())
}

#[cfg(target_os = "linux")]
pub fn prevent_capture(_window: &tauri::Window) -> Result<(), Box<dyn std::error::Error>> { Ok(()) }

pub fn allow_capture(window: &tauri::Window) -> Result<(), Box<dyn std::error::Error>> {
    #[cfg(target_os = "macos")]
    {
        let ns_window = window.ns_window().ok_or("no NSWindow")?;
        unsafe { let _: () = msg_send![ns_window as *mut objc::runtime::Object, setSharingType: 1u64]; }
    }
    #[cfg(target_os = "windows")]
    {
        let hwnd = window.hwnd().ok_or("no HWND")?;
        unsafe { windows::Win32::UI::WindowsAndMessaging::SetWindowDisplayAffinity(hwnd, windows::Win32::UI::WindowsAndMessaging::WDA_NONE)?; }
    }
    let _ = window;
    Ok(())
}