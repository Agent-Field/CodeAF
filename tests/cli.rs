use assert_cmd::Command;
use serde_json::Value;
use std::fs;
use std::io::{BufRead, BufReader, Write};
use std::os::unix::fs::{symlink, MetadataExt, PermissionsExt};
use std::path::{Path, PathBuf};
use std::sync::atomic::{AtomicBool, Ordering};
use std::sync::Arc;
use std::time::{Duration, Instant};
use tempfile::TempDir;

struct ChildGuard(std::process::Child);

impl Drop for ChildGuard {
    fn drop(&mut self) {
        let _ = self.0.kill();
        let _ = self.0.wait();
    }
}

struct Fixture {
    _temp: TempDir,
    repo: PathBuf,
    data: PathBuf,
}

impl Fixture {
    fn new() -> Self {
        let temp = tempfile::tempdir().unwrap();
        let repo = temp.path().join("repo");
        let data = temp.path().join("data");
        fs::create_dir_all(&repo).unwrap();
        git(&repo, &["init", "-b", "main"]);
        git(&repo, &["config", "user.email", "demo@example.com"]);
        git(&repo, &["config", "user.name", "Demo"]);
        fs::write(repo.join("app.txt"), b"tracked original\n").unwrap();
        fs::write(repo.join(".gitignore"), b".env\ncache/\n").unwrap();
        git(&repo, &["add", "app.txt", ".gitignore"]);
        git(&repo, &["commit", "-m", "initial"]);
        fs::write(repo.join(".env"), b"TOKEN=original\n").unwrap();
        fs::create_dir(repo.join("cache")).unwrap();
        fs::write(repo.join("cache/dependency.bin"), vec![7_u8; 180_000]).unwrap();
        fs::write(repo.join("notes.txt"), b"untracked notes\n").unwrap();
        symlink("app.txt", repo.join("app-link")).unwrap();
        let mut permissions = fs::metadata(repo.join("app.txt")).unwrap().permissions();
        permissions.set_mode(0o755);
        fs::set_permissions(repo.join("app.txt"), permissions).unwrap();
        xattr::set(repo.join("app.txt"), "user.furrow-test", b"preserved").unwrap();
        Self {
            _temp: temp,
            repo,
            data,
        }
    }

    fn furrow(&self) -> Command {
        let mut command = Command::cargo_bin("furrow").unwrap();
        command
            .env("FURROW_DATA_DIR", &self.data)
            .env("FURROW_NO_DAEMON", "1")
            .arg("--repo")
            .arg(&self.repo);
        command
    }

    fn watch(&self) -> String {
        let output = self
            .furrow()
            .args(["--json", "watch"])
            .assert()
            .success()
            .get_output()
            .stdout
            .clone();
        let value: Value = serde_json::from_slice(&output).unwrap();
        value["snapshot"].as_str().unwrap().to_owned()
    }
}

fn furrow_at(repo: &Path, data: &Path) -> Command {
    let mut command = Command::cargo_bin("furrow").unwrap();
    command
        .env("FURROW_DATA_DIR", data)
        .env("FURROW_NO_DAEMON", "1")
        .arg("--repo")
        .arg(repo);
    command
}

fn collect_files(root: &Path, files: &mut Vec<PathBuf>) {
    for entry in fs::read_dir(root).unwrap() {
        let entry = entry.unwrap();
        if entry.file_type().unwrap().is_dir() {
            collect_files(&entry.path(), files);
        } else {
            files.push(entry.path());
        }
    }
}

fn tree_physical_bytes(root: &Path) -> u64 {
    let mut files = Vec::new();
    collect_files(root, &mut files);
    files
        .into_iter()
        .map(|path| fs::metadata(path).unwrap().len())
        .sum()
}

fn wait_until(description: &str, timeout: Duration, mut predicate: impl FnMut() -> bool) {
    let deadline = Instant::now() + timeout;
    while Instant::now() < deadline {
        if predicate() {
            return;
        }
        std::thread::sleep(Duration::from_millis(25));
    }
    panic!("timed out waiting for {description}");
}

fn follow_process(repo: &Path, data: &Path) -> ChildGuard {
    let child = std::process::Command::new(env!("CARGO_BIN_EXE_furrow"))
        .env("FURROW_DATA_DIR", data)
        .env("FURROW_NO_DAEMON", "1")
        .arg("--repo")
        .arg(repo)
        .args(["sync", "--follow", "--poll-seconds", "1"])
        .stdout(std::process::Stdio::null())
        .stderr(std::process::Stdio::null())
        .spawn()
        .unwrap();
    ChildGuard(child)
}

/// Spawn `furrow watch --foreground` and return once the daemon has actually
/// installed its filesystem watch, not merely written the workspace pointer.
/// The watcher prints its readiness line only after `Watcher::watch` succeeds,
/// so keying off it removes the race between a fixed startup sleep and a slow
/// initial ingest. The reader thread also keeps draining stderr so the child
/// never blocks writing later "sealed ..." lines.
fn spawn_foreground_watcher(fixture: &Fixture, debounce_ms: &str) -> std::process::Child {
    let mut child = std::process::Command::new(env!("CARGO_BIN_EXE_furrow"))
        .env("FURROW_DATA_DIR", &fixture.data)
        .arg("--repo")
        .arg(&fixture.repo)
        .args(["watch", "--foreground", "--debounce-ms", debounce_ms])
        .stdout(std::process::Stdio::null())
        .stderr(std::process::Stdio::piped())
        .spawn()
        .unwrap();
    let stderr = child.stderr.take().unwrap();
    let ready = Arc::new(AtomicBool::new(false));
    let ready_writer = Arc::clone(&ready);
    std::thread::spawn(move || {
        for line in BufReader::new(stderr).lines().map_while(Result::ok) {
            if line.contains("continuously protecting") {
                ready_writer.store(true, Ordering::Release);
            }
        }
    });
    wait_until(
        "foreground watcher to install its filesystem watch",
        Duration::from_secs(10),
        || ready.load(Ordering::Acquire),
    );
    // Give the native FSEvents stream a moment to warm up before creating the
    // events the test expects to be sealed.
    std::thread::sleep(Duration::from_millis(500));
    child
}

fn ndjson(bytes: &[u8]) -> Vec<Value> {
    std::str::from_utf8(bytes)
        .unwrap()
        .lines()
        .filter(|line| !line.is_empty())
        .map(|line| serde_json::from_str(line).unwrap())
        .collect()
}

#[test]
fn path_rewind_restores_ignored_secret_without_touching_new_work() {
    let fixture = Fixture::new();
    let snapshot = fixture.watch();
    fs::write(fixture.repo.join(".env"), b"TOKEN=destroyed\n").unwrap();
    fs::write(fixture.repo.join("later.txt"), b"keep this\n").unwrap();

    let preview = fixture
        .furrow()
        .args(["rewind", &snapshot, "--paths", ".env", "--dry-run"])
        .assert()
        .success()
        .get_output()
        .stdout
        .clone();
    let value: Value = serde_json::from_slice(&preview).unwrap();
    assert_eq!(value["changes"].as_array().unwrap().len(), 1);
    assert_eq!(value["changes"][0]["path"], ".env");
    assert_eq!(
        fs::read(fixture.repo.join(".env")).unwrap(),
        b"TOKEN=destroyed\n"
    );

    fixture
        .furrow()
        .args(["rewind", &snapshot, "--paths", ".env", "--yes"])
        .assert()
        .success();
    assert_eq!(
        fs::read(fixture.repo.join(".env")).unwrap(),
        b"TOKEN=original\n"
    );
    assert_eq!(
        fs::read(fixture.repo.join("later.txt")).unwrap(),
        b"keep this\n"
    );
}

#[test]
fn status_fidelity_reports_exact_and_known_partial_capture_contracts() {
    let fixture = Fixture::new();
    fixture.watch();
    let output = fixture
        .furrow()
        .args(["--json", "status", "--fidelity"])
        .assert()
        .success()
        .get_output()
        .stdout
        .clone();
    let output: Value = serde_json::from_slice(&output).unwrap();
    assert_eq!(output["fidelity"]["grade"], "partial");
    assert!(output["status"]["head"].as_str().unwrap().len() == 64);
    let aspects = output["fidelity"]["aspects"].as_array().unwrap();
    assert!(aspects.iter().any(|aspect| {
        aspect["aspect"] == "regular_file_bytes" && aspect["fidelity"] == "exact"
    }));
    assert!(aspects.iter().any(|aspect| {
        aspect["aspect"] == "hard_link_groups" && aspect["fidelity"] == "not_preserved_by_rewind"
    }));
    assert!(aspects.iter().any(|aspect| {
        aspect["aspect"] == "sparse_holes" && aspect["fidelity"] == "not_preserved"
    }));
}

#[test]
fn policy_excluded_subtrees_survive_rewind_and_can_later_be_included() {
    let fixture = Fixture::new();
    fs::write(fixture.repo.join(".furrowpolicy"), b"exclude cache\n").unwrap();
    let excluded_snapshot = fixture.watch();

    fs::write(
        fixture.repo.join("cache/dependency.bin"),
        b"new excluded cache state\n",
    )
    .unwrap();
    fs::write(fixture.repo.join("app.txt"), b"damaged app\n").unwrap();
    fs::remove_file(fixture.repo.join(".furrowpolicy")).unwrap();
    fixture
        .furrow()
        .args(["rewind", &excluded_snapshot, "--yes"])
        .assert()
        .success();
    assert_eq!(
        fs::read(fixture.repo.join("cache/dependency.bin")).unwrap(),
        b"new excluded cache state\n"
    );
    assert_eq!(
        fs::read(fixture.repo.join("app.txt")).unwrap(),
        b"tracked original\n"
    );
    assert_eq!(
        fs::read(fixture.repo.join(".furrowpolicy")).unwrap(),
        b"exclude cache\n"
    );
    let fidelity = fixture
        .furrow()
        .args(["--json", "status", "--fidelity"])
        .assert()
        .success()
        .get_output()
        .stdout
        .clone();
    let fidelity: Value = serde_json::from_slice(&fidelity).unwrap();
    assert_eq!(fidelity["fidelity"]["excluded_subtrees"][0], "cache");

    fs::write(
        fixture.repo.join(".furrowpolicy"),
        b"# cache is protected again\n",
    )
    .unwrap();
    let included = fixture
        .furrow()
        .args(["--json", "snap", "-m", "include cache"])
        .assert()
        .success()
        .get_output()
        .stdout
        .clone();
    let included: Value = serde_json::from_slice(&included).unwrap();
    let included = included["snapshot"].as_str().unwrap();
    fs::write(
        fixture.repo.join("cache/dependency.bin"),
        b"later cache damage\n",
    )
    .unwrap();
    fixture
        .furrow()
        .args(["rewind", included, "--yes"])
        .assert()
        .success();
    assert_eq!(
        fs::read(fixture.repo.join("cache/dependency.bin")).unwrap(),
        b"new excluded cache state\n"
    );
}

#[test]
fn estimate_is_read_only_policy_aware_and_accounts_for_existing_cas_chunks() {
    let fixture = Fixture::new();
    fs::write(fixture.repo.join(".furrowpolicy"), b"exclude cache\n").unwrap();
    let before = fixture
        .furrow()
        .args(["--json", "estimate"])
        .assert()
        .success()
        .get_output()
        .stdout
        .clone();
    let before: Value = serde_json::from_slice(&before).unwrap();
    assert_eq!(before["policy_rules"], 1);
    assert_eq!(before["excluded_subtrees"], 1);
    assert!(before["projected_new_chunk_bytes"].as_u64().unwrap() > 0);
    assert_eq!(before["deduplicated_chunk_bytes"], 0);
    assert!(!fixture.repo.join(".furrow").exists());

    fixture.watch();
    let after = fixture
        .furrow()
        .args(["--json", "estimate"])
        .assert()
        .success()
        .get_output()
        .stdout
        .clone();
    let after: Value = serde_json::from_slice(&after).unwrap();
    assert!(after["deduplicated_chunk_bytes"].as_u64().unwrap() > 0);
    assert!(
        after["projected_new_chunk_bytes"].as_u64().unwrap()
            < before["projected_new_chunk_bytes"].as_u64().unwrap()
    );
}

#[test]
fn installed_turn_hooks_seal_attributed_boundaries_from_any_working_directory() {
    let fixture = Fixture::new();
    let installed = fixture
        .furrow()
        .args(["--json", "hook", "install"])
        .assert()
        .success()
        .get_output()
        .stdout
        .clone();
    let installed: Value = serde_json::from_slice(&installed).unwrap();
    assert_eq!(installed["hooks"].as_array().unwrap().len(), 3);
    let pre_turn = fixture.repo.join(".furrow/hooks/pre-turn");
    assert!(fs::metadata(&pre_turn).unwrap().permissions().mode() & 0o111 != 0);

    let outside = fixture._temp.path().join("outside");
    fs::create_dir(&outside).unwrap();
    let output = std::process::Command::new(&pre_turn)
        .current_dir(&outside)
        .env("FURROW_DATA_DIR", &fixture.data)
        .env("FURROW_NO_DAEMON", "1")
        .env("FURROW_BIN", env!("CARGO_BIN_EXE_furrow"))
        .env("FURROW_AGENT_ID", "alpha")
        .env("FURROW_TURN_ID", "7")
        .output()
        .unwrap();
    assert!(
        output.status.success(),
        "{}",
        String::from_utf8_lossy(&output.stderr)
    );
    fixture
        .furrow()
        .args([
            "hook",
            "post-tool",
            "--agent",
            "alpha",
            "--turn",
            "7",
            "--tool",
            "write",
        ])
        .assert()
        .success();

    let timeline = fixture
        .furrow()
        .args(["--json", "timeline", "--limit", "5"])
        .assert()
        .success()
        .get_output()
        .stdout
        .clone();
    let timeline: Value = serde_json::from_slice(&timeline).unwrap();
    assert_eq!(
        timeline[0]["label"],
        "hook post-tool agent=alpha turn=7 tool=write"
    );
    assert_eq!(timeline[0]["trigger"], "agent_run");
    assert!(timeline.as_array().unwrap().iter().any(|snapshot| {
        snapshot["label"] == "hook pre-turn agent=alpha turn=7"
            && snapshot["trigger"] == "agent_run"
    }));
}

