//! Native web tabs: one child webview per web pane, placed over the pane's
//! rectangle in the window that asked for it.
//!
//! A web view is a guest. It is created with `WebviewUrl::External`, follows
//! only the navigations `policy::allows_navigation` accepts, never opens a
//! window of its own, never downloads, is denied every device permission, and
//! keeps its cookies apart from the app's. It cannot reach a command: see the
//! comment at the top of `policy.rs` for why Tauri refuses it.
//!
//! The renderer drives it through the commands below with typed, bounded
//! arguments (a pane id, a URL, a rectangle, one of four history steps). It
//! never sends a script. What the page does comes back as `web://state` and
//! `web://new-tab` events addressed to the owning webview alone.

mod platform;
mod policy;

pub use policy::Rect;
use policy::{AppOrigins, Refusal};
use serde::Serialize;
use std::collections::HashMap;
use std::sync::Mutex;
use std::time::Duration;
use tauri::webview::{DownloadEvent, NewWindowResponse, PageLoadEvent, PermissionResponse};
use tauri::{
    AppHandle, Emitter, LogicalPosition, LogicalSize, Manager, Runtime, Url, Webview,
    WebviewBuilder, WebviewUrl,
};

const STATE_EVENT: &str = "web://state";
const NEW_TAB_EVENT: &str = "web://new-tab";
/// How often, and for how long, a load is watched where failures are silent.
const POLL_EVERY: Duration = Duration::from_millis(400);
const POLL_FOR: u32 = 300;
/// The macOS website data store web views share, apart from the app's (macOS 14+).
#[cfg(target_os = "macos")]
const DATA_STORE: [u8; 16] = *b"codeaf-web-tabs1";

/// A transient line the pane shows once, then forgets.
#[derive(Clone, Copy, Debug, PartialEq, Eq, Serialize)]
#[serde(rename_all = "camelCase")]
pub enum Notice {
    /// The page tried to download a file; web tabs do not download.
    Download,
    /// The page tried to go somewhere a web tab may not go.
    Blocked,
    /// The page asked for the camera, microphone, location or similar.
    Permission,
}

/// Why the pane shows an error instead of the page.
#[derive(Clone, Copy, Debug, PartialEq, Eq, Serialize)]
#[serde(rename_all = "camelCase", tag = "kind", content = "reason")]
pub enum Failure {
    Refused(Refusal),
    Unreachable,
}

#[derive(Clone, Debug, PartialEq, Serialize)]
#[serde(rename_all = "camelCase")]
pub struct WebState {
    pub pane: String,
    pub url: String,
    pub title: String,
    pub loading: bool,
    pub can_back: bool,
    pub can_forward: bool,
    /// False where the platform cannot report history (see platform.rs).
    pub history_known: bool,
    pub failure: Option<Failure>,
    pub notice: Option<Notice>,
}

impl WebState {
    fn new(pane: &str, url: &Url) -> Self {
        WebState {
            pane: pane.into(),
            url: url.to_string(),
            title: String::new(),
            loading: true,
            can_back: false,
            can_forward: false,
            history_known: false,
            failure: None,
            notice: None,
        }
    }
}

#[derive(Clone, Debug, Serialize)]
#[serde(rename_all = "camelCase")]
struct NewTab {
    opener: String,
    url: String,
}

struct Entry {
    owner: String,
    state: WebState,
    /// Bumped on every load start; a watcher for an older load stops.
    load: u64,
    finished: u64,
    /// The view is on screen. An overlay hide and a hidden tab both clear it,
    /// so a later show does not uncover a page that was not showing.
    shown: bool,
}

#[derive(Default)]
struct Views(Mutex<HashMap<String, Entry>>);

fn views<R: Runtime>(app: &AppHandle<R>) -> tauri::State<'_, Views> {
    if app.try_state::<Views>().is_none() {
        app.manage(Views::default());
    }
    app.state::<Views>()
}

fn origins<R: Runtime>(app: &AppHandle<R>) -> AppOrigins {
    AppOrigins::new(app.config().build.dev_url.as_ref())
}

