//! Disk-backed dirty scopes, conflict state, and durable family events.

use crate::content_class::ContentClass;
use crate::model::{ClaimRecord, EntryKind, ObjectId, ObjectKind, Snapshot, TreeEntry};
use crate::store::ObjectStore;
use crate::tree;
use anyhow::Context;
use rusqlite::{params, Connection, OptionalExtension, Transaction};
use serde::{Deserialize, Serialize};
use std::collections::BTreeMap;
use std::path::Path;
use std::time::{Duration, SystemTime, UNIX_EPOCH};

const EXACT: i64 = 0;
const SUBTREE: i64 = 1;
const EVENT_LIMIT: i64 = 10_000;

pub struct Radar {
    connection: Connection,
    family_id: String,
}

#[derive(Clone, Debug)]
pub struct UniverseRegistration<'a> {
    pub fork_id: &'a str,
    pub name: &'a str,
    pub workspace_id: &'a str,
    pub base_snapshot: ObjectId,
    pub head_snapshot: ObjectId,
}

#[derive(Clone, Debug, Default)]
pub struct ForkConflictSummary {
    pub count: u64,
    pub paths: Vec<Vec<u8>>,
}

#[derive(Clone, Debug, Deserialize, Serialize)]
pub struct EventPath {
    pub display: String,
    pub bytes_hex: String,
}

#[derive(Clone, Debug, Deserialize, Serialize)]
pub struct EventFork {
    pub fork_id: String,
    pub name: String,
    pub head_snapshot: String,
}

#[derive(Clone, Debug, Deserialize, Serialize)]
pub struct RadarEvent {
    pub schema: u32,
    pub cursor: String,
    pub event_id: String,
    #[serde(rename = "type")]
    pub event_type: String,
    pub state: String,
    pub conflict_id: String,
    pub path: EventPath,
    pub forks: Vec<EventFork>,
    pub claim_state: String,
    pub occurred_at: i64,
}

#[derive(Clone, Debug, Serialize)]
pub struct EventPage {
    pub cursor_found: bool,
    pub earliest_cursor: Option<String>,
    pub latest_cursor: Option<String>,
    pub events: Vec<RadarEvent>,
}