#[test]
fn try_auto_protects_an_unwatched_workspace_and_preserves_the_command_exit_code() {
    let fixture = Fixture::new();
    let output = fixture
        .furrow()
        .args([
            "try",
            "-m",
            "risky migration",
            "--",
            "/bin/sh",
            "-c",
            "rm -f .env; printf 'damaged\\n' > app.txt; exit 17",
        ])
        .assert()
        .code(17)
        .get_output()
        .clone();
    let stderr = String::from_utf8(output.stderr).unwrap();
    let before = stderr
        .lines()
        .find_map(|line| line.strip_prefix("Protected "))
        .expect("pre-command snapshot was reported");
    assert_eq!(before.len(), 64);
    assert!(stderr.contains(&format!("Undo with: furrow rewind {before}")));
    assert!(!fixture.repo.join(".env").exists());
    assert_eq!(
        fs::read(fixture.repo.join("app.txt")).unwrap(),
        b"damaged\n"
    );

    fixture
        .furrow()
        .args(["rewind", before, "--yes"])
        .assert()
        .success();
    assert_eq!(
        fs::read(fixture.repo.join(".env")).unwrap(),
        b"TOKEN=original\n"
    );
    assert_eq!(
        fs::read(fixture.repo.join("app.txt")).unwrap(),
        b"tracked original\n"
    );

    let timeline = fixture
        .furrow()
        .args(["--json", "timeline", "--limit", "4"])
        .assert()
        .success()
        .get_output()
        .stdout
        .clone();
    let timeline: Value = serde_json::from_slice(&timeline).unwrap();
    let timeline = timeline.as_array().unwrap();
    assert!(timeline.iter().any(|snapshot| {
        snapshot["label"] == "before try: risky migration" && snapshot["trigger"] == "initial"
    }));
    assert!(timeline.iter().any(|snapshot| {
        snapshot["label"] == "after try (exit 17): risky migration"
            && snapshot["trigger"] == "agent_run"
    }));
}

#[test]
fn shrink_previews_without_mutation_then_deletes_and_restores_dependency_caches() {
    let fixture = Fixture::new();
    let dependency = fixture.repo.join("node_modules/pkg/archive.bin");
    fs::create_dir_all(dependency.parent().unwrap()).unwrap();
    fs::write(&dependency, vec![0x5a; 512 * 1024]).unwrap();

    let preview = fixture
        .furrow()
        .args(["--json", "shrink"])
        .assert()
        .success()
        .get_output()
        .stdout
        .clone();
    let preview: Value = serde_json::from_slice(&preview).unwrap();
    assert_eq!(preview["candidates"].as_array().unwrap().len(), 1);
    assert_eq!(preview["candidates"][0]["path"], "node_modules");
    assert!(preview["total_logical_bytes"].as_u64().unwrap() >= 512 * 1024);
    assert!(dependency.exists());
    assert!(!fixture.repo.join(".furrow").exists());

    let applied = fixture
        .furrow()
        .args(["--json", "shrink", "--yes"])
        .assert()
        .success()
        .get_output()
        .stdout
        .clone();
    let applied: Value = serde_json::from_slice(&applied).unwrap();
    let before = applied["before_snapshot"].as_str().unwrap();
    assert_eq!(before.len(), 64);
    assert_eq!(applied["changed"], true);
    assert!(
        applied["estimated_workspace_bytes_removed"]
            .as_u64()
            .unwrap()
            > 0
    );
    assert!(applied["protected_store_bytes_added"].as_u64().is_some());
    assert!(applied["estimated_net_bytes_reclaimed"].as_u64().is_some());
    assert!(!fixture.repo.join("node_modules").exists());
    assert!(fixture.repo.join("cache/dependency.bin").exists());

    fixture
        .furrow()
        .args(["rewind", before, "--yes"])
        .assert()
        .success();
    assert_eq!(fs::read(dependency).unwrap(), vec![0x5a; 512 * 1024]);

    fixture
        .furrow()
        .args(["shrink", "--path", ".git", "--yes"])
        .assert()
        .failure()
        .stderr(predicates::str::contains("refusing to shrink"));
}

#[test]
fn bisect_finds_the_first_bad_snapshot_without_leaking_probe_side_effects() {
    let fixture = Fixture::new();
    fixture.watch();
    fs::write(fixture.repo.join("notes.txt"), b"still good\n").unwrap();
    fixture
        .furrow()
        .args(["snap", "-m", "still good"])
        .assert()
        .success();

    fs::write(fixture.repo.join("app.txt"), b"regression\n").unwrap();
    let first_bad = fixture
        .furrow()
        .args(["--json", "snap", "-m", "introduced regression"])
        .assert()
        .success()
        .get_output()
        .stdout
        .clone();
    let first_bad: Value = serde_json::from_slice(&first_bad).unwrap();
    let first_bad = first_bad["snapshot"].as_str().unwrap();
    fs::write(fixture.repo.join("later.txt"), b"unrelated later work\n").unwrap();
    fixture
        .furrow()
        .args(["snap", "-m", "later bad state"])
        .assert()
        .success();

    let outcome = fixture
        .furrow()
        .args([
            "--json",
            "bisect",
            "--",
            "/bin/sh",
            "-c",
            "grep -q 'tracked original' app.txt; code=$?; printf 'probe mutation\\n' > app.txt; exit $code",
        ])
        .assert()
        .success()
        .get_output()
        .stdout
        .clone();
    let outcome: Value = serde_json::from_slice(&outcome).unwrap();
    assert_eq!(outcome["first_bad_snapshot"], first_bad);
    assert!(outcome["checks"].as_array().unwrap().len() <= 5);
    assert!(outcome["checks"]
        .as_array()
        .unwrap()
        .iter()
        .any(|check| check["passed"] == true));
    assert!(outcome["checks"]
        .as_array()
        .unwrap()
        .iter()
        .any(|check| check["passed"] == false));
    assert_eq!(
        fs::read(fixture.repo.join("app.txt")).unwrap(),
        b"regression\n"
    );
    assert!(!fs::read_dir(fixture._temp.path())
        .unwrap()
        .filter_map(Result::ok)
        .any(|entry| entry
            .file_name()
            .to_string_lossy()
            .starts_with(".furrow-bisect-")));
}

#[test]
fn full_rewind_restores_git_ignored_untracked_metadata_and_is_reversible() {
    let fixture = Fixture::new();
    let snapshot = fixture.watch();

    fs::remove_file(fixture.repo.join("app-link")).unwrap();
    fs::remove_file(fixture.repo.join("app.txt")).unwrap();
    fs::remove_file(fixture.repo.join(".env")).unwrap();
    fs::remove_dir_all(fixture.repo.join("cache")).unwrap();
    fs::write(fixture.repo.join("notes.txt"), b"destroyed notes\n").unwrap();

    let output = fixture
        .furrow()
        .args(["rewind", &snapshot, "--yes"])
        .assert()
        .success()
        .get_output()
        .stdout
        .clone();
    let output = String::from_utf8(output).unwrap();
    let undo = output
        .lines()
        .find_map(|line| line.strip_prefix("Undo snapshot: "))
        .unwrap()
        .to_owned();

    assert_eq!(
        fs::read(fixture.repo.join("app.txt")).unwrap(),
        b"tracked original\n"
    );
    assert_eq!(
        fs::read(fixture.repo.join(".env")).unwrap(),
        b"TOKEN=original\n"
    );
    assert_eq!(
        fs::read(fixture.repo.join("notes.txt")).unwrap(),
        b"untracked notes\n"
    );
    assert_eq!(
        fs::read_link(fixture.repo.join("app-link")).unwrap(),
        Path::new("app.txt")
    );
    assert_eq!(
        fs::metadata(fixture.repo.join("app.txt"))
            .unwrap()
            .permissions()
            .mode()
            & 0o777,
        0o755
    );
    assert_eq!(
        xattr::get(fixture.repo.join("app.txt"), "user.furrow-test")
            .unwrap()
            .unwrap(),
        b"preserved"
    );
    assert_eq!(
        fs::metadata(fixture.repo.join("cache/dependency.bin"))
            .unwrap()
            .len(),
        180_000
    );

    fixture
        .furrow()
        .args(["rewind", &undo, "--yes"])
        .assert()
        .success();
    assert!(!fixture.repo.join("app.txt").exists());
    assert!(!fixture.repo.join(".env").exists());
    assert_eq!(
        fs::read(fixture.repo.join("notes.txt")).unwrap(),
        b"destroyed notes\n"
    );
}

#[test]
fn catalog_is_rebuilt_from_pack_and_authoritative_refs() {
    let fixture = Fixture::new();
    let snapshot = fixture.watch();
    let store = fixture.data.join("store-v1");
    for suffix in [
        "catalog.sqlite3",
        "catalog.sqlite3-wal",
        "catalog.sqlite3-shm",
    ] {
        let path = store.join(suffix);
        if path.exists() {
            fs::remove_file(path).unwrap();
        }
    }

    let output = fixture
        .furrow()
        .args(["--json", "timeline"])
        .assert()
        .success()
        .get_output()
        .stdout
        .clone();
    let timeline: Value = serde_json::from_slice(&output).unwrap();
    assert_eq!(timeline[0]["id"], snapshot);
}

#[test]
fn incomplete_pack_tail_is_truncated_without_losing_visible_snapshots() {
    let fixture = Fixture::new();
    let snapshot = fixture.watch();
    let pack = fixture.data.join("store-v1/packs/pack-000001.agp");
    let valid_len = fs::metadata(&pack).unwrap().len();
    use std::io::Write;
    let mut file = fs::OpenOptions::new().append(true).open(&pack).unwrap();
    file.write_all(b"AGOB\x01").unwrap();
    file.sync_all().unwrap();

    fixture.furrow().arg("status").assert().success();
    assert_eq!(fs::metadata(&pack).unwrap().len(), valid_len);
    let output = fixture
        .furrow()
        .args(["--json", "timeline"])
        .assert()
        .success()
        .get_output()
        .stdout
        .clone();
    let timeline: Value = serde_json::from_slice(&output).unwrap();
    assert_eq!(timeline[0]["id"], snapshot);
}

#[test]
fn sqlite_consistent_rewind_restores_a_logical_database_snapshot() {
    let fixture = Fixture::new();
    let database = fixture.repo.join("dev.sqlite");
    {
        let connection = rusqlite::Connection::open(&database).unwrap();
        connection
            .execute_batch(
                "PRAGMA journal_mode=WAL;
                 CREATE TABLE items(id INTEGER PRIMARY KEY, value TEXT NOT NULL);
                 INSERT INTO items(value) VALUES('safe');",
            )
            .unwrap();
    }
    let snapshot = fixture.watch();
    {
        let connection = rusqlite::Connection::open(&database).unwrap();
        connection
            .execute_batch("DROP TABLE items; CREATE TABLE damage(value TEXT);")
            .unwrap();
    }

    fixture
        .furrow()
        .args([
            "rewind",
            &snapshot,
            "--paths",
            "dev.sqlite",
            "--sqlite-consistent",
            "--yes",
        ])
        .assert()
        .success();

    let connection = rusqlite::Connection::open(&database).unwrap();
    let value: String = connection
        .query_row("SELECT value FROM items WHERE id = 1", [], |row| row.get(0))
        .unwrap();
    assert_eq!(value, "safe");
    let integrity: String = connection
        .query_row("PRAGMA integrity_check", [], |row| row.get(0))
        .unwrap();
    assert_eq!(integrity, "ok");
}

#[test]
fn path_rewind_refuses_to_follow_a_symlink_parent_outside_workspace() {
    let fixture = Fixture::new();
    fs::create_dir(fixture.repo.join("nested")).unwrap();
    fs::write(fixture.repo.join("nested/value.txt"), b"inside\n").unwrap();
    let snapshot = fixture.watch();
    let outside = fixture._temp.path().join("outside");
    fs::create_dir(&outside).unwrap();
    fs::write(outside.join("value.txt"), b"outside\n").unwrap();
    fs::remove_dir_all(fixture.repo.join("nested")).unwrap();
    symlink(&outside, fixture.repo.join("nested")).unwrap();

    fixture
        .furrow()
        .args(["rewind", &snapshot, "--paths", "nested/value.txt", "--yes"])
        .assert()
        .failure()
        .stderr(predicates::str::contains("symlink parent"));
    assert_eq!(fs::read(outside.join("value.txt")).unwrap(), b"outside\n");
}

#[test]
fn watch_refuses_non_git_directories() {
    let temp = tempfile::tempdir().unwrap();
    Command::cargo_bin("furrow")
        .unwrap()
        .env("FURROW_DATA_DIR", temp.path().join("data"))
        .env("FURROW_NO_DAEMON", "1")
        .arg("--repo")
        .arg(temp.path())
        .arg("watch")
        .assert()
        .failure()
        .stderr(predicates::str::contains("requires a Git repository"));
}

#[test]
fn unchanged_snapshots_reuse_cached_blobs_and_add_only_small_metadata() {
    let fixture = Fixture::new();
    fixture.watch();
    let pack = fixture.data.join("store-v1/packs/pack-000001.agp");
    let initial = fs::metadata(&pack).unwrap().len();

    fixture
        .furrow()
        .args(["snap", "-m", "unchanged"])
        .assert()
        .success();
    let unchanged_growth = fs::metadata(&pack).unwrap().len() - initial;
    assert!(
        unchanged_growth < 16 * 1024,
        "unchanged snapshot added {unchanged_growth} bytes"
    );

    fs::write(fixture.repo.join("notes.txt"), b"one small delta\n").unwrap();
    let before_delta = fs::metadata(&pack).unwrap().len();
    fixture
        .furrow()
        .args(["snap", "-m", "small delta"])
        .assert()
        .success();
    let delta_growth = fs::metadata(&pack).unwrap().len() - before_delta;
    assert!(
        delta_growth < 32 * 1024,
        "small delta added {delta_growth} bytes"
    );
}

