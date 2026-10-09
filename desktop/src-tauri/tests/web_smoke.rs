//! A native smoke test of web tabs in a real window: a real child webview loads
//! pages from a loopback server, and the page itself tries to reach the app.
//! It needs a display, so it runs only with CODEAF_WEB_SMOKE=1 (on Linux under
//! xvfb-run). It reports PASS/FAIL lines and exits non-zero on any failure.
//!
//! What it proves natively: a page in a web view cannot invoke a command,
//! the bridge refuses non-web addresses, a page cannot navigate or redirect
//! into an app origin, load/title/history events arrive, history steps work,
//! a snapshot is a real PNG, and close removes the view.

use codeaf_app_lib::web;
use std::io::{BufRead, BufReader, Write};
use std::net::TcpListener;
use std::sync::{Arc, Mutex};
use std::time::{Duration, Instant};
use tauri::{Listener, Manager};

type Seen = Arc<Mutex<Vec<String>>>;

fn page(title: &str, body: &str) -> String {
    format!("<!doctype html><html><head><title>{title}</title></head><body><h1>{title}</h1>{body}</body></html>")
}

/// A tiny loopback server: each page is fixed text; `/report?…` records what the page saw.
fn serve(seen: Seen) -> u16 {
    let listener = TcpListener::bind("127.0.0.1:0").unwrap();
    let port = listener.local_addr().unwrap().port();
    std::thread::spawn(move || {
        for stream in listener.incoming().flatten() {
            let seen = seen.clone();
            std::thread::spawn(move || {
                let mut reader = BufReader::new(&stream);
                let mut line = String::new();
                if reader.read_line(&mut line).is_err() {
                    return;
                }
                let path = line.split_whitespace().nth(1).unwrap_or("/").to_string();
                let probe = "<script>
                    const report = r => { const i = new Image(); i.src = '/report?' + encodeURIComponent(r); };
                    const t = window.__TAURI_INTERNALS__;
                    if (!t) report('invoke:absent');
                    else t.invoke('web_list').then(() => report('invoke:answered'), e => report('invoke:refused ' + String(e).slice(0, 80)));
                    if (t) t.invoke('engine_connection').then(() => report('engine:answered'), e => report('engine:refused'));
                </script>";
                let (status, extra, body) = match path.as_str() {
                    "/start" => ("200 OK", String::new(), page("Smoke start", probe)),
                    "/second" => ("200 OK", String::new(), page("Smoke second", "")),
                    "/redirect-app" => ("302 Found", "Location: tauri://localhost/\r\n".to_string(), String::new()),
                    "/frame-app" => ("200 OK", String::new(), page("Smoke frame", "<iframe src='tauri://localhost/'></iframe><iframe src='http://tauri.localhost/'></iframe>")),
                    "/link-app" =>("200 OK", String::new(), page("Smoke link", "<a id=a href='tauri://localhost/'>app</a><script>setTimeout(() => document.getElementById('a').click(), 200)</script>")),
                    p if p.starts_with("/report?") => {
                        seen.lock().unwrap().push(p.trim_start_matches("/report?").to_string());
                        ("204 No Content", String::new(), String::new())
                    }
                    _ => ("404 Not Found", String::new(), String::new()),
                };
                let mut out = &stream;
                let _ = write!(out, "HTTP/1.1 {status}\r\n{extra}Content-Type: text/html\r\nContent-Length: {}\r\nConnection: close\r\n\r\n{body}", body.len());
            });
        }
    });
    port
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

/// Under xvfb-run, CODEAF_WEB_SMOKE_SHOT=<dir> keeps a picture of the real
/// window with the child view in it, for review outside the source tree.
fn capture_screen(name: &str) {
    #[cfg(target_os = "linux")]
    if let Some(dir) = std::env::var_os("CODEAF_WEB_SMOKE_SHOT") {
        std::thread::sleep(Duration::from_millis(500));
        let out = std::path::Path::new(&dir).join(format!("{name}.xwd"));
        let _ = std::process::Command::new("xwd")
            .args(["-root", "-silent", "-out"])
            .arg(out)
            .status();
    }
    #[cfg(not(target_os = "linux"))]
    let _ = name;
}

fn wait<T>(limit: Duration, mut probe: impl FnMut() -> Option<T>) -> Option<T> {
    let start = Instant::now();
    while start.elapsed() < limit {
        if let Some(value) = probe() {
            return Some(value);
        }
        std::thread::sleep(Duration::from_millis(100));
    }
    None
}

fn main() {
    if std::env::var("CODEAF_WEB_SMOKE").as_deref() != Ok("1") {
        println!("web_smoke skipped: set CODEAF_WEB_SMOKE=1 with a display");
        return;
    }
    let seen: Seen = Arc::default();
    let port = serve(seen.clone());
    let base = format!("http://127.0.0.1:{port}");
    let states: Arc<Mutex<Vec<serde_json::Value>>> = Arc::default();
    let code = Arc::new(Mutex::new(0));
    let exit_code = code.clone();
    let app = tauri::Builder::default()
        .invoke_handler(tauri::generate_handler![
            web::web_open,
            web::web_list,
            web::web_close
        ])
        .setup(move |app| {
            let handle = app.handle().clone();
            let log = states.clone();
            handle.listen_any("web://state", move |event| {
                if let Ok(value) = serde_json::from_str(event.payload()) {
                    log.lock().unwrap().push(value);
                }
            });
            std::thread::spawn(move || {
                let mut run = Run { failed: false };
                scenario(&handle, &base, &seen, &states, &mut run);
                *code.lock().unwrap() = if run.failed { 1 } else { 0 };
                println!("web_smoke {}", if run.failed { "FAILED" } else { "PASSED" });
                handle.exit(if run.failed { 1 } else { 0 });
            });
            Ok(())
        })
        // `test = true` skips a second embedded Info.plist; the window, config and capabilities are the app's own.
        .build(tauri::generate_context!(test = true))
        .expect("smoke app");
    app.run(|_, _| {});
    std::process::exit(*exit_code.lock().unwrap());
}

fn scenario(
    app: &tauri::AppHandle,
    base: &str,
    seen: &Seen,
    states: &Arc<Mutex<Vec<serde_json::Value>>>,
    run: &mut Run,
) {
    use tauri::async_runtime::block_on;
    let main = wait(Duration::from_secs(10), || app.get_webview("main")).expect("main webview");
    let rect = web::Rect {
        x: 0.0,
        y: 0.0,
        width: 800.0,
        height: 560.0,
    };
    let latest = |field: &str| {
        states
            .lock()
            .unwrap()
            .iter()
            .rev()
            .find_map(|s| s.get(field).cloned())
    };
    let has_state =
        |pred: &dyn Fn(&serde_json::Value) -> bool| states.lock().unwrap().iter().any(pred);

    let opened = block_on(web::web_open(
        main.clone(),
        "smoke".into(),
        format!("{base}/start"),
        rect,
        true,
    ));
    run.check(
        "open a page in a child view",
        opened.is_ok(),
        format!("{:?}", opened.as_ref().map(|s| &s.url)),
    );
    let titled = wait(Duration::from_secs(20), || {
        has_state(&|s| s["title"] == "Smoke start" && s["loading"] == false).then_some(())
    });
    run.check(
        "title and finished load arrive as events",
        titled.is_some(),
        format!("last title {:?}", latest("title")),
    );
    capture_screen("linux-child-view");

    let reports = wait(Duration::from_secs(10), || {
        let got = seen.lock().unwrap().clone();
        (got.iter().any(|r| r.starts_with("invoke")) && got.iter().any(|r| r.starts_with("engine")))
            .then_some(got)
    })
    .unwrap_or_else(|| seen.lock().unwrap().clone());
    let decoded: Vec<String> = reports
        .iter()
        .map(|r| r.replace("%3A", ":").replace("%20", " "))
        .collect();
    let invoke_ok = decoded
        .iter()
        .any(|r| r.starts_with("invoke:refused") || r == "invoke:absent")
        && !decoded.iter().any(|r| r == "invoke:answered");
    run.check(
        "the page cannot invoke web_list",
        invoke_ok,
        format!("{decoded:?}"),
    );
    let engine_ok = decoded.iter().any(|r| r.starts_with("engine:refused"))
        && !decoded.iter().any(|r| r == "engine:answered")
        || decoded.iter().any(|r| r == "invoke:absent");
    run.check(
        "the page cannot reach engine_connection",
        engine_ok,
        format!("{decoded:?}"),
    );

    for bad in [
        "tauri://localhost/",
        "file:///etc/hosts",
        "javascript:alert(1)",
        "data:text/html,x",
        "http://tauri.localhost/",
    ] {
        let refused = block_on(web::web_navigate(main.clone(), "smoke".into(), bad.into()));
        run.check(
            "the bridge refuses a non-web address",
            refused.is_err(),
            format!("{bad} -> {refused:?}"),
        );
    }

    let _ = block_on(web::web_navigate(
        main.clone(),
        "smoke".into(),
        format!("{base}/link-app"),
    ));
    let blocked = wait(Duration::from_secs(10), || {
        has_state(&|s| s["notice"] == "blocked").then_some(())
    });
    let view = app.get_webview("web-smoke").expect("view");
    let url = view.url().map(|u| u.to_string()).unwrap_or_default();
    run.check(
        "a page's link into the app is blocked",
        blocked.is_some() && url.starts_with("http://127.0.0.1"),
        format!("url {url}"),
    );

    states.lock().unwrap().clear();
    let _ = block_on(web::web_navigate(
        main.clone(),
        "smoke".into(),
        format!("{base}/frame-app"),
    ));
    let framed = wait(Duration::from_secs(10), || {
        has_state(&|s| s["notice"] == "blocked").then_some(())
    });
    run.check(
        "a frame of the app inside a page is refused",
        framed.is_some(),
        "notice blocked",
    );

    let _ = block_on(web::web_navigate(
        main.clone(),
        "smoke".into(),
        format!("{base}/redirect-app"),
    ));
    std::thread::sleep(Duration::from_secs(3));
    let url = view.url().map(|u| u.to_string()).unwrap_or_default();
    run.check(
        "a redirect into the app does not land there",
        !url.starts_with("tauri:") && !url.contains("tauri.localhost"),
        format!("url {url}"),
    );

    let _ = block_on(web::web_navigate(
        main.clone(),
        "smoke".into(),
        format!("{base}/second"),
    ));
    let second = wait(Duration::from_secs(15), || {
        has_state(&|s| {
            s["title"] == "Smoke second" && s["loading"] == false && s["canBack"] == true
        })
        .then_some(())
    });
    run.check(
        "history: a second page can go back",
        second.is_some(),
        format!("canBack {:?}", latest("canBack")),
    );
    states.lock().unwrap().clear();
    let back = block_on(web::web_history(
        main.clone(),
        "smoke".into(),
        web::HistoryStep::Back,
    ));
    let returned = wait(Duration::from_secs(15), || {
        has_state(&|s| {
            s["url"].as_str().is_some_and(|u| !u.ends_with("/second")) && s["canForward"] == true
        })
        .then_some(())
    });
    run.check(
        "history: Back returns and Forward becomes possible",
        back.is_ok() && returned.is_some(),
        format!("url {:?}", latest("url")),
    );

    let shot = block_on(web::web_snapshot(main.clone(), "smoke".into()));
    let png = shot
        .as_ref()
        .map(|s| s.image.starts_with("data:image/png;base64,iVBORw0KGgo") && s.image.len() > 200)
        .unwrap_or(false);
    run.check(
        "a snapshot is a real PNG from the view",
        png,
        format!(
            "{} base64 chars",
            shot.as_ref().map(|s| s.image.len()).unwrap_or(0)
        ),
    );

    let moved = block_on(web::web_bounds(
        main.clone(),
        "smoke".into(),
        web::Rect {
            x: 20.0,
            y: 40.0,
            width: 400.0,
            height: 300.0,
        },
    ));
    let hidden = block_on(web::web_visible(main.clone(), "smoke".into(), false));
    let shown = block_on(web::web_visible(main.clone(), "smoke".into(), true));
    run.check(
        "bounds and visibility apply",
        moved.is_ok() && hidden.is_ok() && shown.is_ok(),
        format!("{moved:?} {hidden:?} {shown:?}"),
    );
    std::thread::sleep(Duration::from_millis(300));
    capture_screen("linux-child-moved");
    // macOS places a child at its bounds; read back the WKWebView's frame.
    #[cfg(target_os = "macos")]
    {
        let (tx, rx) = std::sync::mpsc::channel();
        let _ = view.with_webview(move |p| {
            // SAFETY: wry hands out its live WKWebView for the closure.
            let page: &objc2_web_kit::WKWebView = unsafe { &*(p.inner() as *const _) };
            let f = page.frame();
            let parent =
                unsafe { page.superview() }.map(|s| (s.frame().size.height, s.isFlipped()));
            let _ = tx.send((f.origin.x, f.origin.y, f.size.width, f.size.height, parent));
        });
        let got = rx.recv_timeout(Duration::from_secs(5)).ok();
        let ok = got.is_some_and(|(x, y, w, h, parent)| {
            let top = match parent {
                Some((height, false)) => height - y - h,
                _ => y,
            };
            (x, top, w, h) == (20.0, 40.0, 400.0, 300.0)
        });
        run.check(
            "macOS: the view covers exactly the pane rectangle",
            ok,
            format!("{got:?}"),
        );
    }
    // Linux places views itself (platform.rs); read back where GTK put this one.
    #[cfg(target_os = "linux")]
    {
        use gtk::prelude::WidgetExt;
        let (tx, rx) = std::sync::mpsc::channel();
        let _ = view.with_webview(move |p| {
            let page = p.inner();
            let a = page.allocation();
            let top = page.toplevel().expect("window");
            let (x, y) = page.translate_coordinates(&top, 0, 0).unwrap_or((-1, -1));
            let _ = tx.send((x, y, a.width(), a.height()));
        });
        let got = rx.recv_timeout(Duration::from_secs(5)).ok();
        run.check(
            "Linux: the view covers exactly the pane rectangle",
            got == Some((20, 40, 400, 300)),
            format!("{got:?}"),
        );
    }

    let closed = block_on(web::web_close(main.clone(), "smoke".into()));
    let gone = wait(Duration::from_secs(5), || {
        app.get_webview("web-smoke").is_none().then_some(())
    });
    run.check(
        "close removes the view",
        closed.is_ok() && gone.is_some(),
        format!("{closed:?}"),
    );
}
