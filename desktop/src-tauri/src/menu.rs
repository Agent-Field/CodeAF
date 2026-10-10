//! Native tab accelerators belong to the app menu on macOS, before WebKit handles keys.
use crate::menu_route::{action_of, target_label};
use tauri::{
    menu::{Menu, MenuEvent, MenuItem, PredefinedMenuItem},
    AppHandle, Emitter, Runtime,
};

pub fn build<R: Runtime>(app: &AppHandle<R>) -> tauri::Result<Menu<R>> {
    // Keep native application, Edit, Services, fullscreen and Help behaviors.
    let menu = Menu::default(app)?;
    for (index, item) in menu.items()?.into_iter().enumerate() {
        let Some(submenu) = item.as_submenu() else {
            continue;
        };
        if cfg!(target_os = "macos") && index == 0 {
            // The application menu: Settings sits directly under About, where macOS puts it.
            submenu.insert_items(
                &[
                    &PredefinedMenuItem::separator(app)?,
                    &MenuItem::with_id(app, "app-settings", "Settings…", true, Some("Cmd+Comma"))?,
                ],
                1,
            )?;
            continue;
        }
        match submenu.text()?.as_str() {
            "File" => {
                // Default CloseWindow reserves Cmd+W, which must close the tab instead.
                for item in submenu.items()? {
                    submenu.remove(&item)?;
                }
                submenu.append_items(&[
                    &MenuItem::with_id(app, "window-new", "New Window", true, Some("Cmd+N"))?,
                    &MenuItem::with_id(app, "tab-new", "New Tab", true, Some("Cmd+T"))?,
                    &MenuItem::with_id(app, "tab-close", "Close Tab", true, Some("Cmd+W"))?,
                    &MenuItem::with_id(
                        app,
                        "tab-close-stop",
                        "Close and Stop",
                        true,
                        Some("Alt+Cmd+W"),
                    )?,
                    &MenuItem::with_id(
                        app,
                        "tab-reopen",
                        "Reopen Closed Tab",
                        true,
                        Some("Cmd+Shift+T"),
                    )?,
                    &PredefinedMenuItem::separator(app)?,
                    &MenuItem::with_id(app, "window-close", "Close Window", true, None::<&str>)?,
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
                    &MenuItem::with_id(app, "view-sidebar", "Toggle Sidebar", true, Some("Cmd+S"))?,
                    &MenuItem::with_id(app, "view-focus", "Focus Mode", true, Some("Cmd+Shift+F"))?,
                    &MenuItem::with_id(app, "view-history", "History", true, Some("Cmd+Y"))?,
                    &MenuItem::with_id(
                        app,
                        "tab-overview",
                        "Show All Tabs",
                        true,
                        Some("Cmd+Shift+Backslash"),
                    )?,
                    // ⌘L has to be a menu accelerator: a web page holds the keys, and
                    // the menu still runs while that page is focused. New Tab and
                    // Close Tab already do; the address chord did not.
                    &MenuItem::with_id(app, "web-address", "Focus Address", true, Some("Cmd+L"))?,
                ])?;
            }
            _ => {}
        }
    }
    Ok(menu)
}

pub fn handle<R: Runtime>(app: &AppHandle<R>, event: MenuEvent) {
    let windows = crate::windows::app_windows(app);
    let focused = windows
        .iter()
        .find(|(_, window)| window.is_focused().unwrap_or(false))
        .map(|(label, _)| label.clone());
    let Some(label) = target_label(focused, windows.iter().map(|(l, _)| l.clone()).collect())
    else {
        return;
    };
    if event.id().as_ref() == "window-new" {
        if let Some(window) = crate::windows::app_window(app, &label) {
            let app = app.clone();
            // Native accelerators must work while a child web page has focus.
            // Build off the event thread, which the window builder may need.
            tauri::async_runtime::spawn(async move {
                let request = crate::windows::OpenRequest {
                    place_key: "now".into(),
                    focus_tab: None,
                    handoff: None,
                    at: None,
                };
                if let Err(error) = crate::windows::open(&app, &window, request) {
                    eprintln!("Unable to open a desktop window: {error}");
                }
            });
        }
        return;
    }
    if event.id().as_ref() == "window-close" {
        if let Some(window) = crate::windows::app_window(app, &label) {
            let _ = window.close();
        }
        return;
    }
    if event.id().as_ref() == "web-address" {
        // No pane: the menu does not know which page had the keys. The renderer
        // focuses the address that is on screen.
        let _ = app.emit_to(label.as_str(), "web://focus-address", serde_json::json!({}));
        return;
    }
    let Some(action) = action_of(event.id().as_ref()) else {
        return;
    };
    // UI-only command: no model, tool, session or engine execution crosses this boundary.
    if let Err(error) = app.emit_to(label.as_str(), "desktop-tab-action", action) {
        eprintln!("Unable to deliver desktop tab action: {error}");
    }
}
