//! "Needs you" signals that reach the person while codeaf is in the background:
//! one system notification per place, and the dock or launcher badge.
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
//! plugin on desktop, so codeaf groups by place itself. The plugin also drops
//! click actions, so codeaf posts through the platform's own notification
//! service and a click opens the question it names (activation.rs).

use std::collections::{BTreeMap, HashSet, VecDeque};
use std::sync::Mutex;

use serde::{Deserialize, Serialize};
use tauri::{AppHandle, Manager, Runtime, UserAttentionType, Webview};
use tauri_plugin_notification::PermissionState;

use crate::activation::{is_chat_id, Activation, Question, Target};
use crate::windows::{app_window, app_windows, trusted};

/// Enough to remember every open question for a long day without growing.
const REMEMBERED: usize = 2048;
const MAX_ITEMS: usize = 256;
const MAX_TEXT: usize = 400;
const MAX_ID: usize = 256;
const MAX_BADGE: u32 = 9999;
/// Body lines name at most this many chats before "and N more".
const NAMED: usize = 3;

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
    ids: HashSet<String>,
    order: VecDeque<String>,
}

impl Seen {
    fn insert(&mut self, id: &str) -> bool {
        if self.ids.contains(id) {
            return false;
        }
        if self.order.len() >= REMEMBERED {
            if let Some(old) = self.order.pop_front() {
                self.ids.remove(&old);
            }
        }
        self.ids.insert(id.to_string());
        self.order.push_back(id.to_string());
        true
    }

    /// An item that is no longer pending may come back later as a new question.
    fn keep_only(&mut self, pending: &HashSet<&str>) {
        self.order.retain(|id| pending.contains(id.as_str()));
        self.ids.retain(|id| pending.contains(id.as_str()));
    }
}

#[derive(Default)]
pub struct Attention(Mutex<Seen>);

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

/// Groups the new items by place, one notification each, in a stable order.
pub fn compose(items: &[&AttentionItem]) -> Vec<Note> {
    let mut groups: BTreeMap<String, (String, Vec<&AttentionItem>)> = BTreeMap::new();
    for item in items {
        let key = item.place_id.clone().unwrap_or_else(|| "now".into());
        let title = item
            .place_name
            .as_deref()
            .map(|n| clean(n, 80))
            .filter(|n| !n.is_empty())
            .unwrap_or_else(|| "codeaf".into());
        groups
            .entry(key)
            .or_insert_with(|| (title, Vec::new()))
            .1
            .push(item);
    }
    groups
        .into_iter()
        .map(|(group, (title, items))| {
            let body = if let [one] = items.as_slice() {
                let chat = clean(&one.chat_title, 80);
                let text = clean(&one.text, MAX_TEXT);
                match (chat.is_empty(), text.is_empty()) {
                    (false, false) => format!("{chat} — {text}"),
                    (false, true) => chat,
                    (true, false) => text,
                    (true, true) => verb(one.kind).into(),
                }
            } else {
                let failed = items.iter().all(|i| i.kind == AttentionKind::Failed);
                let lead = if failed {
                    format!("{} chats stopped with a failure", items.len())
                } else {
                    format!("{} chats need you", items.len())
                };
                let names: Vec<String> = items
                    .iter()
                    .map(|i| clean(&i.chat_title, 40))
                    .filter(|n| !n.is_empty())
                    .collect();
                if names.is_empty() {
                    lead
                } else {
                    let shown = names
                        .iter()
                        .take(NAMED)
                        .cloned()
                        .collect::<Vec<_>>()
                        .join(", ");
                    let more = names.len().saturating_sub(NAMED);
                    if more > 0 {
                        format!("{lead}: {shown} and {more} more")
                    } else {
                        format!("{lead}: {shown}")
                    }
                }
            };
            let targets = items
                .iter()
                .filter_map(|i| Some((i.id.clone(), i.target()?)))
                .collect();
            Note {
                group,
                title,
                body,
                targets,
            }
        })
        .collect()
}

fn verb(kind: AttentionKind) -> &'static str {
    match kind {
        AttentionKind::Failed => "A chat stopped with a failure",
        _ => "A chat needs you",
    }
}

