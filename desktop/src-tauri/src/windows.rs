//! Native multiwindow: open a window on a place, move a tab into another window,
//! list, focus and title windows.
//!
//! A window is a view, like a tab. Closing one never touches the engine, which is
//! app-global, so running work keeps running. A moved tab travels as a typed,
//! bounded `TabHandoff` through Rust, never through a URL or shared storage, and
//! the source window only removes its tab after the target has claimed it.

use std::collections::HashMap;
use std::sync::Mutex;
use std::time::{Duration, Instant};

use serde::{Deserialize, Serialize};
use tauri::{
    AppHandle, Emitter, LogicalPosition, Manager, Runtime, Webview, WebviewUrl, WebviewWindow,
    WebviewWindowBuilder, Window,
};

pub const UNTRUSTED: &str = "Only codeaf's own windows can do that";
const NOT_A_PLACE: &str = "That is not a place codeaf knows";
const NO_WINDOW: &str = "That window is no longer open";
const BAD_TAB: &str = "That tab cannot move to another window";
const BAD_TITLE: &str = "That window title cannot be shown";

/// How long a handoff waits for its window to claim it. A window that never
/// loads must not hold a tab hostage, and the source keeps the tab meanwhile.
const HANDOFF_TTL: Duration = Duration::from_secs(60);
/// New windows step down and right from the window that opened them.
const CASCADE: f64 = 24.0;
const MAX_TITLE: usize = 200;
const MAX_WINDOW_TITLE: usize = 120;
const MAX_DRAFT: usize = 64 * 1024;
const MAX_PATH: usize = 4096;
const MAX_ID: usize = 256;
const MAX_ROUTE: usize = 64;
const MAX_URL: usize = 8192;

/// The one trust rule for every native-controls command: the caller is a
/// top-level codeaf webview (`main` or `w-<digits>`) sitting in the window of the
/// same label. A web page's child view (`web-*`) never passes, even though it
/// lives inside `main`. This is the same predicate the native web lane uses.
pub fn is_trusted_caller(webview_label: &str, window_label: &str) -> bool {
    is_app_window(webview_label) && webview_label == window_label
}

/// `main` or `w-` followed by one or more ASCII digits, nothing else.
pub fn is_app_window(label: &str) -> bool {
    label == "main"
        || label
            .strip_prefix("w-")
            .is_some_and(|n| !n.is_empty() && n.bytes().all(|b| b.is_ascii_digit()))
}

/// codeaf's own window called `label`, if it is open. NEVER look a window up
/// with `get_webview_window` or `webview_windows`: Tauri counts a window as a
/// webview window only while its own webview is its only one, so a window
/// showing a web page (a `web-*` child view) dropped out of both, and a link
/// would have nowhere to land. Adopted from the verified linux-native lookup.
pub fn app_window<R: Runtime>(app: &AppHandle<R>, label: &str) -> Option<Window<R>> {
    is_app_window(label)
        .then(|| app.get_window(label))
        .flatten()
}

/// Every open codeaf window by label, web pages or not (see `app_window`).
pub fn app_windows<R: Runtime>(app: &AppHandle<R>) -> Vec<(String, Window<R>)> {
    app.windows()
        .into_iter()
        .filter(|(label, _)| is_app_window(label))
        .collect()
}

/// Returns the caller's label when it is one of codeaf's own windows.
/// codeaf's own window called `label`, if it is open. NEVER look a window up
/// with `get_webview_window` or `webview_windows`: Tauri counts a window as a
/// webview window only while its own webview is its only one, so a window
/// showing a web page (a `web-*` child view) dropped out of both, and moving a
/// tab, focusing, listing and notifying all said the window was gone.
pub fn app_window<R: Runtime>(app: &AppHandle<R>, label: &str) -> Option<Window<R>> {
    is_app_window(label)
        .then(|| app.get_window(label))
        .flatten()
}

/// Every open codeaf window by label, web pages or not (see `app_window`).
pub fn app_windows<R: Runtime>(app: &AppHandle<R>) -> Vec<(String, Window<R>)> {
    app.windows()
        .into_iter()
        .filter(|(label, _)| is_app_window(label))
        .collect()
}

pub fn trusted<R: Runtime>(caller: &Webview<R>) -> Result<String, String> {
    let window = caller.window();
    if is_trusted_caller(caller.label(), window.label()) {
        Ok(caller.label().to_string())
    } else {
        Err(UNTRUSTED.into())
    }
}

