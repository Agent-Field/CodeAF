//! Sealing a plain folder: no Git, a stat cache that spares unchanged files,
//! changed-path seals, and a composed `.cell/` directory that restores apart.

use super::*;
use std::sync::Mutex;

/// The store location comes from the environment, so tests that set it take turns.
static DATA_DIR: Mutex<()> = Mutex::new(());

struct Setup {
    _guard: std::sync::MutexGuard<'static, ()>,
    _temp: tempfile::TempDir,
    root: PathBuf,
    cell: PathBuf,
}

impl Setup {
    fn new() -> Self {
        let guard = DATA_DIR.lock().unwrap_or_else(|poison| poison.into_inner());
        let temp = tempfile::tempdir().unwrap();
        std::env::set_var("FURROW_DATA_DIR", temp.path().join("data"));
        let root = temp.path().join("work");
        let cell = temp.path().join("private");
        fs::create_dir_all(&root).unwrap();
        Self {
            _guard: guard,
            _temp: temp,
            root,
            cell,
        }
    }

    fn write(&self, relative: &str, body: &str) {
        let path = self.root.join(relative);
        fs::create_dir_all(path.parent().unwrap()).unwrap();
        fs::write(path, body).unwrap();
    }

    fn write_cell(&self, relative: &str, body: &str) {
        let path = self.cell.join(relative);
        fs::create_dir_all(path.parent().unwrap()).unwrap();
        fs::write(path, body).unwrap();
    }

    fn seal(&self, options: SealOptions) -> (FurrowRepository, ObjectId) {
        FurrowRepository::attach_and_seal(
            &self.root,
            Some("test".to_owned()),
            SnapshotTrigger::AgentRun,
            options,
        )
        .unwrap()
    }

    fn overlay(&self) -> SealOptions {
        SealOptions {
            overlay: Some(self.cell.clone()),
            ..SealOptions::default()
        }
    }
}

fn changed(paths: &[&str]) -> SealOptions {
    SealOptions {
        changed: Some(paths.iter().map(PathBuf::from).collect()),
        ..SealOptions::default()
    }
}

fn blob_at(repository: &FurrowRepository, snapshot: &ObjectId, path: &str) -> Option<ObjectId> {
    let snapshot: Snapshot = repository
        .store
        .read_struct(snapshot, ObjectKind::Snapshot)
        .unwrap();
    repository
        .lookup_tree_path(&snapshot.root_tree, path.as_bytes())
        .unwrap()
        .and_then(|entry| entry.target)
}

fn root_tree(repository: &FurrowRepository, snapshot: &ObjectId) -> ObjectId {
    let snapshot: Snapshot = repository
        .store
        .read_struct(snapshot, ObjectKind::Snapshot)
        .unwrap();
    snapshot.root_tree
}

/// Every path under `directory` with its mode and content, except the
/// engine's own `.furrow` directory.
fn digest(directory: &Path) -> Vec<String> {
    let mut lines = Vec::new();
    collect(directory, directory, &mut lines);
    lines.sort();
    lines
}

fn collect(base: &Path, directory: &Path, lines: &mut Vec<String>) {
    let Ok(children) = fs::read_dir(directory) else {
        return;
    };
    for child in children {
        let path = child.unwrap().path();
        let relative = path.strip_prefix(base).unwrap().display().to_string();
        if relative == ".furrow" {
            continue;
        }
        let metadata = fs::symlink_metadata(&path).unwrap();
        if metadata.is_dir() {
            lines.push(format!("{relative}/ {:o}", metadata.permissions().mode()));
            collect(base, &path, lines);
        } else {
            let body = fs::read(&path).unwrap();
            lines.push(format!(
                "{relative} {:o} {}",
                metadata.permissions().mode(),
                hex::encode(blake3::hash(&body).as_bytes())
            ));
        }
    }
}

