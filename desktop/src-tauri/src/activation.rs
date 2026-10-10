//! What a click on one of codeaf's system notifications opens.
//!
//! A notification is posted for questions and failures the renderer listed (see
//! notifications.rs). Each one that names a conversation is given a token here,
//! minted by Rust and never shown to the renderer or written into the
//! notification itself. The platform's own activation callback carries that
//! token back: a freedesktop `ActionInvoked` signal for the default action, the
//! macOS notification-center delegate's content click, a Windows toast's
//! `Activated` event. Nothing else can produce one, so a focus change, a second
//! launch or a crafted link never opens a question.
//!
//! THE TARGET IS FIXED WHEN THE NOTIFICATION IS POSTED. It is the conversation
//! and question the renderer named, checked here, and the click only chooses
//! which of the notification's own targets is still waiting. It is queued for
//! ONE codeaf window, that window is brought forward and told, and it claims the
//! target with `notify_claim`, the same shape `link_claim` uses.
//!
//! WHAT THE PLATFORMS CANNOT DO. A notification is not withdrawn when its
//! question is answered elsewhere; a click on it afterwards opens the
//! conversation without a question to focus. A development build on macOS posts
//! as Terminal (the system refuses an unbundled sender), so there the system
//! brings Terminal forward rather than codeaf.

use std::collections::{HashSet, VecDeque};
use std::sync::Mutex;
use std::time::{Duration, Instant};

use serde::{Deserialize, Serialize};
use tauri::{AppHandle, Emitter, Manager, Runtime, Webview};

use crate::windows::trusted;

/// The event the addressed window hears; the renderer then calls `notify_claim`.
pub const ACTIVATED: &str = "notification://activated";
/// Notifications whose click is remembered. An older one still opens codeaf the
/// way the system chooses, but no longer names a question.
const MAX_ROUTES: usize = 64;
/// A target nobody claimed in this long is dropped, as links.rs drops links.
const TTL: Duration = Duration::from_secs(60);
const MAX_QUEUED: usize = 8;
const MAX_ID: usize = 128;
const MAX_KIND: usize = 64;
/// The largest id the renderer's numbers carry exactly.
const MAX_QUESTION: u64 = (1 << 53) - 1;

/// A question as the conversation's tray names it: its kind and the engine's id.
#[derive(Clone, Debug, PartialEq, Eq, Serialize, Deserialize)]
#[serde(deny_unknown_fields)]
pub struct Question {
    pub kind: String,
    pub id: u64,
}

/// Where a click starts Next up: the attention item and its conversation.
#[derive(Clone, Debug, PartialEq, Eq, Serialize)]
#[serde(rename_all = "camelCase")]
pub struct Target {
    #[serde(skip_serializing_if = "Option::is_none")]
    pub item_id: Option<String>,
    pub chat_id: String,
    #[serde(skip_serializing_if = "Option::is_none")]
    pub question: Option<Question>,
}

/// A conversation id, spelled as a codeaf link spells one.
pub fn is_chat_id(text: &str) -> bool {
    !text.is_empty()
        && text.len() <= MAX_ID
        && text
            .bytes()
            .all(|b| b.is_ascii_alphanumeric() || b == b'_' || b == b'-')
}

impl Question {
    pub fn sound(&self) -> bool {
        (1..=MAX_QUESTION).contains(&self.id)
            && !self.kind.is_empty()
            && self.kind.len() <= MAX_KIND
            && self
                .kind
                .bytes()
                .all(|b| b.is_ascii_alphanumeric() || b == b'_' || b == b'-')
    }
}

struct Route {
    token: u64,
    /// Attention item id and its target, in the order the notification names them.
    targets: Vec<(String, Target)>,
}

#[derive(Default)]
pub struct Routes {
    next: u64,
    live: VecDeque<Route>,
}

impl Routes {
    /// A token for a notification with somewhere to go, or none.
    pub fn register(&mut self, targets: Vec<(String, Target)>) -> Option<u64> {
        if targets.is_empty() {
            return None;
        }
        if self.live.len() >= MAX_ROUTES {
            self.live.pop_front();
        }
        self.next += 1;
        self.live.push_back(Route {
            token: self.next,
            targets,
        });
        Some(self.next)
    }

    pub fn forget(&mut self, token: u64) {
        self.live.retain(|r| r.token != token);
    }