/// `now`, `root`, or a placegraph id: `pl_` and sixteen lowercase hex digits.
pub fn checked_place(key: &str) -> Result<&str, String> {
    let id = key.strip_prefix("pl_").is_some_and(|hex| {
        hex.len() == 16 && hex.bytes().all(|b| matches!(b, b'0'..=b'9' | b'a'..=b'f'))
    });
    if key == "now" || key == "root" || id {
        Ok(key)
    } else {
        Err(NOT_A_PLACE.into())
    }
}

fn plain(text: &str) -> bool {
    !text.chars().any(char::is_control)
}

fn bounded(text: &str, max: usize) -> bool {
    text.len() <= max && plain(text)
}

fn id_like(text: &str) -> bool {
    !text.is_empty() && bounded(text, MAX_ID)
}

/// Only http(s) with a host and no `user:password@`; a credential never rides
/// along with a moved tab.
fn safe_web_url(url: &str) -> bool {
    let Some(rest) = url
        .strip_prefix("https://")
        .or_else(|| url.strip_prefix("http://"))
    else {
        return false;
    };
    let authority = rest.split(['/', '?', '#']).next().unwrap_or("");
    let host = authority.split(':').next().unwrap_or("");
    url.len() <= MAX_URL
        && !authority.contains('@')
        && !host.is_empty()
        && url.chars().all(|c| !c.is_control() && !c.is_whitespace())
}

#[derive(Clone, Copy, Debug, PartialEq, Eq, Serialize, Deserialize)]
#[serde(rename_all = "lowercase")]
pub enum TabKind {
    Conversation,
    Task,
    File,
    Diff,
    Web,
    Terminal,
    Settings,
    History,
    Newtab,
    Inbox,
}

#[derive(Clone, Copy, Debug, PartialEq, Eq, Serialize, Deserialize)]
#[serde(rename_all = "lowercase")]
pub enum TitleSource {
    Message,
    Engine,
    Manual,
}

#[derive(Clone, Copy, Debug, PartialEq, Eq, Serialize, Deserialize)]
#[serde(rename_all = "lowercase")]
pub enum FileView {
    Changes,
    File,
}

#[derive(Clone, Debug, PartialEq, Serialize, Deserialize)]
#[serde(deny_unknown_fields)]
pub struct FileTarget {
    pub path: String,
    #[serde(default, skip_serializing_if = "Option::is_none")]
    pub view: Option<FileView>,
}

#[derive(Clone, Debug, PartialEq, Serialize, Deserialize)]
#[serde(deny_unknown_fields, rename_all = "camelCase")]
pub struct TabRoute {
    #[serde(default, skip_serializing_if = "Option::is_none")]
    pub task_id: Option<String>,
    pub back: Vec<String>,
    pub forward: Vec<String>,
}

/// What a tab points at. `shot` (a captured image) is deliberately not part of
/// the shape: the target window draws its own.
#[derive(Clone, Debug, Default, PartialEq, Serialize, Deserialize)]
#[serde(deny_unknown_fields, rename_all = "camelCase")]
pub struct PaneTarget {
    #[serde(default, skip_serializing_if = "Option::is_none")]
    pub session_id: Option<String>,
    #[serde(default, skip_serializing_if = "Option::is_none")]
    pub path: Option<String>,
    #[serde(default, skip_serializing_if = "Option::is_none")]
    pub terminal_id: Option<String>,
    #[serde(default, skip_serializing_if = "Option::is_none")]
    pub url: Option<String>,
}

/// One tab, as it moves between windows. Unknown fields are refused, so a token,
/// a provider key or a screenshot cannot be smuggled along.
#[derive(Clone, Debug, PartialEq, Serialize, Deserialize)]
#[serde(deny_unknown_fields, rename_all = "camelCase")]
pub struct TabHandoff {
    pub kind: TabKind,
    pub title: String,
    #[serde(default, skip_serializing_if = "Option::is_none")]
    pub draft: Option<String>,
    #[serde(default, skip_serializing_if = "Option::is_none")]
    pub title_source: Option<TitleSource>,
    #[serde(default, skip_serializing_if = "Option::is_none")]
    pub session_file: Option<String>,
    #[serde(default, skip_serializing_if = "Option::is_none")]
    pub path: Option<String>,
    #[serde(default, skip_serializing_if = "Option::is_none")]
    pub file: Option<FileTarget>,
    #[serde(default, skip_serializing_if = "Option::is_none")]
    pub route: Option<TabRoute>,
    #[serde(default, skip_serializing_if = "Option::is_none")]
    pub target: Option<PaneTarget>,
}

