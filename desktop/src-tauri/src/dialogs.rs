//! The system's own file and folder chooser, behind one typed command.
//!
//! The renderer is granted no `dialog:*` permission. It asks Rust for a folder or
//! files; Rust shows the native chooser parented to the asking window, and hands
//! back only what the person picked, canonicalised and checked. Cancelling is an
//! answer, not an error. Nothing here widens a filesystem or asset scope.

use std::path::{Path, PathBuf};
use std::sync::atomic::{AtomicBool, Ordering};

use serde::{Deserialize, Serialize};
use tauri::{Runtime, Webview};
use tauri_plugin_dialog::{DialogExt, FilePath};

use crate::windows::trusted;

const MAX_FILES: usize = 32;
const MAX_PATH: usize = 4096;
const MAX_TITLE: usize = 120;

/// One chooser at a time for the whole app: a second request answers `busy`
/// rather than stacking sheets the person has to dismiss one by one.
static OPEN: AtomicBool = AtomicBool::new(false);

struct Gate;

impl Gate {
    fn enter() -> Option<Gate> {
        OPEN.compare_exchange(false, true, Ordering::AcqRel, Ordering::Acquire)
            .ok()
            .map(|_| Gate)
    }
}

impl Drop for Gate {
    fn drop(&mut self) {
        OPEN.store(false, Ordering::Release);
    }
}

#[derive(Clone, Copy, Debug, PartialEq, Eq, Deserialize)]
#[serde(rename_all = "lowercase")]
pub enum PickKind {
    Folder,
    File,
}

#[derive(Debug, Deserialize)]
#[serde(deny_unknown_fields)]
pub struct PickRequest {
    pub kind: PickKind,
    #[serde(default)]
    pub multiple: bool,
    #[serde(default)]
    pub title: Option<String>,
}

#[derive(Debug, PartialEq, Serialize)]
pub struct Picked {
    pub path: String,
    pub name: String,
}

#[derive(Debug, PartialEq, Serialize)]
#[serde(tag = "status", rename_all = "lowercase")]
pub enum PickResult {
    Picked { paths: Vec<Picked> },
    Cancelled,
    Busy,
}

/// A chosen path the renderer may see: absolute, existing, of the kind asked
/// for, valid UTF-8, free of control characters, and resolved through symlinks.
pub fn checked_choice(path: &Path, kind: PickKind) -> Result<Picked, String> {
    const REFUSED: &str = "codeaf cannot use that choice";
    if !path.is_absolute() {
        return Err(REFUSED.into());
    }
    let resolved: PathBuf = path
        .canonicalize()
        .map_err(|_| "That choice no longer exists")?;
    let right_kind = match kind {
        PickKind::Folder => resolved.is_dir(),
        PickKind::File => resolved.is_file(),
    };
    let text = resolved.to_str().ok_or(REFUSED)?;
    if !right_kind || text.len() > MAX_PATH || text.chars().any(char::is_control) {
        return Err(REFUSED.into());
    }
    let name = resolved
        .file_name()
        .and_then(|n| n.to_str())
        .unwrap_or(text)
        .to_string();
    Ok(Picked {
        path: text.to_string(),
        name,
    })
}

fn title_for(request: &PickRequest) -> Result<String, String> {
    match request.title.as_deref().map(str::trim) {
        Some(t) if t.chars().count() > MAX_TITLE || t.chars().any(char::is_control) => {
            Err("That chooser title cannot be shown".into())
        }
        Some(t) if !t.is_empty() => Ok(t.to_string()),
        _ => Ok(match request.kind {
            PickKind::Folder => "Choose a folder".into(),
            PickKind::File if request.multiple => "Choose files".into(),
            PickKind::File => "Choose a file".into(),
        }),
    }
}

fn to_paths(chosen: Vec<FilePath>) -> Vec<PathBuf> {
    // A URL answer (mobile content URIs) has no local path on desktop; drop it.
    chosen
        .into_iter()
        .filter_map(|p| p.into_path().ok())
        .collect()
}

/// Turns what the chooser returned into one honest answer. Nothing chosen is
/// `cancelled`; anything that fails the checks fails the whole pick, so the
/// renderer never sees half a selection it did not ask for.
pub fn settle(chosen: Option<Vec<PathBuf>>, kind: PickKind) -> Result<PickResult, String> {
    let Some(chosen) = chosen.filter(|c| !c.is_empty()) else {
        return Ok(PickResult::Cancelled);
    };
    if chosen.len() > MAX_FILES {
        return Err(format!("Choose at most {MAX_FILES} files at once"));
    }
    let paths = chosen
        .iter()
        .map(|p| checked_choice(p, kind))
        .collect::<Result<Vec<_>, _>>()?;
    Ok(PickResult::Picked { paths })
}

