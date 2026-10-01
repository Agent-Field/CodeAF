//! A cold primed import explores the graph incrementally: each pass learns
//! only what the objects it stored opened up, instead of re-walking all of it.

mod common;

use common::*;
use furrow::exchange::open_store;
use furrow::exchange::survey::Walk;
use furrow::model::{ObjectKind, SnapshotTrigger};
use furrow::repository::{FurrowRepository, SealOptions};
use std::fs;

const DIRS: usize = 400;

fn wide_source() -> Source {
    let temp = tempfile::tempdir().unwrap();
    let data = temp.path().join("data-a");
    let tree = temp.path().join("tree-a");
    let cell = temp.path().join("cell-a");
    fs::create_dir_all(&cell).unwrap();
    for dir in 0..DIRS {
        let path = tree.join(format!("d{dir:05}"));
        fs::create_dir_all(&path).unwrap();
        for file in 0..3 {
            fs::write(
                path.join(format!("f{file}")),
                format!("content {dir} {file}"),
            )
            .unwrap();
        }
    }
    let options = SealOptions {
        changed: None,
        overlay: Some(cell.clone()),
    };
    let (_, head) = FurrowRepository::attach_and_seal_in(
        &data,
        &tree,
        Some("wide".to_owned()),
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

#[test]
fn wide_primed_import_takes_everything_and_leaves_nothing() {
    let src = wide_source();
    let report = export(&src, LEDGER, &src._temp.path().join("outbox"), 1 << 20);
    let objects = objects_of(&frame_paths(&report));
    let inbox = src._temp.path().join("inbox");
    put_inbox(&inbox, &objects);
    let data = src._temp.path().join("data-b");

    let taken = import_primed(&data, src.head, &inbox).unwrap();

    assert_eq!(taken.imported, objects.len());
    assert_eq!(taken.extras_deleted, 0);
    assert!(want_of(&data, src.head).is_empty());
    assert_eq!(fs::read_dir(&inbox).unwrap().count(), 0);
}

#[test]
fn walk_reports_each_missing_object_once_and_follows_expand() {
    let src = wide_source();
    let src_store = open_store(&src.data).unwrap();
    let snapshot = src_store
        .read_bytes(&src.head, ObjectKind::Snapshot)
        .unwrap();
    let empty = tempfile::tempdir().unwrap();
    let store = open_store(empty.path()).unwrap();

    let mut walk = Walk::new(src.head);
    let first = walk.explore(&store, usize::MAX).unwrap();
    assert_eq!(first.missing, vec![(ObjectKind::Snapshot, src.head)]);
    assert!(
        walk.explore(&store, usize::MAX).unwrap().missing.is_empty(),
        "an object already reported is not listed again"
    );

    walk.expand(ObjectKind::Snapshot, &snapshot).unwrap();
    let next = walk.explore(&store, usize::MAX).unwrap();
    assert_eq!(
        next.missing.len(),
        1,
        "only the snapshot's root tree is new"
    );
    assert_eq!(next.missing[0].0, ObjectKind::Tree);
}
