//! Fast, full-state workspace forks for macOS and Linux.
//!
//! Regular files use the platform's copy-on-write primitive when available and
//! transparently fall back to a streaming copy. The destination is assembled in
//! a sibling staging directory and published with one rename, so callers never
//! observe a partial fork.

use anyhow::{bail, Context};
use filetime::{set_file_times, set_symlink_file_times, FileTime};
use serde::{Deserialize, Serialize};
use std::collections::HashMap;
use std::ffi::OsString;
use std::fmt;
use std::fs::{self, File, Metadata, OpenOptions, ReadDir};
use std::io::{self, Seek, SeekFrom};
#[cfg(target_os = "linux")]
use std::os::fd::AsRawFd;
use std::os::unix::ffi::OsStrExt;
use std::os::unix::fs::{symlink, FileTypeExt, MetadataExt, PermissionsExt};
use std::path::{Path, PathBuf};
use std::sync::atomic::{AtomicU64, Ordering};
use std::time::{Duration, Instant};

static STAGING_SEQUENCE: AtomicU64 = AtomicU64::new(0);

/// The least efficient file-copy mechanism used by a fork.
#[derive(Clone, Copy, Debug, Deserialize, Eq, PartialEq, Serialize)]
#[serde(rename_all = "kebab-case")]
pub enum ForkTier {
    /// Every unique regular file was created with the native CoW primitive.
    NativeCow,
    /// Some files used CoW and some required a byte copy.
    Mixed,
    /// The filesystem did not support CoW and files were copied conventionally.
    StreamingCopy,
}

impl fmt::Display for ForkTier {
    fn fmt(&self, formatter: &mut fmt::Formatter<'_>) -> fmt::Result {
        match self {
            Self::NativeCow => formatter.write_str("native-cow"),
            Self::Mixed => formatter.write_str("mixed"),
            Self::StreamingCopy => formatter.write_str("streaming-copy"),
        }
    }
}

/// Work performed while constructing a workspace fork.
#[derive(Clone, Debug, Eq, PartialEq)]
pub struct ForkReport {
    pub tier: ForkTier,
    /// Regular-file directory entries, including hard-link aliases.
    pub files: u64,
    /// Directories, including the workspace root.
    pub directories: u64,
    pub symlinks: u64,
    pub fifos: u64,
    /// Runtime-only sockets and unsupported device nodes intentionally omitted.
    pub skipped_special: u64,
    /// Sum of file lengths as visible through every directory entry.
    pub logical_bytes: u64,
    /// Logical bytes backed by a newly created CoW clone.
    pub cloned_bytes: u64,
    /// Bytes physically read and written by the fallback path.
    pub copied_bytes: u64,
    /// File entries recreated by linking to an inode already forked.
    pub hardlinked_files: u64,
    pub elapsed: Duration,
    /// The platform cloned the hierarchy in one atomic operation.
    pub atomic_hierarchy: bool,
}

#[derive(Default)]
struct Counters {
    files: u64,
    directories: u64,
    symlinks: u64,
    fifos: u64,
    skipped_special: u64,
    logical_bytes: u64,
    cloned_bytes: u64,
    copied_bytes: u64,
    hardlinked_files: u64,
    cloned_files: u64,
    copied_files: u64,
}

#[derive(Clone, Copy, Debug, Eq, Hash, PartialEq)]
struct Inode {
    device: u64,
    number: u64,
}

struct DirectoryFrame {
    source: PathBuf,
    destination: PathBuf,
    source_metadata: Metadata,
    entries: ReadDir,
}

struct StagingDirectory {
    path: PathBuf,
    armed: bool,
}

impl Drop for StagingDirectory {
    fn drop(&mut self) {
        if self.armed {
            let _ = fs::remove_dir_all(&self.path);
        }
    }
}

/// Fork `source` into a new `destination` directory.
///
/// The operation preserves directories, symlinks, executable and permission
/// modes, timestamps, extended attributes, and hard-link relationships. Memory
/// usage is independent of file size: traversal retains one frame per directory
/// depth and one small map entry per multi-linked inode.
pub fn fork_workspace(
    source: impl AsRef<Path>,
    destination: impl AsRef<Path>,
) -> anyhow::Result<ForkReport> {
    fork_workspace_excluding(source, destination, &[])
}