#[test]
fn warm_fork_is_independent_complete_listed_and_can_run_a_command() {
    let fixture = Fixture::new();
    fixture.watch();
    let destination = fixture._temp.path().join("agent-one");

    let output = fixture
        .furrow()
        .args(["--json", "fork", "agent-one", "--destination"])
        .arg(&destination)
        .assert()
        .success()
        .get_output()
        .stdout
        .clone();
    let summary: Value = serde_json::from_slice(&output).unwrap();
    assert_eq!(summary["plan"]["name"], "agent-one");
    assert!(summary["plan"]["projected_native_cow_ms"].as_u64().unwrap() > 0);
    assert_eq!(
        summary["plan"]["worst_case_copied_bytes"],
        summary["plan"]["logical_bytes"]
    );
    assert_eq!(summary["result"]["name"], "agent-one");
    assert_eq!(summary["plan"]["files"], summary["result"]["files"]);
    assert_eq!(
        summary["plan"]["logical_bytes"],
        summary["result"]["logical_bytes"]
    );
    assert!(
        summary["result"]["cloned_bytes"].as_u64().unwrap()
            + summary["result"]["copied_bytes"].as_u64().unwrap()
            > 0
    );
    assert_eq!(
        fs::read(destination.join(".env")).unwrap(),
        b"TOKEN=original\n"
    );
    assert_eq!(
        fs::read(destination.join("cache/dependency.bin"))
            .unwrap()
            .len(),
        180_000
    );
    assert_ne!(
        fs::read_to_string(destination.join(".furrow/workspace-id")).unwrap(),
        fs::read_to_string(fixture.repo.join(".furrow/workspace-id")).unwrap()
    );

    fs::write(destination.join("app.txt"), b"fork-only change\n").unwrap();
    fs::write(destination.join("fork-result.txt"), b"new result\n").unwrap();
    fs::remove_file(destination.join("notes.txt")).unwrap();
    assert_eq!(
        fs::read(fixture.repo.join("app.txt")).unwrap(),
        b"tracked original\n"
    );

    let diff_output = fixture
        .furrow()
        .args(["--json", "diff", "agent-one"])
        .assert()
        .success()
        .get_output()
        .stdout
        .clone();
    let diff: Value = serde_json::from_slice(&diff_output).unwrap();
    let changes = diff["changes"].as_array().unwrap();
    assert!(changes
        .iter()
        .any(|change| change["path"] == "app.txt" && change["action"] == "modify"));
    assert!(changes
        .iter()
        .any(|change| change["path"] == "fork-result.txt" && change["action"] == "add"));
    assert!(changes
        .iter()
        .any(|change| change["path"] == "notes.txt" && change["action"] == "delete"));

    let listed = fixture
        .furrow()
        .args(["--json", "forks"])
        .assert()
        .success()
        .get_output()
        .stdout
        .clone();
    let listed: Value = serde_json::from_slice(&listed).unwrap();
    assert_eq!(listed.as_array().unwrap().len(), 1);
    assert_eq!(
        listed[0]["destination"],
        destination.to_string_lossy().as_ref()
    );

    let command_destination = fixture._temp.path().join("agent-command");
    fixture
        .furrow()
        .args(["run", "agent-command", "--destination"])
        .arg(&command_destination)
        .args(["--", "sh", "-c", "printf isolated > command-result.txt"])
        .assert()
        .success();
    assert_eq!(
        fs::read(command_destination.join("command-result.txt")).unwrap(),
        b"isolated"
    );
    assert!(!fixture.repo.join("command-result.txt").exists());

    fixture
        .furrow()
        .args(["fork-rm", "agent-one"])
        .assert()
        .success();
    assert!(!destination.exists());
    let remaining = fixture
        .furrow()
        .args(["--json", "forks"])
        .assert()
        .success()
        .get_output()
        .stdout
        .clone();
    let remaining: Value = serde_json::from_slice(&remaining).unwrap();
    assert_eq!(remaining.as_array().unwrap().len(), 1);
    assert_eq!(remaining[0]["name"], "agent-command");
}

#[test]
fn exec_plan_discloses_the_fallback_driver_paths_and_ports() {
    let fixture = Fixture::new();
    fixture.watch();
    let output = fixture
        .furrow()
        .env("FURROW_DISABLE_NAMESPACES", "1")
        .args(["--json", "exec", "-n", "3", "--plan"])
        .assert()
        .success()
        .get_output()
        .stdout
        .clone();
    let plan: Value = serde_json::from_slice(&output).unwrap();
    assert_eq!(plan["driver"]["driver"], "sibling-directory");
    assert_eq!(plan["driver"]["same_canonical_path"], false);
    assert_eq!(plan["universes"].as_array().unwrap().len(), 3);
    assert_eq!(plan["universes"][0]["port"], 3000);
    assert_eq!(plan["universes"][2]["port"], 3002);
    assert_ne!(
        plan["universes"][0]["process_workdir"],
        fixture.repo.to_string_lossy().as_ref()
    );
    assert_eq!(
        plan["universes"][0]["base_snapshot"],
        plan["universes"][2]["base_snapshot"]
    );
}

#[test]
fn exec_runs_multiple_complete_universes_concurrently_and_seals_each_result() {
    let fixture = Fixture::new();
    fixture.watch();
    let output = fixture
        .furrow()
        .env("FURROW_DISABLE_NAMESPACES", "1")
        .args([
            "--json",
            "exec",
            "-n",
            "2",
            "--",
            "sh",
            "-c",
            "printf '%s|%s|%s' \"$FURROW_UNIVERSE_INDEX\" \"$FURROW_WORKDIR\" \"$PORT\" > universe.txt",
        ])
        .assert()
        .success()
        .get_output()
        .stdout
        .clone();
    let result: Value = serde_json::from_slice(&output).unwrap();
    assert_eq!(result["driver"]["driver"], "sibling-directory");
    assert_eq!(result["universes"].as_array().unwrap().len(), 2);
    assert!(result["materialized_ms"].as_u64().is_some());

    for (offset, universe) in result["universes"].as_array().unwrap().iter().enumerate() {
        assert_eq!(universe["exit_code"], 0);
        assert_eq!(universe["fork_id"].as_str().unwrap().len(), 32);
        assert_ne!(universe["head_snapshot"], universe["base_snapshot"]);
        let path = PathBuf::from(universe["path"].as_str().unwrap());
        let contents = fs::read_to_string(path.join("universe.txt")).unwrap();
        let expected_prefix = format!("{}|{}|{}", offset + 1, path.display(), 3000 + offset);
        assert_eq!(contents, expected_prefix);
    }
    assert!(!fixture.repo.join("universe.txt").exists());

    let forks = fixture.furrow().args(["--json", "forks"]).output().unwrap();
    let forks: Value = serde_json::from_slice(&forks.stdout).unwrap();
    assert_eq!(forks.as_array().unwrap().len(), 2);
}

#[test]
fn exec_uses_the_disclosed_workdir_and_preserves_failed_results() {
    let fixture = Fixture::new();
    fixture.watch();
    let plan_output = fixture
        .furrow()
        .args(["--json", "exec", "--fork", "failed-universe", "--plan"])
        .output()
        .unwrap();
    assert!(plan_output.status.success());
    let plan: Value = serde_json::from_slice(&plan_output.stdout).unwrap();
    let expected_workdir = plan["universes"][0]["process_workdir"]
        .as_str()
        .unwrap()
        .to_owned();

    let output = fixture
        .furrow()
        .args([
            "--json",
            "exec",
            "--fork",
            "failed-universe",
            "--",
            "sh",
            "-c",
            "pwd > observed-workdir.txt; printf retained > failed-result.txt; exit 7",
        ])
        .output()
        .unwrap();
    assert_eq!(output.status.code(), Some(7));
    let result: Value = serde_json::from_slice(&output.stdout).unwrap();
    assert_eq!(result["universes"][0]["exit_code"], 7);
    assert_eq!(result["universes"][0]["process_workdir"], expected_workdir);
    let fork = PathBuf::from(result["universes"][0]["path"].as_str().unwrap());
    assert_eq!(
        fs::read_to_string(fork.join("observed-workdir.txt"))
            .unwrap()
            .trim(),
        expected_workdir
    );
    assert_eq!(
        fs::read(fork.join("failed-result.txt")).unwrap(),
        b"retained"
    );
    assert_ne!(
        result["universes"][0]["head_snapshot"],
        result["universes"][0]["base_snapshot"]
    );
    assert!(!fixture.repo.join("failed-result.txt").exists());
}

#[test]
fn conflict_radar_opens_once_resolves_and_replays_from_a_durable_cursor() {
    let fixture = Fixture::new();
    fixture.watch();
    let alpha = fixture._temp.path().join("radar-alpha");
    let beta = fixture._temp.path().join("radar-beta");
    fixture
        .furrow()
        .args(["fork", "radar-alpha", "--destination"])
        .arg(&alpha)
        .assert()
        .success();
    fixture
        .furrow()
        .args(["fork", "radar-beta", "--destination"])
        .arg(&beta)
        .assert()
        .success();

    fs::write(alpha.join("app.txt"), b"alpha implementation\n").unwrap();
    fs::write(beta.join("notes.txt"), b"beta notes\n").unwrap();
    furrow_at(&alpha, &fixture.data)
        .arg("snap")
        .assert()
        .success();
    furrow_at(&beta, &fixture.data)
        .arg("snap")
        .assert()
        .success();
    let disjoint = fixture.furrow().args(["--json", "forks"]).output().unwrap();
    assert!(
        disjoint.status.success(),
        "{}",
        String::from_utf8_lossy(&disjoint.stderr)
    );
    let disjoint: Value = serde_json::from_slice(&disjoint.stdout).unwrap();
    assert!(disjoint
        .as_array()
        .unwrap()
        .iter()
        .all(|fork| fork["conflicts"] == 0));

    fs::write(beta.join("app.txt"), b"beta implementation\n").unwrap();
    furrow_at(&beta, &fixture.data)
        .arg("snap")
        .assert()
        .success();
    let conflicted = fixture.furrow().args(["--json", "forks"]).output().unwrap();
    let conflicted: Value = serde_json::from_slice(&conflicted.stdout).unwrap();
    for fork in conflicted.as_array().unwrap() {
        assert_eq!(fork["fork_id"].as_str().unwrap().len(), 32);
        assert_eq!(fork["conflicts"], 1);
        assert_eq!(fork["conflict_paths"][0], "app.txt");
    }

    let events = fixture.furrow().arg("events").output().unwrap();
    assert!(events.status.success());
    let events = ndjson(&events.stdout);
    let opened: Vec<_> = events
        .iter()
        .filter(|event| event["state"] == "opened")
        .collect();
    assert_eq!(opened.len(), 1);
    assert_eq!(opened[0]["path"]["display"], "app.txt");
    assert_eq!(opened[0]["forks"].as_array().unwrap().len(), 2);
    let cursor = opened[0]["cursor"].as_str().unwrap().to_owned();

    // Repeated reconciliation must not duplicate a stable transition.
    fixture.furrow().arg("forks").assert().success();
    let repeated = fixture
        .furrow()
        .args(["events", "--after", &cursor])
        .output()
        .unwrap();
    assert!(repeated.status.success());
    assert!(repeated.stdout.is_empty());

    furrow_at(&alpha, &fixture.data)
        .args(["claim", "app.txt", "--owner", "alpha-agent"])
        .assert()
        .success();
    fixture.furrow().arg("forks").assert().success();
    let claimed = fixture
        .furrow()
        .args(["events", "--after", &cursor])
        .output()
        .unwrap();
    let claimed = ndjson(&claimed.stdout);
    assert_eq!(claimed.len(), 1);
    assert_eq!(claimed[0]["state"], "updated");
    assert_eq!(claimed[0]["claim_state"], "covered");
    let cursor = claimed[0]["cursor"].as_str().unwrap().to_owned();

    let offline = fixture._temp.path().join("radar-beta-offline");
    fs::rename(&beta, &offline).unwrap();
    let stale = fixture.furrow().args(["--json", "forks"]).output().unwrap();
    let stale: Value = serde_json::from_slice(&stale.stdout).unwrap();
    let beta_status = stale
        .as_array()
        .unwrap()
        .iter()
        .find(|fork| fork["name"] == "radar-beta")
        .unwrap();
    assert_eq!(beta_status["conflicts"], 1);
    assert_eq!(beta_status["radar_stale"], true);
    fs::rename(&offline, &beta).unwrap();

    fs::write(beta.join("app.txt"), b"tracked original\n").unwrap();
    furrow_at(&beta, &fixture.data)
        .arg("snap")
        .assert()
        .success();
    let resolved = fixture.furrow().args(["--json", "forks"]).output().unwrap();
    let resolved: Value = serde_json::from_slice(&resolved.stdout).unwrap();
    assert!(resolved
        .as_array()
        .unwrap()
        .iter()
        .all(|fork| fork["conflicts"] == 0));
    let events = fixture
        .furrow()
        .args(["events", "--after", &cursor])
        .output()
        .unwrap();
    let events = ndjson(&events.stdout);
    assert_eq!(events.len(), 1);
    assert_eq!(events[0]["state"], "resolved");
    assert_eq!(events[0]["conflict_id"], opened[0]["conflict_id"]);
}

#[test]
fn conflict_radar_distinguishes_sibling_edits_from_subtree_deletion() {
    let fixture = Fixture::new();
    fs::create_dir(fixture.repo.join("src")).unwrap();
    fs::write(fixture.repo.join("src/alpha.rs"), b"alpha base\n").unwrap();
    fs::write(fixture.repo.join("src/beta.rs"), b"beta base\n").unwrap();
    fixture.watch();
    let alpha = fixture._temp.path().join("tree-alpha");
    let beta = fixture._temp.path().join("tree-beta");
    fixture
        .furrow()
        .args(["fork", "tree-alpha", "--destination"])
        .arg(&alpha)
        .assert()
        .success();
    fixture
        .furrow()
        .args(["fork", "tree-beta", "--destination"])
        .arg(&beta)
        .assert()
        .success();

    fs::write(alpha.join("src/alpha.rs"), b"alpha changed\n").unwrap();
    fs::write(beta.join("src/beta.rs"), b"beta changed\n").unwrap();
    furrow_at(&alpha, &fixture.data)
        .arg("snap")
        .assert()
        .success();
    furrow_at(&beta, &fixture.data)
        .arg("snap")
        .assert()
        .success();
    let disjoint = fixture.furrow().args(["--json", "forks"]).output().unwrap();
    assert!(
        disjoint.status.success(),
        "{}",
        String::from_utf8_lossy(&disjoint.stderr)
    );
    let disjoint: Value = serde_json::from_slice(&disjoint.stdout).unwrap();
    assert!(disjoint
        .as_array()
        .unwrap()
        .iter()
        .all(|fork| fork["conflicts"] == 0));

    fs::remove_dir_all(alpha.join("src")).unwrap();
    furrow_at(&alpha, &fixture.data)
        .arg("snap")
        .assert()
        .success();
    let conflicted = fixture.furrow().args(["--json", "forks"]).output().unwrap();
    let conflicted: Value = serde_json::from_slice(&conflicted.stdout).unwrap();
    for fork in conflicted.as_array().unwrap() {
        assert_eq!(fork["conflicts"], 1);
        assert_eq!(fork["conflict_paths"][0], "src");
    }
}

#[test]
fn conflict_radar_groups_many_forks_into_one_path_event() {
    let fixture = Fixture::new();
    fixture.watch();
    for name in ["group-alpha", "group-beta", "group-gamma"] {
        let path = fixture._temp.path().join(name);
        fixture
            .furrow()
            .args(["fork", name, "--destination"])
            .arg(&path)
            .assert()
            .success();
        fs::write(path.join("app.txt"), format!("{name}\n")).unwrap();
        furrow_at(&path, &fixture.data)
            .arg("snap")
            .assert()
            .success();
    }

    let human = fixture.furrow().arg("forks").output().unwrap();
    assert!(human.status.success());
    assert_eq!(
        String::from_utf8_lossy(&human.stdout)
            .lines()
            .filter(|line| line.contains("1 conflict"))
            .count(),
        3
    );
    let events = fixture.furrow().arg("events").output().unwrap();
    let opened: Vec<_> = ndjson(&events.stdout)
        .into_iter()
        .filter(|event| event["state"] == "opened")
        .collect();
    assert_eq!(opened.len(), 1);
    assert_eq!(opened[0]["path"]["display"], "app.txt");
    assert_eq!(opened[0]["forks"].as_array().unwrap().len(), 3);
    let cursor = opened[0]["cursor"].as_str().unwrap();

    fixture
        .furrow()
        .args(["fork-rm", "group-gamma"])
        .assert()
        .success();
    let events = fixture
        .furrow()
        .args(["events", "--after", cursor])
        .output()
        .unwrap();
    let events = ndjson(&events.stdout);
    assert_eq!(events.len(), 1);
    assert_eq!(events[0]["state"], "updated");
    assert_eq!(events[0]["forks"].as_array().unwrap().len(), 2);
    let cursor = events[0]["cursor"].as_str().unwrap();

    fixture
        .furrow()
        .args(["fork-rm", "group-beta"])
        .assert()
        .success();
    let events = fixture
        .furrow()
        .args(["events", "--after", cursor])
        .output()
        .unwrap();
    let events = ndjson(&events.stdout);
    assert_eq!(events.len(), 1);
    assert_eq!(events[0]["state"], "resolved");
    let remaining = fixture.furrow().args(["--json", "forks"]).output().unwrap();
    let remaining: Value = serde_json::from_slice(&remaining.stdout).unwrap();
    assert_eq!(remaining.as_array().unwrap().len(), 1);
    assert_eq!(remaining[0]["conflicts"], 0);
}