fn relative_path(path: &str) -> bool {
    !path.is_empty() && bounded(path, MAX_PATH)
}

impl TabHandoff {
    pub fn check(&self) -> Result<(), String> {
        let ok = bounded(&self.title, MAX_TITLE)
            // A draft is the person's own words, so newlines and tabs are kept.
            && self.draft.as_deref().is_none_or(|d| d.len() <= MAX_DRAFT && !d.contains('\0'))
            && self.session_file.as_deref().is_none_or(|f| {
                bounded(f, MAX_PATH) && std::path::Path::new(f).is_absolute()
            })
            && self.path.as_deref().is_none_or(relative_path)
            && self.file.as_ref().is_none_or(|f| relative_path(&f.path))
            && self.route.as_ref().is_none_or(|r| {
                r.task_id.as_deref().is_none_or(id_like)
                    && r.back.len() <= MAX_ROUTE
                    && r.forward.len() <= MAX_ROUTE
                    // '' in the stacks is the conversation itself.
                    && r.back.iter().chain(&r.forward).all(|id| bounded(id, MAX_ID))
            })
            && self.target.as_ref().is_none_or(|t| {
                t.session_id.as_deref().is_none_or(id_like)
                    && t.terminal_id.as_deref().is_none_or(id_like)
                    && t.path.as_deref().is_none_or(relative_path)
                    && t.url.as_deref().is_none_or(safe_web_url)
            });
        if ok {
            Ok(())
        } else {
            Err(BAD_TAB.into())
        }
    }
}

#[derive(Clone, Copy, Debug, PartialEq, Deserialize, Serialize)]
pub struct Point {
    pub x: f64,
    pub y: f64,
}

impl Point {
    fn finite(self) -> bool {
        self.x.is_finite() && self.y.is_finite() && self.x.abs() < 1e6 && self.y.abs() < 1e6
    }
}

struct Pending {
    id: String,
    from: String,
    to: String,
    tab: TabHandoff,
    at: Instant,
}

#[derive(Default)]
struct Book {
    /// Monotonic: a label is never reused, even after its window closed.
    next: u64,
    handoffs: u64,
    places: HashMap<String, String>,
    pending: Vec<Pending>,
}

impl Book {
    fn allocate(&mut self) -> String {
        self.next += 1;
        format!("w-{}", self.next + 1)
    }

    fn sweep(&mut self, now: Instant) {
        self.pending
            .retain(|p| now.duration_since(p.at) < HANDOFF_TTL);
    }

    fn park(&mut self, from: &str, to: &str, tab: TabHandoff, now: Instant) -> String {
        self.sweep(now);
        self.handoffs += 1;
        let id = format!("h-{}", self.handoffs);
        self.pending.push(Pending {
            id: id.clone(),
            from: from.into(),
            to: to.into(),
            tab,
            at: now,
        });
        id
    }

    /// Hands the oldest handoff addressed to `to` over, once.
    fn claim(&mut self, to: &str, now: Instant) -> Option<Pending> {
        self.sweep(now);
        let index = self.pending.iter().position(|p| p.to == to)?;
        Some(self.pending.remove(index))
    }

    fn forget(&mut self, label: &str) {
        self.places.remove(label);
        self.pending.retain(|p| p.to != label);
    }
}

#[derive(Default)]
pub struct Windows(Mutex<Book>);

fn book<R: Runtime>(app: &AppHandle<R>) -> Result<std::sync::MutexGuard<'_, Book>, String> {
    app.state::<Windows>()
        .inner()
        .0
        .lock()
        .map_err(|_| "Windows are unavailable".to_string())
}

/// Called from the app's window-event hook so closed windows leave no state.
pub fn on_destroyed<R: Runtime>(app: &AppHandle<R>, label: &str) {
    if let Ok(mut book) = book(app) {
        book.forget(label);
    }
}

fn place_of<R: Runtime>(app: &AppHandle<R>, label: &str) -> String {
    book(app)
        .ok()
        .and_then(|b| b.places.get(label).cloned())
        .unwrap_or_else(|| "now".into())
}

fn window_url(place: &str) -> WebviewUrl {
    WebviewUrl::App(format!("index.html?place={place}").into())
}

#[derive(Debug, Deserialize)]
#[serde(deny_unknown_fields, rename_all = "camelCase")]
pub struct OpenRequest {
    pub place_key: String,
    #[serde(default)]
    pub focus_tab: Option<String>,
    #[serde(default)]
    pub handoff: Option<TabHandoff>,
    #[serde(default)]
    pub at: Option<Point>,
}

