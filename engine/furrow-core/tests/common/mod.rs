//! Shared fixtures for the Stage 1 exchange tests.
#![allow(dead_code)]

use furrow::exchange::export::{ExportJob, ExportReport};
use furrow::exchange::fetch::Taken;
use furrow::exchange::frame;
use furrow::exchange::ledger::Ledger;
use furrow::exchange::open_store;
use furrow::model::{id_hex, ObjectId, SnapshotTrigger};
use furrow::repository::{FurrowRepository, SealOptions};
use furrow::sealer::{CellSealer, Keys};
use std::collections::BTreeMap;
use std::fs;
use std::os::unix::fs::{symlink, MetadataExt, PermissionsExt};
use std::path::{Path, PathBuf};
use tempfile::TempDir;

pub const LEDGER: &str = "0123456789abcdef";
pub const OTHER_LEDGER: &str = "fedcba9876543210";

pub fn keys() -> Keys {
    Keys {
        cell_key: [0x11; 32],
        dedup: [0x22; 32],
    }
}

pub fn sealer() -> CellSealer {
    CellSealer::new(&keys())
}

/// A sealed tree in its own data directory, with a composed `.cell/` directory
/// kept outside the tree.
pub struct Source {
    pub _temp: TempDir,
    pub data: PathBuf,
    pub tree: PathBuf,
    pub cell: PathBuf,
    pub head: ObjectId,
}

pub fn source() -> Source {
    let temp = tempfile::tempdir().unwrap();
    let data = temp.path().join("data-a");
    let tree = temp.path().join("tree-a");
    let cell = temp.path().join("cell-a");
    write_tree(&tree, &cell);
    let options = SealOptions {
        changed: None,
        overlay: Some(cell.clone()),
    };
    let (_, head) = FurrowRepository::attach_and_seal_in(
        &data,
        &tree,
        Some("fixture".to_owned()),
        SnapshotTrigger::Manual,
        options,
    )
    .unwrap();
    Source {
        _temp: temp,
        data,
        tree,
        cell,
        head,
    }
}

fn pseudo_random(len: usize) -> Vec<u8> {
    let mut state = 0x9e37_79b9_7f4a_7c15_u64;
    (0..len)
        .map(|_| {
            state = state
                .wrapping_mul(6364136223846793005)
                .wrapping_add(1442695040888963407);
            (state >> 33) as u8
        })
        .collect()
}

fn write_tree(tree: &Path, cell: &Path) {
    fs::create_dir_all(tree.join("src/deep")).unwrap();
    fs::create_dir_all(tree.join("empty")).unwrap();
    fs::write(tree.join("src/main.rs"), b"fn main() {}\n").unwrap();
    fs::write(tree.join("src/deep/notes.txt"), b"notes\n").unwrap();
    fs::write(tree.join("big.bin"), pseudo_random(3 << 20)).unwrap();
    fs::write(tree.join("run.sh"), b"#!/bin/sh\n").unwrap();
    fs::set_permissions(tree.join("run.sh"), fs::Permissions::from_mode(0o755)).unwrap();
    fs::set_permissions(tree.join("src/main.rs"), fs::Permissions::from_mode(0o640)).unwrap();
    symlink("src/main.rs", tree.join("link")).unwrap();
    symlink("nowhere", tree.join("dangling")).unwrap();
    let _ = xattr::set(tree.join("run.sh"), "user.exchange", b"kept");
    fs::create_dir_all(cell.join("sub")).unwrap();
    fs::write(cell.join("meta.json"), b"{\"v\":1}").unwrap();
    fs::write(cell.join("sub/note"), b"cell note").unwrap();
    stamp(tree, 1_600_000_000);
    stamp(cell, 1_500_000_000);
}

/// Gives every entry a distinct, old mtime, children before their directory.
fn stamp(dir: &Path, base: i64) {
    let mut seconds = base;
    let mut paths = Vec::new();
    collect(dir, &mut paths);
    paths.sort();
    for path in paths.iter().rev() {
        seconds += 7;
        let time = filetime::FileTime::from_unix_time(seconds, 123_000_000);
        filetime::set_symlink_file_times(path, time, time).unwrap();
    }
}

fn collect(dir: &Path, into: &mut Vec<PathBuf>) {
    into.push(dir.to_owned());
    for entry in fs::read_dir(dir).unwrap() {
        let path = entry.unwrap().path();
        if fs::symlink_metadata(&path).unwrap().is_dir() {
            collect(&path, into);
        } else {
            into.push(path);
        }
    }
}

pub fn export(source: &Source, ledger: &str, outbox: &Path, max_frame: usize) -> ExportReport {
    export_with(
        &source.data,
        source.head,
        &keys(),
        ledger,
        outbox,
        max_frame,
    )
    .unwrap()
}

pub fn export_with(
    data: &Path,
    head: ObjectId,
    keys: &Keys,
    ledger: &str,
    outbox: &Path,
    max_frame: usize,
) -> anyhow::Result<ExportReport> {
    let store = open_store(data)?;
    let ledger = Ledger::named(data, ledger)?;
    ExportJob {
        store: &store,
        keys,
        head,
        ledger: &ledger,
        outbox,
        max_frame,
    }
    .run()
}

