//! The five verbs as the JSON answers a caller sees. The command line and the
//! daemon both call these, so the two front ends cannot drift apart: each only
//! gathers its arguments and hands them over.

use super::export::{published, ExportJob};
use super::fetch::{want, Import};
use super::ledger::Ledger;
use super::materialize::materialize;
use super::open_store;
use crate::model::{id_hex, parse_id};
use crate::sealer::{CellSealer, Keys};
use serde_json::{json, Value};
use std::path::{Path, PathBuf};

pub fn export(
    data_dir: &Path,
    keys: &Keys,
    head: &str,
    ledger: &str,
    outbox: &Path,
    max_frame: usize,
) -> anyhow::Result<Value> {
    let store = open_store(data_dir)?;
    let ledger = Ledger::named(data_dir, ledger)?;
    let job = ExportJob {
        store: &store,
        keys,
        head: parse_id(head)?,
        ledger: &ledger,
        outbox,
        max_frame,
    };
    Ok(serde_json::to_value(job.run()?)?)
}

pub fn record_published(
    data_dir: &Path,
    ledger: &str,
    frames: &[PathBuf],
) -> anyhow::Result<Value> {
    let recorded = published(&Ledger::named(data_dir, ledger)?, frames)?;
    Ok(json!({"recorded": recorded}))
}

pub fn wanted(data_dir: &Path, keys: &Keys, head: &str) -> anyhow::Result<Value> {
    let rids = want(
        &open_store(data_dir)?,
        &CellSealer::new(keys),
        parse_id(head)?,
    )?;
    Ok(json!({"want": rids.iter().map(id_hex).collect::<Vec<_>>()}))
}

pub fn import(data_dir: &Path, keys: &Keys, head: &str, inbox: &Path) -> anyhow::Result<Value> {
    let sealer = CellSealer::new(keys);
    let store = open_store(data_dir)?;
    let imported = Import {
        store: &store,
        sealer: &sealer,
        head: parse_id(head)?,
        inbox,
    }
    .run()?;
    Ok(json!({"imported": imported}))
}

pub fn restore_head(
    data_dir: &Path,
    tree: &Path,
    cell_dir: Option<PathBuf>,
    head: &str,
) -> anyhow::Result<Value> {
    let head = materialize(data_dir, tree, cell_dir, parse_id(head)?)?;
    Ok(json!({"snapshot": id_hex(&head)}))
}
