//! Narrow typed bridge to the operating system's "open" verbs.
//! No shell is involved: every target is passed as one argv entry.

use std::ffi::OsStr;
use std::path::{Path, PathBuf};
use std::process::Command;

const OUTSIDE: &str = "That file is outside the workspace";
const MISSING: &str = "That file no longer exists";

// Canonicalize both sides so `..` and symlinks cannot leave the workspace.
fn confine(path: &str, workspace: &str) -> Result<PathBuf, String> {
    let root = Path::new(workspace)
        .canonicalize()
        .map_err(|_| "The workspace is unavailable".to_string())?;
    let target = Path::new(path);
    if !target.is_absolute() {
        return Err(OUTSIDE.into());
    }
    let resolved = target.canonicalize().map_err(|_| MISSING.to_string())?;
    if resolved.starts_with(&root) {
        Ok(resolved)
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

#[tauri::command]
pub fn open_path(path: String, workspace: String) -> Result<(), String> {
    let resolved = confine(&path, &workspace)?;
    spawn(OPENER, &[resolved.as_os_str()])
}

#[tauri::command]
pub fn reveal_path(path: String, workspace: String) -> Result<(), String> {
    reveal(&confine(&path, &workspace)?)
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

    fn scratch(name: &str) -> PathBuf {
        let dir = std::env::temp_dir().join(format!("codeaf-native-{name}-{}", std::process::id()));
        let _ = fs::remove_dir_all(&dir);
        fs::create_dir_all(&dir).unwrap();
        dir
    }

    #[test]
    fn accepts_files_inside_workspace() {
        let dir = scratch("inside");
        let file = dir.join("a.txt");
        fs::write(&file, "x").unwrap();
        let got = confine(file.to_str().unwrap(), dir.to_str().unwrap()).unwrap();
        assert!(got.ends_with("a.txt"));
    }

    #[test]
    fn refuses_missing_relative_and_dotdot() {
        let dir = scratch("bad");
        let ws = dir.join("ws");
        fs::create_dir_all(&ws).unwrap();
        fs::write(dir.join("outside.txt"), "x").unwrap();
        let w = ws.to_str().unwrap();
        assert!(confine(ws.join("nope").to_str().unwrap(), w).is_err());
        assert!(confine("a.txt", w).is_err());
        let sneaky = ws.join("..").join("outside.txt");
        assert!(confine(sneaky.to_str().unwrap(), w).is_err());
    }

    #[cfg(unix)]
    #[test]
    fn refuses_symlink_escape() {
        let dir = scratch("link");
        let ws = dir.join("ws");
        fs::create_dir_all(&ws).unwrap();
        let secret = dir.join("secret.txt");
        fs::write(&secret, "x").unwrap();
        let link = ws.join("link.txt");
        std::os::unix::fs::symlink(&secret, &link).unwrap();
        assert!(confine(link.to_str().unwrap(), ws.to_str().unwrap()).is_err());
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
}