#[derive(Debug, Serialize)]
#[serde(rename_all = "camelCase")]
pub struct Opened {
    pub label: String,
    #[serde(skip_serializing_if = "Option::is_none")]
    pub handoff_id: Option<String>,
}

/// The new window is built from `main`'s own configuration, so its size,
/// minimum size, macOS overlay title bar and vibrancy come from the one place
/// that is generated from design tokens.
fn build<R: Runtime>(
    app: &AppHandle<R>,
    caller: &Window<R>,
    label: &str,
    place: &str,
    at: Option<Point>,
    focus_tab: Option<&str>,
) -> Result<WebviewWindow<R>, String> {
    let mut config = app
        .config()
        .app
        .windows
        .iter()
        .find(|w| w.label == "main")
        .cloned()
        .ok_or("The main window configuration is missing")?;
    config.label = label.into();
    config.url = match focus_tab {
        Some(id) => WebviewUrl::App(format!("index.html?place={place}&focusTab={id}").into()),
        None => window_url(place),
    };
    config.center = false;
    let position = match at {
        Some(point) => LogicalPosition::new(point.x, point.y),
        None => {
            let scale = caller.scale_factor().unwrap_or(1.0);
            let origin = caller
                .outer_position()
                .map(|p| p.to_logical::<f64>(scale))
                .unwrap_or(LogicalPosition::new(0.0, 0.0));
            LogicalPosition::new(origin.x + CASCADE, origin.y + CASCADE)
        }
    };
    config.x = Some(position.x);
    config.y = Some(position.y);
    WebviewWindowBuilder::from_config(app, &config)
        .map_err(|_| "A new window could not be opened".to_string())?
        .build()
        .map_err(|_| "A new window could not be opened".to_string())
}

#[tauri::command]
pub async fn window_open<R: Runtime>(
    app: AppHandle<R>,
    webview: Webview<R>,
    request: OpenRequest,
) -> Result<Opened, String> {
    let from = trusted(&webview)?;
    let place = checked_place(&request.place_key)?.to_string();
    if request.focus_tab.as_deref().is_some_and(|id| {
        id.is_empty()
            || id.len() > 128
            || !id
                .chars()
                .all(|c| c.is_ascii_alphanumeric() || c == '-' || c == '_')
    }) {
        return Err("That tab cannot be focused".into());
    }
    if let Some(tab) = &request.handoff {
        tab.check()?;
    }
    if request.at.is_some_and(|p| !p.finite()) {
        return Err("That position is outside the screen".into());
    }
    let caller = webview.window();
    let (label, handoff_id) = {
        let mut book = book(&app)?;
        let label = book.allocate();
        let id = request
            .handoff
            .map(|tab| book.park(&from, &label, tab, Instant::now()));
        book.places.insert(label.clone(), place.clone());
        (label, id)
    };
    if let Err(error) = build(
        &app,
        &caller,
        &label,
        &place,
        request.at,
        request.focus_tab.as_deref(),
    ) {
        if let Ok(mut book) = book(&app) {
            book.forget(&label);
        }
        return Err(error);
    }
    Ok(Opened { label, handoff_id })
}

#[derive(Debug, Deserialize)]
#[serde(deny_unknown_fields)]
pub struct MoveRequest {
    pub to: String,
    pub handoff: TabHandoff,
}

#[derive(Clone, Debug, Serialize)]
#[serde(rename_all = "camelCase")]
pub struct HandoffEvent {
    pub handoff_id: String,
}

#[tauri::command]
pub fn window_move_tab<R: Runtime>(
    app: AppHandle<R>,
    webview: Webview<R>,
    request: MoveRequest,
) -> Result<HandoffEvent, String> {
    let from = trusted(&webview)?;
    request.handoff.check()?;
    if !is_app_window(&request.to) || request.to == from {
        return Err(NO_WINDOW.into());
    }
    let target = app_window(&app, &request.to).ok_or(NO_WINDOW.to_string())?;
    let handoff_id = book(&app)?.park(&from, &request.to, request.handoff, Instant::now());
    let event = HandoffEvent { handoff_id };
    let _ = target.emit_to(request.to.as_str(), "window://handoff-ready", event.clone());
    let _ = target.set_focus();
    Ok(event)
}

#[derive(Debug, Serialize)]
#[serde(rename_all = "camelCase")]
pub struct Claimed {
    pub handoff_id: String,
    #[serde(flatten)]
    pub tab: TabHandoff,
}