pub fn frame_paths(report: &ExportReport) -> Vec<PathBuf> {
    report.frames.iter().map(|info| info.path.clone()).collect()
}

/// Every object of the frames, as the (rid, sealed bytes) a fetcher would
/// download one by one.
pub fn objects_of(frames: &[PathBuf]) -> Vec<(ObjectId, Vec<u8>)> {
    frames
        .iter()
        .flat_map(|path| {
            let bytes = fs::read(path).unwrap();
            let (_, objects) = frame::decode(&bytes).unwrap();
            objects
                .into_iter()
                .map(|o| (o.rid, o.bytes.to_vec()))
                .collect::<Vec<_>>()
        })
        .collect()
}

pub fn put_inbox(inbox: &Path, objects: &[(ObjectId, Vec<u8>)]) {
    fs::create_dir_all(inbox).unwrap();
    for (rid, bytes) in objects {
        fs::write(inbox.join(id_hex(rid)), bytes).unwrap();
    }
}

#[derive(Debug, PartialEq, Eq)]
pub struct Node {
    kind: &'static str,
    mode: u32,
    mtime: (i64, i64),
    size: u64,
    content: String,
    xattrs: Vec<(String, Vec<u8>)>,
}

/// Everything a byte-identical restore must reproduce, keyed by relative path.
pub fn dump(root: &Path) -> BTreeMap<String, Node> {
    let mut nodes = BTreeMap::new();
    nodes.insert(".".to_owned(), node_of(root));
    dump_into(root, root, &mut nodes);
    nodes
}

fn dump_into(root: &Path, dir: &Path, nodes: &mut BTreeMap<String, Node>) {
    for entry in fs::read_dir(dir).unwrap() {
        let path = entry.unwrap().path();
        let name = path.strip_prefix(root).unwrap().display().to_string();
        nodes.insert(name, node_of(&path));
        if fs::symlink_metadata(&path).unwrap().is_dir() {
            dump_into(root, &path, nodes);
        }
    }
}

fn node_of(path: &Path) -> Node {
    let meta = fs::symlink_metadata(path).unwrap();
    let (kind, content) = if meta.is_dir() {
        ("dir", String::new())
    } else if meta.file_type().is_symlink() {
        ("link", fs::read_link(path).unwrap().display().to_string())
    } else {
        (
            "file",
            blake3::hash(&fs::read(path).unwrap()).to_hex().to_string(),
        )
    };
    let mut xattrs: Vec<_> = xattr::list(path)
        .map(|names| {
            names
                .map(|n| {
                    (
                        n.to_string_lossy().into_owned(),
                        xattr::get(path, &n).unwrap().unwrap(),
                    )
                })
                .collect()
        })
        .unwrap_or_default();
    xattrs.sort();
    Node {
        kind,
        mode: meta.mode(),
        mtime: (meta.mtime(), meta.mtime_nsec()),
        size: meta.size(),
        content,
        xattrs,
    }
}

/// Every file under `dir`, for scans that look for bytes that must not be there.
pub fn all_files(dir: &Path) -> Vec<PathBuf> {
    let mut files = Vec::new();
    let mut stack = vec![dir.to_owned()];
    while let Some(next) = stack.pop() {
        for entry in fs::read_dir(&next).unwrap() {
            let path = entry.unwrap().path();
            if path.is_dir() {
                stack.push(path);
            } else {
                files.push(path);
            }
        }
    }
    files
}

/// The kind each rid of a source's head names.
pub fn kinds(
    data: &Path,
    head: ObjectId,
) -> std::collections::HashMap<ObjectId, furrow::model::ObjectKind> {
    use furrow::sealer::Sealer;
    let store = open_store(data).unwrap();
    furrow::exchange::survey::survey(&store, head, 1)
        .unwrap()
        .present
        .into_iter()
        .map(|(kind, id)| (sealer().remote_id(kind, &id), kind))
        .collect()
}

pub fn import_all(data: &Path, head: ObjectId, inbox: &Path) -> anyhow::Result<usize> {
    import_with(data, head, &keys(), inbox)
}

pub fn import_with(
    data: &Path,
    head: ObjectId,
    keys: &Keys,
    inbox: &Path,
) -> anyhow::Result<usize> {
    let store = open_store(data)?;
    let sealer = CellSealer::new(keys);
    let ledger = Ledger::named(data, LEDGER)?;
    furrow::exchange::fetch::Import {
        store: &store,
        sealer: &sealer,
        head,
        inbox,
        ledger: &ledger,
    }
    .run()
}

pub fn import_primed(data: &Path, head: ObjectId, inbox: &Path) -> anyhow::Result<Taken> {
    let store = open_store(data)?;
    let sealer = CellSealer::new(&keys());
    let ledger = Ledger::named(data, LEDGER)?;
    furrow::exchange::fetch::Import {
        store: &store,
        sealer: &sealer,
        head,
        inbox,
        ledger: &ledger,
    }
    .run_primed()
}

pub fn want_of(data: &Path, head: ObjectId) -> Vec<ObjectId> {
    let store = open_store(data).unwrap();
    furrow::exchange::fetch::want(&store, &sealer(), head).unwrap()
}