impl Radar {
    pub fn open(store_root: &Path, family_id: &str) -> anyhow::Result<Self> {
        validate_id(family_id, "family")?;
        let directory = store_root.join("families").join(family_id);
        std::fs::create_dir_all(&directory)?;
        let connection = Connection::open(directory.join("radar.sqlite3"))?;
        connection.busy_timeout(Duration::from_secs(5))?;
        connection.execute_batch(
            "PRAGMA journal_mode=WAL;
             PRAGMA synchronous=FULL;
             PRAGMA temp_store=MEMORY;
             CREATE TABLE IF NOT EXISTS universes(
                fork_id TEXT PRIMARY KEY,
                name TEXT NOT NULL,
                workspace_id TEXT NOT NULL,
                base_snapshot BLOB NOT NULL,
                head_snapshot BLOB,
                updated_at INTEGER NOT NULL
             );
             CREATE UNIQUE INDEX IF NOT EXISTS universes_workspace
                ON universes(workspace_id);
             CREATE TABLE IF NOT EXISTS dirty_scopes(
                fork_id TEXT NOT NULL,
                path BLOB NOT NULL,
                scope INTEGER NOT NULL,
                action TEXT NOT NULL,
                PRIMARY KEY(fork_id, path, scope)
             ) WITHOUT ROWID;
             CREATE INDEX IF NOT EXISTS dirty_path
                ON dirty_scopes(path, scope, fork_id);
             CREATE TABLE IF NOT EXISTS active_conflicts(
                path BLOB PRIMARY KEY,
                signature TEXT NOT NULL,
                claim_state TEXT NOT NULL DEFAULT 'unclaimed',
                opened_at INTEGER NOT NULL
             ) WITHOUT ROWID;
             CREATE TABLE IF NOT EXISTS active_members(
                path BLOB NOT NULL,
                fork_id TEXT NOT NULL,
                PRIMARY KEY(path, fork_id)
             ) WITHOUT ROWID;
             CREATE TABLE IF NOT EXISTS events(
                sequence INTEGER PRIMARY KEY AUTOINCREMENT,
                event_id TEXT NOT NULL UNIQUE,
                kind TEXT NOT NULL,
                state TEXT NOT NULL,
                conflict_id TEXT NOT NULL,
                path BLOB NOT NULL,
                forks_json BLOB NOT NULL,
                claim_state TEXT NOT NULL,
                created_at INTEGER NOT NULL
             );",
        )?;
        let has_claim_state: bool = connection.query_row(
            "SELECT EXISTS(
                SELECT 1 FROM pragma_table_info('active_conflicts') WHERE name = 'claim_state'
             )",
            [],
            |row| row.get(0),
        )?;
        if !has_claim_state {
            connection.execute(
                "ALTER TABLE active_conflicts
                 ADD COLUMN claim_state TEXT NOT NULL DEFAULT 'unclaimed'",
                [],
            )?;
        }
        Ok(Self {
            connection,
            family_id: family_id.to_owned(),
        })
    }

    pub fn retain_universes(
        &mut self,
        fork_ids: &[String],
        claims: &[ClaimRecord],
    ) -> anyhow::Result<()> {
        let transaction = self.connection.transaction()?;
        transaction.execute_batch(
            "CREATE TEMP TABLE IF NOT EXISTS radar_keep(fork_id TEXT PRIMARY KEY) WITHOUT ROWID;
             DELETE FROM radar_keep;",
        )?;
        {
            let mut insert = transaction.prepare("INSERT INTO radar_keep(fork_id) VALUES(?1)")?;
            for fork_id in fork_ids {
                validate_id(fork_id, "fork")?;
                insert.execute(params![fork_id])?;
            }
        }
        transaction.execute(
            "DELETE FROM dirty_scopes
             WHERE fork_id NOT IN (SELECT fork_id FROM radar_keep)",
            [],
        )?;
        reconcile_conflicts(&transaction, &self.family_id, claims)?;
        transaction.execute(
            "DELETE FROM universes
             WHERE fork_id NOT IN (SELECT fork_id FROM radar_keep)",
            [],
        )?;
        transaction.commit()?;
        Ok(())
    }

    pub fn observe(
        &mut self,
        store: &ObjectStore,
        registration: UniverseRegistration<'_>,
    ) -> anyhow::Result<bool> {
        validate_id(registration.fork_id, "fork")?;
        let current: Option<(Vec<u8>, Vec<u8>)> = self
            .connection
            .query_row(
                "SELECT base_snapshot, head_snapshot FROM universes WHERE fork_id = ?1",
                params![registration.fork_id],
                |row| Ok((row.get(0)?, row.get(1)?)),
            )
            .optional()?;
        if current.as_ref().is_some_and(|(base, head)| {
            base.as_slice() == registration.base_snapshot
                && head.as_slice() == registration.head_snapshot
        }) {
            self.connection.execute(
                "UPDATE universes SET name = ?2, workspace_id = ?3, updated_at = ?4
                 WHERE fork_id = ?1",
                params![
                    registration.fork_id,
                    registration.name,
                    registration.workspace_id,
                    now()?
                ],
            )?;
            return Ok(false);
        }

        let base: Snapshot =
            store.read_struct(&registration.base_snapshot, ObjectKind::Snapshot)?;
        let head: Snapshot =
            store.read_struct(&registration.head_snapshot, ObjectKind::Snapshot)?;
        let previous = current
            .as_ref()
            .and_then(|(stored_base, stored_head)| {
                (stored_base.as_slice() == registration.base_snapshot)
                    .then(|| object_id(stored_head))
            })
            .transpose()?;
        let previous_root = previous
            .map(|id| store.read_struct::<Snapshot>(&id, ObjectKind::Snapshot))
            .transpose()?
            .map(|snapshot| snapshot.root_tree);

        let transaction = self.connection.transaction()?;
        transaction.execute(
            "INSERT INTO universes(
                fork_id, name, workspace_id, base_snapshot, head_snapshot, updated_at
             ) VALUES(?1, ?2, ?3, ?4, ?5, ?6)
             ON CONFLICT(fork_id) DO UPDATE SET
                name=excluded.name,
                workspace_id=excluded.workspace_id,
                base_snapshot=excluded.base_snapshot,
                head_snapshot=excluded.head_snapshot,
                updated_at=excluded.updated_at",
            params![
                registration.fork_id,
                registration.name,
                registration.workspace_id,
                registration.base_snapshot.as_slice(),
                registration.head_snapshot.as_slice(),
                now()?
            ],
        )?;

        if previous_root.is_none() {
            transaction.execute(
                "DELETE FROM dirty_scopes WHERE fork_id = ?1",
                params![registration.fork_id],
            )?;
        }
        let comparison_root = previous_root.unwrap_or(base.root_tree);
        update_dirty_scopes(
            store,
            &transaction,
            registration.fork_id,
            &base.root_tree,
            &head.root_tree,
            &comparison_root,
        )?;
        transaction.commit()?;
        Ok(true)
    }

    pub fn reconcile(&mut self, claims: &[ClaimRecord]) -> anyhow::Result<()> {
        let transaction = self.connection.transaction()?;
        reconcile_conflicts(&transaction, &self.family_id, claims)?;
        transaction.commit()?;
        Ok(())
    }

    pub fn summaries(&self) -> anyhow::Result<BTreeMap<String, ForkConflictSummary>> {
        let mut summaries = BTreeMap::new();
        let mut statement = self.connection.prepare(
            "SELECT u.fork_id, m.path
             FROM active_members m
             JOIN universes u ON u.fork_id = m.fork_id
             ORDER BY u.name, m.path",
        )?;
        let mut rows = statement.query([])?;
        while let Some(row) = rows.next()? {
            let fork_id: String = row.get(0)?;
            let path: Vec<u8> = row.get(1)?;
            let summary = summaries
                .entry(fork_id)
                .or_insert_with(ForkConflictSummary::default);
            summary.count = summary.count.saturating_add(1);
            if summary.paths.len() < 20 {
                summary.paths.push(path);
            }
        }
        Ok(summaries)
    }

    pub fn updated_at(&self) -> anyhow::Result<BTreeMap<String, i64>> {
        let mut statement = self
            .connection
            .prepare("SELECT fork_id, updated_at FROM universes ORDER BY fork_id")?;
        let rows = statement.query_map([], |row| Ok((row.get(0)?, row.get(1)?)))?;
        Ok(rows.collect::<Result<BTreeMap<_, _>, _>>()?)
    }

    pub fn events_after(&self, after: Option<&str>, limit: usize) -> anyhow::Result<EventPage> {
        anyhow::ensure!((1..=1000).contains(&limit), "event limit must be 1..=1000");
        let cursor_supplied = after.is_some();
        let after_sequence = after
            .map(|cursor| self.parse_cursor(cursor))
            .transpose()?
            .unwrap_or(0);
        let (earliest, latest): (Option<i64>, Option<i64>) = self.connection.query_row(
            "SELECT MIN(sequence), MAX(sequence) FROM events",
            [],
            |row| Ok((row.get(0)?, row.get(1)?)),
        )?;
        let cursor_found = !cursor_supplied
            || (earliest.is_some()
                && after_sequence >= earliest.unwrap_or(1).saturating_sub(1)
                && after_sequence <= latest.unwrap_or(0));
        let mut statement = self.connection.prepare(
            "SELECT sequence, event_id, kind, state, conflict_id, path,
                    forks_json, claim_state, created_at
             FROM events WHERE sequence > ?1 ORDER BY sequence LIMIT ?2",
        )?;
        let rows = statement.query_map(params![after_sequence, limit as i64], |row| {
            let sequence: i64 = row.get(0)?;
            let path: Vec<u8> = row.get(5)?;
            let forks_json: Vec<u8> = row.get(6)?;
            let forks = serde_json::from_slice(&forks_json).map_err(|error| {
                rusqlite::Error::FromSqlConversionFailure(
                    6,
                    rusqlite::types::Type::Blob,
                    Box::new(error),
                )
            })?;
            Ok(RadarEvent {
                schema: 1,
                cursor: format!("{}:{sequence}", self.family_id),
                event_id: row.get(1)?,
                event_type: row.get(2)?,
                state: row.get(3)?,
                conflict_id: row.get(4)?,
                path: EventPath {
                    display: display_path(&path),
                    bytes_hex: hex::encode(path),
                },
                forks,
                claim_state: row.get(7)?,
                occurred_at: row.get(8)?,
            })
        })?;
        Ok(EventPage {
            cursor_found,
            earliest_cursor: earliest.map(|sequence| format!("{}:{sequence}", self.family_id)),
            latest_cursor: latest.map(|sequence| format!("{}:{sequence}", self.family_id)),
            events: rows.collect::<Result<Vec<_>, _>>()?,
        })
    }

    fn parse_cursor(&self, cursor: &str) -> anyhow::Result<i64> {
        let (stream, sequence) = cursor
            .rsplit_once(':')
            .context("event cursor must be <family-id>:<sequence>")?;
        anyhow::ensure!(
            stream == self.family_id,
            "event cursor belongs to another family"
        );
        let sequence = sequence
            .parse::<i64>()
            .context("invalid event cursor sequence")?;
        anyhow::ensure!(sequence >= 0, "event cursor sequence cannot be negative");
        Ok(sequence)
    }
}

