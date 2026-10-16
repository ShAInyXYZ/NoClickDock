//! Windows needs three things tao does not do for an overlay that must never
//! take focus: show without activating (tao's later `show()` activates),
//! refuse activation on click, and stay out of Alt+Tab. From
//! docs/rewrite/research/tauri-windows.md (b); compiled here, run in M7.
use windows::Win32::Foundation::{HWND, LPARAM, LRESULT, WPARAM};
use windows::Win32::UI::Shell::{DefSubclassProc, SetWindowSubclass};
use windows::Win32::UI::WindowsAndMessaging::{
    GetForegroundWindow, GetWindowLongPtrW, SetForegroundWindow, SetWindowLongPtrW, SetWindowPos, ShowWindow, GWL_EXSTYLE, HWND_TOPMOST, MA_NOACTIVATE,
    STYLESTRUCT, SWP_NOACTIVATE, SWP_NOMOVE, SWP_NOSIZE, SWP_SHOWWINDOW, SW_HIDE, SW_SHOWNOACTIVATE,
    WM_MOUSEACTIVATE, WM_STYLECHANGING, WS_EX_NOACTIVATE, WS_EX_TOOLWINDOW,
};

unsafe extern "system" fn refuse_activation(
    hwnd: HWND,
    msg: u32,
    wparam: WPARAM,
    lparam: LPARAM,
    _id: usize,
    _data: usize,
) -> LRESULT {
    match msg {
        WM_MOUSEACTIVATE => return LRESULT(MA_NOACTIVATE as isize),
        // tao rewrites the extended style from its own flags on every flag
        // change — its first show() included — which would drop these two
        // bits and put the overlay in Alt+Tab: keep them in every rewrite.
        WM_STYLECHANGING if wparam.0 as i32 == GWL_EXSTYLE.0 => {
            let style = &mut *(lparam.0 as *mut STYLESTRUCT);
            style.styleNew |= WS_EX_TOOLWINDOW.0 | WS_EX_NOACTIVATE.0;
        }
        _ => {}
    }
    DefSubclassProc(hwnd, msg, wparam, lparam)
}

/// Main thread only (SetWindowSubclass must run on the window's thread).
/// Once is enough: the subclass keeps the bits through tao's rewrites.
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

/// A native menu makes its owner the foreground window (muda); give the
/// foreground back to the user's application once the menu closes.
pub fn popup_menu_keep_foreground<M: tauri::menu::ContextMenu>(
    window: &tauri::WebviewWindow,
    menu: &M,
) -> tauri::Result<()> {
    let previous = unsafe { GetForegroundWindow() };
    let result = window.popup_menu(menu);
    if !previous.is_invalid() {
        unsafe {
            let _ = SetForegroundWindow(previous);
        }
    }
    result
}
