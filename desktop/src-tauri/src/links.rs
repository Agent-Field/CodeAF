//! codeaf links that arrive from OUTSIDE the app: a `codeaf://` address opened
//! from another program, a browser or a terminal. The operating system hands it
//! to the deep-link plugin (a cold start through `get_current`, a running app
//! through `on_open_url`; on Linux a second launch is forwarded to the running
//! one by the single-instance plugin). This module is the only door in.
//!
//! A LINK NAMES A DURABLE TARGET AND NOTHING ELSE: a conversation id, a task id,
//! a workspace-relative path or an engine terminal id (see the renderer's
//! `features/tabs/links/deepLinks.ts`, which this mirrors rule for rule; both
//! read `link-cases.json`). Nothing here opens a file, starts a process or
//! touches the engine. A refused link is dropped before any window sees it; a
//! good one is queued for ONE codeaf window, that window is brought forward and
//! told, and it claims the link with `link_claim`. Link contents are never
//! logged: a path is a person's business.

use std::sync::Mutex;
use std::time::{Duration, Instant};

use tauri::{AppHandle, Emitter, Manager, Runtime, Webview, WebviewWindowBuilder, Window};

use crate::windows::{app_window, app_windows, trusted};

/// The event the addressed window hears; the renderer then calls `link_claim`.
pub const READY: &str = "deep-link://ready";
/// A link nobody claimed in this long is dropped: a window that never booted
/// must not open a stale link minutes later.
const TTL: Duration = Duration::from_secs(60);
/// At most this many links wait at once; a flood keeps the newest.
const MAX_QUEUED: usize = 8;

const SCHEME: &str = "codeaf://";
const MAX_LINK: usize = 8192;
const MAX_ID: usize = 128;
const MAX_TASK: usize = 256;
const MAX_PATH: usize = 4096;

/// Why a link was refused; the same three words the renderer uses.
#[derive(Clone, Copy, Debug, PartialEq, Eq)]
pub enum Problem {
    Malformed,
    Unknown,
    Unsafe,
}

fn plain(text: &str) -> bool {
    !text.chars().any(char::is_control)
}

fn is_id(text: &str) -> bool {
    !text.is_empty()
        && text.len() <= MAX_ID
        && text
            .bytes()
            .all(|b| b.is_ascii_alphanumeric() || b == b'_' || b == b'-')
}

fn is_task_id(text: &str) -> bool {
    !text.is_empty()
        && text.chars().count() <= MAX_TASK
        && plain(text)
        && !text.contains(['/', '\\'])
        && !text.starts_with('#')
        && text != "."
        && text != ".."
}

fn is_relative_path(path: &str) -> bool {
    let drive =
        path.len() >= 2 && path.as_bytes()[0].is_ascii_alphabetic() && path.as_bytes()[1] == b':';
    !path.is_empty()
        && path.len() <= MAX_PATH
        && plain(path)
        && !path.contains('\\')
        && !path.starts_with('/')
        && !drive
        && path
            .split('/')
            .all(|p| !p.is_empty() && p != "." && p != "..")
}

/// Percent-decoding as `decodeURIComponent` does it: every `%` must start two
/// hex digits and the bytes must be UTF-8, or the text is malformed.
fn decode(text: &str) -> Option<String> {
    let bytes = text.as_bytes();
    let mut out = Vec::with_capacity(bytes.len());
    let mut i = 0;
    while i < bytes.len() {
        if bytes[i] == b'%' {
            let hex = bytes.get(i + 1..i + 3)?;
            let hex = std::str::from_utf8(hex).ok()?;
            out.push(u8::from_str_radix(hex, 16).ok()?);
            i += 3;
        } else {
            out.push(bytes[i]);
            i += 1;
        }
    }
    String::from_utf8(out).ok()
}

