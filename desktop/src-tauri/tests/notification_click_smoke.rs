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
        let mode = std::env::var("CODEAF_NOTIFY_SMOKE").unwrap_or_default();
        if mode == "real" {
            return with_app(real_server);
        }
        if mode != "1" {
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

        with_app(move |app, heard, run| scenario(app, &server, &shown, heard, run));
    }

    /// Builds the app with the real notification state, runs `body` beside its
    /// event loop, and exits with its verdict.
    fn with_app(
        body: impl FnOnce(&tauri::AppHandle, &Arc<AtomicUsize>, &mut Run) + Send + 'static,
    ) {
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
                    body(&handle, &heard, &mut run);
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

    /// A real server: post a grouped notification, then wait for its genuine click.
    fn real_server(app: &tauri::AppHandle, heard: &Arc<AtomicUsize>, run: &mut Run) {
        let main = wait(Duration::from_secs(10), || app.get_webview("main")).expect("main webview");
        let window = app.get_window("main").expect("main window");
        let _ = window.hide();
        let _ = wait(Duration::from_secs(5), || {
            (!window.is_focused().unwrap_or(true)).then_some(())
        });
        let place = Some("pl_00000000000000dd");
        let list = vec![
            item(
                "r1:consent:4",
                "needsYou",
                place,
                Some(("r1", "consent", 4)),
            ),
            item("r2:choice:5", "needsYou", place, Some(("r2", "choice", 5))),
        ];
        let posted = notifications::notify_attention(
            app.clone(),
            main.clone(),
            items(list.clone()),
            1,
            Some("smoke-real_server".into()),
        );
        run.check(
            "the real server took the notification",
            posted.as_ref().is_ok_and(|p| p.posted == 1),
            format!("{posted:?}"),
        );
        // The first question is answered before the click, as a newer reading says.
        let _ = notifications::notify_attention(
            app.clone(),
            main.clone(),
            items(vec![list[1].clone()]),
            2,
            Some("smoke-real_server".into()),
        );
        println!("READY: click the notification within 60s");
        let clicked = wait(Duration::from_secs(60), || {
            (heard.load(Ordering::SeqCst) > 0).then_some(())
        });
        let claimed = activation::notify_claim(app.clone(), main.clone())
            .map(|c| serde_json::to_value(c).unwrap());
        run.check(
            "a real click opened the question still waiting",
            clicked.is_some()
                && claimed.as_ref().is_ok_and(|c| {
                    *c == json!([{"chatId": "r2", "question": {"kind": "choice", "id": 5}}])
                }),
            format!("claim {claimed:?}"),
        );
        let shown = wait(Duration::from_secs(5), || {
            window.is_visible().ok().filter(|v| *v)
        });
        run.check("the window is brought forward", shown.is_some(), "visible");
        // Long enough for the driver to photograph the raised window before the app exits.
        println!("CLICKED");
        std::thread::sleep(Duration::from_secs(3));
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
        let posted = notifications::notify_attention(
            app.clone(),
            main.clone(),
            first,
            1,
            Some("smoke-scenario".into()),
        );
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
        let _ = notifications::notify_attention(
            app.clone(),
            main.clone(),
            items(next.clone()),
            2,
            Some("smoke-scenario".into()),
        );
        let posted = wait(Duration::from_secs(10), || {
            shown.lock().unwrap().get(before).cloned()
        });
        let _ = notifications::notify_attention(
            app.clone(),
            main.clone(),
            items(vec![next[1].clone()]),
            3,
            Some("smoke-scenario".into()),
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

        two_windows(app, server, shown, heard, run, &window);
    }

    /// Two real codeaf windows, each with its own reading of the world feed, post
    /// their lists in turn the way two workspaces do. One is a reading behind.
    fn two_windows(
        app: &tauri::AppHandle,
        server: &Connection,
        shown: &Arc<Mutex<Vec<Shown>>>,
        heard: &Arc<AtomicUsize>,
        run: &mut Run,
        main_window: &tauri::Window,
    ) {
        let main = app.get_webview("main").expect("main webview");
        let built = tauri::WebviewWindowBuilder::new(app, "w-2", Default::default())
            .visible(false)
            .build();
        let Some(second) = built
            .ok()
            .and_then(|_| wait(Duration::from_secs(10), || app.get_webview("w-2")))
        else {
            run.check("a second window opens", false, "no w-2");
            return;
        };
        let _ = main_window.hide();
        let _ = wait(Duration::from_secs(5), || {
            (!main_window.is_focused().unwrap_or(true)).then_some(())
        });
        let place = Some("pl_00000000000000cc");
        let q1 = item(
            "y1:consent:8",
            "needsYou",
            place,
            Some(("y1", "consent", 8)),
        );
        let q2 = item("y2:choice:9", "needsYou", place, Some(("y2", "choice", 9)));
        let failure = item("failed:y9:2026-10-09T11:00:00Z", "failed", None, None);
        let both = vec![q1.clone(), q2.clone()];
        let before = shown.lock().unwrap().len();
        let post = |webview: &tauri::Webview, list: &Vec<serde_json::Value>, seq: u64| {
            notifications::notify_attention(
                app.clone(),
                webview.clone(),
                items(list.clone()),
                seq,
                Some("smoke-two_windows".into()),
            )
            .map(|p| (p.posted, p.skipped))
        };

        let first = post(&main, &both, 10);
        let echo = post(&second, &both, 10);
        // y1 is answered; main reads that at 11 and w-2, still at 10, posts its old list.
        let answered = post(&main, &vec![q2.clone()], 11);
        let stale = post(&second, &both, 10);
        // A failure lands at 12; both windows read it; it is marked seen at 13 and main lags.
        let with_failure = vec![q2.clone(), failure.clone()];
        let failed_main = post(&main, &with_failure, 12);
        let failed_second = post(&second, &with_failure, 12);
        let marked = post(&second, &vec![q2.clone()], 13);
        let lagging = post(&main, &with_failure, 12);
        let caught_up = post(&main, &vec![q2.clone()], 13);
        run.check(
            "alternating windows: each question and failure is posted once",
            first.as_ref().is_ok_and(|p| p.0 == 1)
                && echo.as_ref().is_ok_and(|p| p.0 == 0)
                && answered.as_ref().is_ok_and(|p| p.0 == 0)
                && failed_main.as_ref().is_ok_and(|p| p.0 == 1)
                && failed_second.as_ref().is_ok_and(|p| p.0 == 0)
                && marked.as_ref().is_ok_and(|p| p.0 == 0)
                && caught_up.as_ref().is_ok_and(|p| p.0 == 0),
            format!("{first:?} {echo:?} {answered:?} {failed_main:?} {failed_second:?} {marked:?} {caught_up:?}"),
        );
        run.check(
            "a window a reading behind is ignored, not merged",
            stale
                .as_ref()
                .is_ok_and(|p| p.0 == 0 && p.1 == Some(notifications::Skipped::Stale))
                && lagging
                    .as_ref()
                    .is_ok_and(|p| p.0 == 0 && p.1 == Some(notifications::Skipped::Stale)),
            format!("{stale:?} {lagging:?}"),
        );
        let posted = wait(Duration::from_secs(10), || {
            let all = shown.lock().unwrap().clone();
            (all.len() >= before + 2).then_some(all[before..].to_vec())
        })
        .unwrap_or_default();
        std::thread::sleep(Duration::from_millis(500));
        let total = shown.lock().unwrap().len() - before;
        run.check(
            "the notification server was asked to show exactly two",
            total == 2,
            format!(
                "{:?}",
                posted
                    .iter()
                    .map(|s| (&s.summary, &s.body))
                    .collect::<Vec<_>>()
            ),
        );
        let Some(grouped) = posted.iter().find(|s| s.summary == "Marketing").cloned() else {
            return;
        };
        let clicks = heard.load(Ordering::SeqCst);
        click(server, grouped.id);
        let _ = wait(Duration::from_secs(10), || {
            (heard.load(Ordering::SeqCst) > clicks).then_some(())
        });
        let mut claimed = Vec::new();
        for webview in [&main, &second] {
            claimed.extend(
                activation::notify_claim(app.clone(), webview.clone())
                    .map(|c| serde_json::to_value(c).unwrap())
                    .ok()
                    .and_then(|v| v.as_array().cloned())
                    .unwrap_or_default(),
            );
        }
        run.check(
            "the stale window did not point the grouped click back at the answered question",
            claimed == [json!({"chatId": "y2", "question": {"kind": "choice", "id": 9}})],
            format!("claim {claimed:?}"),
        );
    }
}
