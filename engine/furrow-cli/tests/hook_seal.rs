//! `furrow hook` on a plain folder: changed-path lists and a composed directory.

use assert_cmd::Command;
use serde_json::Value;
use std::fs;
use std::path::{Path, PathBuf};

struct Folder {
    _temp: tempfile::TempDir,
    root: PathBuf,
    cell: PathBuf,
    data: PathBuf,
}

impl Folder {
    fn new() -> Self {
        let temp = tempfile::tempdir().unwrap();
        let root = temp.path().join("work");
        fs::create_dir_all(&root).unwrap();
        Self {
            root,
            cell: temp.path().join("private"),
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

    fn turn_end(&self, extra: &[&str], stdin: &[u8]) -> String {
        let output = self
            .furrow()
            .args(["hook", "turn-end", "--turn", "t"])
            .args(extra)
            .write_stdin(stdin.to_vec())
            .assert()
            .success()
            .get_output()
            .stdout
            .clone();
        let doc: Value = serde_json::from_slice(&output).unwrap();
        doc["snapshot"].as_str().unwrap().to_owned()
    }

    fn rewind(&self, snapshot: &str, extra: &[&str]) {
        self.furrow()
            .args(["rewind", snapshot, "--yes"])
            .args(extra)
            .assert()
            .success();
    }
}

fn read(path: &Path) -> String {
    fs::read_to_string(path).unwrap()
}

#[test]
fn a_folder_that_is_not_a_git_repository_is_sealed_and_restored() {
    let folder = Folder::new();
    fs::write(folder.root.join("a.txt"), "one").unwrap();
    let sealed = folder.turn_end(&[], b"");
    assert!(!folder.root.join(".git").exists());

    fs::write(folder.root.join("a.txt"), "damaged").unwrap();
    folder.rewind(&sealed, &[]);

    assert_eq!(read(&folder.root.join("a.txt")), "one");
}

#[test]
fn only_the_paths_on_stdin_are_visited() {
    let folder = Folder::new();
    fs::write(folder.root.join("listed.txt"), "one").unwrap();
    fs::write(folder.root.join("unlisted.txt"), "one").unwrap();
    folder.turn_end(&[], b"");

    fs::write(folder.root.join("listed.txt"), "two").unwrap();
    fs::write(folder.root.join("unlisted.txt"), "two").unwrap();
    let sealed = folder.turn_end(&["--changed-stdin"], b"listed.txt\0");
    fs::write(folder.root.join("listed.txt"), "damaged").unwrap();
    fs::write(folder.root.join("unlisted.txt"), "damaged").unwrap();
    folder.rewind(&sealed, &[]);

    assert_eq!(read(&folder.root.join("listed.txt")), "two");
    assert_eq!(read(&folder.root.join("unlisted.txt")), "one");
}

#[test]
fn a_changed_flag_lists_a_path_and_empty_stdin_means_nothing_changed() {
    let folder = Folder::new();
    fs::write(folder.root.join("a.txt"), "one").unwrap();
    folder.turn_end(&[], b"");

    fs::write(folder.root.join("a.txt"), "two").unwrap();
    let unseen = folder.turn_end(&["--changed-stdin"], b"");
    fs::write(folder.root.join("a.txt"), "damaged").unwrap();
    folder.rewind(&unseen, &[]);
    assert_eq!(read(&folder.root.join("a.txt")), "one");

    fs::write(folder.root.join("a.txt"), "three").unwrap();
    let seen = folder.turn_end(&["--changed", "a.txt"], b"");
    fs::write(folder.root.join("a.txt"), "damaged").unwrap();
    folder.rewind(&seen, &[]);
    assert_eq!(read(&folder.root.join("a.txt")), "three");
}

#[test]
fn the_cell_directory_is_sealed_and_restored_outside_the_workspace() {
    let folder = Folder::new();
    fs::write(folder.root.join("a.txt"), "one").unwrap();
    fs::create_dir_all(folder.cell.join("turns")).unwrap();
    fs::write(folder.cell.join("turns/1.json"), "sealed").unwrap();
    let cell = folder.cell.to_str().unwrap();
    let sealed = folder.turn_end(&["--cell-dir", cell], b"");

    fs::write(folder.cell.join("turns/1.json"), "damaged").unwrap();
    fs::write(folder.root.join("a.txt"), "damaged").unwrap();
    folder.rewind(&sealed, &["--cell-dir", cell]);

    assert_eq!(read(&folder.cell.join("turns/1.json")), "sealed");
    assert_eq!(read(&folder.root.join("a.txt")), "one");
    assert!(!folder.root.join(".cell").exists());
}
