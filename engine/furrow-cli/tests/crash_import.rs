//! An import killed inside its batch leaves the store open, the catalog with
//! no row of this import, every inbox file in place, and a second import that
//! finishes the take.
//!
//! The kill is a process abort at a crash point (`FURROW_FAULT`, compiled in
//! by the `fault-injection` feature), so this proves the ordering of the
//! steps: nothing catalogued is deleted, and nothing deleted is uncatalogued.
//! Pack bytes the rollback outruns are the store's rebuild-from-pack rule's
//! to tolerate, exactly as for a seal.

use assert_cmd::Command;
use furrow::exchange::export::ExportJob;
use furrow::exchange::frame;
use furrow::exchange::ledger::Ledger;
use furrow::exchange::open_store;
use furrow::model::{id_hex, SnapshotTrigger};
use furrow::repository::{FurrowRepository, SealOptions};
use furrow::sealer::Keys;
use std::fs;
use std::path::PathBuf;

const LEDGER: &str = "0123456789abcdef";

/// A source store holding a tree of several small files, exported into an
/// inbox of one file per remote id, as a fetcher would lay it down.
struct Folder {
    _temp: tempfile::TempDir,
    data: PathBuf,
    inbox: PathBuf,
    head: String,
    objects: usize,
}

fn folder() -> Folder {
    let temp = tempfile::tempdir().unwrap();
    let tree = temp.path().join("tree");
    fs::create_dir_all(&tree).unwrap();
    for name in 0..12 {
        fs::write(tree.join(format!("f{name}.txt")), format!("file {name}\n")).unwrap();
    }
    let (_, head) = FurrowRepository::attach_and_seal_in(
        &temp.path().join("data-src"),
        &tree,
        None,
        SnapshotTrigger::Manual,
        SealOptions::default(),
    )
    .unwrap();
    let keys = Keys {
        cell_key: [0x11; 32],
        dedup: [0x22; 32],
    };
    let outbox = temp.path().join("outbox");
    let store = open_store(&temp.path().join("data-src")).unwrap();
    let ledger = Ledger::named(&temp.path().join("data-src"), LEDGER).unwrap();
    let report = ExportJob {
        store: &store,
        keys: &keys,
        head,
        ledger: &ledger,
        outbox: &outbox,
        max_frame: 1 << 20,
    }
    .run()
    .unwrap();
    let objects = report.objects;
    let inbox = temp.path().join("inbox");
    let data = temp.path().join("data-dst");
    fs::create_dir_all(&inbox).unwrap();
    for path in report.frames.iter().map(|info| &info.path) {
        let bytes = fs::read(path).unwrap();
        let decoded = frame::decode(&bytes).unwrap().1;
        for object in decoded {
            fs::write(inbox.join(id_hex(&object.rid)), object.bytes).unwrap();
        }
    }
    Folder {
        _temp: temp,
        data,
        inbox,
        head: id_hex(&head),
        objects,
    }
}

impl Folder {
    fn import(&self, fault: Option<&str>) -> bool {
        let mut command = Command::cargo_bin("furrow").unwrap();
        command
            .env("FURROW_DATA_DIR", &self.data)
            .env("FURROW_NO_DAEMON", "1")
            .env("FURROW_CELL_KEY", hex::encode([0x11; 32]))
            .env("FURROW_DEDUP_SECRET", hex::encode([0x22; 32]))
            .args(["import", "--head", &self.head])
            .arg("--inbox")
            .arg(&self.inbox)
            .arg("--ledger")
            .arg(LEDGER)
            .arg("--json");
        if let Some(step) = fault {
            command.env("FURROW_FAULT", step);
        }
        let output = command.output().unwrap();
        eprintln!("{}", String::from_utf8_lossy(&output.stderr));
        output.status.success()
    }

    fn wanted(&self) -> usize {
        let output = self
            .furrow()
            .args(["want", "--head", &self.head])
            .output()
            .unwrap();
        assert!(output.status.success(), "the store must open after a crash");
        let doc: serde_json::Value = serde_json::from_slice(&output.stdout).unwrap();
        doc["want"].as_array().unwrap().len()
    }

    fn furrow(&self) -> Command {
        let mut command = Command::cargo_bin("furrow").unwrap();
        command
            .env("FURROW_DATA_DIR", &self.data)
            .env("FURROW_NO_DAEMON", "1")
            .env("FURROW_CELL_KEY", hex::encode([0x11; 32]))
            .env("FURROW_DEDUP_SECRET", hex::encode([0x22; 32]))
            .arg("--json");
        command
    }

    /// The primed import as JSON: it reports what it took and what it deleted.
    fn import_primed(&self) -> serde_json::Value {
        let output = self
            .furrow()
            .args(["import", "--head", &self.head])
            .arg("--inbox")
            .arg(&self.inbox)
            .arg("--ledger")
            .arg(LEDGER)
            .arg("--primed")
            .output()
            .unwrap();
        assert!(
            output.status.success(),
            "{}",
            String::from_utf8_lossy(&output.stderr)
        );
        serde_json::from_slice(&output.stdout).unwrap()
    }

    fn files(&self) -> usize {
        fs::read_dir(&self.inbox).unwrap().count()
    }
}

#[test]
fn an_import_killed_in_its_batch_deletes_nothing_and_finishes_again() {
    let folder = folder();

    assert!(
        !folder.import(Some("import.stored")),
        "the faulted import must abort"
    );
    assert_eq!(
        folder.files(),
        folder.objects,
        "no file was deleted before the batch committed"
    );

    // A second import, un-faulted, finishes the take: nothing was lost, and
    // nothing was half-stored.
    assert!(
        folder.import(None),
        "{:?}",
        folder.furrow().output().unwrap().stderr
    );
    assert_eq!(folder.wanted(), 0, "the take is complete");
    assert_eq!(folder.files(), 0, "every accepted file was deleted");
}

/// The un-faulted import of the same inbox works, so the crash point is the
/// only thing the first test's failure can be blamed on.
#[test]
fn an_untouched_import_of_the_same_inbox_succeeds() {
    let folder = folder();
    assert!(folder.import(None));
    assert_eq!(folder.wanted(), 0);
    assert_eq!(folder.files(), 0);
}

/// The priming take knows its inbox holds files this head never asked for: it
/// takes what it wants and deletes the rest, counting what it deleted. The
/// strict import of the same inbox is `unwanted_file_refused`'s to refuse.
#[test]
fn primed_import_deletes_unlisted_files_and_reports_them() {
    let folder = folder();
    // A copy under a rid nothing names: valid bytes, wanted by no pass.
    let sample = fs::read_dir(&folder.inbox)
        .unwrap()
        .next()
        .unwrap()
        .unwrap()
        .path();
    fs::copy(&sample, folder.inbox.join("0".repeat(64))).unwrap();

    let doc = folder.import_primed();
    assert_eq!(doc["imported"], folder.objects, "{}", doc);
    assert_eq!(doc["extras_deleted"], 1, "{}", doc);
    assert_eq!(folder.wanted(), 0, "the take is complete");
    assert_eq!(folder.files(), 0, "the extra went with the taken ones");
}
