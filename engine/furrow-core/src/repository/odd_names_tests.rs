//! Restoring folders that are awkward to write: read-only directories, and
//! names that the file system being written folds into one.
//!
//! A Linux test cannot meet a case-insensitive file system, so the folding tests
//! switch the probe with `FURROW_TEST_NAME_FOLDING` and restore as a Mac would.

use super::seal_tests::DATA_DIR;
use super::*;

struct Folder {
    _guard: std::sync::MutexGuard<'static, ()>,
    _temp: tempfile::TempDir,
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
            _temp: temp,
            root,
        }
    }

    fn write(&self, relative: &str, body: &str) {
        let path = self.root.join(relative);
        fs::create_dir_all(path.parent().unwrap()).unwrap();
        fs::write(path, body).unwrap();
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

    fn read(&self, relative: &str) -> Option<String> {
        fs::read_to_string(self.root.join(relative)).ok()
    }

    fn mode(&self, relative: &str) -> u32 {
        fs::symlink_metadata(self.root.join(relative))
            .unwrap()
            .permissions()
            .mode()
            & 0o7777
    }

    /// Empties the folder the way a machine that never had the files is empty.
    fn clear(&self) {
        for child in fs::read_dir(&self.root).unwrap() {
            let path = child.unwrap().path();
            if path.file_name() == Some(OsStr::new(".furrow")) {
                continue;
            }
            make_writable(&path);
            remove_path(&path).unwrap();
        }
    }
}

fn make_writable(path: &Path) {
    if path.is_dir() {
        fs::set_permissions(path, fs::Permissions::from_mode(0o755)).unwrap();
        for child in fs::read_dir(path).unwrap() {
            make_writable(&child.unwrap().path());
        }
    }
}

/// Runs `body` as a restore onto a file system that folds names this way.
fn folding(spec: &str, body: impl FnOnce()) {
    std::env::set_var(crate::name_fold::TEST_ENV, spec);
    let outcome = std::panic::catch_unwind(std::panic::AssertUnwindSafe(body));
    std::env::remove_var(crate::name_fold::TEST_ENV);
    if let Err(panic) = outcome {
        std::panic::resume_unwind(panic);
    }
}

#[test]
fn a_read_only_directory_with_files_is_restored_and_left_read_only() {
    let folder = Folder::new();
    folder.write("ro/inside.txt", "kept\n");
    folder.write("ro/deep/more.txt", "deeper\n");
    fs::set_permissions(
        folder.root.join("ro/deep"),
        fs::Permissions::from_mode(0o555),
    )
    .unwrap();
    fs::set_permissions(folder.root.join("ro"), fs::Permissions::from_mode(0o555)).unwrap();
    let (repository, id) = folder.seal();
    folder.clear();
    drop(repository);

    let mut repository = FurrowRepository::open(&folder.root).unwrap();
    repository.rewind(&id, &[], false).unwrap();

    assert_eq!(folder.read("ro/inside.txt").as_deref(), Some("kept\n"));
    assert_eq!(folder.read("ro/deep/more.txt").as_deref(), Some("deeper\n"));
    assert_eq!(folder.mode("ro"), 0o555);
    assert_eq!(folder.mode("ro/deep"), 0o555);
    make_writable(&folder.root.join("ro"));
}

#[test]
fn names_that_differ_only_in_case_restore_as_one_copy_and_name_the_other() {
    let folder = Folder::new();
    folder.write("Readme.md", "lower\n");
    folder.write("README.md", "upper\n");
    let (repository, id) = folder.seal();
    folder.clear();
    drop(repository);

    folding("case", || {
        let mut repository = FurrowRepository::open(&folder.root).unwrap();
        let (_, plan) = repository.rewind(&id, &[], false).unwrap();

        assert_eq!(folder.read("README.md").as_deref(), Some("upper\n"));
        assert_eq!(folder.read("Readme.md"), None);
        assert_eq!(plan.held.len(), 1);
        assert_eq!(plan.held[0].path, "Readme.md");
        assert_eq!(plan.held[0].reason, "same name as README.md here");
    });
}

#[test]
fn composed_and_decomposed_spellings_restore_as_one_copy() {
    let folder = Folder::new();
    folder.write("caf\u{e9}.txt", "composed\n");
    folder.write("cafe\u{301}.txt", "decomposed\n");
    let (repository, id) = folder.seal();
    folder.clear();
    drop(repository);

    folding("unicode", || {
        let mut repository = FurrowRepository::open(&folder.root).unwrap();
        let (_, plan) = repository.rewind(&id, &[], false).unwrap();

        // The decomposed spelling sorts first byte for byte, so it is kept.
        assert_eq!(
            folder.read("cafe\u{301}.txt").as_deref(),
            Some("decomposed\n")
        );
        assert_eq!(folder.read("caf\u{e9}.txt"), None);
        assert_eq!(plan.held.len(), 1);
        assert_eq!(plan.held[0].path, "caf\u{e9}.txt");
    });
}