#[test]
fn conflict_event_follower_observes_seals_without_an_in_memory_queue() {
    let fixture = Fixture::new();
    fixture.watch();
    let alpha = fixture._temp.path().join("follow-alpha");
    let beta = fixture._temp.path().join("follow-beta");
    fixture
        .furrow()
        .args(["fork", "follow-alpha", "--destination"])
        .arg(&alpha)
        .assert()
        .success();
    fixture
        .furrow()
        .args(["fork", "follow-beta", "--destination"])
        .arg(&beta)
        .assert()
        .success();
    fixture.furrow().arg("forks").assert().success();

    let mut child = std::process::Command::new(env!("CARGO_BIN_EXE_furrow"))
        .env("FURROW_DATA_DIR", &fixture.data)
        .env("FURROW_NO_DAEMON", "1")
        .arg("--repo")
        .arg(&fixture.repo)
        .args(["events", "--follow", "--interval-ms", "50"])
        .stdout(std::process::Stdio::piped())
        .stderr(std::process::Stdio::null())
        .spawn()
        .unwrap();
    let stdout = child.stdout.take().unwrap();
    let (sender, receiver) = std::sync::mpsc::channel();
    let reader = std::thread::spawn(move || {
        let mut line = String::new();
        let result = BufReader::new(stdout).read_line(&mut line);
        let _ = sender.send((result, line));
    });

    fs::write(alpha.join("app.txt"), b"alpha live\n").unwrap();
    fs::write(beta.join("app.txt"), b"beta live\n").unwrap();
    furrow_at(&alpha, &fixture.data)
        .arg("snap")
        .assert()
        .success();
    furrow_at(&beta, &fixture.data)
        .arg("snap")
        .assert()
        .success();
    let (read, line) = receiver
        .recv_timeout(Duration::from_secs(5))
        .expect("event follower did not receive the conflict");
    assert!(read.unwrap() > 0);
    let event: Value = serde_json::from_str(line.trim()).unwrap();
    assert_eq!(event["type"], "fork_conflict");
    assert_eq!(event["state"], "opened");
    assert_eq!(event["path"]["display"], "app.txt");
    child.kill().ok();
    child.wait().ok();
    reader.join().unwrap();
}

#[test]
fn verified_merge_converges_independent_source_and_fork_changes() {
    let fixture = Fixture::new();
    fixture.watch();
    let fork = fixture._temp.path().join("merge-agent");
    fixture
        .furrow()
        .args(["fork", "merge-agent", "--destination"])
        .arg(&fork)
        .assert()
        .success();

    fs::write(fixture.repo.join("app.txt"), b"source-only work\n").unwrap();
    fs::write(fork.join("agent-result.txt"), b"fork-only work\n").unwrap();
    fixture
        .furrow()
        .args([
            "merge",
            "merge-agent",
            "--check",
            "grep -q 'source-only work' app.txt && grep -q 'fork-only work' agent-result.txt",
        ])
        .assert()
        .success();

    assert_eq!(
        fs::read(fixture.repo.join("app.txt")).unwrap(),
        b"source-only work\n"
    );
    assert_eq!(
        fs::read(fixture.repo.join("agent-result.txt")).unwrap(),
        b"fork-only work\n"
    );
    let timeline = fixture
        .furrow()
        .args(["--json", "timeline"])
        .output()
        .unwrap();
    let timeline: Value = serde_json::from_slice(&timeline.stdout).unwrap();
    assert_eq!(timeline[0]["trigger"], "merge");
}

#[test]
fn advisory_claims_coordinate_sibling_forks_and_are_timeline_recorded() {
    let fixture = Fixture::new();
    fixture.watch();
    let alpha = fixture._temp.path().join("claim-alpha");
    let beta = fixture._temp.path().join("claim-beta");
    fixture
        .furrow()
        .args(["fork", "claim-alpha", "--destination"])
        .arg(&alpha)
        .assert()
        .success();
    fixture
        .furrow()
        .args(["fork", "claim-beta", "--destination"])
        .arg(&beta)
        .assert()
        .success();

    let alpha_claim = furrow_at(&alpha, &fixture.data)
        .args(["--json", "claim", "src/auth/**", "--owner", "alpha-agent"])
        .assert()
        .success()
        .get_output()
        .stdout
        .clone();
    let alpha_claim: Value = serde_json::from_slice(&alpha_claim).unwrap();
    furrow_at(&beta, &fixture.data)
        .args(["claim", "src/auth/login.rs", "--owner", "beta-agent"])
        .assert()
        .failure()
        .stderr(predicates::str::contains("held by alpha-agent"));
    furrow_at(&beta, &fixture.data)
        .args(["claim", "src/payments/**", "--owner", "beta-agent"])
        .assert()
        .success();

    let active = fixture
        .furrow()
        .args(["--json", "claims"])
        .assert()
        .success()
        .get_output()
        .stdout
        .clone();
    let active: Value = serde_json::from_slice(&active).unwrap();
    assert_eq!(active.as_array().unwrap().len(), 2);
    let timeline = furrow_at(&alpha, &fixture.data)
        .args(["--json", "timeline"])
        .assert()
        .success()
        .get_output()
        .stdout
        .clone();
    let timeline: Value = serde_json::from_slice(&timeline).unwrap();
    assert_eq!(timeline[0]["trigger"], "claim");

    furrow_at(&alpha, &fixture.data)
        .args([
            "release",
            alpha_claim["claim"]["id"].as_str().unwrap(),
            "--owner",
            "alpha-agent",
        ])
        .assert()
        .success();
    furrow_at(&beta, &fixture.data)
        .args(["claim", "src/auth/login.rs", "--owner", "beta-agent"])
        .assert()
        .success();
}

#[test]
fn coord_values_propagate_eagerly_and_reconcile_offline_forks_and_tombstones() {
    let fixture = Fixture::new();
    fixture.watch();
    let alpha = fixture._temp.path().join("coord-alpha");
    let beta = fixture._temp.path().join("coord-beta");
    fixture
        .furrow()
        .args(["fork", "coord-alpha", "--destination"])
        .arg(&alpha)
        .assert()
        .success();
    fixture
        .furrow()
        .args(["fork", "coord-beta", "--destination"])
        .arg(&beta)
        .assert()
        .success();

    furrow_at(&alpha, &fixture.data)
        .args([
            "coord",
            "write",
            "tasks/current.md",
            "--value",
            "alpha is working",
            "--owner",
            "alpha-agent",
        ])
        .assert()
        .success();
    for root in [&fixture.repo, &alpha, &beta] {
        assert_eq!(
            fs::read(root.join(".furrow/coord/tasks/current.md")).unwrap(),
            b"alpha is working"
        );
    }

    let offline = fixture._temp.path().join("coord-alpha-offline");
    fs::rename(&alpha, &offline).unwrap();
    furrow_at(&beta, &fixture.data)
        .args([
            "coord",
            "write",
            "tasks/current.md",
            "--value",
            "beta continued",
            "--owner",
            "beta-agent",
        ])
        .assert()
        .success();
    assert_eq!(
        fs::read(offline.join(".furrow/coord/tasks/current.md")).unwrap(),
        b"alpha is working"
    );
    fs::rename(&offline, &alpha).unwrap();
    let recovered = furrow_at(&alpha, &fixture.data)
        .args(["coord", "read", "tasks/current.md"])
        .assert()
        .success()
        .get_output()
        .stdout
        .clone();
    assert_eq!(recovered, b"beta continued");

    fs::rename(&alpha, &offline).unwrap();
    furrow_at(&beta, &fixture.data)
        .args([
            "coord",
            "remove",
            "tasks/current.md",
            "--owner",
            "beta-agent",
        ])
        .assert()
        .success();
    assert!(offline.join(".furrow/coord/tasks/current.md").exists());
    fs::rename(&offline, &alpha).unwrap();
    let listed = furrow_at(&alpha, &fixture.data)
        .args(["--json", "coord", "list"])
        .assert()
        .success()
        .get_output()
        .stdout
        .clone();
    let listed: Value = serde_json::from_slice(&listed).unwrap();
    assert!(listed.as_array().unwrap().is_empty());
    assert!(!alpha.join(".furrow/coord/tasks/current.md").exists());

    let timeline = furrow_at(&beta, &fixture.data)
        .args(["--json", "timeline"])
        .assert()
        .success()
        .get_output()
        .stdout
        .clone();
    let timeline: Value = serde_json::from_slice(&timeline).unwrap();
    assert_eq!(timeline[0]["trigger"], "coord");
}

#[test]
fn watch_fork_returns_seals_after_an_exact_cursor() {
    let fixture = Fixture::new();
    fixture.watch();
    let worker = fixture._temp.path().join("observer-agent");
    fixture
        .furrow()
        .args(["fork", "observer-agent", "--destination"])
        .arg(&worker)
        .assert()
        .success();

    let timeline = furrow_at(&worker, &fixture.data)
        .args(["--json", "timeline"])
        .assert()
        .success()
        .get_output()
        .stdout
        .clone();
    let timeline: Value = serde_json::from_slice(&timeline).unwrap();
    let cursor = timeline[0]["id"].as_str().unwrap();

    fs::write(worker.join("agent-result.txt"), b"completed\n").unwrap();
    let sealed = furrow_at(&worker, &fixture.data)
        .args(["--json", "snap", "-m", "agent completed task"])
        .assert()
        .success()
        .get_output()
        .stdout
        .clone();
    let sealed: Value = serde_json::from_slice(&sealed).unwrap();

    let updates = fixture
        .furrow()
        .args([
            "--json",
            "watch-fork",
            "observer-agent",
            "--after",
            cursor,
            "--once",
        ])
        .assert()
        .success()
        .get_output()
        .stdout
        .clone();
    let updates: Value = serde_json::from_slice(&updates).unwrap();
    assert_eq!(updates["cursor_found"], true);
    assert_eq!(updates["head"], sealed["snapshot"]);
    assert_eq!(updates["snapshots"].as_array().unwrap().len(), 1);
    assert_eq!(updates["snapshots"][0]["id"], sealed["snapshot"]);
    assert_eq!(updates["snapshots"][0]["label"], "agent completed task");
}

#[test]
fn merge_conflict_or_failed_check_never_mutates_source() {
    let fixture = Fixture::new();
    fixture.watch();
    let conflict_fork = fixture._temp.path().join("conflict-agent");
    fixture
        .furrow()
        .args(["fork", "conflict-agent", "--destination"])
        .arg(&conflict_fork)
        .assert()
        .success();
    fs::write(fixture.repo.join("app.txt"), b"source version\n").unwrap();
    fs::write(conflict_fork.join("app.txt"), b"fork version\n").unwrap();

    fixture
        .furrow()
        .args(["merge", "conflict-agent", "--dry-run"])
        .assert()
        .failure()
        .stderr(predicates::str::contains("conflict"));
    assert_eq!(
        fs::read(fixture.repo.join("app.txt")).unwrap(),
        b"source version\n"
    );

    let check_fork = fixture._temp.path().join("check-agent");
    fixture
        .furrow()
        .args(["fork", "check-agent", "--destination"])
        .arg(&check_fork)
        .assert()
        .success();
    fs::write(check_fork.join("check-only.txt"), b"candidate\n").unwrap();
    fixture
        .furrow()
        .args(["merge", "check-agent", "--check", "false"])
        .assert()
        .failure()
        .stderr(predicates::str::contains("merge check failed"));
    assert!(!fixture.repo.join("check-only.txt").exists());
    assert_eq!(
        fs::read(fixture.repo.join("app.txt")).unwrap(),
        b"source version\n"
    );
}

/// Clones `fixture.repo` into `peer`, pairs both sides through a plain
/// directory remote, and bootstraps `peer` to the source's current head, so
/// both workspaces share a recorded common ancestor snapshot.
fn pair_via_directory_remote(
    fixture: &Fixture,
    peer: &Path,
    peer_data: &Path,
    remote: &Path,
    name: &str,
) {
    git(
        fixture.repo.parent().unwrap(),
        &[
            "clone",
            fixture.repo.to_str().unwrap(),
            peer.file_name().unwrap().to_str().unwrap(),
        ],
    );
    fixture.watch();
    furrow_at(peer, peer_data)
        .args(["watch", "--no-daemon"])
        .assert()
        .success();

    let pair_output = fixture
        .furrow()
        .args(["--json", "pair", remote.to_str().unwrap(), "--name", name])
        .assert()
        .success()
        .get_output()
        .stdout
        .clone();
    let pair: Value = serde_json::from_slice(&pair_output).unwrap();
    let key = pair["key_hex"].as_str().unwrap().to_owned();
    furrow_at(peer, peer_data)
        .args([
            "pair",
            remote.to_str().unwrap(),
            "--name",
            name,
            "--key",
            &key,
        ])
        .assert()
        .success();

    fixture.furrow().args(["sync", "--push"]).assert().success();
    furrow_at(peer, peer_data)
        .args(["sync", "--pull", "--bootstrap"])
        .assert()
        .success();
}

/// Pushes the source's current state and pulls it on `peer`, which is
/// expected to report a divergence (each side already carries independent
/// edits). Returns the source's snapshot ID, which sync still fetches into
/// `peer`'s local object store even though it refuses to auto-merge it.
fn push_and_expect_divergence(fixture: &Fixture, peer: &Path, peer_data: &Path) -> String {
    fixture.furrow().args(["sync", "--push"]).assert().success();
    let divergence_output = furrow_at(peer, peer_data)
        .args(["--json", "sync", "--pull"])
        .assert()
        .failure()
        .get_output()
        .stdout
        .clone();
    let divergence: Value = serde_json::from_slice(&divergence_output).unwrap();
    assert_eq!(divergence["disposition"], "diverged");
    divergence["remote_snapshot"].as_str().unwrap().to_owned()
}

