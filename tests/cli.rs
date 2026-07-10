use assert_cmd::Command;
use serde_json::Value;
use std::fs;
use std::io::Write;
use std::os::unix::fs::{symlink, PermissionsExt};
use std::path::{Path, PathBuf};
use std::time::{Duration, Instant};
use tempfile::TempDir;

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
        xattr::set(repo.join("app.txt"), "user.agit-test", b"preserved").unwrap();
        Self {
            _temp: temp,
            repo,
            data,
        }
    }

    fn agit(&self) -> Command {
        let mut command = Command::cargo_bin("agit").unwrap();
        command
            .env("AGIT_DATA_DIR", &self.data)
            .env("AGIT_NO_DAEMON", "1")
            .arg("--repo")
            .arg(&self.repo);
        command
    }

    fn watch(&self) -> String {
        let output = self
            .agit()
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

fn agit_at(repo: &Path, data: &Path) -> Command {
    let mut command = Command::cargo_bin("agit").unwrap();
    command
        .env("AGIT_DATA_DIR", data)
        .env("AGIT_NO_DAEMON", "1")
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

#[test]
fn path_rewind_restores_ignored_secret_without_touching_new_work() {
    let fixture = Fixture::new();
    let snapshot = fixture.watch();
    fs::write(fixture.repo.join(".env"), b"TOKEN=destroyed\n").unwrap();
    fs::write(fixture.repo.join("later.txt"), b"keep this\n").unwrap();

    let preview = fixture
        .agit()
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
        .agit()
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
        .agit()
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
    fs::write(fixture.repo.join(".agitpolicy"), b"exclude cache\n").unwrap();
    let excluded_snapshot = fixture.watch();

    fs::write(
        fixture.repo.join("cache/dependency.bin"),
        b"new excluded cache state\n",
    )
    .unwrap();
    fs::write(fixture.repo.join("app.txt"), b"damaged app\n").unwrap();
    fs::remove_file(fixture.repo.join(".agitpolicy")).unwrap();
    fixture
        .agit()
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
        fs::read(fixture.repo.join(".agitpolicy")).unwrap(),
        b"exclude cache\n"
    );
    let fidelity = fixture
        .agit()
        .args(["--json", "status", "--fidelity"])
        .assert()
        .success()
        .get_output()
        .stdout
        .clone();
    let fidelity: Value = serde_json::from_slice(&fidelity).unwrap();
    assert_eq!(fidelity["fidelity"]["excluded_subtrees"][0], "cache");

    fs::write(
        fixture.repo.join(".agitpolicy"),
        b"# cache is protected again\n",
    )
    .unwrap();
    let included = fixture
        .agit()
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
        .agit()
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
    fs::write(fixture.repo.join(".agitpolicy"), b"exclude cache\n").unwrap();
    let before = fixture
        .agit()
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
    assert!(!fixture.repo.join(".agit").exists());

    fixture.watch();
    let after = fixture
        .agit()
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
        .agit()
        .args(["--json", "hook", "install"])
        .assert()
        .success()
        .get_output()
        .stdout
        .clone();
    let installed: Value = serde_json::from_slice(&installed).unwrap();
    assert_eq!(installed["hooks"].as_array().unwrap().len(), 3);
    let pre_turn = fixture.repo.join(".agit/hooks/pre-turn");
    assert!(fs::metadata(&pre_turn).unwrap().permissions().mode() & 0o111 != 0);

    let outside = fixture._temp.path().join("outside");
    fs::create_dir(&outside).unwrap();
    let output = std::process::Command::new(&pre_turn)
        .current_dir(&outside)
        .env("AGIT_DATA_DIR", &fixture.data)
        .env("AGIT_NO_DAEMON", "1")
        .env("AGIT_BIN", env!("CARGO_BIN_EXE_agit"))
        .env("AGIT_AGENT_ID", "alpha")
        .env("AGIT_TURN_ID", "7")
        .output()
        .unwrap();
    assert!(
        output.status.success(),
        "{}",
        String::from_utf8_lossy(&output.stderr)
    );
    fixture
        .agit()
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
        .agit()
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
        .agit()
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
    assert!(stderr.contains(&format!("Undo with: agit rewind {before}")));
    assert!(!fixture.repo.join(".env").exists());
    assert_eq!(
        fs::read(fixture.repo.join("app.txt")).unwrap(),
        b"damaged\n"
    );

    fixture
        .agit()
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
        .agit()
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
        .agit()
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
    assert!(!fixture.repo.join(".agit").exists());

    let applied = fixture
        .agit()
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
        .agit()
        .args(["rewind", before, "--yes"])
        .assert()
        .success();
    assert_eq!(fs::read(dependency).unwrap(), vec![0x5a; 512 * 1024]);

    fixture
        .agit()
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
        .agit()
        .args(["snap", "-m", "still good"])
        .assert()
        .success();

    fs::write(fixture.repo.join("app.txt"), b"regression\n").unwrap();
    let first_bad = fixture
        .agit()
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
        .agit()
        .args(["snap", "-m", "later bad state"])
        .assert()
        .success();

    let outcome = fixture
        .agit()
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
            .starts_with(".agit-bisect-")));
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
        .agit()
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
        xattr::get(fixture.repo.join("app.txt"), "user.agit-test")
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
        .agit()
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
        .agit()
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

    fixture.agit().arg("status").assert().success();
    assert_eq!(fs::metadata(&pack).unwrap().len(), valid_len);
    let output = fixture
        .agit()
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
        .agit()
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
        .agit()
        .args(["rewind", &snapshot, "--paths", "nested/value.txt", "--yes"])
        .assert()
        .failure()
        .stderr(predicates::str::contains("symlink parent"));
    assert_eq!(fs::read(outside.join("value.txt")).unwrap(), b"outside\n");
}

