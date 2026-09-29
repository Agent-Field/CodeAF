//! The per-store ledger of remote ids already published to one store.
//!
//! One rid per line, append-only and fsynced. A ledger is named by the caller
//! (a digest of the store's URL and the identity), so a new or wiped store gets
//! an empty ledger and the next export sends everything again.

use crate::model::{id_hex, ObjectId};
use anyhow::Context;
use std::collections::HashSet;
use std::fs::{self, OpenOptions};
use std::io::Write;
use std::path::{Path, PathBuf};

const NAME_LEN: usize = 16;

pub struct Ledger {
    path: PathBuf,
}

impl Ledger {
    /// The ledger `name` in `data_dir`, or an error when `name` is not 16
    /// lowercase hex characters.
    pub fn named(data_dir: &Path, name: &str) -> anyhow::Result<Self> {
        anyhow::ensure!(
            name.len() == NAME_LEN && name.bytes().all(is_lower_hex),
            "ledger must be {NAME_LEN} lowercase hexadecimal characters"
        );
        Ok(Self {
            path: data_dir.join(format!("published.{name}")),
        })
    }

    /// Every rid recorded so far. A torn or garbled line only means one rid is
    /// sent again, which the store accepts idempotently, so it is skipped.
    pub fn recorded(&self) -> anyhow::Result<HashSet<ObjectId>> {
        let text = match fs::read_to_string(&self.path) {
            Ok(text) => text,
            Err(error) if error.kind() == std::io::ErrorKind::NotFound => String::new(),
            Err(error) => return Err(error).context("read ledger"),
        };
        Ok(text.lines().filter_map(parse_rid).collect())
    }

    /// Appends `rids` and syncs them to disk before returning.
    pub fn append(&self, rids: &[ObjectId]) -> anyhow::Result<()> {
        if rids.is_empty() {
            return Ok(());
        }
        let lines: String = rids.iter().map(|rid| id_hex(rid) + "\n").collect();
        let mut file = OpenOptions::new()
            .create(true)
            .append(true)
            .open(&self.path)
            .context("open ledger")?;
        file.write_all(lines.as_bytes())?;
        file.sync_all().context("sync ledger")?;
        self.sync_directory()
    }

    /// A new ledger file is only durable once its directory entry is.
    fn sync_directory(&self) -> anyhow::Result<()> {
        let parent = self.path.parent().context("ledger has no directory")?;
        fs::File::open(parent)?
            .sync_all()
            .context("sync ledger directory")
    }
}

fn is_lower_hex(byte: u8) -> bool {
    byte.is_ascii_digit() || (b'a'..=b'f').contains(&byte)
}

/// A rid in its canonical spelling: 64 lowercase hex characters.
pub fn parse_rid(text: &str) -> Option<ObjectId> {
    if text.len() != 64 || !text.bytes().all(is_lower_hex) {
        return None;
    }
    let mut rid = [0; 32];
    hex::decode_to_slice(text, &mut rid).ok()?;
    Some(rid)
}
