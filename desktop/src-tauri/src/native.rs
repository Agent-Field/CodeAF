//! Narrow typed bridge to the operating system's "open" verbs.
//! No shell is involved: every target is passed as one argv entry.
//!
//! open_path and reveal_path do not take a workspace from the window. A path
//! the page can spell is not a directory it may open. Rust reads the cached
//! engine connection (the token never crosses back to the window) and asks
//! GET /api/engine/roots, then opens only a file that sits inside one of them.

use std::ffi::OsStr;
use std::io::{ErrorKind, Read, Write};
use std::net::{IpAddr, Ipv4Addr, Ipv6Addr, SocketAddr, TcpStream};
use std::path::{Path, PathBuf};
use std::process::Command;
use std::time::Duration;

use tauri::Manager;

const OUTSIDE: &str = "That file is outside the workspace";
const MISSING: &str = "That file no longer exists";
const UNAVAILABLE: &str = "The workspace is unavailable";
const ROOTS_PATH: &str = "/api/engine/roots";
const ROOTS_WAIT: Duration = Duration::from_secs(2);
/// A directory list is names, not a file. Past this the answer is not a roots list.
const ROOTS_CAP: usize = 64 * 1024;

// Canonicalise the file and every root so `..` and a symlink cannot leave the
// directories the engine named. An empty list is a remote engine: nothing on
// this disk is inside it, and the path is not statted first, because "missing"
// would tell the window whether the file is here.
fn confine(path: &str, roots: &[String]) -> Result<PathBuf, String> {
    if roots.is_empty() {
        return Err(OUTSIDE.into());
    }
    let target = Path::new(path);
    if !target.is_absolute() {
        return Err(OUTSIDE.into());
    }
    let resolved = target.canonicalize().map_err(|_| MISSING.to_string())?;
    let mut resolved_a_root = false;
    for root in roots {
        let root_path = Path::new(root.as_str());
        // A relative root would resolve against this process's directory and
        // widen the set. The engine only names absolute directories.
        if !root_path.is_absolute() {
            continue;
        }
        let Ok(canonical) = root_path.canonicalize() else {
            continue;
        };
        resolved_a_root = true;
        // Component-wise, so a sibling named like a prefix (`ws` / `ws-other`) is outside.
        if resolved.starts_with(&canonical) {
            return Ok(resolved);
        }
    }
    if !resolved_a_root {
        Err(UNAVAILABLE.into())
    } else {
        Err(OUTSIDE.into())
    }
}

// Only http(s) with a non-empty host may be handed to the OS.
fn checked_url(url: &str) -> Result<&str, String> {
    let rest = url
        .strip_prefix("https://")
        .or_else(|| url.strip_prefix("http://"))
        .ok_or("Only web links can be opened")?;
    let authority = rest.split(['/', '?', '#']).next().unwrap_or("");
    let host = authority.rsplit('@').next().unwrap_or("");
    let host = host.split(':').next().unwrap_or("");
    let clean = url.chars().all(|c| !c.is_control() && !c.is_whitespace());
    if host.is_empty() || !clean {
        return Err("That link is not valid".into());
    }
    Ok(url)
}

fn spawn(program: &str, args: &[&OsStr]) -> Result<(), String> {
    Command::new(program)
        .args(args)
        .spawn()
        .map(|_| ())
        .map_err(|_| "The system could not open that".to_string())
}

#[cfg(target_os = "macos")]
const OPENER: &str = "open";
#[cfg(not(target_os = "macos"))]
const OPENER: &str = "xdg-open";

#[cfg(target_os = "macos")]
fn reveal(resolved: &Path) -> Result<(), String> {
    spawn(OPENER, &["-R".as_ref(), resolved.as_os_str()])
}

#[cfg(not(target_os = "macos"))]
fn reveal(resolved: &Path) -> Result<(), String> {
    let folder = resolved.parent().unwrap_or(resolved);
    spawn(OPENER, &[folder.as_os_str()])
}

struct RootsTarget {
    addr: SocketAddr,
    host_header: String,
}