fn update_dirty_scopes(
    store: &ObjectStore,
    transaction: &Transaction<'_>,
    fork_id: &str,
    base_root: &ObjectId,
    head_root: &ObjectId,
    comparison_root: &ObjectId,
) -> anyhow::Result<()> {
    visit_changed_scopes(
        store,
        Some(*comparison_root),
        Some(*head_root),
        Vec::new(),
        &mut |path| recompute_scope(store, transaction, fork_id, base_root, head_root, path),
    )
}

fn visit_changed_scopes<F>(
    store: &ObjectStore,
    before: Option<ObjectId>,
    after: Option<ObjectId>,
    prefix: Vec<u8>,
    visitor: &mut F,
) -> anyhow::Result<()>
where
    F: FnMut(&[u8]) -> anyhow::Result<()>,
{
    match (before, after) {
        (Some(before), Some(after)) => {
            tree::diff_entries(store, &before, &after, &mut |left, right| {
                let entry = left
                    .as_ref()
                    .or(right.as_ref())
                    .context("missing tree entry")?;
                let path = join_path(&prefix, &entry.name);
                let left_tree = left
                    .as_ref()
                    .filter(|entry| entry.kind == EntryKind::Directory)
                    .and_then(|entry| entry.target);
                let right_tree = right
                    .as_ref()
                    .filter(|entry| entry.kind == EntryKind::Directory)
                    .and_then(|entry| entry.target);
                if left_tree.is_some() && right_tree.is_some() {
                    visitor(&path)?;
                    visit_changed_scopes(store, left_tree, right_tree, path, visitor)
                } else {
                    visitor(&path)
                }
            })
        }
        _ => anyhow::bail!("radar roots must both exist"),
    }
}

