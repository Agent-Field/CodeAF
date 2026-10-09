//! A native smoke test of a system-notification click in a real app: codeaf
//! posts through the real session-bus path in activation.rs to a notification
//! server, the server reports a click the way a freedesktop server does
//! (`ActivationToken`, then `ActionInvoked` for the default action), and the
//! target arrives at the window. Linux only. It needs a display and a session
//! bus of its own, so it runs only with CODEAF_NOTIFY_SMOKE=1:
//!
//!     dbus-run-session -- xvfb-run -a env CODEAF_NOTIFY_SMOKE=1 \
//!         cargo test --test notification_click_smoke
//!
//! The server here is a stub that owns `org.freedesktop.Notifications` on that
//! private bus; it records what it was asked to show and clicks on request. It
//! is not a desktop shell: what it proves is codeaf's side of the protocol, a
//! real bus, a real event loop and a real window. It reports PASS/FAIL lines
//! and exits non-zero on any failure.

#[cfg(not(target_os = "linux"))]
fn main() {
    println!("notification_click_smoke skipped: the session-bus path is Linux only");
}

#[cfg(target_os = "linux")]
fn main() {
    linux::main();
}

#[cfg(target_os = "linux")]
mod linux {
    use std::collections::HashMap;
    use std::sync::atomic::{AtomicUsize, Ordering};
    use std::sync::{Arc, Mutex};
    use std::time::{Duration, Instant};

    use codeaf_app_lib::{activation, notifications};
    use serde_json::json;
    use tauri::{Listener, Manager};
    use zbus::blocking::Connection;
    use zbus::zvariant::OwnedValue;

    const SERVICE: &str = "org.freedesktop.Notifications";
    const PATH: &str = "/org/freedesktop/Notifications";

    #[derive(Clone, Debug)]
    struct Shown {
        id: u32,
        summary: String,
        body: String,
        actions: Vec<String>,
    }

    #[derive(Clone, Default)]
    struct Stub {
        shown: Arc<Mutex<Vec<Shown>>>,
    }

    #[zbus::interface(name = "org.freedesktop.Notifications")]
    impl Stub {
        #[allow(clippy::too_many_arguments)]
        fn notify(
            &self,
            _app_name: &str,
            _replaces_id: u32,
            _app_icon: &str,
            summary: &str,
            body: &str,
            actions: Vec<String>,
            _hints: HashMap<String, OwnedValue>,
            _expire_timeout: i32,
        ) -> u32 {
            let mut shown = self.shown.lock().unwrap();
            let id = shown.len() as u32 + 1;
            shown.push(Shown {
                id,
                summary: summary.into(),
                body: body.into(),
                actions,
            });
            id
        }

        fn get_capabilities(&self) -> Vec<String> {
            vec!["body".into(), "actions".into()]
        }

        fn close_notification(&self, _id: u32) {}

        fn get_server_information(&self) -> (String, String, String, String) {
            ("smoke".into(), "codeaf".into(), "0".into(), "1.2".into())
        }
    }

    struct Run {
        failed: bool,
    }

    impl Run {
        fn check(&mut self, name: &str, ok: bool, detail: impl std::fmt::Display) {
            println!("{} {name}: {detail}", if ok { "PASS" } else { "FAIL" });
            self.failed |= !ok;
        }
    }

    fn wait<T>(limit: Duration, mut probe: impl FnMut() -> Option<T>) -> Option<T> {
        let start = Instant::now();
        while start.elapsed() < limit {
            if let Some(value) = probe() {
                return Some(value);
            }
            std::thread::sleep(Duration::from_millis(50));
        }
        None
    }

    /// What a freedesktop server sends when the person clicks a notification's body.
    fn click(server: &Connection, id: u32) {
        let _ = server.emit_signal(
            None::<&str>,
            PATH,
            SERVICE,
            "ActivationToken",
            &(id, "smoke-activation-token"),
        );
        let _ = server.emit_signal(
            None::<&str>,
            PATH,
            SERVICE,
            "ActionInvoked",
            &(id, "default"),
        );
        let _ = server.emit_signal(
            None::<&str>,
            PATH,
            SERVICE,
            "NotificationClosed",
            &(id, 2u32),
        );
    }