// The cached connection is an origin, not a URL the window composed. Only a
// loopback HTTP origin is dialed, and localhost is dialed as 127.0.0.1 so a
// hosts-file name cannot point the request somewhere else.
fn roots_target(base: &str) -> Result<RootsTarget, String> {
    if base.len() > 256 || base.chars().any(|c| c.is_control() || c.is_whitespace()) {
        return Err(UNAVAILABLE.into());
    }
    let Some(rest) = base.strip_prefix("http://") else {
        return Err(UNAVAILABLE.into());
    };
    if rest.contains('@') || rest.contains('?') || rest.contains('#') {
        return Err(UNAVAILABLE.into());
    }
    let (authority, path) = match rest.split_once('/') {
        Some((authority, path)) => (authority, path),
        None => (rest, ""),
    };
    if !path.is_empty() {
        return Err(UNAVAILABLE.into());
    }
    let (host, port) = split_host_port(authority)?;
    let ip = match host {
        "127.0.0.1" | "localhost" => IpAddr::V4(Ipv4Addr::LOCALHOST),
        "::1" => IpAddr::V6(Ipv6Addr::LOCALHOST),
        _ => return Err(UNAVAILABLE.into()),
    };
    let port: u16 = port.parse().map_err(|_| UNAVAILABLE.to_string())?;
    if port == 0 {
        return Err(UNAVAILABLE.into());
    }
    let host_header = if host == "::1" {
        format!("[::1]:{port}")
    } else {
        format!("127.0.0.1:{port}")
    };
    Ok(RootsTarget {
        addr: SocketAddr::new(ip, port),
        host_header,
    })
}

fn split_host_port(authority: &str) -> Result<(&str, &str), String> {
    if let Some(rest) = authority.strip_prefix('[') {
        let Some((host, port)) = rest.split_once("]:") else {
            return Err(UNAVAILABLE.into());
        };
        if host != "::1" || port.is_empty() || !port.bytes().all(|byte| byte.is_ascii_digit()) {
            return Err(UNAVAILABLE.into());
        }
        return Ok((host, port));
    }
    let Some((host, port)) = authority.rsplit_once(':') else {
        return Err(UNAVAILABLE.into());
    };
    if host.is_empty() || port.is_empty() || !port.bytes().all(|byte| byte.is_ascii_digit()) {
        return Err(UNAVAILABLE.into());
    }
    Ok((host, port))
}

// A bearer token is one header value. A newline in it would add a header.
fn token_is_safe(token: &str) -> bool {
    !token.is_empty() && token.bytes().all(|byte| (0x21..0x7f).contains(&byte))
}

fn fetch_roots(base_url: &str, token: &str) -> Result<Vec<String>, String> {
    if !token_is_safe(token) {
        return Err(UNAVAILABLE.into());
    }
    let target = roots_target(base_url)?;
    let mut stream = TcpStream::connect_timeout(&target.addr, ROOTS_WAIT)
        .map_err(|_| UNAVAILABLE.to_string())?;
    stream
        .set_read_timeout(Some(ROOTS_WAIT))
        .map_err(|_| UNAVAILABLE.to_string())?;
    stream
        .set_write_timeout(Some(ROOTS_WAIT))
        .map_err(|_| UNAVAILABLE.to_string())?;
    // Connection: close, and no redirect is ever issued. The token stays in this process.
    let request = format!(
        "GET {ROOTS_PATH} HTTP/1.1\r\nHost: {host}\r\nAuthorization: Bearer {token}\r\nAccept: application/json\r\nConnection: close\r\n\r\n",
        host = target.host_header,
    );
    stream
        .write_all(request.as_bytes())
        .map_err(|_| UNAVAILABLE.to_string())?;
    let raw = read_response(&mut stream)?;
    roots_from_http(&raw)
}

