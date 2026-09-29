//! Eager, versioned coordination files shared by sibling workspaces.

use crate::store::ObjectStore;
use anyhow::Context;
use fs2::FileExt;
use serde::Serialize;
use std::fs::{self, File, OpenOptions};
use std::io::{Read, Write};
use std::path::{Component, Path, PathBuf};

const MAX_COORD_BYTES: u64 = 1024 * 1024;
const MAX_COORD_FILES: usize = 10_000;
const FAMILY_FILE: &str = ".furrow/family-id";
const COORD_ROOT: &str = ".furrow/coord";

#[derive(Debug, Clone, Serialize)]
pub struct CoordFailure {
    pub workspace: PathBuf,
    pub error: String,
}

#[derive(Debug, Clone, Serialize)]
pub struct CoordPropagation {
    pub path: String,
    pub bytes: u64,
    pub propagated_workspaces: u64,
    pub failures: Vec<CoordFailure>,
}

#[derive(Debug, Clone, Serialize)]
pub struct CoordEntry {
    pub path: String,
    pub bytes: u64,
}

pub fn write(
    root: &Path,
    store: &ObjectStore,
    family_id: &str,
    relative: &Path,
    bytes: &[u8],
) -> anyhow::Result<CoordPropagation> {
    anyhow::ensure!(
        bytes.len() as u64 <= MAX_COORD_BYTES,
        "coord values are limited to 1 MiB"
    );
    let relative = validate_relative(relative)?;
    let _lock = family_lock(store, family_id)?;
    let family = family_root(store, family_id);
    let authority = family.join("coord/files").join(&relative);
    write_safe(&family.join("coord/files"), &authority, bytes)?;
    let tombstone = tombstone_path(&family, &relative);
    if tombstone.exists() {
        fs::remove_file(tombstone)?;
    }
    let report = propagate(store, family_id, &relative, Some(bytes))?;
    ensure_current_materialized(root, &relative, bytes)?;
    Ok(report)
}

pub fn remove(
    root: &Path,
    store: &ObjectStore,
    family_id: &str,
    relative: &Path,
) -> anyhow::Result<CoordPropagation> {
    let relative = validate_relative(relative)?;
    let _lock = family_lock(store, family_id)?;
    let family = family_root(store, family_id);
    let tombstone = tombstone_path(&family, &relative);
    atomic_write(&tombstone, b"")?;
    let authority = family.join("coord/files").join(&relative);
    if fs::symlink_metadata(&authority).is_ok() {
        remove_leaf(&authority)?;
    }
    let report = propagate(store, family_id, &relative, None)?;
    let current = root.join(COORD_ROOT).join(&relative);
    anyhow::ensure!(
        fs::symlink_metadata(current).is_err(),
        "coord deletion did not reach the current workspace"
    );
    Ok(report)
}

pub fn reconcile(root: &Path, store: &ObjectStore, family_id: &str) -> anyhow::Result<u64> {
    let family = family_root(store, family_id);
    if !family.join("coord").is_dir() {
        return Ok(0);
    }
    let _lock = family_lock(store, family_id)?;
    let mut applied = 0_u64;
    let tombstones = family.join("coord/tombstones");
    if tombstones.is_dir() {
        for entry in fs::read_dir(&tombstones)? {
            let entry = entry?;
            if !entry.file_type()?.is_file() {
                continue;
            }
            let encoded = entry.file_name().to_string_lossy().into_owned();
            let bytes = hex::decode(encoded).context("invalid coord tombstone")?;
            let relative = PathBuf::from(String::from_utf8(bytes)?);
            let relative = validate_relative(&relative)?;
            let destination = root.join(COORD_ROOT).join(relative);
            ensure_safe_parent(&root.join(COORD_ROOT), &destination)?;
            if fs::symlink_metadata(&destination).is_ok() {
                remove_leaf(&destination)?;
                applied += 1;
            }
        }
    }
    let files = family.join("coord/files");
    if files.is_dir() {
        let mut authority = Vec::new();
        collect_files(&files, &files, &mut authority)?;
        for (relative, source) in authority {
            let bytes = read_bounded(&source)?;
            let destination = root.join(COORD_ROOT).join(&relative);
            write_safe(&root.join(COORD_ROOT), &destination, &bytes)?;
            applied += 1;
        }
    }
    Ok(applied)
}

