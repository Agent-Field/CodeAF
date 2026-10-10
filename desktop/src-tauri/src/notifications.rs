//! "Needs you" signals that reach the person while codeaf is in the background:
//! one system notification per new item, and the dock or launcher badge.
//!
//! Every window watches the same attention feed, so the bookkeeping that keeps a
//! question from being announced twice lives here, once for the whole app,
//! rather than in each window. Nothing here reads a conversation; the renderer
//! sends the few words a notification shows.
//!
//! WHAT THE PLATFORM CAN AND CANNOT TELL US. `tauri-plugin-notification` 2.5 on
//! desktop answers every permission question with "granted" without asking the
//! operating system, and posts on a background task whose failure it discards.
//! So `verified` is false on desktop: the system may still silence codeaf, and
//! the badge and the Inbox stay the source of truth. Grouping is ignored by the
//! plugin on desktop; codeaf retains the place identity for each notification. The plugin also drops
//! click actions, so codeaf posts through the platform's own notification
//! service and a click opens the question it names (activation.rs).
//!
//! WHICH WINDOW IS RIGHT. Every window reads the engine's world feed on a stream
//! of its own and posts its whole list, so two windows can disagree for a moment:
//! one has applied a record the other has not. Each list therefore carries the
//! feed sequence it was derived from, and only a list at the newest sequence any
//! open window has reported may say what is pending. An older list is ignored
//! rather than merged, because merging would bring an answered question back and
//! announce it again. The engine numbers its feed per process, so a window whose
//! feed carries a process identity. A new identity retires the old one; delayed
//! posts from that retired engine cannot reset the authority or restore questions.

use std::collections::{HashMap, HashSet};
use std::sync::Mutex;

use serde::{Deserialize, Serialize};
use tauri::{AppHandle, Manager, Runtime, UserAttentionType, Webview};
use tauri_plugin_notification::PermissionState;

use crate::activation::{is_chat_id, Activation, Question, Target};
use crate::windows::{app_window, app_windows, trusted};

/// Bound retired engine identities so an unknown old feed is refused rather than trusted.
const REMEMBERED: usize = 2048;
const MAX_ITEMS: usize = 256;
const MAX_TEXT: usize = 400;
const MAX_ID: usize = 256;
const MAX_BADGE: u32 = 9999;

#[derive(Clone, Copy, Debug, PartialEq, Eq, Serialize, Deserialize)]
#[serde(rename_all = "camelCase")]
pub enum AttentionKind {
    NeedsYou,
    Failed,
    Running,
}

#[derive(Clone, Debug, Deserialize)]
#[serde(deny_unknown_fields, rename_all = "camelCase")]
pub struct AttentionItem {
    pub id: String,
    pub kind: AttentionKind,
    pub chat_title: String,
    pub text: String,
    /// A foreground or nonblocking item stays pending without announcing it.
    #[serde(default)]
    pub silent: bool,
    #[serde(default)]
    pub place_id: Option<String>,
    #[serde(default)]
    pub place_name: Option<String>,
    /// The conversation a click on this item opens.
    #[serde(default)]
    pub chat_id: Option<String>,
    /// The question a click focuses in that conversation's tray.
    #[serde(default)]
    pub question: Option<Question>,
}

impl AttentionItem {
    fn target(&self) -> Option<Target> {
        Some(Target {
            item_id: Some(self.id.clone()),
            chat_id: self.chat_id.clone()?,
            question: self.question.clone(),
        })
    }
}

#[derive(Clone, Copy, Debug, PartialEq, Eq, Serialize)]
#[serde(rename_all = "lowercase")]
pub enum Permission {
    Granted,
    Denied,
    Unavailable,
}

#[derive(Debug, Serialize)]
pub struct PermissionAnswer {
    pub state: Permission,
    /// True only when the operating system itself was asked.
    pub verified: bool,
}