#[test]
fn a_held_directory_takes_its_contents_with_it() {
    let folder = Folder::new();
    folder.write("Docs/a.md", "lower dir\n");
    folder.write("DOCS/b.md", "upper dir\n");
    let (repository, id) = folder.seal();
    folder.clear();
    drop(repository);

    folding("case", || {
        let mut repository = FurrowRepository::open(&folder.root).unwrap();
        let (_, plan) = repository.rewind(&id, &[], false).unwrap();

        assert_eq!(folder.read("DOCS/b.md").as_deref(), Some("upper dir\n"));
        assert_eq!(folder.read("DOCS/a.md"), None);
        assert_eq!(
            plan.held
                .iter()
                .map(|held| held.path.as_str())
                .collect::<Vec<_>>(),
            ["Docs"]
        );
    });
}

#[test]
fn a_twin_already_on_disk_is_not_overwritten_by_a_newly_arrived_name() {
    let folder = Folder::new();
    folder.write("README.md", "upper\n");
    let (repository, first) = folder.seal();
    folder.write("Readme.md", "lower\n");
    let (_, second) = FurrowRepository::attach_and_seal(
        &folder.root,
        Some("test".to_owned()),
        SnapshotTrigger::AgentRun,
        SealOptions::default(),
    )
    .unwrap();
    drop(repository);
    fs::remove_file(folder.root.join("Readme.md")).unwrap();
    let mut repository = FurrowRepository::open(&folder.root).unwrap();
    repository.rewind(&first, &[], false).unwrap();

    folding("case", || {
        let (_, plan) = repository.rewind(&second, &[], false).unwrap();

        assert_eq!(folder.read("README.md").as_deref(), Some("upper\n"));
        assert_eq!(plan.held.len(), 1);
    });
}

#[test]
fn a_file_system_that_keeps_every_name_restores_both_copies_and_holds_none() {
    let folder = Folder::new();
    folder.write("Readme.md", "lower\n");
    folder.write("README.md", "upper\n");
    let (repository, id) = folder.seal();
    folder.clear();
    drop(repository);

    let mut repository = FurrowRepository::open(&folder.root).unwrap();
    let (_, plan) = repository.rewind(&id, &[], false).unwrap();

    assert_eq!(folder.read("Readme.md").as_deref(), Some("lower\n"));
    assert_eq!(folder.read("README.md").as_deref(), Some("upper\n"));
    assert!(plan.held.is_empty());
}

fn read_only_tree(folder: &Folder) {
    folder.write("ro/inside.txt", "kept\n");
    folder.write("ro/deep/more.txt", "deeper\n");
    fs::set_permissions(
        folder.root.join("ro/deep"),
        fs::Permissions::from_mode(0o555),
    )
    .unwrap();
    fs::set_permissions(folder.root.join("ro"), fs::Permissions::from_mode(0o555)).unwrap();
}

fn assert_read_only_copy(copy: &Path) {
    assert_eq!(
        fs::read_to_string(copy.join("ro/inside.txt")).unwrap(),
        "kept\n"
    );
    assert_eq!(
        fs::read_to_string(copy.join("ro/deep/more.txt")).unwrap(),
        "deeper\n"
    );
    for name in ["ro", "ro/deep"] {
        let mode = fs::metadata(copy.join(name)).unwrap().permissions().mode();
        assert_eq!(mode & 0o7777, 0o555, "{name}");
    }
    make_writable(&copy.join("ro"));
}

#[test]
fn a_fork_from_a_snapshot_copies_a_read_only_directory_with_files() {
    let folder = Folder::new();
    read_only_tree(&folder);
    let (mut repository, id) = folder.seal();
    let destination = folder._temp.path().join("fork-at");

    let plan = repository.prepare_fork_at("at", &destination, &id).unwrap();
    repository.materialize_fork(plan).unwrap();

    assert_read_only_copy(&destination);
    make_writable(&folder.root.join("ro"));
}

#[test]
fn a_live_fork_copies_a_read_only_directory_with_files() {
    let folder = Folder::new();
    read_only_tree(&folder);
    let (mut repository, _) = folder.seal();
    let destination = folder._temp.path().join("fork-live");

    repository.fork("live", &destination).unwrap();

    assert_read_only_copy(&destination);
    make_writable(&folder.root.join("ro"));
}
