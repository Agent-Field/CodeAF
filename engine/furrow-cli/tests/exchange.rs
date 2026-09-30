//! The exchange verbs through the real binary: two data directories, frames
//! carried between them as inbox files, and the secrets only in the environment.

use assert_cmd::Command;
use furrow::exchange::frame;
use furrow::model::{id_hex, SnapshotTrigger};
use furrow::repository::{FurrowRepository, SealOptions};
use serde_json::Value;
use std::collections::BTreeMap;
use std::fs;
use std::os::unix::fs::{symlink, MetadataExt, PermissionsExt};
use std::path::{Path, PathBuf};

const CELL_KEY: &str = "1111111111111111111111111111111111111111111111111111111111111111";
const DEDUP: &str = "2222222222222222222222222222222222222222222222222222222222222222";
const LEDGER: &str = "0123456789abcdef";

fn furrow(data: &Path, repo: &Path) -> Command {
    let mut command = Command::cargo_bin("furrow").unwrap();
    command
        .env("FURROW_DATA_DIR", data)
        .env("FURROW_NO_DAEMON", "1")
        .env("FURROW_CELL_KEY", CELL_KEY)
        .env("FURROW_DEDUP_SECRET", DEDUP)
        .arg("--json")
        .arg("--repo")
        .arg(repo);
    command
}

fn run(data: &Path, repo: &Path, args: &[&str]) -> Value {
    let output = furrow(data, repo)
        .args(args)
        .assert()
        .success()
        .get_output()
        .stdout
        .clone();
    serde_json::from_slice(&output).unwrap()
}

struct Source {
    temp: tempfile::TempDir,
    data: PathBuf,
    tree: PathBuf,
    cell: PathBuf,
    head: String,
}

fn source() -> Source {
    let temp = tempfile::tempdir().unwrap();
    let (data, tree, cell) = (
        temp.path().join("a-data"),
        temp.path().join("a-tree"),
        temp.path().join("a-cell"),
    );
    fs::create_dir_all(tree.join("dir")).unwrap();
    fs::create_dir_all(cell.join("logs")).unwrap();
    fs::write(tree.join("dir/one.txt"), b"one\n").unwrap();
    fs::write(
        tree.join("big.bin"),
        (0..2_u32 << 20)
            .map(|n| (n.wrapping_mul(2_654_435_761) >> 13) as u8)
            .collect::<Vec<_>>(),
    )
    .unwrap();
    fs::write(tree.join("tool.sh"), b"#!/bin/sh\n").unwrap();
    fs::set_permissions(tree.join("tool.sh"), fs::Permissions::from_mode(0o750)).unwrap();
    symlink("dir/one.txt", tree.join("shortcut")).unwrap();
    fs::write(cell.join("logs/turn-1"), b"turn one").unwrap();
    for path in [
        tree.join("dir/one.txt"),
        tree.join("big.bin"),
        tree.join("tool.sh"),
        tree.join("dir"),
        cell.join("logs/turn-1"),
        cell.join("logs"),
    ] {
        let time = filetime::FileTime::from_unix_time(1_580_000_000, 5_000_000);
        filetime::set_file_times(&path, time, time).unwrap();
    }
    let options = SealOptions {
        changed: None,
        overlay: Some(cell.clone()),
    };
    let (_, head) =
        FurrowRepository::attach_and_seal_in(&data, &tree, None, SnapshotTrigger::Manual, options)
            .unwrap();
    Source {
        temp,
        data,
        tree,
        cell,
        head: id_hex(&head),
    }
}

fn dump(root: &Path) -> BTreeMap<String, (u32, i64, i64, Vec<u8>)> {
    let mut nodes = BTreeMap::new();
    let mut stack = vec![root.to_owned()];
    while let Some(dir) = stack.pop() {
        for entry in fs::read_dir(&dir).unwrap() {
            let path = entry.unwrap().path();
            let meta = fs::symlink_metadata(&path).unwrap();
            let body = if meta.is_dir() {
                stack.push(path.clone());
                Vec::new()
            } else if meta.file_type().is_symlink() {
                fs::read_link(&path)
                    .unwrap()
                    .to_string_lossy()
                    .into_owned()
                    .into_bytes()
            } else {
                fs::read(&path).unwrap()
            };
            let name = path.strip_prefix(root).unwrap().display().to_string();
            nodes.insert(name, (meta.mode(), meta.mtime(), meta.mtime_nsec(), body));
        }
    }
    nodes
}