#[derive(Clone, Copy, Debug, PartialEq, Eq, Serialize)]
#[serde(rename_all = "kebab-case")]
pub enum Skipped {
    Focused,
    NothingNew,
    Denied,
    /// Another window has already reported a newer reading of the feed.
    Stale,
}

#[derive(Debug, Serialize)]
pub struct Posted {
    pub posted: usize,
    pub groups: usize,
    pub skipped: Option<Skipped>,
}

/// One notification's words.
#[derive(Debug, PartialEq, Eq)]
pub struct Note {
    pub group: String,
    pub title: String,
    pub body: String,
    /// Where a click goes, by attention item, in the order the body names them.
    pub targets: Vec<(String, Target)>,
}

#[derive(Default)]
struct Seen {
    /// Identities live for the app process so a long session cannot replay an old question as new.
    ids: HashSet<String>,
}

impl Seen {
    fn insert(&mut self, id: &str) -> bool {
        self.ids.insert(id.to_string())
    }
}

/// The feed sequence each open window last reported.
#[derive(Default)]
struct Feed {
    windows: HashMap<String, u64>,
    epoch: Option<String>,
    retired: HashSet<String>,
}

impl Feed {
    fn accept_epoch(&mut self, epoch: &str) -> bool {
        if epoch.len() > 128 || epoch.is_empty() {
            return false;
        }
        if self.epoch.as_deref() == Some(epoch) {
            return true;
        }
        if self.retired.contains(epoch) || self.retired.len() >= REMEMBERED {
            return false;
        }
        if let Some(previous) = self.epoch.replace(epoch.to_string()) {
            self.retired.insert(previous);
        }
        self.windows.clear();
        true
    }
    fn accept(&mut self, window: &str, seq: u64, open: &HashSet<String>) -> bool {
        self.windows
            .retain(|label, _| open.contains(label) || label == window);
        if self.windows.get(window).is_some_and(|&mine| seq < mine) {
            return false;
        }
        self.windows.insert(window.to_string(), seq);
        self.current(window, seq)
    }
    fn current(&self, _window: &str, seq: u64) -> bool {
        self.windows.values().all(|&other| other <= seq)
    }
}

/// Everything the app remembers about the attention feed, across all windows.
#[derive(Default)]
struct Book {
    /// What has been announced (or seen while a window was focused).
    seen: Seen,
    /// The ids the newest list named: what a click may still focus.
    pending: HashSet<String>,
    feed: Feed,
}

#[derive(Default)]
pub struct Attention(Mutex<Book>, Mutex<()>);

fn clean(text: &str, max: usize) -> String {
    let flat: String = text
        .chars()
        .map(|c| if c.is_control() { ' ' } else { c })
        .collect();
    let flat = flat.split_whitespace().collect::<Vec<_>>().join(" ");
    if flat.chars().count() <= max {
        flat
    } else {
        let cut: String = flat.chars().take(max.saturating_sub(1)).collect();
        format!("{}…", cut.trim_end())
    }
}

fn checked(items: &[AttentionItem]) -> Result<(), String> {
    let ok = items.len() <= MAX_ITEMS
        && items.iter().all(|i| {
            !i.id.is_empty()
                && i.id.len() <= MAX_ID
                && i.place_id.as_deref().is_none_or(|p| p.len() <= MAX_ID)
                && i.chat_id.as_deref().is_none_or(is_chat_id)
                && i.question
                    .as_ref()
                    .is_none_or(|q| q.sound() && i.chat_id.is_some())
        });
    if ok {
        Ok(())
    } else {
        Err("Those attention items cannot be shown".into())
    }
}

/// Whether an item deserves a notification at all: running work never does.
fn notifies(item: &AttentionItem) -> bool {
    matches!(item.kind, AttentionKind::NeedsYou | AttentionKind::Failed)
}

