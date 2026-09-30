//! The operations a client can ask for. A verb reads its own arguments and
//! answers a JSON value; the daemon knows verbs only through [`Verb`].

use crate::handle::Handle;
use anyhow::Context;
use furrow::exchange::keys::parse_secret;
use furrow::exchange::ops;
use furrow::model::id_hex;
use furrow::sealer::Keys;
use serde::Deserialize;
use serde_json::{json, Value};
use std::path::PathBuf;

pub trait Verb: Sync {
    fn name(&self) -> &'static str;
    fn run(&self, handle: &mut Handle, args: Value) -> anyhow::Result<Value>;
}

/// Every verb that works on a store.
pub fn lookup(name: &str) -> Option<&'static dyn Verb> {
    const VERBS: [&dyn Verb; 9] = [
        &Seal,
        &Snap,
        &Restore,
        &Log,
        &Export,
        &Published,
        &Want,
        &Import,
        &Materialize,
    ];
    VERBS.into_iter().find(|verb| verb.name() == name)
}

fn args<T: for<'de> Deserialize<'de>>(value: Value) -> anyhow::Result<T> {
    serde_json::from_value(value).context("bad arguments")
}

/// The label a turn-end seal carries; the command line builds the same one.
pub fn hook_label(event: &str, agent: &str, turn: Option<&str>, tool: Option<&str>) -> String {
    let mut label = format!("hook {event} agent={agent}");
    for (key, value) in [("turn", turn), ("tool", tool)] {
        if let Some(value) = value {
            label.push_str(&format!(" {key}={value}"));
        }
    }
    label
}

struct Seal;

#[derive(Deserialize)]
struct SealArgs {
    #[serde(default = "default_agent")]
    agent: String,
    #[serde(default)]
    turn: Option<String>,
    /// Null walks the tree; a list visits only those paths.
    #[serde(default)]
    changed: Option<Vec<PathBuf>>,
}

fn default_agent() -> String {
    "agent".to_owned()
}

impl Verb for Seal {
    fn name(&self) -> &'static str {
        "seal"
    }

    fn run(&self, handle: &mut Handle, value: Value) -> anyhow::Result<Value> {
        let a: SealArgs = args(value)?;
        let label = hook_label("turn-end", &a.agent, a.turn.as_deref(), None);
        let id = handle.seal(label.clone(), a.changed)?;
        Ok(json!({"event": "turn-end", "label": label, "snapshot": id_hex(&id)}))
    }
}

struct Snap;

#[derive(Deserialize)]
struct SnapArgs {
    #[serde(default)]
    message: Option<String>,
}

impl Verb for Snap {
    fn name(&self) -> &'static str {
        "snap"
    }

    fn run(&self, handle: &mut Handle, value: Value) -> anyhow::Result<Value> {
        let a: SnapArgs = args(value)?;
        let manual = furrow::model::SnapshotTrigger::Manual;
        let id = handle.repo()?.snapshot(a.message, manual)?;
        Ok(json!({"snapshot": id_hex(&id)}))
    }
}

struct Restore;

#[derive(Deserialize)]
struct RestoreArgs {
    snapshot: String,
    #[serde(default)]
    paths: Vec<PathBuf>,
    #[serde(default)]
    sqlite_consistent: bool,
}

impl Verb for Restore {
    fn name(&self) -> &'static str {
        "restore"
    }

    fn run(&self, handle: &mut Handle, value: Value) -> anyhow::Result<Value> {
        let a: RestoreArgs = args(value)?;
        let repo = handle.repo()?;
        let target = repo.resolve_snapshot(&a.snapshot)?;
        let plan = repo.plan_rewind(&target, &a.paths)?;
        if plan.changes.is_empty() {
            return Ok(json!({"restored": id_hex(&target), "changes": 0}));
        }
        let (pre, applied) = repo
            .rewind(&target, &a.paths, a.sqlite_consistent)
            .context("rewind failed")?;
        Ok(json!({
            "restored": applied.target,
            "pre_rewind_snapshot": id_hex(&pre),
            "changes": applied.changes.len(),
        }))
    }
}

struct Log;

#[derive(Deserialize)]
struct LogArgs {
    #[serde(default = "default_limit")]
    limit: usize,
}

fn default_limit() -> usize {
    20
}

