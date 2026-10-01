//! A warm `materialize` restores a head over a tree that already holds an
//! older one, and writes only what differs between the two. The staging folder
//! of a take is such a tree: it starts as a copy of the root made of hard
//! links, so the engine must never write into a file that exists.

mod common;

use common::*;
use furrow::exchange::materialize::materialize;
use furrow::model::{ObjectId, SnapshotTrigger};
use furrow::repository::FurrowRepository;
use std::fs;
use std::os::unix::fs::{symlink, MetadataExt, PermissionsExt};
use std::path::{Path, PathBuf};

/// Seals the source tree as it is now.
fn seal(src: &Source) -> ObjectId {
    let mut repo =
        FurrowRepository::open_composed_in(&src.data, &src.tree, Some(src.cell.clone())).unwrap();
    repo.snapshot(None, SnapshotTrigger::Manual).unwrap()
}

/// Brings `head` and everything it needs to the receiver's store.
fn deliver(src: &Source, dst: &Receiver, head: ObjectId, ledger: &str) {
    let outbox = src._temp.path().join(format!("outbox-{head:?}"));
    let report = export_with(&src.data, head, &keys(), ledger, &outbox, 300 * 1024).unwrap();
    put_inbox(&dst.inbox, &objects_of(&frame_paths(&report)));
    import_all(&dst.data, head, &dst.inbox).unwrap();
}

struct Receiver {
    _temp: tempfile::TempDir,
    data: PathBuf,
    inbox: PathBuf,
    tree: PathBuf,
    cell: PathBuf,
}

/// A receiver that holds `first` on disk, and the source moved on to `second`
/// by `change`.
fn warm(change: impl FnOnce(&Path)) -> (Source, Receiver, ObjectId) {
    let src = source();
    fs::write(src.tree.join("keep.txt"), b"never changes\n").unwrap();
    let first = seal(&src);
    let temp = tempfile::tempdir().unwrap();
    let dst = Receiver {
        data: temp.path().join("data-b"),
        inbox: temp.path().join("inbox"),
        tree: temp.path().join("tree-b"),
        cell: temp.path().join("cell-b"),
        _temp: temp,
    };
    fs::create_dir_all(&dst.tree).unwrap();
    deliver(&src, &dst, first, "0000000000000001");
    materialize(&dst.data, &dst.tree, Some(dst.cell.clone()), first).unwrap();
    change(&src.tree);
    let second = seal(&src);
    deliver(&src, &dst, second, "0000000000000001");
    (src, dst, second)
}

/// The staging folder of a take: directories made anew, files hard-linked.
fn linked_copy(from: &Path, to: &Path) {
    fs::create_dir_all(to).unwrap();
    for entry in fs::read_dir(from).unwrap() {
        let path = entry.unwrap().path();
        let target = to.join(path.file_name().unwrap());
        let meta = fs::symlink_metadata(&path).unwrap();
        if meta.is_dir() {
            linked_copy(&path, &target);
        } else if meta.file_type().is_symlink() {
            symlink(fs::read_link(&path).unwrap(), &target).unwrap();
        } else {
            fs::hard_link(&path, &target).unwrap();
        }
    }
}

fn inode(path: impl AsRef<Path>) -> u64 {
    fs::symlink_metadata(path).unwrap().ino()
}

fn without_root(
    mut nodes: std::collections::BTreeMap<String, Node>,
) -> std::collections::BTreeMap<String, Node> {
    nodes.remove(".");
    nodes
}

#[test]
fn a_one_file_change_writes_one_file() {
    let (_src, dst, second) =
        warm(|tree| fs::write(tree.join("src/main.rs"), b"fn main() { 1 }\n").unwrap());
    let files = [
        "keep.txt",
        "big.bin",
        "run.sh",
        "src/deep/notes.txt",
        "src/main.rs",
    ];
    let before: Vec<u64> = files.iter().map(|f| inode(dst.tree.join(f))).collect();

    materialize(&dst.data, &dst.tree, Some(dst.cell.clone()), second).unwrap();

    let after: Vec<u64> = files.iter().map(|f| inode(dst.tree.join(f))).collect();
    let rewritten: Vec<_> = files
        .iter()
        .zip(before.iter().zip(&after))
        .filter(|(_, (b, a))| b != a)
        .map(|(f, _)| *f)
        .collect();
    assert_eq!(rewritten, ["src/main.rs"]);
}

#[test]
fn a_take_of_the_head_already_held_writes_nothing() {
    let (_src, dst, _) = warm(|_| {});
    let held = FurrowRepository::open_composed_in(&dst.data, &dst.tree, Some(dst.cell.clone()))
        .unwrap()
        .resolve_snapshot("head")
        .unwrap();
    let before = without_root(dump(&dst.tree));
    let inodes: Vec<u64> = all_files(&dst.tree).iter().map(inode).collect();

    materialize(&dst.data, &dst.tree, Some(dst.cell.clone()), held).unwrap();

    assert_eq!(without_root(dump(&dst.tree)), before);
    assert_eq!(
        all_files(&dst.tree).iter().map(inode).collect::<Vec<_>>(),
        inodes
    );
}

#[test]
fn deletes_renames_and_mode_only_changes_reach_a_linked_copy_and_spare_the_original() {
    let (src, dst, second) = warm(|tree| {
        fs::remove_file(tree.join("src/deep/notes.txt")).unwrap();
        fs::rename(tree.join("big.bin"), tree.join("moved.bin")).unwrap();
        fs::set_permissions(tree.join("run.sh"), fs::Permissions::from_mode(0o700)).unwrap();
    });
    let original = without_root(dump(&dst.tree));
    let stage = dst._temp.path().join("tree-b.taking");
    linked_copy(&dst.tree, &stage);

    materialize(&dst.data, &stage, Some(dst.cell.clone()), second).unwrap();

    assert_eq!(
        without_root(dump(&stage)),
        without_root(dump(&src.tree)),
        "the copy holds the new head"
    );
    assert_eq!(
        without_root(dump(&dst.tree)),
        original,
        "the original tree is byte, mode and time identical"
    );
    assert_eq!(
        inode(stage.join("keep.txt")),
        inode(dst.tree.join("keep.txt")),
        "an unchanged file stays one file"
    );
    assert_ne!(
        inode(stage.join("run.sh")),
        inode(dst.tree.join("run.sh")),
        "a mode change made a new file"
    );
}
