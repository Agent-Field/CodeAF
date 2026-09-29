//! Exact, bounded-memory reachability collection for the global object store.

use crate::catalog::PackCheckpoint;
use crate::content_class::{classify, ContentClass};
use crate::model::{Blob, EntryKind, ObjectId, ObjectKind, Snapshot, Tree};
use crate::refs::RefLog;
use crate::retention::RetentionLog;
use crate::store::ObjectStore;
use anyhow::Context;
use rusqlite::{params, Connection, OptionalExtension};
use serde::{Deserialize, Serialize};
use std::fs::{self, File, OpenOptions};
use std::io::{Seek, Write};
use std::path::{Path, PathBuf};
use std::time::{SystemTime, UNIX_EPOCH};

const OBJECT_MAGIC: &[u8; 4] = b"AGOB";
const OBJECT_END: &[u8; 4] = b"AGND";
const OBJECT_VERSION: u8 = 1;
const HEADER_LEN: u64 = 4 + 1 + 1 + 8 + 32 + 32;
const RECORD_OVERHEAD: u64 = HEADER_LEN + OBJECT_END.len() as u64;
const COMPACTION_BATCH: usize = 1_024;
const ALL_CLASS_MASK: u16 = (1 << ContentClass::ALL.len()) - 1;
const BLOB_BYTES_BIT: u16 = 1 << 15;
const DAY_SECONDS: i64 = 24 * 60 * 60;

#[derive(Debug, Clone, Serialize)]
pub struct GcReport {
    pub dry_run: bool,
    pub roots: u64,
    pub reachable_objects: u64,
    pub unreachable_objects: u64,
    pub reachable_payload_bytes: u64,
    pub unreachable_payload_bytes: u64,
    pub physical_bytes_before: u64,
    pub physical_bytes_after: u64,
    pub reclaimed_bytes: u64,
    pub published_snapshots: u64,
    pub retained_snapshots: u64,
    pub thinned_snapshots: u64,
}

#[derive(Default)]
struct RootSummary {
    roots: u64,
    published_snapshots: u64,
    retained_snapshots: u64,
}

#[derive(Debug, Deserialize)]
struct RestoreRoots {
    pre_snapshot: ObjectId,
    target_snapshot: ObjectId,
}

#[derive(Debug, Deserialize)]
struct SyncRoot {
    snapshot: ObjectId,
}

struct MarkDatabase {
    path: PathBuf,
    connection: Connection,
}

impl MarkDatabase {
    fn create(store_root: &Path) -> anyhow::Result<Self> {
        let path = store_root.join("locks").join(format!(
            "gc-mark-{}-{}.sqlite3",
            std::process::id(),
            unique_generation()
        ));
        let connection = Connection::open(&path)?;
        connection.pragma_update(None, "journal_mode", "OFF")?;
        connection.pragma_update(None, "synchronous", "OFF")?;
        connection.execute_batch(
            "CREATE TABLE marks (
                id BLOB PRIMARY KEY,
                kind INTEGER NOT NULL,
                mask INTEGER NOT NULL,
                processed INTEGER NOT NULL DEFAULT 0
             ) WITHOUT ROWID;
             CREATE INDEX marks_pending ON marks(processed, id);
             CREATE TABLE locations (
                id BLOB PRIMARY KEY,
                kind INTEGER NOT NULL,
                offset INTEGER NOT NULL,
                len INTEGER NOT NULL
             ) WITHOUT ROWID;",
        )?;
        Ok(Self { path, connection })
    }

    fn enqueue(&self, id: &ObjectId, kind: ObjectKind, mask: u16) -> anyhow::Result<bool> {
        Ok(self.connection.execute(
            "INSERT INTO marks(id, kind, mask) VALUES(?1, ?2, ?3)
             ON CONFLICT(id) DO UPDATE SET
                mask = marks.mask | excluded.mask,
                processed = 0
             WHERE (marks.mask | excluded.mask) != marks.mask",
            params![id.as_slice(), kind as u8, mask],
        )? != 0)
    }

    fn next(&self) -> anyhow::Result<Option<(ObjectId, ObjectKind, u16)>> {
        self.connection
            .query_row(
                "SELECT id, kind, mask FROM marks WHERE processed = 0 ORDER BY id LIMIT 1",
                [],
                |row| {
                    Ok((
                        row.get::<_, Vec<u8>>(0)?,
                        row.get::<_, u8>(1)?,
                        row.get::<_, u16>(2)?,
                    ))
                },
            )
            .optional()?
            .map(|(bytes, kind, mask)| {
                let id = vec_to_id(bytes)?;
                let kind = ObjectKind::from_u8(kind).context("invalid kind in GC mark database")?;
                Ok((id, kind, mask))
            })
            .transpose()
    }

    fn processed(&self, id: &ObjectId) -> anyhow::Result<()> {
        self.connection.execute(
            "UPDATE marks SET processed = 1 WHERE id = ?1",
            params![id.as_slice()],
        )?;
        Ok(())
    }

    fn counts(&self) -> anyhow::Result<(u64, u64)> {
        let count = self
            .connection
            .query_row("SELECT COUNT(*) FROM marks", [], |row| row.get::<_, i64>(0))?;
        Ok((count as u64, 0))
    }
}

