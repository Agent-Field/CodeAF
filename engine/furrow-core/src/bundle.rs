//! Streaming, self-verifying transport bundles for exact snapshots.

use crate::model::{Blob, EntryKind, ObjectId, ObjectKind, Snapshot, Tree};
use crate::store::{object_id, ObjectStore};
use anyhow::Context;
use rusqlite::{params, Connection, OptionalExtension};
use std::io::{Read, Write};

const BUNDLE_MAGIC: &[u8; 5] = b"AGSB\x01";
const RECORD_MAGIC: &[u8; 4] = b"AGOR";
const END_MAGIC: &[u8; 4] = b"AGSE";
const MAX_BUNDLE_OBJECT: u64 = 256 * 1024 * 1024;

pub fn export(
    store: &ObjectStore,
    snapshot: ObjectId,
    mut output: impl Write,
) -> anyhow::Result<u64> {
    output.write_all(BUNDLE_MAGIC)?;
    output.write_all(&snapshot)?;
    let mut count = 0_u64;
    for_each_reachable(store, snapshot, |id, kind, bytes| {
        output.write_all(RECORD_MAGIC)?;
        output.write_all(&[kind as u8])?;
        output.write_all(&(bytes.len() as u64).to_le_bytes())?;
        output.write_all(id)?;
        output.write_all(blake3::hash(bytes).as_bytes())?;
        output.write_all(bytes)?;
        count += 1;
        Ok(())
    })?;
    output.write_all(END_MAGIC)?;
    output.write_all(&count.to_le_bytes())?;
    Ok(count)
}

pub fn import(store: &ObjectStore, mut input: impl Read) -> anyhow::Result<ObjectId> {
    let mut magic = [0; BUNDLE_MAGIC.len()];
    input.read_exact(&mut magic)?;
    anyhow::ensure!(&magic == BUNDLE_MAGIC, "invalid snapshot bundle header");
    let mut snapshot = [0; 32];
    input.read_exact(&mut snapshot)?;
    let marks_file = tempfile::NamedTempFile::new()?;
    let marks = Connection::open(marks_file.path())?;
    marks.execute_batch(
        "PRAGMA journal_mode=OFF;
         PRAGMA synchronous=OFF;
         CREATE TABLE marks(
            id BLOB PRIMARY KEY,
            kind INTEGER NOT NULL,
            processed INTEGER NOT NULL DEFAULT 0
         ) WITHOUT ROWID;
         BEGIN;",
    )?;
    enqueue(&marks, &snapshot, ObjectKind::Snapshot)?;
    let mut count = 0_u64;
    loop {
        let mut record_magic = [0; 4];
        input
            .read_exact(&mut record_magic)
            .context("snapshot bundle is truncated")?;
        if &record_magic == END_MAGIC {
            let mut expected = [0; 8];
            input.read_exact(&mut expected)?;
            anyhow::ensure!(
                u64::from_le_bytes(expected) == count,
                "snapshot bundle record count mismatch"
            );
            let pending: u64 = marks.query_row(
                "SELECT COUNT(*) FROM marks WHERE processed = 0",
                [],
                |row| row.get(0),
            )?;
            anyhow::ensure!(pending == 0, "snapshot bundle omits reachable objects");
            let mut trailing = [0_u8; 1];
            anyhow::ensure!(
                input.read(&mut trailing)? == 0,
                "snapshot bundle contains trailing data"
            );
            break;
        }
        anyhow::ensure!(
            &record_magic == RECORD_MAGIC,
            "invalid bundle record header"
        );
        let mut kind = [0; 1];
        let mut length = [0; 8];
        let mut id = [0; 32];
        let mut checksum = [0; 32];
        input.read_exact(&mut kind)?;
        input.read_exact(&mut length)?;
        input.read_exact(&mut id)?;
        input.read_exact(&mut checksum)?;
        let kind = ObjectKind::from_u8(kind[0]).context("invalid bundle object kind")?;
        let expected: Option<(u8, bool)> = marks
            .query_row(
                "SELECT kind, processed FROM marks WHERE id = ?1",
                params![id.as_slice()],
                |row| Ok((row.get(0)?, row.get(1)?)),
            )
            .optional()?;
        let (expected_kind, processed) = expected.context("bundle contains an unrelated object")?;
        anyhow::ensure!(!processed, "bundle contains a duplicate object");
        anyhow::ensure!(expected_kind == kind as u8, "bundle object kind mismatch");
        let length = u64::from_le_bytes(length);
        anyhow::ensure!(length <= MAX_BUNDLE_OBJECT, "bundle object is too large");
        let mut bytes = vec![0; length as usize];
        input
            .read_exact(&mut bytes)
            .context("bundle object payload is truncated")?;
        anyhow::ensure!(
            blake3::hash(&bytes).as_bytes() == &checksum,
            "bundle object checksum mismatch"
        );
        anyhow::ensure!(object_id(kind, &bytes) == id, "bundle object ID mismatch");
        anyhow::ensure!(
            store.put_bytes(kind, &bytes)? == id,
            "bundle import ID mismatch"
        );
        enqueue_edges(&marks, kind, &bytes)?;
        marks.execute(
            "UPDATE marks SET processed = 1 WHERE id = ?1",
            params![id.as_slice()],
        )?;
        count += 1;
    }
    marks.execute_batch("COMMIT;")?;

    // The framing may be valid while omitting a referenced object. Traverse
    // the imported root before accepting the bundle as complete.
    for_each_reachable(store, snapshot, |_id, _kind, _bytes| Ok(()))?;
    Ok(snapshot)
}