pub(crate) fn fork_workspace_excluding(
    source: impl AsRef<Path>,
    destination: impl AsRef<Path>,
    excluded: &[&Path],
) -> anyhow::Result<ForkReport> {
    let started = Instant::now();
    let source = source.as_ref();
    let destination = destination.as_ref();

    let source = source
        .canonicalize()
        .with_context(|| format!("resolve fork source {}", source.display()))?;
    let source_metadata = fs::symlink_metadata(&source)
        .with_context(|| format!("stat fork source {}", source.display()))?;
    if !source_metadata.is_dir() || source_metadata.file_type().is_symlink() {
        bail!("fork source must be a directory: {}", source.display());
    }
    if destination.exists() || fs::symlink_metadata(destination).is_ok() {
        bail!("fork destination already exists: {}", destination.display());
    }

    let destination_name = destination
        .file_name()
        .filter(|name| !name.is_empty())
        .context("fork destination must have a final path component")?;
    let destination_parent = destination
        .parent()
        .unwrap_or_else(|| Path::new("."))
        .canonicalize()
        .with_context(|| format!("resolve destination parent for {}", destination.display()))?;
    let destination = destination_parent.join(destination_name);
    if destination.starts_with(&source) || source.starts_with(&destination) {
        bail!(
            "fork source and destination must not overlap: {} and {}",
            source.display(),
            destination.display()
        );
    }

    let staging_path = unique_staging_path(&destination_parent, destination_name);
    #[cfg(target_os = "macos")]
    if let Some(report) = try_atomic_hierarchy_clone(
        &source,
        &staging_path,
        &destination,
        &destination_parent,
        excluded,
        started,
    )? {
        return Ok(report);
    }
    fs::create_dir(&staging_path)
        .with_context(|| format!("create fork staging directory {}", staging_path.display()))?;
    let mut staging = StagingDirectory {
        path: staging_path.clone(),
        armed: true,
    };

    let mut counters = Counters {
        directories: 1,
        ..Counters::default()
    };
    let mut hardlinks = HashMap::<Inode, PathBuf>::new();
    let root_entries =
        fs::read_dir(&source).with_context(|| format!("read fork source {}", source.display()))?;
    let mut directories = vec![DirectoryFrame {
        source: source.clone(),
        destination: staging_path.clone(),
        source_metadata,
        entries: root_entries,
    }];

    while !directories.is_empty() {
        let next = directories
            .last_mut()
            .expect("directory stack is nonempty")
            .entries
            .next();
        let Some(entry) = next else {
            let completed = directories.pop().expect("directory stack is nonempty");
            apply_metadata(
                &completed.source,
                &completed.destination,
                &completed.source_metadata,
                false,
            )?;
            continue;
        };

        let entry = entry.with_context(|| {
            let directory = &directories
                .last()
                .expect("directory stack is nonempty")
                .source;
            format!("read directory entry in {}", directory.display())
        })?;
        let source_path = entry.path();
        let relative = source_path.strip_prefix(&source)?;
        if excluded.iter().any(|excluded| relative == *excluded) {
            continue;
        }
        let destination_path = directories
            .last()
            .expect("directory stack is nonempty")
            .destination
            .join(entry.file_name());
        let metadata = fs::symlink_metadata(&source_path)
            .with_context(|| format!("stat {}", source_path.display()))?;
        let file_type = metadata.file_type();

        if file_type.is_dir() {
            fs::create_dir(&destination_path)
                .with_context(|| format!("create directory {}", destination_path.display()))?;
            let entries = fs::read_dir(&source_path)
                .with_context(|| format!("read directory {}", source_path.display()))?;
            counters.directories += 1;
            directories.push(DirectoryFrame {
                source: source_path,
                destination: destination_path,
                source_metadata: metadata,
                entries,
            });
        } else if file_type.is_file() {
            counters.files += 1;
            counters.logical_bytes = counters.logical_bytes.saturating_add(metadata.len());
            let inode = Inode {
                device: metadata.dev(),
                number: metadata.ino(),
            };

            if metadata.nlink() > 1 {
                if let Some(existing) = hardlinks.get(&inode) {
                    fs::hard_link(existing, &destination_path).with_context(|| {
                        format!(
                            "recreate hard link {} -> {}",
                            destination_path.display(),
                            existing.display()
                        )
                    })?;
                    counters.hardlinked_files += 1;
                    continue;
                }
            }

            let method = clone_or_copy(&source_path, &destination_path)
                .with_context(|| format!("fork file {}", source_path.display()))?;
            match method {
                FileMethod::NativeCow => {
                    counters.cloned_files += 1;
                    counters.cloned_bytes = counters.cloned_bytes.saturating_add(metadata.len());
                }
                FileMethod::StreamingCopy(bytes) => {
                    counters.copied_files += 1;
                    counters.copied_bytes = counters.copied_bytes.saturating_add(bytes);
                }
            }
            apply_metadata(&source_path, &destination_path, &metadata, false)?;
            if metadata.nlink() > 1 {
                hardlinks.insert(inode, destination_path);
            }
        } else if file_type.is_symlink() {
            let target = fs::read_link(&source_path)
                .with_context(|| format!("read symlink {}", source_path.display()))?;
            symlink(&target, &destination_path).with_context(|| {
                format!(
                    "create symlink {} -> {}",
                    destination_path.display(),
                    target.display()
                )
            })?;
            apply_metadata(&source_path, &destination_path, &metadata, true)?;
            counters.symlinks += 1;
        } else if file_type.is_fifo() {
            create_fifo(&destination_path, metadata.permissions().mode())
                .with_context(|| format!("create FIFO {}", destination_path.display()))?;
            apply_metadata(&source_path, &destination_path, &metadata, false)?;
            counters.fifos += 1;
        } else {
            counters.skipped_special += 1;
        }
    }

    fs::rename(&staging_path, &destination).with_context(|| {
        format!(
            "publish fork {} as {}",
            staging_path.display(),
            destination.display()
        )
    })?;
    staging.armed = false;

    let tier = match (counters.cloned_files, counters.copied_files) {
        (_, 0) => ForkTier::NativeCow,
        (0, _) => ForkTier::StreamingCopy,
        _ => ForkTier::Mixed,
    };
    Ok(ForkReport {
        tier,
        files: counters.files,
        directories: counters.directories,
        symlinks: counters.symlinks,
        fifos: counters.fifos,
        skipped_special: counters.skipped_special,
        logical_bytes: counters.logical_bytes,
        cloned_bytes: counters.cloned_bytes,
        copied_bytes: counters.copied_bytes,
        hardlinked_files: counters.hardlinked_files,
        elapsed: started.elapsed(),
        atomic_hierarchy: false,
    })
}

