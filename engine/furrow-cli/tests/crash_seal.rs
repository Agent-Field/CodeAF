//! A seal killed at each step boundary leaves a store that opens consistent,
//! whose last completed seal is intact, and that seals again.
//!
//! The kill is a process abort at a crash point (`FURROW_FAULT`, compiled in by
//! the `fault-injection` feature), so this proves the ordering of the steps: no
//! step leaves a state the next open cannot use. Whether each step's sync
//! reaches the disk is the kernel's contract, not something a test can drive.

use assert_cmd::Command;
use serde_json::Value;
use std::fs;
use std::path::{Path, PathBuf};

/// Step boundaries of a seal, in order. Before `seal.logged` the new snapshot
/// is not yet visible; from it on, the reference log names it.
const STEPS: [&str; 4] = [
    "seal.captured",
    "seal.packed",
    "seal.committed",
    "seal.logged",
];

struct Folder {
    _temp: tempfile::TempDir,
    root: PathBuf,
    data: PathBuf,
}

impl Folder {
    fn new() -> Self {
        let temp = tempfile::tempdir().unwrap();
        let root = temp.path().join("work");
        fs::create_dir_all(&root).unwrap();
        for name in ["a.txt", "b.txt", "c.txt"] {
            fs::write(root.join(name), format!("{name} original")).unwrap();
        }
        Self {
            root,
            data: temp.path().join("data"),
            _temp: temp,
        }
    }

    fn furrow(&self) -> Command {
        let mut command = Command::cargo_bin("furrow").unwrap();
        command
            .env("FURROW_DATA_DIR", &self.data)
            .env("FURROW_NO_DAEMON", "1")
            .arg("--repo")
            .arg(&self.root)
            .arg("--json");
        command
    }

    fn seal(&self, fault: Option<&str>) -> Option<String> {
        let mut command = self.furrow();
        command.args(["hook", "turn-end", "--turn", "t", "--changed", "a.txt"]);
        if let Some(step) = fault {
            command.env("FURROW_FAULT", step);
        }
        let output = command.output().unwrap();
        output.status.success().then(|| {
            let doc: Value = serde_json::from_slice(&output.stdout).unwrap();
            doc["snapshot"].as_str().unwrap().to_owned()
        })
    }

    fn head(&self) -> String {
        let output = self.furrow().arg("status").output().unwrap();
        assert!(output.status.success(), "the store must open after a crash");
        let doc: Value = serde_json::from_slice(&output.stdout).unwrap();
        doc["head"].as_str().unwrap().to_owned()
    }

    fn restored(&self, snapshot: &str) -> String {
        fs::write(self.root.join("a.txt"), "damaged").unwrap();
        self.furrow()
            .args(["rewind", snapshot, "--yes"])
            .assert()
            .success();
        read(&self.root.join("a.txt"))
    }
}

fn read(path: &Path) -> String {
    fs::read_to_string(path).unwrap()
}

#[test]
fn a_seal_killed_at_any_step_leaves_the_last_completed_seal_intact() {
    for step in STEPS {
        let folder = Folder::new();
        let first = folder.seal(None).unwrap();
        fs::write(folder.root.join("a.txt"), "second").unwrap();

        assert!(folder.seal(Some(step)).is_none(), "{step} must abort");

        let head = folder.head();
        if step == "seal.logged" {
            assert_ne!(head, first, "{step}: the log already names the new seal");
            assert_eq!(folder.restored(&head), "second", "{step}");
        } else {
            assert_eq!(head, first, "{step}: the killed seal is not visible");
            assert_eq!(folder.restored(&first), "a.txt original", "{step}");
        }

        fs::write(folder.root.join("a.txt"), "third").unwrap();
        let next = folder
            .seal(None)
            .unwrap_or_else(|| panic!("{step}: reseal"));
        assert_eq!(folder.head(), next, "{step}");
        assert_eq!(folder.restored(&next), "third", "{step}");
    }
}

/// The catalog commits without a sync because it is an index of the packs and
/// the reference log. Losing it whole is the worst a crash can do to it.
#[test]
fn a_lost_catalog_is_rebuilt_from_the_pack_and_the_log() {
    let folder = Folder::new();
    let first = folder.seal(None).unwrap();
    fs::write(folder.root.join("a.txt"), "second").unwrap();
    let second = folder.seal(None).unwrap();

    for name in [
        "catalog.sqlite3",
        "catalog.sqlite3-wal",
        "catalog.sqlite3-shm",
    ] {
        let _ = fs::remove_file(folder.data.join("store-v1").join(name));
    }

    assert_eq!(folder.head(), second);
    assert_eq!(folder.restored(&first), "a.txt original");
    assert_eq!(folder.restored(&second), "second");
    fs::write(folder.root.join("a.txt"), "third").unwrap();
    assert!(folder.seal(None).is_some());
}