fn read_response(stream: &mut TcpStream) -> Result<Vec<u8>, String> {
    let mut buf = Vec::new();
    let mut tmp = [0u8; 4096];
    loop {
        if buf.len() > ROOTS_CAP {
            return Err(UNAVAILABLE.into());
        }
        match stream.read(&mut tmp) {
            Ok(0) => break,
            Ok(n) => {
                let room = ROOTS_CAP.saturating_sub(buf.len());
                if n > room {
                    return Err(UNAVAILABLE.into());
                }
                buf.extend_from_slice(&tmp[..n]);
                if response_complete(&buf) {
                    break;
                }
            }
            Err(err) if err.kind() == ErrorKind::Interrupted => continue,
            Err(_) => {
                if response_complete(&buf) {
                    break;
                }
                return Err(UNAVAILABLE.into());
            }
        }
    }
    if buf.is_empty() {
        return Err(UNAVAILABLE.into());
    }
    Ok(buf)
}

fn response_complete(buf: &[u8]) -> bool {
    let Some(header_end) = find_bytes(buf, b"\r\n\r\n") else {
        return false;
    };
    let head = &buf[..header_end];
    let body = &buf[header_end + 4..];
    if transfer_is_chunked(head) {
        return decode_chunked(body).is_ok();
    }
    if let Some(len) = content_length(head) {
        return len <= ROOTS_CAP && body.len() >= len;
    }
    false
}

fn roots_from_http(raw: &[u8]) -> Result<Vec<String>, String> {
    let Some(header_end) = find_bytes(raw, b"\r\n\r\n") else {
        return Err(UNAVAILABLE.into());
    };
    let head = std::str::from_utf8(&raw[..header_end]).map_err(|_| UNAVAILABLE.to_string())?;
    let mut lines = head.split("\r\n");
    let status = lines.next().unwrap_or("");
    let mut parts = status.split_whitespace();
    let version = parts.next().unwrap_or("");
    let code = parts.next().unwrap_or("");
    if (version != "HTTP/1.0" && version != "HTTP/1.1") || code != "200" {
        // A redirect is not followed. The body of any other status is not a roots list.
        return Err(UNAVAILABLE.into());
    }
    let body = &raw[header_end + 4..];
    let bytes = if transfer_is_chunked(raw[..header_end].as_ref()) {
        decode_chunked(body)?
    } else if let Some(len) = content_length(raw[..header_end].as_ref()) {
        if len > body.len() || len > ROOTS_CAP {
            return Err(UNAVAILABLE.into());
        }
        body[..len].to_vec()
    } else {
        body.to_vec()
    };
    if content_encoding_is_set(raw[..header_end].as_ref()) {
        return Err(UNAVAILABLE.into());
    }
    #[derive(serde::Deserialize)]
    struct RootsBody {
        #[serde(default)]
        roots: Option<Vec<String>>,
    }
    let parsed: RootsBody = serde_json::from_slice(&bytes).map_err(|_| UNAVAILABLE.to_string())?;
    Ok(parsed.roots.unwrap_or_default())
}

fn transfer_is_chunked(head: &[u8]) -> bool {
    header_value(head, "transfer-encoding")
        .map(|value| value.to_ascii_lowercase().contains("chunked"))
        .unwrap_or(false)
}

fn content_encoding_is_set(head: &[u8]) -> bool {
    header_value(head, "content-encoding")
        .map(|value| {
            let lower = value.to_ascii_lowercase();
            !lower.is_empty() && lower != "identity"
        })
        .unwrap_or(false)
}

fn content_length(head: &[u8]) -> Option<usize> {
    let value = header_value(head, "content-length")?;
    if !value.bytes().all(|byte| byte.is_ascii_digit()) {
        return None;
    }
    value.parse().ok()
}

fn header_value<'a>(head: &'a [u8], name: &str) -> Option<&'a str> {
    let head = std::str::from_utf8(head).ok()?;
    let mut found = None;
    for line in head.split("\r\n").skip(1) {
        let Some((key, value)) = line.split_once(':') else {
            continue;
        };
        if key.eq_ignore_ascii_case(name) {
            if found.is_some() {
                return None;
            }
            found = Some(value.trim());
        }
    }
    found
}