#[cfg(target_os = "macos")]
fn try_atomic_hierarchy_clone(
    source: &Path,
    staging_path: &Path,
    destination: &Path,
    destination_parent: &Path,
    excluded: &[&Path],
    started: Instant,
) -> anyhow::Result<Option<ForkReport>> {
    match native_clone(source, staging_path) {
        Ok(()) => {}
        Err(_) => {
            if staging_path.exists() {
                fs::remove_dir_all(staging_path)?;
            }
            return Ok(None);
        }
    }
    let mut staging = StagingDirectory {
        path: staging_path.to_owned(),
        armed: true,
    };
    for relative in excluded {
        anyhow::ensure!(
            !relative.is_absolute()
                && relative
                    .components()
                    .all(|component| matches!(component, std::path::Component::Normal(_))),
            "fork exclusion must be a safe relative path"
        );
        let path = staging_path.join(relative);
        let Ok(metadata) = fs::symlink_metadata(&path) else {
            continue;
        };
        if metadata.is_dir() && !metadata.file_type().is_symlink() {
            fs::remove_dir_all(path)?;
        } else {
            fs::remove_file(path)?;
        }
    }
    repair_directory_metadata(source, staging_path, excluded)?;
    let (counters, source_has_hardlinks) = measure_cloned_hierarchy(source, staging_path)?;
    if source_has_hardlinks || counters.skipped_special > 0 {
        return Ok(None);
    }
    fs::rename(staging_path, destination)
        .with_context(|| format!("publish fork {}", destination.display()))?;
    File::open(destination_parent)?.sync_all()?;
    staging.armed = false;
    Ok(Some(ForkReport {
        tier: ForkTier::NativeCow,
        files: counters.files,
        directories: counters.directories,
        symlinks: counters.symlinks,
        fifos: counters.fifos,
        skipped_special: counters.skipped_special,
        logical_bytes: counters.logical_bytes,
        cloned_bytes: counters.logical_bytes,
        copied_bytes: 0,
        hardlinked_files: counters.hardlinked_files,
        elapsed: started.elapsed(),
        atomic_hierarchy: true,
    }))
}

