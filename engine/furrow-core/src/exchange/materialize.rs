//! `materialize`: put an imported head on disk.

use crate::model::{ObjectId, SnapshotTrigger};
use crate::repository::{FurrowRepository, HeldPath, SealOptions};
use std::path::{Path, PathBuf};

/// Restores `head` into `target`, which may be an empty folder in a data
/// directory that has never seen it. `cell_dir`, when given, receives the
/// sealed `.cell/` entry outside the tree, as a composed rewind does.
///
/// The folder is attached first, which seals what it holds now (nothing, for
/// an empty one), and the head is then restored over it by the ordinary
/// rewind, so a folder that was not empty is undoable. The head then becomes
/// the workspace head, so the next seal descends from it; its own parent stays
/// with the sender, which makes the chain shallow there.
///
/// A path whose name this file system cannot keep next to another is not
/// written (see [`crate::name_fold`]); [`materialize_holding`] returns those.
pub fn materialize(
    data_dir: &Path,
    target: &Path,
    cell_dir: Option<PathBuf>,
    head: ObjectId,
) -> anyhow::Result<ObjectId> {
    materialize_holding(data_dir, target, cell_dir, head).map(|(head, _)| head)
}

/// [`materialize`], also answering the paths of the head it did not write
/// because their names collide on this file system, so the caller can record
/// them.
pub fn materialize_holding(
    data_dir: &Path,
    target: &Path,
    cell_dir: Option<PathBuf>,
    head: ObjectId,
) -> anyhow::Result<(ObjectId, Vec<HeldPath>)> {
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
    let (_, plan) = repository.rewind(&head, &[], false)?;
    repository.adopt_head(&head)?;
    Ok((head, plan.held))
}