    /// The target a click on `token` opens, once. The first item still waiting
    /// wins; when every one has been answered, the first conversation opens
    /// with no question to focus, because the question it named is gone.
    pub fn resolve(&mut self, token: u64, pending: &HashSet<String>) -> Option<Target> {
        let at = self.live.iter().position(|r| r.token == token)?;
        let route = self.live.remove(at)?;
        if let Some((_, target)) = route.targets.iter().find(|(id, _)| pending.contains(id)) {
            return Some(target.clone());
        }
        route.targets.into_iter().next().map(|(_, target)| Target {
            item_id: None,
            question: None,
            ..target
        })
    }
}

struct Queued {
    target: Target,
    to: String,
    at: Instant,
}

#[derive(Default)]
struct Queue(Vec<Queued>);

impl Queue {
    fn push(&mut self, target: Target, to: &str, now: Instant) {
        self.0.retain(|q| now.duration_since(q.at) < TTL);
        if self.0.len() >= MAX_QUEUED {
            self.0.remove(0);
        }
        self.0.push(Queued {
            target,
            to: to.into(),
            at: now,
        });
    }

    fn claim(&mut self, to: &str, now: Instant) -> Vec<Target> {
        self.0.retain(|q| now.duration_since(q.at) < TTL);
        let (mine, rest): (Vec<_>, Vec<_>) = std::mem::take(&mut self.0)
            .into_iter()
            .partition(|q| q.to == to);
        self.0 = rest;
        mine.into_iter().map(|q| q.target).collect()
    }
}

#[derive(Default)]
pub struct Activation {
    routes: Mutex<Routes>,
    queue: Mutex<Queue>,
    #[cfg(target_os = "linux")]
    bus: std::sync::OnceLock<Option<xdg::Bus>>,
}

impl Activation {
    pub fn register(&self, targets: Vec<(String, Target)>) -> Option<u64> {
        self.routes.lock().ok()?.register(targets)
    }

    pub fn forget(&self, token: u64) {
        if let Ok(mut routes) = self.routes.lock() {
            routes.forget(token);
        }
    }
}

fn forget<R: Runtime>(app: &AppHandle<R>, token: u64) {
    if let Some(state) = app.try_state::<Activation>() {
        state.forget(token);
    }
}

/// A genuine click on the notification `token` was posted with. `startup` is
/// the activation token a Wayland notification server hands over with it.
fn activate<R: Runtime>(app: &AppHandle<R>, token: u64, startup: Option<String>) {
    // What is still waiting is read before the routes are locked: notify_attention
    // holds the attention lock first, so the two are never held the other way round.
    let pending = crate::notifications::pending(app);
    let Some(state) = app.try_state::<Activation>() else {
        return;
    };
    let Some(target) = state
        .routes
        .lock()
        .ok()
        .and_then(|mut r| r.resolve(token, &pending))
    else {
        return;
    };
    let handle = app.clone();
    let _ = app.run_on_main_thread(move || deliver(&handle, target, startup));
}

fn deliver<R: Runtime>(app: &AppHandle<R>, target: Target, startup: Option<String>) {
    let Some(window) = crate::links::deliverable_window(app) else {
        return;
    };
    let label = window.label().to_string();
    let state = app.state::<Activation>();
    let Ok(mut queue) = state.queue.lock() else {
        return;
    };
    queue.push(target, &label, Instant::now());
    drop(queue);
    #[cfg(target_os = "linux")]
    if let (Some(startup), Ok(gtk)) = (startup.as_deref(), window.gtk_window()) {
        // On Wayland only a window presented with the click's own activation
        // token may take focus; on X11 the id is a harmless startup hint.
        use gtk::prelude::GtkWindowExt;
        gtk.set_startup_id(startup);
    }
    #[cfg(not(target_os = "linux"))]
    let _ = startup;
    crate::links::bring_forward(&window);
    let _ = window.emit_to(label.as_str(), ACTIVATED, ());
}

/// Called from the app's window-event hook: a closed window's targets go with it.
pub fn on_destroyed<R: Runtime>(app: &AppHandle<R>, label: &str) {
    if let Some(state) = app.try_state::<Activation>() {
        if let Ok(mut queue) = state.queue.lock() {
            queue.0.retain(|q| q.to != label);
        }
    }
}

/// The notification targets queued for the calling window. Only codeaf's own
/// windows may ask.
#[tauri::command]
pub fn notify_claim<R: Runtime>(
    app: AppHandle<R>,
    webview: Webview<R>,
) -> Result<Vec<Target>, String> {
    let me = trusted(&webview)?;
    let state = app.state::<Activation>();
    let mut queue = state
        .queue
        .lock()
        .map_err(|_| "Notifications are unavailable".to_string())?;
    Ok(queue.claim(&me, Instant::now()))
}

