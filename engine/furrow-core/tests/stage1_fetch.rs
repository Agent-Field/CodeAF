//! Stage 1 contract 5.2 and 5.4: `want`, `import` and `materialize`.

mod common;

use common::*;
use furrow::exchange::materialize::materialize;
use furrow::model::{id_hex, ObjectId, ObjectKind, SnapshotTrigger};
use furrow::repository::{FurrowRepository, SealOptions};
use std::collections::BTreeSet;
use std::fs;
use std::path::PathBuf;

const SMALL_FRAME: usize = 300 * 1024;

/// A receiving side: its own data directory and inbox.
struct Receiver {
    _temp: tempfile::TempDir,
    data: PathBuf,
    inbox: PathBuf,
    tree: PathBuf,
    cell: PathBuf,
}

fn receiver() -> Receiver {
    let temp = tempfile::tempdir().unwrap();
    let tree = temp.path().join("tree-b");
    fs::create_dir_all(&tree).unwrap();
    Receiver {
        data: temp.path().join("data-b"),
        inbox: temp.path().join("inbox"),
        cell: temp.path().join("cell-b"),
        tree,
        _temp: temp,
    }
}

fn exported(src: &Source) -> Vec<(ObjectId, Vec<u8>)> {
    let report = export(src, LEDGER, &src._temp.path().join("outbox"), SMALL_FRAME);
    objects_of(&frame_paths(&report))
}

fn of_kinds(
    objects: &[(ObjectId, Vec<u8>)],
    kinds: &std::collections::HashMap<ObjectId, ObjectKind>,
    wanted: &[ObjectKind],
) -> Vec<(ObjectId, Vec<u8>)> {
    let keep = |rid: &ObjectId| wanted.contains(&kinds[rid]);
    objects
        .iter()
        .filter(|(rid, _)| keep(rid))
        .cloned()
        .collect()
}

fn rids(objects: &[(ObjectId, Vec<u8>)]) -> BTreeSet<ObjectId> {
    objects.iter().map(|(rid, _)| *rid).collect()
}

const STRUCTURE: [ObjectKind; 3] = [ObjectKind::Snapshot, ObjectKind::Tree, ObjectKind::Xattrs];

#[test]
fn export_import_materialize_byte_identical() {
    let src = source();
    let objects = exported(&src);
    let dst = receiver();
    put_inbox(&dst.inbox, &objects);

    assert_eq!(
        import_all(&dst.data, src.head, &dst.inbox).unwrap(),
        objects.len()
    );
    assert!(want_of(&dst.data, src.head).is_empty());
    assert_eq!(
        fs::read_dir(&dst.inbox).unwrap().count(),
        0,
        "imported files are deleted"
    );

    let head = materialize(&dst.data, &dst.tree, Some(dst.cell.clone()), src.head).unwrap();
    assert_eq!(head, src.head);
    // A directory's own mtime is recorded in its parent's entry, so the root
    // folder (which has no parent entry) is the one node a snapshot cannot pin.
    let (mut restored, mut original) = (dump(&dst.tree), dump(&src.tree));
    restored.remove(".");
    original.remove(".");
    assert_eq!(
        restored, original,
        "the tree: modes, symlinks, mtimes, xattrs"
    );
    assert_eq!(
        dump(&dst.cell),
        dump(&src.cell),
        "the composed .cell/ directory"
    );
}

#[test]
fn materialize_without_a_cell_dir_puts_dot_cell_in_the_tree() {
    let src = source();
    let dst = receiver();
    put_inbox(&dst.inbox, &exported(&src));
    import_all(&dst.data, src.head, &dst.inbox).unwrap();
    materialize(&dst.data, &dst.tree, None, src.head).unwrap();

    let mut restored = dump(&dst.tree);
    restored.retain(|name, _| name != "." && !name.starts_with(".furrow"));
    let cell = dump(&src.cell);
    for (name, node) in cell {
        if name != "." {
            assert_eq!(
                restored.remove(&format!(".cell/{name}")),
                Some(node),
                "{name}"
            );
        }
    }
    restored.remove(".cell").expect("the .cell entry itself");
    let mut original = dump(&src.tree);
    original.remove(".");
    assert_eq!(restored, original);
}

