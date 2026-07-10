//! Embedded, loopback-only Mission Control server.

use crate::model::{id_hex, parse_id};
use crate::AgitRepository;
use anyhow::Context;
use serde::Deserialize;
use serde_json::{json, Value};
use std::collections::BTreeMap;
use std::io::{Read, Write};
use std::net::TcpListener;
use std::path::{Path, PathBuf};
use std::process::Command;
use std::sync::Arc;
use tiny_http::{Header, Method, Request, Response, Server, StatusCode};

const INDEX_HTML: &str = include_str!("../ui/index.html");
const APP_CSS: &str = include_str!("../ui/app.css");
const APP_JS: &str = include_str!("../ui/app.js");
const MAX_BODY: usize = 1024 * 1024;
const MAX_URL: usize = 8192;
const MAX_HEADERS: usize = 64;

#[derive(Clone)]
struct State {
    root: PathBuf,
    origin: String,
    host: String,
    token: String,
    merge_check: Option<String>,
}

#[derive(Debug, Clone, serde::Serialize)]
pub struct ServerInfo {
    pub origin: String,
    pub launch_url: String,
}

pub fn run(
    root: &Path,
    port: u16,
    no_open: bool,
    merge_check: Option<String>,
    json_output: bool,
) -> anyhow::Result<()> {
    let root = root.canonicalize()?;
    let listener = TcpListener::bind(("127.0.0.1", port))?;
    let address = listener.local_addr()?;
    let server = Server::from_listener(listener, None)
        .map_err(|error| anyhow::anyhow!("start Mission Control: {error}"))?;
    let origin = format!("http://{address}");
    let token = random_token()?;
    let info = ServerInfo {
        origin: origin.clone(),
        launch_url: format!("{origin}/#token={token}"),
    };
    if json_output {
        println!("{}", serde_json::to_string(&info)?);
    } else {
        println!("Mission Control {}", info.launch_url);
    }
    std::io::stdout().flush()?;
    if !no_open {
        if let Err(error) = open_browser(&info.launch_url) {
            eprintln!("warning: browser did not open: {error}");
            eprintln!("open {}", info.launch_url);
        }
    }

    let state = Arc::new(State {
        root,
        origin,
        host: address.to_string(),
        token,
        merge_check,
    });
    let server = Arc::new(server);
    let mut workers = Vec::with_capacity(4);
    for _ in 0..4 {
        let server = Arc::clone(&server);
        let state = Arc::clone(&state);
        workers.push(std::thread::spawn(move || {
            while let Ok(request) = server.recv() {
                handle(request, &state);
            }
        }));
    }
    for worker in workers {
        let _ = worker.join();
    }
    Ok(())
}

fn handle(mut request: Request, state: &State) {
    let response = dispatch(&mut request, state).unwrap_or_else(|error| {
        api_error(
            400,
            "invalid_request",
            &error.to_string(),
            "refresh and retry with the current workspace state",
        )
    });
    let _ = request.respond(response);
}