impl Drop for MarkDatabase {
    fn drop(&mut self) {
        let _ = fs::remove_file(&self.path);
        let _ = fs::remove_file(self.path.with_extension("sqlite3-wal"));
        let _ = fs::remove_file(self.path.with_extension("sqlite3-shm"));
    }
}

pub fn collect(store: &mut ObjectStore, dry_run: bool) -> anyhow::Result<GcReport> {
    let _maintenance = store.acquire_maintenance_exclusive()?;
    collect_locked(store, dry_run)
}

fn collect_locked(store: &mut ObjectStore, dry_run: bool) -> anyhow::Result<GcReport> {
    let now = SystemTime::now()
        .duration_since(UNIX_EPOCH)
        .context("system clock is before Unix epoch")?
        .as_secs() as i64;
    collect_locked_at(store, dry_run, now)
}

fn collect_locked_at(store: &mut ObjectStore, dry_run: bool, now: i64) -> anyhow::Result<GcReport> {
    let marks = MarkDatabase::create(store.root())?;
    let root_summary = enqueue_roots(store.root(), &marks, now, !dry_run)?;
    traverse(store, &marks)?;

    let reachable_objects = marks.counts()?.0;
    let total_objects = store.stats_unlocked()?.objects;
    let total_payload = store.object_payload_bytes()?;
    let reachable_payload = reachable_payload_bytes(store, &marks)?;
    let before = store.stats_unlocked()?.physical_bytes;
    let projected = reachable_payload.saturating_add(reachable_objects * RECORD_OVERHEAD);
    let unreachable_objects = total_objects.saturating_sub(reachable_objects);
    let unreachable_payload = total_payload.saturating_sub(reachable_payload);

    if dry_run {
        return Ok(GcReport {
            dry_run: true,
            roots: root_summary.roots,
            reachable_objects,
            unreachable_objects,
            reachable_payload_bytes: reachable_payload,
            unreachable_payload_bytes: unreachable_payload,
            physical_bytes_before: before,
            physical_bytes_after: projected,
            reclaimed_bytes: before.saturating_sub(projected),
            published_snapshots: root_summary.published_snapshots,
            retained_snapshots: root_summary.retained_snapshots,
            thinned_snapshots: root_summary
                .published_snapshots
                .saturating_sub(root_summary.retained_snapshots),
        });
    }

    let after = compact(store, &marks)?;
    Ok(GcReport {
        dry_run: false,
        roots: root_summary.roots,
        reachable_objects,
        unreachable_objects,
        reachable_payload_bytes: reachable_payload,
        unreachable_payload_bytes: unreachable_payload,
        physical_bytes_before: before,
        physical_bytes_after: after,
        reclaimed_bytes: before.saturating_sub(after),
        published_snapshots: root_summary.published_snapshots,
        retained_snapshots: root_summary.retained_snapshots,
        thinned_snapshots: root_summary
            .published_snapshots
            .saturating_sub(root_summary.retained_snapshots),
    })
}

fn enqueue_roots(
    store_root: &Path,
    marks: &MarkDatabase,
    now: i64,
    persist_retention: bool,
) -> anyhow::Result<RootSummary> {
    let mut summary = RootSummary::default();
    let workspaces = store_root.join("workspaces");
    if !workspaces.exists() {
        return Ok(summary);
    }
    for entry in fs::read_dir(workspaces)? {
        let entry = entry?;
        if !entry.file_type()?.is_dir() {
            continue;
        }
        let workspace_id = entry.file_name().to_string_lossy().into_owned();
        let refs = RefLog::open(store_root, &workspace_id)?;
        let retention = RetentionLog::open(store_root, &workspace_id)?;
        let state = if persist_retention {
            retention.apply(&refs, now)?
        } else {
            retention.plan(&refs, now)?
        };
        let head_sequence = refs.head()?.map_or(0, |record| record.sequence);
        refs.for_each_record(|record| {
            summary.published_snapshots += 1;
            if state.retains(record.sequence, &record.snapshot_id) {
                summary.retained_snapshots += 1;
                let mask = byte_mask(
                    now,
                    record.sealed_at,
                    record.sequence == head_sequence || state.is_pinned(&record.snapshot_id),
                );
                if marks.enqueue(&record.snapshot_id, ObjectKind::Snapshot, mask)? {
                    summary.roots += 1;
                }
            }
            Ok(())
        })?;

        let intent_path = entry.path().join("restore.intent");
        if intent_path.exists() {
            let intent: RestoreRoots = serde_json::from_slice(&fs::read(&intent_path)?)
                .with_context(|| format!("read restore intent {}", intent_path.display()))?;
            for id in [intent.pre_snapshot, intent.target_snapshot] {
                if marks.enqueue(&id, ObjectKind::Snapshot, ALL_CLASS_MASK)? {
                    summary.roots += 1;
                }
            }
        }

        let incoming_path = entry.path().join("sync/incoming.json");
        if incoming_path.exists() {
            let incoming: SyncRoot = serde_json::from_slice(&fs::read(&incoming_path)?)
                .with_context(|| format!("read incoming sync root {}", incoming_path.display()))?;
            if marks.enqueue(&incoming.snapshot, ObjectKind::Snapshot, ALL_CLASS_MASK)? {
                summary.roots += 1;
            }
        }
    }
    Ok(summary)
}