/// Accepts exactly the links the renderer's `parseDeepLink` accepts.
pub fn check(raw: &str) -> Result<(), Problem> {
    if raw.is_empty()
        || raw.len() > MAX_LINK
        || raw.chars().any(|c| c.is_control() || c.is_whitespace())
    {
        return Err(Problem::Malformed);
    }
    let lower = raw.get(..SCHEME.len()).map(str::to_ascii_lowercase);
    if lower.as_deref() != Some(SCHEME) || raw.contains('#') {
        return Err(Problem::Malformed);
    }
    let rest = &raw[SCHEME.len()..];
    let (path, query) = match rest.split_once('?') {
        Some((path, query)) => (path, Some(query)),
        None => (rest, None),
    };
    let segments: Vec<&str> = path.split('/').collect();
    let kind = segments[0];
    let shaped = segments.iter().all(|s| !s.is_empty())
        && match kind {
            "chat" => {
                query.is_none()
                    && (segments.len() == 2 || (segments.len() == 4 && segments[2] == "task"))
            }
            "file" | "diff" => {
                segments.len() == 2
                    && query.is_some_and(|q| q.starts_with("path=") && !q.contains('&'))
            }
            "terminal" => query.is_none() && segments.len() == 3,
            _ => false,
        };
    if !shaped {
        return Err(Problem::Unknown);
    }
    let parts: Vec<String> = segments
        .iter()
        .map(|s| decode(s))
        .collect::<Option<_>>()
        .ok_or(Problem::Malformed)?;
    let value = match query {
        Some(q) => Some(decode(&q["path=".len()..]).ok_or(Problem::Malformed)?),
        None => None,
    };
    let sound = is_id(&parts[1])
        && match kind {
            "chat" => parts.get(3).is_none_or(|task| is_task_id(task)),
            "terminal" => is_id(&parts[2]),
            _ => value.as_deref().is_some_and(is_relative_path),
        };
    if sound {
        Ok(())
    } else {
        Err(Problem::Unsafe)
    }
}

struct Queued {
    link: String,
    to: String,
    at: Instant,
}

#[derive(Default)]
struct Queue(Vec<Queued>);

impl Queue {
    fn sweep(&mut self, now: Instant) {
        self.0.retain(|q| now.duration_since(q.at) < TTL);
    }

    fn push(&mut self, link: String, to: &str, now: Instant) {
        self.sweep(now);
        if self.0.len() >= MAX_QUEUED {
            self.0.remove(0);
        }
        self.0.push(Queued {
            link,
            to: to.into(),
            at: now,
        });
    }

    /// Every link addressed to `to`, oldest first, each handed over once.
    fn claim(&mut self, to: &str, now: Instant) -> Vec<String> {
        self.sweep(now);
        let (mine, rest): (Vec<_>, Vec<_>) = std::mem::take(&mut self.0)
            .into_iter()
            .partition(|q| q.to == to);
        self.0 = rest;
        mine.into_iter().map(|q| q.link).collect()
    }

    fn forget(&mut self, label: &str) {
        self.0.retain(|q| q.to != label);
    }
}

#[derive(Default)]
pub struct Links(Mutex<Queue>);

/// The window a link goes to: the codeaf window in front, else `main`, else
/// the oldest open one. Looked up with `app_windows`, never `webview_windows`:
/// a window that is showing a web page is still a codeaf window. A web page's
/// own view (`web-*`) is never a candidate.
fn addressee<R: Runtime>(app: &AppHandle<R>) -> Option<Window<R>> {
    let mut windows = app_windows(app);
    windows.sort_by_key(|(label, _)| {
        label
            .strip_prefix("w-")
            .and_then(|n| n.parse::<u64>().ok())
            .unwrap_or(0)
    });
    if let Some(index) = windows
        .iter()
        .position(|(_, w)| w.is_focused().unwrap_or(false))
    {
        return Some(windows.swap_remove(index).1);
    }
    windows.into_iter().next().map(|(_, w)| w)
}

/// The codeaf window a second launch should come forward to.
pub fn front_window<R: Runtime>(app: &AppHandle<R>) -> Option<Window<R>> {
    addressee(app)
}

/// A Mac app lives on with every window closed; a link then opens `main` again
/// from its own configuration.
fn reopen_main<R: Runtime>(app: &AppHandle<R>) -> Option<Window<R>> {
    let config = app
        .config()
        .app
        .windows
        .iter()
        .find(|w| w.label == "main")?
        .clone();
    let built = WebviewWindowBuilder::from_config(app, &config)
        .ok()?
        .build()
        .ok()?;
    // The window just created has one webview, but the same lookup is used
    // everywhere so a later child view cannot hide it.
    app_window(app, built.label())
}