fn dispatch(request: &mut Request, state: &State) -> anyhow::Result<HttpResponse> {
    anyhow::ensure!(request.url().len() <= MAX_URL, "request URL is too long");
    anyhow::ensure!(
        request.headers().len() <= MAX_HEADERS,
        "too many request headers"
    );
    anyhow::ensure!(
        request
            .remote_addr()
            .is_some_and(|address| address.ip().is_loopback()),
        "Mission Control accepts loopback clients only"
    );
    require_single_header(request, "Host", &state.host)?;
    let (path, query) = split_target(request.url())?;

    if request.method() == &Method::Options {
        return Ok(api_error(
            405,
            "method_not_allowed",
            "CORS preflight is not supported",
            "use the same-origin Mission Control page",
        ));
    }
    if path == "/" && request.method() == &Method::Get {
        return Ok(static_response(
            200,
            "text/html; charset=utf-8",
            INDEX_HTML.as_bytes(),
            false,
        ));
    }
    if path == "/assets/app.css" && request.method() == &Method::Get {
        return Ok(static_response(
            200,
            "text/css; charset=utf-8",
            APP_CSS.as_bytes(),
            false,
        ));
    }
    if path == "/assets/app.js" && request.method() == &Method::Get {
        return Ok(static_response(
            200,
            "text/javascript; charset=utf-8",
            APP_JS.as_bytes(),
            false,
        ));
    }
    if !path.starts_with("/api/v1/") {
        return Ok(api_error(
            404,
            "not_found",
            "route not found",
            "return to Mission Control",
        ));
    }
    authorize(request, state, request.method() == &Method::Post)?;

    match (request.method(), path.as_str()) {
        (&Method::Get, "/api/v1/status") => {
            let repository = AgitRepository::open(&state.root)?;
            if query_bool(&query, "fidelity")? {
                json_response(json!({
                    "status": repository.status()?,
                    "fidelity": repository.fidelity()?
                }))
            } else {
                json_response(serde_json::to_value(repository.status()?)?)
            }
        }
        (&Method::Get, "/api/v1/timeline") => {
            let limit = query_usize(&query, "limit", 100, 1, 1000)?;
            json_response(serde_json::to_value(
                AgitRepository::open(&state.root)?.timeline(limit)?,
            )?)
        }
        (&Method::Get, "/api/v1/forks") => json_response(serde_json::to_value(
            AgitRepository::open(&state.root)?.forks()?,
        )?),
        (&Method::Get, "/api/v1/events") => {
            let limit = query_usize(&query, "limit", 100, 1, 1000)?;
            let after = query.get("after").map(String::as_str);
            json_response(serde_json::to_value(
                AgitRepository::open(&state.root)?.events(after, limit)?,
            )?)
        }
        (&Method::Get, "/api/v1/config") => json_response(json!({
            "merge_apply": state.merge_check.is_some(),
            "poll_interval_ms": 5000
        })),
        (&Method::Post, "/api/v1/diff") => {
            let body: DiffRequest = body_json(request)?;
            json_response(serde_json::to_value(
                AgitRepository::open(&state.root)?.diff(&body.target)?,
            )?)
        }
        (&Method::Post, "/api/v1/rewind/plan") => {
            let body: RewindPlanRequest = body_json(request)?;
            let repository = AgitRepository::open(&state.root)?;
            let target = exact_snapshot(&repository, &body.snapshot)?;
            json_response(serde_json::to_value(
                repository.plan_rewind(&target, &body.paths)?,
            )?)
        }
        (&Method::Post, "/api/v1/rewind/apply") => {
            let body: RewindApplyRequest = body_json(request)?;
            anyhow::ensure!(
                body.snapshot == body.confirm_snapshot,
                "confirm_snapshot must exactly match snapshot"
            );
            let mut repository = AgitRepository::open(&state.root)?;
            let target = exact_snapshot(&repository, &body.snapshot)?;
            let current = repository.plan_rewind(&target, &body.paths)?;
            if !constant_time_equal(&current.preview_digest, &body.preview_digest) {
                return Ok(api_error(
                    409,
                    "stale_preview",
                    "workspace state changed after the rewind preview",
                    "preview the rewind again before applying it",
                ));
            }
            let (pre, applied) = repository.rewind(
                &target,
                &body.paths,
                body.sqlite_consistent.unwrap_or(false),
            )?;
            json_response(json!({
                "restored": applied.target,
                "pre_rewind_snapshot": id_hex(&pre),
                "changes": applied.changes.len()
            }))
        }
        (&Method::Post, "/api/v1/merge/preview") => {
            let body: MergeRequest = body_json(request)?;
            json_response(serde_json::to_value(
                AgitRepository::open(&state.root)?.merge(&body.fork, None, true)?,
            )?)
        }
        (&Method::Post, "/api/v1/merge/apply") => {
            let body: MergeApplyRequest = body_json(request)?;
            let check = state.merge_check.as_deref().context(
                "merge apply is disabled; restart `agit ui` with --merge-check <command>",
            )?;
            let mut repository = AgitRepository::open(&state.root)?;
            let current = repository.merge(&body.fork, None, true)?;
            if !constant_time_equal(&current.preview_digest, &body.preview_digest) {
                return Ok(api_error(
                    409,
                    "stale_preview",
                    "source or fork state changed after the merge preview",
                    "preview the merge again before applying it",
                ));
            }
            json_response(serde_json::to_value(repository.merge(
                &body.fork,
                Some(check),
                false,
            )?)?)
        }
        (&Method::Post, "/api/v1/forks/discard") => {
            let body: ForkMutationRequest = body_json(request)?;
            anyhow::ensure!(
                body.fork_id == body.confirm_fork_id,
                "confirm_fork_id must exactly match fork_id"
            );
            let mut repository = AgitRepository::open(&state.root)?;
            let fork = repository
                .forks()?
                .into_iter()
                .find(|fork| fork.fork_id == body.fork_id)
                .context("fork ID was not found")?;
            let removal = repository.remove_fork(&fork.name, false)?;
            json_response(json!({"fork_id": body.fork_id, "result": removal}))
        }
        (&Method::Post, "/api/v1/pins") => {
            let body: PinRequest = body_json(request)?;
            let mut repository = AgitRepository::open(&state.root)?;
            let snapshot = exact_snapshot(&repository, &body.snapshot)?;
            let changed = if body.pinned {
                repository.pin(&snapshot)?
            } else {
                repository.unpin(&snapshot)?
            };
            json_response(json!({
                "snapshot": id_hex(&snapshot),
                "pinned": body.pinned,
                "changed": changed
            }))
        }
        _ => Ok(api_error(
            405,
            "method_not_allowed",
            "method is not allowed for this route",
            "use a documented Mission Control operation",
        )),
    }
}

