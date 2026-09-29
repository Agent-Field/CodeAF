//! The operations a client can ask for. A verb reads its own arguments and
//! answers a JSON value; the daemon knows verbs only through [`Verb`].

use crate::handle::Handle;
use anyhow::Context;
use furrow::model::id_hex;
use serde::Deserialize;
use serde_json::{json, Value};
use std::path::PathBuf;

pub trait Verb: Sync {
    fn name(&self) -> &'static str;
    fn run(&self, handle: &mut Handle, args: Value) -> anyhow::Result<Value>;
}

/// Every verb that works on a store.
pub fn lookup(name: &str) -> Option<&'static dyn Verb> {
    const VERBS: [&dyn Verb; 4] = [&Seal, &Snap, &Restore, &Log];
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