/// Each new item keeps its own words and place identity.
pub fn compose(items: &[&AttentionItem]) -> Vec<Note> {
    items
        .iter()
        .map(|item| {
            let chat = clean(&item.chat_title, 80);
            let text = clean(&item.text, MAX_TEXT);
            let body = match (chat.is_empty(), text.is_empty()) {
                (false, false) => format!("{chat} — {text}"),
                (false, true) => chat,
                (true, false) => text,
                (true, true) => String::new(),
            };
            Note {
                group: item.place_id.clone().unwrap_or_else(|| "now".into()),
                title: item
                    .place_name
                    .as_deref()
                    .map(|name| clean(name, 80))
                    .filter(|name| !name.is_empty())
                    .unwrap_or_else(|| "codeaf".into()),
                body,
                targets: item
                    .target()
                    .map(|target| vec![(item.id.clone(), target)])
                    .unwrap_or_default(),
            }
        })
        .collect()
}

/// Records what is pending and returns only the items never announced before.
///
/// A replayed identity is not a new question. Foreground and nonblocking items
/// are remembered silently so a later blur cannot announce old work.
fn fresh<'a>(book: &mut Book, items: &'a [AttentionItem]) -> Vec<&'a AttentionItem> {
    let wanted: Vec<&AttentionItem> = items.iter().filter(|i| notifies(i)).collect();
    book.pending = wanted.iter().map(|i| i.id.clone()).collect();
    wanted
        .into_iter()
        .filter(|i| book.seen.insert(&i.id) && !i.silent)
        .collect()
}

fn permission<R: Runtime>(app: &AppHandle<R>, ask: bool) -> PermissionAnswer {
    let Some(plugin) = app.try_state::<tauri_plugin_notification::Notification<R>>() else {
        return PermissionAnswer {
            state: Permission::Unavailable,
            verified: false,
        };
    };
    let answer = if ask {
        plugin.request_permission()
    } else {
        plugin.permission_state()
    };
    let state = match answer {
        Ok(PermissionState::Granted) => Permission::Granted,
        Ok(PermissionState::Denied) => Permission::Denied,
        // Prompt states only exist on mobile; on desktop nothing can be asked.
        Ok(_) => Permission::Denied,
        Err(_) => Permission::Unavailable,
    };
    PermissionAnswer {
        state,
        verified: cfg!(mobile),
    }
}

#[tauri::command]
pub fn notify_permission<R: Runtime>(
    app: AppHandle<R>,
    webview: Webview<R>,
) -> Result<PermissionAnswer, String> {
    trusted(&webview)?;
    Ok(permission(&app, false))
}

#[tauri::command]
pub fn notify_request_permission<R: Runtime>(
    app: AppHandle<R>,
    webview: Webview<R>,
) -> Result<PermissionAnswer, String> {
    trusted(&webview)?;
    Ok(permission(&app, true))
}

/// The attention items still waiting, as the newest list had them.
pub fn pending<R: Runtime>(app: &AppHandle<R>) -> HashSet<String> {
    app.try_state::<Attention>()
        .and_then(|state| state.0.lock().ok().map(|book| book.pending.clone()))
        .unwrap_or_default()
}

fn open_windows<R: Runtime>(app: &AppHandle<R>) -> HashSet<String> {
    app_windows(app)
        .into_iter()
        .map(|(label, _)| label)
        .collect()
}

fn app_focused<R: Runtime>(app: &AppHandle<R>) -> bool {
    app_windows(app)
        .iter()
        .any(|(_, w)| w.is_focused().unwrap_or(false))
}