/// Brings a window forward the way a person would expect a link to.
pub fn bring_forward<R: Runtime>(window: &Window<R>) {
    let _ = window.unminimize();
    let _ = window.show();
    let _ = window.set_focus();
}

/// Takes links from the operating system. Refused ones are dropped here;
/// sound ones are queued for one window, which is brought forward and told.
pub fn receive<R: Runtime>(app: &AppHandle<R>, links: impl IntoIterator<Item = String>) {
    let sound: Vec<String> = links.into_iter().filter(|l| check(l).is_ok()).collect();
    if sound.is_empty() {
        return;
    }
    let Some(window) = addressee(app).or_else(|| reopen_main(app)) else {
        return;
    };
    let label = window.label().to_string();
    let links = app.state::<Links>();
    let Ok(mut queue) = links.0.lock() else {
        return;
    };
    let now = Instant::now();
    for link in sound {
        queue.push(link, &label, now);
    }
    drop(queue);
    bring_forward(&window);
    let _ = window.emit_to(label.as_str(), READY, ());
}

/// Called from the app's window-event hook: a closed window's links go with it.
pub fn on_destroyed<R: Runtime>(app: &AppHandle<R>, label: &str) {
    if let Some(links) = app.try_state::<Links>() {
        if let Ok(mut queue) = links.0.lock() {
            queue.forget(label);
        }
    }
}

/// The links queued for the calling window. Only codeaf's own windows may ask.
#[tauri::command]
pub fn link_claim<R: Runtime>(
    app: AppHandle<R>,
    webview: Webview<R>,
) -> Result<Vec<String>, String> {
    let me = trusted(&webview)?;
    let links = app.state::<Links>();
    let mut queue = links
        .0
        .lock()
        .map_err(|_| "Links are unavailable".to_string())?;
    Ok(queue.claim(&me, Instant::now()))
}

