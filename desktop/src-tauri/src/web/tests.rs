//! The isolation proof, run against Tauri's own IPC gate with this app's real
//! `tauri.conf.json` and `capabilities/`. Each request goes through
//! `Webview::on_message`, the function every page's `invoke` reaches.

use super::*;
use tauri::ipc::{CallbackFn, InvokeBody};
use tauri::test::{get_ipc_response, mock_builder, MockRuntime, INVOKE_KEY};
use tauri::webview::InvokeRequest;
use tauri::{App, WebviewWindowBuilder};

/// Stands in for every app command a page might want (engine_connection is
/// one): reaching it means the gate let the caller through.
#[tauri::command]
fn reached() -> &'static str {
    "reached"
}

/// `get_ipc_response` takes anything that is a webview; a bare `Webview` is not `AsRef` of itself.
struct View(Webview<MockRuntime>);
impl AsRef<Webview<MockRuntime>> for View {
    fn as_ref(&self) -> &Webview<MockRuntime> {
        &self.0
    }
}

fn app() -> App<MockRuntime> {
    mock_builder()
        .invoke_handler(tauri::generate_handler![reached, web_list, web_open])
        .build(tauri::generate_context!(test = true))
        .expect("mock app")
}

fn request(cmd: &str, origin: &str) -> InvokeRequest {
    InvokeRequest {
        cmd: cmd.into(),
        callback: CallbackFn(0),
        error: CallbackFn(1),
        url: origin.parse().unwrap(),
        body: InvokeBody::default(),
        headers: Default::default(),
        invoke_key: INVOKE_KEY.to_string(),
    }
}

/// The main window plus one child web view on it, as `web_open` builds it.
fn main_with_child(app: &App<MockRuntime>) -> (View, View) {
    let main = WebviewWindowBuilder::new(app, "main", Default::default())
        .build()
        .unwrap();
    let child = main
        .as_ref()
        .window()
        .add_child(
            WebviewBuilder::new(
                "web-p1",
                WebviewUrl::External("https://example.com".parse().unwrap()),
            ),
            LogicalPosition::new(0, 0),
            LogicalSize::new(100, 100),
        )
        .unwrap();
    (View(main.as_ref().clone()), View(child))
}

const PAGE: &str = "https://example.com";
const APP: &str = "tauri://localhost";

#[test]
fn a_web_page_reaches_no_command_and_no_plugin() {
    let app = app();
    let (_, child) = main_with_child(&app);
    for cmd in [
        "reached",
        "web_list",
        "web_open",
        "engine_connection",
        "plugin:event|listen",
        "plugin:shell|spawn",
    ] {
        let refused = get_ipc_response(&child, request(cmd, PAGE));
        assert!(refused.is_err(), "{cmd} was answered for a remote page");
    }
}

#[test]
fn the_origin_not_the_label_is_the_gate() {
    let app = app();
    let (main, _) = main_with_child(&app);
    // Even the main webview is refused while it shows a page that is not the app.
    assert!(get_ipc_response(&main, request("reached", PAGE)).is_err());
    assert!(get_ipc_response(&main, request("reached", APP)).is_ok());
}

#[test]
fn a_child_on_an_app_origin_would_be_trusted_so_navigation_must_never_get_there() {
    let app = app();
    let (_, child) = main_with_child(&app);
    // The capability names the window "main", and Tauri matches window labels
    // as well as webview labels, so a child on an app origin passes the ACL.
    let reached = get_ipc_response(&child, request("reached", APP));
    assert!(
        reached.is_ok(),
        "window-label capability semantics changed; re-read policy.rs"
    );
    // The web commands still refuse it: only a top-level app webview is a caller.
    let refused = get_ipc_response(&child, request("web_list", APP));
    assert!(refused.is_err());
    // And no navigation can get it there.
    let origins = AppOrigins::new(app.config().build.dev_url.as_ref());
    for target in [
        APP,
        "http://tauri.localhost/",
        "ipc://localhost/reached",
        "http://localhost:1420/",
    ] {
        assert!(
            !policy::allows_navigation(&target.parse().unwrap(), &origins),
            "{target}"
        );
    }
}

#[test]
fn the_main_window_lists_only_its_own_views() {
    let app = app();
    let (main, _) = main_with_child(&app);
    let listed = get_ipc_response(&main, request("web_list", APP)).expect("main may list");
    assert_eq!(
        listed
            .deserialize::<Vec<serde_json::Value>>()
            .unwrap()
            .len(),
        0
    );
}

/// Plugin calls are always checked against the capability files, so this is
/// where the real `capabilities/default.json` shows its scope.
fn acl_refused(view: &View, origin: &str) -> bool {
    match get_ipc_response(view, request("plugin:event|listen", origin)) {
        Ok(_) => false,
        Err(error) => error.to_string().contains("not allowed"),
    }
}

#[test]
fn the_real_capability_covers_the_main_window_and_nothing_remote() {
    let app = app();
    let (main, child) = main_with_child(&app);
    assert!(
        !acl_refused(&main, APP),
        "the main webview lost core:default"
    );
    assert!(acl_refused(&main, PAGE));
    assert!(
        acl_refused(&child, PAGE),
        "a web page reached the event plugin"
    );
    // Window-label semantics: a child of `main` on an app origin is covered.
    assert!(!acl_refused(&child, APP));
    // A window the capability does not name is not, even on an app origin.
    let other = WebviewWindowBuilder::new(&app, "other", Default::default())
        .build()
        .unwrap();
    assert!(acl_refused(&View(other.as_ref().clone()), APP));
}