#[tauri::command]
pub fn window_claim_handoff<R: Runtime>(
    app: AppHandle<R>,
    webview: Webview<R>,
) -> Result<Option<Claimed>, String> {
    let me = trusted(&webview)?;
    let Some(pending) = book(&app)?.claim(&me, Instant::now()) else {
        return Ok(None);
    };
    // Only now may the source drop its tab: the move has landed.
    let _ = app.emit_to(
        pending.from.as_str(),
        "window://handoff-claimed",
        HandoffEvent {
            handoff_id: pending.id.clone(),
        },
    );
    Ok(Some(Claimed {
        handoff_id: pending.id,
        tab: pending.tab,
    }))
}

#[derive(Debug, Serialize)]
#[serde(rename_all = "camelCase")]
pub struct WindowRow {
    pub label: String,
    pub place_key: String,
    pub focused: bool,
    pub title: String,
}

#[tauri::command]
pub fn window_list<R: Runtime>(
    app: AppHandle<R>,
    webview: Webview<R>,
) -> Result<Vec<WindowRow>, String> {
    trusted(&webview)?;
    let mut rows: Vec<WindowRow> = app_windows(&app)
        .into_iter()
        .map(|(label, window)| WindowRow {
            place_key: place_of(&app, &label),
            focused: window.is_focused().unwrap_or(false),
            title: window.title().unwrap_or_default(),
            label,
        })
        .collect();
    rows.sort_by_key(|row| order(&row.label));
    Ok(rows)
}

fn order(label: &str) -> u64 {
    label
        .strip_prefix("w-")
        .and_then(|n| n.parse().ok())
        .unwrap_or(0)
}

#[tauri::command]
pub fn window_focus<R: Runtime>(
    app: AppHandle<R>,
    webview: Webview<R>,
    label: String,
) -> Result<(), String> {
    trusted(&webview)?;
    if !is_app_window(&label) {
        return Err(NO_WINDOW.into());
    }
    let window = app_window(&app, &label).ok_or(NO_WINDOW)?;
    let _ = window.unminimize();
    window
        .set_focus()
        .map_err(|_| "That window could not be brought forward".into())
}

pub fn window_title(title: &str) -> Result<String, String> {
    let title = title.trim();
    if title.chars().count() > MAX_WINDOW_TITLE || !plain(title) {
        return Err(BAD_TITLE.into());
    }
    Ok(if title.is_empty() || title == "codeaf" {
        "codeaf".into()
    } else {
        format!("{title} — codeaf")
    })
}

#[tauri::command]
pub fn window_set_title<R: Runtime>(webview: Webview<R>, title: String) -> Result<(), String> {
    trusted(&webview)?;
    let title = window_title(&title)?;
    webview
        .window()
        .set_title(&title)
        .map_err(|_| BAD_TITLE.into())
}

#[derive(Debug, Serialize)]
#[serde(rename_all = "camelCase")]
pub struct WindowContext {
    pub label: String,
    pub place_key: String,
}

#[tauri::command]
pub fn window_context<R: Runtime>(
    app: AppHandle<R>,
    webview: Webview<R>,
) -> Result<WindowContext, String> {
    let label = trusted(&webview)?;
    Ok(WindowContext {
        place_key: place_of(&app, &label),
        label,
    })
}

#[cfg(test)]
mod tests {
    use super::*;

    fn tab() -> TabHandoff {
        serde_json::from_value(serde_json::json!({
            "kind": "conversation",
            "title": "Fix the parser",
            "draft": "line one\nline two",
            "sessionFile": "/home/me/.codeaf/v3/projects/x/abc/session.jsonl",
            "route": { "taskId": "t1", "back": [""], "forward": [] },
            "target": { "sessionId": "abc", "url": "https://example.com/a?b=1" }
        }))
        .unwrap()
    }

    #[test]
    fn only_codeaf_windows_are_trusted() {
        for (webview, window) in [("main", "main"), ("w-2", "w-2"), ("w-10", "w-10")] {
            assert!(is_trusted_caller(webview, window), "{webview}");
        }
        for (webview, window) in [
            ("web-p1", "main"),
            ("web-main", "main"),
            ("main", "w-2"),
            ("w-2", "main"),
            ("w-", "w-"),
            ("w-x", "w-x"),
            ("w-2a", "w-2a"),
            ("W-2", "W-2"),
            ("other", "other"),
            ("", ""),
        ] {
            assert!(!is_trusted_caller(webview, window), "{webview} in {window}");
        }
    }