fn decode_chunked(input: &[u8]) -> Result<Vec<u8>, String> {
    let mut out = Vec::new();
    let mut rest = input;
    loop {
        let Some(line_end) = find_bytes(rest, b"\r\n") else {
            return Err(UNAVAILABLE.into());
        };
        let line = std::str::from_utf8(&rest[..line_end]).map_err(|_| UNAVAILABLE.to_string())?;
        let size_text = line.split(';').next().unwrap_or("").trim();
        if size_text.is_empty() || !size_text.bytes().all(|byte| byte.is_ascii_hexdigit()) {
            return Err(UNAVAILABLE.into());
        }
        let size = usize::from_str_radix(size_text, 16).map_err(|_| UNAVAILABLE.to_string())?;
        rest = &rest[line_end + 2..];
        if size == 0 {
            return Ok(out);
        }
        if size > ROOTS_CAP || out.len().saturating_add(size) > ROOTS_CAP || rest.len() < size + 2 {
            return Err(UNAVAILABLE.into());
        }
        out.extend_from_slice(&rest[..size]);
        if &rest[size..size + 2] != b"\r\n" {
            return Err(UNAVAILABLE.into());
        }
        rest = &rest[size + 2..];
    }
}

fn find_bytes(haystack: &[u8], needle: &[u8]) -> Option<usize> {
    haystack
        .windows(needle.len())
        .position(|window| window == needle)
}

// The cached connection only. This does not start an engine: opening a file
// must not be what brings the sidecar up, and a window-supplied token is not a parameter.
async fn engine_roots(app: &tauri::AppHandle) -> Result<Vec<String>, String> {
    let runtime = app.state::<crate::EngineRuntime>();
    let connection = {
        let guard = runtime.connection.lock().await;
        guard.clone().ok_or_else(|| UNAVAILABLE.to_string())?
    };
    let url = connection.url;
    let token = connection.token;
    tauri::async_runtime::spawn_blocking(move || fetch_roots(&url, &token))
        .await
        .map_err(|_| UNAVAILABLE.to_string())?
}

#[tauri::command]
pub async fn open_path(app: tauri::AppHandle, path: String) -> Result<(), String> {
    let resolved = confine(&path, &engine_roots(&app).await?)?;
    spawn(OPENER, &[resolved.as_os_str()])
}

#[tauri::command]
pub async fn reveal_path(app: tauri::AppHandle, path: String) -> Result<(), String> {
    reveal(&confine(&path, &engine_roots(&app).await?)?)
}

/// This machine's name, as the engine reports its own host. The renderer compares the two to decide whether
/// "Open in editor" can reach the engine's files (only when the engine runs on this very machine).
#[tauri::command]
pub fn host_name() -> Result<String, String> {
    let output = std::process::Command::new("hostname")
        .output()
        .map_err(|error| error.to_string())?;
    let name = String::from_utf8_lossy(&output.stdout).trim().to_string();
    if name.is_empty() {
        Err("this machine has no name".into())
    } else {
        Ok(name)
    }
}

#[tauri::command]
pub fn open_url(url: String) -> Result<(), String> {
    let url = checked_url(&url)?;
    spawn(OPENER, &[url.as_ref()])
}

#[cfg(test)]
mod tests {
    use super::*;
    use std::fs;
    use std::io::{Read, Write};
    use std::net::{IpAddr, Ipv4Addr, TcpListener, TcpStream};
    use std::sync::mpsc;
    use std::thread;
    use std::time::Duration;

    fn scratch(name: &str) -> PathBuf {
        let dir = std::env::temp_dir().join(format!("codeaf-native-{name}-{}", std::process::id()));
        let _ = fs::remove_dir_all(&dir);
        fs::create_dir_all(&dir).unwrap();
        dir
    }

    fn root_of(path: &Path) -> String {
        path.to_str().unwrap().to_string()
    }