fn recompute_scope(
    store: &ObjectStore,
    transaction: &Transaction<'_>,
    fork_id: &str,
    base_root: &ObjectId,
    head_root: &ObjectId,
    path: &[u8],
) -> anyhow::Result<()> {
    if ignored_path(path) {
        remove_scope(transaction, fork_id, path, true)?;
        return Ok(());
    }
    let base = tree::find_path(store, base_root, path)?;
    let head = tree::find_path(store, head_root, path)?;
    let structural = base
        .as_ref()
        .is_some_and(|entry| entry.kind == EntryKind::Directory)
        != head
            .as_ref()
            .is_some_and(|entry| entry.kind == EntryKind::Directory);
    remove_scope(transaction, fork_id, path, structural)?;

    match (&base, &head) {
        (Some(left), Some(right))
            if left.kind == EntryKind::Directory && right.kind == EntryKind::Directory =>
        {
            if !directory_metadata_equal(left, right) && relevant(right.class) {
                insert_scope(transaction, fork_id, path, EXACT, "modify")?;
            }
        }
        (Some(left), None) if left.kind == EntryKind::Directory => {
            if relevant(left.class) {
                insert_scope(transaction, fork_id, path, SUBTREE, "delete")?;
            }
        }
        (None, Some(right)) if right.kind == EntryKind::Directory => {
            insert_added_subtree(store, transaction, fork_id, path, right)?;
        }
        (Some(left), Some(right)) if structural => {
            if relevant(left.class) || relevant(right.class) {
                insert_scope(transaction, fork_id, path, SUBTREE, "modify")?;
            }
        }
        (Some(left), Some(right)) if entry_semantically_equal(left, right) => {}
        (Some(left), Some(right)) => {
            if relevant(left.class) || relevant(right.class) {
                insert_scope(transaction, fork_id, path, EXACT, "modify")?;
            }
        }
        (Some(left), None) => {
            if relevant(left.class) {
                insert_scope(transaction, fork_id, path, EXACT, "delete")?;
            }
        }
        (None, Some(right)) => {
            if relevant(right.class) {
                insert_scope(transaction, fork_id, path, EXACT, "add")?;
            }
        }
        (None, None) => {}
    }
    Ok(())
}