#[test]
fn import_resumes_after_crash() {
    let src = source();
    let objects = exported(&src);
    let kinds = kinds(&src.data, src.head);
    let dst = receiver();

    // First run: the structure arrives and the process dies before the rest.
    let structure = of_kinds(&objects, &kinds, &STRUCTURE);
    put_inbox(&dst.inbox, &structure);
    assert_eq!(
        import_all(&dst.data, src.head, &dst.inbox).unwrap(),
        structure.len()
    );
    let blobs = of_kinds(&objects, &kinds, &[ObjectKind::Blob]);
    assert_eq!(
        BTreeSet::from_iter(want_of(&dst.data, src.head)),
        rids(&blobs)
    );

    // Second run: half the blobs arrive, together with a stale copy of an
    // object stored before the crash whose file was never deleted.
    let (first_half, rest) = blobs.split_at(blobs.len() / 2);
    put_inbox(&dst.inbox, first_half);
    put_inbox(&dst.inbox, &structure[..1]);
    assert_eq!(
        import_all(&dst.data, src.head, &dst.inbox).unwrap(),
        first_half.len()
    );
    assert_eq!(
        fs::read_dir(&dst.inbox).unwrap().count(),
        0,
        "the stale copy is dropped"
    );
    let wanted = BTreeSet::from_iter(want_of(&dst.data, src.head));
    assert!(
        rest.iter().all(|(rid, _)| wanted.contains(rid)),
        "the undelivered blobs are still wanted"
    );
    assert!(
        first_half.iter().all(|(rid, _)| !wanted.contains(rid)),
        "delivered blobs are not asked for again"
    );
    assert!(
        wanted.iter().any(|rid| kinds[rid] == ObjectKind::Chunk),
        "their chunks now are"
    );

    // Third run finishes the job.
    put_inbox(&dst.inbox, &objects);
    let remaining = objects.len() - structure.len() - first_half.len();
    assert_eq!(
        import_all(&dst.data, src.head, &dst.inbox).unwrap(),
        remaining
    );
    assert!(want_of(&dst.data, src.head).is_empty());
}

#[test]
fn tampered_object_refused() {
    let src = source();
    let objects = exported(&src);
    let kinds = kinds(&src.data, src.head);
    let dst = receiver();
    let snapshot_rid = *rids(&of_kinds(&objects, &kinds, &[ObjectKind::Snapshot]))
        .first()
        .unwrap();

    let mut tampered = objects.clone();
    let victim = tampered
        .iter_mut()
        .find(|(rid, _)| *rid == snapshot_rid)
        .unwrap();
    let middle = victim.1.len() / 2;
    victim.1[middle] ^= 1;
    put_inbox(&dst.inbox, &tampered);

    let error = import_all(&dst.data, src.head, &dst.inbox).unwrap_err();
    assert!(
        error.to_string().contains(&id_hex(&snapshot_rid)),
        "{error:#}"
    );
    assert_eq!(
        want_of(&dst.data, src.head),
        vec![snapshot_rid],
        "the store is unchanged"
    );
    assert!(dst.inbox.join(id_hex(&snapshot_rid)).exists());

    // The same file under the wrong key fails the same way.
    put_inbox(&dst.inbox, &objects);
    let wrong = furrow::sealer::Keys {
        cell_key: [0x33; 32],
        dedup: [0x22; 32],
    };
    let error = import_with(&dst.data, src.head, &wrong, &dst.inbox).unwrap_err();
    assert!(!error.to_string().is_empty());
    assert_eq!(want_of(&dst.data, src.head).len(), 1);
}

#[test]
fn a_tampered_child_keeps_the_objects_stored_before_it() {
    let src = source();
    let objects = exported(&src);
    let kinds = kinds(&src.data, src.head);
    let dst = receiver();
    let mut tampered = of_kinds(&objects, &kinds, &STRUCTURE);
    let tree = tampered
        .iter_mut()
        .find(|(rid, _)| kinds[rid] == ObjectKind::Tree)
        .unwrap();
    let (tree_rid, len) = (tree.0, tree.1.len());
    tree.1[len - 1] ^= 1;
    put_inbox(&dst.inbox, &tampered);

    let error = import_all(&dst.data, src.head, &dst.inbox).unwrap_err();
    assert!(error.to_string().contains(&id_hex(&tree_rid)), "{error:#}");
    let wanted = want_of(&dst.data, src.head);
    assert!(wanted.contains(&tree_rid), "the bad tree is still wanted");
    let snapshot = furrow::sealer::Sealer::remote_id(&sealer(), ObjectKind::Snapshot, &src.head);
    assert!(
        !wanted.contains(&snapshot),
        "the snapshot was stored before the failure"
    );
}

