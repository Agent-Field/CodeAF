//! Hide every web view on the calling window while a menu, palette or other
//! overlay is up, and show again only the panes that are still open.
//!
//! A native child view is painted above the DOM, so the page would cover the
//! menu. The renderer keeps a refcount (`holdWebOverlay`) and calls these two
//! commands at the edges: once when the count leaves zero, once when it
//! returns. A second hide while one is already in force does nothing, and a
//! second show does nothing, so a nested menu cannot flash the page back.
//!
//! A pane closed while the hold is up is not in the show list, and even if it
//! were, its view is gone. Show never recreates it.

use std::collections::HashMap;
use std::sync::{Mutex, OnceLock};

use tauri::{Runtime, Webview};

struct Hold {
    /// A hide has been applied and the matching show has not.
    active: bool,
    /// Panes that were on screen when the hide ran. The show list is the
    /// renderer's; this set is only the record of what we concealed.
    panes: Vec<String>,
}

fn holds() -> &'static Mutex<HashMap<String, Hold>> {
    static HOLDS: OnceLock<Mutex<HashMap<String, Hold>>> = OnceLock::new();
    HOLDS.get_or_init(|| Mutex::new(HashMap::new()))
}

/// True while this window's web views are held down by an overlay. A poisoned
/// lock stays held: painting the page over a menu is the worse failure.
pub(crate) fn is_holding(window: &str) -> bool {
    match holds().lock() {
        Ok(map) => map.get(window).is_some_and(|hold| hold.active),
        Err(_) => true,
    }
}

/// Starts a hold. False when one is already active, so the caller hides once.
fn begin_hold(active: &mut bool) -> bool {
    if *active {
        return false;
    }
    *active = true;
    true
}

/// Ends a hold. False when none is active, so a repeated show does nothing.
fn end_hold(active: &mut bool) -> bool {
    if !*active {
        return false;
    }
    *active = false;
    true
}

/// The panes from `wanted` that still have a view. A pane closed while the
/// overlay was up is absent from `alive` and is not returned.
pub(crate) fn panes_to_reveal(wanted: &[String], alive: impl Fn(&str) -> bool) -> Vec<String> {
    wanted.iter().filter(|pane| alive(pane)).cloned().collect()
}

/// Hides every web view on the caller's window that is currently on screen.
#[tauri::command]
pub async fn web_hide_all<R: Runtime>(caller: Webview<R>) -> Result<(), String> {
    let window = caller.window().label().to_string();
    {
        let mut map = holds().lock().map_err(|_| "Web pages are unavailable")?;
        let hold = map.entry(window.clone()).or_insert(Hold {
            active: false,
            panes: Vec::new(),
        });
        // Set before the snapshot so a show that races the hide cannot win.
        if !begin_hold(&mut hold.active) {
            return Ok(());
        }
    }
    let panes = match crate::web::shown_panes(&caller) {
        Ok(panes) => panes,
        Err(err) => {
            if let Ok(mut map) = holds().lock() {
                if let Some(hold) = map.get_mut(&window) {
                    hold.active = false;
                }
            }
            return Err(err);
        }
    };
    if let Ok(mut map) = holds().lock() {
        if let Some(hold) = map.get_mut(&window) {
            hold.panes = panes.clone();
        }
    }
    for pane in panes {
        let _ = crate::web::conceal_pane(&caller, &pane)?;
    }
    Ok(())
}

/// Shows the panes the renderer still wants. One that was closed is skipped.
/// A call with no hold in force does nothing, so a repeated release is safe.
#[tauri::command]
pub async fn web_show_all<R: Runtime>(
    caller: Webview<R>,
    panes: Vec<String>,
) -> Result<(), String> {
    let window = caller.window().label().to_string();
    {
        let mut map = holds().lock().map_err(|_| "Web pages are unavailable")?;
        let Some(hold) = map.get_mut(&window) else {
            return Ok(());
        };
        if !end_hold(&mut hold.active) {
            return Ok(());
        }
        hold.panes.clear();
    }
    // The hold flag is down before any show, so a pane opened during the
    // overlay can be revealed here and is not blocked by `web_visible`.
    let mut living = Vec::new();
    for pane in &panes {
        if crate::web::view_is_open(&caller, pane)? {
            living.push(pane.clone());
        }
    }
    for pane in panes_to_reveal(&panes, |pane| living.iter().any(|open| open == pane)) {
        let _ = crate::web::reveal_pane(&caller, &pane)?;
    }
    Ok(())
}

#[cfg(test)]
mod tests {
    use super::{begin_hold, end_hold, panes_to_reveal};

    #[test]
    fn hide_all_is_idempotent_and_show_all_is_idempotent() {
        let mut active = false;
        assert!(begin_hold(&mut active));
        assert!(
            !begin_hold(&mut active),
            "a nested hide must not hide again"
        );
        assert!(end_hold(&mut active));
        assert!(!end_hold(&mut active), "a second show must not show again");
    }

    #[test]
    fn a_pane_closed_while_held_is_not_re_shown() {
        let wanted = vec!["a".to_string(), "b".to_string()];
        let revealed = panes_to_reveal(&wanted, |pane| pane == "b");
        assert_eq!(revealed, vec!["b".to_string()]);
    }
}