pub fn for_each_reachable(
    store: &ObjectStore,
    snapshot: ObjectId,
    mut visitor: impl FnMut(&ObjectId, ObjectKind, &[u8]) -> anyhow::Result<()>,
) -> anyhow::Result<u64> {
    let marks_file = tempfile::NamedTempFile::new()?;
    let marks = Connection::open(marks_file.path())?;
    marks.execute_batch(
        "PRAGMA journal_mode=OFF;
         PRAGMA synchronous=OFF;
         CREATE TABLE marks(
            id BLOB PRIMARY KEY,
            kind INTEGER NOT NULL,
            processed INTEGER NOT NULL DEFAULT 0
         ) WITHOUT ROWID;
         CREATE INDEX unprocessed ON marks(processed, id);",
    )?;
    marks.execute_batch("BEGIN;")?;
    enqueue(&marks, &snapshot, ObjectKind::Snapshot)?;
    let mut count = 0_u64;
    while let Some((id, expected)) = next_mark(&marks)? {
        let bytes = store.read_bytes(&id, expected)?;
        visitor(&id, expected, &bytes)?;
        enqueue_edges(&marks, expected, &bytes)?;
        marks.execute(
            "UPDATE marks SET processed = 1 WHERE id = ?1",
            params![id.as_slice()],
        )?;
        count += 1;
    }
    marks.execute_batch("COMMIT;")?;
    Ok(count)
}

fn enqueue(connection: &Connection, id: &ObjectId, kind: ObjectKind) -> anyhow::Result<()> {
    connection.execute(
        "INSERT OR IGNORE INTO marks(id, kind) VALUES(?1, ?2)",
        params![id.as_slice(), kind as u8],
    )?;
    let actual: u8 = connection.query_row(
        "SELECT kind FROM marks WHERE id = ?1",
        params![id.as_slice()],
        |row| row.get(0),
    )?;
    anyhow::ensure!(
        actual == kind as u8,
        "object reached with conflicting kinds"
    );
    Ok(())
}

fn next_mark(connection: &Connection) -> anyhow::Result<Option<(ObjectId, ObjectKind)>> {
    let raw: Option<(Vec<u8>, u8)> = connection
        .query_row(
            "SELECT id, kind FROM marks WHERE processed = 0 ORDER BY id LIMIT 1",
            [],
            |row| Ok((row.get(0)?, row.get(1)?)),
        )
        .optional()?;
    raw.map(|(bytes, kind)| {
        anyhow::ensure!(bytes.len() == 32, "invalid marked object ID");
        let mut id = [0; 32];
        id.copy_from_slice(&bytes);
        Ok((
            id,
            ObjectKind::from_u8(kind).context("invalid marked object kind")?,
        ))
    })
    .transpose()
}

fn enqueue_edges(connection: &Connection, kind: ObjectKind, bytes: &[u8]) -> anyhow::Result<()> {
    for (id, kind) in object_edges(kind, bytes)? {
        enqueue(connection, &id, kind)?;
    }
    Ok(())
}