    fn item(
        id: &str,
        kind: &str,
        place: Option<&str>,
        chat: Option<(&str, &str, u64)>,
    ) -> serde_json::Value {
        let mut item = json!({"id": id, "kind": kind, "chatTitle": "Launch plan", "text": "Which branch should I use?"});
        if let Some(place) = place {
            item["placeId"] = json!(place);
            item["placeName"] = json!("Marketing");
        }
        if let Some((chat, question, number)) = chat {
            item["chatId"] = json!(chat);
            item["question"] = json!({"kind": question, "id": number});
        }
        item
    }

    fn items(values: Vec<serde_json::Value>) -> Vec<notifications::AttentionItem> {
        values
            .into_iter()
            .map(|v| serde_json::from_value(v).expect("an attention item"))
            .collect()
    }

    pub fn main() {
        if std::env::var("CODEAF_NOTIFY_SMOKE").as_deref() != Ok("1") {
            println!("notification_click_smoke skipped: set CODEAF_NOTIFY_SMOKE=1 under dbus-run-session and a display");
            return;
        }
        let stub = Stub::default();
        let shown = Arc::clone(&stub.shown);
        let server = zbus::blocking::connection::Builder::session()
            .and_then(|b| b.name(SERVICE))
            .and_then(|b| b.serve_at(PATH, stub))
            .and_then(|b| b.build())
            .expect("a private session bus (run under dbus-run-session)");

        let heard = Arc::new(AtomicUsize::new(0));
        let code = Arc::new(Mutex::new(0));
        let exit_code = Arc::clone(&code);
        let ear = Arc::clone(&heard);
        let app = tauri::Builder::default()
            .manage(notifications::Attention::default())
            .manage(activation::Activation::default())
            .plugin(tauri_plugin_notification::init())
            .setup(move |app| {
                let handle = app.handle().clone();
                handle.listen_any(activation::ACTIVATED, move |_| {
                    ear.fetch_add(1, Ordering::SeqCst);
                });
                std::thread::spawn(move || {
                    let mut run = Run { failed: false };
                    scenario(&handle, &server, &shown, &heard, &mut run);
                    *code.lock().unwrap() = i32::from(run.failed);
                    println!(
                        "notification_click_smoke {}",
                        if run.failed { "FAILED" } else { "PASSED" }
                    );
                    handle.exit(i32::from(run.failed));
                });
                Ok(())
            })
            // `test = true` skips a second embedded Info.plist; the window and config are the app's own.
            .build(tauri::generate_context!(test = true))
            .expect("smoke app");
        app.run(|_, _| {});
        std::process::exit(*exit_code.lock().unwrap());
    }

