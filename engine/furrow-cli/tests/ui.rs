use serde_json::Value;
use std::fs;
use std::io::{BufRead, BufReader, Read, Write};
use std::net::TcpStream;
use std::path::{Path, PathBuf};
use std::process::{Child, Stdio};
use tempfile::TempDir;

struct Fixture {
    _temp: TempDir,
    repo: PathBuf,
    data: PathBuf,
}

impl Fixture {
    fn new() -> Self {
        let temp = tempfile::tempdir().unwrap();
        let repo = temp.path().join("repo");
        let data = temp.path().join("data");
        fs::create_dir(&repo).unwrap();
        git(&repo, &["init", "-q", "-b", "main"]);
        git(&repo, &["config", "user.email", "ui@example.com"]);
        git(&repo, &["config", "user.name", "UI Test"]);
        fs::write(repo.join("app.txt"), b"base\n").unwrap();
        git(&repo, &["add", "app.txt"]);
        git(&repo, &["commit", "-q", "-m", "initial"]);
        Self {
            _temp: temp,
            repo,
            data,
        }
    }

    fn watch(&self) -> String {
        let output = std::process::Command::new(env!("CARGO_BIN_EXE_furrow"))
            .env("FURROW_DATA_DIR", &self.data)
            .env("FURROW_NO_DAEMON", "1")
            .arg("--repo")
            .arg(&self.repo)
            .args(["--json", "watch", "--no-daemon"])
            .output()
            .unwrap();
        assert!(output.status.success());
        serde_json::from_slice::<Value>(&output.stdout).unwrap()["snapshot"]
            .as_str()
            .unwrap()
            .to_owned()
    }

    fn ui(&self) -> RunningUi {
        self.ui_with_merge_check(None)
    }

    fn ui_with_merge_check(&self, merge_check: Option<&str>) -> RunningUi {
        let mut command = std::process::Command::new(env!("CARGO_BIN_EXE_furrow"));
        command
            .env("FURROW_DATA_DIR", &self.data)
            .env("FURROW_NO_DAEMON", "1")
            .arg("--repo")
            .arg(&self.repo)
            .args(["--json", "ui", "--no-open", "--port", "0"])
            .stdout(Stdio::piped())
            .stderr(Stdio::piped());
        if let Some(check) = merge_check {
            command.args(["--merge-check", check]);
        }
        let mut child = command.spawn().unwrap();
        let stdout = child.stdout.take().unwrap();
        let mut reader = BufReader::new(stdout);
        let mut line = String::new();
        assert!(reader.read_line(&mut line).unwrap() > 0);
        let info: Value = serde_json::from_str(&line).unwrap();
        let launch = info["launch_url"].as_str().unwrap();
        let origin = info["origin"].as_str().unwrap().to_owned();
        let token = launch.split("#token=").nth(1).unwrap().to_owned();
        let address = origin.strip_prefix("http://").unwrap().to_owned();
        RunningUi {
            child,
            _stdout: reader,
            address,
            origin,
            token,
        }
    }
}

struct RunningUi {
    child: Child,
    _stdout: BufReader<std::process::ChildStdout>,
    address: String,
    origin: String,
    token: String,
}

impl RunningUi {
    fn request(&self, method: &str, target: &str, body: Option<&Value>) -> HttpResponse {
        self.request_with(method, target, body, true, true, &self.address)
    }

    fn request_with(
        &self,
        method: &str,
        target: &str,
        body: Option<&Value>,
        authorize: bool,
        mutation_headers: bool,
        host: &str,
    ) -> HttpResponse {
        let body = body
            .map(serde_json::to_vec)
            .transpose()
            .unwrap()
            .unwrap_or_default();
        let mut request =
            format!("{method} {target} HTTP/1.1\r\nHost: {host}\r\nConnection: close\r\n");
        if authorize {
            request.push_str(&format!("Authorization: Bearer {}\r\n", self.token));
        }
        if !body.is_empty() || method == "POST" {
            request.push_str(&format!(
                "Content-Type: application/json\r\nContent-Length: {}\r\n",
                body.len()
            ));
        }
        if mutation_headers && method == "POST" {
            request.push_str(&format!(
                "Origin: {}\r\nX-Furrow-UI: 1\r\nSec-Fetch-Site: same-origin\r\n",
                self.origin
            ));
        }
        request.push_str("\r\n");
        let mut stream = TcpStream::connect(&self.address).unwrap();
        stream.write_all(request.as_bytes()).unwrap();
        stream.write_all(&body).unwrap();
        stream.shutdown(std::net::Shutdown::Write).unwrap();
        let mut response = Vec::new();
        stream.read_to_end(&mut response).unwrap();
        HttpResponse::parse(&response)
    }
}