/// Changes one view's state and sends the result to its owner alone.
fn update<R: Runtime>(app: &AppHandle<R>, label: &str, change: impl FnOnce(&mut Entry)) {
    let sent = {
        let views = views(app);
        let Ok(mut map) = views.0.lock() else { return };
        let Some(entry) = map.get_mut(label) else {
            return;
        };
        change(entry);
        let sent = (entry.owner.clone(), entry.state.clone());
        entry.state.notice = None;
        sent
    };
    let _ = app.emit_to(tauri::EventTarget::webview(sent.0), STATE_EVENT, sent.1);
}

/// Reads history and loading from the platform view, then reports.
fn refresh<R: Runtime>(view: &Webview<R>) {
    let app = view.app_handle().clone();
    let label = view.label().to_string();
    let url = view.url().ok().map(|u| u.to_string());
    platform::probe(
        view,
        Box::new(move |probe| {
            update(&app, &label, |entry| {
                if let Some(url) = url {
                    entry.state.url = url;
                }
                entry.state.can_back = probe.can_back;
                entry.state.can_forward = probe.can_forward;
                entry.state.history_known = probe.known;
            });
        }),
    );
}

/// Where a load failure is silent, a load that stops without finishing failed.
/// One short-lived thread per load asks the platform view, never the main thread.
fn watch_load<R: Runtime>(view: Webview<R>, load: u64) {
    if !platform::POLLS_FOR_FAILURE {
        return;
    }
    std::thread::spawn(move || {
        let app = view.app_handle().clone();
        let label = view.label().to_string();
        let mut idle = 0;
        for _ in 0..POLL_FOR {
            std::thread::sleep(POLL_EVERY);
            let current = views(&app)
                .0
                .lock()
                .ok()
                .and_then(|map| map.get(&label).map(|e| (e.load, e.finished)));
            if !matches!(current, Some((now, finished)) if now == load && finished < load) {
                return;
            }
            let (tx, rx) = std::sync::mpsc::sync_channel(1);
            platform::probe(
                &view,
                Box::new(move |probe| {
                    let _ = tx.try_send(probe.loading);
                }),
            );
            match rx.recv_timeout(POLL_EVERY * 5) {
                Ok(false) => idle += 1,
                Ok(true) => idle = 0,
                Err(_) => return,
            }
            if idle >= 2 {
                update(&app, &label, |entry| {
                    if entry.load == load && entry.finished < load {
                        entry.finished = load;
                        entry.state.loading = false;
                        entry.state.failure = Some(Failure::Unreachable);
                    }
                });
                return;
            }
        }
    });
}