#[cfg(target_os = "macos")]
fn repair_directory_metadata(
    source: &Path,
    destination: &Path,
    excluded: &[&Path],
) -> anyhow::Result<()> {
    struct Frame {
        source: PathBuf,
        destination: PathBuf,
        metadata: Metadata,
        entries: ReadDir,
    }

    let metadata = fs::symlink_metadata(source)?;
    let mut stack = vec![Frame {
        source: source.to_owned(),
        destination: destination.to_owned(),
        metadata,
        entries: fs::read_dir(source)?,
    }];
    while let Some(frame) = stack.last_mut() {
        let Some(entry) = frame.entries.next() else {
            let completed = stack.pop().expect("directory stack is nonempty");
            apply_metadata(
                &completed.source,
                &completed.destination,
                &completed.metadata,
                false,
            )?;
            continue;
        };
        let entry = entry?;
        let metadata = fs::symlink_metadata(entry.path())?;
        if metadata.is_dir() && !metadata.file_type().is_symlink() {
            let relative = entry.path().strip_prefix(source)?.to_owned();
            if excluded.iter().any(|excluded| relative == *excluded) {
                continue;
            }
            let destination = destination.join(relative);
            stack.push(Frame {
                source: entry.path(),
                destination,
                metadata,
                entries: fs::read_dir(entry.path())?,
            });
        }
    }
    Ok(())
}

#[cfg(target_os = "macos")]
fn measure_cloned_hierarchy(source: &Path, root: &Path) -> anyhow::Result<(Counters, bool)> {
    let mut counters = Counters {
        directories: 1,
        ..Counters::default()
    };
    let mut directories = vec![fs::read_dir(root)?];
    let mut hardlinks = std::collections::HashSet::<Inode>::new();
    let mut source_has_hardlinks = false;
    while let Some(entries) = directories.last_mut() {
        let Some(entry) = entries.next() else {
            directories.pop();
            continue;
        };
        let entry = entry?;
        let metadata = fs::symlink_metadata(entry.path())?;
        let file_type = metadata.file_type();
        if file_type.is_dir() {
            counters.directories += 1;
            directories.push(fs::read_dir(entry.path())?);
        } else if file_type.is_file() {
            counters.files += 1;
            counters.logical_bytes = counters.logical_bytes.saturating_add(metadata.len());
            if metadata.nlink() > 1
                && !hardlinks.insert(Inode {
                    device: metadata.dev(),
                    number: metadata.ino(),
                })
            {
                counters.hardlinked_files += 1;
            }
            let relative = entry.path().strip_prefix(root)?.to_owned();
            if fs::symlink_metadata(source.join(relative))?.nlink() > 1 {
                source_has_hardlinks = true;
            }
        } else if file_type.is_symlink() {
            counters.symlinks += 1;
        } else if file_type.is_fifo() {
            counters.fifos += 1;
        } else {
            counters.skipped_special += 1;
            fs::remove_file(entry.path())?;
        }
    }
    Ok((counters, source_has_hardlinks))
}

fn create_fifo(path: &Path, mode: u32) -> io::Result<()> {
    use std::ffi::CString;

    let path = CString::new(path.as_os_str().as_bytes())
        .map_err(|_| io::Error::new(io::ErrorKind::InvalidInput, "FIFO path contains NUL"))?;
    // SAFETY: the C string is valid for the call and mkfifo does not retain it.
    if unsafe { libc::mkfifo(path.as_ptr(), mode as libc::mode_t) } == 0 {
        Ok(())
    } else {
        Err(io::Error::last_os_error())
    }
}

fn unique_staging_path(parent: &Path, destination_name: &std::ffi::OsStr) -> PathBuf {
    loop {
        let sequence = STAGING_SEQUENCE.fetch_add(1, Ordering::Relaxed);
        let mut name = OsString::from(".");
        name.push(destination_name);
        name.push(format!(".agit-fork-{}-{sequence}", std::process::id()));
        let candidate = parent.join(name);
        if fs::symlink_metadata(&candidate).is_err() {
            return candidate;
        }
    }
}