fn traverse(store: &ObjectStore, marks: &MarkDatabase) -> anyhow::Result<()> {
    while let Some((id, expected, mask)) = marks.next()? {
        let actual = store.object_kind(&id)?;
        anyhow::ensure!(actual == expected, "reachable object kind mismatch");
        let bytes = store.read_bytes_unlocked(&id, expected)?;
        match expected {
            ObjectKind::Snapshot => {
                let snapshot: Snapshot = serde_json::from_slice(&bytes)?;
                marks.enqueue(&snapshot.root_tree, ObjectKind::Tree, mask)?;
                for backup in snapshot.sqlite_backups {
                    let keep_bytes = mask & classify(&backup.path).bit() != 0;
                    marks.enqueue(
                        &backup.blob,
                        ObjectKind::Blob,
                        if keep_bytes { BLOB_BYTES_BIT } else { 0 },
                    )?;
                }
                // Snapshot.parent is timeline metadata, not a reachability edge.
            }
            ObjectKind::Tree => {
                let tree: Tree = serde_json::from_slice(&bytes)?;
                for page in tree.pages {
                    marks.enqueue(&page.target, ObjectKind::Tree, mask)?;
                }
                for entry in tree.entries {
                    if let Some(xattrs) = entry.xattrs {
                        marks.enqueue(&xattrs, ObjectKind::Xattrs, 0)?;
                    }
                    match (entry.kind, entry.target) {
                        (EntryKind::File, Some(target)) => {
                            marks.enqueue(
                                &target,
                                ObjectKind::Blob,
                                if mask & entry.class.bit() != 0 {
                                    BLOB_BYTES_BIT
                                } else {
                                    0
                                },
                            )?;
                        }
                        (EntryKind::Directory, Some(target)) => {
                            marks.enqueue(&target, ObjectKind::Tree, mask)?;
                        }
                        _ => {}
                    }
                }
            }
            ObjectKind::Blob => {
                let blob: Blob = serde_json::from_slice(&bytes)?;
                if mask & BLOB_BYTES_BIT != 0 {
                    for chunk in blob.chunks {
                        marks.enqueue(&chunk.id, ObjectKind::Chunk, 0)?;
                    }
                }
            }
            ObjectKind::Chunk | ObjectKind::Xattrs => {}
        }
        marks.processed(&id)?;
    }
    Ok(())
}

fn byte_mask(now: i64, sealed_at: i64, exact_root: bool) -> u16 {
    if exact_root || sealed_at > now {
        return ALL_CLASS_MASK;
    }
    let age = now.saturating_sub(sealed_at);
    let mut mask = ContentClass::Source.bit()
        | ContentClass::VcsMeta.bit()
        | ContentClass::ConfigSecret.bit()
        | ContentClass::Lockfile.bit();
    if age <= 30 * DAY_SECONDS {
        mask |= ContentClass::Database.bit();
    }
    if age <= 3 * DAY_SECONDS {
        mask |= ContentClass::Dependency.bit() | ContentClass::BuildOutput.bit();
    }
    if age <= DAY_SECONDS {
        mask |= ContentClass::Scratch.bit();
    }
    mask
}

fn reachable_payload_bytes(store: &ObjectStore, marks: &MarkDatabase) -> anyhow::Result<u64> {
    let mut total = 0_u64;
    let mut last: Option<Vec<u8>> = None;
    loop {
        let batch = mark_batch(&marks.connection, last.as_deref())?;
        if batch.is_empty() {
            break;
        }
        for (id, _) in &batch {
            total = total.saturating_add(store.object_len(id)?);
        }
        last = batch.last().map(|(id, _)| id.to_vec());
    }
    Ok(total)
}

