//! Streaming discovery and removal of regenerable workspace caches.

use crate::policy::CapturePolicy;
use anyhow::Context;
use serde::Serialize;
use std::ffi::OsStr;
use std::fs::{self, ReadDir};
use std::os::unix::ffi::OsStrExt;
use std::os::unix::fs::MetadataExt;
use std::path::{Component, Path, PathBuf};

const MAX_CANDIDATES: usize = 10_000;
const MAX_DEPTH: usize = 1_024;

#[derive(Debug, Clone, Serialize)]
pub struct ShrinkCandidate {
    pub path: String,
    pub class: &'static str,
    pub entries: u64,
    pub logical_bytes: u64,
    pub physical_bytes: u64,
    #[serde(skip)]
    relative: PathBuf,
}

#[derive(Debug, Clone, Serialize)]
pub struct ShrinkPlan {
    pub candidates: Vec<ShrinkCandidate>,
    pub total_entries: u64,
    pub total_logical_bytes: u64,
    pub total_physical_bytes: u64,
}

pub fn discover(root: &Path, includes: &[PathBuf]) -> anyhow::Result<ShrinkPlan> {
    let root = root
        .canonicalize()
        .with_context(|| format!("open {}", root.display()))?;
    let policy = CapturePolicy::load(&root)?;
    let mut candidates = Vec::new();

    for include in includes {
        let relative = validate_relative(include)?;
        refuse_internal_path(&relative)?;
        anyhow::ensure!(
            !policy.excludes_bytes(relative.as_os_str().as_bytes()),
            "cannot shrink policy-excluded path {}; its undo bytes are not captured",
            include.display()
        );
        let path = root.join(&relative);
        let metadata = fs::symlink_metadata(&path)
            .with_context(|| format!("inspect shrink path {}", include.display()))?;
        let usage = measure(&path, &metadata)?;
        candidates.push(candidate(relative, "explicit", usage));
        anyhow::ensure!(
            candidates.len() <= MAX_CANDIDATES,
            "shrink discovery exceeds {MAX_CANDIDATES} candidates"
        );
    }

    let mut stack = vec![Frame::open(root.clone())?];
    while !stack.is_empty() {
        let next = stack.last_mut().expect("stack is not empty").entries.next();
        let Some(entry) = next else {
            stack.pop();
            continue;
        };
        let entry = entry?;
        let file_type = entry.file_type()?;
        if !file_type.is_dir() || file_type.is_symlink() {
            continue;
        }
        let relative = entry.path().strip_prefix(&root)?.to_owned();
        if is_internal_root(&relative) {
            continue;
        }
        if policy.excludes_bytes(relative.as_os_str().as_bytes()) {
            continue;
        }
        if let Some(class) = classify(&entry.path()) {
            let metadata = fs::symlink_metadata(entry.path())?;
            let usage = measure(&entry.path(), &metadata)?;
            candidates.push(candidate(relative, class, usage));
            anyhow::ensure!(
                candidates.len() <= MAX_CANDIDATES,
                "shrink discovery exceeds {MAX_CANDIDATES} candidates"
            );
            continue;
        }
        anyhow::ensure!(stack.len() < MAX_DEPTH, "workspace tree exceeds safe depth");
        stack.push(Frame::open(entry.path())?);
    }

    candidates.sort_by(|left, right| left.relative.cmp(&right.relative));
    let mut normalized: Vec<ShrinkCandidate> = Vec::with_capacity(candidates.len());
    for candidate in candidates {
        if normalized
            .iter()
            .any(|parent| candidate.relative.starts_with(&parent.relative))
        {
            continue;
        }
        normalized.push(candidate);
    }
    let total_entries = normalized
        .iter()
        .fold(0_u64, |total, item| total.saturating_add(item.entries));
    let total_logical_bytes = normalized.iter().fold(0_u64, |total, item| {
        total.saturating_add(item.logical_bytes)
    });
    let total_physical_bytes = normalized.iter().fold(0_u64, |total, item| {
        total.saturating_add(item.physical_bytes)
    });
    Ok(ShrinkPlan {
        candidates: normalized,
        total_entries,
        total_logical_bytes,
        total_physical_bytes,
    })
}

pub fn apply(root: &Path, plan: &ShrinkPlan) -> anyhow::Result<()> {
    let root = root.canonicalize()?;
    for candidate in &plan.candidates {
        let relative = validate_relative(&candidate.relative)?;
        refuse_internal_path(&relative)?;
        let path = root.join(relative);
        ensure_safe_ancestors(&root, &path)?;
        let metadata = match fs::symlink_metadata(&path) {
            Ok(metadata) => metadata,
            Err(error) if error.kind() == std::io::ErrorKind::NotFound => continue,
            Err(error) => return Err(error.into()),
        };
        if metadata.is_dir() && !metadata.file_type().is_symlink() {
            fs::remove_dir_all(&path)?;
        } else {
            fs::remove_file(&path)?;
        }
    }
    Ok(())
}

struct Frame {
    entries: ReadDir,
}

impl Frame {
    fn open(path: PathBuf) -> anyhow::Result<Self> {
        Ok(Self {
            entries: fs::read_dir(path)?,
        })
    }
}

#[derive(Debug, Clone, Copy)]
struct Usage {
    entries: u64,
    logical_bytes: u64,
    physical_bytes: u64,
}