#[test]
fn merge_by_snapshot_id_converges_independent_changes_synced_through_a_directory_remote() {
    let fixture = Fixture::new();
    let peer = fixture.repo.parent().unwrap().join("snapshot-merge-peer");
    let peer_data = fixture
        .repo
        .parent()
        .unwrap()
        .join("snapshot-merge-peer-data");
    let remote = fixture.repo.parent().unwrap().join("snapshot-merge-remote");
    pair_via_directory_remote(&fixture, &peer, &peer_data, &remote, "snapshot-merge");

    fs::write(peer.join("peer-only.txt"), b"peer work\n").unwrap();
    fs::write(fixture.repo.join("source-only.txt"), b"source work\n").unwrap();
    let theirs = push_and_expect_divergence(&fixture, &peer, &peer_data);

    // No --base: the pull boundary snapshot the two sides share is the only
    // common ancestor, so it must be derived unambiguously.
    furrow_at(&peer, &peer_data)
        .args([
            "merge",
            "--snapshot",
            &theirs,
            "--check",
            "grep -q 'peer work' peer-only.txt && grep -q 'source work' source-only.txt",
        ])
        .assert()
        .success();

    assert_eq!(
        fs::read(peer.join("peer-only.txt")).unwrap(),
        b"peer work\n"
    );
    assert_eq!(
        fs::read(peer.join("source-only.txt")).unwrap(),
        b"source work\n"
    );
    let timeline = furrow_at(&peer, &peer_data)
        .args(["--json", "timeline"])
        .assert()
        .success()
        .get_output()
        .stdout
        .clone();
    let timeline: Value = serde_json::from_slice(&timeline).unwrap();
    assert_eq!(timeline[0]["trigger"], "merge");
}

#[test]
fn merge_by_snapshot_id_reports_conflicts_identically_to_a_fork_merge() {
    let fixture = Fixture::new();
    let peer = fixture
        .repo
        .parent()
        .unwrap()
        .join("snapshot-conflict-peer");
    let peer_data = fixture
        .repo
        .parent()
        .unwrap()
        .join("snapshot-conflict-peer-data");
    let remote = fixture
        .repo
        .parent()
        .unwrap()
        .join("snapshot-conflict-remote");
    pair_via_directory_remote(&fixture, &peer, &peer_data, &remote, "snapshot-conflict");

    fs::write(peer.join("app.txt"), b"peer version\n").unwrap();
    fs::write(fixture.repo.join("app.txt"), b"source version\n").unwrap();
    let theirs = push_and_expect_divergence(&fixture, &peer, &peer_data);

    let snapshot_output = furrow_at(&peer, &peer_data)
        .args(["--json", "merge", "--snapshot", &theirs, "--dry-run"])
        .assert()
        .failure()
        .get_output()
        .stdout
        .clone();
    let snapshot_outcome: Value = serde_json::from_slice(&snapshot_output).unwrap();
    let snapshot_conflicts = snapshot_outcome["conflicts"].clone();
    assert_eq!(snapshot_conflicts.as_array().unwrap().len(), 1);
    assert_eq!(snapshot_conflicts[0]["kind"], "modify_modify");
    assert!(fs::read(peer.join("app.txt")).unwrap() == b"peer version\n");

    // The identical conflicting edit through a plain fork merge must report
    // the same conflict shape: same path, same kind.
    let conflict_fixture = Fixture::new();
    conflict_fixture.watch();
    let fork = conflict_fixture._temp.path().join("app-conflict-fork");
    conflict_fixture
        .furrow()
        .args(["fork", "app-conflict-fork", "--destination"])
        .arg(&fork)
        .assert()
        .success();
    fs::write(conflict_fixture.repo.join("app.txt"), b"source version\n").unwrap();
    fs::write(fork.join("app.txt"), b"peer version\n").unwrap();
    let fork_output = conflict_fixture
        .furrow()
        .args(["--json", "merge", "app-conflict-fork", "--dry-run"])
        .assert()
        .failure()
        .get_output()
        .stdout
        .clone();
    let fork_outcome: Value = serde_json::from_slice(&fork_output).unwrap();
    assert_eq!(fork_outcome["conflicts"], snapshot_conflicts);
}

#[test]
fn paged_flat_directory_snapshot_restores_entries_across_pages() {
    let fixture = Fixture::new();
    let flat = fixture.repo.join("flat");
    fs::create_dir(&flat).unwrap();
    for index in 0..1_200 {
        fs::write(
            flat.join(format!("entry-{index:04}.txt")),
            format!("value {index}\n"),
        )
        .unwrap();
    }
    let snapshot = fixture.watch();
    for index in [0, 599, 1_199] {
        fs::remove_file(flat.join(format!("entry-{index:04}.txt"))).unwrap();
    }

    fixture
        .furrow()
        .args(["rewind", &snapshot, "--paths", "flat", "--yes"])
        .assert()
        .success();
    for index in [0, 599, 1_199] {
        assert_eq!(
            fs::read(flat.join(format!("entry-{index:04}.txt"))).unwrap(),
            format!("value {index}\n").as_bytes()
        );
    }
}

#[test]
fn interrupted_rewind_rolls_back_to_pre_rewind_state_on_next_command() {
    let fixture = Fixture::new();
    let snapshot = fixture.watch();
    fs::write(fixture.repo.join(".env"), b"TOKEN=broken\n").unwrap();
    fs::write(fixture.repo.join("app.txt"), b"broken app\n").unwrap();

    fixture
        .furrow()
        .env("FURROW_FAILPOINT", "rewind_after_first_change")
        .args(["rewind", &snapshot, "--yes"])
        .assert()
        .code(86);

    // Opening the repository detects the durable intent and restores the
    // automatic pre-rewind snapshot before serving status.
    fixture
        .furrow()
        .arg("status")
        .assert()
        .success()
        .stderr(predicates::str::contains("recovering interrupted rewind"));
    assert_eq!(
        fs::read(fixture.repo.join(".env")).unwrap(),
        b"TOKEN=broken\n"
    );
    assert_eq!(
        fs::read(fixture.repo.join("app.txt")).unwrap(),
        b"broken app\n"
    );
}

#[test]
fn live_writer_interference_is_rescued_and_rewind_rolls_back() {
    let fixture = Fixture::new();
    let target = fixture.watch();
    fs::write(fixture.repo.join("app.txt"), b"pre-rewind damage\n").unwrap();

    let mut child = std::process::Command::new(env!("CARGO_BIN_EXE_furrow"))
        .env("FURROW_DATA_DIR", &fixture.data)
        .env("FURROW_NO_DAEMON", "1")
        .env("FURROW_TEST_REWIND_PAUSE_AFTER_APPLY_MS", "750")
        .arg("--repo")
        .arg(&fixture.repo)
        .args(["rewind", &target, "--yes"])
        .stdout(std::process::Stdio::piped())
        .stderr(std::process::Stdio::piped())
        .spawn()
        .unwrap();
    let deadline = Instant::now() + Duration::from_secs(10);
    loop {
        assert!(
            Instant::now() < deadline,
            "rewind never reached its target state"
        );
        if fs::read(fixture.repo.join("app.txt")).unwrap_or_default() == b"tracked original\n" {
            break;
        }
        assert!(
            child.try_wait().unwrap().is_none(),
            "rewind exited before interference"
        );
        std::thread::sleep(Duration::from_millis(10));
    }
    fs::write(fixture.repo.join("app.txt"), b"live writer bytes\n").unwrap();

    let output = child.wait_with_output().unwrap();
    assert!(!output.status.success());
    let stderr = String::from_utf8(output.stderr).unwrap();
    let marker = "interference was preserved as snapshot ";
    let rescue = stderr
        .split_once(marker)
        .map(|(_, value)| &value[..64])
        .expect("rewind did not report its interference rescue snapshot");
    assert_eq!(
        fs::read(fixture.repo.join("app.txt")).unwrap(),
        b"pre-rewind damage\n"
    );

    fixture
        .furrow()
        .args(["rewind", rescue, "--yes"])
        .assert()
        .success();
    assert_eq!(
        fs::read(fixture.repo.join("app.txt")).unwrap(),
        b"live writer bytes\n"
    );
}

#[test]
fn precondition_interference_cancels_rewind_without_touching_writer_bytes() {
    let fixture = Fixture::new();
    let target = fixture.watch();
    fs::write(fixture.repo.join("app.txt"), b"pre-rewind damage\n").unwrap();

    let child = std::process::Command::new(env!("CARGO_BIN_EXE_furrow"))
        .env("FURROW_DATA_DIR", &fixture.data)
        .env("FURROW_NO_DAEMON", "1")
        .env("FURROW_TEST_REWIND_PAUSE_BEFORE_PRECONDITION_MS", "750")
        .arg("--repo")
        .arg(&fixture.repo)
        .args(["rewind", &target, "--yes"])
        .stdout(std::process::Stdio::piped())
        .stderr(std::process::Stdio::piped())
        .spawn()
        .unwrap();
    let deadline = Instant::now() + Duration::from_secs(10);
    loop {
        assert!(
            Instant::now() < deadline,
            "pre-rewind seal was not published"
        );
        let timeline = fixture
            .furrow()
            .args(["--json", "timeline", "--limit", "1"])
            .output()
            .unwrap();
        if timeline.status.success() {
            let timeline: Value = serde_json::from_slice(&timeline.stdout).unwrap();
            if timeline[0]["trigger"] == "pre_rewind" {
                break;
            }
        }
        std::thread::sleep(Duration::from_millis(10));
    }
    fs::write(fixture.repo.join("app.txt"), b"writer before mutation\n").unwrap();

    let output = child.wait_with_output().unwrap();
    assert!(!output.status.success());
    assert!(String::from_utf8(output.stderr)
        .unwrap()
        .contains("cancelled before mutation"));
    assert_eq!(
        fs::read(fixture.repo.join("app.txt")).unwrap(),
        b"writer before mutation\n"
    );
}

#[test]
fn foreground_watcher_seals_after_write_quiescence() {
    let fixture = Fixture::new();
    let mut child = spawn_foreground_watcher(&fixture, "100");
    let deadline = Instant::now() + Duration::from_secs(10);
    assert!(
        child.try_wait().unwrap().is_none(),
        "foreground watcher exited during initial protection"
    );
    fs::write(
        fixture.repo.join("automatic.txt"),
        b"captured automatically\n",
    )
    .unwrap();
    fs::write(fixture.repo.join("app.txt"), b"watcher version\n").unwrap();
    fs::remove_file(fixture.repo.join(".env")).unwrap();
    fs::create_dir(fixture.repo.join("watcher-dir")).unwrap();
    fs::write(
        fixture.repo.join("watcher-dir/nested.txt"),
        b"nested watcher state\n",
    )
    .unwrap();
    fs::remove_dir_all(fixture.repo.join("cache")).unwrap();

    let mut watcher_snapshot = None;
    while Instant::now() < deadline {
        let output = fixture
            .furrow()
            .args(["--json", "timeline"])
            .output()
            .unwrap();
        if output.status.success() {
            let value: Value = serde_json::from_slice(&output.stdout).unwrap();
            watcher_snapshot = value
                .as_array()
                .unwrap()
                .iter()
                .find(|row| row["trigger"] == "watcher")
                .and_then(|row| row["id"].as_str())
                .map(str::to_owned);
            if watcher_snapshot.is_some() {
                break;
            }
        }
        std::thread::sleep(Duration::from_millis(100));
    }
    child.kill().ok();
    child.wait().ok();
    let watcher_snapshot = watcher_snapshot.expect("watcher did not publish a snapshot");

    fs::write(fixture.repo.join("app.txt"), b"later damage\n").unwrap();
    fs::write(fixture.repo.join(".env"), b"TOKEN=recreated\n").unwrap();
    fs::remove_dir_all(fixture.repo.join("watcher-dir")).unwrap();
    fs::create_dir(fixture.repo.join("cache")).unwrap();
    fs::write(fixture.repo.join("cache/damage"), b"recreated\n").unwrap();
    fixture
        .furrow()
        .args(["rewind", &watcher_snapshot, "--yes"])
        .assert()
        .success();
    assert_eq!(
        fs::read(fixture.repo.join("app.txt")).unwrap(),
        b"watcher version\n"
    );
    assert!(!fixture.repo.join(".env").exists());
    assert!(!fixture.repo.join("cache").exists());
    assert_eq!(
        fs::read(fixture.repo.join("watcher-dir/nested.txt")).unwrap(),
        b"nested watcher state\n"
    );
}

#[test]
fn watcher_reloads_policy_and_ignores_excluded_subtree_churn() {
    let fixture = Fixture::new();
    let mut child = spawn_foreground_watcher(&fixture, "75");
    let deadline = Instant::now() + Duration::from_secs(10);
    let before = fixture
        .furrow()
        .args(["--json", "timeline", "--limit", "1"])
        .output()
        .unwrap();
    let before: Value = serde_json::from_slice(&before.stdout).unwrap();
    let before = before[0]["id"].as_str().unwrap().to_owned();

    fs::write(fixture.repo.join(".furrowpolicy"), b"exclude cache\n").unwrap();
    let policy_head = loop {
        assert!(Instant::now() < deadline, "policy change was not sealed");
        let output = fixture
            .furrow()
            .args(["--json", "timeline", "--limit", "1"])
            .output()
            .unwrap();
        if output.status.success() {
            let value: Value = serde_json::from_slice(&output.stdout).unwrap();
            let head = value[0]["id"].as_str().unwrap();
            if head != before && value[0]["trigger"] == "watcher" {
                break head.to_owned();
            }
        }
        std::thread::sleep(Duration::from_millis(75));
    };

    fs::write(
        fixture.repo.join("cache/dependency.bin"),
        vec![3_u8; 180_000],
    )
    .unwrap();
    std::thread::sleep(Duration::from_millis(500));
    let after = fixture
        .furrow()
        .args(["--json", "timeline", "--limit", "1"])
        .output()
        .unwrap();
    let after: Value = serde_json::from_slice(&after.stdout).unwrap();
    child.kill().ok();
    child.wait().ok();
    assert_eq!(after[0]["id"], policy_head);
}

#[test]
fn default_watch_starts_background_protection_and_forget_stops_it() {
    let fixture = Fixture::new();
    std::process::Command::new(env!("CARGO_BIN_EXE_furrow"))
        .env("FURROW_DATA_DIR", &fixture.data)
        .arg("--repo")
        .arg(&fixture.repo)
        .arg("watch")
        .status()
        .unwrap()
        .success()
        .then_some(())
        .expect("watch failed");

    let output = fixture
        .furrow()
        .args(["--json", "status"])
        .output()
        .unwrap();
    let status: Value = serde_json::from_slice(&output.stdout).unwrap();
    assert_eq!(status["watcher_running"], true);

    fixture.furrow().arg("forget").assert().success();
    let workspace = fixture.data.join("store-v1/workspaces");
    let pid_files: Vec<_> = fs::read_dir(workspace)
        .unwrap()
        .filter_map(Result::ok)
        .map(|entry| entry.path().join("daemon.pid"))
        .filter(|path| path.exists())
        .collect();
    assert!(pid_files.is_empty());
}

