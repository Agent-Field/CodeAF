//! Opens a new window focused on one tab.
//!
//! "Move to new window" and dragging a tab out of the strip both ask for this.
//! A window is a view of a place, so the new window opens on the place it was
//! given — the same place the tab already lives in — and the tab stays in the
//! shared set. Nothing is copied and nothing is closed. A drag supplies the
//! drop point; the menu does not, and the window then cascades. Once the new
//! view has finished loading, only that window is told which tab to show.
//! A web tab is included: the window that is showing it is the one that has
//! its page, so focusing the tab there is what puts the page in the new window.

use std::sync::atomic::{AtomicBool, Ordering};
use std::sync::Arc;

use serde::{Deserialize, Serialize};
use tauri::webview::PageLoadEvent;
use tauri::{AppHandle, Emitter, EventTarget, Runtime, Webview, WebviewWindow};

use crate::windows::{self, OpenRequest, OpenResult, Point};

/// The new window listens for this and focuses the named tab. No other window
/// is a target: a neighbour on the same place keeps the tab it was already showing.
pub const FOCUS_EVENT: &str = "window://focus-tab";

#[derive(Debug, Deserialize, PartialEq)]
#[serde(deny_unknown_fields, rename_all = "camelCase")]
pub struct MoveToWindow {
    pub tab_id: String,
    pub place_key: String,
    /// Logical screen coordinates of the drop. Absent for the menu, which cascades.
    #[serde(default)]
    pub at: Option<Point>,
}

#[derive(Clone, Debug, PartialEq, Eq, Serialize)]
#[serde(rename_all = "camelCase")]
pub struct FocusTab {
    pub tab_id: String,
}

/// Refuses a tab, a place or a drop point that must not open a window.
/// Callers test this without building one.
pub fn validate(request: &MoveToWindow) -> Result<(), String> {
    windows::checked_focus_tab(&request.tab_id)?;
    windows::checked_place(&request.place_key)?;
    if request.at.is_some_and(|point| !point.finite()) {
        return Err(windows::BAD_POSITION.into());
    }
    Ok(())
}

/// The view is ready when its first load has finished. Started is too early:
/// the page has not registered its listener yet. A later load must not steal
/// the focus back, so only the first Finished counts.
pub(crate) fn take_ready(event: PageLoadEvent, sent: &AtomicBool) -> bool {
    event == PageLoadEvent::Finished && !sent.swap(true, Ordering::SeqCst)
}

/// Tells one window which tab to show. The renderer listens with
/// `WebviewWindow.listen`, which is this target, so a neighbour listening the
/// same way does not hear it.
pub(crate) fn emit_focus<R: Runtime>(window: &WebviewWindow<R>, tab_id: &str) {
    let _ = window.emit_to(
        EventTarget::webview_window(window.label()),
        FOCUS_EVENT,
        FocusTab {
            tab_id: tab_id.to_string(),
        },
    );
}

fn label_of(opened: OpenResult) -> String {
    match opened {
        OpenResult::Label(label) | OpenResult::Handoff(windows::Opened { label, .. }) => label,
    }
}

#[tauri::command]
pub async fn tab_move_to_window<R: Runtime>(
    app: AppHandle<R>,
    webview: Webview<R>,
    request: MoveToWindow,
) -> Result<String, String> {
    windows::trusted(&webview)?;
    validate(&request)?;
    let tab_id = request.tab_id.clone();
    let sent = Arc::new(AtomicBool::new(false));
    let opened = windows::open_placed(
        &app,
        &webview,
        OpenRequest {
            place_key: request.place_key,
            focus_tab: Some(tab_id.clone()),
            handoff: None,
            at: request.at,
        },
        move |window, payload| {
            if take_ready(payload.event(), &sent) {
                emit_focus(&window, &tab_id);
            }
        },
    )?;
    Ok(label_of(opened))
}

#[cfg(test)]
mod tests {
    use super::*;

    fn request(value: serde_json::Value) -> Result<MoveToWindow, String> {
        serde_json::from_value(value).map_err(|error| error.to_string())
    }

