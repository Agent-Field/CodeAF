//! Deterministic three-way planning for complete workspace trees.

use crate::model::{EntryKind, ObjectId, TreeEntry};
use crate::store::ObjectStore;
use crate::tree;
use serde::Serialize;
use std::collections::{BTreeMap, BTreeSet};
use std::fmt;

#[derive(Debug, Clone, Serialize)]
pub struct MergePlan {
    pub changes: Vec<MergeChange>,
    pub conflicts: Vec<MergeConflict>,
}

#[derive(Debug, Clone, Serialize)]
pub struct MergeChange {
    #[serde(with = "serde_bytes")]
    pub path: Vec<u8>,
    pub action: MergeAction,
}

#[derive(Debug, Clone, Serialize)]
#[serde(rename_all = "snake_case", tag = "kind", content = "entry")]
pub enum MergeAction {
    Set(TreeEntry),
    Remove,
}

#[derive(Debug, Clone, Serialize)]
pub struct MergeConflict {
    #[serde(with = "serde_bytes")]
    pub path: Vec<u8>,
    pub kind: ConflictKind,
}

#[derive(Debug, Clone, Copy, Eq, PartialEq, Serialize)]
#[serde(rename_all = "snake_case")]
pub enum ConflictKind {
    ModifyModify,
    DeleteModify,
    Type,
    Ancestor,
}

impl fmt::Display for ConflictKind {
    fn fmt(&self, formatter: &mut fmt::Formatter<'_>) -> fmt::Result {
        formatter.write_str(match self {
            Self::ModifyModify => "modify/modify",
            Self::DeleteModify => "delete/modify",
            Self::Type => "type",
            Self::Ancestor => "ancestor",
        })
    }
}

pub fn plan(
    base: &BTreeMap<Vec<u8>, TreeEntry>,
    ours: &BTreeMap<Vec<u8>, TreeEntry>,
    theirs: &BTreeMap<Vec<u8>, TreeEntry>,
) -> MergePlan {
    let mut paths = BTreeSet::new();
    paths.extend(base.keys().cloned());
    paths.extend(ours.keys().cloned());
    paths.extend(theirs.keys().cloned());

    let mut changes = Vec::new();
    let mut conflicts = Vec::new();
    for path in paths {
        let base_entry = base.get(&path);
        let ours_entry = ours.get(&path);
        let theirs_entry = theirs.get(&path);
        if equivalent(ours_entry, theirs_entry) {
            continue;
        }
        if equivalent(base_entry, ours_entry) {
            changes.push(MergeChange {
                path,
                action: theirs_entry
                    .cloned()
                    .map_or(MergeAction::Remove, MergeAction::Set),
            });
            continue;
        }
        if equivalent(base_entry, theirs_entry) {
            continue;
        }

        conflicts.push(MergeConflict {
            path,
            kind: direct_conflict(base_entry, ours_entry, theirs_entry),
        });
    }

    let mut ancestor_conflicts = BTreeSet::new();
    for (path, base_entry) in base {
        if base_entry.kind != EntryKind::Directory {
            continue;
        }
        let ours_is_directory = ours
            .get(path)
            .is_some_and(|entry| entry.kind == EntryKind::Directory);
        let theirs_is_directory = theirs
            .get(path)
            .is_some_and(|entry| entry.kind == EntryKind::Directory);
        if !ours_is_directory
            && theirs_is_directory
            && subtree_changed(base, theirs, path)
            && !equivalent(ours.get(path), theirs.get(path))
        {
            ancestor_conflicts.insert(path.clone());
        }
        if !theirs_is_directory
            && ours_is_directory
            && subtree_changed(base, ours, path)
            && !equivalent(ours.get(path), theirs.get(path))
        {
            ancestor_conflicts.insert(path.clone());
        }
    }
    for path in ancestor_conflicts {
        if !conflicts.iter().any(|conflict| conflict.path == path) {
            conflicts.push(MergeConflict {
                path: path.clone(),
                kind: ConflictKind::Ancestor,
            });
        }
    }
    conflicts.sort_by(|left, right| left.path.cmp(&right.path));
    conflicts.dedup_by(|left, right| left.path == right.path);
    changes.retain(|change| {
        !conflicts.iter().any(|conflict| {
            change.path == conflict.path || is_descendant(&change.path, &conflict.path)
        })
    });
    MergePlan { changes, conflicts }
}