#[tauri::command]
pub fn notify_attention<R: Runtime>(
    app: AppHandle<R>,
    webview: Webview<R>,
    items: Vec<AttentionItem>,
    seq: u64,
    epoch: Option<String>,
) -> Result<Posted, String> {
    let me = trusted(&webview)?;
    checked(&items)?;
    let open = open_windows(&app);
    let state = app.state::<Attention>();
    // Keep command effects in the same order as accepted feed readings. Click
    // handlers only need the book mutex, which is released before posting.
    let _posting = state
        .1
        .lock()
        .map_err(|_| "Notifications are unavailable")?;
    let mut book = state
        .0
        .lock()
        .map_err(|_| "Notifications are unavailable")?;
    // A window behind the newest reading may neither announce nor change what is
    // pending: its list can still hold a question answered since.
    if !book.feed.accept_epoch(epoch.as_deref().unwrap_or("")) || !book.feed.accept(&me, seq, &open)
    {
        return Ok(Posted {
            posted: 0,
            groups: 0,
            skipped: Some(Skipped::Stale),
        });
    }
    // Remember what is pending even while focused, so switching away later does
    // not announce a question the person already saw in the window.
    let new = fresh(&mut book, &items);
    // Released before posting: a click's handler reads what is pending.
    drop(book);
    if app_focused(&app) {
        return Ok(Posted {
            posted: 0,
            groups: 0,
            skipped: Some(Skipped::Focused),
        });
    }
    if new.is_empty() {
        return Ok(Posted {
            posted: 0,
            groups: 0,
            skipped: Some(Skipped::NothingNew),
        });
    }
    let notes = compose(&new);
    let groups = notes
        .iter()
        .map(|note| &note.group)
        .collect::<HashSet<_>>()
        .len();
    if permission(&app, false).state != Permission::Granted {
        attract(&app);
        return Ok(Posted {
            posted: 0,
            groups,
            skipped: Some(Skipped::Denied),
        });
    }
    let routes = app.state::<Activation>();
    let mut posted = 0;
    for note in &notes {
        let token = routes.register(note.targets.clone());
        if crate::activation::show(&app, &note.title, &note.body, token) {
            posted += 1;
        } else if let Some(token) = token {
            routes.forget(token);
        }
    }
    if posted < notes.len() {
        attract(&app);
    }
    Ok(Posted {
        posted,
        groups,
        skipped: None,
    })
}

/// The fallback when a notification cannot be posted: the dock icon bounces once
/// on macOS, and the window is marked urgent on Linux.
pub fn attract<R: Runtime>(app: &AppHandle<R>) {
    let window =
        app_window(app, "main").or_else(|| app_windows(app).into_iter().next().map(|(_, w)| w));
    if let Some(window) = window {
        let _ = window.request_user_attention(Some(UserAttentionType::Informational));
    }
}

#[derive(Debug, Serialize)]
pub struct BadgeAnswer {
    pub applied: bool,
    #[serde(skip_serializing_if = "Option::is_none")]
    pub reason: Option<&'static str>,
}

pub fn badge_value(count: u32) -> Option<i64> {
    (count > 0).then(|| i64::from(count.min(MAX_BADGE)))
}