impl Drop for RunningUi {
    fn drop(&mut self) {
        self.child.kill().ok();
        self.child.wait().ok();
    }
}

struct HttpResponse {
    status: u16,
    headers: String,
    body: Vec<u8>,
}

impl HttpResponse {
    fn parse(bytes: &[u8]) -> Self {
        let boundary = bytes
            .windows(4)
            .position(|window| window == b"\r\n\r\n")
            .unwrap();
        let headers = String::from_utf8(bytes[..boundary].to_vec()).unwrap();
        let status = headers
            .lines()
            .next()
            .unwrap()
            .split_whitespace()
            .nth(1)
            .unwrap()
            .parse()
            .unwrap();
        Self {
            status,
            headers,
            body: bytes[boundary + 4..].to_vec(),
        }
    }

    fn json(&self) -> Value {
        serde_json::from_slice(&self.body).unwrap()
    }
}

#[test]
fn mission_control_is_loopback_capability_guarded_and_offline() {
    let fixture = Fixture::new();
    fixture.watch();
    let ui = fixture.ui();
    assert!(ui.address.starts_with("127.0.0.1:"));
    assert_eq!(ui.token.len(), 64);

    let page = ui.request_with("GET", "/", None, false, false, &ui.address);
    assert_eq!(page.status, 200);
    assert!(page.headers.contains("Content-Security-Policy:"));
    assert!(page.headers.contains("frame-ancestors 'none'"));
    assert!(page.headers.contains("Referrer-Policy: no-referrer"));
    let html = String::from_utf8(page.body).unwrap();
    assert!(html.contains("Mission Control"));
    assert!(html.contains("/assets/icons.svg#gem"));
    assert!(html.contains("name=\"color-scheme\" content=\"dark\""));
    assert!(!html.contains("https://"));

    let icons = ui.request_with("GET", "/assets/icons.svg", None, false, false, &ui.address);
    assert_eq!(icons.status, 200);
    assert!(icons.headers.contains("Content-Type: image/svg+xml"));
    assert!(icons.headers.contains("max-age=31536000, immutable"));
    assert!(String::from_utf8_lossy(&icons.body).contains("id=\"undo-2\""));
    let font = ui.request_with(
        "GET",
        "/assets/geist.woff2",
        None,
        false,
        false,
        &ui.address,
    );
    assert_eq!(font.status, 200);
    assert!(font.headers.contains("Content-Type: font/woff2"));
    assert!(font.body.len() > 20_000);

    let script = ui.request_with("GET", "/assets/app.js", None, false, false, &ui.address);
    assert_eq!(script.status, 200);
    let script = String::from_utf8(script.body).unwrap();
    assert!(!script.contains("https://"));
    assert!(script.contains("http://www.w3.org/2000/svg"));
    assert!(script.contains("item.addEventListener(\"click\", () => inspectSnapshot(snapshot))"));
    assert!(script.contains("button(\"undo-2\", \"Preview restore to this point\""));
    assert!(!script
        .replace("http://www.w3.org/2000/svg", "")
        .contains("http://"));

    let unauthorized = ui.request_with("GET", "/api/v1/status", None, false, false, &ui.address);
    assert_ne!(unauthorized.status, 200);
    let rebound = ui.request_with("GET", "/", None, false, false, "evil.example");
    assert_ne!(rebound.status, 200);
    let preflight = ui.request_with("OPTIONS", "/api/v1/pins", None, false, false, &ui.address);
    assert_eq!(preflight.status, 405);

    let status = ui.request("GET", "/api/v1/status?fidelity=true", None);
    assert_eq!(status.status, 200);
    assert_eq!(status.json()["fidelity"]["grade"], "partial");
    let no_origin = ui.request_with(
        "POST",
        "/api/v1/pins",
        Some(&serde_json::json!({"snapshot": "0".repeat(64), "pinned": true})),
        true,
        false,
        &ui.address,
    );
    assert_ne!(no_origin.status, 200);
}

