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

/// Walks from `head` breadth-first, stopping once `missing_limit` objects are
/// known to be absent.
pub fn survey(store: &ObjectStore, head: ObjectId, missing_limit: usize) -> anyhow::Result<Survey> {
    let mut found = Survey::default();
    let mut seen = HashSet::from([head]);
    let mut queue = VecDeque::from([(ObjectKind::Snapshot, head)]);
    while let Some((kind, id)) = queue.pop_front() {
        if found.missing.len() >= missing_limit {
            break;
        }
        if !store.contains_object(&id)? {
            found.missing.push((kind, id));
            continue;
        }
        found.present.push((kind, id));
        for (child, child_kind) in children(store, kind, &id)? {
            if seen.insert(child) {
                queue.push_back((child_kind, child));
            }
        }
    }
    Ok(found)
}

/// Chunks and xattrs are leaves, so they are never opened during a walk.
fn children(
    store: &ObjectStore,
    kind: ObjectKind,
    id: &ObjectId,
) -> anyhow::Result<Vec<(ObjectId, ObjectKind)>> {
    match kind {
        ObjectKind::Chunk | ObjectKind::Xattrs => Ok(Vec::new()),
        _ => object_edges(kind, &store.read_bytes(id, kind)?),
    }
}