#[test]
fn watch_refuses_non_git_directories() {
    let temp = tempfile::tempdir().unwrap();
    Command::cargo_bin("agit")
        .unwrap()
        .env("AGIT_DATA_DIR", temp.path().join("data"))
        .env("AGIT_NO_DAEMON", "1")
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
        .agit()
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
        .agit()
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
        .agit()
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
        fs::read_to_string(destination.join(".agit/workspace-id")).unwrap(),
        fs::read_to_string(fixture.repo.join(".agit/workspace-id")).unwrap()
    );

    fs::write(destination.join("app.txt"), b"fork-only change\n").unwrap();
    fs::write(destination.join("fork-result.txt"), b"new result\n").unwrap();
    fs::remove_file(destination.join("notes.txt")).unwrap();
    assert_eq!(
        fs::read(fixture.repo.join("app.txt")).unwrap(),
        b"tracked original\n"
    );

    let diff_output = fixture
        .agit()
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
        .agit()
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
        .agit()
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
        .agit()
        .args(["fork-rm", "agent-one"])
        .assert()
        .success();
    assert!(!destination.exists());
    let remaining = fixture
        .agit()
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
        .agit()
        .env("AGIT_DISABLE_NAMESPACES", "1")
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
        .agit()
        .env("AGIT_DISABLE_NAMESPACES", "1")
        .args([
            "--json",
            "exec",
            "-n",
            "2",
            "--",
            "sh",
            "-c",
            "printf '%s|%s|%s' \"$AGIT_UNIVERSE_INDEX\" \"$AGIT_WORKDIR\" \"$PORT\" > universe.txt",
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
        assert_eq!(universe["fork_id"], universe["fork"]);
        assert_ne!(universe["head_snapshot"], universe["base_snapshot"]);
        let path = PathBuf::from(universe["path"].as_str().unwrap());
        let contents = fs::read_to_string(path.join("universe.txt")).unwrap();
        let expected_prefix = format!("{}|{}|{}", offset + 1, path.display(), 3000 + offset);
        assert_eq!(contents, expected_prefix);
    }
    assert!(!fixture.repo.join("universe.txt").exists());

    let forks = fixture.agit().args(["--json", "forks"]).output().unwrap();
    let forks: Value = serde_json::from_slice(&forks.stdout).unwrap();
    assert_eq!(forks.as_array().unwrap().len(), 2);
}

