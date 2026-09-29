//! `materialize`: put an imported head on disk.

use crate::model::{ObjectId, SnapshotTrigger};
use crate::repository::{FurrowRepository, SealOptions};
use std::path::{Path, PathBuf};

/// Restores `head` into `target`, which may be an empty folder in a data
/// directory that has never seen it. `cell_dir`, when given, receives the
/// sealed `.cell/` entry outside the tree, as a composed rewind does.
///
/// The folder is attached first, which seals what it holds now (nothing, for
/// an empty one), and the head is then restored over it by the ordinary
/// rewind, so a folder that was not empty is undoable.
pub fn materialize(
    data_dir: &Path,
    target: &Path,
    cell_dir: Option<PathBuf>,
    head: ObjectId,
) -> anyhow::Result<ObjectId> {
    let options = SealOptions {
        changed: None,
        overlay: cell_dir,
    };
    let (mut repository, _) = FurrowRepository::attach_and_seal_in(
        data_dir,
        target,
        Some("before materialize".to_owned()),
        SnapshotTrigger::SyncPull,
        options,
    )?;
    repository.rewind(&head, &[], false)?;
    Ok(head)
}
