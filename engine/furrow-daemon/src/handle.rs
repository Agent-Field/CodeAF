//! One store's open repository: opened on first use, kept for the daemon's
//! life. It lives on the thread that made it (the repository is not `Send`).

use crate::wire::Target;
use anyhow::Context;
use furrow::model::{ObjectId, SnapshotTrigger};
use furrow::{FurrowRepository, SealOptions};
use std::path::PathBuf;

pub struct Handle {
    target: Target,
    repo: Option<FurrowRepository>,
}

impl Handle {
    pub fn new(target: Target) -> Self {
        Self { target, repo: None }
    }

    /// The open repository, opened on first use. The workspace must already
    /// be attached: only a seal attaches.
    pub fn repo(&mut self) -> anyhow::Result<&mut FurrowRepository> {
        if self.repo.is_none() {
            let target = &self.target;
            let repo = FurrowRepository::open_composed_in(
                &target.data_dir,
                &target.tree,
                target.cell_dir.clone(),
            )
            .context("open store")?;
            self.repo = Some(repo);
        }
        Ok(self.repo.as_mut().expect("opened above"))
    }

    /// Seals the tree, attaching it first when this store has not seen it.
    pub fn seal(
        &mut self,
        label: String,
        changed: Option<Vec<PathBuf>>,
    ) -> anyhow::Result<ObjectId> {
        match self.repo.as_mut() {
            Some(repo) => repo.seal(Some(label), SnapshotTrigger::AgentRun, changed.as_deref()),
            None => self.attach_and_seal(label, changed),
        }
    }

    fn attach_and_seal(
        &mut self,
        label: String,
        changed: Option<Vec<PathBuf>>,
    ) -> anyhow::Result<ObjectId> {
        let target = &self.target;
        let options = SealOptions {
            changed,
            overlay: target.cell_dir.clone(),
        };
        let (repo, id) = FurrowRepository::attach_and_seal_in(
            &target.data_dir,
            &target.tree,
            Some(label),
            SnapshotTrigger::AgentRun,
            options,
        )?;
        self.repo = Some(repo);
        Ok(id)
    }
}
