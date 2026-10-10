//! Open a confined path in one editor the engine just listed.
//!
//! The window names an id from GET /sessions/{id}/editors. That id is a
//! desktop-file basename or a Mac bundle id. It is never a program and never
//! a shell command. This command asks that route again, and only then asks
//! the route's own open action to start the editor. The process line is built
//! on the engine, from the id it enumerated, as separate argv. Nothing here
//! passes a renderer string to a shell.

use std::path::{Path, PathBuf};

use serde::Deserialize;

use crate::native::{self, HttpParts};

const UNKNOWN: &str = "That editor is not one this machine just listed for this file.";
const CANNOT: &str = "This machine cannot start an editor.";

const KNOWN_REASONS: &[&str] = &[
    "The engine is on another machine.",
    "This machine has no display, so an editor cannot be opened.",
    "This machine cannot list editors.",
    "This machine cannot start an editor.",
];

/// At most the engine's own cap, with room for a list that arrives a little long.
const EDITOR_CAP: usize = 32;

/// What a fresh GET /editors said. Ids that are not handler ids are dropped
/// here, so a poisoned row never becomes something we could start.
struct FreshEditors {
    ids: Vec<String>,
    local: bool,
    open: bool,
    reason: String,
}

/// The only thing a successful check produces: the confined path and an id
/// that was on the fresh list. There is no program and no argument string.
#[derive(Debug)]
struct OpenPlan {
    path: PathBuf,
    editor_id: String,
}

#[derive(Deserialize)]
struct EditorBody {
    #[serde(default)]
    editors: Vec<EditorRow>,
    #[serde(default)]
    local: bool,
    #[serde(default)]
    open: bool,
    #[serde(default)]
    reason: String,
}

#[derive(Deserialize)]
struct EditorRow {
    id: String,
}

// The same shape the engine accepts: a desktop basename or a bundle id.
// A space, a slash or a shell mark never matches, so it cannot be an argument.
fn safe_editor_id(id: &str) -> bool {
    let bytes = id.as_bytes();
    if bytes.is_empty() || bytes.len() > 181 || id.contains("..") {
        return false;
    }
    if !bytes[0].is_ascii_alphanumeric() {
        return false;
    }
    bytes
        .iter()
        .all(|byte| byte.is_ascii_alphanumeric() || matches!(byte, b'.' | b'+' | b'_' | b'-'))
}

// Bridge session ids are 32 random bytes, hex. Anything else would be a path
// written into the request target.
fn checked_session(session: &str) -> Result<&str, String> {
    if session.len() == 64 && session.bytes().all(|byte| byte.is_ascii_hexdigit()) {
        Ok(session)
    } else {
        Err(native::UNAVAILABLE.into())
    }
}

fn query_escape(value: &str) -> String {
    let mut out = String::new();
    for byte in value.bytes() {
        match byte {
            b'A'..=b'Z' | b'a'..=b'z' | b'0'..=b'9' | b'-' | b'_' | b'.' | b'~' => {
                out.push(byte as char);
            }
            _ => out.push_str(&format!("%{byte:02X}")),
        }
    }
    out
}

fn editors_get_path(session: &str, file: &str) -> Result<String, String> {
    let session = checked_session(session)?;
    if file.is_empty() || file.contains('\0') {
        return Err(native::UNAVAILABLE.into());
    }
    Ok(format!(
        "/api/engine/sessions/{session}/editors?path={}",
        query_escape(file)
    ))
}

fn editors_post_path(session: &str) -> Result<String, String> {
    let session = checked_session(session)?;
    Ok(format!("/api/engine/sessions/{session}/editors/open"))
}

fn known_reason(reason: &str) -> Option<&str> {
    KNOWN_REASONS
        .iter()
        .find(|known| **known == reason)
        .copied()
}

fn fresh_from_json(bytes: &[u8]) -> Result<FreshEditors, String> {
    let parsed: EditorBody =
        serde_json::from_slice(bytes).map_err(|_| native::UNAVAILABLE.to_string())?;
    let mut ids = Vec::new();
    for row in parsed.editors {
        if ids.len() == EDITOR_CAP {
            break;
        }
        if safe_editor_id(&row.id) && !ids.iter().any(|id| id == &row.id) {
            ids.push(row.id);
        }
    }
    Ok(FreshEditors {
        ids,
        local: parsed.local,
        open: parsed.open,
        reason: known_reason(&parsed.reason).unwrap_or("").to_string(),
    })
}

fn cannot_open(fresh: &FreshEditors) -> String {
    if fresh.reason.is_empty() {
        CANNOT.to_string()
    } else {
        fresh.reason.clone()
    }
}

/// Confine first, then accept the id only when this listing just named it and
/// the engine can start a program. An id that fails the shape check is refused
/// even when the listing repeats it.
fn plan_open(
    path: &str,
    editor_id: &str,
    roots: &[String],
    fresh: &FreshEditors,
) -> Result<OpenPlan, String> {
    let resolved = native::confine(path, roots)?;
    if !safe_editor_id(editor_id) || !fresh.ids.iter().any(|id| id == editor_id) {
        return Err(UNKNOWN.into());
    }
    if !fresh.local || !fresh.open {
        return Err(cannot_open(fresh));
    }
    Ok(OpenPlan {
        path: resolved,
        editor_id: editor_id.to_string(),
    })
}

