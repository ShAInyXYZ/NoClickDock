//! Windows needs three things tao does not do for an overlay that must never
//! take focus: show without activating (tao's later `show()` activates),
//! refuse activation on click, and stay out of Alt+Tab. From
//! docs/rewrite/research/tauri-windows.md (b); compiled here, run in M7.
use windows::Win32::Foundation::{HWND, LPARAM, LRESULT, WPARAM};
use windows::Win32::UI::Shell::{DefSubclassProc, SetWindowSubclass};
use windows::Win32::UI::WindowsAndMessaging::{
    GetWindowLongPtrW, SetWindowLongPtrW, SetWindowPos, ShowWindow, GWL_EXSTYLE, HWND_TOPMOST, MA_NOACTIVATE,
    SWP_NOACTIVATE, SWP_NOMOVE, SWP_NOSIZE, SWP_SHOWWINDOW, SW_HIDE, SW_SHOWNOACTIVATE, WM_MOUSEACTIVATE,
    WS_EX_NOACTIVATE, WS_EX_TOOLWINDOW,
};

unsafe extern "system" fn refuse_activation(
    hwnd: HWND,
    msg: u32,
    wparam: WPARAM,
    lparam: LPARAM,
    _id: usize,
    _data: usize,
) -> LRESULT {
    if msg == WM_MOUSEACTIVATE {
        return LRESULT(MA_NOACTIVATE as isize);
    }
    DefSubclassProc(hwnd, msg, wparam, lparam)
}

/// Main thread only. Tao rewrites the extended style on every flag change,
/// so this runs after the last one.
pub fn harden(window: &tauri::WebviewWindow) -> tauri::Result<()> {
    let hwnd = window.hwnd()?;
    unsafe {
        let ex = GetWindowLongPtrW(hwnd, GWL_EXSTYLE);
        SetWindowLongPtrW(hwnd, GWL_EXSTYLE, ex | (WS_EX_TOOLWINDOW.0 | WS_EX_NOACTIVATE.0) as isize);
        let _ = SetWindowSubclass(hwnd, Some(refuse_activation), 0x4e4344, 0);
    }
    Ok(())
}

pub fn show(window: &tauri::WebviewWindow) -> tauri::Result<()> {
    let hwnd = window.hwnd()?;
    unsafe {
        let _ = ShowWindow(hwnd, SW_SHOWNOACTIVATE);
        let _ = SetWindowPos(
            hwnd,
            Some(HWND_TOPMOST),
            0,
            0,
            0,
            0,
            SWP_NOACTIVATE | SWP_NOMOVE | SWP_NOSIZE | SWP_SHOWWINDOW,
        );
    }
    Ok(())
}

pub fn hide(window: &tauri::WebviewWindow) -> tauri::Result<()> {
    unsafe {
        let _ = ShowWindow(window.hwnd()?, SW_HIDE);
    }
    Ok(())
}