fn insert_added_subtree(
    store: &ObjectStore,
    transaction: &Transaction<'_>,
    fork_id: &str,
    path: &[u8],
    directory: &TreeEntry,
) -> anyhow::Result<()> {
    let root = directory.target.context("added directory has no tree")?;
    let mut leaves = 0_u64;
    visit_subtree(store, &root, path.to_vec(), &mut |child_path, entry| {
        if entry.kind != EntryKind::Directory && relevant(entry.class) && !ignored_path(child_path)
        {
            leaves = leaves.saturating_add(1);
            insert_scope(transaction, fork_id, child_path, EXACT, "add")?;
        }
        Ok(())
    })?;
    if leaves == 0 && relevant(directory.class) {
        insert_scope(transaction, fork_id, path, EXACT, "add")?;
    }
    Ok(())
}

fn visit_subtree<F>(
    store: &ObjectStore,
    root: &ObjectId,
    prefix: Vec<u8>,
    visitor: &mut F,
) -> anyhow::Result<()>
where
    F: FnMut(&[u8], &TreeEntry) -> anyhow::Result<()>,
{
    tree::for_each_entry(store, root, |entry| {
        let path = join_path(&prefix, &entry.name);
        visitor(&path, &entry)?;
        if entry.kind == EntryKind::Directory {
            let child = entry.target.context("directory has no tree")?;
            visit_subtree(store, &child, path, visitor)?;
        }
        Ok(())
    })
}

fn remove_scope(
    transaction: &Transaction<'_>,
    fork_id: &str,
    path: &[u8],
    descendants: bool,
) -> anyhow::Result<()> {
    if descendants {
        transaction.execute(
            "DELETE FROM dirty_scopes WHERE fork_id = ?1 AND
             (path = ?2 OR (substr(path, 1, length(?2)) = ?2 AND
                            substr(path, length(?2) + 1, 1) = x'2f'))",
            params![fork_id, path],
        )?;
    } else {
        transaction.execute(
            "DELETE FROM dirty_scopes WHERE fork_id = ?1 AND path = ?2",
            params![fork_id, path],
        )?;
    }
    Ok(())
}

fn insert_scope(
    transaction: &Transaction<'_>,
    fork_id: &str,
    path: &[u8],
    scope: i64,
    action: &str,
) -> anyhow::Result<()> {
    transaction.execute(
        "INSERT INTO dirty_scopes(fork_id, path, scope, action) VALUES(?1, ?2, ?3, ?4)
         ON CONFLICT(fork_id, path, scope) DO UPDATE SET action=excluded.action",
        params![fork_id, path, scope, action],
    )?;
    Ok(())
}