/// Posts one notification. With a token, the platform's activation callback
/// for this notification, and nothing else, opens its target. Returns false
/// only when nothing could be handed to the system; a failure the system
/// reports later draws the person's attention to the window instead.
pub fn show<R: Runtime>(app: &AppHandle<R>, title: &str, body: &str, token: Option<u64>) -> bool {
    #[cfg(target_os = "linux")]
    {
        let state = app.state::<Activation>();
        let bus = state.bus.get_or_init(|| xdg::Bus::start(app.clone()));
        if let Some(bus) = bus {
            bus.post(app.clone(), title.into(), body.into(), token);
            return true;
        }
    }
    #[cfg(target_os = "macos")]
    {
        mac::post(app, title.into(), body.into(), token);
        true
    }
    #[cfg(windows)]
    {
        win::post(app, title.into(), body.into(), token);
        true
    }
    #[cfg(not(any(target_os = "macos", windows)))]
    {
        // No session bus to listen on: post the way the plugin does, and say
        // honestly that this notification opens nothing in particular.
        if let Some(token) = token {
            forget(app, token);
        }
        use tauri_plugin_notification::NotificationExt;
        app.notification()
            .builder()
            .title(title)
            .body(body)
            .show()
            .is_ok()
    }
}

/// The freedesktop notification service over the session bus. One connection
/// posts every notification and hears every signal, because servers such as
/// dunst send `ActionInvoked` only to the connection that posted.
#[cfg(target_os = "linux")]
mod xdg {
    use std::collections::{HashMap, VecDeque};
    use std::sync::{Arc, Mutex};

    use tauri::{AppHandle, Runtime};
    use zbus::blocking::{Connection, MessageIterator};
    use zbus::message::Type;
    use zbus::zvariant::Value;
    use zbus::MatchRule;

    const SERVICE: &str = "org.freedesktop.Notifications";
    const PATH: &str = "/org/freedesktop/Notifications";
    /// Notification ids the click listener remembers, oldest dropped first.
    const REMEMBERED: usize = super::MAX_ROUTES * 2;
    const MAX_STARTUP: usize = 512;

    /// One notification the server accepted: its id, the bus name of the
    /// server that answered, and the route token it opens.
    struct Shown {
        id: u32,
        server: String,
        token: u64,
    }

    #[derive(Default)]
    struct Posted {
        shown: VecDeque<Shown>,
        /// The activation token a server sent ahead of the click, by id.
        startup: HashMap<u32, String>,
    }

    impl Posted {
        fn remember(&mut self, shown: Shown) {
            if self.shown.len() >= REMEMBERED {
                if let Some(old) = self.shown.pop_front() {
                    self.startup.remove(&old.id);
                }
            }
            self.shown.push_back(shown);
        }

        /// Whether `id` is a notification that `sender` posted for us. A signal
        /// from any other connection on the bus, even one naming our id, is not
        /// a click on our notification.
        fn ours(&self, id: u32, sender: &str) -> bool {
            self.shown.iter().any(|s| s.id == id && s.server == sender)
        }

        fn take(&mut self, id: u32, sender: &str) -> Option<u64> {
            let at = self
                .shown
                .iter()
                .position(|s| s.id == id && s.server == sender)?;
            self.startup.remove(&id);
            self.shown.remove(at).map(|s| s.token)
        }
    }

    pub struct Bus {
        connection: Connection,
        posted: Arc<Mutex<Posted>>,
    }

    impl Bus {
        pub fn start<R: Runtime>(app: AppHandle<R>) -> Option<Bus> {
            let connection = Connection::session().ok()?;
            let rule = MatchRule::builder()
                .msg_type(Type::Signal)
                .interface(SERVICE)
                .ok()?
                .path(PATH)
                .ok()?
                .build();
            // Subscribed before the first notification is posted, so no click
            // can arrive before anyone is listening.
            let signals = MessageIterator::for_match_rule(rule, &connection, Some(64)).ok()?;
            let posted = Arc::new(Mutex::new(Posted::default()));
            let heard = Arc::clone(&posted);
            std::thread::Builder::new()
                .name("codeaf-notification-clicks".into())
                .spawn(move || listen(app, signals, heard))
                .ok()?;
            Some(Bus { connection, posted })
        }