fn builder<R: Runtime>(app: &AppHandle<R>, label: &str, url: Url) -> WebviewBuilder<R> {
    let origins = origins(app);
    let (nav_app, nav_label) = (app.clone(), label.to_string());
    let (pop_app, pop_label, pop_origins) = (app.clone(), label.to_string(), origins.clone());
    let (load_origins, perm_app) = (origins.clone(), app.clone());
    let builder = WebviewBuilder::new(label, WebviewUrl::External(url))
        .on_navigation(move |target| {
            let allowed = policy::allows_navigation(target, &origins);
            if !allowed {
                update(&nav_app, &nav_label, |entry| {
                    entry.state.notice = Some(Notice::Blocked)
                });
            }
            allowed
        })
        // window.open and target=_blank become a request for a new web tab.
        // The page never gets a window of its own.
        .on_new_window(move |target, _features| {
            let opener = pop_label
                .strip_prefix(policy::LABEL_PREFIX)
                .unwrap_or_default()
                .to_string();
            match policy::checked_url(target.as_str(), &pop_origins) {
                Ok(url) => {
                    let owner = views(&pop_app)
                        .0
                        .lock()
                        .ok()
                        .and_then(|map| map.get(&pop_label).map(|e| e.owner.clone()));
                    if let Some(owner) = owner {
                        let _ = pop_app.emit_to(
                            tauri::EventTarget::webview(owner),
                            NEW_TAB_EVENT,
                            NewTab {
                                opener,
                                url: url.to_string(),
                            },
                        );
                    }
                }
                Err(_) if target.as_str() == "about:blank" => {}
                Err(_) => update(&pop_app, &pop_label, |entry| {
                    entry.state.notice = Some(Notice::Blocked)
                }),
            }
            NewWindowResponse::Deny
        })
        .on_document_title_changed(|view, title| {
            let title: String = title.chars().take(512).collect();
            let app = view.app_handle().clone();
            update(&app, view.label(), |entry| entry.state.title = title);
            refresh(&view);
        })
        .on_page_load(move |view, payload| {
            let app = view.app_handle().clone();
            let label = view.label().to_string();
            // A redirect the engine followed without asking is checked here.
            if !policy::allows_navigation(payload.url(), &load_origins) {
                let _ = view.navigate(Url::parse("about:blank").expect("static URL"));
                update(&app, &label, |entry| {
                    entry.state.loading = false;
                    entry.state.failure = Some(Failure::Refused(Refusal::AppOrigin));
                });
                return;
            }
            let url = payload.url().to_string();
            match payload.event() {
                PageLoadEvent::Started => {
                    let mut load = 0;
                    update(&app, &label, |entry| {
                        entry.load += 1;
                        load = entry.load;
                        entry.state.loading = true;
                        entry.state.failure = None;
                        entry.state.url = url;
                    });
                    watch_load(view.clone(), load);
                }
                PageLoadEvent::Finished => {
                    update(&app, &label, |entry| {
                        entry.finished = entry.load;
                        entry.state.loading = false;
                        entry.state.url = url;
                    });
                }
            }
            refresh(&view);
        })
        .on_download(|view, event| {
            if let DownloadEvent::Requested { .. } = event {
                let app = view.app_handle().clone();
                update(&app, view.label(), |entry| {
                    entry.state.notice = Some(Notice::Download)
                });
            }
            false
        })
        .on_permission_request(move |view, _kind| {
            update(&perm_app, view.label(), |entry| {
                entry.state.notice = Some(Notice::Permission)
            });
            PermissionResponse::Deny
        })
        .devtools(false)
        .focused(false);
    with_separate_store(app, builder)
}

/// Web pages keep their cookies and storage apart from the app's own.
#[cfg(target_os = "macos")]
fn with_separate_store<R: Runtime>(
    _app: &AppHandle<R>,
    builder: WebviewBuilder<R>,
) -> WebviewBuilder<R> {
    builder.data_store_identifier(DATA_STORE)
}

#[cfg(not(target_os = "macos"))]
fn with_separate_store<R: Runtime>(
    app: &AppHandle<R>,
    builder: WebviewBuilder<R>,
) -> WebviewBuilder<R> {
    match app.path().app_local_data_dir() {
        Ok(dir) => builder.data_directory(dir.join("web-tabs")),
        Err(_) => builder.incognito(true),
    }
}

/// The caller must be a top-level app webview, and must own the view it names.
fn caller_label<R: Runtime>(caller: &Webview<R>) -> Result<String, String> {
    let window = caller.window();
    if policy::is_trusted_caller(caller.label(), window.label()) {
        Ok(caller.label().to_string())
    } else {
        Err("Only codeaf's own window may open web pages".into())
    }
}

fn owns_view<R: Runtime>(app: &AppHandle<R>, label: &str, owner: &str) -> Result<bool, String> {
    Ok(views(app)
        .0
        .lock()
        .map_err(|_| "Web pages are unavailable")?
        .get(label)
        .is_some_and(|e| e.owner == owner))
}

fn owned_view<R: Runtime>(caller: &Webview<R>, pane: &str) -> Result<Webview<R>, String> {
    let owner = caller_label(caller)?;
    let label = policy::label_for(policy::checked_pane(pane)?);
    let app = caller.app_handle();
    let owns = owns_view(app, &label, &owner)?;
    let view = app.get_webview(&label).filter(|_| owns);
    view.ok_or_else(|| "That web page is closed".into())
}

/// Closing a webview is carried out by the event loop; a view with the same
/// label cannot be created until it has gone. Bounded, so a stuck close fails
/// the open with a sentence instead of waiting forever.
async fn wait_gone<R: Runtime>(app: &AppHandle<R>, label: &str) {
    for _ in 0..40 {
        if app.get_webview(label).is_none() {
            return;
        }
        let _ = tauri::async_runtime::spawn_blocking(|| {
            std::thread::sleep(std::time::Duration::from_millis(25))
        })
        .await;
    }
}