#[tauri::command]
pub fn badge_set<R: Runtime>(
    app: AppHandle<R>,
    webview: Webview<R>,
    count: u32,
    seq: u64,
    epoch: Option<String>,
) -> Result<BadgeAnswer, String> {
    let me = trusted(&webview)?;
    // The badge follows the same newest reading as the notifications; a window
    // behind it would put back a count that has already gone down.
    let state = app.state::<Attention>();
    let _posting = state
        .1
        .lock()
        .map_err(|_| "Notifications are unavailable")?;
    let stale = {
        let mut book = state
            .0
            .lock()
            .map_err(|_| "Notifications are unavailable")?;
        !book.feed.accept_epoch(epoch.as_deref().unwrap_or(""))
            || !book.feed.accept(&me, seq, &open_windows(&app))
    };
    if stale {
        return Ok(BadgeAnswer {
            applied: false,
            reason: Some("stale"),
        });
    }
    if cfg!(windows) {
        return Ok(BadgeAnswer {
            applied: false,
            reason: Some("unavailable"),
        });
    }
    // macOS has one dock badge for the app; Linux launchers show it per app too,
    // so setting it through any one codeaf window is enough.
    let Some(window) =
        app_window(&app, "main").or_else(|| app_windows(&app).into_iter().next().map(|(_, w)| w))
    else {
        return Ok(BadgeAnswer {
            applied: false,
            reason: Some("unavailable"),
        });
    };
    match window.set_badge_count(badge_value(count)) {
        // Linux hands the count to the launcher (Unity LauncherEntry) without an
        // answer; whether it is drawn depends on the desktop, so say so.
        Ok(()) => Ok(BadgeAnswer {
            applied: true,
            reason: cfg!(target_os = "linux").then_some("launcher-dependent"),
        }),
        Err(_) => Ok(BadgeAnswer {
            applied: false,
            reason: Some("unavailable"),
        }),
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    fn item(
        id: &str,
        kind: AttentionKind,
        place: Option<(&str, &str)>,
        chat: &str,
    ) -> AttentionItem {
        AttentionItem {
            id: id.into(),
            kind,
            chat_title: chat.into(),
            text: "Which branch should I use?".into(),
            silent: false,
            place_id: place.map(|p| p.0.into()),
            place_name: place.map(|p| p.1.into()),
            chat_id: None,
            question: None,
        }
    }

    fn asking(id: &str, chat: &str, kind: &str, question: u64) -> AttentionItem {
        AttentionItem {
            chat_id: Some(chat.into()),
            question: Some(Question {
                kind: kind.into(),
                id: question,
            }),
            ..item(id, AttentionKind::NeedsYou, MKT, chat)
        }
    }

    const MKT: Option<(&str, &str)> = Some(("pl_00000000000000aa", "Marketing"));

    #[test]
    fn the_same_item_never_announces_twice() {
        let mut book = Book::default();
        let items = vec![item("a", AttentionKind::NeedsYou, MKT, "Launch")];
        assert_eq!(fresh(&mut book, &items).len(), 1);
        assert_eq!(fresh(&mut book, &items).len(), 0);
    }

    #[test]
    fn a_silent_question_stays_quiet_after_blur() {
        let mut book = Book::default();
        let mut question = item("q", AttentionKind::NeedsYou, MKT, "Release");
        question.silent = true;
        assert!(fresh(&mut book, &[question.clone()]).is_empty());
        question.silent = false;
        assert!(fresh(&mut book, &[question]).is_empty());
    }

    #[test]
    fn running_never_notifies_and_failed_does() {
        let mut book = Book::default();
        let items = vec![
            item("r", AttentionKind::Running, MKT, "Build"),
            item("f", AttentionKind::Failed, MKT, "Deploy"),
        ];
        let got = fresh(&mut book, &items);
        assert_eq!(got.iter().map(|i| i.id.as_str()).collect::<Vec<_>>(), ["f"]);
        assert_eq!(book.pending, HashSet::from(["f".to_string()]));
    }

    #[test]
    fn an_answered_question_is_not_announced_again() {
        let mut book = Book::default();
        let a = vec![item("a", AttentionKind::NeedsYou, None, "Launch")];
        assert_eq!(fresh(&mut book, &a).len(), 1);
        assert_eq!(fresh(&mut book, &[]).len(), 0);
        assert!(book.pending.is_empty());
        assert_eq!(fresh(&mut book, &a).len(), 0);
    }

    #[test]
    fn a_failure_that_left_the_list_is_not_announced_again() {
        let mut book = Book::default();
        let f = vec![item("failed:s1:t", AttentionKind::Failed, None, "Nightly")];
        assert_eq!(fresh(&mut book, &f).len(), 1);
        // Marked seen, or aged out by one window's clock a moment before another's.
        assert_eq!(fresh(&mut book, &[]).len(), 0);
        assert!(
            book.pending.is_empty(),
            "it is no longer something a click focuses"
        );
        assert_eq!(fresh(&mut book, &f).len(), 0);
    }

    #[test]
    fn old_identities_remain_quiet_in_a_long_session() {
        let mut seen = Seen::default();
        for i in 0..REMEMBERED + 10 {
            seen.insert(&i.to_string());
        }
        assert_eq!(seen.ids.len(), REMEMBERED + 10);
        assert!(
            !seen.insert("0"),
            "an old question remains known after thousands of newer items"
        );
    }

    fn open(labels: &[&str]) -> HashSet<String> {
        labels.iter().map(|l| l.to_string()).collect()
    }

    /// One window's post as notify_attention handles it, without a platform.
    fn post<'a>(
        book: &mut Book,
        window: &str,
        seq: u64,
        items: &'a [AttentionItem],
    ) -> Option<Vec<&'a str>> {
        let windows = open(&["main", "w-2"]);
        if !book.feed.accept(window, seq, &windows) {
            return None;
        }
        Some(fresh(book, items).iter().map(|i| i.id.as_str()).collect())
    }

    fn failed(id: &str) -> AttentionItem {
        item(id, AttentionKind::Failed, None, "Nightly")
    }

    #[test]
    fn two_windows_alternating_announce_each_question_and_failure_once() {
        let mut book = Book::default();
        let both = vec![
            asking("s1:consent:7", "s1", "consent", 7),
            failed("failed:s9:t1"),
        ];
        let answered = vec![failed("failed:s9:t1")];
        let marked = vec![];
        // Both windows read seq 10; the first to post announces, the second repeats it.
        assert_eq!(
            post(&mut book, "main", 10, &both),
            Some(vec!["s1:consent:7", "failed:s9:t1"])
        );
        assert_eq!(post(&mut book, "w-2", 10, &both), Some(vec![]));
        // main reads the answer at 11; w-2, still at 10, posts its old list again.
        assert_eq!(post(&mut book, "main", 11, &answered), Some(vec![]));
        assert_eq!(
            post(&mut book, "w-2", 10, &both),
            None,
            "a stale list is ignored"
        );
        assert_eq!(book.pending, HashSet::from(["failed:s9:t1".to_string()]));
        // w-2 catches up; then the failure is marked seen and main is behind.
        assert_eq!(post(&mut book, "w-2", 11, &answered), Some(vec![]));
        assert_eq!(post(&mut book, "w-2", 12, &marked), Some(vec![]));
        assert_eq!(post(&mut book, "main", 11, &answered), None);
        assert_eq!(post(&mut book, "main", 12, &marked), Some(vec![]));
        assert!(book.pending.is_empty());
        // Nothing was announced twice across the whole alternation.
    }

    #[test]
    fn a_stale_window_cannot_point_a_grouped_click_at_an_answered_question() {
        use crate::activation::Routes;
        let mut book = Book::default();
        let mut routes = Routes::default();
        let a = asking("s1:consent:7", "s1", "consent", 7);
        let b = asking("s2:choice:3", "s2", "choice", 3);
        let first = vec![a.clone(), b.clone()];
        let new = post(&mut book, "main", 5, &first).unwrap();
        assert_eq!(new.len(), 2);
        let token = routes.register(
            compose(&first.iter().collect::<Vec<_>>())
                .into_iter()
                .flat_map(|note| note.targets)
                .collect(),
        );
        // s1's question is answered: main reads it at 6, then w-2 posts its reading from 5.
        assert!(post(&mut book, "main", 6, std::slice::from_ref(&b)).is_some());
        assert!(post(&mut book, "w-2", 5, &first).is_none());
        assert_eq!(
            routes.resolve(token.unwrap(), &book.pending),
            Some(Target {
                item_id: Some("s2:choice:3".into()),
                chat_id: "s2".into(),
                question: Some(Question {
                    kind: "choice".into(),
                    id: 3
                })
            }),
            "the click goes to the question still waiting, not the answered one"
        );
    }

    #[test]
    fn an_engine_restart_restarts_the_count_for_every_window() {
        let mut book = Book::default();
        let q = vec![asking("s1:consent:7", "s1", "consent", 7)];
        assert!(post(&mut book, "main", 900, &q).is_some());
        assert!(post(&mut book, "w-2", 900, &q).is_some());
        // The engine restarts; main reconnects first and is reset to seq 3.
        assert!(
            {
                assert!(book.feed.accept_epoch("restarted"));
                post(&mut book, "main", 3, &[]).is_some()
            },
            "main's own count went backwards"
        );
        assert!(book.pending.is_empty());
        // w-2 was reset too; its new reading is not outranked by the old engine's.
        assert!(post(&mut book, "w-2", 3, &[]).is_some());
    }

    #[test]
    fn delayed_same_window_posts_and_badges_never_reset_authority() {
        let mut feed = Feed::default();
        assert!(feed.accept_epoch("engine-a"));
        assert!(!feed.accept_epoch(""));
        assert!(!feed.accept_epoch(&"x".repeat(129)));
        let windows = open(&["main", "w-2"]);
        assert!(feed.accept("main", 10, &windows));
        assert!(!feed.current("main", 9));
        assert!(!feed.accept("main", 9, &windows));
        assert!(!feed.accept("w-2", 9, &windows));
        assert!(feed.current("main", 10));
    }

    #[test]
    fn retired_engine_cannot_restore_questions_after_restart() {
        let mut book = Book::default();
        let windows = open(&["main", "w-2"]);
        assert!(book.feed.accept_epoch("engine-a"));
        assert!(book.feed.accept("main", 900, &windows));
        assert!(book.feed.accept("w-2", 900, &windows));
        assert!(book.feed.accept_epoch("engine-b"));
        assert!(book.feed.accept("main", 3, &windows));
        assert!(!book.feed.accept_epoch("engine-a"));
        assert!(book.feed.current("main", 3));
        assert!(book.feed.accept("w-2", 3, &windows));
        assert!(!book.feed.accept("main", 2, &windows));
    }

    #[test]
    fn a_closed_window_no_longer_outranks_anyone() {
        let mut feed = Feed::default();
        assert!(feed.accept("w-7", 50, &open(&["main", "w-7"])));
        assert!(!feed.accept("main", 40, &open(&["main", "w-7"])));
        assert!(feed.accept("main", 40, &open(&["main"])), "w-7 closed");
        assert_eq!(
            feed.windows.len(),
            1,
            "readings are kept for open windows only"
        );
        assert!(feed.current("main", 40));
        assert!(!feed.current("w-9", 39));
    }

    #[test]
    fn one_notification_per_item_named_after_its_place() {
        let a = item("a", AttentionKind::NeedsYou, MKT, "Launch plan");
        let b = item("b", AttentionKind::NeedsYou, MKT, "Pricing page");
        let c = item("c", AttentionKind::Failed, None, "Nightly");
        let notes = compose(&[&a, &b, &c]);
        assert_eq!(notes.len(), 3);
        let now = notes.iter().find(|n| n.group == "now").unwrap();
        assert_eq!(now.title, "codeaf");
        assert_eq!(now.body, "Nightly — Which branch should I use?");
        let mkt = notes
            .iter()
            .find(|n| n.group == "pl_00000000000000aa")
            .unwrap();
        assert_eq!(mkt.title, "Marketing");
        assert_eq!(mkt.body, "Launch plan — Which branch should I use?");
    }

    #[test]
    fn every_item_keeps_its_chat_title() {
        let items: Vec<AttentionItem> = (0..5)
            .map(|i| {
                item(
                    &i.to_string(),
                    AttentionKind::Failed,
                    MKT,
                    &format!("Chat {i}"),
                )
            })
            .collect();
        let refs: Vec<&AttentionItem> = items.iter().collect();
        assert_eq!(
            compose(&refs)[0].body,
            "Chat 0 — Which branch should I use?"
        );
    }

    #[test]
    fn words_are_flattened_and_bounded() {
        let mut odd = item("a", AttentionKind::NeedsYou, None, "Line\none\u{1b}[2J");
        odd.text = "x".repeat(MAX_TEXT * 2);
        let note = &compose(&[&odd])[0];
        assert!(!note.body.chars().any(char::is_control));
        assert!(note.body.starts_with("Line one [2J — "));
        assert!(note.body.chars().count() <= 80 + 3 + MAX_TEXT);
        assert!(note.body.ends_with('…'));
    }

    #[test]
    fn unknown_words_do_not_invent_a_body() {
        let mut bare = item("a", AttentionKind::Failed, None, "");
        bare.text = String::new();
        assert_eq!(compose(&[&bare])[0].body, "");
    }

    #[test]
    fn requests_are_bounded_and_strict() {
        assert!(checked(&[item("", AttentionKind::NeedsYou, None, "x")]).is_err());
        let many: Vec<_> = (0..=MAX_ITEMS)
            .map(|i| item(&i.to_string(), AttentionKind::NeedsYou, None, "x"))
            .collect();
        assert!(checked(&many).is_err());
        let bad = serde_json::json!({"id": "a", "kind": "needsYou", "chatTitle": "x", "text": "y", "icon": "/etc/x.png"});
        assert!(serde_json::from_value::<AttentionItem>(bad).is_err());
        let wire =
            serde_json::json!({"id": "a", "kind": "needsYou", "chatTitle": "x", "text": "y"});
        assert!(serde_json::from_value::<AttentionItem>(wire).is_ok());
    }

    #[test]
    fn each_notification_carries_the_targets_it_names_in_order() {
        let a = asking("s1:consent:7", "s1", "consent", 7);
        let b = asking("s2:choice:3", "s2", "choice", 3);
        let bare = item("c", AttentionKind::Failed, None, "Nightly");
        let notes = compose(&[&a, &bare, &b]);
        let mkt = notes
            .iter()
            .find(|n| n.group == "pl_00000000000000aa")
            .unwrap();
        let ids: Vec<&str> = mkt.targets.iter().map(|(id, _)| id.as_str()).collect();
        assert_eq!(ids, ["s1:consent:7"]);
        assert_eq!(
            mkt.targets[0].1,
            Target {
                item_id: Some("s1:consent:7".into()),
                chat_id: "s1".into(),
                question: Some(Question {
                    kind: "consent".into(),
                    id: 7
                })
            }
        );
        // An item that names no conversation gives its notification nowhere to go.
        let now = notes.iter().find(|n| n.group == "now").unwrap();
        assert!(now.targets.is_empty());
    }

    #[test]
    fn targets_are_checked_before_anything_is_posted() {
        assert!(checked(&[asking("a", "9446cc2627f3deae", "consent", 7)]).is_ok());
        assert!(checked(&[asking("a", "../../etc", "consent", 7)]).is_err());
        assert!(checked(&[asking("a", "s1", "consent", 0)]).is_err());
        assert!(checked(&[asking("a", "s1", "rm -rf", 7)]).is_err());
        let orphan = AttentionItem {
            chat_id: None,
            ..asking("a", "s1", "consent", 7)
        };
        assert!(
            checked(&[orphan]).is_err(),
            "a question needs its conversation"
        );
        let wire = serde_json::json!({"id": "a", "kind": "needsYou", "chatTitle": "x", "text": "y",
            "chatId": "s1", "question": {"kind": "consent", "id": 7}});
        let parsed = serde_json::from_value::<AttentionItem>(wire).unwrap();
        assert_eq!(parsed.chat_id.as_deref(), Some("s1"));
        let extra = serde_json::json!({"id": "a", "kind": "needsYou", "chatTitle": "x", "text": "y",
            "chatId": "s1", "question": {"kind": "consent", "id": 7, "url": "x"}});
        assert!(serde_json::from_value::<AttentionItem>(extra).is_err());
    }

    #[test]
    fn the_badge_clears_at_zero_and_is_capped() {
        assert_eq!(badge_value(0), None);
        assert_eq!(badge_value(3), Some(3));
        assert_eq!(badge_value(u32::MAX), Some(i64::from(MAX_BADGE)));
    }
}