    #[test]
    fn place_keys_are_now_root_or_placegraph_ids() {
        for good in ["now", "root", "pl_0123456789abcdef"] {
            assert_eq!(checked_place(good), Ok(good));
        }
        for bad in [
            "",
            "Now",
            "pl_",
            "pl_0123456789ABCDEF",
            "pl_0123456789abcde",
            "pl_0123456789abcdef0",
            "p-0123456789ab",
            "now&token=x",
            "../now",
            "pl_0123456789abcdeg",
        ] {
            assert!(checked_place(bad).is_err(), "{bad}");
        }
    }

    #[test]
    fn labels_are_monotonic_and_never_reused() {
        let mut book = Book::default();
        let a = book.allocate();
        let b = book.allocate();
        assert_eq!((a.as_str(), b.as_str()), ("w-2", "w-3"));
        book.forget(&a);
        assert_eq!(book.allocate(), "w-4");
        assert!(is_app_window(&a) && is_app_window(&b));
    }

    #[test]
    fn a_handoff_is_claimed_once_by_its_own_window() {
        let mut book = Book::default();
        let now = Instant::now();
        let id = book.park("main", "w-2", tab(), now);
        assert!(
            book.claim("w-3", now).is_none(),
            "another window cannot take it"
        );
        let got = book.claim("w-2", now).unwrap();
        assert_eq!((got.id, got.from.as_str()), (id, "main"));
        assert!(book.claim("w-2", now).is_none(), "claimed once");
    }

    #[test]
    fn handoffs_expire_and_die_with_their_window() {
        let mut book = Book::default();
        let then = Instant::now();
        book.park("main", "w-2", tab(), then);
        assert!(book.claim("w-2", then + HANDOFF_TTL).is_none());
        book.park("main", "w-3", tab(), then);
        book.forget("w-3");
        assert!(book.claim("w-3", then).is_none());
    }

    #[test]
    fn a_valid_tab_round_trips() {
        let tab = tab();
        tab.check().unwrap();
        let wire = serde_json::to_value(&tab).unwrap();
        assert_eq!(
            wire["sessionFile"],
            "/home/me/.codeaf/v3/projects/x/abc/session.jsonl"
        );
        assert_eq!(serde_json::from_value::<TabHandoff>(wire).unwrap(), tab);
    }

    #[test]
    fn unknown_fields_and_secrets_are_refused() {
        for extra in [
            serde_json::json!({ "token": "abc" }),
            serde_json::json!({ "pinned": true }),
            serde_json::json!({ "target": { "shot": "data:image/png;base64,AA" } }),
            serde_json::json!({ "kind": "shell" }),
        ] {
            let mut value = serde_json::to_value(tab()).unwrap();
            for (k, v) in extra.as_object().unwrap() {
                value[k] = v.clone();
            }
            assert!(serde_json::from_value::<TabHandoff>(value).is_err());
        }
    }

    #[test]
    fn bounded_and_safe_fields_only() {
        type Change = Box<dyn Fn(&mut TabHandoff)>;
        let cases: Vec<Change> = vec![
            Box::new(|t| t.title = "x".repeat(MAX_TITLE + 1)),
            Box::new(|t| t.title = "a\u{7}b".into()),
            Box::new(|t| t.draft = Some("x".repeat(MAX_DRAFT + 1))),
            Box::new(|t| t.session_file = Some("relative/session.jsonl".into())),
            Box::new(|t| {
                t.target.as_mut().unwrap().url = Some("https://user:pw@example.com".into())
            }),
            Box::new(|t| t.target.as_mut().unwrap().url = Some("file:///etc/passwd".into())),
            Box::new(|t| t.target.as_mut().unwrap().url = Some("javascript:alert(1)".into())),
            Box::new(|t| t.target.as_mut().unwrap().session_id = Some(String::new())),
            Box::new(|t| t.route.as_mut().unwrap().back = vec![String::new(); MAX_ROUTE + 1]),
        ];
        for (i, change) in cases.iter().enumerate() {
            let mut t = tab();
            change(&mut t);
            assert!(t.check().is_err(), "case {i}");
        }
    }

    #[test]
    fn window_titles_are_suffixed_and_plain() {
        assert_eq!(window_title("Marketing").unwrap(), "Marketing — codeaf");
        assert_eq!(window_title("  ").unwrap(), "codeaf");
        assert!(window_title("a\nb").is_err());
        assert!(window_title(&"x".repeat(MAX_WINDOW_TITLE + 1)).is_err());
    }

    #[test]
    fn positions_must_be_finite() {
        assert!(Point { x: 10.0, y: 20.0 }.finite());
        assert!(!Point {
            x: f64::NAN,
            y: 0.0
        }
        .finite());
        assert!(!Point { x: 1e9, y: 0.0 }.finite());
    }
}