    #[test]
    fn a_tab_a_place_and_a_drop_point_are_checked_before_any_window() {
        let ok = request(serde_json::json!({
            "tabId": "tab-1",
            "placeKey": "now",
            "at": { "x": 40, "y": 80 }
        }))
        .unwrap();
        assert!(validate(&ok).is_ok());
        assert!(validate(&MoveToWindow {
            place_key: "p-0123456789ab".into(),
            ..ok
        })
        .is_ok());

        for (tab_id, place_key, at) in [
            ("", "now", None),
            ("x&place=other", "now", None),
            ("tab id", "now", None),
            ("a/b", "now", None),
            (&"t".repeat(129), "now", None),
            ("tab-1", "root", None),
            ("tab-1", "pl_0123456789abcdef", None),
            ("tab-1", "now&token=x", None),
            (
                "tab-1",
                "now",
                Some(Point {
                    x: f64::NAN,
                    y: 0.0,
                }),
            ),
            ("tab-1", "now", Some(Point { x: 1e9, y: 0.0 })),
        ] {
            let refused = validate(&MoveToWindow {
                tab_id: tab_id.into(),
                place_key: place_key.into(),
                at,
            });
            assert!(refused.is_err(), "{tab_id} {place_key}");
        }
    }

    #[test]
    fn unknown_fields_never_become_a_move() {
        for extra in [
            serde_json::json!({ "tabId": "tab-1", "placeKey": "now", "token": "secret" }),
            serde_json::json!({ "tabId": "tab-1", "placeKey": "now", "url": "https://evil.example" }),
            serde_json::json!({ "tabId": "tab-1", "placeKey": "now", "handoff": { "kind": "web" } }),
        ] {
            assert!(request(extra).is_err());
        }
        assert!(request(serde_json::json!({ "placeKey": "now" })).is_err());
        let menu = request(serde_json::json!({ "tabId": "tab-1", "placeKey": "now" })).unwrap();
        assert_eq!(menu.at, None);
        assert!(validate(&menu).is_ok());
    }

    #[test]
    fn the_focus_notice_names_only_the_tab_and_waits_for_the_first_finished_load() {
        let notice = FocusTab {
            tab_id: "tab-1".into(),
        };
        assert_eq!(
            serde_json::to_value(&notice).unwrap(),
            serde_json::json!({ "tabId": "tab-1" })
        );
        let sent = AtomicBool::new(false);
        assert!(!take_ready(PageLoadEvent::Started, &sent));
        assert!(
            !sent.load(Ordering::SeqCst),
            "a start must not consume the notice"
        );
        assert!(take_ready(PageLoadEvent::Finished, &sent));
        assert!(!take_ready(PageLoadEvent::Finished, &sent));
        assert!(!take_ready(PageLoadEvent::Started, &sent));
    }
}

/// The command driven through Tauri's real IPC path. The mock runtime does not
/// finish a page load, so the notice itself is delivered by `emit_focus` and
/// the command's hook is the `take_ready` gate tested above.
#[cfg(test)]
mod ipc_tests {
    use super::*;
    use std::time::Duration;
    use tauri::ipc::{CallbackFn, InvokeBody};
    use tauri::test::{get_ipc_response, mock_builder, mock_context, noop_assets, MockRuntime};
    use tauri::webview::InvokeRequest;
    use tauri::{Listener, Manager, WebviewWindowBuilder};

    fn app() -> tauri::App<MockRuntime> {
        let mut context = mock_context(noop_assets());
        context
            .config_mut()
            .app
            .windows
            .push(tauri::utils::config::WindowConfig {
                label: "main".into(),
                width: 1200.0,
                height: 800.0,
                ..Default::default()
            });
        mock_builder()
            .manage(windows::Windows::default())
            .invoke_handler(tauri::generate_handler![tab_move_to_window])
            .build(context)
            .unwrap()
    }

    fn window(app: &tauri::App<MockRuntime>, label: &str) -> WebviewWindow<MockRuntime> {
        WebviewWindowBuilder::new(app, label, Default::default())
            .build()
            .unwrap()
    }