fn measure(path: &Path, metadata: &fs::Metadata) -> anyhow::Result<Usage> {
    let mut usage = Usage {
        entries: 1,
        logical_bytes: metadata.len(),
        physical_bytes: metadata.blocks().saturating_mul(512),
    };
    if !metadata.is_dir() || metadata.file_type().is_symlink() {
        return Ok(usage);
    }
    let mut stack = vec![Frame::open(path.to_owned())?];
    while !stack.is_empty() {
        let next = stack.last_mut().expect("stack is not empty").entries.next();
        let Some(entry) = next else {
            stack.pop();
            continue;
        };
        let entry = entry?;
        let metadata = fs::symlink_metadata(entry.path())?;
        usage.entries = usage.entries.saturating_add(1);
        usage.logical_bytes = usage.logical_bytes.saturating_add(metadata.len());
        usage.physical_bytes = usage
            .physical_bytes
            .saturating_add(metadata.blocks().saturating_mul(512));
        if metadata.is_dir() && !metadata.file_type().is_symlink() {
            anyhow::ensure!(stack.len() < MAX_DEPTH, "cache tree exceeds safe depth");
            stack.push(Frame::open(entry.path())?);
        }
    }
    Ok(usage)
}

fn candidate(relative: PathBuf, class: &'static str, usage: Usage) -> ShrinkCandidate {
    ShrinkCandidate {
        path: relative.to_string_lossy().into_owned(),
        class,
        entries: usage.entries,
        logical_bytes: usage.logical_bytes,
        physical_bytes: usage.physical_bytes,
        relative,
    }
}

fn classify(path: &Path) -> Option<&'static str> {
    let name = path.file_name()?;
    if name == OsStr::new("node_modules") {
        return Some("javascript_dependencies");
    }
    if matches_name(
        name,
        &[".next", ".nuxt", ".parcel-cache", ".turbo", ".vite"],
    ) {
        return Some("frontend_cache");
    }
    if matches_name(name, &[".venv", "venv", "__pycache__"]) {
        return Some("python_cache");
    }
    if name == OsStr::new("target")
        && path
            .parent()
            .is_some_and(|parent| parent.join("Cargo.toml").is_file())
    {
        return Some("rust_build_cache");
    }
    None
}

fn matches_name(name: &OsStr, candidates: &[&str]) -> bool {
    candidates
        .iter()
        .any(|candidate| name == OsStr::new(candidate))
}

fn validate_relative(path: &Path) -> anyhow::Result<PathBuf> {
    anyhow::ensure!(!path.as_os_str().is_empty(), "shrink path cannot be empty");
    anyhow::ensure!(!path.is_absolute(), "shrink path must be relative");
    anyhow::ensure!(
        path.components()
            .all(|component| matches!(component, Component::Normal(_))),
        "shrink path contains an unsafe component"
    );
    Ok(path.to_owned())
}

fn refuse_internal_path(path: &Path) -> anyhow::Result<()> {
    let first = path.components().next();
    anyhow::ensure!(
        !matches!(first, Some(Component::Normal(name)) if name == OsStr::new(".git") || name == OsStr::new(".furrow")),
        "refusing to shrink furrow or Git internal state"
    );
    Ok(())
}

fn is_internal_root(path: &Path) -> bool {
    matches!(
        path.components().next(),
        Some(Component::Normal(name)) if name == OsStr::new(".git") || name == OsStr::new(".furrow")
    )
}

fn ensure_safe_ancestors(root: &Path, path: &Path) -> anyhow::Result<()> {
    let relative = path
        .strip_prefix(root)
        .context("shrink path escaped the workspace")?;
    let mut current = root.to_owned();
    let components: Vec<_> = relative.components().collect();
    for component in components.iter().take(components.len().saturating_sub(1)) {
        let Component::Normal(name) = component else {
            anyhow::bail!("shrink path contains an unsafe component")
        };
        current.push(name);
        let metadata = fs::symlink_metadata(&current)?;
        anyhow::ensure!(
            metadata.is_dir() && !metadata.file_type().is_symlink(),
            "shrink path has an unsafe parent"
        );
    }
    Ok(())
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn discovers_known_caches_without_entering_internal_or_symlinked_trees() {
        let temporary = tempfile::tempdir().unwrap();
        fs::create_dir_all(temporary.path().join("node_modules/pkg")).unwrap();
        fs::write(
            temporary.path().join("node_modules/pkg/index.js"),
            b"module",
        )
        .unwrap();
        fs::create_dir_all(temporary.path().join(".git/node_modules/hidden")).unwrap();
        let outside = tempfile::tempdir().unwrap();
        fs::create_dir_all(outside.path().join("node_modules/private")).unwrap();
        std::os::unix::fs::symlink(outside.path(), temporary.path().join("linked")).unwrap();

        let plan = discover(temporary.path(), &[]).unwrap();
        assert_eq!(plan.candidates.len(), 1);
        assert_eq!(plan.candidates[0].path, "node_modules");
        assert!(plan.total_entries >= 3);
    }

    #[test]
    fn explicit_paths_are_validated_and_nested_candidates_are_collapsed() {
        let temporary = tempfile::tempdir().unwrap();
        fs::create_dir_all(temporary.path().join("cache/node_modules/pkg")).unwrap();
        fs::write(
            temporary.path().join("cache/node_modules/pkg/index.js"),
            b"module",
        )
        .unwrap();

        let plan = discover(temporary.path(), &[PathBuf::from("cache")]).unwrap();
        assert_eq!(plan.candidates.len(), 1);
        assert_eq!(plan.candidates[0].path, "cache");
        assert!(discover(temporary.path(), &[PathBuf::from("../outside")]).is_err());
        assert!(discover(temporary.path(), &[PathBuf::from(".git")]).is_err());

        fs::write(temporary.path().join(".furrowpolicy"), b"exclude cache\n").unwrap();
        assert!(discover(temporary.path(), &[PathBuf::from("cache")])
            .unwrap_err()
            .to_string()
            .contains("undo bytes are not captured"));
    }
}