#[test]
fn mission_control_pins_and_refuses_a_stale_rewind_preview() {
    let fixture = Fixture::new();
    let snapshot = fixture.watch();
    fs::write(fixture.repo.join("app.txt"), b"first change\n").unwrap();
    let ui = fixture.ui();

    let pin = ui.request(
        "POST",
        "/api/v1/pins",
        Some(&serde_json::json!({"snapshot": snapshot, "pinned": true})),
    );
    assert_eq!(pin.status, 200);
    assert_eq!(pin.json()["pinned"], true);
    let timeline = ui.request("GET", "/api/v1/timeline?limit=10", None).json();
    let pinned = timeline
        .as_array()
        .unwrap()
        .iter()
        .find(|row| row["id"] == snapshot)
        .unwrap();
    assert_eq!(pinned["pinned"], true);

    let preview = ui.request(
        "POST",
        "/api/v1/rewind/plan",
        Some(&serde_json::json!({"snapshot": snapshot, "paths": []})),
    );
    assert_eq!(preview.status, 200);
    let preview = preview.json();
    assert_eq!(preview["changes"].as_array().unwrap().len(), 1);
    fs::write(fixture.repo.join("app.txt"), b"newer writer bytes\n").unwrap();

    let apply = ui.request(
        "POST",
        "/api/v1/rewind/apply",
        Some(&serde_json::json!({
            "snapshot": snapshot,
            "confirm_snapshot": snapshot,
            "preview_digest": preview["preview_digest"],
            "paths": [],
            "sqlite_consistent": false
        })),
    );
    assert_eq!(apply.status, 409);
    assert_eq!(apply.json()["error"]["code"], "stale_preview");
    assert_eq!(
        fs::read(fixture.repo.join("app.txt")).unwrap(),
        b"newer writer bytes\n"
    );
}

#[test]
fn mission_control_applies_a_real_verified_merge() {
    let fixture = Fixture::new();
    fixture.watch();
    let fork = fixture.repo.parent().unwrap().join("review");
    let fork_output = std::process::Command::new(env!("CARGO_BIN_EXE_furrow"))
        .env("FURROW_DATA_DIR", &fixture.data)
        .env("FURROW_NO_DAEMON", "1")
        .arg("--repo")
        .arg(&fixture.repo)
        .args(["fork", "review", "--destination"])
        .arg(&fork)
        .output()
        .unwrap();
    assert!(fork_output.status.success());
    fs::write(fork.join("app.txt"), b"verified merge bytes\n").unwrap();
    let snap = std::process::Command::new(env!("CARGO_BIN_EXE_furrow"))
        .env("FURROW_DATA_DIR", &fixture.data)
        .env("FURROW_NO_DAEMON", "1")
        .arg("--repo")
        .arg(&fork)
        .args(["snap", "-m", "review result"])
        .output()
        .unwrap();
    assert!(snap.status.success());

    let ui = fixture.ui_with_merge_check(Some("git diff --check"));
    let timeline_before = ui.request("GET", "/api/v1/timeline?limit=100", None).json();
    let preview = ui.request(
        "POST",
        "/api/v1/merge/preview",
        Some(&serde_json::json!({"fork": "review"})),
    );
    assert_eq!(preview.status, 200);
    let preview = preview.json();
    assert!(preview["conflicts"].as_array().unwrap().is_empty());
    assert_eq!(preview["changes"], 1);
    let timeline_after = ui.request("GET", "/api/v1/timeline?limit=100", None).json();
    assert_eq!(
        timeline_after, timeline_before,
        "merge preview must not publish restore points"
    );
    assert_eq!(fs::read(fixture.repo.join("app.txt")).unwrap(), b"base\n");

    let apply = ui.request(
        "POST",
        "/api/v1/merge/apply",
        Some(&serde_json::json!({
            "fork": "review",
            "preview_digest": preview["preview_digest"]
        })),
    );
    assert_eq!(
        apply.status,
        200,
        "{}",
        String::from_utf8_lossy(&apply.body)
    );
    assert!(apply.json()["result_snapshot"].is_string());
    assert_eq!(
        fs::read(fixture.repo.join("app.txt")).unwrap(),
        b"verified merge bytes\n"
    );
}

fn git(root: &Path, arguments: &[&str]) {
    let status = std::process::Command::new("git")
        .arg("-C")
        .arg(root)
        .args(arguments)
        .status()
        .unwrap();
    assert!(status.success());
}