    fn scenario(
        app: &tauri::AppHandle,
        server: &Connection,
        shown: &Arc<Mutex<Vec<Shown>>>,
        heard: &Arc<AtomicUsize>,
        run: &mut Run,
    ) {
        let main = wait(Duration::from_secs(10), || app.get_webview("main")).expect("main webview");
        let window = app.get_window("main").expect("main window");
        // Notifications are posted only while no codeaf window is focused.
        let _ = window.hide();
        let _ = wait(Duration::from_secs(5), || {
            (!window.is_focused().unwrap_or(true)).then_some(())
        });

        let first = items(vec![
            item(
                "s1:consent:7",
                "needsYou",
                Some("pl_00000000000000aa"),
                Some(("s1", "consent", 7)),
            ),
            item(
                "s2:choice:3",
                "needsYou",
                Some("pl_00000000000000aa"),
                Some(("s2", "choice", 3)),
            ),
            item("failed:s9", "failed", None, None),
        ]);
        let posted = notifications::notify_attention(app.clone(), main.clone(), first);
        run.check(
            "two places become two notifications",
            posted
                .as_ref()
                .is_ok_and(|p| p.posted == 2 && p.groups == 2 && p.skipped.is_none()),
            format!("{posted:?}"),
        );
        let got = wait(Duration::from_secs(10), || {
            let all = shown.lock().unwrap().clone();
            (all.len() == 2).then_some(all)
        })
        .unwrap_or_default();
        let grouped = got.iter().find(|s| s.summary == "Marketing").cloned();
        let bare = got.iter().find(|s| s.summary != "Marketing").cloned();
        run.check(
            "a notification with somewhere to go offers the default action",
            grouped
                .as_ref()
                .is_some_and(|s| s.actions == ["default", "Open"]),
            format!("{grouped:?}"),
        );
        run.check(
            "a notification that names no conversation offers none",
            bare.as_ref().is_some_and(|s| s.actions.is_empty()),
            format!("{bare:?}"),
        );
        run.check(
            "the notification shows words, never its target",
            got.iter()
                .all(|s| !s.body.contains("s1:") && !s.body.contains("consent")),
            format!("{:?}", got.iter().map(|s| &s.body).collect::<Vec<_>>()),
        );
        let Some(grouped) = grouped else { return };

        // Another connection on the same bus names our notification's id.
        let rogue = Connection::session().expect("a second connection");
        let _ = rogue.emit_signal(
            None::<&str>,
            PATH,
            SERVICE,
            "ActionInvoked",
            &(grouped.id, "default"),
        );
        std::thread::sleep(Duration::from_millis(800));
        let claimed = activation::notify_claim(app.clone(), main.clone());
        run.check(
            "a click signal from anyone but the server opens nothing",
            heard.load(Ordering::SeqCst) == 0 && claimed.as_ref().is_ok_and(|c| c.is_empty()),
            format!("events {} claim {claimed:?}", heard.load(Ordering::SeqCst)),
        );

        click(server, grouped.id);
        let arrived = wait(Duration::from_secs(10), || {
            (heard.load(Ordering::SeqCst) == 1).then_some(())
        });
        let claimed = activation::notify_claim(app.clone(), main.clone())
            .map(|c| serde_json::to_value(c).unwrap());
        run.check(
            "a genuine click reaches the window with that question",
            arrived.is_some()
                && claimed.as_ref().is_ok_and(|c| {
                    *c == json!([{"chatId": "s1", "question": {"kind": "consent", "id": 7}}])
                }),
            format!("claim {claimed:?}"),
        );
        let shown_again = wait(Duration::from_secs(5), || {
            window.is_visible().ok().filter(|v| *v)
        });
        run.check(
            "the window is brought forward",
            shown_again.is_some(),
            "visible",
        );
        run.check(
            "a claim is handed over once",
            activation::notify_claim(app.clone(), main.clone()).is_ok_and(|c| c.is_empty()),
            "empty",
        );
        click(server, grouped.id);
        std::thread::sleep(Duration::from_millis(800));
        run.check(
            "a second click on the same notification opens nothing more",
            heard.load(Ordering::SeqCst) == 1,
            format!("events {}", heard.load(Ordering::SeqCst)),
        );

        // A grouped notification whose first question is answered before the click.
        let _ = window.hide();
        let _ = wait(Duration::from_secs(5), || {
            (!window.is_focused().unwrap_or(true)).then_some(())
        });
        let before = shown.lock().unwrap().len();
        let next = vec![
            item(
                "x1:consent:4",
                "needsYou",
                Some("pl_00000000000000bb"),
                Some(("x1", "consent", 4)),
            ),
            item(
                "x2:choice:5",
                "needsYou",
                Some("pl_00000000000000bb"),
                Some(("x2", "choice", 5)),
            ),
        ];
        let _ = notifications::notify_attention(app.clone(), main.clone(), items(next.clone()));
        let posted = wait(Duration::from_secs(10), || {
            shown.lock().unwrap().get(before).cloned()
        });
        let _ = notifications::notify_attention(
            app.clone(),
            main.clone(),
            items(vec![next[1].clone()]),
        );
        if let Some(posted) = posted {
            click(server, posted.id);
        }
        let arrived = wait(Duration::from_secs(10), || {
            (heard.load(Ordering::SeqCst) == 2).then_some(())
        });
        let claimed = activation::notify_claim(app.clone(), main.clone())
            .map(|c| serde_json::to_value(c).unwrap());
        run.check(
            "a grouped click skips the question already answered",
            arrived.is_some()
                && claimed.as_ref().is_ok_and(|c| {
                    *c == json!([{"chatId": "x2", "question": {"kind": "choice", "id": 5}}])
                }),
            format!("claim {claimed:?}"),
        );
    }
}
