//! `want` and `import`: learn what a head still needs, then accept it.

use super::ledger::{parse_rid, Ledger};
use super::survey::{survey, Object};
use crate::fault;
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
    /// The ledger of the store the objects came from. Whatever a store handed
    /// us is by definition already in it, so it is recorded as published there
    /// and the next export never sends it back.
    pub ledger: &'a Ledger,
}

/// What one import call took: the objects it stored and, in primed mode, the
/// unwanted inbox files it deleted.
#[derive(Debug, Default, PartialEq, Eq)]
pub struct Taken {
    pub imported: usize,
    pub extras_deleted: usize,
}

impl Import<'_> {
    /// Takes every wanted file out of the inbox and returns how many it took.
    ///
    /// A delivered object can name children that were not wanted until it
    /// arrived, so passes repeat until one takes nothing. A file that no pass
    /// wanted is an error.
    pub fn run(&self) -> anyhow::Result<usize> {
        Ok(self.take(false)?.imported)
    }

    /// The same import with the rule a priming take needs: an inbox file no
    /// pass wanted is deleted rather than an error, and the count comes back.
    pub fn run_primed(&self) -> anyhow::Result<Taken> {
        self.take(true)
    }

    /// One call, one batch: the catalog commits once and the active pack
    /// syncs once, however many objects and passes the inbox holds, instead
    /// of once per object. The files are deleted only after the batch
    /// commits, so a failure mid-batch rolls the catalog back to a state with
    /// no partial object and still leaves every file its next attempt wants;
    /// pack bytes the rollback outruns are the store's rebuild-from-pack
    /// rule's to tolerate, as the comment on `ObjectStore::batched` says.
    fn take(&self, primed: bool) -> anyhow::Result<Taken> {
        let mut taken = Taken::default();
        let mut stored = Vec::new();
        self.store.batched(|| {
            loop {
                let pass = self.pass(&mut stored)?;
                if pass.is_empty() {
                    break;
                }
                self.ledger.append(&pass)?;
                taken.imported += pass.len();
            }
            fault::point("import.stored");
            Ok(())
        })?;
        for path in stored {
            fs::remove_file(&path).with_context(|| format!("delete {}", path.display()))?;
        }
        taken.extras_deleted = self.settle_leftovers(primed)?;
        Ok(taken)
    }

    /// One pass over the inbox: the rids of the objects it stored, and the
    /// paths of the files now safe to delete once the batch commits.
    fn pass(&self, stored: &mut Vec<PathBuf>) -> anyhow::Result<Vec<ObjectId>> {
        let wanted = self.wanted()?;
        let mut taken = Vec::new();
        for (rid, path) in self.inbox_files()? {
            if let Some((kind, id)) = wanted.get(&rid) {
                self.store_verified(*kind, id, &rid, &path)
                    .map_err(|error| {
                        anyhow::anyhow!("import object {}: {error:#}", id_hex(&rid))
                    })?;
                taken.push(rid);
                stored.push(path);
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
    /// stored (a crash between storing and deleting) or unwanted. The copies
    /// go in both modes; the unwanted ones are an error in strict mode, with
    /// nothing deleted, and are deleted in primed mode, where the caller
    /// already knows the inbox holds more than this head wants.
    fn settle_leftovers(&self, primed: bool) -> anyhow::Result<usize> {
        let leftovers = self.inbox_files()?;
        if leftovers.is_empty() {
            return Ok(0);
        }
        let held = self.held_rids()?;
        let extra = |(rid, _): &(&ObjectId, &PathBuf)| !held.contains(*rid);
        if !primed {
            if let Some((rid, _)) = leftovers.iter().find(extra) {
                anyhow::bail!(
                    "import object {}: not wanted for head {}",
                    id_hex(rid),
                    id_hex(&self.head)
                );
            }
        }
        for path in leftovers.values() {
            fs::remove_file(path).with_context(|| format!("delete {}", path.display()))?;
        }
        Ok(leftovers.iter().filter(extra).count())
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