#[test]
fn exec_uses_the_disclosed_workdir_and_preserves_failed_results() {
    let fixture = Fixture::new();
    fixture.watch();
    let plan_output = fixture
        .agit()
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
        .agit()
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
fn verified_merge_converges_independent_source_and_fork_changes() {
    let fixture = Fixture::new();
    fixture.watch();
    let fork = fixture._temp.path().join("merge-agent");
    fixture
        .agit()
        .args(["fork", "merge-agent", "--destination"])
        .arg(&fork)
        .assert()
        .success();

    fs::write(fixture.repo.join("app.txt"), b"source-only work\n").unwrap();
    fs::write(fork.join("agent-result.txt"), b"fork-only work\n").unwrap();
    fixture
        .agit()
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
        .agit()
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
        .agit()
        .args(["fork", "claim-alpha", "--destination"])
        .arg(&alpha)
        .assert()
        .success();
    fixture
        .agit()
        .args(["fork", "claim-beta", "--destination"])
        .arg(&beta)
        .assert()
        .success();

    let alpha_claim = agit_at(&alpha, &fixture.data)
        .args(["--json", "claim", "src/auth/**", "--owner", "alpha-agent"])
        .assert()
        .success()
        .get_output()
        .stdout
        .clone();
    let alpha_claim: Value = serde_json::from_slice(&alpha_claim).unwrap();
    agit_at(&beta, &fixture.data)
        .args(["claim", "src/auth/login.rs", "--owner", "beta-agent"])
        .assert()
        .failure()
        .stderr(predicates::str::contains("held by alpha-agent"));
    agit_at(&beta, &fixture.data)
        .args(["claim", "src/payments/**", "--owner", "beta-agent"])
        .assert()
        .success();

    let active = fixture
        .agit()
        .args(["--json", "claims"])
        .assert()
        .success()
        .get_output()
        .stdout
        .clone();
    let active: Value = serde_json::from_slice(&active).unwrap();
    assert_eq!(active.as_array().unwrap().len(), 2);
    let timeline = agit_at(&alpha, &fixture.data)
        .args(["--json", "timeline"])
        .assert()
        .success()
        .get_output()
        .stdout
        .clone();
    let timeline: Value = serde_json::from_slice(&timeline).unwrap();
    assert_eq!(timeline[0]["trigger"], "claim");

    agit_at(&alpha, &fixture.data)
        .args([
            "release",
            alpha_claim["claim"]["id"].as_str().unwrap(),
            "--owner",
            "alpha-agent",
        ])
        .assert()
        .success();
    agit_at(&beta, &fixture.data)
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
        .agit()
        .args(["fork", "coord-alpha", "--destination"])
        .arg(&alpha)
        .assert()
        .success();
    fixture
        .agit()
        .args(["fork", "coord-beta", "--destination"])
        .arg(&beta)
        .assert()
        .success();

    agit_at(&alpha, &fixture.data)
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
            fs::read(root.join(".agit/coord/tasks/current.md")).unwrap(),
            b"alpha is working"
        );
    }

    let offline = fixture._temp.path().join("coord-alpha-offline");
    fs::rename(&alpha, &offline).unwrap();
    agit_at(&beta, &fixture.data)
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
        fs::read(offline.join(".agit/coord/tasks/current.md")).unwrap(),
        b"alpha is working"
    );
    fs::rename(&offline, &alpha).unwrap();
    let recovered = agit_at(&alpha, &fixture.data)
        .args(["coord", "read", "tasks/current.md"])
        .assert()
        .success()
        .get_output()
        .stdout
        .clone();
    assert_eq!(recovered, b"beta continued");

    fs::rename(&alpha, &offline).unwrap();
    agit_at(&beta, &fixture.data)
        .args([
            "coord",
            "remove",
            "tasks/current.md",
            "--owner",
            "beta-agent",
        ])
        .assert()
        .success();
    assert!(offline.join(".agit/coord/tasks/current.md").exists());
    fs::rename(&offline, &alpha).unwrap();
    let listed = agit_at(&alpha, &fixture.data)
        .args(["--json", "coord", "list"])
        .assert()
        .success()
        .get_output()
        .stdout
        .clone();
    let listed: Value = serde_json::from_slice(&listed).unwrap();
    assert!(listed.as_array().unwrap().is_empty());
    assert!(!alpha.join(".agit/coord/tasks/current.md").exists());

    let timeline = agit_at(&beta, &fixture.data)
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
        .agit()
        .args(["fork", "observer-agent", "--destination"])
        .arg(&worker)
        .assert()
        .success();

    let timeline = agit_at(&worker, &fixture.data)
        .args(["--json", "timeline"])
        .assert()
        .success()
        .get_output()
        .stdout
        .clone();
    let timeline: Value = serde_json::from_slice(&timeline).unwrap();
    let cursor = timeline[0]["id"].as_str().unwrap();

    fs::write(worker.join("agent-result.txt"), b"completed\n").unwrap();
    let sealed = agit_at(&worker, &fixture.data)
        .args(["--json", "snap", "-m", "agent completed task"])
        .assert()
        .success()
        .get_output()
        .stdout
        .clone();
    let sealed: Value = serde_json::from_slice(&sealed).unwrap();

    let updates = fixture
        .agit()
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
        .agit()
        .args(["fork", "conflict-agent", "--destination"])
        .arg(&conflict_fork)
        .assert()
        .success();
    fs::write(fixture.repo.join("app.txt"), b"source version\n").unwrap();
    fs::write(conflict_fork.join("app.txt"), b"fork version\n").unwrap();

    fixture
        .agit()
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
        .agit()
        .args(["fork", "check-agent", "--destination"])
        .arg(&check_fork)
        .assert()
        .success();
    fs::write(check_fork.join("check-only.txt"), b"candidate\n").unwrap();
    fixture
        .agit()
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
        .agit()
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
        .agit()
        .env("AGIT_FAILPOINT", "rewind_after_first_change")
        .args(["rewind", &snapshot, "--yes"])
        .assert()
        .code(86);

    // Opening the repository detects the durable intent and restores the
    // automatic pre-rewind snapshot before serving status.
    fixture
        .agit()
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

    let mut child = std::process::Command::new(env!("CARGO_BIN_EXE_agit"))
        .env("AGIT_DATA_DIR", &fixture.data)
        .env("AGIT_NO_DAEMON", "1")
        .env("AGIT_TEST_REWIND_PAUSE_AFTER_APPLY_MS", "750")
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
        .agit()
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

    let child = std::process::Command::new(env!("CARGO_BIN_EXE_agit"))
        .env("AGIT_DATA_DIR", &fixture.data)
        .env("AGIT_NO_DAEMON", "1")
        .env("AGIT_TEST_REWIND_PAUSE_BEFORE_PRECONDITION_MS", "750")
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
            .agit()
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
    let mut child = std::process::Command::new(env!("CARGO_BIN_EXE_agit"))
        .env("AGIT_DATA_DIR", &fixture.data)
        .arg("--repo")
        .arg(&fixture.repo)
        .args(["watch", "--foreground", "--debounce-ms", "100"])
        .stdout(std::process::Stdio::null())
        .stderr(std::process::Stdio::null())
        .spawn()
        .unwrap();

    let deadline = Instant::now() + Duration::from_secs(10);
    while !fixture.repo.join(".agit/workspace-id").exists() {
        assert!(Instant::now() < deadline, "watcher did not attach in time");
        std::thread::sleep(Duration::from_millis(25));
    }
    // The initial snapshot precedes watcher installation. Give the native
    // backend time to enter its event loop before creating the test event.
    // Avoid polling the growing pack during this one-time ingest.
    std::thread::sleep(Duration::from_millis(1500));
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
            .agit()
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
        .agit()
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
    let mut child = std::process::Command::new(env!("CARGO_BIN_EXE_agit"))
        .env("AGIT_DATA_DIR", &fixture.data)
        .arg("--repo")
        .arg(&fixture.repo)
        .args(["watch", "--foreground", "--debounce-ms", "75"])
        .stdout(std::process::Stdio::null())
        .stderr(std::process::Stdio::null())
        .spawn()
        .unwrap();
    let deadline = Instant::now() + Duration::from_secs(10);
    while !fixture.repo.join(".agit/workspace-id").exists() {
        assert!(Instant::now() < deadline, "watcher did not attach in time");
        std::thread::sleep(Duration::from_millis(25));
    }
    std::thread::sleep(Duration::from_millis(1200));
    let before = fixture
        .agit()
        .args(["--json", "timeline", "--limit", "1"])
        .output()
        .unwrap();
    let before: Value = serde_json::from_slice(&before.stdout).unwrap();
    let before = before[0]["id"].as_str().unwrap().to_owned();

    fs::write(fixture.repo.join(".agitpolicy"), b"exclude cache\n").unwrap();
    let policy_head = loop {
        assert!(Instant::now() < deadline, "policy change was not sealed");
        let output = fixture
            .agit()
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
        .agit()
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
    std::process::Command::new(env!("CARGO_BIN_EXE_agit"))
        .env("AGIT_DATA_DIR", &fixture.data)
        .arg("--repo")
        .arg(&fixture.repo)
        .arg("watch")
        .status()
        .unwrap()
        .success()
        .then_some(())
        .expect("watch failed");

    let output = fixture.agit().args(["--json", "status"]).output().unwrap();
    let status: Value = serde_json::from_slice(&output.stdout).unwrap();
    assert_eq!(status["watcher_running"], true);

    fixture.agit().arg("forget").assert().success();
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
    assert!(!fixture.repo.join(".agit/workspace-id").exists());
    assert!(!fixture.repo.join(".env").exists());

    fixture
        .agit()
        .args(["rewind", &snapshot, "--yes"])
        .assert()
        .success();
    assert_eq!(
        fs::read(fixture.repo.join(".env")).unwrap(),
        b"TOKEN=original\n"
    );
    assert!(fixture.repo.join(".agit/workspace-id").exists());
}

