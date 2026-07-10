//! Cross-process suppression of filesystem events produced by snapshot apply.

use anyhow::Context;
use serde::{Deserialize, Serialize};
use std::collections::{BTreeMap, BTreeSet};
use std::fs;
use std::os::unix::ffi::OsStrExt;
use std::os::unix::fs::MetadataExt;
use std::path::{Path, PathBuf};

const ACTIVE_FILE: &str = "apply-active.json";
const BASELINE_FILE: &str = "apply-baseline.json";

#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub enum FilterResult {
    Ready,
    ApplyActive,
}

#[derive(Serialize, Deserialize)]
struct ActiveApply {
    pid: u32,
}

#[derive(Debug, Clone, Serialize, Deserialize, PartialEq, Eq)]
struct FileIdentity {
    size: u64,
    mtime_secs: i64,
    mtime_nanos: i64,
    inode: u64,
}

#[derive(Serialize, Deserialize)]
struct BaselineEntry {
    #[serde(with = "serde_bytes")]
    path: Vec<u8>,
    identity: Option<FileIdentity>,
}

/// Marks an apply before its first workspace mutation. Finishing the guard
/// records post-write identities without reading file contents.
pub struct ApplyGuard {
    state_dir: PathBuf,
    root: PathBuf,
    paths: Vec<PathBuf>,
    finished: bool,
}

impl ApplyGuard {
    pub fn begin(
        state_dir: &Path,
        root: &Path,
        paths: impl IntoIterator<Item = PathBuf>,
    ) -> anyhow::Result<Self> {
        fs::create_dir_all(state_dir)?;
        atomic_write(
            &state_dir.join(ACTIVE_FILE),
            &serde_json::to_vec(&ActiveApply {
                pid: std::process::id(),
            })?,
        )?;
        Ok(Self {
            state_dir: state_dir.to_owned(),
            root: root.to_owned(),
            paths: paths.into_iter().collect(),
            finished: false,
        })
    }

    pub fn finish(mut self) -> anyhow::Result<()> {
        let mut entries = Vec::with_capacity(self.paths.len());
        for path in &self.paths {
            let relative = if path.is_absolute() {
                path.strip_prefix(&self.root).with_context(|| {
                    format!("applied path is outside workspace: {}", path.display())
                })?
            } else {
                path.as_path()
            };
            entries.push(BaselineEntry {
                path: relative.as_os_str().as_bytes().to_vec(),
                identity: identity(&self.root.join(relative))?,
            });
        }
        atomic_write(
            &self.state_dir.join(BASELINE_FILE),
            &serde_json::to_vec(&entries)?,
        )?;
        remove_if_exists(&self.state_dir.join(ACTIVE_FILE))?;
        self.finished = true;
        Ok(())
    }
}

impl Drop for ApplyGuard {
    fn drop(&mut self) {
        if !self.finished {
            let _ = remove_if_exists(&self.state_dir.join(ACTIVE_FILE));
        }
    }
}

/// Removes events that exactly match the baseline installed by the applier.
/// Events remain queued while a live apply is active, closing the race between
/// native notification delivery and post-write baseline installation.
pub fn filter_events(
    state_dir: &Path,
    root: &Path,
    paths: &mut BTreeSet<PathBuf>,
) -> anyhow::Result<FilterResult> {
    let active_path = state_dir.join(ACTIVE_FILE);
    if active_path.exists() {
        let active: ActiveApply = serde_json::from_slice(&fs::read(&active_path)?)?;
        if process_alive(active.pid) {
            return Ok(FilterResult::ApplyActive);
        }
        remove_if_exists(&active_path)?;
    }

    let baseline_path = state_dir.join(BASELINE_FILE);
    let entries: Vec<BaselineEntry> = match fs::read(&baseline_path) {
        Ok(bytes) => serde_json::from_slice(&bytes).context("decode apply watcher baseline")?,
        Err(error) if error.kind() == std::io::ErrorKind::NotFound => {
            return Ok(FilterResult::Ready)
        }
        Err(error) => return Err(error.into()),
    };
    let baseline: BTreeMap<_, _> = entries
        .into_iter()
        .map(|entry| (entry.path, entry.identity))
        .collect();
    paths.retain(|path| {
        let relative = if path.is_absolute() {
            match path.strip_prefix(root) {
                Ok(path) => path,
                Err(_) => return true,
            }
        } else {
            path.as_path()
        };
        let key = relative.as_os_str().as_bytes();
        match baseline.get(key) {
            Some(expected) => identity(&root.join(relative)).as_ref().ok() != Some(expected),
            None => true,
        }
    });
    Ok(FilterResult::Ready)
}