fn files_under(dir: &Path) -> Vec<PathBuf> {
    let mut found = Vec::new();
    let mut stack = vec![dir.to_owned()];
    while let Some(next) = stack.pop() {
        for entry in fs::read_dir(next).unwrap() {
            let path = entry.unwrap().path();
            if path.is_dir() {
                stack.push(path)
            } else {
                found.push(path)
            }
        }
    }
    found
}

#[test]
fn two_data_dirs_round_trip_byte_identical() {
    let src = source();
    let temp = src.temp.path();
    let (b_data, b_tree, b_cell, inbox, outbox) = (
        temp.join("b-data"),
        temp.join("b-tree"),
        temp.join("b-cell"),
        temp.join("inbox"),
        temp.join("outbox"),
    );
    fs::create_dir_all(&b_tree).unwrap();
    fs::create_dir_all(&inbox).unwrap();

    let sent = run(
        &src.data,
        &src.tree,
        &[
            "export",
            "--head",
            &src.head,
            "--outbox",
            outbox.to_str().unwrap(),
            "--ledger",
            LEDGER,
            "--max-frame",
            "200000",
        ],
    );
    let frames: Vec<String> = sent["frames"]
        .as_array()
        .unwrap()
        .iter()
        .map(|f| f["path"].as_str().unwrap().to_owned())
        .collect();
    assert!(frames.len() > 1);

    // The uploader hands every object of every frame to the fetcher as one file.
    for path in &frames {
        let bytes = fs::read(path).unwrap();
        for object in frame::decode(&bytes).unwrap().1 {
            fs::write(inbox.join(hex::encode(object.rid)), object.bytes).unwrap();
        }
    }
    let mut args = vec!["published", "--ledger", LEDGER];
    args.extend(frames.iter().map(String::as_str));
    assert_eq!(
        run(&src.data, &src.tree, &args)["recorded"],
        sent["objects"]
    );
    assert_eq!(
        run(
            &src.data,
            &src.tree,
            &[
                "export",
                "--head",
                &src.head,
                "--outbox",
                outbox.to_str().unwrap(),
                "--ledger",
                LEDGER
            ]
        )["frames"]
            .as_array()
            .unwrap()
            .len(),
        0
    );

    assert!(
        !run(&b_data, &b_tree, &["want", "--head", &src.head])["want"]
            .as_array()
            .unwrap()
            .is_empty()
    );
    let imported = run(
        &b_data,
        &b_tree,
        &[
            "import",
            "--head",
            &src.head,
            "--inbox",
            inbox.to_str().unwrap(),
        ],
    );
    assert_eq!(imported["imported"], sent["objects"]);
    assert!(
        run(&b_data, &b_tree, &["want", "--head", &src.head])["want"]
            .as_array()
            .unwrap()
            .is_empty()
    );

    let restored = run(
        &b_data,
        &b_tree,
        &[
            "materialize",
            "--head",
            &src.head,
            "--cell-dir",
            b_cell.to_str().unwrap(),
        ],
    );
    assert_eq!(restored["snapshot"], src.head.as_str());
    assert_eq!(dump(&b_tree), dump(&src.tree));
    assert_eq!(dump(&b_cell), dump(&src.cell));
}