#[test]
fn plain_forget_stays_detached_instead_of_being_rediscovered() {
    let fixture = Fixture::new();
    fixture.watch();
    let workspace_id = fs::read_to_string(fixture.repo.join(".agit/workspace-id")).unwrap();

    fixture.agit().arg("forget").assert().success();
    assert!(!fixture.repo.join(".agit/workspace-id").exists());
    fixture
        .agit()
        .arg("status")
        .assert()
        .failure()
        .stderr(predicates::str::contains("not watched"));
    let output = fixture
        .agit()
        .args(["--json", "gc", "--dry-run"])
        .assert()
        .success()
        .get_output()
        .stdout
        .clone();
    let report: Value = serde_json::from_slice(&output).unwrap();
    assert!(report["roots"].as_u64().unwrap() > 0);

    fixture
        .agit()
        .args(["watch", "--no-daemon"])
        .assert()
        .success();
    let new_workspace_id = fs::read_to_string(fixture.repo.join(".agit/workspace-id")).unwrap();
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
    let second_agit = || {
        let mut command = Command::cargo_bin("agit").unwrap();
        command
            .env("AGIT_DATA_DIR", &fixture.data)
            .env("AGIT_NO_DAEMON", "1")
            .arg("--repo")
            .arg(&second);
        command
    };
    let output = second_agit()
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
        .agit()
        .args(["forget", "--purge"])
        .assert()
        .success();
    let preview_output = fixture
        .agit()
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
        .agit()
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
    second_agit()
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
    agit_at(&peer, &peer_data)
        .args(["watch", "--no-daemon"])
        .assert()
        .success();

    let pair_output = fixture
        .agit()
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
    agit_at(&peer, &peer_data)
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

    fixture.agit().args(["sync", "--push"]).assert().success();
    let bootstrap_output = agit_at(&peer, &peer_data)
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

    fs::write(fixture.repo.join("app.txt"), b"remote delta\n").unwrap();
    let second_push = fixture
        .agit()
        .args(["--json", "sync", "--push"])
        .assert()
        .success()
        .get_output()
        .stdout
        .clone();
    let second_push: Value = serde_json::from_slice(&second_push).unwrap();
    assert!(second_push["reused_objects"].as_u64().unwrap() > 0);
    assert!(second_push["uploaded_objects"].as_u64().unwrap() < 10);
    let pull_output = agit_at(&peer, &peer_data)
        .args(["--json", "sync", "--pull"])
        .assert()
        .success()
        .get_output()
        .stdout
        .clone();
    let pull: Value = serde_json::from_slice(&pull_output).unwrap();
    assert_eq!(pull["disposition"], "fast_forwarded");
    assert_eq!(fs::read(peer.join("app.txt")).unwrap(), b"remote delta\n");

    fs::write(peer.join("notes.txt"), b"offline peer work\n").unwrap();
    fs::write(fixture.repo.join("app.txt"), b"new remote work\n").unwrap();
    fixture.agit().args(["sync", "--push"]).assert().success();
    let divergence_output = agit_at(&peer, &peer_data)
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
    agit_at(&peer, &peer_data).arg("gc").assert().success();
    agit_at(&peer, &peer_data)
        .args([
            "rewind",
            divergence["remote_snapshot"].as_str().unwrap(),
            "--dry-run",
        ])
        .assert()
        .success();
    agit_at(&peer, &peer_data)
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
    fixture.agit().args(["sync", "--push"]).assert().success();

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
        b"#!/bin/sh\nwhile [ \"$1\" != \"--\" ]; do shift; done\nshift\nshift\nshift\nexec \"$AGIT_TEST_BIN\" \"$@\"\n",
    )
    .unwrap();
    let mut mode = fs::metadata(&wrapper).unwrap().permissions();
    mode.set_mode(0o755);
    fs::set_permissions(&wrapper, mode).unwrap();

    fixture.watch();
    agit_at(&peer, &peer_data)
        .args(["watch", "--no-daemon"])
        .assert()
        .success();
    let ssh_agit = |repo: &Path, data: &Path| {
        let mut command = agit_at(repo, data);
        command
            .env("AGIT_SSH_COMMAND", &wrapper)
            .env("AGIT_REMOTE_DATA_DIR", &remote_data)
            .env("AGIT_TEST_BIN", env!("CARGO_BIN_EXE_agit"));
        command
    };

    let pair_output = ssh_agit(&fixture.repo, &fixture.data)
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
    ssh_agit(&peer, &peer_data)
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
    ssh_agit(&fixture.repo, &fixture.data)
        .args(["sync", "--push"])
        .assert()
        .success();
    let pull = ssh_agit(&peer, &peer_data)
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
    let push = ssh_agit(&fixture.repo, &fixture.data)
        .args(["--json", "sync", "--push"])
        .assert()
        .success()
        .get_output()
        .stdout
        .clone();
    let push: Value = serde_json::from_slice(&push).unwrap();
    assert!(push["uploaded_objects"].as_u64().unwrap() < 10);
    let pull = ssh_agit(&peer, &peer_data)
        .args(["--json", "sync", "--pull"])
        .assert()
        .success()
        .get_output()
        .stdout
        .clone();
    let pull: Value = serde_json::from_slice(&pull).unwrap();
    assert_eq!(pull["disposition"], "fast_forwarded");
    assert_eq!(fs::read(peer.join("app.txt")).unwrap(), b"ssh delta\n");
}