/// Records what is pending and returns only the items never announced before.
fn fresh<'a>(seen: &mut Seen, items: &'a [AttentionItem]) -> Vec<&'a AttentionItem> {
    let pending: HashSet<&str> = items
        .iter()
        .filter(|i| notifies(i))
        .map(|i| i.id.as_str())
        .collect();
    seen.keep_only(&pending);
    items
        .iter()
        .filter(|i| notifies(i))
        .filter(|i| seen.insert(&i.id))
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

/// The attention items still waiting, as the last list from the renderer had them.
pub fn pending<R: Runtime>(app: &AppHandle<R>) -> HashSet<String> {
    app.try_state::<Attention>()
        .and_then(|state| state.0.lock().ok().map(|seen| seen.ids.clone()))
        .unwrap_or_default()
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
) -> Result<Posted, String> {
    trusted(&webview)?;
    checked(&items)?;
    let state = app.state::<Attention>();
    let mut seen = state
        .0
        .lock()
        .map_err(|_| "Notifications are unavailable")?;
    // Remember what is pending even while focused, so switching away later does
    // not announce a question the person already saw in the window.
    let new = fresh(&mut seen, &items);
    // Released before posting: a click's handler reads what is pending.
    drop(seen);
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
    if permission(&app, false).state != Permission::Granted {
        attract(&app);
        return Ok(Posted {
            posted: 0,
            groups: notes.len(),
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
        groups: notes.len(),
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
) -> Result<BadgeAnswer, String> {
    trusted(&webview)?;
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
        let mut seen = Seen::default();
        let items = vec![item("a", AttentionKind::NeedsYou, MKT, "Launch")];
        assert_eq!(fresh(&mut seen, &items).len(), 1);
        assert_eq!(fresh(&mut seen, &items).len(), 0);
    }

    #[test]
    fn running_never_notifies_and_failed_does() {
        let mut seen = Seen::default();
        let items = vec![
            item("r", AttentionKind::Running, MKT, "Build"),
            item("f", AttentionKind::Failed, MKT, "Deploy"),
        ];
        let got = fresh(&mut seen, &items);
        assert_eq!(got.iter().map(|i| i.id.as_str()).collect::<Vec<_>>(), ["f"]);
    }

    #[test]
    fn an_answered_question_can_come_back_as_new() {
        let mut seen = Seen::default();
        let a = vec![item("a", AttentionKind::NeedsYou, None, "Launch")];
        assert_eq!(fresh(&mut seen, &a).len(), 1);
        assert_eq!(fresh(&mut seen, &[]).len(), 0);
        assert_eq!(fresh(&mut seen, &a).len(), 1);
    }

    #[test]
    fn memory_is_bounded() {
        let mut seen = Seen::default();
        for i in 0..REMEMBERED + 10 {
            seen.insert(&i.to_string());
        }
        assert_eq!(seen.ids.len(), REMEMBERED);
        assert!(seen.insert("0"), "the oldest was forgotten");
    }

    #[test]
    fn one_notification_per_place_named_after_it() {
        let a = item("a", AttentionKind::NeedsYou, MKT, "Launch plan");
        let b = item("b", AttentionKind::NeedsYou, MKT, "Pricing page");
        let c = item("c", AttentionKind::Failed, None, "Nightly");
        let notes = compose(&[&a, &b, &c]);
        assert_eq!(notes.len(), 2);
        let now = notes.iter().find(|n| n.group == "now").unwrap();
        assert_eq!(now.title, "codeaf");
        assert_eq!(now.body, "Nightly — Which branch should I use?");
        let mkt = notes
            .iter()
            .find(|n| n.group == "pl_00000000000000aa")
            .unwrap();
        assert_eq!(mkt.title, "Marketing");
        assert_eq!(mkt.body, "2 chats need you: Launch plan, Pricing page");
    }

    #[test]
    fn long_groups_name_three_and_count_the_rest() {
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
            "5 chats stopped with a failure: Chat 0, Chat 1, Chat 2 and 2 more"
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
    fn empty_words_fall_back_to_a_plain_sentence() {
        let mut bare = item("a", AttentionKind::Failed, None, "");
        bare.text = String::new();
        assert_eq!(compose(&[&bare])[0].body, "A chat stopped with a failure");
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
        assert_eq!(ids, ["s1:consent:7", "s2:choice:3"]);
        assert_eq!(
            mkt.targets[0].1,
            Target {
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