#[test]
fn recovery_rediscovers_workspace_after_git_clean_removes_pointer() {
    let fixture = Fixture::new();
    let snapshot = fixture.watch();
    git(&fixture.repo, &["clean", "-fdx"]);
    assert!(!fixture.repo.join(".furrow/workspace-id").exists());
    assert!(!fixture.repo.join(".env").exists());

    fixture
        .furrow()
        .args(["rewind", &snapshot, "--yes"])
        .assert()
        .success();
    assert_eq!(
        fs::read(fixture.repo.join(".env")).unwrap(),
        b"TOKEN=original\n"
    );
    assert!(fixture.repo.join(".furrow/workspace-id").exists());
}

#[test]
fn plain_forget_stays_detached_instead_of_being_rediscovered() {
    let fixture = Fixture::new();
    fixture.watch();
    let workspace_id = fs::read_to_string(fixture.repo.join(".furrow/workspace-id")).unwrap();

    fixture.furrow().arg("forget").assert().success();
    assert!(!fixture.repo.join(".furrow/workspace-id").exists());
    fixture
        .furrow()
        .arg("status")
        .assert()
        .failure()
        .stderr(predicates::str::contains("not watched"));
    let output = fixture
        .furrow()
        .args(["--json", "gc", "--dry-run"])
        .assert()
        .success()
        .get_output()
        .stdout
        .clone();
    let report: Value = serde_json::from_slice(&output).unwrap();
    assert!(report["roots"].as_u64().unwrap() > 0);

    fixture
        .furrow()
        .args(["watch", "--no-daemon"])
        .assert()
        .success();
    let new_workspace_id = fs::read_to_string(fixture.repo.join(".furrow/workspace-id")).unwrap();
    assert_ne!(workspace_id, new_workspace_id);
}

#[test]
fn purge_then_gc_reclaims_only_unshared_history() {
    let fixture = Fixture::new();
    fixture.watch();

    let second = fixture.repo.parent().unwrap().join("second");
    git(
        fixture.repo.parent().unwrap(),
        &["clone", fixture.repo.to_str().unwrap(), "second"],
    );
    let second_furrow = || {
        let mut command = Command::cargo_bin("furrow").unwrap();
        command
            .env("FURROW_DATA_DIR", &fixture.data)
            .env("FURROW_NO_DAEMON", "1")
            .arg("--repo")
            .arg(&second);
        command
    };
    let output = second_furrow()
        .args(["--json", "watch", "--no-daemon"])
        .assert()
        .success()
        .get_output()
        .stdout
        .clone();
    let second_snapshot = serde_json::from_slice::<Value>(&output).unwrap()["snapshot"]
        .as_str()
        .unwrap()
        .to_owned();

    fixture
        .furrow()
        .args(["forget", "--purge"])
        .assert()
        .success();
    let preview_output = fixture
        .furrow()
        .args(["--json", "gc", "--dry-run"])
        .assert()
        .success()
        .get_output()
        .stdout
        .clone();
    let preview: Value = serde_json::from_slice(&preview_output).unwrap();
    assert_eq!(preview["dry_run"], true);
    assert!(preview["unreachable_objects"].as_u64().unwrap() > 0);

    let output = fixture
        .furrow()
        .args(["--json", "gc"])
        .assert()
        .success()
        .get_output()
        .stdout
        .clone();
    let report: Value = serde_json::from_slice(&output).unwrap();
    assert!(report["unreachable_objects"].as_u64().unwrap() > 0);
    assert!(report["reclaimed_bytes"].as_u64().unwrap() > 0);

    fs::write(second.join("app.txt"), b"destroyed after gc\n").unwrap();
    second_furrow()
        .args(["rewind", &second_snapshot, "--paths", "app.txt", "--yes"])
        .assert()
        .success();
    assert_eq!(
        fs::read(second.join("app.txt")).unwrap(),
        b"tracked original\n"
    );
}

#[test]
fn encrypted_two_store_sync_fast_forwards_and_preserves_divergence() {
    let fixture = Fixture::new();
    let peer = fixture.repo.parent().unwrap().join("peer");
    let peer_data = fixture.repo.parent().unwrap().join("peer-data");
    let remote = fixture.repo.parent().unwrap().join("remote");
    git(
        fixture.repo.parent().unwrap(),
        &["clone", fixture.repo.to_str().unwrap(), "peer"],
    );
    fixture.watch();
    furrow_at(&peer, &peer_data)
        .args(["watch", "--no-daemon"])
        .assert()
        .success();

    let pair_output = fixture
        .furrow()
        .args([
            "--json",
            "pair",
            remote.to_str().unwrap(),
            "--name",
            "shared-workspace",
        ])
        .assert()
        .success()
        .get_output()
        .stdout
        .clone();
    let pair: Value = serde_json::from_slice(&pair_output).unwrap();
    let key = pair["key_hex"].as_str().unwrap();
    furrow_at(&peer, &peer_data)
        .args([
            "pair",
            remote.to_str().unwrap(),
            "--name",
            "shared-workspace",
            "--key",
            key,
        ])
        .assert()
        .success();

    fixture.furrow().args(["sync", "--push"]).assert().success();
    let bootstrap_output = furrow_at(&peer, &peer_data)
        .args(["--json", "sync", "--pull", "--bootstrap"])
        .assert()
        .success()
        .get_output()
        .stdout
        .clone();
    let bootstrap: Value = serde_json::from_slice(&bootstrap_output).unwrap();
    assert_eq!(bootstrap["disposition"], "bootstrapped");
    assert_eq!(fs::read(peer.join(".env")).unwrap(), b"TOKEN=original\n");
    assert_eq!(
        fs::read(peer.join("cache/dependency.bin")).unwrap(),
        vec![7_u8; 180_000]
    );
    let unchanged_inode = fs::metadata(peer.join("cache/dependency.bin"))
        .unwrap()
        .ino();

    fs::write(fixture.repo.join("app.txt"), b"remote delta\n").unwrap();
    let second_push = fixture
        .furrow()
        .args(["--json", "sync", "--push"])
        .assert()
        .success()
        .get_output()
        .stdout
        .clone();
    let second_push: Value = serde_json::from_slice(&second_push).unwrap();
    assert!(second_push["reused_objects"].as_u64().unwrap() > 0);
    assert!(second_push["uploaded_objects"].as_u64().unwrap() < 10);
    let mut pull_command = furrow_at(&peer, &peer_data);
    let pull = pull_command
        .args(["--json", "sync", "--pull", "--timings"])
        .assert()
        .success();
    let pull_stderr = String::from_utf8_lossy(&pull.get_output().stderr);
    for phase in [
        "diff-compute=",
        "divergence-check=",
        "write=",
        "fsync=",
        "baseline-install=",
        "watcher-requiesce=",
    ] {
        assert!(
            pull_stderr.contains(phase),
            "pull --timings omitted apply phase {phase}: {pull_stderr}"
        );
    }
    let pull: Value = serde_json::from_slice(&pull.get_output().stdout).unwrap();
    assert_eq!(pull["disposition"], "fast_forwarded");
    assert_eq!(
        pull["local_snapshot"], second_push["snapshot"],
        "a clean receiver must adopt the already-sealed remote identity"
    );
    assert_eq!(
        pull["remote_snapshot"], second_push["snapshot"],
        "the authenticated remote head must remain the adopted identity"
    );
    assert_eq!(fs::read(peer.join("app.txt")).unwrap(), b"remote delta\n");
    assert_eq!(
        fs::metadata(peer.join("cache/dependency.bin"))
            .unwrap()
            .ino(),
        unchanged_inode,
        "delta materialization must not replace an unchanged repository path"
    );
    let status = furrow_at(&peer, &peer_data)
        .args(["--json", "status"])
        .assert()
        .success()
        .get_output()
        .stdout
        .clone();
    let status: Value = serde_json::from_slice(&status).unwrap();
    assert_eq!(status["head"], second_push["snapshot"]);

    fs::write(peer.join("notes.txt"), b"offline peer work\n").unwrap();
    fs::write(fixture.repo.join("app.txt"), b"new remote work\n").unwrap();
    fixture.furrow().args(["sync", "--push"]).assert().success();
    let divergence_output = furrow_at(&peer, &peer_data)
        .args(["--json", "sync", "--pull"])
        .assert()
        .failure()
        .get_output()
        .stdout
        .clone();
    let divergence: Value = serde_json::from_slice(&divergence_output).unwrap();
    assert_eq!(divergence["disposition"], "diverged");
    assert_eq!(
        fs::read(peer.join("notes.txt")).unwrap(),
        b"offline peer work\n"
    );

    // The incoming sibling remains an exact GC root and can still be
    // materialized by its full authenticated snapshot ID after compaction.
    furrow_at(&peer, &peer_data).arg("gc").assert().success();
    furrow_at(&peer, &peer_data)
        .args([
            "rewind",
            divergence["remote_snapshot"].as_str().unwrap(),
            "--dry-run",
        ])
        .assert()
        .success();
    furrow_at(&peer, &peer_data)
        .args(["sync", "--push", "--takeover"])
        .assert()
        .failure()
        .stderr(predicates::str::contains(
            "remote workspace changed since this machine last synchronized",
        ));
    fs::write(
        fixture.repo.join("after-stale-attempt.txt"),
        b"writer retained\n",
    )
    .unwrap();
    fixture.furrow().args(["sync", "--push"]).assert().success();

    let mut remote_files = Vec::new();
    collect_files(&remote, &mut remote_files);
    for path in remote_files {
        let bytes = fs::read(path).unwrap();
        assert!(!bytes
            .windows(b"TOKEN=original".len())
            .any(|window| window == b"TOKEN=original"));
    }
}

#[test]
fn sync_ref_flag_publishes_and_pulls_an_independent_named_head() {
    let fixture = Fixture::new();
    let peer = fixture.repo.parent().unwrap().join("peer");
    let peer_data = fixture.repo.parent().unwrap().join("peer-data");
    let remote = fixture.repo.parent().unwrap().join("remote");
    git(
        fixture.repo.parent().unwrap(),
        &["clone", fixture.repo.to_str().unwrap(), "peer"],
    );
    fixture.watch();
    furrow_at(&peer, &peer_data)
        .args(["watch", "--no-daemon"])
        .assert()
        .success();

    let pair_output = fixture
        .furrow()
        .args([
            "--json",
            "pair",
            remote.to_str().unwrap(),
            "--name",
            "shared-workspace",
        ])
        .assert()
        .success()
        .get_output()
        .stdout
        .clone();
    let pair: Value = serde_json::from_slice(&pair_output).unwrap();
    let key = pair["key_hex"].as_str().unwrap();
    furrow_at(&peer, &peer_data)
        .args([
            "pair",
            remote.to_str().unwrap(),
            "--name",
            "shared-workspace",
            "--key",
            key,
        ])
        .assert()
        .success();

    // Publish under a named ref rather than the default HEAD.
    fixture
        .furrow()
        .args(["sync", "--push", "--ref", "team-a"])
        .assert()
        .success();

    // Pulling a ref that was never published fails with a clear error
    // naming the missing ref, not a generic one.
    furrow_at(&peer, &peer_data)
        .args(["sync", "--pull", "--ref", "does-not-exist"])
        .assert()
        .failure()
        .stderr(predicates::str::contains("does-not-exist"));

    // Pulling the actual named ref materializes it on a clean receiver.
    let pull_output = furrow_at(&peer, &peer_data)
        .args(["--json", "sync", "--pull", "--ref", "team-a", "--bootstrap"])
        .assert()
        .success()
        .get_output()
        .stdout
        .clone();
    let pull: Value = serde_json::from_slice(&pull_output).unwrap();
    assert_eq!(pull["disposition"], "bootstrapped");
    assert_eq!(
        fs::read(peer.join("app.txt")).unwrap(),
        b"tracked original\n"
    );

    // The default, ref-less HEAD/LEASE keys stay untouched; the named ref
    // lives in its own nested, independent slot.
    let namespace_root = fs::read_dir(&remote)
        .unwrap()
        .next()
        .unwrap()
        .unwrap()
        .path();
    assert!(!namespace_root.join("HEAD").exists());
    assert!(!namespace_root.join("LEASE").exists());
    assert!(namespace_root.join("refs/team-a/HEAD").is_file());
    assert!(namespace_root.join("refs/team-a/LEASE").is_file());
}

#[test]
fn remote_add_and_follow_keep_two_directory_backed_machines_current_with_small_deltas() {
    let fixture = Fixture::new();
    let peer = fixture.repo.parent().unwrap().join("follow-peer");
    let peer_data = fixture.repo.parent().unwrap().join("follow-peer-data");
    let remote = fixture.repo.parent().unwrap().join("follow-remote");
    git(
        fixture.repo.parent().unwrap(),
        &["clone", fixture.repo.to_str().unwrap(), "follow-peer"],
    );
    fixture.watch();
    furrow_at(&peer, &peer_data)
        .args(["watch", "--no-daemon"])
        .assert()
        .success();

    let add = fixture
        .furrow()
        .args([
            "--json",
            "remote",
            "add",
            remote.to_str().unwrap(),
            "--name",
            "live-workspace",
        ])
        .assert()
        .success()
        .get_output()
        .stdout
        .clone();
    let add: Value = serde_json::from_slice(&add).unwrap();
    assert_eq!(add["namespace"], "live-workspace");
    assert_eq!(
        PathBuf::from(add["remote"].as_str().unwrap()),
        fs::canonicalize(&remote).unwrap()
    );
    let key = add["key_hex"].as_str().unwrap();
    assert_eq!(key.len(), 64);

    furrow_at(&peer, &peer_data)
        .args([
            "remote",
            "add",
            remote.to_str().unwrap(),
            "--name",
            "live-workspace",
            "--key",
            key,
        ])
        .assert()
        .success();

    let _publisher = follow_process(&fixture.repo, &fixture.data);
    wait_until(
        "the first followed snapshot to publish",
        Duration::from_secs(10),
        || {
            let mut files = Vec::new();
            collect_files(&remote, &mut files);
            files.iter().any(|path| path.file_name().unwrap() == "HEAD")
        },
    );
    let bootstrap = furrow_at(&peer, &peer_data)
        .args(["--json", "sync", "--pull", "--bootstrap"])
        .assert()
        .success()
        .get_output()
        .stdout
        .clone();
    let bootstrap: Value = serde_json::from_slice(&bootstrap).unwrap();
    assert_eq!(bootstrap["disposition"], "bootstrapped");
    assert_eq!(fs::read(peer.join(".env")).unwrap(), b"TOKEN=original\n");

    let _subscriber = follow_process(&peer, &peer_data);
    let bytes_before_delta = tree_physical_bytes(&remote);
    fs::write(fixture.repo.join("app.txt"), b"one small agent edit\n").unwrap();
    fixture
        .furrow()
        .args(["snap", "-m", "agent completed report edit"])
        .assert()
        .success();

    wait_until(
        "the peer to receive the followed delta",
        Duration::from_secs(12),
        || fs::read(peer.join("app.txt")).ok().as_deref() == Some(b"one small agent edit\n"),
    );
    let delta_bytes = tree_physical_bytes(&remote).saturating_sub(bytes_before_delta);
    assert!(
        delta_bytes < 64 * 1024,
        "a tiny edit unexpectedly added {delta_bytes} remote bytes"
    );
    assert_eq!(
        fs::read(peer.join("cache/dependency.bin")).unwrap(),
        vec![7_u8; 180_000]
    );
}