#[test]
fn seals_a_folder_that_is_not_a_git_repository() {
    let setup = Setup::new();
    setup.write("src/main.txt", "hello\n");
    assert!(!setup.root.join(".git").exists());

    let (repository, id) = setup.seal(SealOptions::default());

    assert!(blob_at(&repository, &id, "src/main.txt").is_some());
    assert!(!setup.root.join(".git").exists());
}

#[test]
fn a_file_untouched_since_it_was_read_is_not_read_again() {
    let setup = Setup::new();
    setup.write("old.txt", "old\n");
    // Older than the timestamp granularity by the time it is first read.
    std::thread::sleep(std::time::Duration::from_millis(
        (crate::catalog::RACY_WINDOW_NANOS / 1_000_000 + 200) as u64,
    ));
    setup.write("fresh.txt", "fresh\n");
    let (repository, _) = setup.seal(SealOptions::default());
    let cached = |name: &str| {
        repository
            .store
            .cached_file(&repository.workspace_id, name.as_bytes())
            .unwrap()
            .unwrap()
    };
    let (old, fresh) = (cached("old.txt"), cached("fresh.txt"));
    assert!(!old.is_racy(), "a file older than the window is trusted");
    assert!(fresh.is_racy(), "a file touched inside the window is not");

    let (repository, _) = setup.seal(SealOptions::default());
    let again = |name: &str| {
        repository
            .store
            .cached_file(&repository.workspace_id, name.as_bytes())
            .unwrap()
            .unwrap()
    };
    assert_eq!(again("old.txt").cached_at_nanos, old.cached_at_nanos);
    assert!(
        again("fresh.txt").cached_at_nanos > fresh.cached_at_nanos,
        "a racily clean file is read again"
    );
}

#[test]
fn a_changed_file_is_captured_and_an_unchanged_one_is_not_revisited() {
    let setup = Setup::new();
    setup.write("a.txt", "one\n");
    setup.write("b.txt", "one\n");
    let (repository, first) = setup.seal(SealOptions::default());
    let before = blob_at(&repository, &first, "a.txt");

    setup.write("a.txt", "two\n");
    setup.write("b.txt", "two\n");
    let (repository, second) = setup.seal(changed(&["a.txt"]));

    assert_ne!(blob_at(&repository, &second, "a.txt"), before);
    assert_eq!(
        blob_at(&repository, &second, "b.txt"),
        blob_at(&repository, &first, "b.txt"),
        "a path that was not listed is not visited"
    );
}

#[test]
fn an_empty_changed_list_means_nothing_changed() {
    let setup = Setup::new();
    setup.write("a.txt", "one\n");
    let (repository, first) = setup.seal(SealOptions::default());
    setup.write("a.txt", "two\n");

    let (repository_again, second) = setup.seal(changed(&[]));

    assert_eq!(
        root_tree(&repository, &first),
        root_tree(&repository_again, &second)
    );
}

#[test]
fn created_and_deleted_paths_follow_the_changed_list() {
    let setup = Setup::new();
    setup.write("keep.txt", "k\n");
    setup.write("gone.txt", "g\n");
    setup.seal(SealOptions::default());

    setup.write("dir/new.txt", "n\n");
    fs::remove_file(setup.root.join("gone.txt")).unwrap();
    let (repository, id) = setup.seal(changed(&["dir", "gone.txt"]));

    assert!(blob_at(&repository, &id, "dir/new.txt").is_some());
    assert!(blob_at(&repository, &id, "gone.txt").is_none());
    assert!(blob_at(&repository, &id, "keep.txt").is_some());
}

#[test]
fn a_database_is_found_without_opening_every_file() {
    let setup = Setup::new();
    let database = setup.root.join("state.db");
    let connection = rusqlite::Connection::open(&database).unwrap();
    connection
        .execute_batch("CREATE TABLE t(x); INSERT INTO t VALUES (1);")
        .unwrap();
    connection.close().map_err(|(_, error)| error).unwrap();
    setup.write("plain.txt", "not a database\n");

    let (repository, id) = setup.seal(SealOptions::default());

    let snapshot: Snapshot = repository
        .store
        .read_struct(&id, ObjectKind::Snapshot)
        .unwrap();
    let paths: Vec<_> = snapshot
        .sqlite_backups
        .iter()
        .map(|backup| backup.path.clone())
        .collect();
    assert_eq!(paths, [b"state.db".to_vec()]);
}

