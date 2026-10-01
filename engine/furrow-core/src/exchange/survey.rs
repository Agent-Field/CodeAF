//! What of a snapshot a store holds, and what it still lacks.

use crate::bundle::object_edges;
use crate::model::{ObjectId, ObjectKind};
use crate::store::ObjectStore;
use std::collections::{HashSet, VecDeque};

pub type Object = (ObjectKind, ObjectId);

/// The objects reachable from a head, split by whether the store has them.
/// Missing objects are not opened, so nothing below them is listed.
#[derive(Default)]
pub struct Survey {
    pub present: Vec<Object>,
    pub missing: Vec<Object>,
}

/// A breadth-first walk that can be resumed. `survey` runs it once; an import
/// keeps it across passes so each pass explores only what the objects it just
/// stored opened up, instead of re-reading every object already walked.
pub struct Walk {
    seen: HashSet<ObjectId>,
    queue: VecDeque<Object>,
}

impl Walk {
    pub fn new(head: ObjectId) -> Self {
        Self {
            seen: HashSet::from([head]),
            queue: VecDeque::from([(ObjectKind::Snapshot, head)]),
        }
    }

    /// Explores until the queue is empty or `missing_limit` objects are known
    /// to be absent, and returns what this call newly found. An object absent
    /// from the store is reported once and never queued again, so a later call
    /// lists only objects that `expand` has since made reachable.
    pub fn explore(&mut self, store: &ObjectStore, missing_limit: usize) -> anyhow::Result<Survey> {
        // One maintenance section for the whole exploration: every object read
        // inside it reuses the lock and the open pack instead of taking them again.
        let _section = store.acquire_maintenance_shared()?;
        let mut found = Survey::default();
        while found.missing.len() < missing_limit {
            let Some((kind, id)) = self.queue.pop_front() else {
                break;
            };
            if !store.contains_object(&id)? {
                found.missing.push((kind, id));
                continue;
            }
            found.present.push((kind, id));
            if !is_leaf(kind) {
                self.expand(kind, &store.read_bytes(&id, kind)?)?;
            }
        }
        Ok(found)
    }

    /// Queues the children of an object whose bytes the caller already holds.
    pub fn expand(&mut self, kind: ObjectKind, bytes: &[u8]) -> anyhow::Result<()> {
        for (child, child_kind) in object_edges(kind, bytes)? {
            if self.seen.insert(child) {
                self.queue.push_back((child_kind, child));
            }
        }
        Ok(())
    }
}

/// Walks from `head` breadth-first, stopping once `missing_limit` objects are
/// known to be absent.
pub fn survey(store: &ObjectStore, head: ObjectId, missing_limit: usize) -> anyhow::Result<Survey> {
    Walk::new(head).explore(store, missing_limit)
}

/// Chunks and xattrs are leaves, so they are never opened during a walk.
fn is_leaf(kind: ObjectKind) -> bool {
    matches!(kind, ObjectKind::Chunk | ObjectKind::Xattrs)
}