/// Plans from Merkle roots while retaining only paths that differ from the
/// base. Identical directory pages are skipped by `tree::diff_entries`, so
/// memory is bounded by semantic changes rather than total repository size.
pub fn plan_trees(
    store: &ObjectStore,
    base_root: &ObjectId,
    ours_root: &ObjectId,
    theirs_root: &ObjectId,
) -> anyhow::Result<MergePlan> {
    let mut ours_delta = BTreeMap::new();
    let mut theirs_delta = BTreeMap::new();
    collect_delta(store, base_root, ours_root, &[], &mut ours_delta)?;
    collect_delta(store, base_root, theirs_root, &[], &mut theirs_delta)?;

    let mut paths = BTreeSet::new();
    paths.extend(ours_delta.keys().cloned());
    paths.extend(theirs_delta.keys().cloned());
    let mut changes = Vec::new();
    let mut conflicts = Vec::new();
    for path in paths {
        let base_entry = ours_delta
            .get(&path)
            .or_else(|| theirs_delta.get(&path))
            .and_then(|delta| delta.before.as_ref());
        let ours_entry = ours_delta
            .get(&path)
            .map_or(base_entry, |delta| delta.after.as_ref());
        let theirs_entry = theirs_delta
            .get(&path)
            .map_or(base_entry, |delta| delta.after.as_ref());

        if equivalent(ours_entry, theirs_entry) {
            continue;
        }
        if equivalent(base_entry, ours_entry) {
            changes.push(MergeChange {
                path,
                action: theirs_entry
                    .cloned()
                    .map_or(MergeAction::Remove, MergeAction::Set),
            });
            continue;
        }
        if equivalent(base_entry, theirs_entry) {
            continue;
        }
        conflicts.push(MergeConflict {
            path,
            kind: direct_conflict(base_entry, ours_entry, theirs_entry),
        });
    }

    for (path, delta) in &ours_delta {
        let Some(base_entry) = delta.before.as_ref() else {
            continue;
        };
        if base_entry.kind != EntryKind::Directory {
            continue;
        }
        let ours_is_directory = delta
            .after
            .as_ref()
            .is_some_and(|entry| entry.kind == EntryKind::Directory);
        let theirs_entry = theirs_delta
            .get(path)
            .and_then(|delta| delta.after.as_ref())
            .unwrap_or(base_entry);
        if !ours_is_directory
            && theirs_entry.kind == EntryKind::Directory
            && delta_has_descendant(&theirs_delta, path)
        {
            add_ancestor_conflict(&mut conflicts, path);
        }
    }
    for (path, delta) in &theirs_delta {
        let Some(base_entry) = delta.before.as_ref() else {
            continue;
        };
        if base_entry.kind != EntryKind::Directory {
            continue;
        }
        let theirs_is_directory = delta
            .after
            .as_ref()
            .is_some_and(|entry| entry.kind == EntryKind::Directory);
        let ours_entry = ours_delta
            .get(path)
            .and_then(|delta| delta.after.as_ref())
            .unwrap_or(base_entry);
        if !theirs_is_directory
            && ours_entry.kind == EntryKind::Directory
            && delta_has_descendant(&ours_delta, path)
        {
            add_ancestor_conflict(&mut conflicts, path);
        }
    }

    conflicts.sort_by(|left, right| left.path.cmp(&right.path));
    conflicts.dedup_by(|left, right| left.path == right.path);
    changes.retain(|change| {
        !conflicts.iter().any(|conflict| {
            change.path == conflict.path || is_descendant(&change.path, &conflict.path)
        })
    });
    Ok(MergePlan { changes, conflicts })
}

#[derive(Debug, Clone)]
struct Delta {
    before: Option<TreeEntry>,
    after: Option<TreeEntry>,
}