fn compact(store: &mut ObjectStore, marks: &MarkDatabase) -> anyhow::Result<u64> {
    let generation = unique_generation();
    let pack_name = format!("pack-gc-{generation}.agp");
    let packs = store.root().join("packs");
    let temporary = packs.join(format!(".{pack_name}.tmp"));
    let final_path = packs.join(&pack_name);
    let mut output = OpenOptions::new()
        .create_new(true)
        .write(true)
        .open(&temporary)?;
    let mut count = 0_u64;
    let mut position = 0_u64;
    let mut last_id = None;
    let mut last_record_start = 0_u64;

    loop {
        let batch = mark_batch(&marks.connection, last_id.as_deref())?;
        if batch.is_empty() {
            break;
        }
        for (id, expected) in &batch {
            let bytes = store.read_bytes_unlocked(id, *expected)?;
            let record_start = position;
            let payload_offset = write_record(&mut output, *expected, id, &bytes)?;
            position = payload_offset + bytes.len() as u64 + OBJECT_END.len() as u64;
            marks.connection.execute(
                "INSERT INTO locations(id, kind, offset, len) VALUES(?1, ?2, ?3, ?4)",
                params![
                    id.as_slice(),
                    *expected as u8,
                    payload_offset as i64,
                    bytes.len() as i64
                ],
            )?;
            count += 1;
            last_record_start = record_start;
        }
        last_id = batch.last().map(|(id, _)| id.to_vec());
    }
    output.sync_all()?;
    drop(output);
    fs::rename(&temporary, &final_path)?;
    File::open(&packs)?.sync_all()?;

    if std::env::var_os("FURROW_GC_FAILPOINT").as_deref()
        == Some(std::ffi::OsStr::new("after_pack_publish"))
    {
        anyhow::bail!("injected GC failure after pack publish");
    }

    let checkpoint = PackCheckpoint {
        verified_len: position,
        object_count: count,
        last_object: last_id.map(vec_to_id).transpose()?,
        last_record_start,
    };
    store.replace_objects_from_gc(&marks.path, &pack_name, &checkpoint)?;

    if std::env::var_os("FURROW_GC_FAILPOINT").as_deref()
        == Some(std::ffi::OsStr::new("after_catalog_swap"))
    {
        anyhow::bail!("injected GC failure after catalog swap");
    }

    // CURRENT is the recovery boundary. Before this rename the old generation
    // can rebuild a deleted catalog; afterward the new compacted generation can.
    store.activate_pack(&pack_name)?;

    for entry in fs::read_dir(&packs)? {
        let entry = entry?;
        if entry.file_type()?.is_file()
            && entry.path().extension() == Some(std::ffi::OsStr::new("agp"))
            && entry.file_name() != std::ffi::OsStr::new(&pack_name)
        {
            fs::remove_file(entry.path())?;
        }
    }
    File::open(&packs)?.sync_all()?;
    Ok(fs::metadata(final_path)?.len())
}

fn mark_batch(
    connection: &Connection,
    after: Option<&[u8]>,
) -> anyhow::Result<Vec<(ObjectId, ObjectKind)>> {
    let mut statement = if after.is_some() {
        connection.prepare("SELECT id, kind FROM marks WHERE id > ?1 ORDER BY id LIMIT ?2")?
    } else {
        connection.prepare("SELECT id, kind FROM marks ORDER BY id LIMIT ?1")?
    };
    let collect = |row: &rusqlite::Row<'_>| -> rusqlite::Result<(Vec<u8>, u8)> {
        Ok((row.get(0)?, row.get(1)?))
    };
    let mut raw = Vec::new();
    if let Some(after) = after {
        for row in statement.query_map(params![after, COMPACTION_BATCH as i64], collect)? {
            raw.push(row?);
        }
    } else {
        for row in statement.query_map(params![COMPACTION_BATCH as i64], collect)? {
            raw.push(row?);
        }
    }
    raw.into_iter()
        .map(|(id, kind)| {
            Ok((
                vec_to_id(id)?,
                ObjectKind::from_u8(kind).context("invalid marked object kind")?,
            ))
        })
        .collect()
}

fn write_record(
    output: &mut File,
    kind: ObjectKind,
    id: &ObjectId,
    bytes: &[u8],
) -> anyhow::Result<u64> {
    let checksum = blake3::hash(bytes);
    output.write_all(OBJECT_MAGIC)?;
    output.write_all(&[OBJECT_VERSION, kind as u8])?;
    output.write_all(&(bytes.len() as u64).to_le_bytes())?;
    output.write_all(id)?;
    output.write_all(checksum.as_bytes())?;
    output.write_all(bytes)?;
    output.write_all(OBJECT_END)?;
    Ok(output.stream_position()? - bytes.len() as u64 - OBJECT_END.len() as u64)
}

fn unique_generation() -> u128 {
    SystemTime::now()
        .duration_since(UNIX_EPOCH)
        .unwrap_or_default()
        .as_nanos()
}

fn vec_to_id(bytes: Vec<u8>) -> anyhow::Result<ObjectId> {
    anyhow::ensure!(bytes.len() == 32, "invalid object ID in GC database");
    let mut id = [0_u8; 32];
    id.copy_from_slice(&bytes);
    Ok(id)
}

#[cfg(test)]
mod tests {
    use super::*;
    use crate::model::{
        ChunkRef, SealQuality, SnapshotTrigger, SqliteBackup, TreeEntry, TreePage, XattrEntry,
        Xattrs,
    };

    fn empty_tree(store: &ObjectStore) -> ObjectId {
        store
            .put_struct(
                ObjectKind::Tree,
                &Tree {
                    entries: Vec::new(),
                    pages: Vec::new(),
                },
            )
            .unwrap()
    }

    fn snapshot(root_tree: ObjectId, parent: Option<ObjectId>) -> Snapshot {
        Snapshot {
            root_tree,
            parent,
            merge_parents: Vec::new(),
            sealed_at_secs: 1,
            sealed_at_nanos: 0,
            quality: SealQuality::Quiescent,
            trigger: SnapshotTrigger::Manual,
            label: None,
            sqlite_backups: Vec::new(),
            claims: Vec::new(),
            excluded_paths: Vec::new(),
        }
    }