#[derive(Deserialize)]
struct DiffRequest {
    target: String,
}

#[derive(Deserialize)]
struct RewindPlanRequest {
    snapshot: String,
    #[serde(default)]
    paths: Vec<PathBuf>,
}

#[derive(Deserialize)]
struct RewindApplyRequest {
    snapshot: String,
    confirm_snapshot: String,
    preview_digest: String,
    #[serde(default)]
    paths: Vec<PathBuf>,
    sqlite_consistent: Option<bool>,
}

#[derive(Deserialize)]
struct MergeRequest {
    fork: String,
}

#[derive(Deserialize)]
struct MergeApplyRequest {
    fork: String,
    preview_digest: String,
}

#[derive(Deserialize)]
struct ForkMutationRequest {
    fork_id: String,
    confirm_fork_id: String,
}

#[derive(Deserialize)]
struct PinRequest {
    snapshot: String,
    pinned: bool,
}

fn exact_snapshot(repository: &AgitRepository, value: &str) -> anyhow::Result<[u8; 32]> {
    anyhow::ensure!(
        value.len() == 64,
        "Mission Control actions require a full snapshot ID"
    );
    let snapshot = parse_id(value)?;
    repository.resolve_snapshot(value)?;
    Ok(snapshot)
}

fn body_json<T: for<'de> Deserialize<'de>>(request: &mut Request) -> anyhow::Result<T> {
    let length = request.body_length().unwrap_or(0);
    anyhow::ensure!(length <= MAX_BODY, "request body exceeds 1 MiB");
    anyhow::ensure!(
        header(request, "Content-Type").is_some_and(|value| value.starts_with("application/json")),
        "request Content-Type must be application/json"
    );
    let mut body = Vec::with_capacity(length.min(MAX_BODY));
    request
        .as_reader()
        .take((MAX_BODY + 1) as u64)
        .read_to_end(&mut body)?;
    anyhow::ensure!(body.len() <= MAX_BODY, "request body exceeds 1 MiB");
    Ok(serde_json::from_slice(&body)?)
}

fn authorize(request: &Request, state: &State, mutation: bool) -> anyhow::Result<()> {
    let authorization = header(request, "Authorization").context("missing Authorization")?;
    let supplied = authorization
        .strip_prefix("Bearer ")
        .context("Authorization must use Bearer")?;
    anyhow::ensure!(
        constant_time_equal(supplied, &state.token),
        "invalid Mission Control capability"
    );
    if mutation {
        require_single_header(request, "Origin", &state.origin)?;
        require_single_header(request, "X-Agit-UI", "1")?;
        if let Some(site) = header(request, "Sec-Fetch-Site") {
            anyhow::ensure!(site == "same-origin", "cross-site mutation refused");
        }
    }
    Ok(())
}

fn require_single_header(request: &Request, name: &str, expected: &str) -> anyhow::Result<()> {
    let values = request
        .headers()
        .iter()
        .filter(|header| header.field.as_str().as_str().eq_ignore_ascii_case(name))
        .map(|header| header.value.as_str())
        .collect::<Vec<_>>();
    anyhow::ensure!(values.len() == 1, "request requires one {name} header");
    anyhow::ensure!(values[0] == expected, "invalid {name} header");
    Ok(())
}

fn header<'a>(request: &'a Request, name: &str) -> Option<&'a str> {
    let mut values = request
        .headers()
        .iter()
        .filter(|header| header.field.as_str().as_str().eq_ignore_ascii_case(name));
    let first = values.next()?.value.as_str();
    values.next().is_none().then_some(first)
}

fn split_target(target: &str) -> anyhow::Result<(String, BTreeMap<String, String>)> {
    let (path, query) = target.split_once('?').unwrap_or((target, ""));
    anyhow::ensure!(path.starts_with('/'), "request path must be absolute");
    anyhow::ensure!(!path.contains(".."), "path traversal refused");
    let mut values = BTreeMap::new();
    for pair in query.split('&').filter(|pair| !pair.is_empty()) {
        let (name, value) = pair.split_once('=').unwrap_or((pair, ""));
        let name = percent_decode(name)?;
        let value = percent_decode(value)?;
        anyhow::ensure!(values.insert(name, value).is_none(), "duplicate query key");
    }
    Ok((path.to_owned(), values))
}

fn percent_decode(value: &str) -> anyhow::Result<String> {
    let bytes = value.as_bytes();
    let mut decoded = Vec::with_capacity(bytes.len());
    let mut index = 0;
    while index < bytes.len() {
        if bytes[index] == b'%' {
            anyhow::ensure!(index + 2 < bytes.len(), "truncated percent escape");
            let high = hex_nibble(bytes[index + 1]).context("invalid percent escape")?;
            let low = hex_nibble(bytes[index + 2]).context("invalid percent escape")?;
            decoded.push(high << 4 | low);
            index += 3;
        } else {
            decoded.push(if bytes[index] == b'+' {
                b' '
            } else {
                bytes[index]
            });
            index += 1;
        }
    }
    String::from_utf8(decoded).context("query value is not UTF-8")
}