fn collect_delta(
    store: &ObjectStore,
    before_root: &ObjectId,
    after_root: &ObjectId,
    prefix: &[u8],
    output: &mut BTreeMap<Vec<u8>, Delta>,
) -> anyhow::Result<()> {
    if before_root == after_root {
        return Ok(());
    }
    tree::diff_entries(store, before_root, after_root, &mut |before, after| {
        let name = before
            .as_ref()
            .or(after.as_ref())
            .expect("tree diff always provides an entry")
            .name
            .clone();
        let path = join_path(prefix, &name);
        match (&before, &after) {
            (Some(left), Some(right))
                if left.kind == EntryKind::Directory && right.kind == EntryKind::Directory =>
            {
                if !equivalent(Some(left), Some(right)) {
                    output.insert(
                        path.clone(),
                        Delta {
                            before: before.clone(),
                            after: after.clone(),
                        },
                    );
                }
                collect_delta(
                    store,
                    &left.target.expect("directory has a tree target"),
                    &right.target.expect("directory has a tree target"),
                    &path,
                    output,
                )?;
            }
            _ => {
                if !equivalent(before.as_ref(), after.as_ref()) {
                    output.insert(
                        path.clone(),
                        Delta {
                            before: before.clone(),
                            after: after.clone(),
                        },
                    );
                }
                if let Some(entry) = before
                    .as_ref()
                    .filter(|entry| entry.kind == EntryKind::Directory)
                {
                    collect_subtree(
                        store,
                        &entry.target.expect("directory has a tree target"),
                        &path,
                        false,
                        output,
                    )?;
                }
                if let Some(entry) = after
                    .as_ref()
                    .filter(|entry| entry.kind == EntryKind::Directory)
                {
                    collect_subtree(
                        store,
                        &entry.target.expect("directory has a tree target"),
                        &path,
                        true,
                        output,
                    )?;
                }
            }
        }
        Ok(())
    })
}

fn collect_subtree(
    store: &ObjectStore,
    root: &ObjectId,
    prefix: &[u8],
    added: bool,
    output: &mut BTreeMap<Vec<u8>, Delta>,
) -> anyhow::Result<()> {
    tree::for_each_entry(store, root, |entry| {
        let path = join_path(prefix, &entry.name);
        let delta = if added {
            Delta {
                before: None,
                after: Some(entry.clone()),
            }
        } else {
            Delta {
                before: Some(entry.clone()),
                after: None,
            }
        };
        output.insert(path.clone(), delta);
        if entry.kind == EntryKind::Directory {
            collect_subtree(
                store,
                &entry.target.expect("directory has a tree target"),
                &path,
                added,
                output,
            )?;
        }
        Ok(())
    })
}

fn join_path(prefix: &[u8], name: &[u8]) -> Vec<u8> {
    if prefix.is_empty() {
        return name.to_vec();
    }
    let mut path = Vec::with_capacity(prefix.len() + 1 + name.len());
    path.extend_from_slice(prefix);
    path.push(b'/');
    path.extend_from_slice(name);
    path
}

fn delta_has_descendant(deltas: &BTreeMap<Vec<u8>, Delta>, directory: &[u8]) -> bool {
    deltas
        .range(directory.to_vec()..)
        .find(|(path, _)| path.as_slice() != directory)
        .is_some_and(|(path, _)| is_descendant(path, directory))
}

fn add_ancestor_conflict(conflicts: &mut Vec<MergeConflict>, path: &[u8]) {
    if !conflicts.iter().any(|conflict| conflict.path == path) {
        conflicts.push(MergeConflict {
            path: path.to_vec(),
            kind: ConflictKind::Ancestor,
        });
    }
}

fn equivalent(left: Option<&TreeEntry>, right: Option<&TreeEntry>) -> bool {
    match (left, right) {
        (None, None) => true,
        (Some(left), Some(right)) => {
            let mut left = left.clone();
            let mut right = right.clone();
            left.mtime_secs = 0;
            left.mtime_nanos = 0;
            right.mtime_secs = 0;
            right.mtime_nanos = 0;
            if left.kind == EntryKind::Directory && right.kind == EntryKind::Directory {
                left.target = None;
                right.target = None;
            }
            left == right
        }
        _ => false,
    }
}