fn reconcile_conflicts(
    transaction: &Transaction<'_>,
    family_id: &str,
    claims: &[ClaimRecord],
) -> anyhow::Result<()> {
    transaction.execute_batch(
        "DROP TABLE IF EXISTS temp.radar_pairs;
         DROP TABLE IF EXISTS temp.radar_next;
         DROP TABLE IF EXISTS temp.radar_next_members;
         CREATE TEMP TABLE radar_pairs(path BLOB, left_id TEXT, right_id TEXT);
         INSERT INTO radar_pairs(path, left_id, right_id)
         SELECT CASE
                  WHEN d1.scope = 1 AND d2.scope = 1 AND length(d1.path) <= length(d2.path)
                    THEN d1.path
                  WHEN d1.scope = 1 THEN d1.path
                  WHEN d2.scope = 1 THEN d2.path
                  ELSE d1.path
                END,
                d1.fork_id, d2.fork_id
         FROM dirty_scopes d1 JOIN dirty_scopes d2 ON d1.fork_id < d2.fork_id
         WHERE
           (d1.scope = 0 AND d2.scope = 0 AND d1.path = d2.path)
           OR (d1.scope = 1 AND
               (d2.path = d1.path OR
                (substr(d2.path, 1, length(d1.path)) = d1.path AND
                 substr(d2.path, length(d1.path) + 1, 1) = x'2f')))
           OR (d2.scope = 1 AND
               (d1.path = d2.path OR
                (substr(d1.path, 1, length(d2.path)) = d2.path AND
                 substr(d1.path, length(d2.path) + 1, 1) = x'2f')));
         CREATE TEMP TABLE radar_next(
           path BLOB PRIMARY KEY, signature TEXT, claim_state TEXT
         ) WITHOUT ROWID;
         INSERT INTO radar_next(path, signature, claim_state)
         SELECT path, '', 'unclaimed' FROM radar_pairs GROUP BY path;
         DELETE FROM radar_next AS child WHERE EXISTS(
           SELECT 1 FROM radar_next parent
           WHERE length(parent.path) < length(child.path)
             AND substr(child.path, 1, length(parent.path)) = parent.path
             AND substr(child.path, length(parent.path) + 1, 1) = x'2f'
         );
         CREATE TEMP TABLE radar_next_members(
           path BLOB NOT NULL, fork_id TEXT NOT NULL,
           PRIMARY KEY(path, fork_id)
         ) WITHOUT ROWID;
         INSERT OR IGNORE INTO radar_next_members(path, fork_id)
           SELECT candidate.path, pairs.left_id
           FROM radar_next candidate JOIN radar_pairs pairs ON pairs.path = candidate.path;
         INSERT OR IGNORE INTO radar_next_members(path, fork_id)
           SELECT candidate.path, pairs.right_id
           FROM radar_next candidate JOIN radar_pairs pairs ON pairs.path = candidate.path;",
    )?;

    let paths = {
        let mut statement = transaction.prepare(
            "SELECT path FROM radar_next
             UNION SELECT path FROM active_conflicts
             ORDER BY path",
        )?;
        let mapped = statement.query_map([], |row| row.get::<_, Vec<u8>>(0))?;
        mapped.collect::<Result<Vec<_>, _>>()?
    };
    for path in paths {
        let next_forks = event_forks(transaction, "radar_next_members", &path)?;
        let old_forks = event_forks(transaction, "active_members", &path)?;
        let next_signature = fork_signature(&next_forks);
        let old_signature = fork_signature(&old_forks);
        let next_claim_state = if next_forks.is_empty() {
            "unclaimed".to_owned()
        } else {
            claim_state(&path, claims)
        };
        let old_claim_state: String = transaction
            .query_row(
                "SELECT claim_state FROM active_conflicts WHERE path = ?1",
                params![&path],
                |row| row.get(0),
            )
            .optional()?
            .unwrap_or_else(|| "unclaimed".to_owned());
        if !next_forks.is_empty() {
            transaction.execute(
                "UPDATE radar_next SET signature = ?2, claim_state = ?3 WHERE path = ?1",
                params![&path, &next_signature, &next_claim_state],
            )?;
        }
        if next_signature == old_signature && next_claim_state == old_claim_state {
            continue;
        }
        let (state, forks, event_claim_state) = match (old_forks.is_empty(), next_forks.is_empty())
        {
            (true, false) => ("opened", next_forks, next_claim_state),
            (false, true) => ("resolved", old_forks, old_claim_state),
            (false, false) => ("updated", next_forks, next_claim_state),
            (true, true) => continue,
        };
        insert_event(
            transaction,
            family_id,
            &path,
            state,
            &forks,
            &event_claim_state,
        )?;
    }
    transaction.execute_batch(
        "DELETE FROM active_members;
         DELETE FROM active_conflicts;
         INSERT INTO active_conflicts(path, signature, claim_state, opened_at)
            SELECT path, signature, claim_state, unixepoch() FROM radar_next;
         INSERT INTO active_members(path, fork_id)
            SELECT path, fork_id FROM radar_next_members;",
    )?;
    transaction.execute(
        "DELETE FROM events WHERE sequence <=
         COALESCE((SELECT MAX(sequence) - ?1 FROM events), 0)",
        params![EVENT_LIMIT],
    )?;
    Ok(())
}

