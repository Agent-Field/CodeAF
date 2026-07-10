//! Policy-aware, disk-backed onboarding cost estimation.

use crate::chunker::ChunkStream;
use crate::model::ObjectKind;
use crate::policy::CapturePolicy;
use crate::store::{object_id, ObjectStore};
use anyhow::Context;
use rusqlite::{params, Connection, Transaction};
use serde::Serialize;
use std::fs::{self, File, ReadDir};
use std::io::BufReader;
use std::os::unix::ffi::OsStrExt;
use std::os::unix::fs::MetadataExt;
use std::path::{Path, PathBuf};

const MAX_DEPTH: usize = 1_024;

#[derive(Debug, Clone, Serialize)]
pub struct CaptureEstimate {
    pub files: u64,
    pub directories: u64,
    pub symlinks: u64,
    pub special_entries: u64,
    pub excluded_subtrees: u64,
    pub policy_rules: usize,
    pub logical_bytes: u64,
    pub physical_bytes: u64,
    pub unique_chunks: u64,
    pub deduplicated_chunk_bytes: u64,
    pub projected_new_chunk_bytes: u64,
}

pub fn calculate(root: &Path, store: &ObjectStore) -> anyhow::Result<CaptureEstimate> {
    let root = root.canonicalize()?;
    let policy = CapturePolicy::load(&root)?;
    let database = tempfile::NamedTempFile::new()?;
    let mut connection = Connection::open(database.path())?;
    connection.execute_batch(
        "PRAGMA journal_mode=OFF;
         PRAGMA synchronous=OFF;
         PRAGMA temp_store=FILE;
         CREATE TABLE chunks (
            id BLOB PRIMARY KEY,
            len INTEGER NOT NULL,
            present INTEGER NOT NULL
         ) WITHOUT ROWID;",
    )?;
    let transaction = connection.transaction()?;
    let mut estimate = CaptureEstimate {
        files: 0,
        directories: 1,
        symlinks: 0,
        special_entries: 0,
        excluded_subtrees: 0,
        policy_rules: policy.rules().count(),
        logical_bytes: 0,
        physical_bytes: 0,
        unique_chunks: 0,
        deduplicated_chunk_bytes: 0,
        projected_new_chunk_bytes: 0,
    };
    let mut stack = vec![Frame::open(root.clone())?];
    let mut savepoint = 0_u64;
    while !stack.is_empty() {
        let next = stack.last_mut().expect("stack is not empty").entries.next();
        let Some(entry) = next else {
            stack.pop();
            continue;
        };
        let entry = entry?;
        let path = entry.path();
        let relative = path.strip_prefix(&root)?.as_os_str().as_bytes();
        if relative == b".furrow/workspace-id" {
            continue;
        }
        if policy.excludes_bytes(relative) {
            estimate.excluded_subtrees = estimate.excluded_subtrees.saturating_add(1);
            continue;
        }
        let metadata = fs::symlink_metadata(&path)?;
        let file_type = metadata.file_type();
        if file_type.is_dir() && !file_type.is_symlink() {
            estimate.directories = estimate.directories.saturating_add(1);
            anyhow::ensure!(stack.len() < MAX_DEPTH, "workspace tree exceeds safe depth");
            stack.push(Frame::open(path)?);
        } else if file_type.is_file() {
            let usage = estimate_file(&transaction, store, &path, &mut savepoint)?;
            estimate.files = estimate.files.saturating_add(1);
            estimate.logical_bytes = estimate.logical_bytes.saturating_add(usage.logical_bytes);
            estimate.physical_bytes = estimate.physical_bytes.saturating_add(usage.physical_bytes);
        } else if file_type.is_symlink() {
            estimate.symlinks = estimate.symlinks.saturating_add(1);
        } else {
            estimate.special_entries = estimate.special_entries.saturating_add(1);
        }
    }
    transaction.commit()?;
    let (unique_chunks, deduplicated, projected): (i64, i64, i64) = connection.query_row(
        "SELECT COUNT(*),
                COALESCE(SUM(CASE WHEN present = 1 THEN len ELSE 0 END), 0),
                COALESCE(SUM(CASE WHEN present = 0 THEN len ELSE 0 END), 0)
         FROM chunks",
        [],
        |row| Ok((row.get(0)?, row.get(1)?, row.get(2)?)),
    )?;
    estimate.unique_chunks = unique_chunks as u64;
    estimate.deduplicated_chunk_bytes = deduplicated as u64;
    estimate.projected_new_chunk_bytes = projected as u64;
    Ok(estimate)
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

struct FileUsage {
    logical_bytes: u64,
    physical_bytes: u64,
}

fn estimate_file(
    transaction: &Transaction<'_>,
    store: &ObjectStore,
    path: &Path,
    sequence: &mut u64,
) -> anyhow::Result<FileUsage> {
    for _ in 0..3 {
        *sequence = sequence.saturating_add(1);
        let savepoint = format!("file_{}", *sequence);
        transaction.execute_batch(&format!("SAVEPOINT {savepoint}"))?;
        let file = File::open(path).with_context(|| format!("open {}", path.display()))?;
        let before = file.metadata()?;
        let mut stream = ChunkStream::new(BufReader::with_capacity(256 * 1024, file));
        while let Some(chunk) = stream.next_chunk()? {
            let id = object_id(ObjectKind::Chunk, &chunk);
            let inserted = transaction.execute(
                "INSERT OR IGNORE INTO chunks(id, len, present) VALUES(?1, ?2, 0)",
                params![id.as_slice(), chunk.len() as i64],
            )?;
            if inserted != 0 && store.contains_object(&id)? {
                transaction.execute(
                    "UPDATE chunks SET present = 1 WHERE id = ?1",
                    params![id.as_slice()],
                )?;
            }
        }
        let after = fs::metadata(path)?;
        if stable(&before, &after) {
            transaction.execute_batch(&format!("RELEASE {savepoint}"))?;
            return Ok(FileUsage {
                logical_bytes: after.len(),
                physical_bytes: after.blocks().saturating_mul(512),
            });
        }
        transaction.execute_batch(&format!("ROLLBACK TO {savepoint}; RELEASE {savepoint}"))?;
    }
    anyhow::bail!(
        "file changed repeatedly while estimating: {}",
        path.display()
    )
}

fn stable(before: &fs::Metadata, after: &fs::Metadata) -> bool {
    before.dev() == after.dev()
        && before.ino() == after.ino()
        && before.len() == after.len()
        && before.mtime() == after.mtime()
        && before.mtime_nsec() == after.mtime_nsec()
        && before.ctime() == after.ctime()
        && before.ctime_nsec() == after.ctime_nsec()
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn estimates_unique_and_existing_chunks_without_counting_excluded_subtrees() {
        let temporary = tempfile::tempdir().unwrap();
        let root = temporary.path().join("repo");
        fs::create_dir_all(root.join("cache")).unwrap();
        fs::write(root.join("one.bin"), vec![7_u8; 80_000]).unwrap();
        fs::write(root.join("two.bin"), vec![7_u8; 80_000]).unwrap();
        fs::write(root.join("cache/ignored.bin"), vec![9_u8; 80_000]).unwrap();
        fs::write(root.join(".furrowpolicy"), b"exclude cache\n").unwrap();
        let store = ObjectStore::open(temporary.path().join("store")).unwrap();

        let first = calculate(&root, &store).unwrap();
        assert_eq!(first.files, 3);
        assert_eq!(first.excluded_subtrees, 1);
        assert_eq!(first.logical_bytes, 160_014);
        assert!(first.projected_new_chunk_bytes < first.logical_bytes);
        assert_eq!(first.deduplicated_chunk_bytes, 0);

        let bytes = vec![7_u8; 80_000];
        let mut stream = ChunkStream::new(std::io::Cursor::new(bytes));
        while let Some(chunk) = stream.next_chunk().unwrap() {
            store.put_bytes(ObjectKind::Chunk, &chunk).unwrap();
        }
        let second = calculate(&root, &store).unwrap();
        assert!(second.deduplicated_chunk_bytes > 0);
        assert!(second.projected_new_chunk_bytes < first.projected_new_chunk_bytes);
    }
}
