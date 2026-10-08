//! Native tab accelerators belong to the app menu on macOS, before WebKit handles keys.
use tauri::{
    menu::{Menu, MenuEvent, MenuItem, PredefinedMenuItem},
    AppHandle, Emitter, Manager, Runtime,
};

#[derive(Clone, serde::Serialize)]
#[serde(rename_all = "lowercase")]
enum TabAction {
    New,
    Close,
    Reopen,
    Overview,
    Next,
    Previous,
}

pub fn build<R: Runtime>(app: &AppHandle<R>) -> tauri::Result<Menu<R>> {
    // Keep native application, Edit, Services, fullscreen and Help behaviors.
    let menu = Menu::default(app)?;
    for item in menu.items()? {
        let Some(submenu) = item.as_submenu() else {
            continue;
        };
        match submenu.text()?.as_str() {
            "File" => {
                // Default CloseWindow reserves Cmd+W, which must close the tab instead.
                for item in submenu.items()? {
                    submenu.remove(&item)?;
                }
                submenu.append_items(&[
                    &MenuItem::with_id(app, "tab-new", "New Tab", true, Some("Cmd+T"))?,
                    &MenuItem::with_id(app, "tab-close", "Close Tab", true, Some("Cmd+W"))?,
                    &MenuItem::with_id(
                        app,
                        "tab-reopen",
                        "Reopen Closed Tab",
                        true,
                        Some("Cmd+Shift+T"),
                    )?,
                    &PredefinedMenuItem::separator(app)?,
                    &MenuItem::with_id(
                        app,
                        "window-close",
                        "Close Window",
                        true,
                        Some("Cmd+Shift+W"),
                    )?,
                ])?;
            }
            "Window" => {
                // The default Window menu also includes the same conflicting Cmd+W item.
                // Preserve its submenu identity so macOS still recognizes the Window menu.
                for item in submenu.items()? {
                    submenu.remove(&item)?;
                }
                submenu.append_items(&[
                    &PredefinedMenuItem::minimize(app, None)?,
                    &PredefinedMenuItem::maximize(app, None)?,
                    &PredefinedMenuItem::separator(app)?,
                    &MenuItem::with_id(
                        app,
                        "tab-next",
                        "Select Next Tab",
                        true,
                        Some("Cmd+Shift+BracketRight"),
                    )?,
                    &MenuItem::with_id(
                        app,
                        "tab-previous",
                        "Select Previous Tab",
                        true,
                        Some("Cmd+Shift+BracketLeft"),
                    )?,
                ])?;
            }
            "View" => {
                submenu.append_items(&[
                    &PredefinedMenuItem::separator(app)?,
                    &MenuItem::with_id(
                        app,
                        "tab-overview",
                        "Show All Tabs",
                        true,
                        Some("Cmd+Shift+Backslash"),
                    )?,
                ])?;
            }
            _ => {}
        }
    }
    Ok(menu)
}

pub fn handle<R: Runtime>(app: &AppHandle<R>, event: MenuEvent) {
    let action = match event.id().as_ref() {
        "tab-new" => TabAction::New,
        "tab-close" => TabAction::Close,
        "tab-reopen" => TabAction::Reopen,
        "tab-overview" => TabAction::Overview,
        "tab-next" => TabAction::Next,
        "tab-previous" => TabAction::Previous,
        "window-close" => {
            let focused = app
                .webview_windows()
                .into_values()
                .find(|window| window.is_focused().unwrap_or(false));
            if let Some(window) = focused.or_else(|| app.get_webview_window("main")) {
                let _ = window.close();
            }
            return;
        }
        _ => return,
    };
    // UI-only command: no model, tool, session or engine execution crosses this boundary.
    if let Err(error) = app.emit_to("main", "desktop-tab-action", action) {
        eprintln!("Unable to deliver desktop tab action: {error}");
    }
}