fn event_forks(
    transaction: &Transaction<'_>,
    table: &str,
    path: &[u8],
) -> anyhow::Result<Vec<EventFork>> {
    anyhow::ensure!(
        matches!(table, "radar_next_members" | "active_members"),
        "invalid radar member table"
    );
    let sql = format!(
        "SELECT u.fork_id, u.name, u.head_snapshot
         FROM {table} m JOIN universes u ON u.fork_id = m.fork_id
         WHERE m.path = ?1 ORDER BY u.fork_id"
    );
    let mut statement = transaction.prepare(&sql)?;
    let rows = statement.query_map(params![path], |row| {
        let head: Vec<u8> = row.get(2)?;
        Ok(EventFork {
            fork_id: row.get(0)?,
            name: row.get(1)?,
            head_snapshot: hex::encode(head),
        })
    })?;
    Ok(rows.collect::<Result<Vec<_>, _>>()?)
}

fn insert_event(
    transaction: &Transaction<'_>,
    family_id: &str,
    path: &[u8],
    state: &str,
    forks: &[EventFork],
    claim_state: &str,
) -> anyhow::Result<()> {
    let conflict_id = digest_id(b"agit:radar-conflict:v1\0", &[family_id.as_bytes(), path]);
    let signature = fork_signature(forks);
    let event_id = digest_id(
        b"agit:radar-event:v1\0",
        &[
            conflict_id.as_bytes(),
            state.as_bytes(),
            signature.as_bytes(),
            claim_state.as_bytes(),
        ],
    );
    transaction.execute(
        "INSERT OR IGNORE INTO events(
            event_id, kind, state, conflict_id, path, forks_json, claim_state, created_at
         ) VALUES(?1, 'fork_conflict', ?2, ?3, ?4, ?5, ?6, ?7)",
        params![
            event_id,
            state,
            conflict_id,
            path,
            serde_json::to_vec(forks)?,
            claim_state,
            now()?
        ],
    )?;
    Ok(())
}

fn claim_state(path: &[u8], claims: &[ClaimRecord]) -> String {
    let mut workspaces = claims
        .iter()
        .filter(|claim| crate::claims::matches_path(&claim.pattern, path))
        .map(|claim| claim.workspace_id.as_str())
        .collect::<Vec<_>>();
    workspaces.sort_unstable();
    workspaces.dedup();
    match workspaces.len() {
        0 => "unclaimed",
        1 => "covered",
        _ => "contested",
    }
    .to_owned()
}

fn fork_signature(forks: &[EventFork]) -> String {
    forks
        .iter()
        .map(|fork| format!("{}@{}", fork.fork_id, fork.head_snapshot))
        .collect::<Vec<_>>()
        .join(",")
}

fn digest_id(domain: &[u8], parts: &[&[u8]]) -> String {
    let mut hasher = blake3::Hasher::new();
    hasher.update(domain);
    for part in parts {
        hasher.update(part);
        hasher.update(b"\0");
    }
    hex::encode(&hasher.finalize().as_bytes()[..16])
}