fn direct_conflict(
    base: Option<&TreeEntry>,
    ours: Option<&TreeEntry>,
    theirs: Option<&TreeEntry>,
) -> ConflictKind {
    if ours.is_none() || theirs.is_none() {
        return ConflictKind::DeleteModify;
    }
    let ours = ours.expect("checked above");
    let theirs = theirs.expect("checked above");
    if ours.kind != theirs.kind || base.is_some_and(|entry| entry.kind != ours.kind) {
        ConflictKind::Type
    } else {
        ConflictKind::ModifyModify
    }
}

fn subtree_changed(
    base: &BTreeMap<Vec<u8>, TreeEntry>,
    side: &BTreeMap<Vec<u8>, TreeEntry>,
    directory: &[u8],
) -> bool {
    base.iter()
        .filter(|(path, _)| is_descendant(path, directory))
        .any(|(path, entry)| !equivalent(Some(entry), side.get(path)))
        || side
            .iter()
            .filter(|(path, _)| is_descendant(path, directory))
            .any(|(path, entry)| !equivalent(base.get(path), Some(entry)))
}

fn is_descendant(path: &[u8], ancestor: &[u8]) -> bool {
    path.len() > ancestor.len()
        && path.starts_with(ancestor)
        && path.get(ancestor.len()) == Some(&b'/')
}

#[cfg(test)]
mod tests {
    use super::*;
    use crate::store::ObjectStore;
    use crate::tree;

    fn file(byte: u8) -> TreeEntry {
        TreeEntry {
            name: vec![byte],
            kind: EntryKind::File,
            target: Some([byte; 32]),
            link_target: Vec::new(),
            mode: 0o100644,
            size: 1,
            mtime_secs: 0,
            mtime_nanos: 0,
            xattrs: None,
            class: Default::default(),
        }
    }

    fn directory(target: u8) -> TreeEntry {
        TreeEntry {
            name: b"dir".to_vec(),
            kind: EntryKind::Directory,
            target: Some([target; 32]),
            link_target: Vec::new(),
            mode: 0o40755,
            size: 0,
            mtime_secs: 0,
            mtime_nanos: 0,
            xattrs: None,
            class: Default::default(),
        }
    }

    fn named_file(name: &[u8], byte: u8) -> TreeEntry {
        let mut entry = file(byte);
        entry.name = name.to_vec();
        entry
    }

    fn named_directory(name: &[u8], target: ObjectId) -> TreeEntry {
        let mut entry = directory(0);
        entry.name = name.to_vec();
        entry.target = Some(target);
        entry
    }

    fn assert_merkle_matches_flat(
        store: &ObjectStore,
        roots: [ObjectId; 3],
        maps: [&BTreeMap<Vec<u8>, TreeEntry>; 3],
    ) {
        let expected = plan(maps[0], maps[1], maps[2]);
        let actual = plan_trees(store, &roots[0], &roots[1], &roots[2]).unwrap();
        assert_eq!(
            serde_json::to_value(actual).unwrap(),
            serde_json::to_value(expected).unwrap()
        );
    }

    #[test]
    fn merges_independent_changes_and_ignores_directory_merkle_ids() {
        let base = BTreeMap::from([
            (b"dir".to_vec(), directory(1)),
            (b"dir/a".to_vec(), file(1)),
            (b"dir/b".to_vec(), file(2)),
        ]);
        let mut ours = base.clone();
        ours.insert(b"dir".to_vec(), directory(2));
        ours.insert(b"dir/a".to_vec(), file(3));
        let mut theirs = base.clone();
        theirs.insert(b"dir".to_vec(), directory(3));
        theirs.insert(b"dir/b".to_vec(), file(4));

        let plan = plan(&base, &ours, &theirs);
        assert!(plan.conflicts.is_empty());
        assert_eq!(plan.changes.len(), 1);
        assert_eq!(plan.changes[0].path, b"dir/b");
    }

    #[test]
    fn reports_divergent_and_delete_modify_conflicts() {
        let base = BTreeMap::from([(b"value".to_vec(), file(1))]);
        let ours = BTreeMap::from([(b"value".to_vec(), file(2))]);
        let theirs = BTreeMap::from([(b"value".to_vec(), file(3))]);
        assert_eq!(
            plan(&base, &ours, &theirs).conflicts[0].kind,
            ConflictKind::ModifyModify
        );
        assert_eq!(
            plan(&base, &BTreeMap::new(), &theirs).conflicts[0].kind,
            ConflictKind::DeleteModify
        );
    }