fn place<R: Runtime>(
    caller: &Webview<R>,
    view: Option<&Webview<R>>,
    rect: Rect,
) -> Result<Rect, String> {
    let window = caller.window();
    let scale = window
        .scale_factor()
        .map_err(|_| "The window is unavailable")?;
    let size = window
        .inner_size()
        .map_err(|_| "The window is unavailable")?
        .to_logical::<f64>(scale);
    let rect = rect.clamped(size.width, size.height)?;
    if let Some(view) = view {
        view.set_position(LogicalPosition::new(rect.x, rect.y))
            .map_err(|_| "The page could not move")?;
        view.set_size(LogicalSize::new(rect.width, rect.height))
            .map_err(|_| "The page could not resize")?;
        platform::place(view, rect);
    }
    Ok(rect)
}

/// Opens `url` in the pane's web view, creating the view on the caller's
/// window the first time. `visible: false` creates it hidden (an overlay is up).
#[tauri::command]
pub async fn web_open<R: Runtime>(
    caller: Webview<R>,
    pane: String,
    url: String,
    rect: Rect,
    visible: bool,
) -> Result<WebState, String> {
    let owner = caller_label(&caller)?;
    let pane = policy::checked_pane(&pane)?.to_string();
    let app = caller.app_handle().clone();
    let url = policy::checked_url(&url, &origins(&app)).map_err(|r| r.sentence().to_string())?;
    let label = policy::label_for(&pane);
    if let Some(view) = app.get_webview(&label) {
        if owns_view(&app, &label, &owner)? {
            place(&caller, Some(&view), rect)?;
            view.navigate(url).map_err(|_| "The page could not open")?;
            return current(&app, &label);
        }
        // The pane's tab moved to another of codeaf's windows: the new window
        // takes the pane and the old window's view goes. The page reloads there;
        // a native view cannot change windows. The old window's own close for
        // this pane then finds it is no longer the owner and does nothing.
        views(&app)
            .0
            .lock()
            .map_err(|_| "Web pages are unavailable")?
            .remove(&label);
        let _ = view.close();
        wait_gone(&app, &label).await;
    }
    {
        let views = views(&app);
        let mut map = views.0.lock().map_err(|_| "Web pages are unavailable")?;
        if map.contains_key(&label) {
            return Err("That web page is still opening".into());
        }
        if map.len() >= policy::MAX_VIEWS {
            return Err("Too many web pages are open; close one first".into());
        }
        map.insert(
            label.clone(),
            Entry {
                owner,
                state: WebState::new(&pane, &url),
                load: 0,
                finished: 0,
                // Created hidden. It is marked shown only after it is left on screen.
                shown: false,
            },
        );
    }
    let rect = place(&caller, None, rect)?;
    let built = caller.window().add_child(
        builder(&app, &label, url),
        LogicalPosition::new(rect.x, rect.y),
        LogicalSize::new(rect.width, rect.height),
    );
    let view = match built {
        Ok(view) => view,
        Err(_) => {
            views(&app)
                .0
                .lock()
                .map_err(|_| "Web pages are unavailable")?
                .remove(&label);
            return Err("The page could not open".into());
        }
    };
    platform::adopt(&caller, &view, rect);
    // While the page holds the keys, a document listener in the app never runs.
    // Linux registers the three app chords on the window; macOS uses the menu.
    platform::bind_app_chords(&app, caller.label(), &view, &pane);
    // An overlay already up must win over the pane's own request to be seen:
    // a page created under a menu would otherwise paint over it.
    let conceal = !visible || crate::weboverlay::is_holding(caller.window().label());
    if conceal {
        let _ = view.hide();
    } else {
        mark_shown(&app, &label, true)?;
    }
    let (fail_app, fail_label) = (app.clone(), label.clone());
    platform::watch_failures(
        &view,
        Box::new(move || {
            update(&fail_app, &fail_label, |entry| {
                entry.finished = entry.load;
                entry.state.loading = false;
                entry.state.failure = Some(Failure::Unreachable);
            })
        }),
    );
    current(&app, &label)
}