fn object_id(bytes: &[u8]) -> anyhow::Result<ObjectId> {
    anyhow::ensure!(bytes.len() == 32, "invalid stored snapshot ID");
    let mut id = [0_u8; 32];
    id.copy_from_slice(bytes);
    Ok(id)
}

fn validate_id(value: &str, kind: &str) -> anyhow::Result<()> {
    anyhow::ensure!(
        value.len() == 32 && value.bytes().all(|byte| byte.is_ascii_hexdigit()),
        "invalid {kind} ID"
    );
    Ok(())
}

fn join_path(prefix: &[u8], name: &[u8]) -> Vec<u8> {
    let mut path = Vec::with_capacity(prefix.len() + usize::from(!prefix.is_empty()) + name.len());
    path.extend_from_slice(prefix);
    if !path.is_empty() {
        path.push(b'/');
    }
    path.extend_from_slice(name);
    path
}

fn directory_metadata_equal(left: &TreeEntry, right: &TreeEntry) -> bool {
    left.kind == right.kind
        && left.name == right.name
        && left.mode == right.mode
        && left.xattrs == right.xattrs
        && left.class == right.class
}

fn entry_semantically_equal(left: &TreeEntry, right: &TreeEntry) -> bool {
    left.kind == right.kind
        && left.name == right.name
        && left.target == right.target
        && left.link_target == right.link_target
        && left.mode == right.mode
        && left.size == right.size
        && left.xattrs == right.xattrs
        && left.class == right.class
}

fn relevant(class: ContentClass) -> bool {
    matches!(
        class,
        ContentClass::Source
            | ContentClass::ConfigSecret
            | ContentClass::Database
            | ContentClass::Lockfile
    )
}

fn ignored_path(path: &[u8]) -> bool {
    path == b".agit" || path.starts_with(b".agit/") || path == b".git" || path.starts_with(b".git/")
}

fn display_path(path: &[u8]) -> String {
    String::from_utf8_lossy(path).into_owned()
}

fn now() -> anyhow::Result<i64> {
    Ok(SystemTime::now()
        .duration_since(UNIX_EPOCH)
        .context("clock is before Unix epoch")?
        .as_secs() as i64)
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn event_pages_preserve_raw_paths_and_reject_wrong_or_expired_cursors() {
        let temporary = tempfile::tempdir().unwrap();
        let family = "a".repeat(32);
        let radar = Radar::open(temporary.path(), &family).unwrap();
        let raw_path = [b'n', 0xff, b'\n'];
        radar
            .connection
            .execute(
                "INSERT INTO events(
                    event_id, kind, state, conflict_id, path, forks_json,
                    claim_state, created_at
                 ) VALUES('event-1', 'fork_conflict', 'opened', 'conflict-1',
                          ?1, x'5b5d', 'unclaimed', 1)",
                params![raw_path.as_slice()],
            )
            .unwrap();
        let page = radar.events_after(None, 100).unwrap();
        assert!(page.cursor_found);
        assert_eq!(page.events[0].path.bytes_hex, hex::encode(raw_path));
        assert!(page.events[0].path.display.contains('\u{fffd}'));
        assert!(radar
            .events_after(Some(&format!("{}:1", "b".repeat(32))), 100)
            .is_err());
        assert!(
            !radar
                .events_after(Some(&format!("{family}:2")), 100)
                .unwrap()
                .cursor_found
        );

        radar
            .connection
            .execute("DELETE FROM events WHERE sequence = 1", [])
            .unwrap();
        radar
            .connection
            .execute(
                "INSERT INTO events(
                    event_id, kind, state, conflict_id, path, forks_json,
                    claim_state, created_at
                 ) VALUES('event-2', 'fork_conflict', 'opened', 'conflict-2',
                          x'62', x'5b5d', 'unclaimed', 2)",
                [],
            )
            .unwrap();
        assert!(
            !radar
                .events_after(Some(&format!("{family}:0")), 100)
                .unwrap()
                .cursor_found
        );
    }
}