#[test]
fn unwanted_file_refused() {
    let src = source();
    let objects = exported(&src);
    let kinds = kinds(&src.data, src.head);
    let dst = receiver();
    let chunk = of_kinds(&objects, &kinds, &[ObjectKind::Chunk]).remove(0);
    put_inbox(&dst.inbox, std::slice::from_ref(&chunk));

    let error = import_all(&dst.data, src.head, &dst.inbox).unwrap_err();
    assert!(error.to_string().contains(&id_hex(&chunk.0)), "{error:#}");
    assert_eq!(want_of(&dst.data, src.head).len(), 1, "nothing was stored");
    assert!(
        dst.inbox.join(id_hex(&chunk.0)).exists(),
        "the file is left for the caller"
    );

    // A name that is not a rid is refused, a dot file is a download in flight.
    fs::remove_file(dst.inbox.join(id_hex(&chunk.0))).unwrap();
    fs::write(dst.inbox.join(".partial"), b"half").unwrap();
    assert_eq!(import_all(&dst.data, src.head, &dst.inbox).unwrap(), 0);
    fs::write(dst.inbox.join("readme.txt"), b"?").unwrap();
    let error = import_all(&dst.data, src.head, &dst.inbox).unwrap_err();
    assert!(error.to_string().contains("readme.txt"), "{error:#}");
}

#[test]
fn want_is_empty_when_complete() {
    let src = source();
    assert!(want_of(&src.data, src.head).is_empty());
    let dst = receiver();
    put_inbox(&dst.inbox, &exported(&src));
    import_all(&dst.data, src.head, &dst.inbox).unwrap();
    assert!(want_of(&dst.data, src.head).is_empty());
}

#[test]
fn want_starts_at_the_snapshot_and_is_bounded() {
    let src = source();
    let dst = receiver();
    let snapshot = furrow::sealer::Sealer::remote_id(&sealer(), ObjectKind::Snapshot, &src.head);
    assert_eq!(want_of(&dst.data, src.head), vec![snapshot]);

    let wide = wide_source(1200);
    let objects = exported(&wide);
    let kinds = kinds(&wide.data, wide.head);
    put_inbox(&dst.inbox, &of_kinds(&objects, &kinds, &STRUCTURE));
    import_all(&dst.data, wide.head, &dst.inbox).unwrap();
    let wanted = want_of(&dst.data, wide.head);
    assert_eq!(wanted.len(), 1000);
    assert!(wanted.iter().all(|rid| kinds[rid] == ObjectKind::Blob));
}

/// A tree of many small files: one blob and one chunk each.
fn wide_source(files: usize) -> Source {
    let temp = tempfile::tempdir().unwrap();
    let tree = temp.path().join("tree-w");
    fs::create_dir_all(&tree).unwrap();
    for index in 0..files {
        fs::write(tree.join(format!("f{index:05}")), format!("file {index}")).unwrap();
    }
    let (_, head) = FurrowRepository::attach_and_seal_in(
        &temp.path().join("data-w"),
        &tree,
        None,
        SnapshotTrigger::Manual,
        SealOptions::default(),
    )
    .unwrap();
    Source {
        data: temp.path().join("data-w"),
        cell: temp.path().join("cell-w"),
        tree,
        head,
        _temp: temp,
    }
}

#[test]
fn materialize_refuses_an_incomplete_head_and_leaves_the_folder_empty() {
    let src = source();
    let objects = exported(&src);
    let kinds = kinds(&src.data, src.head);
    let dst = receiver();
    put_inbox(&dst.inbox, &of_kinds(&objects, &kinds, &STRUCTURE));
    import_all(&dst.data, src.head, &dst.inbox).unwrap();

    assert!(materialize(&dst.data, &dst.tree, Some(dst.cell.clone()), src.head).is_err());
    assert_eq!(fs::read_dir(&dst.tree).unwrap().count(), 0);
}

