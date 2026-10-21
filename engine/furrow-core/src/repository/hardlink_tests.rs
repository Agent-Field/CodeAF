//! Hard links of a folder: a snapshot records which paths were one file, and a
//! restore makes them one file again without ever tying them to a file that
//! lives outside the tree.

use super::seal_tests::DATA_DIR;
use super::*;
use std::os::unix::fs::MetadataExt;

struct Folder {
    _guard: std::sync::MutexGuard<'static, ()>,
    temp: tempfile::TempDir,
    root: PathBuf,
}

impl Folder {
    fn new() -> Self {
        let guard = DATA_DIR.lock().unwrap_or_else(|poison| poison.into_inner());
        let temp = tempfile::tempdir().unwrap();
        std::env::set_var("FURROW_DATA_DIR", temp.path().join("data"));
        let root = temp.path().join("work");
        fs::create_dir_all(&root).unwrap();
        Self {
            _guard: guard,
            temp,
            root,
        }
    }

    fn write(&self, relative: &str, body: &str) {
        let path = self.root.join(relative);
        fs::create_dir_all(path.parent().unwrap()).unwrap();
        fs::write(path, body).unwrap();
    }

    fn link(&self, from: &str, to: &str) {
        let path = self.root.join(to);
        fs::create_dir_all(path.parent().unwrap()).unwrap();
        fs::hard_link(self.root.join(from), path).unwrap();
    }

    fn inode(&self, relative: &str) -> u64 {
        fs::metadata(self.root.join(relative)).unwrap().ino()
    }

    fn seal(&self) -> (FurrowRepository, ObjectId) {
        FurrowRepository::attach_and_seal(
            &self.root,
            Some("test".to_owned()),
            SnapshotTrigger::AgentRun,
            SealOptions::default(),
        )
        .unwrap()
    }

    /// Empties the folder the way a machine that never had the files is empty.
    fn clear(&self) {
        for child in fs::read_dir(&self.root).unwrap() {
            let path = child.unwrap().path();
            if path.file_name() != Some(OsStr::new(".furrow")) {
                remove_path(&path).unwrap();
            }
        }
    }
}

fn groups_of(repository: &FurrowRepository, id: &ObjectId) -> Vec<Vec<String>> {
    let snapshot: Snapshot = repository
        .store
        .read_struct(id, ObjectKind::Snapshot)
        .unwrap();
    snapshot
        .hardlinks
        .iter()
        .map(|group| {
            group
                .paths
                .iter()
                .map(|path| String::from_utf8(path.to_vec()).unwrap())
                .collect()
        })
        .collect()
}

#[test]
fn a_snapshot_records_the_paths_that_are_one_file_and_no_others() {
    let folder = Folder::new();
    folder.write("a.txt", "shared\n");
    folder.link("a.txt", "deep/b.txt");
    folder.write("lone.txt", "alone\n");
    folder.write("other.txt", "second\n");
    folder.link("other.txt", "other-too.txt");
    folder.link("other.txt", "z/other-three.txt");

    let (repository, id) = folder.seal();

    assert_eq!(
        groups_of(&repository, &id),
        vec![
            vec!["a.txt", "deep/b.txt"],
            vec!["other-too.txt", "other.txt", "z/other-three.txt"],
        ]
    );
}

#[test]
fn a_file_shared_only_with_something_outside_the_tree_is_not_a_group() {
    let folder = Folder::new();
    let outside = folder.temp.path().join("seed-source.txt");
    fs::write(&outside, "seeded\n").unwrap();
    fs::hard_link(&outside, folder.root.join("seeded.txt")).unwrap();
    folder.write("plain.txt", "plain\n");

    let (repository, id) = folder.seal();

    assert_eq!(fs::metadata(&outside).unwrap().nlink(), 2);
    assert!(groups_of(&repository, &id).is_empty());
}

#[test]
fn a_snapshot_without_links_serialises_exactly_as_before_the_field_existed() {
    let folder = Folder::new();
    folder.write("a.txt", "one\n");
    let (repository, id) = folder.seal();
    let bytes = repository
        .store
        .read_bytes(&id, ObjectKind::Snapshot)
        .unwrap();

    assert!(!String::from_utf8_lossy(&bytes).contains("hardlinks"));
    let snapshot: Snapshot = serde_json::from_slice(&bytes).unwrap();
    assert!(snapshot.hardlinks.is_empty());
}