#[test]
fn network_clone_materializes_exact_state_and_removes_failed_destinations() {
    let fixture = Fixture::new();
    let remote_data = fixture.repo.parent().unwrap().join("clone-remote-data");
    let clone_data = fixture.repo.parent().unwrap().join("clone-client-data");
    let wrapper = fixture.repo.parent().unwrap().join("clone-fake-ssh.sh");
    fs::write(
        &wrapper,
        b"#!/bin/sh\nwhile [ \"$1\" != \"--\" ]; do shift; done\nshift\nshift\nshift\nexec \"$FURROW_TEST_BIN\" \"$@\"\n",
    )
    .unwrap();
    let mut mode = fs::metadata(&wrapper).unwrap().permissions();
    mode.set_mode(0o755);
    fs::set_permissions(&wrapper, mode).unwrap();
    fixture.watch();

    let ssh_furrow = |repo: &Path, data: &Path| {
        let mut command = furrow_at(repo, data);
        command
            .env("FURROW_SSH_COMMAND", &wrapper)
            .env("FURROW_REMOTE_DATA_DIR", &remote_data)
            .env("FURROW_TEST_BIN", env!("CARGO_BIN_EXE_furrow"));
        command
    };
    let add = ssh_furrow(&fixture.repo, &fixture.data)
        .args([
            "--json",
            "remote",
            "add",
            "ssh://fake-host",
            "--name",
            "clone-workspace",
        ])
        .assert()
        .success()
        .get_output()
        .stdout
        .clone();
    let add: Value = serde_json::from_slice(&add).unwrap();
    let key = add["key_hex"].as_str().unwrap();
    ssh_furrow(&fixture.repo, &fixture.data)
        .args(["sync", "--push"])
        .assert()
        .success();

    let failed = fixture.repo.parent().unwrap().join("failed-clone");
    ssh_furrow(&fixture.repo, &clone_data)
        .args([
            "clone",
            "ssh://fake-host/clone-workspace",
            failed.to_str().unwrap(),
            "--key",
            &"00".repeat(32),
            "--no-watch",
        ])
        .assert()
        .failure()
        .stderr(predicates::str::contains(
            "incomplete destination was removed",
        ));
    assert!(!failed.exists());

    let destination = fixture.repo.parent().unwrap().join("network-clone");
    let output = ssh_furrow(&fixture.repo, &clone_data)
        .args([
            "--json",
            "clone",
            "ssh://fake-host/clone-workspace",
            destination.to_str().unwrap(),
            "--key",
            key,
            "--no-watch",
        ])
        .assert()
        .success()
        .get_output()
        .stdout
        .clone();
    let output: Value = serde_json::from_slice(&output).unwrap();
    assert_eq!(output["sync"]["disposition"], "bootstrapped");
    assert_eq!(
        fs::read(destination.join(".env")).unwrap(),
        b"TOKEN=original\n"
    );
    assert_eq!(
        fs::read(destination.join("cache/dependency.bin")).unwrap(),
        vec![7_u8; 180_000]
    );
    assert_eq!(
        fs::read_link(destination.join("app-link")).unwrap(),
        PathBuf::from("app.txt")
    );
}

#[test]
fn persistent_ssh_helper_syncs_independent_stores_over_framed_stdio() {
    let fixture = Fixture::new();
    let peer = fixture.repo.parent().unwrap().join("ssh-peer");
    let peer_data = fixture.repo.parent().unwrap().join("ssh-peer-data");
    let remote_data = fixture.repo.parent().unwrap().join("ssh-remote-data");
    let wrapper = fixture.repo.parent().unwrap().join("fake-ssh.sh");
    git(
        fixture.repo.parent().unwrap(),
        &["clone", fixture.repo.to_str().unwrap(), "ssh-peer"],
    );
    fs::write(
        &wrapper,
        b"#!/bin/sh\ntest -z \"${FURROW_SSH_START_LOG:-}\" || printf 'start\\n' >> \"$FURROW_SSH_START_LOG\"\nwhile [ \"$1\" != \"--\" ]; do shift; done\nshift\nshift\nshift\nexec \"$FURROW_TEST_BIN\" \"$@\"\n",
    )
    .unwrap();
    let mut mode = fs::metadata(&wrapper).unwrap().permissions();
    mode.set_mode(0o755);
    fs::set_permissions(&wrapper, mode).unwrap();

    fixture.watch();
    furrow_at(&peer, &peer_data)
        .args(["watch", "--no-daemon"])
        .assert()
        .success();
    let ssh_furrow = |repo: &Path, data: &Path| {
        let mut command = furrow_at(repo, data);
        command
            .env("FURROW_SSH_COMMAND", &wrapper)
            .env("FURROW_REMOTE_DATA_DIR", &remote_data)
            .env("FURROW_TEST_BIN", env!("CARGO_BIN_EXE_furrow"));
        command
    };

    let pair_output = ssh_furrow(&fixture.repo, &fixture.data)
        .args([
            "--json",
            "pair",
            "ssh://fake-host",
            "--name",
            "ssh-workspace",
        ])
        .assert()
        .success()
        .get_output()
        .stdout
        .clone();
    let pair: Value = serde_json::from_slice(&pair_output).unwrap();
    assert_eq!(pair["remote"], "ssh://fake-host");
    let key = pair["key_hex"].as_str().unwrap();
    ssh_furrow(&peer, &peer_data)
        .args([
            "pair",
            "ssh://fake-host",
            "--name",
            "ssh-workspace",
            "--key",
            key,
        ])
        .assert()
        .success();
    ssh_furrow(&fixture.repo, &fixture.data)
        .args(["sync", "--push"])
        .assert()
        .success();
    let pull = ssh_furrow(&peer, &peer_data)
        .args(["--json", "sync", "--pull", "--bootstrap"])
        .assert()
        .success()
        .get_output()
        .stdout
        .clone();
    let pull: Value = serde_json::from_slice(&pull).unwrap();
    assert_eq!(pull["disposition"], "bootstrapped");
    assert_eq!(fs::read(peer.join(".env")).unwrap(), b"TOKEN=original\n");
    assert_eq!(
        fs::read(peer.join("cache/dependency.bin")).unwrap().len(),
        180_000
    );

    fs::write(fixture.repo.join("app.txt"), b"ssh delta\n").unwrap();
    let push = ssh_furrow(&fixture.repo, &fixture.data)
        .args(["--json", "sync", "--push"])
        .assert()
        .success()
        .get_output()
        .stdout
        .clone();
    let push: Value = serde_json::from_slice(&push).unwrap();
    assert!(push["uploaded_objects"].as_u64().unwrap() < 10);
    let pull = ssh_furrow(&peer, &peer_data)
        .args(["--json", "sync", "--pull"])
        .assert()
        .success()
        .get_output()
        .stdout
        .clone();
    let pull: Value = serde_json::from_slice(&pull).unwrap();
    assert_eq!(pull["disposition"], "fast_forwarded");
    assert_eq!(fs::read(peer.join("app.txt")).unwrap(), b"ssh delta\n");

    let start_log = fixture.repo.parent().unwrap().join("ssh-starts.log");
    let timing_log = fixture.repo.parent().unwrap().join("follow-timings.log");
    let follower = std::process::Command::new(env!("CARGO_BIN_EXE_furrow"))
        .env("FURROW_DATA_DIR", &fixture.data)
        .env("FURROW_NO_DAEMON", "1")
        .env("FURROW_SSH_COMMAND", &wrapper)
        .env("FURROW_REMOTE_DATA_DIR", &remote_data)
        .env("FURROW_TEST_BIN", env!("CARGO_BIN_EXE_furrow"))
        .env("FURROW_SSH_START_LOG", &start_log)
        .arg("--repo")
        .arg(&fixture.repo)
        .args(["sync", "--follow", "--poll-seconds", "1", "--timings"])
        .stdout(std::process::Stdio::null())
        .stderr(fs::File::create(&timing_log).unwrap())
        .spawn()
        .unwrap();
    let follower = ChildGuard(follower);
    wait_until("follow session to connect", Duration::from_secs(5), || {
        start_log.exists()
    });
    std::thread::sleep(Duration::from_millis(100));

    fs::write(peer.join("app.txt"), b"warm session notification\n").unwrap();
    furrow_at(&peer, &peer_data)
        .args(["snap", "-m", "peer notification"])
        .assert()
        .success();
    let notify_started = Instant::now();
    let publish = ssh_furrow(&peer, &peer_data)
        .args(["sync", "--push", "--takeover", "--timings"])
        .assert()
        .success();
    assert!(
        String::from_utf8_lossy(&publish.get_output().stderr).contains("sync timings:"),
        "--timings must expose the transport phase log"
    );
    wait_until(
        "warm follower to materialize the notified head",
        Duration::from_secs(2),
        || {
            fs::read(fixture.repo.join("app.txt")).ok().as_deref()
                == Some(b"warm session notification\n")
        },
    );
    let notify_elapsed = notify_started.elapsed();
    assert!(
        notify_elapsed < Duration::from_secs(2),
        "a local warm-session publish should not wait for the fallback poll interval"
    );
    eprintln!(
        "warm_session_publish_to_materialize_ms={}",
        notify_elapsed.as_millis()
    );
    std::thread::sleep(Duration::from_millis(1100));
    drop(follower);
    assert_eq!(
        fs::read_to_string(start_log).unwrap().lines().count(),
        1,
        "follow must reuse one SSH process across reconciliation cycles"
    );
    let follow_timings = fs::read_to_string(timing_log).unwrap();
    assert!(
        follow_timings.contains("reused_connection=true"),
        "the second operation on a follow session must pay zero connection cost"
    );
    assert!(
        follow_timings.contains("notify=") && !follow_timings.contains("notify=n/a"),
        "a live-session pull must measure durable publish-to-notify latency"
    );
}

#[test]
fn mcp_stdio_negotiates_lifecycle_lists_tools_and_keeps_errors_in_protocol() {
    let fixture = Fixture::new();
    let snapshot = fixture.watch();
    fs::write(fixture.repo.join(".env"), b"TOKEN=changed\n").unwrap();

    let mut child = std::process::Command::new(env!("CARGO_BIN_EXE_furrow"))
        .env("FURROW_DATA_DIR", &fixture.data)
        .env("FURROW_NO_DAEMON", "1")
        .arg("--repo")
        .arg(&fixture.repo)
        .arg("mcp")
        .stdin(std::process::Stdio::piped())
        .stdout(std::process::Stdio::piped())
        .stderr(std::process::Stdio::piped())
        .spawn()
        .unwrap();
    let requests = [
        serde_json::json!({"jsonrpc":"2.0","id":1,"method":"tools/list"}),
        serde_json::json!({
            "jsonrpc":"2.0","id":2,"method":"initialize","params":{
                "protocolVersion":"2025-11-25","capabilities":{},
                "clientInfo":{"name":"furrow-test","version":"1"}
            }
        }),
        serde_json::json!({"jsonrpc":"2.0","method":"notifications/initialized"}),
        serde_json::json!({"jsonrpc":"2.0","id":3,"method":"tools/list"}),
        serde_json::json!({
            "jsonrpc":"2.0","id":"status","method":"tools/call",
            "params":{"name":"furrow.status","arguments":{"fidelity":true}}
        }),
        serde_json::json!({
            "jsonrpc":"2.0","id":5,"method":"tools/call",
            "params":{"name":"furrow.snapshot","arguments":{"message":"agent boundary"}}
        }),
        serde_json::json!({
            "jsonrpc":"2.0","id":6,"method":"tools/call",
            "params":{"name":"furrow.rewind_apply","arguments":{"snapshot":snapshot}}
        }),
        serde_json::json!({
            "jsonrpc":"2.0","id":"forks","method":"tools/call",
            "params":{"name":"furrow.forks","arguments":{}}
        }),
        serde_json::json!({
            "jsonrpc":"2.0","id":"events","method":"tools/call",
            "params":{"name":"furrow.events","arguments":{"limit":10}}
        }),
        serde_json::json!({
            "jsonrpc":"2.0","id":7,"method":"tools/call",
            "params":{"name":"furrow.unknown","arguments":{}}
        }),
    ];
    {
        let stdin = child.stdin.as_mut().unwrap();
        for request in requests {
            serde_json::to_writer(&mut *stdin, &request).unwrap();
            stdin.write_all(b"\n").unwrap();
        }
    }
    drop(child.stdin.take());
    let output = child.wait_with_output().unwrap();
    assert!(
        output.status.success(),
        "{}",
        String::from_utf8_lossy(&output.stderr)
    );
    let responses: Vec<Value> = String::from_utf8(output.stdout)
        .unwrap()
        .lines()
        .map(|line| serde_json::from_str(line).unwrap())
        .collect();
    assert_eq!(responses.len(), 9);
    assert_eq!(responses[0]["error"]["code"], -32002);
    assert_eq!(responses[1]["result"]["protocolVersion"], "2025-11-25");
    let tools = responses[2]["result"]["tools"].as_array().unwrap();
    assert!(tools
        .iter()
        .any(|tool| tool["name"] == "furrow.rewind_plan"));
    assert!(tools.iter().any(|tool| tool["name"] == "furrow.fork"));
    assert!(tools.iter().any(|tool| tool["name"] == "furrow.claim"));
    assert!(tools
        .iter()
        .any(|tool| tool["name"] == "furrow.coord_write"));
    assert!(tools
        .iter()
        .any(|tool| tool["name"] == "furrow.fork_updates"));
    assert!(tools.iter().any(|tool| tool["name"] == "furrow.forks"));
    assert!(tools.iter().any(|tool| tool["name"] == "furrow.events"));
    assert_eq!(responses[3]["id"], "status");
    assert_eq!(responses[3]["result"]["isError"], false);
    assert_eq!(
        responses[3]["result"]["structuredContent"]["fidelity"]["grade"],
        "partial"
    );
    assert_eq!(responses[4]["result"]["isError"], false);
    assert_eq!(responses[5]["result"]["isError"], true);
    assert!(responses[5]["result"]["content"][0]["text"]
        .as_str()
        .unwrap()
        .contains("confirm_snapshot"));
    assert_eq!(responses[6]["id"], "forks");
    assert_eq!(
        responses[6]["result"]["structuredContent"],
        serde_json::json!([])
    );
    assert_eq!(responses[7]["id"], "events");
    assert_eq!(
        responses[7]["result"]["structuredContent"]["events"],
        serde_json::json!([])
    );
    assert_eq!(responses[8]["error"]["code"], -32602);
}