/// JSON for POST /editors/open. The keys are the path and the id. There is no
/// command field, and the id was already checked against the fresh list.
fn open_payload(plan: &OpenPlan) -> Result<Vec<u8>, String> {
    if !safe_editor_id(&plan.editor_id) {
        return Err(UNKNOWN.into());
    }
    let path = plan
        .path
        .to_str()
        .ok_or_else(|| native::UNAVAILABLE.to_string())?;
    serde_json::to_vec(&serde_json::json!({ "path": path, "id": plan.editor_id }))
        .map_err(|_| native::UNAVAILABLE.to_string())
}

async fn fresh_editors(
    app: &tauri::AppHandle,
    session: &str,
    file: &Path,
) -> Result<FreshEditors, String> {
    let file = file
        .to_str()
        .ok_or_else(|| native::UNAVAILABLE.to_string())?;
    let path = editors_get_path(session, file)?;
    let raw = native::engine_http(app, "GET", path, None).await?;
    let HttpParts { status, body } = native::http_parts(&raw)?;
    if status == 403 {
        return Err(native::OUTSIDE.into());
    }
    if status != 200 {
        return Err(native::UNAVAILABLE.into());
    }
    fresh_from_json(&body)
}

async fn start_editor(
    app: &tauri::AppHandle,
    session: &str,
    plan: &OpenPlan,
) -> Result<(), String> {
    let path = editors_post_path(session)?;
    let body = open_payload(plan)?;
    let raw = native::engine_http(app, "POST", path, Some(body)).await?;
    let HttpParts { status, .. } = native::http_parts(&raw)?;
    match status {
        200 => Ok(()),
        400 => Err(UNKNOWN.into()),
        403 => Err(native::OUTSIDE.into()),
        _ => Err(native::UNAVAILABLE.into()),
    }
}

/// Open `path` in the editor `editor_id` from a fresh listing for `session`.
///
/// `session` is the conversation id the window already used to list editors.
/// The design does not name it; the editors route is addressed by it, so the
/// fresh list is that conversation's and not a command line from the page.
#[tauri::command]
pub async fn open_with(
    app: tauri::AppHandle,
    path: String,
    editor_id: String,
    session: String,
) -> Result<(), String> {
    // A command-shaped id is refused before any request, so it is never copied
    // into a URL or a JSON body.
    if !safe_editor_id(&editor_id) {
        return Err(UNKNOWN.into());
    }
    let _ = checked_session(&session)?;
    let roots = native::engine_roots(&app).await?;
    let resolved = native::confine(&path, &roots)?;
    let fresh = fresh_editors(&app, &session, &resolved).await?;
    let plan = plan_open(&path, &editor_id, &roots, &fresh)?;
    start_editor(&app, &session, &plan).await
}

#[cfg(test)]
fn scratch(name: &str) -> PathBuf {
    let dir = std::env::temp_dir().join(format!("codeaf-editors-{name}-{}", std::process::id()));
    let _ = std::fs::remove_dir_all(&dir);
    std::fs::create_dir_all(&dir).unwrap();
    dir
}

#[cfg(test)]
fn root_of(path: &Path) -> String {
    path.to_str().unwrap().to_string()
}

#[cfg(test)]
fn listed(ids: &[&str]) -> FreshEditors {
    FreshEditors {
        ids: ids.iter().map(|id| (*id).to_string()).collect(),
        local: true,
        open: true,
        reason: String::new(),
    }
}

#[cfg(test)]
fn try_open(
    path: &str,
    editor_id: &str,
    roots: &[String],
    fresh: &FreshEditors,
) -> Result<OpenPlan, String> {
    plan_open(path, editor_id, roots, fresh)
}