    #[test]
    fn identical_additions_with_different_timestamps_are_already_converged() {
        let base = BTreeMap::new();
        let ours = BTreeMap::from([(b"coord/task".to_vec(), file(1))]);
        let mut timestamped = file(1);
        timestamped.mtime_secs = 42;
        timestamped.mtime_nanos = 7;
        let theirs = BTreeMap::from([(b"coord/task".to_vec(), timestamped)]);

        let plan = plan(&base, &ours, &theirs);
        assert!(plan.conflicts.is_empty());
        assert!(plan.changes.is_empty());
    }

    #[test]
    fn directory_delete_conflicts_with_new_descendant() {
        let base = BTreeMap::from([
            (b"dir".to_vec(), directory(1)),
            (b"dir/old".to_vec(), file(1)),
        ]);
        let ours = BTreeMap::new();
        let mut theirs = base.clone();
        theirs.insert(b"dir/new".to_vec(), file(2));
        theirs.insert(b"dir".to_vec(), directory(2));

        let plan = plan(&base, &ours, &theirs);
        assert!(plan
            .conflicts
            .iter()
            .any(|conflict| conflict.path == b"dir"));
        assert!(plan.changes.is_empty());
    }

    #[test]
    fn merkle_planner_matches_flat_oracle_for_nested_changes_and_ancestor_conflicts() {
        let temporary = tempfile::tempdir().unwrap();
        let store = ObjectStore::open(temporary.path().join("store")).unwrap();

        let base_child =
            tree::write(&store, vec![named_file(b"a", 1), named_file(b"b", 2)]).unwrap();
        let ours_child =
            tree::write(&store, vec![named_file(b"a", 3), named_file(b"b", 2)]).unwrap();
        let theirs_child = tree::write(
            &store,
            vec![
                named_file(b"a", 1),
                named_file(b"b", 4),
                named_file(b"c", 5),
            ],
        )
        .unwrap();
        let roots = [
            tree::write(&store, vec![named_directory(b"dir", base_child)]).unwrap(),
            tree::write(&store, vec![named_directory(b"dir", ours_child)]).unwrap(),
            tree::write(&store, vec![named_directory(b"dir", theirs_child)]).unwrap(),
        ];
        let base = BTreeMap::from([
            (b"dir".to_vec(), named_directory(b"dir", base_child)),
            (b"dir/a".to_vec(), named_file(b"a", 1)),
            (b"dir/b".to_vec(), named_file(b"b", 2)),
        ]);
        let ours = BTreeMap::from([
            (b"dir".to_vec(), named_directory(b"dir", ours_child)),
            (b"dir/a".to_vec(), named_file(b"a", 3)),
            (b"dir/b".to_vec(), named_file(b"b", 2)),
        ]);
        let theirs = BTreeMap::from([
            (b"dir".to_vec(), named_directory(b"dir", theirs_child)),
            (b"dir/a".to_vec(), named_file(b"a", 1)),
            (b"dir/b".to_vec(), named_file(b"b", 4)),
            (b"dir/c".to_vec(), named_file(b"c", 5)),
        ]);
        assert_merkle_matches_flat(&store, roots, [&base, &ours, &theirs]);

        let deleted_root = tree::write(&store, Vec::new()).unwrap();
        let added_child =
            tree::write(&store, vec![named_file(b"a", 1), named_file(b"new", 6)]).unwrap();
        let added_root = tree::write(&store, vec![named_directory(b"dir", added_child)]).unwrap();
        let deleted = BTreeMap::new();
        let added = BTreeMap::from([
            (b"dir".to_vec(), named_directory(b"dir", added_child)),
            (b"dir/a".to_vec(), named_file(b"a", 1)),
            (b"dir/new".to_vec(), named_file(b"new", 6)),
        ]);
        assert_merkle_matches_flat(
            &store,
            [roots[0], deleted_root, added_root],
            [&base, &deleted, &added],
        );
    }
}