#[test]
fn a_restore_makes_the_recorded_paths_one_file_again() {
    let folder = Folder::new();
    folder.write("a.txt", "shared\n");
    folder.link("a.txt", "deep/b.txt");
    folder.write("lone.txt", "alone\n");
    let (repository, id) = folder.seal();
    folder.clear();
    drop(repository);

    let mut repository = FurrowRepository::open(&folder.root).unwrap();
    repository.rewind(&id, &[], false).unwrap();

    assert_eq!(folder.inode("a.txt"), folder.inode("deep/b.txt"));
    assert_ne!(folder.inode("a.txt"), folder.inode("lone.txt"));
    let metadata = fs::metadata(folder.root.join("a.txt")).unwrap();
    assert_eq!(metadata.nlink(), 2);
    assert_eq!(
        fs::read_to_string(folder.root.join("deep/b.txt")).unwrap(),
        "shared\n"
    );
}

#[test]
fn a_restore_links_members_that_were_already_there_as_separate_copies() {
    let folder = Folder::new();
    folder.write("a.txt", "shared\n");
    folder.link("a.txt", "b.txt");
    let (repository, id) = folder.seal();
    // The other machine holds the same bytes, with the same times, as two files.
    let metadata = fs::metadata(folder.root.join("a.txt")).unwrap();
    fs::remove_file(folder.root.join("b.txt")).unwrap();
    fs::copy(folder.root.join("a.txt"), folder.root.join("b.txt")).unwrap();
    let mtime = FileTime::from_last_modification_time(&metadata);
    filetime::set_file_mtime(folder.root.join("b.txt"), mtime).unwrap();
    assert_ne!(folder.inode("a.txt"), folder.inode("b.txt"));
    drop(repository);

    let mut repository = FurrowRepository::open(&folder.root).unwrap();
    repository.rewind(&id, &[], false).unwrap();

    assert_eq!(folder.inode("a.txt"), folder.inode("b.txt"));
}

#[test]
fn a_restore_never_writes_through_the_file_it_shares_with_the_outside() {
    let folder = Folder::new();
    folder.write("a.txt", "new\n");
    folder.link("a.txt", "b.txt");
    let (repository, id) = folder.seal();
    folder.clear();
    // A seeded folder: its files are links to the previous folder's files.
    let previous = folder.temp.path().join("previous-a.txt");
    fs::write(&previous, "old\n").unwrap();
    fs::hard_link(&previous, folder.root.join("a.txt")).unwrap();
    drop(repository);

    let mut repository = FurrowRepository::open(&folder.root).unwrap();
    repository.rewind(&id, &[], false).unwrap();

    assert_eq!(fs::read_to_string(&previous).unwrap(), "old\n");
    assert_eq!(folder.inode("a.txt"), folder.inode("b.txt"));
    assert_ne!(
        folder.inode("a.txt"),
        fs::metadata(&previous).unwrap().ino()
    );
    assert_eq!(
        fs::read_to_string(folder.root.join("b.txt")).unwrap(),
        "new\n"
    );
}

#[test]
fn a_changed_path_seal_follows_a_link_made_and_a_link_removed() {
    let folder = Folder::new();
    folder.write("a.txt", "one\n");
    folder.write("b.txt", "two\n");
    let (mut repository, first) = folder.seal();
    assert!(groups_of(&repository, &first).is_empty());

    fs::remove_file(folder.root.join("b.txt")).unwrap();
    folder.link("a.txt", "b.txt");
    let linked = repository
        .snapshot_changed_paths(None, SnapshotTrigger::Watcher, &[PathBuf::from("b.txt")])
        .unwrap();
    assert_eq!(
        groups_of(&repository, &linked),
        vec![vec!["a.txt", "b.txt"]]
    );

    fs::remove_file(folder.root.join("b.txt")).unwrap();
    let unlinked = repository
        .snapshot_changed_paths(None, SnapshotTrigger::Watcher, &[PathBuf::from("b.txt")])
        .unwrap();
    assert!(groups_of(&repository, &unlinked).is_empty());
}