        pub fn post<R: Runtime>(
            &self,
            app: AppHandle<R>,
            title: String,
            body: String,
            token: Option<u64>,
        ) {
            let connection = self.connection.clone();
            let posted = Arc::clone(&self.posted);
            // A session-bus round trip may stall on a hung server; never on the main thread.
            tauri::async_runtime::spawn_blocking(move || {
                let name = app.package_info().name.clone();
                // Only a notification with somewhere to go offers the default
                // action; a server then makes the whole body clickable.
                let actions: Vec<&str> = if token.is_some() {
                    vec!["default", "Open"]
                } else {
                    Vec::new()
                };
                let hints: HashMap<&str, Value> = HashMap::new();
                let sent = connection.call_method(
                    Some(SERVICE),
                    PATH,
                    Some(SERVICE),
                    "Notify",
                    &(
                        name.as_str(),
                        0u32,
                        name.as_str(),
                        title.as_str(),
                        body.as_str(),
                        actions,
                        hints,
                        -1i32,
                    ),
                );
                let answer = sent.ok().and_then(|reply| {
                    let server = reply.header().sender()?.to_string();
                    let id = reply.body().deserialize::<u32>().ok()?;
                    Some((id, server))
                });
                match (answer, token) {
                    (Some((id, server)), Some(token)) => {
                        if let Ok(mut posted) = posted.lock() {
                            posted.remember(Shown { id, server, token });
                        }
                    }
                    (Some(_), None) => {}
                    (None, token) => {
                        if let Some(token) = token {
                            super::forget(&app, token);
                        }
                        crate::notifications::attract(&app);
                    }
                }
            });
        }
    }

    fn listen<R: Runtime>(app: AppHandle<R>, signals: MessageIterator, posted: Arc<Mutex<Posted>>) {
        for message in signals {
            let Ok(message) = message else { continue };
            let header = message.header();
            let member = header.member().map(|m| m.as_str().to_owned());
            let Some(sender) = header.sender().map(|s| s.to_string()) else {
                continue;
            };
            let Ok(mut known) = posted.lock() else { return };
            match member.as_deref() {
                Some("ActivationToken") => {
                    if let Ok((id, startup)) = message.body().deserialize::<(u32, String)>() {
                        if known.ours(id, &sender)
                            && startup.len() <= MAX_STARTUP
                            && !startup.chars().any(char::is_control)
                        {
                            known.startup.insert(id, startup);
                        }
                    }
                }
                Some("ActionInvoked") => {
                    if let Ok((id, action)) = message.body().deserialize::<(u32, String)>() {
                        if action != "default" || !known.ours(id, &sender) {
                            continue;
                        }
                        let startup = known.startup.get(&id).cloned();
                        if let Some(token) = known.take(id, &sender) {
                            drop(known);
                            super::activate(&app, token, startup);
                        }
                    }
                }
                Some("NotificationClosed") => {
                    if let Ok((id, _reason)) = message.body().deserialize::<(u32, u32)>() {
                        if let Some(token) = known.take(id, &sender) {
                            drop(known);
                            super::forget(&app, token);
                        }
                    }
                }
                _ => {}
            }
        }
    }

    #[cfg(test)]
    mod tests {
        use super::*;

        fn shown(id: u32, token: u64) -> Shown {
            Shown {
                id,
                server: ":1.42".into(),
                token,
            }
        }

        #[test]
        fn a_taken_id_is_gone_and_others_stay() {
            let mut posted = Posted::default();
            posted.remember(shown(7, 1));
            posted.remember(shown(8, 2));
            assert_eq!(posted.take(7, ":1.42"), Some(1));
            assert_eq!(posted.take(7, ":1.42"), None);
            assert_eq!(posted.take(8, ":1.42"), Some(2));
        }

        #[test]
        fn only_the_server_that_posted_it_can_click_it() {
            let mut posted = Posted::default();
            posted.remember(shown(7, 1));
            assert!(!posted.ours(7, ":1.99"));
            assert_eq!(
                posted.take(7, ":1.99"),
                None,
                "another connection naming our id"
            );
            assert_eq!(posted.take(7, ":1.42"), Some(1));
        }

        #[test]
        fn what_is_remembered_is_bounded() {
            let mut posted = Posted::default();
            for n in 0..(REMEMBERED as u32 + 5) {
                posted.remember(shown(n, u64::from(n)));
                posted.startup.insert(n, "token".into());
            }
            assert_eq!(posted.shown.len(), REMEMBERED);
            assert!(!posted.ours(0, ":1.42"));
            assert!(posted.startup.len() <= REMEMBERED);
        }
    }
}