    #[test]
    fn accepts_a_file_inside_any_root() {
        let dir = scratch("inside");
        let a = dir.join("a");
        let b = dir.join("b");
        fs::create_dir_all(&a).unwrap();
        fs::create_dir_all(&b).unwrap();
        let file_a = a.join("a.txt");
        let file_b = b.join("b.txt");
        fs::write(&file_a, "x").unwrap();
        fs::write(&file_b, "y").unwrap();
        let roots = vec![root_of(&a), root_of(&b)];
        assert!(confine(file_a.to_str().unwrap(), &roots)
            .unwrap()
            .ends_with("a.txt"));
        assert!(confine(file_b.to_str().unwrap(), &roots)
            .unwrap()
            .ends_with("b.txt"));
        let outside = dir.join("no.txt");
        fs::write(&outside, "z").unwrap();
        assert_eq!(
            confine(outside.to_str().unwrap(), &roots).unwrap_err(),
            OUTSIDE
        );
        let sibling = dir.join("a-other");
        fs::create_dir_all(&sibling).unwrap();
        let sibling_file = sibling.join("x.txt");
        fs::write(&sibling_file, "z").unwrap();
        assert_eq!(
            confine(sibling_file.to_str().unwrap(), &vec![root_of(&a)]).unwrap_err(),
            OUTSIDE
        );
    }

    #[test]
    fn refuses_missing_relative_and_dotdot() {
        let dir = scratch("bad");
        let ws = dir.join("ws");
        fs::create_dir_all(&ws).unwrap();
        fs::write(dir.join("outside.txt"), "x").unwrap();
        let roots = vec![root_of(&ws)];
        assert!(confine(ws.join("nope").to_str().unwrap(), &roots).is_err());
        assert_eq!(confine("a.txt", &roots).unwrap_err(), OUTSIDE);
        let sneaky = ws.join("..").join("outside.txt");
        assert_eq!(
            confine(sneaky.to_str().unwrap(), &roots).unwrap_err(),
            OUTSIDE
        );
    }

    #[test]
    fn empty_roots_refuse_every_path() {
        let dir = scratch("empty");
        let file = dir.join("a.txt");
        fs::write(&file, "x").unwrap();
        assert_eq!(confine(file.to_str().unwrap(), &[]).unwrap_err(), OUTSIDE);
        assert_eq!(
            confine(dir.join("missing").to_str().unwrap(), &[]).unwrap_err(),
            OUTSIDE
        );
        assert_eq!(confine("a.txt", &[]).unwrap_err(), OUTSIDE);
    }

    #[cfg(unix)]
    #[test]
    fn refuses_symlink_escape() {
        let dir = scratch("link");
        let a = dir.join("a");
        let b = dir.join("b");
        fs::create_dir_all(&a).unwrap();
        fs::create_dir_all(&b).unwrap();
        let secret = dir.join("secret.txt");
        fs::write(&secret, "x").unwrap();
        let link = a.join("link.txt");
        std::os::unix::fs::symlink(&secret, &link).unwrap();
        let roots = vec![root_of(&a), root_of(&b)];
        assert_eq!(
            confine(link.to_str().unwrap(), &roots).unwrap_err(),
            OUTSIDE
        );
        let kept = b.join("kept.txt");
        fs::write(&kept, "y").unwrap();
        let inside = a.join("inside.txt");
        std::os::unix::fs::symlink(&kept, &inside).unwrap();
        assert!(confine(inside.to_str().unwrap(), &roots)
            .unwrap()
            .ends_with("kept.txt"));
    }

    #[test]
    fn urls_need_http_scheme_and_host() {
        assert!(checked_url("https://example.com/a?b=1").is_ok());
        assert!(checked_url("http://localhost:3000").is_ok());
        for bad in [
            "file:///etc/passwd",
            "javascript:alert(1)",
            "https://",
            "http:///x",
            "ftp://a.com",
            "https://a.com/ b",
            "example.com",
        ] {
            assert!(checked_url(bad).is_err(), "{bad}");
        }
    }

    #[test]
    fn roots_url_is_loopback_only() {
        assert!(roots_target("http://127.0.0.1:9").is_ok());
        assert_eq!(
            roots_target("http://localhost:9").unwrap().addr.ip(),
            IpAddr::V4(Ipv4Addr::LOCALHOST)
        );
        assert!(roots_target("http://[::1]:9").is_ok());
        for bad in [
            "https://127.0.0.1:9",
            "http://example.com:9",
            "http://127.0.0.1.evil:9",
            "http://user@127.0.0.1:9",
            "http://127.0.0.1:9/api/engine/roots",
            "http://127.0.0.1",
            "http://127.0.0.1:0",
            "http://0.0.0.0:9",
            "http://127.0.0.1:9?x=1",
        ] {
            assert!(roots_target(bad).is_err(), "{bad}");
        }
        assert!(fetch_roots("http://127.0.0.1:9", "bad\ntoken").is_err());
        assert!(fetch_roots("http://example.com:9", "token").is_err());
    }