    #[test]
    fn exact_gc_follows_pages_and_chunks_but_not_snapshot_parent() {
        let temporary = tempfile::tempdir().unwrap();
        let mut store = ObjectStore::open(temporary.path().to_owned()).unwrap();
        store.ensure_workspace("workspace", b"/workspace").unwrap();

        let kept_chunk = store.put_bytes(ObjectKind::Chunk, b"kept bytes").unwrap();
        let kept_blob = store
            .put_struct(
                ObjectKind::Blob,
                &Blob {
                    chunks: vec![ChunkRef {
                        id: kept_chunk,
                        len: 10,
                    }],
                    total_len: 10,
                },
            )
            .unwrap();
        let xattrs = store
            .put_struct(
                ObjectKind::Xattrs,
                &Xattrs {
                    entries: vec![XattrEntry {
                        name: b"user.test".to_vec(),
                        value: b"yes".to_vec(),
                    }],
                },
            )
            .unwrap();
        let leaf = store
            .put_struct(
                ObjectKind::Tree,
                &Tree {
                    entries: vec![TreeEntry {
                        name: b"file".to_vec(),
                        kind: EntryKind::File,
                        target: Some(kept_blob),
                        link_target: Vec::new(),
                        mode: 0o100644,
                        size: 10,
                        mtime_secs: 1,
                        mtime_nanos: 0,
                        xattrs: Some(xattrs),
                        class: Default::default(),
                    }],
                    pages: Vec::new(),
                },
            )
            .unwrap();
        let root = store
            .put_struct(
                ObjectKind::Tree,
                &Tree {
                    entries: Vec::new(),
                    pages: vec![TreePage {
                        first_name: b"file".to_vec(),
                        last_name: b"file".to_vec(),
                        entry_count: 1,
                        target: leaf,
                    }],
                },
            )
            .unwrap();

        let orphan_tree = empty_tree(&store);
        let orphan_parent = store
            .put_struct(ObjectKind::Snapshot, &snapshot(orphan_tree, None))
            .unwrap();
        let abandoned = store
            .put_bytes(ObjectKind::Chunk, b"not referenced")
            .unwrap();
        let mut current = snapshot(root, Some(orphan_parent));
        current.sqlite_backups.push(SqliteBackup {
            path: b"dev.sqlite".to_vec(),
            blob: kept_blob,
            integrity_ok: true,
        });
        let current = store.put_struct(ObjectKind::Snapshot, &current).unwrap();
        store
            .publish_snapshot("workspace", current, 1, None, SnapshotTrigger::Manual)
            .unwrap();

        let bytes_before_preview = store.stats().unwrap().physical_bytes;
        let preview = collect(&mut store, true).unwrap();
        assert!(preview.dry_run);
        assert_eq!(preview.reachable_objects, 6);
        assert_eq!(preview.unreachable_objects, 3);
        assert!(store.read_bytes(&abandoned, ObjectKind::Chunk).is_ok());
        assert_eq!(store.stats().unwrap().physical_bytes, bytes_before_preview);

        let report = collect(&mut store, false).unwrap();
        assert_eq!(report.reachable_objects, 6);
        assert_eq!(report.unreachable_objects, 3);
        assert!(store.read_bytes(&kept_chunk, ObjectKind::Chunk).is_ok());
        assert!(store.read_bytes(&leaf, ObjectKind::Tree).is_ok());
        assert!(store
            .read_bytes(&orphan_parent, ObjectKind::Snapshot)
            .is_err());
        assert!(store.read_bytes(&orphan_tree, ObjectKind::Tree).is_err());
        assert!(store.read_bytes(&abandoned, ObjectKind::Chunk).is_err());
        assert!(report.physical_bytes_after < report.physical_bytes_before);

        let appended = store
            .put_bytes(ObjectKind::Chunk, b"written after compaction")
            .unwrap();
        drop(store);
        for suffix in ["", "-wal", "-shm"] {
            let _ = fs::remove_file(temporary.path().join(format!("catalog.sqlite3{suffix}")));
        }
        let rebuilt = ObjectStore::open(temporary.path().to_owned()).unwrap();
        assert!(rebuilt.read_bytes(&kept_chunk, ObjectKind::Chunk).is_ok());
        assert!(rebuilt.read_bytes(&leaf, ObjectKind::Tree).is_ok());
        assert_eq!(
            rebuilt.read_bytes(&appended, ObjectKind::Chunk).unwrap(),
            b"written after compaction"
        );
    }

    #[test]
    fn restore_intent_is_an_independent_gc_root() {
        let temporary = tempfile::tempdir().unwrap();
        let mut store = ObjectStore::open(temporary.path().to_owned()).unwrap();
        store.ensure_workspace("workspace", b"/workspace").unwrap();
        let tree = empty_tree(&store);
        let snapshot = store
            .put_struct(ObjectKind::Snapshot, &snapshot(tree, None))
            .unwrap();
        fs::write(
            store.workspace_data_dir("workspace").join("restore.intent"),
            serde_json::to_vec(&serde_json::json!({
                "pre_snapshot": snapshot,
                "target_snapshot": snapshot,
                "paths": []
            }))
            .unwrap(),
        )
        .unwrap();

        let report = collect(&mut store, false).unwrap();
        assert_eq!(report.roots, 1);
        assert_eq!(report.reachable_objects, 2);
        assert!(store.read_bytes(&snapshot, ObjectKind::Snapshot).is_ok());
        assert!(store.read_bytes(&tree, ObjectKind::Tree).is_ok());
    }

