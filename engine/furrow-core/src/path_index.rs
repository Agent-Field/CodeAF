//! Rebuildable disk-backed path state for delta-only directory sealing.

use crate::model::{EntryKind, ObjectId, TreeEntry};
use anyhow::Context;
use rusqlite::config::DbConfig;
use rusqlite::{params, Connection, OptionalExtension};
use std::path::Path;
use std::time::Duration;

pub const CHILD_BATCH: usize = 512;

pub struct PathIndex {
    connection: Connection,
    transaction_active: bool,
}

#[derive(Debug, Clone, Copy, Default)]
pub struct PathUsage {
    pub files: u64,
    pub directories: u64,
    pub symlinks: u64,
    pub fifos: u64,
    pub special: u64,
    pub logical_bytes: u64,
}

impl PathIndex {
    pub fn open(path: &Path) -> anyhow::Result<Self> {
        let connection = Connection::open(path)?;
        // The index is rebuildable, so closing it need not pay two syncs to
        // fold its log back into the database: the log is folded in when it
        // grows, and every open reads through it.
        connection.set_db_config(DbConfig::SQLITE_DBCONFIG_NO_CKPT_ON_CLOSE, true)?;
        connection.execute_batch(
            "PRAGMA journal_mode=WAL;
             PRAGMA synchronous=NORMAL;
             PRAGMA temp_store=MEMORY;",
        )?;
        crate::sqlite_adapter::fold_oversized_wal(&connection, path)?;
        connection.execute_batch(
            "CREATE TABLE IF NOT EXISTS entries (
                path BLOB PRIMARY KEY,
                parent BLOB NOT NULL,
                name BLOB NOT NULL,
                entry BLOB NOT NULL
             ) WITHOUT ROWID;
             CREATE INDEX IF NOT EXISTS entries_parent_name
                ON entries(parent, name);
             CREATE TABLE IF NOT EXISTS links (
                path BLOB PRIMARY KEY,
                device INTEGER NOT NULL,
                inode INTEGER NOT NULL
             ) WITHOUT ROWID;
             CREATE INDEX IF NOT EXISTS links_identity ON links(device, inode);
             CREATE TABLE IF NOT EXISTS state (
                singleton INTEGER PRIMARY KEY CHECK(singleton = 1),
                root_tree BLOB NOT NULL,
                generation TEXT NOT NULL
             );",
        )?;
        let has_generation: bool = connection.query_row(
            "SELECT EXISTS(
                SELECT 1 FROM pragma_table_info('state') WHERE name = 'generation'
             )",
            [],
            |row| row.get(0),
        )?;
        if !has_generation {
            connection.execute(
                "ALTER TABLE state ADD COLUMN generation TEXT NOT NULL DEFAULT ''",
                [],
            )?;
        }
        Ok(Self {
            connection,
            transaction_active: false,
        })
    }

    pub fn begin(&mut self) -> anyhow::Result<()> {
        anyhow::ensure!(!self.transaction_active, "path-index transaction is active");
        self.connection.execute_batch("BEGIN IMMEDIATE")?;
        self.transaction_active = true;
        Ok(())
    }

    pub fn backup_to(&self, path: &Path) -> anyhow::Result<()> {
        let mut destination = Connection::open(path)?;
        let backup = rusqlite::backup::Backup::new(&self.connection, &mut destination)?;
        backup.run_to_completion(256, Duration::from_millis(1), None)?;
        Ok(())
    }

    pub fn commit(&mut self) -> anyhow::Result<()> {
        anyhow::ensure!(
            self.transaction_active,
            "path-index transaction is not active"
        );
        self.connection.execute_batch("COMMIT")?;
        self.transaction_active = false;
        Ok(())
    }

    pub fn rollback(&mut self) -> anyhow::Result<()> {
        if self.transaction_active {
            self.connection.execute_batch("ROLLBACK")?;
            self.transaction_active = false;
        }
        Ok(())
    }

    pub fn reset(&self) -> anyhow::Result<()> {
        self.connection.execute("DELETE FROM entries", [])?;
        self.connection.execute("DELETE FROM links", [])?;
        self.connection.execute("DELETE FROM state", [])?;
        Ok(())
    }

    pub fn upsert(&self, path: &[u8], parent: &[u8], entry: &TreeEntry) -> anyhow::Result<()> {
        self.connection.execute(
            "INSERT INTO entries(path, parent, name, entry) VALUES(?1, ?2, ?3, ?4)
             ON CONFLICT(path) DO UPDATE SET
                parent=excluded.parent,
                name=excluded.name,
                entry=excluded.entry",
            params![path, parent, &entry.name, serde_json::to_vec(entry)?],
        )?;
        Ok(())
    }

    pub fn entry(&self, path: &[u8]) -> anyhow::Result<Option<TreeEntry>> {
        let bytes: Option<Vec<u8>> = self
            .connection
            .query_row(
                "SELECT entry FROM entries WHERE path = ?1",
                params![path],
                |row| row.get(0),
            )
            .optional()?;
        bytes
            .map(|bytes| serde_json::from_slice(&bytes).context("decode indexed tree entry"))
            .transpose()
    }

    pub fn remove_subtree(&self, path: &[u8]) -> anyhow::Result<u64> {
        const DESCENDANTS: &str = "WITH RECURSIVE descendants(path) AS (
                SELECT path FROM entries WHERE path = ?1
                UNION ALL
                SELECT child.path
                FROM entries child JOIN descendants parent ON child.parent = parent.path
             )";
        // The link rows go first, while the entries that name the subtree exist.
        self.connection.execute(
            &format!("{DESCENDANTS} DELETE FROM links WHERE path IN descendants"),
            params![path],
        )?;
        let removed = self.connection.execute(
            &format!("{DESCENDANTS} DELETE FROM entries WHERE path IN descendants"),
            params![path],
        )?;
        Ok(removed as u64)
    }

    /// Notes which file `path` is on disk when it shares that file with another
    /// directory entry (`identity` is its device and inode), and forgets the
    /// note when it does not. Two paths are one file when both notes agree.
    pub fn note_link(&self, path: &[u8], identity: Option<(u64, u64)>) -> anyhow::Result<()> {
        match identity {
            Some((device, inode)) => self.connection.execute(
                "INSERT OR REPLACE INTO links(path, device, inode) VALUES(?1, ?2, ?3)",
                params![path, device as i64, inode as i64],
            )?,
            None => self
                .connection
                .execute("DELETE FROM links WHERE path = ?1", params![path])?,
        };
        Ok(())
    }

    /// The device and inode noted for `path`, when it shares its file.
    pub fn link_identity(&self, path: &[u8]) -> anyhow::Result<Option<(u64, u64)>> {
        Ok(self
            .connection
            .query_row(
                "SELECT device, inode FROM links WHERE path = ?1",
                params![path],
                |row| Ok((row.get::<_, i64>(0)? as u64, row.get::<_, i64>(1)? as u64)),
            )
            .optional()?)
    }

    /// True when `path` shares its file with something and no other path of
    /// the index is known to be that file. After an incremental capture this
    /// means its siblings were not looked at, because nothing told us they
    /// changed when the link was made.
    pub fn link_has_no_known_sibling(&self, path: &[u8]) -> anyhow::Result<bool> {
        Ok(self.connection.query_row(
            "SELECT EXISTS(SELECT 1 FROM links mine WHERE mine.path = ?1)
                AND NOT EXISTS(
                    SELECT 1 FROM links mine JOIN links other
                      ON other.device = mine.device AND other.inode = mine.inode
                     AND other.path <> mine.path
                    WHERE mine.path = ?1)",
            params![path],
            |row| row.get(0),
        )?)
    }

    /// The paths that share a file with another path of the index, as sets in
    /// bytewise order of their first path. A file linked to something outside
    /// the tree (the store, another folder) is alone here and is not a group.
    pub fn link_groups(&self) -> anyhow::Result<Vec<Vec<Vec<u8>>>> {
        let mut statement = self.connection.prepare(
            "SELECT device, inode, path FROM links
             WHERE (device, inode) IN (
                SELECT device, inode FROM links GROUP BY device, inode HAVING COUNT(*) > 1)
             ORDER BY device, inode, path",
        )?;
        let rows = statement.query_map([], |row| {
            Ok((
                (row.get::<_, i64>(0)?, row.get::<_, i64>(1)?),
                row.get::<_, Vec<u8>>(2)?,
            ))
        })?;
        let mut groups: Vec<Vec<Vec<u8>>> = Vec::new();
        let mut last = None;
        for row in rows {
            let (identity, path) = row?;
            match last.replace(identity) {
                Some(previous) if previous == identity => {
                    groups.last_mut().expect("a group is open").push(path)
                }
                _ => groups.push(vec![path]),
            }
        }
        groups.sort();
        Ok(groups)
    }

    pub fn remove(&self, path: &[u8]) -> anyhow::Result<bool> {
        self.connection
            .execute("DELETE FROM links WHERE path = ?1", params![path])?;
        Ok(self
            .connection
            .execute("DELETE FROM entries WHERE path = ?1", params![path])?
            != 0)
    }

    pub fn children_after(
        &self,
        parent: &[u8],
        after_name: Option<&[u8]>,
        limit: usize,
    ) -> anyhow::Result<Vec<TreeEntry>> {
        let after_name = after_name.unwrap_or_default();
        let mut statement = self.connection.prepare(
            "SELECT entry FROM entries
             WHERE parent = ?1 AND name > ?2
             ORDER BY name LIMIT ?3",
        )?;
        let rows = statement.query_map(params![parent, after_name, limit as i64], |row| {
            row.get::<_, Vec<u8>>(0)
        })?;
        rows.map(|row| {
            serde_json::from_slice(&row?).map_err(|error| {
                rusqlite::Error::FromSqlConversionFailure(
                    0,
                    rusqlite::types::Type::Blob,
                    Box::new(error),
                )
            })
        })
        .collect::<Result<Vec<_>, _>>()
        .map_err(Into::into)
    }

    pub fn entries_after(
        &self,
        after_path: Option<&[u8]>,
        limit: usize,
    ) -> anyhow::Result<Vec<(Vec<u8>, TreeEntry)>> {
        let after_path = after_path.unwrap_or_default();
        let mut statement = self.connection.prepare(
            "SELECT path, entry FROM entries
             WHERE path > ?1 ORDER BY path LIMIT ?2",
        )?;
        let rows = statement.query_map(params![after_path, limit as i64], |row| {
            Ok((row.get::<_, Vec<u8>>(0)?, row.get::<_, Vec<u8>>(1)?))
        })?;
        let mut entries = Vec::new();
        for row in rows {
            let (path, entry) = row?;
            entries.push((
                path,
                serde_json::from_slice(&entry).context("decode indexed tree entry")?,
            ));
        }
        Ok(entries)
    }

    pub fn set_root(&self, root: &ObjectId, generation: &str) -> anyhow::Result<()> {
        self.connection.execute(
            "INSERT INTO state(singleton, root_tree, generation) VALUES(1, ?1, ?2)
             ON CONFLICT(singleton) DO UPDATE SET
                root_tree=excluded.root_tree,
                generation=excluded.generation",
            params![root.as_slice(), generation],
        )?;
        Ok(())
    }

    pub fn root(&self, generation: &str) -> anyhow::Result<Option<ObjectId>> {
        let bytes: Option<Vec<u8>> = self
            .connection
            .query_row(
                "SELECT root_tree FROM state WHERE singleton = 1 AND generation = ?1",
                params![generation],
                |row| row.get(0),
            )
            .optional()?;
        bytes
            .map(|bytes| {
                anyhow::ensure!(bytes.len() == 32, "invalid path-index root ID");
                let mut id = [0; 32];
                id.copy_from_slice(&bytes);
                Ok(id)
            })
            .transpose()
    }

    pub fn count(&self) -> anyhow::Result<u64> {
        Ok(self
            .connection
            .query_row("SELECT COUNT(*) FROM entries", [], |row| {
                row.get::<_, i64>(0)
            })? as u64)
    }

    pub fn usage(&self) -> anyhow::Result<PathUsage> {
        let mut statement = self.connection.prepare("SELECT entry FROM entries")?;
        let mut rows = statement.query([])?;
        let mut usage = PathUsage {
            directories: 1,
            ..PathUsage::default()
        };
        while let Some(row) = rows.next()? {
            let bytes: Vec<u8> = row.get(0)?;
            let entry: TreeEntry =
                serde_json::from_slice(&bytes).context("decode indexed tree entry")?;
            match entry.kind {
                EntryKind::File => {
                    usage.files = usage.files.saturating_add(1);
                    usage.logical_bytes = usage.logical_bytes.saturating_add(entry.size);
                }
                EntryKind::Directory => usage.directories = usage.directories.saturating_add(1),
                EntryKind::Symlink => usage.symlinks = usage.symlinks.saturating_add(1),
                EntryKind::Fifo => usage.fifos = usage.fifos.saturating_add(1),
                EntryKind::SocketMarker => usage.special = usage.special.saturating_add(1),
            }
        }
        Ok(usage)
    }
}