pub fn read(root: &Path, relative: &Path) -> anyhow::Result<Vec<u8>> {
    let relative = validate_relative(relative)?;
    let path = root.join(COORD_ROOT).join(relative);
    ensure_safe_parent(&root.join(COORD_ROOT), &path)?;
    read_bounded(&path)
}

pub fn list(root: &Path) -> anyhow::Result<Vec<CoordEntry>> {
    let coord = root.join(COORD_ROOT);
    if !coord.is_dir() {
        return Ok(Vec::new());
    }
    let mut files = Vec::new();
    collect_files(&coord, &coord, &mut files)?;
    let mut entries = Vec::with_capacity(files.len());
    for (relative, path) in files {
        entries.push(CoordEntry {
            path: relative.to_string_lossy().into_owned(),
            bytes: fs::metadata(path)?.len(),
        });
    }
    entries.sort_by(|left, right| left.path.cmp(&right.path));
    Ok(entries)
}

fn propagate(
    store: &ObjectStore,
    family_id: &str,
    relative: &Path,
    bytes: Option<&[u8]>,
) -> anyhow::Result<CoordPropagation> {
    let mut propagated = 0_u64;
    let mut failures = Vec::new();
    for (_, workspace) in store.workspace_roots()? {
        if !workspace.is_dir() || workspace_family(&workspace).as_deref() != Some(family_id) {
            continue;
        }
        let destination = workspace.join(COORD_ROOT).join(relative);
        let result = match bytes {
            Some(bytes) => write_safe(&workspace.join(COORD_ROOT), &destination, bytes),
            None => (|| {
                ensure_safe_parent(&workspace.join(COORD_ROOT), &destination)?;
                if fs::symlink_metadata(&destination).is_ok() {
                    remove_leaf(&destination)
                } else {
                    Ok(())
                }
            })(),
        };
        match result {
            Ok(()) => propagated += 1,
            Err(error) => failures.push(CoordFailure {
                workspace,
                error: error.to_string(),
            }),
        }
    }
    Ok(CoordPropagation {
        path: relative.to_string_lossy().into_owned(),
        bytes: bytes.map_or(0, |bytes| bytes.len() as u64),
        propagated_workspaces: propagated,
        failures,
    })
}

fn workspace_family(root: &Path) -> Option<String> {
    let path = root.join(FAMILY_FILE);
    let metadata = fs::symlink_metadata(&path).ok()?;
    if !metadata.is_file() || metadata.file_type().is_symlink() {
        return None;
    }
    fs::read_to_string(path)
        .ok()
        .map(|value| value.trim().to_owned())
}

fn ensure_current_materialized(
    root: &Path,
    relative: &Path,
    expected: &[u8],
) -> anyhow::Result<()> {
    let actual = read(root, relative)?;
    anyhow::ensure!(
        actual == expected,
        "coord write did not reach the current workspace"
    );
    Ok(())
}

fn family_lock(store: &ObjectStore, family_id: &str) -> anyhow::Result<File> {
    anyhow::ensure!(
        family_id.len() == 32
            && family_id
                .bytes()
                .all(|byte| byte.is_ascii_hexdigit() && !byte.is_ascii_uppercase()),
        "invalid family ID"
    );
    let family = family_root(store, family_id);
    fs::create_dir_all(&family)?;
    let file = OpenOptions::new()
        .create(true)
        .truncate(false)
        .read(true)
        .write(true)
        .open(family.join("coord.lock"))?;
    file.lock_exclusive()?;
    Ok(file)
}

fn family_root(store: &ObjectStore, family_id: &str) -> PathBuf {
    store.root().join("families").join(family_id)
}

fn tombstone_path(family: &Path, relative: &Path) -> PathBuf {
    family
        .join("coord/tombstones")
        .join(hex::encode(relative.to_string_lossy().as_bytes()))
}

fn validate_relative(path: &Path) -> anyhow::Result<PathBuf> {
    anyhow::ensure!(!path.as_os_str().is_empty(), "coord path cannot be empty");
    anyhow::ensure!(!path.is_absolute(), "coord path must be relative");
    let value = path.to_str().context("coord path must be valid UTF-8")?;
    anyhow::ensure!(value.len() <= 1024, "coord path is too long");
    anyhow::ensure!(
        path.components()
            .all(|component| matches!(component, Component::Normal(_))),
        "coord path contains an unsafe component"
    );
    Ok(path.to_owned())
}