pub(crate) fn object_edges(
    kind: ObjectKind,
    bytes: &[u8],
) -> anyhow::Result<Vec<(ObjectId, ObjectKind)>> {
    let mut edges = Vec::new();
    match kind {
        ObjectKind::Snapshot => {
            let snapshot: Snapshot = serde_json::from_slice(bytes)?;
            edges.push((snapshot.root_tree, ObjectKind::Tree));
            for backup in snapshot.sqlite_backups {
                edges.push((backup.blob, ObjectKind::Blob));
            }
        }
        ObjectKind::Tree => {
            let tree: Tree = serde_json::from_slice(bytes)?;
            for page in tree.pages {
                edges.push((page.target, ObjectKind::Tree));
            }
            for entry in tree.entries {
                if let Some(xattrs) = entry.xattrs {
                    edges.push((xattrs, ObjectKind::Xattrs));
                }
                match (entry.kind, entry.target) {
                    (EntryKind::File, Some(id)) => edges.push((id, ObjectKind::Blob)),
                    (EntryKind::Directory, Some(id)) => edges.push((id, ObjectKind::Tree)),
                    _ => {}
                }
            }
        }
        ObjectKind::Blob => {
            let blob: Blob = serde_json::from_slice(bytes)?;
            for chunk in blob.chunks {
                edges.push((chunk.id, ObjectKind::Chunk));
            }
        }
        ObjectKind::Chunk | ObjectKind::Xattrs => {}
    }
    Ok(edges)
}

#[cfg(test)]
mod tests {
    use super::*;
    use crate::model::{
        Blob, ChunkRef, SealQuality, SnapshotTrigger, TreeEntry, XattrEntry, Xattrs,
    };

    fn snapshot_fixture(store: &ObjectStore) -> ObjectId {
        let chunk = store.put_bytes(ObjectKind::Chunk, b"bundle data").unwrap();
        let blob = store
            .put_struct(
                ObjectKind::Blob,
                &Blob {
                    chunks: vec![ChunkRef { id: chunk, len: 11 }],
                    total_len: 11,
                },
            )
            .unwrap();
        let xattrs = store
            .put_struct(
                ObjectKind::Xattrs,
                &Xattrs {
                    entries: vec![XattrEntry {
                        name: b"user.test".to_vec(),
                        value: b"kept".to_vec(),
                    }],
                },
            )
            .unwrap();
        let tree = store
            .put_struct(
                ObjectKind::Tree,
                &Tree {
                    entries: vec![TreeEntry {
                        name: b"file".to_vec(),
                        kind: EntryKind::File,
                        target: Some(blob),
                        link_target: Vec::new(),
                        mode: 0o100644,
                        size: 11,
                        mtime_secs: 0,
                        mtime_nanos: 0,
                        xattrs: Some(xattrs),
                        class: Default::default(),
                    }],
                    pages: Vec::new(),
                },
            )
            .unwrap();
        store
            .put_struct(
                ObjectKind::Snapshot,
                &Snapshot {
                    root_tree: tree,
                    parent: None,
                    merge_parents: Vec::new(),
                    sealed_at_secs: 0,
                    sealed_at_nanos: 0,
                    quality: SealQuality::Quiescent,
                    trigger: SnapshotTrigger::Manual,
                    label: None,
                    sqlite_backups: Vec::new(),
                    claims: Vec::new(),
                    excluded_paths: Vec::new(),
                },
            )
            .unwrap()
    }

    #[test]
    fn exact_bundle_round_trip_and_corruption_rejection() {
        let source_dir = tempfile::tempdir().unwrap();
        let source = ObjectStore::open(source_dir.path().join("store")).unwrap();
        let snapshot = snapshot_fixture(&source);
        let mut bundle = Vec::new();
        assert_eq!(export(&source, snapshot, &mut bundle).unwrap(), 5);

        let target_dir = tempfile::tempdir().unwrap();
        let target = ObjectStore::open(target_dir.path().join("store")).unwrap();
        assert_eq!(import(&target, bundle.as_slice()).unwrap(), snapshot);
        assert_eq!(
            for_each_reachable(&target, snapshot, |_, _, _| Ok(())).unwrap(),
            5
        );

        let mut damaged = bundle;
        let middle = damaged.len() / 2;
        damaged[middle] ^= 1;
        let damaged_target = ObjectStore::open(target_dir.path().join("damaged")).unwrap();
        assert!(import(&damaged_target, damaged.as_slice()).is_err());

        let mut padded = Vec::new();
        export(&source, snapshot, &mut padded).unwrap();
        padded.extend_from_slice(b"untrusted padding");
        let padded_target = ObjectStore::open(target_dir.path().join("padded")).unwrap();
        assert!(import(&padded_target, padded.as_slice()).is_err());
    }
}