    #[test]
    fn thinning_reclaims_old_graphs_but_preserves_pins_and_head() {
        let temporary = tempfile::tempdir().unwrap();
        let mut store = ObjectStore::open(temporary.path().to_owned()).unwrap();
        store.ensure_workspace("workspace", b"/workspace").unwrap();
        let week = 7 * 24 * 60 * 60;
        let now = 200 * 24 * 60 * 60;

        let mut snapshots = Vec::new();
        let mut chunks = Vec::new();
        for index in 0..3_u8 {
            let chunk = store.put_bytes(ObjectKind::Chunk, &[index; 32]).unwrap();
            let blob = store
                .put_struct(
                    ObjectKind::Blob,
                    &Blob {
                        chunks: vec![ChunkRef { id: chunk, len: 32 }],
                        total_len: 32,
                    },
                )
                .unwrap();
            let tree = store
                .put_struct(
                    ObjectKind::Tree,
                    &Tree {
                        entries: vec![TreeEntry {
                            name: b"state".to_vec(),
                            kind: EntryKind::File,
                            target: Some(blob),
                            link_target: Vec::new(),
                            mode: 0o100644,
                            size: 32,
                            mtime_secs: index as i64,
                            mtime_nanos: 0,
                            xattrs: None,
                            class: Default::default(),
                        }],
                        pages: Vec::new(),
                    },
                )
                .unwrap();
            let sealed_at = week + index as i64;
            let snapshot = store
                .put_struct(
                    ObjectKind::Snapshot,
                    &Snapshot {
                        sealed_at_secs: sealed_at,
                        ..snapshot(tree, snapshots.last().copied())
                    },
                )
                .unwrap();
            store
                .publish_snapshot(
                    "workspace",
                    snapshot,
                    sealed_at,
                    None,
                    SnapshotTrigger::Manual,
                )
                .unwrap();
            snapshots.push(snapshot);
            chunks.push(chunk);
        }
        assert!(store.pin_snapshot("workspace", snapshots[1]).unwrap());

        let preview = collect_locked_at(&mut store, true, now).unwrap();
        assert_eq!(preview.published_snapshots, 3);
        assert_eq!(preview.retained_snapshots, 2);
        assert_eq!(preview.thinned_snapshots, 1);
        assert_eq!(store.timeline("workspace", 10).unwrap().len(), 3);
        assert!(store.read_bytes(&chunks[0], ObjectKind::Chunk).is_ok());

        let collected = collect_locked_at(&mut store, false, now).unwrap();
        assert_eq!(collected.retained_snapshots, 2);
        assert!(store.read_bytes(&chunks[0], ObjectKind::Chunk).is_err());
        assert!(store.read_bytes(&chunks[1], ObjectKind::Chunk).is_ok());
        assert!(store.read_bytes(&chunks[2], ObjectKind::Chunk).is_ok());
        let timeline = store.timeline("workspace", 10).unwrap();
        assert_eq!(timeline.len(), 2);
        assert_eq!(timeline[0].id, snapshots[2]);
        assert_eq!(timeline[1].id, snapshots[1]);

        assert!(store.unpin_snapshot("workspace", &snapshots[1]).unwrap());
        collect_locked_at(&mut store, false, now).unwrap();
        assert!(store.read_bytes(&chunks[1], ObjectKind::Chunk).is_err());
        assert!(store.read_bytes(&chunks[2], ObjectKind::Chunk).is_ok());
    }