#[derive(Clone, Copy, Debug, Eq, PartialEq)]
enum FileMethod {
    NativeCow,
    StreamingCopy(u64),
}

fn clone_or_copy(source: &Path, destination: &Path) -> io::Result<FileMethod> {
    match native_clone(source, destination) {
        Ok(()) => Ok(FileMethod::NativeCow),
        Err(_) => {
            // clonefile may leave a destination behind for some error classes.
            match fs::remove_file(destination) {
                Ok(()) => {}
                Err(error) if error.kind() == io::ErrorKind::NotFound => {}
                Err(error) => return Err(error),
            }
            streaming_copy(source, destination).map(FileMethod::StreamingCopy)
        }
    }
}

#[cfg(target_os = "macos")]
fn native_clone(source: &Path, destination: &Path) -> io::Result<()> {
    use std::ffi::CString;

    let source = CString::new(source.as_os_str().as_bytes())
        .map_err(|_| io::Error::new(io::ErrorKind::InvalidInput, "source path contains NUL"))?;
    let destination = CString::new(destination.as_os_str().as_bytes()).map_err(|_| {
        io::Error::new(io::ErrorKind::InvalidInput, "destination path contains NUL")
    })?;
    // SAFETY: both C strings live for the call and clonefile does not retain them.
    let result = unsafe { libc::clonefile(source.as_ptr(), destination.as_ptr(), 0) };
    if result == 0 {
        Ok(())
    } else {
        Err(io::Error::last_os_error())
    }
}

#[cfg(target_os = "linux")]
fn native_clone(source: &Path, destination: &Path) -> io::Result<()> {
    let source = File::open(source)?;
    let destination = OpenOptions::new()
        .write(true)
        .create_new(true)
        .open(destination)?;
    // SAFETY: both descriptors are valid for the duration of the ioctl. FICLONE
    // does not retain either descriptor.
    let result = unsafe {
        libc::ioctl(
            destination.as_raw_fd(),
            libc::FICLONE as libc::c_ulong,
            source.as_raw_fd(),
        )
    };
    if result == 0 {
        Ok(())
    } else {
        Err(io::Error::last_os_error())
    }
}

#[cfg(not(any(target_os = "macos", target_os = "linux")))]
fn native_clone(_source: &Path, _destination: &Path) -> io::Result<()> {
    Err(io::Error::new(
        io::ErrorKind::Unsupported,
        "native CoW cloning is only supported on macOS and Linux",
    ))
}

fn streaming_copy(source: &Path, destination: &Path) -> io::Result<u64> {
    let mut destination = OpenOptions::new()
        .write(true)
        .create_new(true)
        .open(destination)?;
    let mut last_length = 0;

    for _ in 0..3 {
        let mut source = File::open(source)?;
        let before = source.metadata()?;
        destination.set_len(0)?;
        destination.seek(SeekFrom::Start(0))?;
        last_length = io::copy(&mut source, &mut destination)?;
        let after = source.metadata()?;
        if stable_file(&before, &after) && last_length == after.len() {
            return Ok(last_length);
        }
    }

    Err(io::Error::new(
        io::ErrorKind::Other,
        format!("source changed repeatedly while copying ({last_length} bytes in last attempt)"),
    ))
}

fn stable_file(before: &Metadata, after: &Metadata) -> bool {
    before.dev() == after.dev()
        && before.ino() == after.ino()
        && before.len() == after.len()
        && before.mtime() == after.mtime()
        && before.mtime_nsec() == after.mtime_nsec()
}

fn apply_metadata(
    source: &Path,
    destination: &Path,
    metadata: &Metadata,
    symlink_path: bool,
) -> anyhow::Result<()> {
    copy_xattrs(source, destination)?;
    let accessed = FileTime::from_last_access_time(metadata);
    let modified = FileTime::from_last_modification_time(metadata);
    if symlink_path {
        set_symlink_file_times(destination, accessed, modified)
            .with_context(|| format!("set symlink times on {}", destination.display()))?;
    } else {
        fs::set_permissions(
            destination,
            fs::Permissions::from_mode(metadata.permissions().mode()),
        )
        .with_context(|| format!("set mode on {}", destination.display()))?;
        set_file_times(destination, accessed, modified)
            .with_context(|| format!("set times on {}", destination.display()))?;
    }
    Ok(())
}