#[test]
fn keys_come_only_from_the_environment() {
    let src = source();
    let outbox = src.temp.path().join("outbox");
    let out = outbox.to_str().unwrap();

    // No flag carries a secret, so argv can never show one.
    for flag in ["--cell-key", "--dedup-secret", "--dedup"] {
        furrow(&src.data, &src.tree)
            .args([
                "export", "--head", &src.head, "--outbox", out, "--ledger", LEDGER, flag, CELL_KEY,
            ])
            .assert()
            .failure();
    }
    // Without the variables the verb refuses and names them, not their values.
    let refusal = Command::cargo_bin("furrow")
        .unwrap()
        .env("FURROW_DATA_DIR", &src.data)
        .env_remove("FURROW_CELL_KEY")
        .env_remove("FURROW_DEDUP_SECRET")
        .args([
            "export", "--head", &src.head, "--outbox", out, "--ledger", LEDGER,
        ])
        .assert()
        .failure();
    let stderr = String::from_utf8_lossy(&refusal.get_output().stderr).into_owned();
    assert!(stderr.contains("FURROW_CELL_KEY"), "{stderr}");

    // A full run leaves neither secret on disk.
    run(
        &src.data,
        &src.tree,
        &[
            "export", "--head", &src.head, "--outbox", out, "--ledger", LEDGER,
        ],
    );
    let needles = [
        CELL_KEY.as_bytes().to_vec(),
        DEDUP.as_bytes().to_vec(),
        vec![0x11; 32],
        vec![0x22; 32],
    ];
    for path in files_under(&src.data)
        .into_iter()
        .chain(files_under(&outbox))
    {
        let bytes = fs::read(&path).unwrap();
        for needle in &needles {
            assert!(
                !bytes.windows(needle.len()).any(|w| w == needle.as_slice()),
                "{}",
                path.display()
            );
        }
    }
}

#[test]
fn a_bad_ledger_name_is_refused_before_anything_is_written() {
    let src = source();
    let outbox = src.temp.path().join("outbox");
    furrow(&src.data, &src.tree)
        .args([
            "export",
            "--head",
            &src.head,
            "--outbox",
            outbox.to_str().unwrap(),
            "--ledger",
            "NOT-HEX",
        ])
        .assert()
        .failure();
    assert!(!outbox.exists());
}

/// Every file of a tree except the engine's own folder, with its mode and bytes.
fn files_of(root: &Path) -> BTreeMap<String, (u32, Vec<u8>)> {
    dump(root)
        .into_iter()
        .filter(|(name, _)| !name.starts_with(".furrow"))
        .map(|(name, (mode, _, _, body))| (name, (mode, body)))
        .collect()
}

/// A take that dies part way through writing the changed files leaves a whole
/// tree, never a mix of two heads: the next use of the folder rolls it back to
/// the head it held, and the take can then be done again.
#[test]
fn a_materialize_that_dies_mid_write_leaves_a_whole_tree() {
    let src = source();
    let (tree, cell) = (
        src.temp.path().join("b-tree"),
        src.temp.path().join("b-cell"),
    );
    fs::create_dir_all(&tree).unwrap();
    let cell_dir = cell.to_str().unwrap();
    let materialize = |head: &str| {
        let mut command = furrow(&src.data, &tree);
        command.args(["materialize", "--head", head, "--cell-dir", cell_dir]);
        command
    };
    materialize(&src.head).assert().success();
    let held = files_of(&tree);

    fs::write(src.tree.join("dir/one.txt"), b"two\n").unwrap();
    fs::write(src.tree.join("added.txt"), b"added\n").unwrap();
    let second = id_hex(
        &FurrowRepository::open_composed_in(&src.data, &src.tree, Some(src.cell.clone()))
            .unwrap()
            .snapshot(None, SnapshotTrigger::Manual)
            .unwrap(),
    );
    materialize(&second)
        .env("FURROW_FAILPOINT", "rewind_after_first_change")
        .assert()
        .code(86);
    // Attaching the folder again is what recovers it.
    materialize(&src.head).assert().success();
    assert_eq!(files_of(&tree), held, "the crash left the old head whole");

    materialize(&second).assert().success();
    assert_eq!(fs::read(tree.join("dir/one.txt")).unwrap(), b"two\n");
    assert_eq!(fs::read(tree.join("added.txt")).unwrap(), b"added\n");
}