    const LOCAL: &str = "tauri://localhost";

    fn call(
        from: &WebviewWindow<MockRuntime>,
        body: serde_json::Value,
    ) -> Result<serde_json::Value, serde_json::Value> {
        get_ipc_response(
            from,
            InvokeRequest {
                cmd: "tab_move_to_window".into(),
                callback: CallbackFn(0),
                error: CallbackFn(1),
                url: LOCAL.parse().unwrap(),
                body: InvokeBody::Json(body),
                headers: Default::default(),
                invoke_key: tauri::test::INVOKE_KEY.to_string(),
            },
        )
        .map(|b| b.deserialize::<serde_json::Value>().unwrap())
    }

    #[test]
    fn the_menu_and_a_drop_each_open_a_window_on_that_place_for_that_tab() {
        let app = app();
        let main = window(&app, "main");
        let menu = call(
            &main,
            serde_json::json!({ "request": { "tabId": "tab-1", "placeKey": "now" } }),
        )
        .unwrap();
        assert_eq!(menu, "w-2");
        let opened = app
            .get_webview_window("w-2")
            .expect("the menu opened a window");
        let url = opened.url().unwrap().to_string();
        assert!(url.contains("place=now"), "{url}");
        assert!(url.contains("tab=tab-1"), "{url}");

        let dragged = call(
            &main,
            serde_json::json!({
                "request": {
                    "tabId": "web_1",
                    "placeKey": "p-0123456789ab",
                    "at": { "x": 40, "y": 80 }
                }
            }),
        )
        .unwrap();
        assert_eq!(dragged, "w-3");
        let torn = app
            .get_webview_window("w-3")
            .expect("the drag opened a window");
        let torn_url = torn.url().unwrap().to_string();
        assert!(torn_url.contains("place=p-0123456789ab"), "{torn_url}");
        assert!(torn_url.contains("tab=web_1"), "{torn_url}");
    }

    #[test]
    fn the_focus_notice_reaches_only_the_new_window() {
        let app = app();
        let main = window(&app, "main");
        let label = call(
            &main,
            serde_json::json!({ "request": { "tabId": "tab-1", "placeKey": "now" } }),
        )
        .unwrap();
        let opened = app.get_webview_window(label.as_str().unwrap()).unwrap();
        let (tx, rx) = std::sync::mpsc::channel();
        let source = tx.clone();
        main.listen(FOCUS_EVENT, move |event| {
            let _ = source.send(format!("main:{}", event.payload()));
        });
        let target = tx.clone();
        opened.listen(FOCUS_EVENT, move |event| {
            let _ = target.send(format!("opened:{}", event.payload()));
        });
        emit_focus(&opened, "tab-1");
        assert_eq!(
            rx.recv_timeout(Duration::from_secs(1)).unwrap(),
            r#"opened:{"tabId":"tab-1"}"#
        );
        assert!(rx.try_recv().is_err(), "the source window must not hear it");
    }

    #[test]
    fn a_bad_request_or_a_web_page_opens_nothing() {
        let app = app();
        let main = window(&app, "main");
        let stranger = window(&app, "web-p1");
        for body in [
            serde_json::json!({ "request": { "tabId": "", "placeKey": "now" } }),
            serde_json::json!({ "request": { "tabId": "tab-1", "placeKey": "root" } }),
            serde_json::json!({ "request": { "tabId": "tab-1", "placeKey": "now", "at": { "x": 1e12, "y": 0 } } }),
            serde_json::json!({ "request": { "tabId": "tab-1", "placeKey": "now", "token": "t" } }),
            serde_json::json!({ "tabId": "tab-1", "placeKey": "now" }),
        ] {
            assert!(call(&main, body).is_err());
        }
        assert_eq!(
            call(
                &stranger,
                serde_json::json!({ "request": { "tabId": "tab-1", "placeKey": "now" } }),
            ),
            Err(serde_json::json!(windows::UNTRUSTED))
        );
        assert_eq!(app.webview_windows().len(), 2);
    }
}
