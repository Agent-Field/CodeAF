//! `export` and `published`: send what a store lacks, record what it has.

use super::frame::{self, Frame, MAX_FRAME};
use super::ledger::Ledger;
use super::survey::{survey, Object};
use crate::model::{id_hex, ObjectId, ObjectKind};
use crate::sealer::{CellSealer, Keys, Sealer};
use crate::store::ObjectStore;
use anyhow::Context;
use serde::Serialize;
use std::fs;
use std::io::Write;
use std::path::{Path, PathBuf};

pub const DEFAULT_MAX_FRAME: usize = 1 << 20;

/// Children before parents: a receiver that sees a parent already has, or is
/// about to receive, everything the parent names. The snapshot goes last.
const SEND_ORDER: [ObjectKind; 5] = [
    ObjectKind::Chunk,
    ObjectKind::Blob,
    ObjectKind::Xattrs,
    ObjectKind::Tree,
    ObjectKind::Snapshot,
];

pub struct ExportJob<'a> {
    pub store: &'a ObjectStore,
    pub keys: &'a Keys,
    pub head: ObjectId,
    pub ledger: &'a Ledger,
    pub outbox: &'a Path,
    pub max_frame: usize,
}

#[derive(Serialize)]
pub struct FrameInfo {
    pub path: PathBuf,
    pub objects: usize,
    pub bytes: usize,
}

#[derive(Serialize)]
pub struct ExportReport {
    pub frames: Vec<FrameInfo>,
    pub head_rid: String,
    pub objects: usize,
    pub bytes: usize,
}

impl ExportJob<'_> {
    /// Seals every object under `head` the ledger does not list and writes
    /// them to the outbox as frames. The result depends only on the store, the
    /// keys and the ledger, so a repeat after a crash writes the same files.
    pub fn run(&self) -> anyhow::Result<ExportReport> {
        anyhow::ensure!(
            self.max_frame <= MAX_FRAME,
            "max-frame is larger than the {MAX_FRAME} bytes a store accepts"
        );
        let sealer = CellSealer::new(self.keys);
        let unsent = self.unsent(&sealer)?;
        fs::create_dir_all(self.outbox)?;
        let outbox = self.outbox.canonicalize()?;
        let mut frames = Vec::new();
        let sealed = unsent
            .into_iter()
            .map(|(kind, id)| seal_stored(&sealer, self.store, kind, &id));
        let key_id = frame::cell_key_id(&self.keys.cell_key);
        frame::pack(&key_id, self.max_frame, sealed, |frame| {
            frames.push(write_frame(&outbox, &frame)?);
            Ok(())
        })?;
        Ok(ExportReport {
            head_rid: id_hex(&sealer.remote_id(ObjectKind::Snapshot, &self.head)),
            objects: frames.iter().map(|info| info.objects).sum(),
            bytes: frames.iter().map(|info| info.bytes).sum(),
            frames,
        })
    }

    /// The reachable objects whose rid the ledger lacks, in send order.
    fn unsent(&self, sealer: &CellSealer) -> anyhow::Result<Vec<Object>> {
        let found = survey(self.store, self.head, 1)?;
        anyhow::ensure!(
            found.missing.is_empty(),
            "head {} is not complete in this store",
            id_hex(&self.head)
        );
        let sent = self.ledger.recorded()?;
        let mut unsent: Vec<Object> = found
            .present
            .into_iter()
            .filter(|(kind, id)| !sent.contains(&sealer.remote_id(*kind, id)))
            .collect();
        unsent.sort_by_key(|(kind, id)| (send_rank(*kind), *id));
        Ok(unsent)
    }
}

fn send_rank(kind: ObjectKind) -> usize {
    SEND_ORDER
        .iter()
        .position(|k| *k == kind)
        .unwrap_or(SEND_ORDER.len())
}

fn seal_stored(
    sealer: &dyn Sealer,
    store: &ObjectStore,
    kind: ObjectKind,
    id: &ObjectId,
) -> anyhow::Result<(ObjectId, Vec<u8>)> {
    sealer.seal(kind, id, &store.read_bytes(id, kind)?)
}

/// Writes the frame under a content-derived name, invisible until complete.
fn write_frame(outbox: &Path, frame: &Frame) -> anyhow::Result<FrameInfo> {
    let path = outbox.join(frame.file_name());
    let partial = path.with_extension("part");
    let mut file = fs::File::create(&partial)?;
    file.write_all(&frame.bytes)?;
    file.sync_all()?;
    fs::rename(&partial, &path)?;
    Ok(FrameInfo {
        path,
        objects: frame.rids.len(),
        bytes: frame.bytes.len(),
    })
}

/// Records the rids of uploaded frames in `ledger`, then deletes the frame
/// files. The ledger is written first: a crash between the two steps leaves
/// files behind, never a rid claimed for an object that was not sent. A frame
/// already deleted by an earlier attempt counts for nothing.
pub fn published(ledger: &Ledger, frames: &[PathBuf]) -> anyhow::Result<usize> {
    let mut rids = Vec::new();
    let mut present = Vec::new();
    for path in frames {
        match fs::read(path) {
            Ok(bytes) => {
                let (_, objects) =
                    frame::decode(&bytes).with_context(|| format!("frame {}", path.display()))?;
                rids.extend(objects.iter().map(|object| object.rid));
                present.push(path);
            }
            Err(error) if error.kind() == std::io::ErrorKind::NotFound => {}
            Err(error) => return Err(error).with_context(|| format!("read {}", path.display())),
        }
    }
    ledger.append(&rids)?;
    for path in present {
        fs::remove_file(path).with_context(|| format!("delete {}", path.display()))?;
    }
    Ok(rids.len())
}