    #[test]
    fn expired_class_bytes_drop_while_manifests_and_head_remain_exact() {
        let temporary = tempfile::tempdir().unwrap();
        let mut store = ObjectStore::open(temporary.path().to_owned()).unwrap();
        store.ensure_workspace("workspace", b"/workspace").unwrap();
        let week = 7 * DAY_SECONDS;

        let make_graph = |store: &ObjectStore,
                          bytes: &[u8],
                          class: ContentClass,
                          sealed_at: i64,
                          parent: Option<ObjectId>| {
            let chunk = store.put_bytes(ObjectKind::Chunk, bytes).unwrap();
            let blob = store
                .put_struct(
                    ObjectKind::Blob,
                    &Blob {
                        chunks: vec![ChunkRef {
                            id: chunk,
                            len: bytes.len() as u32,
                        }],
                        total_len: bytes.len() as u64,
                    },
                )
                .unwrap();
            let tree = store
                .put_struct(
                    ObjectKind::Tree,
                    &Tree {
                        entries: vec![TreeEntry {
                            name: b"state".to_vec(),
                            kind: EntryKind::File,
                            target: Some(blob),
                            link_target: Vec::new(),
                            mode: 0o100644,
                            size: bytes.len() as u64,
                            mtime_secs: sealed_at,
                            mtime_nanos: 0,
                            xattrs: None,
                            class,
                        }],
                        pages: Vec::new(),
                    },
                )
                .unwrap();
            let snapshot = store
                .put_struct(
                    ObjectKind::Snapshot,
                    &Snapshot {
                        sealed_at_secs: sealed_at,
                        ..snapshot(tree, parent)
                    },
                )
                .unwrap();
            (snapshot, tree, blob, chunk)
        };

        let old = make_graph(
            &store,
            b"expired log bytes",
            ContentClass::Scratch,
            week,
            None,
        );
        store
            .publish_snapshot("workspace", old.0, week, None, SnapshotTrigger::Manual)
            .unwrap();
        let head = make_graph(
            &store,
            b"current source bytes",
            ContentClass::Source,
            2 * week,
            Some(old.0),
        );
        store
            .publish_snapshot("workspace", head.0, 2 * week, None, SnapshotTrigger::Manual)
            .unwrap();

        crate::budget::save(
            store.root(),
            crate::budget::BudgetConfig {
                max_store_bytes: 1,
                reserved_free_bytes: 0,
            },
        )
        .unwrap();
        let report = store.enforce_budget(true).unwrap().unwrap();
        assert_eq!(report.retained_snapshots, 2);
        assert!(store.read_bytes(&old.0, ObjectKind::Snapshot).is_ok());
        assert!(store.read_bytes(&old.1, ObjectKind::Tree).is_ok());
        assert!(store.read_bytes(&old.2, ObjectKind::Blob).is_ok());
        assert!(store.read_bytes(&old.3, ObjectKind::Chunk).is_err());
        assert!(store.read_bytes(&head.3, ObjectKind::Chunk).is_ok());
        let grade = crate::repository::derive_materialization(&store, &old.0).unwrap();
        assert_eq!(grade.grade, "partial");
        assert_eq!(grade.partial_classes, vec!["scratch"]);
        assert_eq!(grade.missing_paths.len(), 1);
        assert_eq!(grade.missing_paths[0].path, "state");
        assert_eq!(grade.missing_paths[0].recovery, "regenerate-or-fetch");
        assert_eq!(
            crate::repository::derive_materialization(&store, &head.0)
                .unwrap()
                .grade,
            "exact"
        );
        let budget = store.budget_status().unwrap();
        assert!(!budget.satisfied);
        assert!(budget.over_store_bytes > 0);
    }

    #[test]
    fn byte_windows_match_the_declared_class_policy() {
        let now = 100 * DAY_SECONDS;
        let permanent = ContentClass::Source.bit()
            | ContentClass::VcsMeta.bit()
            | ContentClass::ConfigSecret.bit()
            | ContentClass::Lockfile.bit();
        assert_eq!(byte_mask(now, 0, false) & permanent, permanent);
        assert_ne!(
            byte_mask(now, now - 30 * DAY_SECONDS, false) & ContentClass::Database.bit(),
            0
        );
        assert_eq!(
            byte_mask(now, now - 30 * DAY_SECONDS - 1, false) & ContentClass::Database.bit(),
            0
        );
        assert_ne!(
            byte_mask(now, now - 3 * DAY_SECONDS, false) & ContentClass::Dependency.bit(),
            0
        );
        assert_eq!(
            byte_mask(now, now - DAY_SECONDS - 1, false) & ContentClass::Scratch.bit(),
            0
        );
        assert_eq!(byte_mask(now, 0, true), ALL_CLASS_MASK);
    }