fn hex_nibble(value: u8) -> Option<u8> {
    match value {
        b'0'..=b'9' => Some(value - b'0'),
        b'a'..=b'f' => Some(value - b'a' + 10),
        b'A'..=b'F' => Some(value - b'A' + 10),
        _ => None,
    }
}

fn query_bool(query: &BTreeMap<String, String>, name: &str) -> anyhow::Result<bool> {
    match query.get(name).map(String::as_str) {
        None | Some("false" | "0") => Ok(false),
        Some("true" | "1") => Ok(true),
        Some(_) => anyhow::bail!("{name} must be true or false"),
    }
}

fn query_usize(
    query: &BTreeMap<String, String>,
    name: &str,
    default: usize,
    minimum: usize,
    maximum: usize,
) -> anyhow::Result<usize> {
    let value = query
        .get(name)
        .map(|value| value.parse::<usize>())
        .transpose()?
        .unwrap_or(default);
    anyhow::ensure!(
        (minimum..=maximum).contains(&value),
        "{name} is out of range"
    );
    Ok(value)
}

type HttpResponse = Response<std::io::Cursor<Vec<u8>>>;

fn json_response(value: Value) -> anyhow::Result<HttpResponse> {
    Ok(response(
        200,
        "application/json; charset=utf-8",
        serde_json::to_vec(&value)?,
        false,
    ))
}

fn api_error(status: u16, code: &str, message: &str, remedy: &str) -> HttpResponse {
    response(
        status,
        "application/json; charset=utf-8",
        serde_json::to_vec(&json!({
            "error": {"code": code, "message": message, "remedy": remedy}
        }))
        .unwrap_or_default(),
        false,
    )
}

fn static_response(status: u16, content_type: &str, body: &[u8], immutable: bool) -> HttpResponse {
    response(status, content_type, body.to_vec(), immutable)
}

fn response(status: u16, content_type: &str, body: Vec<u8>, immutable: bool) -> HttpResponse {
    let cache = if immutable {
        "public, max-age=31536000, immutable"
    } else {
        "no-store"
    };
    let mut response = Response::from_data(body).with_status_code(StatusCode(status));
    for (name, value) in [
        ("Content-Type", content_type),
        ("Cache-Control", cache),
        (
            "Content-Security-Policy",
            "default-src 'self'; script-src 'self'; style-src 'self'; connect-src 'self'; img-src 'self' data:; object-src 'none'; base-uri 'none'; frame-ancestors 'none'; form-action 'none'",
        ),
        ("X-Content-Type-Options", "nosniff"),
        ("X-Frame-Options", "DENY"),
        ("Referrer-Policy", "no-referrer"),
        ("Cross-Origin-Opener-Policy", "same-origin"),
    ] {
        response.add_header(Header::from_bytes(name, value).expect("static header is valid"));
    }
    response
}

fn constant_time_equal(left: &str, right: &str) -> bool {
    let left = left.as_bytes();
    let right = right.as_bytes();
    let mut difference = left.len() ^ right.len();
    for index in 0..left.len().max(right.len()) {
        difference |= usize::from(
            left.get(index).copied().unwrap_or(0) ^ right.get(index).copied().unwrap_or(0),
        );
    }
    difference == 0
}

fn random_token() -> anyhow::Result<String> {
    let mut token = [0_u8; 32];
    getrandom::getrandom(&mut token)
        .map_err(|error| anyhow::anyhow!("generate UI capability: {error}"))?;
    Ok(hex::encode(token))
}

fn open_browser(url: &str) -> anyhow::Result<()> {
    let (program, argument) = if cfg!(target_os = "macos") {
        ("open", url)
    } else if cfg!(target_os = "linux") {
        ("xdg-open", url)
    } else {
        anyhow::bail!("automatic browser opening is unsupported on this platform")
    };
    Command::new(program)
        .arg(argument)
        .spawn()
        .with_context(|| format!("start {program}"))?;
    Ok(())
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn query_decoding_and_constant_time_tokens_are_strict() {
        let (_, query) = split_target("/api/v1/events?after=abc%3A12&limit=20").unwrap();
        assert_eq!(query["after"], "abc:12");
        assert_eq!(query["limit"], "20");
        assert!(split_target("/../secret").is_err());
        assert!(constant_time_equal("abc", "abc"));
        assert!(!constant_time_equal("abc", "abd"));
        assert!(!constant_time_equal("abc", "ab"));
    }
}