    fn read_request(sock: &mut TcpStream) -> String {
        sock.set_read_timeout(Some(Duration::from_secs(2))).unwrap();
        let mut buf = Vec::new();
        let mut tmp = [0u8; 1024];
        while !buf.windows(4).any(|window| window == b"\r\n\r\n") {
            let n = sock.read(&mut tmp).unwrap();
            assert!(n > 0, "the client closed before the request");
            buf.extend_from_slice(&tmp[..n]);
        }
        String::from_utf8(buf).unwrap()
    }

    #[test]
    fn fetches_the_roots_list_and_does_not_follow_a_redirect() {
        let listener = TcpListener::bind("127.0.0.1:0").unwrap();
        let port = listener.local_addr().unwrap().port();
        let (sent, request) = mpsc::channel();
        thread::spawn(move || {
            let (mut sock, _) = listener.accept().unwrap();
            let req = read_request(&mut sock);
            sent.send(req).unwrap();
            let body = r#"{"roots":["/tmp/a","/tmp/b"]}"#;
            let resp = format!(
                "HTTP/1.1 200 OK\r\nContent-Type: application/json\r\nContent-Length: {}\r\nConnection: close\r\n\r\n{body}",
                body.len()
            );
            sock.write_all(resp.as_bytes()).unwrap();
        });
        let roots = fetch_roots(&format!("http://127.0.0.1:{port}"), "test-token").unwrap();
        let req = request.recv().unwrap();
        assert!(
            req.starts_with("GET /api/engine/roots HTTP/1.1\r\n"),
            "{req}"
        );
        assert!(
            req.contains("Authorization: Bearer test-token\r\n"),
            "{req}"
        );
        assert!(!req.to_lowercase().contains("workspace"), "{req}");
        assert_eq!(roots, vec!["/tmp/a".to_string(), "/tmp/b".to_string()]);

        let redirect = TcpListener::bind("127.0.0.1:0").unwrap();
        let port = redirect.local_addr().unwrap().port();
        thread::spawn(move || {
            let (mut sock, _) = redirect.accept().unwrap();
            sock.set_read_timeout(Some(Duration::from_secs(2))).unwrap();
            let mut buf = [0u8; 1024];
            let _ = sock.read(&mut buf);
            let body = r#"{"roots":["/tmp/should-not-open"]}"#;
            let resp = format!(
                "HTTP/1.1 302 Found\r\nLocation: http://127.0.0.1:9/api/engine/roots\r\nContent-Length: {}\r\nConnection: close\r\n\r\n{body}",
                body.len()
            );
            sock.write_all(resp.as_bytes()).unwrap();
        });
        assert!(fetch_roots(&format!("http://127.0.0.1:{port}"), "test-token").is_err());
    }

    #[test]
    fn an_empty_roots_body_and_a_chunked_body_parse() {
        let empty = b"HTTP/1.1 200 OK\r\nContent-Length: 12\r\n\r\n{\"roots\":[]}";
        assert!(roots_from_http(empty).unwrap().is_empty());
        let missing = b"HTTP/1.1 200 OK\r\nContent-Length: 2\r\n\r\n{}";
        assert!(roots_from_http(missing).unwrap().is_empty());
        let nulls = b"HTTP/1.1 200 OK\r\nContent-Length: 14\r\n\r\n{\"roots\":null}";
        assert!(roots_from_http(nulls).unwrap().is_empty());
        let chunked = b"HTTP/1.1 200 OK\r\nTransfer-Encoding: chunked\r\n\r\n10\r\n{\"roots\":[\"/a\"]}\r\n0\r\n\r\n";
        assert_eq!(roots_from_http(chunked).unwrap(), vec!["/a".to_string()]);
        let redirected = b"HTTP/1.1 302 Found\r\nContent-Length: 12\r\n\r\n{\"roots\":[]}";
        assert!(roots_from_http(redirected).is_err());
    }
}