#[test]
fn refuses_path_outside_roots() {
    let dir = scratch("outside");
    let ws = dir.join("ws");
    std::fs::create_dir_all(&ws).unwrap();
    let inside = ws.join("a.txt");
    std::fs::write(&inside, "x").unwrap();
    let outside = dir.join("no.txt");
    std::fs::write(&outside, "z").unwrap();
    let roots = vec![root_of(&ws)];
    let fresh = listed(&["code.desktop"]);

    let err = try_open(outside.to_str().unwrap(), "code.desktop", &roots, &fresh).unwrap_err();
    assert_eq!(err, native::OUTSIDE);
    assert_eq!(
        try_open("a.txt", "code.desktop", &roots, &fresh).unwrap_err(),
        native::OUTSIDE
    );
    assert_eq!(
        try_open(outside.to_str().unwrap(), "code.desktop", &[], &fresh).unwrap_err(),
        native::OUTSIDE
    );
    let sneaky = ws.join("..").join("no.txt");
    assert_eq!(
        try_open(sneaky.to_str().unwrap(), "code.desktop", &roots, &fresh).unwrap_err(),
        native::OUTSIDE
    );
    let sibling = dir.join("ws-other");
    std::fs::create_dir_all(&sibling).unwrap();
    let sibling_file = sibling.join("x.txt");
    std::fs::write(&sibling_file, "z").unwrap();
    assert_eq!(
        try_open(
            sibling_file.to_str().unwrap(),
            "code.desktop",
            &roots,
            &fresh
        )
        .unwrap_err(),
        native::OUTSIDE
    );
    assert_eq!(
        try_open(
            ws.join("missing.txt").to_str().unwrap(),
            "code.desktop",
            &roots,
            &fresh
        )
        .unwrap_err(),
        native::MISSING
    );

    #[cfg(unix)]
    {
        let secret = dir.join("secret.txt");
        std::fs::write(&secret, "x").unwrap();
        let link = ws.join("link.txt");
        std::os::unix::fs::symlink(&secret, &link).unwrap();
        assert_eq!(
            try_open(link.to_str().unwrap(), "code.desktop", &roots, &fresh).unwrap_err(),
            native::OUTSIDE
        );
    }

    // The file that is inside is not what this test refuses. It is here so a
    // regression that refuses every path still fails the outside cases above
    // for the outside reason, and a listed editor does not open the outside one.
    let plan = try_open(inside.to_str().unwrap(), "code.desktop", &roots, &fresh).unwrap();
    assert!(plan.path.ends_with("a.txt"));
    assert_eq!(plan.editor_id, "code.desktop");
}

#[test]
fn refuses_unknown_editor() {
    let dir = scratch("unknown");
    let ws = dir.join("ws");
    std::fs::create_dir_all(&ws).unwrap();
    let file = ws.join("a.txt");
    std::fs::write(&file, "x").unwrap();
    let roots = vec![root_of(&ws)];
    let path = file.to_str().unwrap();
    let fresh = listed(&["code.desktop"]);

    for id in [
        "other.desktop",
        "gedit",
        "bash -c id",
        "code.desktop;id",
        "/usr/bin/gedit",
        "sh -c touch /tmp/x",
        "",
        "..",
        "code.desktop/../../bin",
    ] {
        let err = try_open(path, id, &roots, &fresh).unwrap_err();
        assert_eq!(err, UNKNOWN, "{id}");
    }

    // A listing that repeats a command line does not make that string an editor.
    let poisoned = fresh_from_json(
        br#"{"editors":[{"id":"code.desktop","name":"Code"},{"id":"bash -c id","name":"no"},{"id":"/usr/bin/gedit","name":"Gedit"}],"local":true,"open":true}"#,
    )
    .unwrap();
    assert_eq!(poisoned.ids, vec!["code.desktop".to_string()]);
    assert_eq!(
        try_open(path, "bash -c id", &roots, &poisoned).unwrap_err(),
        UNKNOWN
    );
    assert_eq!(
        try_open(path, "/usr/bin/gedit", &roots, &poisoned).unwrap_err(),
        UNKNOWN
    );

    let session = "ab".repeat(32);
    assert!(session.len() == 64);
    let get = editors_get_path(&session, path).unwrap();
    assert!(get.starts_with(&format!("/api/engine/sessions/{session}/editors?path=")));
    assert!(!get.contains("bash"));
    assert!(editors_get_path("../etc/passwd", path).is_err());
    assert!(editors_get_path("bash -c id", path).is_err());
    assert!(editors_post_path("../../editors/open").is_err());
    let post = editors_post_path(&session).unwrap();
    assert_eq!(post, format!("/api/engine/sessions/{session}/editors/open"));
    assert!(!post.contains("command"));
}

#[test]
fn known_editor_is_an_id_not_a_command() {
    let dir = scratch("known");
    let ws = dir.join("ws");
    std::fs::create_dir_all(&ws).unwrap();
    let file = ws.join("a.txt");
    std::fs::write(&file, "x").unwrap();
    let roots = vec![root_of(&ws)];
    let fresh = fresh_from_json(
        br#"{"editors":[{"id":"code.desktop","name":"Code","default":true}],"local":true,"open":true}"#,
    )
    .unwrap();
    let plan = try_open(file.to_str().unwrap(), "code.desktop", &roots, &fresh).unwrap();
    let bytes = open_payload(&plan).unwrap();
    let value: serde_json::Value = serde_json::from_slice(&bytes).unwrap();
    let obj = value.as_object().unwrap();
    assert_eq!(obj.len(), 2);
    assert_eq!(obj["id"], "code.desktop");
    assert!(obj["path"].as_str().unwrap().ends_with("a.txt"));
    assert!(obj.get("command").is_none());
    assert!(obj.get("exec").is_none());

    let remote = FreshEditors {
        ids: vec!["code.desktop".to_string()],
        local: false,
        open: false,
        reason: "The engine is on another machine.".into(),
    };
    assert_eq!(
        try_open(file.to_str().unwrap(), "code.desktop", &roots, &remote).unwrap_err(),
        "The engine is on another machine."
    );
    let headless = FreshEditors {
        ids: vec!["code.desktop".to_string()],
        local: true,
        open: false,
        reason: String::new(),
    };
    assert_eq!(
        try_open(file.to_str().unwrap(), "code.desktop", &roots, &headless).unwrap_err(),
        CANNOT
    );
}