    #[test]
    fn gc_preserves_fork_machine_pin_and_shared_roots_across_index_recovery() {
        let temporary = tempfile::tempdir().unwrap();
        let store_root = temporary.path().to_owned();
        let mut store = ObjectStore::open(store_root.clone()).unwrap();
        for (workspace, root) in [
            ("main", b"/main".as_slice()),
            ("fork", b"/fork".as_slice()),
            ("machine", b"/machine".as_slice()),
        ] {
            store.ensure_workspace(workspace, root).unwrap();
        }
        let week = 7 * DAY_SECONDS;

        let graph = |store: &ObjectStore,
                     byte: u8,
                     class: ContentClass,
                     sealed_at: i64,
                     parent: Option<ObjectId>| {
            let chunk = store.put_bytes(ObjectKind::Chunk, &[byte; 64]).unwrap();
            let blob = store
                .put_struct(
                    ObjectKind::Blob,
                    &Blob {
                        chunks: vec![ChunkRef { id: chunk, len: 64 }],
                        total_len: 64,
                    },
                )
                .unwrap();
            let tree = store
                .put_struct(
                    ObjectKind::Tree,
                    &Tree {
                        entries: vec![TreeEntry {
                            name: vec![b'a' + byte],
                            kind: EntryKind::File,
                            target: Some(blob),
                            link_target: Vec::new(),
                            mode: 0o100644,
                            size: 64,
                            mtime_secs: sealed_at,
                            mtime_nanos: 0,
                            xattrs: None,
                            class,
                        }],
                        pages: Vec::new(),
                    },
                )
                .unwrap();
            let snapshot = store
                .put_struct(
                    ObjectKind::Snapshot,
                    &Snapshot {
                        sealed_at_secs: sealed_at,
                        ..snapshot(tree, parent)
                    },
                )
                .unwrap();
            (snapshot, chunk)
        };

        let abandoned = graph(&store, 1, ContentClass::Scratch, week, None);
        store
            .publish_snapshot("main", abandoned.0, week, None, SnapshotTrigger::Manual)
            .unwrap();
        let pinned = graph(
            &store,
            2,
            ContentClass::Scratch,
            week + 1,
            Some(abandoned.0),
        );
        store
            .publish_snapshot("main", pinned.0, week + 1, None, SnapshotTrigger::Manual)
            .unwrap();
        let main_head = graph(&store, 3, ContentClass::Source, week + 2, Some(pinned.0));
        store
            .publish_snapshot("main", main_head.0, week + 2, None, SnapshotTrigger::Manual)
            .unwrap();
        store.pin_snapshot("main", pinned.0).unwrap();

        let fork_head = graph(&store, 4, ContentClass::Scratch, week, None);
        store
            .publish_snapshot("fork", fork_head.0, week, None, SnapshotTrigger::Manual)
            .unwrap();
        let machine_head = graph(&store, 5, ContentClass::Scratch, week, None);
        store
            .publish_snapshot(
                "machine",
                machine_head.0,
                week,
                None,
                SnapshotTrigger::Manual,
            )
            .unwrap();

        let shared_chunk = store
            .put_bytes(ObjectKind::Chunk, b"shared protected bytes")
            .unwrap();
        let shared_blob = store
            .put_struct(
                ObjectKind::Blob,
                &Blob {
                    chunks: vec![ChunkRef {
                        id: shared_chunk,
                        len: 22,
                    }],
                    total_len: 22,
                },
            )
            .unwrap();
        let shared_tree = |store: &ObjectStore, class| {
            store
                .put_struct(
                    ObjectKind::Tree,
                    &Tree {
                        entries: vec![TreeEntry {
                            name: b"shared".to_vec(),
                            kind: EntryKind::File,
                            target: Some(shared_blob),
                            link_target: Vec::new(),
                            mode: 0o100644,
                            size: 22,
                            mtime_secs: week,
                            mtime_nanos: 0,
                            xattrs: None,
                            class,
                        }],
                        pages: Vec::new(),
                    },
                )
                .unwrap()
        };
        let expired_shared = store
            .put_struct(
                ObjectKind::Snapshot,
                &Snapshot {
                    sealed_at_secs: 2 * week,
                    ..snapshot(
                        shared_tree(&store, ContentClass::Scratch),
                        Some(main_head.0),
                    )
                },
            )
            .unwrap();
        store
            .publish_snapshot(
                "main",
                expired_shared,
                2 * week,
                None,
                SnapshotTrigger::Manual,
            )
            .unwrap();
        let protected_shared = store
            .put_struct(
                ObjectKind::Snapshot,
                &Snapshot {
                    sealed_at_secs: 3 * week,
                    ..snapshot(
                        shared_tree(&store, ContentClass::Source),
                        Some(expired_shared),
                    )
                },
            )
            .unwrap();
        store
            .publish_snapshot(
                "main",
                protected_shared,
                3 * week,
                None,
                SnapshotTrigger::Manual,
            )
            .unwrap();

        let now = 200 * DAY_SECONDS;
        collect_locked_at(&mut store, false, now).unwrap();
        assert!(store.read_bytes(&abandoned.1, ObjectKind::Chunk).is_err());
        for chunk in [
            pinned.1,
            main_head.1,
            fork_head.1,
            machine_head.1,
            shared_chunk,
        ] {
            assert!(store.read_bytes(&chunk, ObjectKind::Chunk).is_ok());
        }

        drop(store);
        for suffix in ["", "-wal", "-shm"] {
            let _ = fs::remove_file(store_root.join(format!("catalog.sqlite3{suffix}")));
        }
        for workspace in ["main", "fork", "machine"] {
            let _ = fs::remove_file(
                store_root
                    .join("workspaces")
                    .join(workspace)
                    .join("refs.index"),
            );
        }
        let mut recovered = ObjectStore::open(store_root).unwrap();
        for (workspace, root) in [
            ("main", b"/main".as_slice()),
            ("fork", b"/fork".as_slice()),
            ("machine", b"/machine".as_slice()),
        ] {
            recovered.ensure_workspace(workspace, root).unwrap();
            assert!(!recovered.timeline(workspace, 10).unwrap().is_empty());
        }
        for chunk in [pinned.1, fork_head.1, machine_head.1, shared_chunk] {
            assert!(recovered.read_bytes(&chunk, ObjectKind::Chunk).is_ok());
        }

        recovered.unpin_snapshot("main", &pinned.0).unwrap();
        recovered.purge_workspace("fork").unwrap();
        recovered.purge_workspace("machine").unwrap();
        collect_locked_at(&mut recovered, false, now).unwrap();
        for chunk in [pinned.1, fork_head.1, machine_head.1] {
            assert!(recovered.read_bytes(&chunk, ObjectKind::Chunk).is_err());
        }
        assert!(recovered
            .read_bytes(&shared_chunk, ObjectKind::Chunk)
            .is_ok());
    }
}