fn copy_xattrs(source: &Path, destination: &Path) -> anyhow::Result<()> {
    let names = xattr::list(source)
        .with_context(|| format!("list extended attributes on {}", source.display()))?;
    for name in names {
        if let Some(value) = xattr::get(source, &name).with_context(|| {
            format!("read extended attribute {:?} on {}", name, source.display())
        })? {
            if xattr::get(destination, &name).with_context(|| {
                format!(
                    "read existing extended attribute {:?} on {}",
                    name,
                    destination.display()
                )
            })? == Some(value.clone())
            {
                continue;
            }
            xattr::set(destination, &name, &value).with_context(|| {
                format!(
                    "set extended attribute {:?} on {}",
                    name,
                    destination.display()
                )
            })?;
        }
    }
    Ok(())
}

#[cfg(test)]
mod tests {
    use super::*;
    use std::os::unix::fs::PermissionsExt;

    #[test]
    fn forks_files_links_modes_and_xattrs() {
        let temporary = tempfile::tempdir().unwrap();
        let source = temporary.path().join("source");
        let destination = temporary.path().join("fork");
        fs::create_dir(&source).unwrap();
        fs::create_dir(source.join("nested")).unwrap();
        fs::write(source.join("nested/tool"), b"#!/bin/sh\necho fork\n").unwrap();
        fs::set_permissions(
            source.join("nested/tool"),
            fs::Permissions::from_mode(0o751),
        )
        .unwrap();
        fs::hard_link(source.join("nested/tool"), source.join("tool-alias")).unwrap();
        symlink("nested/tool", source.join("tool-link")).unwrap();
        xattr::set(source.join("nested/tool"), "user.agit-fork-test", b"kept").unwrap();

        let report = fork_workspace(&source, &destination).unwrap();

        assert_eq!(
            fs::read(destination.join("nested/tool")).unwrap(),
            b"#!/bin/sh\necho fork\n"
        );
        assert_eq!(
            fs::read_link(destination.join("tool-link")).unwrap(),
            Path::new("nested/tool")
        );
        assert_eq!(
            fs::metadata(destination.join("nested/tool"))
                .unwrap()
                .permissions()
                .mode()
                & 0o777,
            0o751
        );
        assert_eq!(
            xattr::get(destination.join("nested/tool"), "user.agit-fork-test").unwrap(),
            Some(b"kept".to_vec())
        );
        assert_eq!(
            fs::metadata(destination.join("nested/tool")).unwrap().ino(),
            fs::metadata(destination.join("tool-alias")).unwrap().ino()
        );
        assert_eq!(report.files, 2);
        assert_eq!(report.directories, 2);
        assert_eq!(report.symlinks, 1);
        assert_eq!(report.hardlinked_files, 1);
    }

    #[test]
    fn rejects_existing_or_nested_destinations_without_mutation() {
        let temporary = tempfile::tempdir().unwrap();
        let source = temporary.path().join("source");
        fs::create_dir(&source).unwrap();
        fs::write(source.join("kept"), b"data").unwrap();

        let nested = source.join("nested");
        let error = fork_workspace(&source, &nested).unwrap_err();
        assert!(error.to_string().contains("must not overlap"));
        assert!(!nested.exists());

        let existing = temporary.path().join("existing");
        fs::create_dir(&existing).unwrap();
        let error = fork_workspace(&source, &existing).unwrap_err();
        assert!(error.to_string().contains("already exists"));
        assert!(source.join("kept").exists());
    }

    #[cfg(target_os = "macos")]
    #[test]
    fn atomic_hierarchy_fast_path_preserves_directory_metadata() {
        let temporary = tempfile::tempdir().unwrap();
        let source = temporary.path().join("source");
        let destination = temporary.path().join("destination");
        fs::create_dir_all(source.join("nested/deep")).unwrap();
        fs::write(source.join("nested/deep/file"), b"atomic clone").unwrap();
        let report = fork_workspace(&source, &destination).unwrap();
        assert!(report.atomic_hierarchy);
        assert_eq!(
            fs::metadata(source.join("nested")).unwrap().mtime_nsec(),
            fs::metadata(destination.join("nested"))
                .unwrap()
                .mtime_nsec()
        );
        assert_eq!(
            fs::read(destination.join("nested/deep/file")).unwrap(),
            b"atomic clone"
        );
    }
}