#[test]
fn the_composed_directory_is_sealed_as_dot_cell_beside_the_workspace_files() {
    let setup = Setup::new();
    setup.write("a.txt", "a\n");
    setup.write(".b", "sorts before the overlay\n");
    setup.write("z.txt", "z\n");
    setup.write_cell("turns/1.json", "{}\n");

    let (repository, id) = setup.seal(setup.overlay());

    assert!(blob_at(&repository, &id, ".cell/turns/1.json").is_some());
    assert!(blob_at(&repository, &id, "a.txt").is_some());
    assert!(
        !setup.root.join(".cell").exists(),
        "nothing lands in the workspace"
    );
    let tree = root_tree(&repository, &id);
    let mut names = Vec::new();
    tree::for_each_entry(&repository.store, &tree, |entry| {
        names.push(String::from_utf8(entry.name).unwrap());
        Ok(())
    })
    .unwrap();
    assert_eq!(names, [".b", ".cell", "a.txt", "z.txt"]);
}

#[test]
fn a_changed_path_seal_always_recomposes_the_directory() {
    let setup = Setup::new();
    setup.write("a.txt", "a\n");
    setup.write_cell("turns/1.json", "one\n");
    let (repository, first) = setup.seal(setup.overlay());
    let before = blob_at(&repository, &first, ".cell/turns/1.json");

    setup.write_cell("turns/1.json", "two\n");
    setup.write_cell("turns/2.json", "new\n");
    let options = SealOptions {
        changed: Some(Vec::new()),
        ..setup.overlay()
    };
    let (repository, second) = setup.seal(options);

    assert_ne!(blob_at(&repository, &second, ".cell/turns/1.json"), before);
    assert!(blob_at(&repository, &second, ".cell/turns/2.json").is_some());
}

#[test]
fn the_composed_tree_restores_byte_identical_into_two_places() {
    let setup = Setup::new();
    setup.write("src/main.txt", "package main\n");
    setup.write("empty", "");
    setup.write_cell("turns/1.json", "{\"n\":1}\n");
    setup.write_cell("receipts/a.json", "{}\n");
    fs::set_permissions(
        setup.cell.join("receipts/a.json"),
        fs::Permissions::from_mode(0o600),
    )
    .unwrap();
    let (mut repository, id) = setup.seal(setup.overlay());
    let (work, private) = (digest(&setup.root), digest(&setup.cell));

    fs::remove_dir_all(setup.root.join("src")).unwrap();
    setup.write("stray.txt", "unsealed\n");
    fs::remove_dir_all(setup.cell.join("turns")).unwrap();
    setup.write_cell("receipts/extra.json", "unsealed\n");
    fs::write(setup.cell.join("receipts/a.json"), "damaged").unwrap();
    repository = repository.with_overlay(Some(setup.cell.clone()));
    repository.rewind(&id, &[], false).unwrap();

    assert_eq!(digest(&setup.root), work);
    assert_eq!(digest(&setup.cell), private);
    assert!(!setup.root.join(".cell").exists());
}

#[test]
fn a_composed_directory_that_did_not_exist_at_the_seal_is_emptied_on_restore() {
    let setup = Setup::new();
    setup.write("a.txt", "a\n");
    let (repository, id) = setup.seal(SealOptions::default());
    setup.write_cell("late.json", "{}\n");

    let mut repository = repository.with_overlay(Some(setup.cell.clone()));
    repository.rewind(&id, &[], false).unwrap();

    assert!(digest(&setup.cell).is_empty());
}