#[test]
fn mcp_stdio_negotiates_lifecycle_lists_tools_and_keeps_errors_in_protocol() {
    let fixture = Fixture::new();
    let snapshot = fixture.watch();
    fs::write(fixture.repo.join(".env"), b"TOKEN=changed\n").unwrap();

    let mut child = std::process::Command::new(env!("CARGO_BIN_EXE_agit"))
        .env("AGIT_DATA_DIR", &fixture.data)
        .env("AGIT_NO_DAEMON", "1")
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
                "clientInfo":{"name":"agit-test","version":"1"}
            }
        }),
        serde_json::json!({"jsonrpc":"2.0","method":"notifications/initialized"}),
        serde_json::json!({"jsonrpc":"2.0","id":3,"method":"tools/list"}),
        serde_json::json!({
            "jsonrpc":"2.0","id":"status","method":"tools/call",
            "params":{"name":"agit.status","arguments":{"fidelity":true}}
        }),
        serde_json::json!({
            "jsonrpc":"2.0","id":5,"method":"tools/call",
            "params":{"name":"agit.snapshot","arguments":{"message":"agent boundary"}}
        }),
        serde_json::json!({
            "jsonrpc":"2.0","id":6,"method":"tools/call",
            "params":{"name":"agit.rewind_apply","arguments":{"snapshot":snapshot}}
        }),
        serde_json::json!({
            "jsonrpc":"2.0","id":7,"method":"tools/call",
            "params":{"name":"agit.unknown","arguments":{}}
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
    assert_eq!(responses.len(), 7);
    assert_eq!(responses[0]["error"]["code"], -32002);
    assert_eq!(responses[1]["result"]["protocolVersion"], "2025-11-25");
    let tools = responses[2]["result"]["tools"].as_array().unwrap();
    assert!(tools.iter().any(|tool| tool["name"] == "agit.rewind_plan"));
    assert!(tools.iter().any(|tool| tool["name"] == "agit.fork"));
    assert!(tools.iter().any(|tool| tool["name"] == "agit.claim"));
    assert!(tools.iter().any(|tool| tool["name"] == "agit.coord_write"));
    assert!(tools.iter().any(|tool| tool["name"] == "agit.fork_updates"));
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
    assert_eq!(responses[6]["error"]["code"], -32602);
}

#[test]
fn snapshots_can_be_pinned_and_unpinned_idempotently() {
    let fixture = Fixture::new();
    let snapshot = fixture.watch();

    let pin = fixture
        .agit()
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
        .agit()
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
        .agit()
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
        .agit()
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
        .agit()
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
        .agit()
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

fn git(repo: &Path, args: &[&str]) {
    let status = std::process::Command::new("git")
        .arg("-C")
        .arg(repo)
        .args(args)
        .status()
        .unwrap();
    assert!(status.success());
}