/// Wires the deep-link plugin: the link a cold start was launched with, and
/// every link that arrives while codeaf runs.
#[cfg(desktop)]
pub fn setup<R: Runtime>(app: &AppHandle<R>) {
    use tauri_plugin_deep_link::DeepLinkExt;
    // A packaged app is registered by its bundle (Info.plist, the .desktop
    // file). A development build registers itself on Linux only when asked,
    // because registering rewrites the person's default handler for codeaf://.
    #[cfg(target_os = "linux")]
    if std::env::var("CODEAF_DESKTOP_REGISTER_LINKS").as_deref() == Ok("1") {
        let _ = app.deep_link().register_all();
    }
    let handle = app.clone();
    app.deep_link().on_open_url(move |event| {
        receive(&handle, event.urls().into_iter().map(|u| u.to_string()));
    });
    if let Ok(Some(urls)) = app.deep_link().get_current() {
        receive(app, urls.into_iter().map(|u| u.to_string()));
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    const CASES: &str = include_str!("../../src/features/tabs/links/link-cases.json");

    fn problem(word: &str) -> Problem {
        match word {
            "malformed" => Problem::Malformed,
            "unknown" => Problem::Unknown,
            "unsafe" => Problem::Unsafe,
            other => panic!("unknown reason {other}"),
        }
    }

    #[test]
    fn the_shared_corpus_agrees_with_the_renderer() {
        let cases: serde_json::Value = serde_json::from_str(CASES).unwrap();
        let valid = cases["valid"].as_array().unwrap();
        let invalid = cases["invalid"].as_array().unwrap();
        assert!(valid.len() >= 5 && invalid.len() >= 20);
        for case in valid {
            let raw = case["raw"].as_str().unwrap();
            assert_eq!(check(raw), Ok(()), "{raw}");
            let canonical = case["canonical"].as_str().unwrap();
            assert_eq!(check(canonical), Ok(()), "{canonical}");
        }
        for case in invalid {
            let raw = case["raw"].as_str().unwrap();
            let want = problem(case["reason"].as_str().unwrap());
            assert_eq!(check(raw), Err(want), "{raw:?}");
        }
    }

    #[test]
    fn an_overlong_link_is_malformed() {
        let long = format!("codeaf://file/abc?path={}b", "a/".repeat(4100));
        assert_eq!(check(&long), Err(Problem::Malformed));
    }

    #[test]
    fn links_go_to_their_window_once_and_expire() {
        let mut queue = Queue::default();
        let start = Instant::now();
        queue.push("codeaf://chat/a".into(), "main", start);
        queue.push("codeaf://chat/b".into(), "w-2", start);
        assert!(queue.claim("w-3", start).is_empty());
        assert_eq!(queue.claim("main", start), vec!["codeaf://chat/a"]);
        assert!(queue.claim("main", start).is_empty());
        assert!(queue.claim("w-2", start + TTL).is_empty());
    }

    #[test]
    fn a_flood_keeps_only_the_newest_links() {
        let mut queue = Queue::default();
        let now = Instant::now();
        for n in 0..(MAX_QUEUED + 3) {
            queue.push(format!("codeaf://chat/c{n}"), "main", now);
        }
        let claimed = queue.claim("main", now);
        assert_eq!(claimed.len(), MAX_QUEUED);
        assert_eq!(claimed[0], "codeaf://chat/c3");
    }

    #[test]
    fn a_closed_window_takes_its_links_with_it() {
        let mut queue = Queue::default();
        let now = Instant::now();
        queue.push("codeaf://chat/a".into(), "w-2", now);
        queue.forget("w-2");
        assert!(queue.claim("w-2", now).is_empty());
    }
}

/// `link_claim` driven through Tauri's real IPC path on the mock runtime.
#[cfg(test)]
mod ipc_tests {
    use super::*;
    use tauri::ipc::{CallbackFn, InvokeBody};
    use tauri::test::{get_ipc_response, mock_builder, mock_context, noop_assets, MockRuntime};
    use tauri::webview::InvokeRequest;
    use tauri::WebviewWindow;

    fn app() -> tauri::App<MockRuntime> {
        mock_builder()
            .manage(Links::default())
            .invoke_handler(tauri::generate_handler![link_claim])
            .build(mock_context(noop_assets()))
            .unwrap()
    }

    fn claim(from: &WebviewWindow<MockRuntime>) -> Result<serde_json::Value, serde_json::Value> {
        get_ipc_response(
            from,
            InvokeRequest {
                cmd: "link_claim".into(),
                callback: CallbackFn(0),
                error: CallbackFn(1),
                url: "tauri://localhost".parse().unwrap(),
                body: InvokeBody::Json(serde_json::json!({})),
                headers: Default::default(),
                invoke_key: tauri::test::INVOKE_KEY.to_string(),
            },
        )
        .map(|b| b.deserialize::<serde_json::Value>().unwrap())
    }

    #[test]
    fn a_link_reaches_the_window_in_front_and_only_it() {
        let app = app();
        let main = WebviewWindowBuilder::new(&app, "main", Default::default())
            .build()
            .unwrap();
        let other = WebviewWindowBuilder::new(&app, "w-2", Default::default())
            .build()
            .unwrap();
        let stranger = WebviewWindowBuilder::new(&app, "web-p1", Default::default())
            .build()
            .unwrap();
        receive(
            app.handle(),
            [
                "codeaf://chat/9446cc2627f3deae".to_string(),
                "codeaf://file/9446cc2627f3deae?path=../../etc/passwd".to_string(),
                "javascript:alert(1)".to_string(),
            ],
        );
        // A web page's view is refused before it can see a queue.
        assert!(claim(&stranger).is_err());
        let to_main = claim(&main).unwrap();
        let to_other = claim(&other).unwrap();
        let mut all: Vec<serde_json::Value> = to_main.as_array().unwrap().clone();
        all.extend(to_other.as_array().unwrap().iter().cloned());
        assert_eq!(
            all,
            vec![serde_json::json!("codeaf://chat/9446cc2627f3deae")]
        );
        assert_eq!(claim(&main).unwrap(), serde_json::json!([]));
    }
}
