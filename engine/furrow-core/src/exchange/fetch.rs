//! `want` and `import`: learn what a head still needs, then accept it.

use super::ledger::parse_rid;
use super::survey::{survey, Object};
use crate::model::{id_hex, ObjectId};
use crate::sealer::Sealer;
use crate::store::ObjectStore;
use anyhow::Context;
use std::collections::{BTreeMap, HashMap, HashSet};
use std::fs;
use std::path::{Path, PathBuf};

/// The most rids one `want` names.
pub const WANT_LIMIT: usize = 1000;

/// The rids needed next to complete `head` locally, nearest the snapshot
/// first. Empty means the head is complete.
pub fn want(
    store: &ObjectStore,
    sealer: &dyn Sealer,
    head: ObjectId,
) -> anyhow::Result<Vec<ObjectId>> {
    let found = survey(store, head, WANT_LIMIT)?;
    Ok(found
        .missing
        .iter()
        .map(|(kind, id)| sealer.remote_id(*kind, id))
        .collect())
}

pub struct Import<'a> {
    pub store: &'a ObjectStore,
    pub sealer: &'a dyn Sealer,
    pub head: ObjectId,
    pub inbox: &'a Path,
}

impl Import<'_> {
    /// Takes every wanted file out of the inbox and returns how many it took.
    ///
    /// A delivered object can name children that were not wanted until it
    /// arrived, so passes repeat until one takes nothing. Each object is
    /// verified before it is stored and its file is deleted only afterwards,
    /// so a failure leaves whole objects and untouched files, never a partial
    /// object. A file that no pass wanted is an error.
    pub fn run(&self) -> anyhow::Result<usize> {
        let mut imported = 0;
        loop {
            let taken = self.pass()?;
            if taken == 0 {
                break;
            }
            imported += taken;
        }
        self.settle_leftovers()?;
        Ok(imported)
    }

    fn pass(&self) -> anyhow::Result<usize> {
        let wanted = self.wanted()?;
        let mut taken = 0;
        for (rid, path) in self.inbox_files()? {
            if let Some((kind, id)) = wanted.get(&rid) {
                self.accept(*kind, id, &rid, &path)?;
                taken += 1;
            }
        }
        Ok(taken)
    }

    fn wanted(&self) -> anyhow::Result<HashMap<ObjectId, Object>> {
        let missing = survey(self.store, self.head, WANT_LIMIT)?.missing;
        Ok(missing
            .into_iter()
            .map(|(kind, id)| (self.sealer.remote_id(kind, &id), (kind, id)))
            .collect())
    }

    fn accept(
        &self,
        kind: crate::model::ObjectKind,
        id: &ObjectId,
        rid: &ObjectId,
        path: &Path,
    ) -> anyhow::Result<()> {
        self.store_verified(kind, id, rid, path)
            .map_err(|error| anyhow::anyhow!("import object {}: {error:#}", id_hex(rid)))?;
        fs::remove_file(path).with_context(|| format!("delete {}", path.display()))
    }

    fn store_verified(
        &self,
        kind: crate::model::ObjectKind,
        id: &ObjectId,
        rid: &ObjectId,
        path: &Path,
    ) -> anyhow::Result<()> {
        let sealed = fs::read(path)?;
        let bytes = self.sealer.open(kind, id, rid, &sealed)?;
        anyhow::ensure!(
            self.store.put_bytes(kind, &bytes)? == *id,
            "stored id does not match"
        );
        Ok(())
    }

    /// Files left after the last pass are either copies of objects already
    /// stored (a crash between storing and deleting) or unwanted. The first
    /// unwanted one is an error and nothing is deleted; otherwise the copies go.
    fn settle_leftovers(&self) -> anyhow::Result<()> {
        let leftovers = self.inbox_files()?;
        if leftovers.is_empty() {
            return Ok(());
        }
        let held = self.held_rids()?;
        if let Some((rid, _)) = leftovers.iter().find(|(rid, _)| !held.contains(*rid)) {
            anyhow::bail!(
                "import object {}: not wanted for head {}",
                id_hex(rid),
                id_hex(&self.head)
            );
        }
        for (_, path) in leftovers {
            fs::remove_file(&path).with_context(|| format!("delete {}", path.display()))?;
        }
        Ok(())
    }

    fn held_rids(&self) -> anyhow::Result<HashSet<ObjectId>> {
        let found = survey(self.store, self.head, usize::MAX)?;
        Ok(found
            .present
            .iter()
            .map(|(kind, id)| self.sealer.remote_id(*kind, id))
            .collect())
    }

    /// The inbox files by rid. Names starting with a dot are files still being
    /// written and are left alone; any other name that is not a rid is an error.
    fn inbox_files(&self) -> anyhow::Result<BTreeMap<ObjectId, PathBuf>> {
        let mut files = BTreeMap::new();
        for entry in fs::read_dir(self.inbox)
            .with_context(|| format!("read inbox {}", self.inbox.display()))?
        {
            let entry = entry?;
            let name = entry.file_name().to_string_lossy().into_owned();
            if name.starts_with('.') {
                continue;
            }
            let rid = parse_rid(&name)
                .with_context(|| format!("inbox file {name} is not named by a rid"))?;
            files.insert(rid, entry.path());
        }
        Ok(files)
    }
}