fn write_safe(base: &Path, destination: &Path, bytes: &[u8]) -> anyhow::Result<()> {
    ensure_safe_parent(base, destination)?;
    if let Ok(metadata) = fs::symlink_metadata(destination) {
        anyhow::ensure!(
            metadata.is_file() && !metadata.file_type().is_symlink(),
            "coord destination is not a regular file"
        );
    }
    atomic_write(destination, bytes)
}

fn ensure_safe_parent(base: &Path, destination: &Path) -> anyhow::Result<()> {
    fs::create_dir_all(base)?;
    let base_metadata = fs::symlink_metadata(base)?;
    anyhow::ensure!(
        base_metadata.is_dir() && !base_metadata.file_type().is_symlink(),
        "coord root is not a real directory"
    );
    let relative = destination
        .strip_prefix(base)
        .context("coord destination escaped its root")?;
    let mut current = base.to_owned();
    let components: Vec<_> = relative.components().collect();
    for component in components.iter().take(components.len().saturating_sub(1)) {
        let Component::Normal(name) = component else {
            anyhow::bail!("coord path contains an unsafe component")
        };
        current.push(name);
        match fs::symlink_metadata(&current) {
            Ok(metadata) => anyhow::ensure!(
                metadata.is_dir() && !metadata.file_type().is_symlink(),
                "coord parent is not a real directory"
            ),
            Err(error) if error.kind() == std::io::ErrorKind::NotFound => {
                fs::create_dir(&current)?;
            }
            Err(error) => return Err(error.into()),
        }
    }
    Ok(())
}

fn atomic_write(path: &Path, bytes: &[u8]) -> anyhow::Result<()> {
    let parent = path.parent().context("coord file has no parent")?;
    fs::create_dir_all(parent)?;
    let mut temporary = tempfile::NamedTempFile::new_in(parent)?;
    temporary.write_all(bytes)?;
    temporary.as_file().sync_all()?;
    temporary.persist(path).map_err(|error| error.error)?;
    File::open(parent)?.sync_all()?;
    Ok(())
}

fn read_bounded(path: &Path) -> anyhow::Result<Vec<u8>> {
    let metadata = fs::symlink_metadata(path)?;
    anyhow::ensure!(
        metadata.is_file() && !metadata.file_type().is_symlink(),
        "coord value is not a regular file"
    );
    anyhow::ensure!(
        metadata.len() <= MAX_COORD_BYTES,
        "coord value exceeds 1 MiB"
    );
    let mut bytes = Vec::with_capacity(metadata.len() as usize);
    File::open(path)?
        .take(MAX_COORD_BYTES + 1)
        .read_to_end(&mut bytes)?;
    anyhow::ensure!(
        bytes.len() as u64 <= MAX_COORD_BYTES,
        "coord value exceeds 1 MiB"
    );
    Ok(bytes)
}

fn collect_files(
    base: &Path,
    directory: &Path,
    output: &mut Vec<(PathBuf, PathBuf)>,
) -> anyhow::Result<()> {
    for entry in fs::read_dir(directory)? {
        let entry = entry?;
        let file_type = entry.file_type()?;
        anyhow::ensure!(!file_type.is_symlink(), "coord tree contains a symlink");
        if file_type.is_dir() {
            collect_files(base, &entry.path(), output)?;
        } else if file_type.is_file() {
            anyhow::ensure!(
                output.len() < MAX_COORD_FILES,
                "coord tree exceeds 10,000 files"
            );
            output.push((entry.path().strip_prefix(base)?.to_owned(), entry.path()));
        }
    }
    Ok(())
}

fn remove_leaf(path: &Path) -> anyhow::Result<()> {
    let metadata = fs::symlink_metadata(path)?;
    if metadata.is_dir() && !metadata.file_type().is_symlink() {
        anyhow::bail!("coord leaf is a directory")
    }
    fs::remove_file(path)?;
    Ok(())
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn validates_paths_and_refuses_symlink_parents() {
        assert!(validate_relative(Path::new("tasks/alpha.md")).is_ok());
        assert!(validate_relative(Path::new("../secret")).is_err());
        let temporary = tempfile::tempdir().unwrap();
        let outside = tempfile::tempdir().unwrap();
        fs::create_dir_all(temporary.path().join("coord")).unwrap();
        std::os::unix::fs::symlink(outside.path(), temporary.path().join("coord/link")).unwrap();
        assert!(write_safe(
            &temporary.path().join("coord"),
            &temporary.path().join("coord/link/value"),
            b"blocked"
        )
        .is_err());
    }
}
