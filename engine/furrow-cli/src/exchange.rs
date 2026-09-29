//! The verbs that move a snapshot between stores as sealed frames. Secrets
//! come from the environment only; no flag carries one.

use clap::Subcommand;
use furrow::exchange::export::DEFAULT_MAX_FRAME;
use furrow::exchange::keys::keys_from_env;
use furrow::exchange::ops;
use furrow::repository::data_root;
use serde_json::Value;
use std::path::{Path, PathBuf};

#[derive(Subcommand)]
pub enum Exchange {
    /// Seal what the named store lacks into frames in an outbox.
    /// Reads FURROW_CELL_KEY and FURROW_DEDUP_SECRET.
    Export {
        /// Snapshot ID to send.
        #[arg(long)]
        head: String,
        #[arg(long)]
        outbox: PathBuf,
        /// Ledger name: 16 lowercase hex characters naming the destination store.
        #[arg(long)]
        ledger: String,
        #[arg(long, default_value_t = DEFAULT_MAX_FRAME)]
        max_frame: usize,
    },
    /// Record uploaded frames in the ledger and delete them.
    Published {
        #[arg(long)]
        ledger: String,
        #[arg(required = true)]
        frames: Vec<PathBuf>,
    },
    /// List the remote ids still needed to complete a snapshot locally.
    /// Reads FURROW_CELL_KEY and FURROW_DEDUP_SECRET.
    Want {
        #[arg(long)]
        head: String,
    },
    /// Verify and store the objects in an inbox, one file per remote id.
    /// Reads FURROW_CELL_KEY and FURROW_DEDUP_SECRET.
    Import {
        #[arg(long)]
        head: String,
        #[arg(long)]
        inbox: PathBuf,
    },
    /// Restore a snapshot held in the store into the --repo folder.
    Materialize {
        #[arg(long)]
        head: String,
        /// Put the sealed `.cell/` entry in this directory instead of the tree.
        #[arg(long, value_name = "DIR")]
        cell_dir: Option<PathBuf>,
    },
}

pub fn run(verb: Exchange, repo: &Path, json: bool) -> anyhow::Result<()> {
    let ok = execute(verb, repo)?;
    if json {
        println!("{ok}");
    } else {
        println!("{}", describe(&ok));
    }
    Ok(())
}

fn execute(verb: Exchange, repo: &Path) -> anyhow::Result<Value> {
    let data_dir = data_root()?;
    match verb {
        Exchange::Export {
            head,
            outbox,
            ledger,
            max_frame,
        } => ops::export(
            &data_dir,
            &keys_from_env()?,
            &head,
            &ledger,
            &outbox,
            max_frame,
        ),
        Exchange::Published { ledger, frames } => {
            ops::record_published(&data_dir, &ledger, &frames)
        }
        Exchange::Want { head } => ops::wanted(&data_dir, &keys_from_env()?, &head),
        Exchange::Import { head, inbox } => {
            ops::import(&data_dir, &keys_from_env()?, &head, &inbox)
        }
        Exchange::Materialize { head, cell_dir } => {
            ops::restore_head(&data_dir, repo, cell_dir, &head)
        }
    }
}

/// One line for a person; scripts use `--json`.
fn describe(ok: &Value) -> String {
    if let Some(frames) = ok["frames"].as_array() {
        return format!(
            "Exported {} objects in {} frames",
            ok["objects"],
            frames.len()
        );
    }
    if let Some(want) = ok["want"].as_array() {
        return format!("{} objects wanted", want.len());
    }
    if let Some(recorded) = ok.get("recorded") {
        return format!("Recorded {recorded} objects");
    }
    if let Some(imported) = ok.get("imported") {
        return format!("Imported {imported} objects");
    }
    format!(
        "Restored snapshot {}",
        ok["snapshot"].as_str().unwrap_or_default()
    )
}