#[tauri::command]
pub async fn dialog_pick<R: Runtime>(
    webview: Webview<R>,
    request: PickRequest,
) -> Result<PickResult, String> {
    trusted(&webview)?;
    let title = title_for(&request)?;
    let Some(_gate) = Gate::enter() else {
        return Ok(PickResult::Busy);
    };
    let window = webview.window();
    let builder = webview.dialog().file().set_title(title).set_parent(&window);
    // The blocking variants are the plugin's documented choice in async commands:
    // this runs off the main thread, and the plugin shows the chooser on it.
    let chosen = match (request.kind, request.multiple) {
        (PickKind::Folder, _) => builder.blocking_pick_folder().map(|p| vec![p]),
        (PickKind::File, false) => builder.blocking_pick_file().map(|p| vec![p]),
        (PickKind::File, true) => builder.blocking_pick_files(),
    };
    settle(chosen.map(to_paths), request.kind)
}

#[cfg(test)]
mod tests {
    use super::*;
    use std::fs;

    fn scratch(name: &str) -> PathBuf {
        let dir =
            std::env::temp_dir().join(format!("codeaf-dialogs-{name}-{}", std::process::id()));
        let _ = fs::remove_dir_all(&dir);
        fs::create_dir_all(&dir).unwrap();
        dir
    }

    #[test]
    fn nothing_chosen_is_cancelled_not_an_error() {
        assert_eq!(settle(None, PickKind::Folder), Ok(PickResult::Cancelled));
        assert_eq!(
            settle(Some(vec![]), PickKind::File),
            Ok(PickResult::Cancelled)
        );
    }

    #[test]
    fn a_folder_choice_is_canonical() {
        let dir = scratch("folder");
        let inner = dir.join("project");
        fs::create_dir_all(&inner).unwrap();
        let dotted = dir.join("project").join("..").join("project");
        let got = settle(Some(vec![dotted]), PickKind::Folder).unwrap();
        let PickResult::Picked { paths } = got else {
            panic!("picked")
        };
        assert_eq!(paths[0].name, "project");
        assert_eq!(Path::new(&paths[0].path), inner.canonicalize().unwrap());
    }

    #[test]
    fn the_wrong_kind_missing_or_relative_is_refused() {
        let dir = scratch("kind");
        let file = dir.join("a.txt");
        fs::write(&file, "x").unwrap();
        assert!(checked_choice(&file, PickKind::Folder).is_err());
        assert!(checked_choice(&dir, PickKind::File).is_err());
        assert!(checked_choice(&dir.join("gone"), PickKind::Folder).is_err());
        assert!(checked_choice(Path::new("relative"), PickKind::Folder).is_err());
        assert!(checked_choice(&file, PickKind::File).is_ok());
    }

    #[test]
    fn one_bad_entry_fails_the_whole_pick() {
        let dir = scratch("mixed");
        let good = dir.join("a.txt");
        fs::write(&good, "x").unwrap();
        assert!(settle(Some(vec![good, dir.join("gone.txt")]), PickKind::File).is_err());
    }

    #[test]
    fn too_many_files_are_refused() {
        let dir = scratch("many");
        let files: Vec<PathBuf> = (0..=MAX_FILES)
            .map(|i| {
                let f = dir.join(format!("{i}.txt"));
                fs::write(&f, "x").unwrap();
                f
            })
            .collect();
        assert!(settle(Some(files), PickKind::File).is_err());
    }

    #[cfg(unix)]
    #[test]
    fn a_control_character_in_a_path_is_refused() {
        let dir = scratch("ctl");
        let odd = dir.join("a\nb");
        fs::create_dir_all(&odd).unwrap();
        assert!(checked_choice(&odd, PickKind::Folder).is_err());
    }

    #[test]
    fn titles_default_by_kind_and_refuse_control_characters() {
        let req = |kind, multiple, title: Option<&str>| PickRequest {
            kind,
            multiple,
            title: title.map(String::from),
        };
        assert_eq!(
            title_for(&req(PickKind::Folder, false, None)).unwrap(),
            "Choose a folder"
        );
        assert_eq!(
            title_for(&req(PickKind::File, true, Some(" "))).unwrap(),
            "Choose files"
        );
        assert_eq!(
            title_for(&req(PickKind::File, false, Some("Attach"))).unwrap(),
            "Attach"
        );
        assert!(title_for(&req(PickKind::File, false, Some("a\u{1b}[2J"))).is_err());
    }

    #[test]
    fn requests_refuse_unknown_fields() {
        let ok: PickRequest =
            serde_json::from_value(serde_json::json!({"kind": "folder"})).unwrap();
        assert_eq!(ok.kind, PickKind::Folder);
        for bad in [
            serde_json::json!({"kind": "folder", "defaultPath": "/etc"}),
            serde_json::json!({"kind": "save"}),
        ] {
            assert!(serde_json::from_value::<PickRequest>(bad).is_err());
        }
    }

    #[test]
    fn only_one_chooser_at_a_time() {
        let first = Gate::enter().expect("free");
        assert!(Gate::enter().is_none());
        drop(first);
        assert!(Gate::enter().is_some());
    }

    #[test]
    fn results_serialise_with_a_status_tag() {
        assert_eq!(
            serde_json::to_value(PickResult::Cancelled).unwrap(),
            serde_json::json!({"status": "cancelled"})
        );
    }
}
