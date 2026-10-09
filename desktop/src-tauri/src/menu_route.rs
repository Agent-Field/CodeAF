//! Pure routing for native menu commands, kept off the macOS gate so Linux CI tests it.
#![cfg_attr(not(target_os = "macos"), allow(dead_code))]

#[derive(Clone, Debug, PartialEq, serde::Serialize)]
#[serde(rename_all = "kebab-case")]
pub enum TabAction {
    New,
    Close,
    CloseStop,
    Reopen,
    Overview,
    Next,
    Previous,
    Settings,
    Focus,
    Sidebar,
    History,
    NewWindow,
}

/// The menu item ids that become UI-only actions. `window-close` is handled natively.
pub fn action_of(id: &str) -> Option<TabAction> {
    Some(match id {
        "tab-new" => TabAction::New,
        "tab-close" => TabAction::Close,
        "tab-close-stop" => TabAction::CloseStop,
        "tab-reopen" => TabAction::Reopen,
        "tab-overview" => TabAction::Overview,
        "tab-next" => TabAction::Next,
        "tab-previous" => TabAction::Previous,
        "app-settings" => TabAction::Settings,
        "view-focus" => TabAction::Focus,
        "view-sidebar" => TabAction::Sidebar,
        "view-history" => TabAction::History,
        "window-new" => TabAction::NewWindow,
        _ => return None,
    })
}

/// The window that owns a menu command: the focused one, else the first (main before w-N).
/// A command never lands in a window the person is not looking at.
pub fn target_label(focused: Option<String>, mut open: Vec<String>) -> Option<String> {
    focused.or_else(|| {
        open.sort_by_key(|label| (label != "main", label.len(), label.clone()));
        open.into_iter().next()
    })
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn every_menu_item_routes_to_one_typed_action() {
        for (id, wire) in [
            ("tab-close-stop", "close-stop"),
            ("app-settings", "settings"),
            ("view-focus", "focus"),
            ("view-sidebar", "sidebar"),
            ("view-history", "history"),
            ("window-new", "new-window"),
            ("tab-next", "next"),
        ] {
            let action = action_of(id).expect(id);
            assert_eq!(serde_json::to_value(&action).unwrap(), wire);
        }
        assert_eq!(action_of("window-close"), None);
        assert_eq!(action_of("unknown"), None);
    }

    #[test]
    fn a_command_goes_to_the_focused_window_not_main() {
        let open = vec!["main".to_string(), "w-2".to_string()];
        assert_eq!(
            target_label(Some("w-2".into()), open.clone()).as_deref(),
            Some("w-2")
        );
        assert_eq!(
            target_label(None, vec!["w-3".into(), "w-2".into(), "main".into()]).as_deref(),
            Some("main")
        );
        assert_eq!(
            target_label(None, vec!["w-10".into(), "w-2".into()]).as_deref(),
            Some("w-2")
        );
        assert_eq!(target_label(None, vec![]), None);
    }
}