/// The commands driven through Tauri's real IPC path on the mock runtime: the
/// label the request arrives from is the label the command sees.
#[cfg(test)]
mod ipc_tests {
    use super::*;
    use tauri::ipc::{CallbackFn, InvokeBody};
    use tauri::test::{get_ipc_response, mock_builder, mock_context, noop_assets, MockRuntime};
    use tauri::webview::InvokeRequest;

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
            .manage(Windows::default())
            .manage(crate::notifications::Attention::default())
            .invoke_handler(tauri::generate_handler![
                window_open,
                window_move_tab,
                window_claim_handoff,
                window_list,
                window_focus,
                window_set_title,
                window_context,
                crate::dialogs::dialog_pick,
                crate::notifications::notify_permission,
                crate::notifications::notify_attention,
                crate::notifications::badge_set
            ])
            .build(context)
            .unwrap()
    }

    fn window(app: &tauri::App<MockRuntime>, label: &str) -> WebviewWindow<MockRuntime> {
        WebviewWindowBuilder::new(app, label, Default::default())
            .build()
            .unwrap()
    }

    /// The app's own origin on macOS and Linux.
    const LOCAL: &str = "tauri://localhost";

    fn call(
        from: &WebviewWindow<MockRuntime>,
        cmd: &str,
        body: serde_json::Value,
    ) -> Result<serde_json::Value, serde_json::Value> {
        call_from(from, LOCAL, cmd, body)
    }

    fn call_from(
        from: &WebviewWindow<MockRuntime>,
        origin: &str,
        cmd: &str,
        body: serde_json::Value,
    ) -> Result<serde_json::Value, serde_json::Value> {
        get_ipc_response(
            from,
            InvokeRequest {
                cmd: cmd.into(),
                callback: CallbackFn(0),
                error: CallbackFn(1),
                url: origin.parse().unwrap(),
                body: InvokeBody::Json(body),
                headers: Default::default(),
                invoke_key: tauri::test::INVOKE_KEY.to_string(),
            },
        )
        .map(|b| b.deserialize::<serde_json::Value>().unwrap())
    }

    fn handoff() -> serde_json::Value {
        serde_json::json!({ "kind": "conversation", "title": "Fix the parser", "draft": "half" })
    }

    #[test]
    fn a_foreign_label_is_refused_by_every_command() {
        let app = app();
        let stranger = window(&app, "web-p1");
        for (cmd, body) in [
            (
                "window_open",
                serde_json::json!({ "request": { "placeKey": "now" } }),
            ),
            ("window_list", serde_json::json!({})),
            ("window_context", serde_json::json!({})),
            ("window_claim_handoff", serde_json::json!({})),
            ("window_set_title", serde_json::json!({ "title": "x" })),
            ("window_focus", serde_json::json!({ "label": "main" })),
            (
                "dialog_pick",
                serde_json::json!({ "request": { "kind": "folder" } }),
            ),
            ("notify_permission", serde_json::json!({})),
            ("notify_attention", serde_json::json!({ "items": [] })),
            ("badge_set", serde_json::json!({ "count": 1 })),
        ] {
            assert_eq!(
                call(&stranger, cmd, body),
                Err(serde_json::json!(UNTRUSTED)),
                "{cmd}"
            );
        }
    }

    /// The first gate is Tauri's own: a page from the web reaches no app command
    /// without a `remote` capability, which codeaf never grants. The label check
    /// above is the second gate, for anything that gets past the first.
    #[test]
    fn a_web_origin_is_refused_before_any_command_runs() {
        let app = app();
        let main = window(&app, "main");
        for origin in ["https://example.com/", "http://127.0.0.1:9/"] {
            let refused =
                call_from(&main, origin, "window_list", serde_json::json!({})).unwrap_err();
            assert!(
                refused.as_str().unwrap().contains("not allowed"),
                "{origin}: {refused}"
            );
        }
        assert!(call(&main, "window_list", serde_json::json!({})).is_ok());
    }

    #[test]
    fn opening_a_window_on_a_place_and_moving_a_tab_into_it() {
        let app = app();
        let main = window(&app, "main");
        let opened = call(
            &main,
            "window_open",
            serde_json::json!({ "request": { "placeKey": "pl_0123456789abcdef", "handoff": handoff() } }),
        )
        .unwrap();
        assert_eq!(opened["label"], "w-2");
        assert_eq!(opened["handoffId"], "h-1");
        let second = app.get_webview_window("w-2").expect("the window exists");

        // The source cannot take its own handoff; the target takes it once.
        assert_eq!(
            call(&main, "window_claim_handoff", serde_json::json!({})),
            Ok(serde_json::Value::Null)
        );
        let claimed = call(&second, "window_claim_handoff", serde_json::json!({})).unwrap();
        assert_eq!(claimed["handoffId"], "h-1");
        assert_eq!(claimed["draft"], "half");
        assert_eq!(
            call(&second, "window_claim_handoff", serde_json::json!({})),
            Ok(serde_json::Value::Null)
        );

        let context = call(&second, "window_context", serde_json::json!({})).unwrap();
        assert_eq!(
            context,
            serde_json::json!({ "label": "w-2", "placeKey": "pl_0123456789abcdef" })
        );
        let rows = call(&main, "window_list", serde_json::json!({})).unwrap();
        let labels: Vec<_> = rows
            .as_array()
            .unwrap()
            .iter()
            .map(|r| r["label"].clone())
            .collect();
        assert_eq!(
            labels,
            [serde_json::json!("main"), serde_json::json!("w-2")]
        );

        // Back the other way, into an already open window.
        let moved = call(
            &second,
            "window_move_tab",
            serde_json::json!({ "request": { "to": "main", "handoff": handoff() } }),
        )
        .unwrap();
        assert_eq!(moved["handoffId"], "h-2");
        assert_eq!(
            call(&main, "window_claim_handoff", serde_json::json!({})).unwrap()["handoffId"],
            "h-2"
        );
    }

    /// A web page in a window is a child view, and Tauri then stops counting
    /// that window as a webview window. Every window command must still find it:
    /// on Linux the native app refused "Move to new window" from any window that
    /// had shown a page, and could not move a tab into one.
    #[test]
    fn a_window_showing_a_web_page_is_still_one_of_ours() {
        let app = app();
        let main = window(&app, "main");
        main.as_ref()
            .window()
            .add_child(
                tauri::webview::WebviewBuilder::new(
                    "web-1",
                    WebviewUrl::External("https://example.com/".parse().unwrap()),
                ),
                LogicalPosition::new(0.0, 40.0),
                tauri::LogicalSize::new(400.0, 300.0),
            )
            .unwrap();
        assert!(app.get_webview_window("main").is_none());

        let opened = call(
            &main,
            "window_open",
            serde_json::json!({ "request": { "placeKey": "now", "handoff": handoff() } }),
        )
        .unwrap();
        assert_eq!(opened["label"], "w-2");
        let rows = call(&main, "window_list", serde_json::json!({})).unwrap();
        let labels: Vec<_> = rows
            .as_array()
            .unwrap()
            .iter()
            .map(|r| r["label"].clone())
            .collect();
        assert_eq!(
            labels,
            [serde_json::json!("main"), serde_json::json!("w-2")]
        );
        let second = app.get_webview_window("w-2").expect("the window exists");
        assert!(call(
            &second,
            "window_move_tab",
            serde_json::json!({ "request": { "to": "main", "handoff": handoff() } })
        )
        .is_ok());
        assert!(call(
            &second,
            "window_focus",
            serde_json::json!({ "label": "main" })
        )
        .is_ok());
    }

    #[test]
    fn bad_requests_open_nothing() {
        let app = app();
        let main = window(&app, "main");
        for request in [
            serde_json::json!({ "placeKey": "p-0123456789ab" }),
            serde_json::json!({ "placeKey": "now", "handoff": { "kind": "conversation", "title": "x", "token": "t" } }),
            serde_json::json!({ "placeKey": "now", "url": "https://evil.example" }),
            serde_json::json!({ "placeKey": "now", "at": { "x": 1e12, "y": 0 } }),
        ] {
            assert!(call(
                &main,
                "window_open",
                serde_json::json!({ "request": request })
            )
            .is_err());
        }
        assert!(call(
            &main,
            "window_move_tab",
            serde_json::json!({ "request": { "to": "main", "handoff": handoff() } })
        )
        .is_err());
        assert!(call(
            &main,
            "window_move_tab",
            serde_json::json!({ "request": { "to": "w-9", "handoff": handoff() } })
        )
        .is_err());
        assert_eq!(app.webview_windows().len(), 1);
    }

    #[test]
    fn without_the_plugin_notifications_say_unavailable() {
        let app = app();
        let main = window(&app, "main");
        assert_eq!(
            call(&main, "notify_permission", serde_json::json!({})).unwrap(),
            serde_json::json!({ "state": "unavailable", "verified": false })
        );
    }
}