fn identity(path: &Path) -> anyhow::Result<Option<FileIdentity>> {
    match fs::symlink_metadata(path) {
        Ok(metadata) => Ok(Some(FileIdentity {
            size: metadata.len(),
            mtime_secs: metadata.mtime(),
            mtime_nanos: metadata.mtime_nsec(),
            inode: metadata.ino(),
        })),
        Err(error) if error.kind() == std::io::ErrorKind::NotFound => Ok(None),
        Err(error) => Err(error.into()),
    }
}

fn process_alive(pid: u32) -> bool {
    let result = unsafe { libc::kill(pid as i32, 0) };
    result == 0 || std::io::Error::last_os_error().raw_os_error() == Some(libc::EPERM)
}

fn remove_if_exists(path: &Path) -> anyhow::Result<()> {
    match fs::remove_file(path) {
        Ok(()) => Ok(()),
        Err(error) if error.kind() == std::io::ErrorKind::NotFound => Ok(()),
        Err(error) => Err(error.into()),
    }
}

fn atomic_write(path: &Path, bytes: &[u8]) -> anyhow::Result<()> {
    let parent = path.parent().context("self-write state has no parent")?;
    let mut temporary = tempfile::NamedTempFile::new_in(parent)?;
    use std::io::Write;
    temporary.write_all(bytes)?;
    temporary.persist(path).map_err(|error| error.error)?;
    Ok(())
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn suppresses_only_unchanged_applier_paths() {
        let temporary = tempfile::tempdir().unwrap();
        let root = temporary.path().join("root");
        let state = temporary.path().join("state");
        fs::create_dir(&root).unwrap();
        fs::write(root.join("applied"), b"incoming").unwrap();
        fs::write(root.join("user"), b"local").unwrap();

        ApplyGuard::begin(&state, &root, [PathBuf::from("applied")])
            .unwrap()
            .finish()
            .unwrap();
        let mut events = BTreeSet::from([root.join("applied"), root.join("user")]);
        assert_eq!(
            filter_events(&state, &root, &mut events).unwrap(),
            FilterResult::Ready
        );
        assert_eq!(events, BTreeSet::from([root.join("user")]));

        fs::write(root.join("applied"), b"user changed this afterward").unwrap();
        events.insert(root.join("applied"));
        filter_events(&state, &root, &mut events).unwrap();
        assert!(events.contains(&root.join("applied")));
    }

    #[test]
    fn defers_events_until_apply_finishes_and_handles_removal() {
        let temporary = tempfile::tempdir().unwrap();
        let root = temporary.path().join("root");
        let state = temporary.path().join("state");
        fs::create_dir(&root).unwrap();
        fs::write(root.join("removed"), b"old").unwrap();
        let guard = ApplyGuard::begin(&state, &root, [PathBuf::from("removed")]).unwrap();
        fs::remove_file(root.join("removed")).unwrap();
        let mut events = BTreeSet::from([root.join("removed")]);
        assert_eq!(
            filter_events(&state, &root, &mut events).unwrap(),
            FilterResult::ApplyActive
        );
        guard.finish().unwrap();
        assert_eq!(
            filter_events(&state, &root, &mut events).unwrap(),
            FilterResult::Ready
        );
        assert!(events.is_empty());
    }
}