/// Sends one app chord to the window that owns the page. New and close use the
/// same event the menu already sends; the address chord is its own event so a
/// tab command never has to mean "focus the address".
pub(super) fn deliver_chord<R: Runtime>(
    app: &AppHandle<R>,
    owner: &str,
    pane: Option<&str>,
    chord: policy::AppChord,
) {
    let (event, payload) = match chord {
        policy::AppChord::NewTab => ("desktop-tab-action", serde_json::json!("new")),
        policy::AppChord::CloseTab => ("desktop-tab-action", serde_json::json!("close")),
        policy::AppChord::Address => ("web://focus-address", serde_json::json!({ "pane": pane })),
    };
    let _ = app.emit_to(owner, event, payload);
}

fn current<R: Runtime>(app: &AppHandle<R>, label: &str) -> Result<WebState, String> {
    let views = views(app);
    let map = views.0.lock().map_err(|_| "Web pages are unavailable")?;
    map.get(label)
        .map(|e| e.state.clone())
        .ok_or_else(|| "That web page is closed".into())
}

#[tauri::command]
pub async fn web_navigate<R: Runtime>(
    caller: Webview<R>,
    pane: String,
    url: String,
) -> Result<(), String> {
    let view = owned_view(&caller, &pane)?;
    let url = policy::checked_url(&url, &origins(caller.app_handle()))
        .map_err(|r| r.sentence().to_string())?;
    view.navigate(url)
        .map_err(|_| "The page could not open".into())
}

/// Moves and sizes the view to the pane's rectangle (sent once per frame at most).
#[tauri::command]
pub async fn web_bounds<R: Runtime>(
    caller: Webview<R>,
    pane: String,
    rect: Rect,
) -> Result<(), String> {
    let view = owned_view(&caller, &pane)?;
    place(&caller, Some(&view), rect).map(|_| ())
}

/// Hides a view while something of the app's is drawn over its pane, or while
/// its tab is not showing, and shows it again afterwards.
#[tauri::command]
pub async fn web_visible<R: Runtime>(
    caller: Webview<R>,
    pane: String,
    visible: bool,
) -> Result<(), String> {
    if visible && crate::weboverlay::is_holding(caller.window().label()) {
        // The overlay still owns visibility. Showing now would paint the page
        // over the menu. The release shows the panes that are still open.
        let _ = owned_view(&caller, &pane)?;
        return Ok(());
    }
    let alive = if visible {
        reveal_pane(&caller, &pane)?
    } else {
        conceal_pane(&caller, &pane)?
    };
    if !alive {
        return Err("That web page is closed".into());
    }
    Ok(())
}

fn mark_shown<R: Runtime>(app: &AppHandle<R>, label: &str, shown: bool) -> Result<(), String> {
    let views = views(app);
    let mut map = views.0.lock().map_err(|_| "Web pages are unavailable")?;
    if let Some(entry) = map.get_mut(label) {
        entry.shown = shown;
    }
    Ok(())
}

/// Panes on this caller's window whose views are currently on screen.
pub(crate) fn shown_panes<R: Runtime>(caller: &Webview<R>) -> Result<Vec<String>, String> {
    let owner = caller_label(caller)?;
    let views = views(caller.app_handle());
    let map = views.0.lock().map_err(|_| "Web pages are unavailable")?;
    Ok(map
        .values()
        .filter(|entry| entry.owner == owner && entry.shown)
        .map(|entry| entry.state.pane.clone())
        .collect())
}

/// Whether this caller still has that pane's view. A closed pane is false.
pub(crate) fn view_is_open<R: Runtime>(caller: &Webview<R>, pane: &str) -> Result<bool, String> {
    let Ok(pane) = policy::checked_pane(pane) else {
        return Ok(false);
    };
    let owner = caller_label(caller)?;
    let label = policy::label_for(pane);
    let app = caller.app_handle();
    let owns = owns_view(app, &label, &owner)?;
    Ok(owns && app.get_webview(&label).is_some())
}