/// macOS: the notification center's delegate reports a click on the content of
/// the very notification a waiting thread posted.
#[cfg(target_os = "macos")]
mod mac {
    use std::sync::atomic::{AtomicUsize, Ordering};

    use mac_notification_sys::{Notification, NotificationResponse};
    use tauri::{AppHandle, Runtime};

    /// Each clickable notification holds one parked thread until it is clicked
    /// or cleared from Notification Center. Past this many, a new one is posted
    /// without a route rather than growing the process.
    const MAX_WAITING: usize = 16;
    static WAITING: AtomicUsize = AtomicUsize::new(0);

    pub fn post<R: Runtime>(app: &AppHandle<R>, title: String, body: String, token: Option<u64>) {
        let identifier = if tauri::is_dev() {
            "com.apple.Terminal".to_string()
        } else {
            app.config().identifier.clone()
        };
        // Set once for the process; the plugin's own fallback sets the same.
        let _ = mac_notification_sys::set_application(&identifier);
        let token = token.filter(|_| {
            if WAITING.fetch_add(1, Ordering::SeqCst) < MAX_WAITING {
                true
            } else {
                WAITING.fetch_sub(1, Ordering::SeqCst);
                false
            }
        });
        let owned = app.clone();
        let spawned = std::thread::Builder::new()
            .name("codeaf-notification".into())
            .spawn(move || {
                let mut note = Notification::new();
                note.title(&title).message(&body);
                if token.is_some() {
                    note.wait_for_click(true);
                } else {
                    note.asynchronous(true);
                }
                let answer = note.send();
                if token.is_some() {
                    WAITING.fetch_sub(1, Ordering::SeqCst);
                }
                match (answer, token) {
                    (Ok(NotificationResponse::Click), Some(token)) => {
                        super::activate(&owned, token, None)
                    }
                    (Ok(_), Some(token)) => super::forget(&owned, token),
                    (Ok(_), None) => {}
                    (Err(_), token) => {
                        if let Some(token) = token {
                            super::forget(&owned, token);
                        }
                        crate::notifications::attract(&owned);
                    }
                }
            });
        if spawned.is_err() {
            if let Some(token) = token {
                WAITING.fetch_sub(1, Ordering::SeqCst);
                super::forget(app, token);
            }
        }
    }
}

/// Windows: the toast's own `Activated` event, raised in this process.
#[cfg(windows)]
mod win {
    use tauri::{AppHandle, Runtime};
    use tauri_winrt_notification::{Toast, ToastDismissalReason};