#[test]
fn snapshots_can_be_pinned_and_unpinned_idempotently() {
    let fixture = Fixture::new();
    let snapshot = fixture.watch();

    let pin = fixture
        .furrow()
        .args(["--json", "pin", &snapshot])
        .assert()
        .success()
        .get_output()
        .stdout
        .clone();
    let pin: Value = serde_json::from_slice(&pin).unwrap();
    assert_eq!(pin["snapshot"], snapshot);
    assert_eq!(pin["pinned"], true);
    assert_eq!(pin["changed"], true);

    let repeated = fixture
        .furrow()
        .args(["--json", "pin", &snapshot])
        .assert()
        .success()
        .get_output()
        .stdout
        .clone();
    assert_eq!(
        serde_json::from_slice::<Value>(&repeated).unwrap()["changed"],
        false
    );

    let unpin = fixture
        .furrow()
        .args(["--json", "unpin", &snapshot])
        .assert()
        .success()
        .get_output()
        .stdout
        .clone();
    let unpin: Value = serde_json::from_slice(&unpin).unwrap();
    assert_eq!(unpin["snapshot"], snapshot);
    assert_eq!(unpin["pinned"], false);
    assert_eq!(unpin["changed"], true);
}

#[test]
fn global_budget_is_persistent_visible_and_never_deletes_the_head_to_fit() {
    let fixture = Fixture::new();
    let head = fixture.watch();
    let configured = fixture
        .furrow()
        .args(["--json", "budget", "--max", "1", "--reserve-free", "0"])
        .assert()
        .success()
        .get_output()
        .stdout
        .clone();
    let configured: Value = serde_json::from_slice(&configured).unwrap();
    assert_eq!(configured["max_store_bytes"], 1);
    assert_eq!(configured["reserved_free_bytes"], 0);
    assert_eq!(configured["satisfied"], false);
    assert!(configured["over_store_bytes"].as_u64().unwrap() > 0);

    let status = fixture
        .furrow()
        .args(["--json", "status"])
        .assert()
        .success()
        .get_output()
        .stdout
        .clone();
    let status: Value = serde_json::from_slice(&status).unwrap();
    assert_eq!(status["head"], head);
    assert_eq!(status["budget"]["max_store_bytes"], 1);
    assert_eq!(status["budget"]["satisfied"], false);

    let timeline = fixture
        .furrow()
        .args(["--json", "timeline", "--limit", "1"])
        .assert()
        .success()
        .get_output()
        .stdout
        .clone();
    let timeline: Value = serde_json::from_slice(&timeline).unwrap();
    assert_eq!(timeline[0]["id"], head);
    assert_eq!(timeline[0]["materialization"]["grade"], "exact");
}

#[derive(Debug, PartialEq, Eq)]
enum ManifestEntry {
    File {
        content_hash: [u8; 32],
        mode: u32,
        mtime: (i64, i64),
    },
    Directory {
        mode: u32,
    },
    Symlink {
        target: PathBuf,
    },
}

/// A path-sorted, content-hashed description of everything under `root`
/// except the internal `.furrow` directory, suitable for asserting that two
/// independently materialized workspaces are byte-identical.
fn directory_manifest(root: &Path) -> Vec<(PathBuf, ManifestEntry)> {
    fn walk(root: &Path, dir: &Path, entries: &mut Vec<(PathBuf, ManifestEntry)>) {
        for entry in fs::read_dir(dir).unwrap() {
            let entry = entry.unwrap();
            let path = entry.path();
            let relative = path.strip_prefix(root).unwrap().to_path_buf();
            if relative == Path::new(".furrow") {
                continue;
            }
            let metadata = fs::symlink_metadata(&path).unwrap();
            let file_type = metadata.file_type();
            if file_type.is_symlink() {
                entries.push((
                    relative,
                    ManifestEntry::Symlink {
                        target: fs::read_link(&path).unwrap(),
                    },
                ));
            } else if file_type.is_dir() {
                entries.push((
                    relative,
                    ManifestEntry::Directory {
                        mode: metadata.permissions().mode() & 0o777,
                    },
                ));
                walk(root, &path, entries);
            } else {
                let content_hash = *blake3::hash(&fs::read(&path).unwrap()).as_bytes();
                entries.push((
                    relative,
                    ManifestEntry::File {
                        content_hash,
                        mode: metadata.permissions().mode() & 0o777,
                        mtime: (metadata.mtime(), metadata.mtime_nsec()),
                    },
                ));
            }
        }
    }
    let mut entries = Vec::new();
    walk(root, root, &mut entries);
    entries.sort_by(|left, right| left.0.cmp(&right.0));
    entries
}

#[test]
fn fork_at_snapshot_matches_rewind_and_does_not_touch_the_source() {
    let fixture = Fixture::new();
    let base = fixture.watch();

    // Diverge the live workspace after the snapshot was sealed.
    fs::write(fixture.repo.join("app.txt"), b"diverged after snapshot\n").unwrap();
    fs::write(
        fixture.repo.join("new-file.txt"),
        b"created after snapshot\n",
    )
    .unwrap();
    fs::remove_file(fixture.repo.join("notes.txt")).unwrap();

    let at_dest = fixture._temp.path().join("fork-at-dest");
    fixture
        .furrow()
        .args(["fork", "snap-fork", "--at", &base, "--destination"])
        .arg(&at_dest)
        .assert()
        .success();

    // Independently reproduce the same snapshot by rewinding a plain live
    // fork, and compare the two resulting trees byte-for-byte.
    let check_dest = fixture._temp.path().join("rewind-check-dest");
    fixture
        .furrow()
        .args(["fork", "rewind-check", "--destination"])
        .arg(&check_dest)
        .assert()
        .success();
    furrow_at(&check_dest, &fixture.data)
        .args(["rewind", &base, "--yes"])
        .assert()
        .success();

    assert_eq!(
        directory_manifest(&at_dest),
        directory_manifest(&check_dest)
    );

    // The source workspace was not touched by `fork --at`.
    assert_eq!(
        fs::read(fixture.repo.join("app.txt")).unwrap(),
        b"diverged after snapshot\n"
    );
    assert!(fixture.repo.join("new-file.txt").exists());
    assert!(!fixture.repo.join("notes.txt").exists());

    let forks = fixture
        .furrow()
        .args(["--json", "forks"])
        .assert()
        .success()
        .get_output()
        .stdout
        .clone();
    let forks: Value = serde_json::from_slice(&forks).unwrap();
    let snap_fork = forks
        .as_array()
        .unwrap()
        .iter()
        .find(|fork| fork["name"] == "snap-fork")
        .unwrap();
    assert_eq!(snap_fork["base_snapshot"], base);
}

#[test]
fn merge_of_a_snapshot_fork_uses_the_specified_base_despite_source_divergence() {
    let fixture = Fixture::new();
    let base = fixture.watch();

    // The source moves on after `base` was sealed.
    fs::write(fixture.repo.join("app.txt"), b"source diverged\n").unwrap();
    fixture.furrow().arg("snap").assert().success();

    let fork_dest = fixture._temp.path().join("fork-at-merge");
    fixture
        .furrow()
        .args(["fork", "at-merge", "--at", &base, "--destination"])
        .arg(&fork_dest)
        .assert()
        .success();
    // The fork itself still reflects `base`, not the source's later edit.
    assert_eq!(
        fs::read(fork_dest.join("app.txt")).unwrap(),
        b"tracked original\n"
    );
    fs::write(
        fork_dest.join("agent-result.txt"),
        b"fork-only work based on base\n",
    )
    .unwrap();

    fixture
        .furrow()
        .args([
            "merge",
            "at-merge",
            "--check",
            "grep -q 'source diverged' app.txt && grep -q 'fork-only work based on base' agent-result.txt",
        ])
        .assert()
        .success();

    // If the merge base had incorrectly been the source's current head
    // instead of `base`, app.txt would look unchanged there and the fork's
    // reversion to the old content would "win", clobbering the source edit.
    assert_eq!(
        fs::read(fixture.repo.join("app.txt")).unwrap(),
        b"source diverged\n"
    );
    assert_eq!(
        fs::read(fixture.repo.join("agent-result.txt")).unwrap(),
        b"fork-only work based on base\n"
    );
}

#[test]
fn fork_at_rejects_an_unresolvable_snapshot_without_creating_the_destination() {
    let fixture = Fixture::new();
    fixture.watch();
    let dest = fixture._temp.path().join("fork-at-unknown");
    fixture
        .furrow()
        .args(["fork", "ghost", "--at", "deadbeef", "--destination"])
        .arg(&dest)
        .assert()
        .failure()
        .stderr(predicates::str::contains("was not found"));
    assert!(!dest.exists());
}

fn git(repo: &Path, args: &[&str]) {
    let status = std::process::Command::new("git")
        .arg("-C")
        .arg(repo)
        .args(args)
        .status()
        .unwrap();
    assert!(status.success());
}

/// Pair `fixture` and a freshly watched `peer` clone to a shared directory
/// remote, returning the peer/data/remote paths. The source is pushed so the
/// peer can bootstrap from it.
fn prepare_streaming_peer(fixture: &Fixture, tag: &str) -> (PathBuf, PathBuf, PathBuf) {
    let parent = fixture.repo.parent().unwrap();
    let peer = parent.join(format!("{tag}-peer"));
    let peer_data = parent.join(format!("{tag}-peer-data"));
    let remote = parent.join(format!("{tag}-remote"));
    git(
        parent,
        &[
            "clone",
            fixture.repo.to_str().unwrap(),
            &format!("{tag}-peer"),
        ],
    );
    fixture.watch();
    furrow_at(&peer, &peer_data)
        .args(["watch", "--no-daemon"])
        .assert()
        .success();

    let pair_output = fixture
        .furrow()
        .args([
            "--json",
            "pair",
            remote.to_str().unwrap(),
            "--name",
            "streamed-workspace",
        ])
        .assert()
        .success()
        .get_output()
        .stdout
        .clone();
    let pair: Value = serde_json::from_slice(&pair_output).unwrap();
    let key = pair["key_hex"].as_str().unwrap();
    furrow_at(&peer, &peer_data)
        .args([
            "pair",
            remote.to_str().unwrap(),
            "--name",
            "streamed-workspace",
            "--key",
            key,
        ])
        .assert()
        .success();
    fixture.furrow().args(["sync", "--push"]).assert().success();
    (peer, peer_data, remote)
}

fn assert_workspace_matches_source(fixture: &Fixture, peer: &Path, context: &str) {
    for relative in [".env", "notes.txt", "app.txt", "cache/dependency.bin"] {
        assert_eq!(
            fs::read(peer.join(relative)).unwrap(),
            fs::read(fixture.repo.join(relative)).unwrap(),
            "{context}: byte mismatch for {relative}"
        );
    }
    assert_eq!(
        fs::read_link(peer.join("app-link")).unwrap(),
        fs::read_link(fixture.repo.join("app-link")).unwrap(),
        "{context}: symlink mismatch"
    );
    assert_eq!(
        fs::metadata(peer.join("app.txt")).unwrap().mode() & 0o777,
        fs::metadata(fixture.repo.join("app.txt")).unwrap().mode() & 0o777,
        "{context}: mode mismatch"
    );
    assert_eq!(
        xattr::get(peer.join("app.txt"), "user.furrow-test")
            .unwrap()
            .as_deref(),
        Some(&b"preserved"[..]),
        "{context}: xattr mismatch"
    );
}

#[test]
fn streamed_clone_is_byte_identical_and_reports_materialized_progress() {
    let fixture = Fixture::new();
    let (peer, peer_data, _remote) = prepare_streaming_peer(&fixture, "streamed");

    let bootstrap_output = furrow_at(&peer, &peer_data)
        .args(["--json", "sync", "--pull", "--bootstrap"])
        .assert()
        .success()
        .get_output()
        .stdout
        .clone();
    let bootstrap: Value = serde_json::from_slice(&bootstrap_output).unwrap();
    assert_eq!(bootstrap["disposition"], "bootstrapped");

    // Streaming pipelines fetch and checkout: files are written while blobs are
    // still arriving, so the materialized-files counter must advance and the
    // transfer must actually have moved objects.
    assert!(
        bootstrap["materialized_files"].as_u64().unwrap() > 0,
        "streamed pull reported no materialized files: {bootstrap}"
    );
    assert!(bootstrap["fetched_objects"].as_u64().unwrap() > 0);

    // The streamed result is byte-identical to the source workspace, including
    // gitignored payloads, the large binary, executable mode, xattrs, and the
    // symlink — exactly what the fully-resolved checkout produced before.
    assert_workspace_matches_source(&fixture, &peer, "streamed clone");
    assert_eq!(
        fs::read(peer.join("cache/dependency.bin")).unwrap(),
        vec![7_u8; 180_000]
    );
}

#[test]
fn interrupted_streamed_pull_recovers_and_completes_byte_identical() {
    let fixture = Fixture::new();
    let (peer, peer_data, _remote) = prepare_streaming_peer(&fixture, "interrupted");

    // Kill the pull mid-stream: the failpoint exits the process immediately
    // after the first file is materialized, long before the whole graph lands.
    furrow_at(&peer, &peer_data)
        .env("FURROW_FAILPOINT", "pull_after_first_file")
        .args(["sync", "--pull", "--bootstrap"])
        .assert()
        .code(87);

    // The large gitignored payload never arrived because the transfer aborted,
    // so materialization was genuinely partial rather than all-or-nothing.
    assert!(
        !peer.join("cache/dependency.bin").exists(),
        "the interrupted pull materialized the whole tree, not a partial stream"
    );

    // Opening the workspace detects the durable restore intent and rolls the
    // partially materialized delta back to the pre-pull snapshot.
    furrow_at(&peer, &peer_data)
        .arg("status")
        .assert()
        .success()
        .stderr(predicates::str::contains("recovering interrupted rewind"));
    assert!(
        !peer.join(".env").exists(),
        "the partially streamed delta was not rolled back on recovery"
    );

    // Re-pulling after recovery completes cleanly and reproduces the exact
    // source workspace, reusing whatever the aborted attempt already fetched.
    let bootstrap_output = furrow_at(&peer, &peer_data)
        .args(["--json", "sync", "--pull", "--bootstrap"])
        .assert()
        .success()
        .get_output()
        .stdout
        .clone();
    let bootstrap: Value = serde_json::from_slice(&bootstrap_output).unwrap();
    assert_eq!(bootstrap["disposition"], "bootstrapped");
    assert_workspace_matches_source(&fixture, &peer, "recovered clone");
    assert_eq!(
        fs::read(peer.join("cache/dependency.bin")).unwrap(),
        vec![7_u8; 180_000]
    );
}