impl Verb for Log {
    fn name(&self) -> &'static str {
        "log"
    }

    fn run(&self, handle: &mut Handle, value: Value) -> anyhow::Result<Value> {
        let a: LogArgs = args(value)?;
        Ok(serde_json::to_value(handle.repo()?.timeline(a.limit)?)?)
    }
}

/// Arguments that carry the identity's secrets. A failure to read them says
/// only that they are bad: serde's own message can quote the offending value.
fn secret_args<T: for<'de> Deserialize<'de>>(value: Value) -> anyhow::Result<T> {
    serde_json::from_value(value).map_err(|_| anyhow::anyhow!("bad arguments"))
}

/// The two secrets as they arrive over the socket. Never logged, never written.
#[derive(Deserialize)]
struct KeyArgs {
    #[serde(default)]
    cell_key: Option<String>,
    #[serde(default)]
    dedup: Option<String>,
}

impl KeyArgs {
    fn keys(self) -> anyhow::Result<Keys> {
        Ok(Keys {
            cell_key: parse_secret("cell_key", self.cell_key)?,
            dedup: parse_secret("dedup", self.dedup)?,
        })
    }
}

struct Export;

#[derive(Deserialize)]
struct ExportArgs {
    head: String,
    outbox: PathBuf,
    ledger: String,
    #[serde(default = "default_max_frame")]
    max_frame: usize,
    #[serde(flatten)]
    keys: KeyArgs,
}

fn default_max_frame() -> usize {
    furrow::exchange::export::DEFAULT_MAX_FRAME
}

impl Verb for Export {
    fn name(&self) -> &'static str {
        "export"
    }

    fn run(&self, handle: &mut Handle, value: Value) -> anyhow::Result<Value> {
        let a: ExportArgs = secret_args(value)?;
        let data_dir = &handle.target().data_dir;
        ops::export(
            data_dir,
            &a.keys.keys()?,
            &a.head,
            &a.ledger,
            &a.outbox,
            a.max_frame,
        )
    }
}

struct Published;

#[derive(Deserialize)]
struct PublishedArgs {
    ledger: String,
    frames: Vec<PathBuf>,
}

impl Verb for Published {
    fn name(&self) -> &'static str {
        "published"
    }

    fn run(&self, handle: &mut Handle, value: Value) -> anyhow::Result<Value> {
        let a: PublishedArgs = args(value)?;
        ops::record_published(&handle.target().data_dir, &a.ledger, &a.frames)
    }
}

struct Want;

#[derive(Deserialize)]
struct WantArgs {
    head: String,
    #[serde(flatten)]
    keys: KeyArgs,
}

impl Verb for Want {
    fn name(&self) -> &'static str {
        "want"
    }

    fn run(&self, handle: &mut Handle, value: Value) -> anyhow::Result<Value> {
        let a: WantArgs = secret_args(value)?;
        ops::wanted(&handle.target().data_dir, &a.keys.keys()?, &a.head)
    }
}

struct Import;

#[derive(Deserialize)]
struct ImportArgs {
    head: String,
    inbox: PathBuf,
    ledger: String,
    /// Priming mode: unwanted inbox files are deleted, not an error.
    #[serde(default)]
    primed: bool,
    #[serde(flatten)]
    keys: KeyArgs,
}

impl Verb for Import {
    fn name(&self) -> &'static str {
        "import"
    }

    fn run(&self, handle: &mut Handle, value: Value) -> anyhow::Result<Value> {
        let a: ImportArgs = secret_args(value)?;
        ops::import(
            &handle.target().data_dir,
            &a.keys.keys()?,
            &a.head,
            &a.ledger,
            &a.inbox,
            a.primed,
        )
    }
}

struct Materialize;

#[derive(Deserialize)]
struct MaterializeArgs {
    head: String,
}

impl Verb for Materialize {
    fn name(&self) -> &'static str {
        "materialize"
    }

    /// Materializing moves the workspace head on disk, so the cached
    /// repository is dropped and the next verb reopens the store.
    fn run(&self, handle: &mut Handle, value: Value) -> anyhow::Result<Value> {
        let a: MaterializeArgs = args(value)?;
        handle.release();
        let target = handle.target();
        ops::restore_head(
            &target.data_dir,
            &target.tree,
            target.cell_dir.clone(),
            &a.head,
        )
    }
}