/// Hides one owned view and remembers that it is not on screen. False when the
/// pane is already gone, so a close during a hide is not an error.
pub(crate) fn conceal_pane<R: Runtime>(caller: &Webview<R>, pane: &str) -> Result<bool, String> {
    let Ok(pane) = policy::checked_pane(pane) else {
        return Ok(false);
    };
    let owner = caller_label(caller)?;
    let label = policy::label_for(pane);
    let app = caller.app_handle();
    let owns = owns_view(app, &label, &owner)?;
    let Some(view) = app.get_webview(&label).filter(|_| owns) else {
        return Ok(false);
    };
    view.hide()
        .map_err(|_| "The page could not change".to_string())?;
    mark_shown(app, &label, false)?;
    Ok(true)
}

/// Shows one owned view. False when the pane was closed, which is the whole
/// point of calling it after an overlay: a closed page is not brought back.
pub(crate) fn reveal_pane<R: Runtime>(caller: &Webview<R>, pane: &str) -> Result<bool, String> {
    let Ok(pane) = policy::checked_pane(pane) else {
        return Ok(false);
    };
    let owner = caller_label(caller)?;
    let label = policy::label_for(pane);
    let app = caller.app_handle();
    let owns = owns_view(app, &label, &owner)?;
    let Some(view) = app.get_webview(&label).filter(|_| owns) else {
        return Ok(false);
    };
    view.show()
        .map_err(|_| "The page could not change".to_string())?;
    mark_shown(app, &label, true)?;
    Ok(true)
}

#[derive(Clone, Copy, Debug, serde::Deserialize, PartialEq, Eq)]
#[serde(rename_all = "camelCase")]
pub enum HistoryStep {
    Back,
    Forward,
    Reload,
    Stop,
}

#[tauri::command]
pub async fn web_history<R: Runtime>(
    caller: Webview<R>,
    pane: String,
    step: HistoryStep,
) -> Result<(), String> {
    let view = owned_view(&caller, &pane)?;
    match step {
        HistoryStep::Back => platform::step(&view, platform::Step::Back),
        HistoryStep::Forward => platform::step(&view, platform::Step::Forward),
        HistoryStep::Stop => platform::step(&view, platform::Step::Stop),
        HistoryStep::Reload => view.reload().map_err(|_| "The page could not reload")?,
    }
    Ok(())
}

/// Closes the pane's web view. Nothing else: a conversation the page was
/// attached to keeps running, because a tab is a view, not the work.
#[tauri::command]
pub async fn web_close<R: Runtime>(caller: Webview<R>, pane: String) -> Result<(), String> {
    let view = owned_view(&caller, &pane)?;
    views(caller.app_handle())
        .0
        .lock()
        .map_err(|_| "Web pages are unavailable")?
        .remove(view.label());
    view.close().map_err(|_| "The page could not close".into())
}

#[derive(Clone, Debug, Serialize)]
#[serde(rename_all = "camelCase")]
pub struct Snapshot {
    /// A `data:image/png;base64,…` URL the renderer's CSP already allows.
    pub image: String,
    pub url: String,
}

/// A picture of the page as it is drawn now, for a hover card or overview
/// card. Unavailable where the platform cannot take one.
#[tauri::command]
pub async fn web_snapshot<R: Runtime>(
    caller: Webview<R>,
    pane: String,
) -> Result<Snapshot, String> {
    let view = owned_view(&caller, &pane)?;
    let url = view.url().map(|u| u.to_string()).unwrap_or_default();
    let (tx, mut rx) = tauri::async_runtime::channel(1);
    platform::snapshot(
        &view,
        Box::new(move |png| {
            let _ = tx.try_send(png);
        }),
    );
    let png = rx.recv().await.ok_or("unavailable")??;
    if png.is_empty() || png.len() > platform::SNAPSHOT_BYTES {
        return Err("unavailable".into());
    }
    Ok(Snapshot {
        image: format!("data:image/png;base64,{}", policy::base64(&png)),
        url,
    })
}

/// The views the calling window owns, for a renderer that reloaded and must
/// close views whose panes are gone.
#[tauri::command]
pub async fn web_list<R: Runtime>(caller: Webview<R>) -> Result<Vec<WebState>, String> {
    let owner = caller_label(&caller)?;
    let views = views(caller.app_handle());
    let map = views.0.lock().map_err(|_| "Web pages are unavailable")?;
    Ok(map
        .values()
        .filter(|e| e.owner == owner)
        .map(|e| e.state.clone())
        .collect())
}

#[cfg(test)]
mod tests;