/// A sends its second snapshot only; the first stays behind on A.
fn second_snapshot(src: &Source) -> (ObjectId, ObjectId) {
    let mut repo =
        FurrowRepository::open_composed_in(&src.data, &src.tree, Some(src.cell.clone())).unwrap();
    fs::write(
        src.tree.join("later.txt"),
        b"written after the first snapshot\n",
    )
    .unwrap();
    let second = repo
        .snapshot(Some("second".to_owned()), SnapshotTrigger::Manual)
        .unwrap();
    (src.head, second)
}

fn delivered(src: &Source, head: ObjectId) -> Receiver {
    let dst = receiver();
    let report = export_with(
        &src.data,
        head,
        &keys(),
        LEDGER,
        &src._temp.path().join("outbox"),
        SMALL_FRAME,
    )
    .unwrap();
    put_inbox(&dst.inbox, &objects_of(&frame_paths(&report)));
    import_all(&dst.data, head, &dst.inbox).unwrap();
    materialize(&dst.data, &dst.tree, Some(dst.cell.clone()), head).unwrap();
    dst
}

fn parent_of(data: &std::path::Path, id: &ObjectId) -> Option<ObjectId> {
    let store = furrow::exchange::open_store(data).unwrap();
    store
        .read_struct::<furrow::model::Snapshot>(id, ObjectKind::Snapshot)
        .unwrap()
        .parent
}

#[test]
fn the_next_seal_after_materialize_descends_from_the_imported_head() {
    let src = source();
    let (first, second) = second_snapshot(&src);
    let dst = delivered(&src, second);
    let mut repo =
        FurrowRepository::open_composed_in(&dst.data, &dst.tree, Some(dst.cell.clone())).unwrap();
    fs::write(dst.tree.join("on-b.txt"), b"continued here\n").unwrap();
    let next = repo.snapshot(None, SnapshotTrigger::Manual).unwrap();
    assert_eq!(parent_of(&dst.data, &next), Some(second));
    assert_ne!(Some(first), parent_of(&dst.data, &next));
}

#[test]
fn log_marks_the_imported_head_shallow() {
    let src = source();
    let (_, second) = second_snapshot(&src);
    let dst = delivered(&src, second);
    let repo =
        FurrowRepository::open_composed_in(&dst.data, &dst.tree, Some(dst.cell.clone())).unwrap();
    let log = repo.timeline(20).unwrap();
    assert_eq!(
        log[0].id,
        id_hex(&second),
        "the imported head is the newest entry"
    );
    assert!(log[0].shallow);
    assert!(
        log[1..].iter().all(|entry| !entry.shallow),
        "B's own snapshots have their parents"
    );
    let json = serde_json::to_value(&log).unwrap();
    assert_eq!(json[0]["shallow"], true);
    assert!(
        json[1].get("shallow").is_none(),
        "the field is additive: absent when false"
    );
}

#[test]
fn gc_keeps_everything_reachable_and_ignores_the_missing_parent() {
    let src = source();
    let (_, second) = second_snapshot(&src);
    let dst = delivered(&src, second);
    let mut repo =
        FurrowRepository::open_composed_in(&dst.data, &dst.tree, Some(dst.cell.clone())).unwrap();
    fs::write(dst.tree.join("on-b.txt"), b"continued here\n").unwrap();
    let next = repo.snapshot(None, SnapshotTrigger::Manual).unwrap();
    repo.gc(false).unwrap();
    assert!(
        want_of(&dst.data, second).is_empty(),
        "the adopted head is intact"
    );
    assert!(want_of(&dst.data, next).is_empty());
    assert!(repo
        .materialization(&second)
        .unwrap()
        .missing_paths
        .is_empty());
}

#[test]
fn restore_to_an_id_from_the_senders_older_history_refuses_cleanly() {
    let src = source();
    let (first, second) = second_snapshot(&src);
    let dst = delivered(&src, second);
    let repo =
        FurrowRepository::open_composed_in(&dst.data, &dst.tree, Some(dst.cell.clone())).unwrap();
    let before = dump(&dst.tree);
    let error = repo.resolve_snapshot(&id_hex(&first)).unwrap_err();
    assert!(error.to_string().contains(&id_hex(&first)), "{error:#}");
    assert_eq!(dump(&dst.tree), before, "nothing was restored");
}