    pub fn post<R: Runtime>(app: &AppHandle<R>, title: String, body: String, token: Option<u64>) {
        // The same identity the plugin uses: an installed app posts as itself,
        // a build run from its target folder has none and posts as PowerShell.
        let installed = tauri::utils::platform::current_exe()
            .ok()
            .and_then(|exe| exe.parent().map(|dir| dir.display().to_string()))
            .is_some_and(|dir| {
                let sep = std::path::MAIN_SEPARATOR;
                !dir.ends_with(&format!("{sep}target{sep}debug"))
                    && !dir.ends_with(&format!("{sep}target{sep}release"))
            });
        let app_id = if installed {
            app.config().identifier.clone()
        } else {
            Toast::POWERSHELL_APP_ID.to_string()
        };
        let app = app.clone();
        tauri::async_runtime::spawn_blocking(move || {
            let mut toast = Toast::new(&app_id).title(&title).text1(&body);
            if let Some(token) = token {
                let clicked = app.clone();
                let closed = app.clone();
                toast = toast
                    .on_activated(move |_| {
                        super::activate(&clicked, token, None);
                        Ok(())
                    })
                    // A toast that times out still waits in Action Center and can
                    // be clicked there; only the person clearing it ends the route.
                    .on_dismissed(move |reason| {
                        if reason == Some(ToastDismissalReason::UserCanceled) {
                            super::forget(&closed, token);
                        }
                        Ok(())
                    });
            }
            if toast.show().is_err() {
                if let Some(token) = token {
                    super::forget(&app, token);
                }
                crate::notifications::attract(&app);
            }
        });
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    fn target(chat: &str, question: Option<(&str, u64)>) -> Target {
        Target {
            item_id: question.map(|(kind, id)| format!("{chat}:{kind}:{id}")),
            chat_id: chat.into(),
            question: question.map(|(kind, id)| Question {
                kind: kind.into(),
                id,
            }),
        }
    }

    fn pending(ids: &[&str]) -> HashSet<String> {
        ids.iter().map(|s| s.to_string()).collect()
    }

    #[test]
    fn a_click_opens_its_own_notification_question_once() {
        let mut routes = Routes::default();
        let a = routes
            .register(vec![(
                "s1:consent:7".into(),
                target("s1", Some(("consent", 7))),
            )])
            .unwrap();
        let b = routes
            .register(vec![(
                "s2:choice:3".into(),
                target("s2", Some(("choice", 3))),
            )])
            .unwrap();
        let now = pending(&["s1:consent:7", "s2:choice:3"]);
        assert_eq!(
            routes.resolve(b, &now),
            Some(target("s2", Some(("choice", 3))))
        );
        assert_eq!(routes.resolve(b, &now), None, "a click is spent once");
        assert_eq!(
            routes.resolve(a, &now),
            Some(target("s1", Some(("consent", 7))))
        );
    }

    #[test]
    fn an_unknown_token_opens_nothing() {
        let mut routes = Routes::default();
        routes.register(vec![("x".into(), target("s1", None))]);
        assert_eq!(routes.resolve(999, &pending(&["x"])), None);
        assert_eq!(Routes::default().register(Vec::new()), None);
    }

    #[test]
    fn a_grouped_click_skips_answered_questions() {
        let mut routes = Routes::default();
        let token = routes
            .register(vec![
                ("s1:consent:1".into(), target("s1", Some(("consent", 1)))),
                ("s2:consent:2".into(), target("s2", Some(("consent", 2)))),
            ])
            .unwrap();
        assert_eq!(
            routes.resolve(token, &pending(&["s2:consent:2"])),
            Some(target("s2", Some(("consent", 2))))
        );
    }

    #[test]
    fn an_answered_question_opens_its_conversation_only() {
        let mut routes = Routes::default();
        let token = routes
            .register(vec![(
                "s1:consent:1".into(),
                target("s1", Some(("consent", 1))),
            )])
            .unwrap();
        assert_eq!(
            routes.resolve(token, &pending(&[])),
            Some(target("s1", None))
        );
    }

    #[test]
    fn routes_are_bounded_and_forgettable() {
        let mut routes = Routes::default();
        let first = routes
            .register(vec![("a".into(), target("s", None))])
            .unwrap();
        for _ in 0..MAX_ROUTES {
            routes.register(vec![("a".into(), target("s", None))]);
        }
        assert_eq!(routes.live.len(), MAX_ROUTES);
        assert_eq!(routes.resolve(first, &pending(&["a"])), None);
        let last = routes.next;
        routes.forget(last);
        assert_eq!(routes.resolve(last, &pending(&["a"])), None);
    }

    #[test]
    fn targets_go_to_their_window_once_and_expire() {
        let mut queue = Queue::default();
        let now = Instant::now();
        queue.push(target("s1", None), "main", now);
        queue.push(target("s2", None), "w-2", now);
        assert!(queue.claim("w-3", now).is_empty());
        assert_eq!(queue.claim("main", now), vec![target("s1", None)]);
        assert!(queue.claim("main", now).is_empty());
        assert!(queue.claim("w-2", now + TTL).is_empty());
    }

    #[test]
    fn ids_and_questions_are_checked() {
        assert!(is_chat_id("9446cc2627f3deae"));
        assert!(!is_chat_id("../etc"));
        assert!(!is_chat_id(""));
        assert!(!is_chat_id(&"a".repeat(MAX_ID + 1)));
        let ok = Question {
            kind: "consent".into(),
            id: 7,
        };
        assert!(ok.sound());
        assert!(!Question {
            id: 0,
            ..ok.clone()
        }
        .sound());
        assert!(!Question {
            id: MAX_QUESTION + 1,
            ..ok.clone()
        }
        .sound());
        assert!(!Question {
            kind: "a b".into(),
            ..ok.clone()
        }
        .sound());
        assert!(!Question {
            kind: String::new(),
            ..ok
        }
        .sound());
    }

    #[test]
    fn the_renderer_hears_only_the_target() {
        let wire = serde_json::to_value(target("s1", Some(("consent", 7)))).unwrap();
        assert_eq!(
            wire,
            serde_json::json!({"itemId": "s1:consent:7", "chatId": "s1", "question": {"kind": "consent", "id": 7}})
        );
        let bare = serde_json::to_value(target("s1", None)).unwrap();
        assert_eq!(bare, serde_json::json!({"chatId": "s1"}));
    }
}