impl Drop for PathIndex {
    fn drop(&mut self) {
        if self.transaction_active {
            let _ = self.connection.execute_batch("ROLLBACK");
        }
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    use crate::model::EntryKind;

    fn entry(name: &[u8], target: u8) -> TreeEntry {
        TreeEntry {
            name: name.to_vec(),
            kind: EntryKind::File,
            target: Some([target; 32]),
            link_target: Vec::new(),
            mode: 0o100644,
            size: 1,
            mtime_secs: 0,
            mtime_nanos: 0,
            xattrs: None,
            class: Default::default(),
        }
    }

    #[test]
    fn batches_children_and_removes_descendants_transactionally() {
        let temporary = tempfile::tempdir().unwrap();
        let mut index = PathIndex::open(&temporary.path().join("paths.sqlite3")).unwrap();
        index.begin().unwrap();
        let directory = TreeEntry {
            name: b"dir".to_vec(),
            kind: EntryKind::Directory,
            target: Some([1; 32]),
            link_target: Vec::new(),
            mode: 0o40755,
            size: 0,
            mtime_secs: 0,
            mtime_nanos: 0,
            xattrs: None,
            class: Default::default(),
        };
        index.upsert(b"dir", b"", &directory).unwrap();
        index.upsert(b"dir/a", b"dir", &entry(b"a", 2)).unwrap();
        index.upsert(b"dir/b", b"dir", &entry(b"b", 3)).unwrap();
        index.set_root(&[9; 32], "pack-one").unwrap();
        index.commit().unwrap();

        let usage = index.usage().unwrap();
        assert_eq!(usage.files, 2);
        assert_eq!(usage.directories, 2);
        assert_eq!(usage.logical_bytes, 2);

        let first = index.children_after(b"dir", None, 1).unwrap();
        assert_eq!(first[0].name, b"a");
        let second = index
            .children_after(b"dir", Some(&first[0].name), CHILD_BATCH)
            .unwrap();
        assert_eq!(second[0].name, b"b");
        assert_eq!(index.root("pack-one").unwrap(), Some([9; 32]));
        assert_eq!(index.root("pack-two").unwrap(), None);

        index.begin().unwrap();
        assert_eq!(index.remove_subtree(b"dir").unwrap(), 3);
        index.rollback().unwrap();
        assert_eq!(index.count().unwrap(), 3);
        index.begin().unwrap();
        assert_eq!(index.remove_subtree(b"dir").unwrap(), 3);
        index.commit().unwrap();
        assert_eq!(index.count().unwrap(), 0);
    }
}
