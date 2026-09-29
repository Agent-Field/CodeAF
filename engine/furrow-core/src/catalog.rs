use crate::model::{ObjectId, ObjectKind};
use anyhow::Context;
use rusqlite::config::DbConfig;
use rusqlite::{params, Connection, OptionalExtension};
use std::path::Path;

#[derive(Debug, Clone)]
pub struct ObjectLocation {
    pub pack: String,
    pub offset: u64,
    pub len: u64,
    pub kind: ObjectKind,
}

#[derive(Debug, Clone)]
pub struct PackCheckpoint {
    pub verified_len: u64,
    pub object_count: u64,
    pub last_object: Option<ObjectId>,
    pub last_record_start: u64,
}

pub struct Catalog {
    conn: Connection,
}

impl Catalog {
    pub fn open(path: &Path) -> anyhow::Result<Self> {
        let conn =
            Connection::open(path).with_context(|| format!("open catalog {}", path.display()))?;
        conn.pragma_update(None, "journal_mode", "WAL")?;
        conn.pragma_update(None, "synchronous", "FULL")?;
        // Every commit is already durable in the log, so closing need not pay
        // two more syncs to copy it back into the database file; the log is
        // folded in as it grows.
        conn.set_db_config(DbConfig::SQLITE_DBCONFIG_NO_CKPT_ON_CLOSE, true)?;
        conn.execute_batch(
            "
            CREATE TABLE IF NOT EXISTS objects (
                id BLOB PRIMARY KEY,
                kind INTEGER NOT NULL,
                pack TEXT NOT NULL,
                offset INTEGER NOT NULL,
                len INTEGER NOT NULL
            ) WITHOUT ROWID;
            CREATE TABLE IF NOT EXISTS workspaces (
                id TEXT PRIMARY KEY,
                root BLOB NOT NULL UNIQUE,
                head BLOB
            );
            CREATE TABLE IF NOT EXISTS timeline (
                workspace_id TEXT NOT NULL,
                sequence INTEGER PRIMARY KEY AUTOINCREMENT,
                snapshot_id BLOB NOT NULL UNIQUE,
                sealed_at INTEGER NOT NULL,
                label TEXT,
                trigger TEXT NOT NULL
            );
            CREATE INDEX IF NOT EXISTS timeline_workspace_sequence
                ON timeline(workspace_id, sequence DESC);
            DROP TABLE IF EXISTS file_cache;
            CREATE TABLE IF NOT EXISTS stat_cache (
                workspace_id TEXT NOT NULL,
                path BLOB NOT NULL,
                device INTEGER NOT NULL,
                inode INTEGER NOT NULL,
                size INTEGER NOT NULL,
                mtime_secs INTEGER NOT NULL,
                mtime_nanos INTEGER NOT NULL,
                ctime_secs INTEGER NOT NULL,
                ctime_nanos INTEGER NOT NULL,
                mode INTEGER NOT NULL,
                blob_id BLOB NOT NULL,
                xattrs BLOB,
                sqlite INTEGER NOT NULL,
                cached_at_nanos INTEGER NOT NULL,
                PRIMARY KEY(workspace_id, path)
            ) WITHOUT ROWID;
            CREATE TABLE IF NOT EXISTS directory_cache (
                workspace_id TEXT NOT NULL,
                path BLOB NOT NULL,
                tree_id BLOB NOT NULL,
                PRIMARY KEY(workspace_id, path)
            ) WITHOUT ROWID;
            CREATE TABLE IF NOT EXISTS pack_checkpoints (
                pack TEXT PRIMARY KEY,
                verified_len INTEGER NOT NULL,
                object_count INTEGER NOT NULL,
                last_object BLOB,
                last_record_start INTEGER NOT NULL
            ) WITHOUT ROWID;
            ",
        )?;
        Ok(Self { conn })
    }

    pub fn cached_file(
        &self,
        workspace_id: &str,
        path: &[u8],
    ) -> anyhow::Result<Option<CachedFile>> {
        let mut statement = self.conn.prepare_cached(SELECT_STAT)?;
        statement
            .query_row(params![workspace_id, path], read_stat_row)
            .optional()?
            .map(CachedFile::try_from)
            .transpose()
    }

    pub fn cached_files(
        &self,
        workspace_id: &str,
        paths: &[Vec<u8>],
    ) -> anyhow::Result<Vec<Option<CachedFile>>> {
        paths
            .iter()
            .map(|path| self.cached_file(workspace_id, path))
            .collect()
    }

    pub fn cache_file(
        &self,
        workspace_id: &str,
        path: &[u8],
        file: &CachedFile,
    ) -> anyhow::Result<()> {
        self.conn.prepare_cached(UPSERT_STAT)?.execute(params![
            workspace_id,
            path,
            file.device as i64,
            file.inode as i64,
            file.size as i64,
            file.mtime_secs,
            file.mtime_nanos,
            file.ctime_secs,
            file.ctime_nanos,
            file.mode,
            file.blob_id.as_slice(),
            file.xattrs.as_ref().map(|id| id.as_slice()),
            file.sqlite,
            file.cached_at_nanos,
        ])?;
        Ok(())
    }

    /// Paths whose cached content is a SQLite database, in path order.
    pub fn sqlite_paths(&self, workspace_id: &str) -> anyhow::Result<Vec<Vec<u8>>> {
        let mut statement = self.conn.prepare_cached(
            "SELECT path FROM stat_cache WHERE workspace_id = ?1 AND sqlite = 1 ORDER BY path",
        )?;
        let rows = statement.query_map(params![workspace_id], |row| row.get(0))?;
        Ok(rows.collect::<Result<_, _>>()?)
    }

    /// Runs `write` as one transaction, so its many small catalog writes cost
    /// one durable commit instead of one each. Inside an open transaction it
    /// simply runs, and the outer commit covers it.
    pub fn batch<T>(&self, write: impl FnOnce() -> anyhow::Result<T>) -> anyhow::Result<T> {
        if !self.conn.is_autocommit() {
            return write();
        }
        self.conn.execute_batch("BEGIN IMMEDIATE")?;
        match write() {
            Ok(value) => {
                self.conn.execute_batch("COMMIT")?;
                Ok(value)
            }
            Err(error) => {
                let _ = self.conn.execute_batch("ROLLBACK");
                Err(error)
            }
        }
    }

    pub fn cached_directory(
        &self,
        workspace_id: &str,
        path: &[u8],
    ) -> anyhow::Result<Option<ObjectId>> {
        let value: Option<Vec<u8>> = self
            .conn
            .query_row(
                "SELECT tree_id FROM directory_cache WHERE workspace_id = ?1 AND path = ?2",
                params![workspace_id, path],
                |row| row.get(0),
            )
            .optional()?;
        value.map(vec_to_id).transpose()
    }

    pub fn cache_directory(
        &self,
        workspace_id: &str,
        path: &[u8],
        tree_id: &ObjectId,
    ) -> anyhow::Result<()> {
        self.conn.execute(
            "INSERT INTO directory_cache(workspace_id, path, tree_id) VALUES(?1, ?2, ?3)
             ON CONFLICT(workspace_id, path) DO UPDATE SET tree_id=excluded.tree_id",
            params![workspace_id, path, tree_id.as_slice()],
        )?;
        Ok(())
    }

    pub fn object(&self, id: &ObjectId) -> anyhow::Result<Option<ObjectLocation>> {
        self.conn
            .query_row(
                "SELECT kind, pack, offset, len FROM objects WHERE id = ?1",
                params![id.as_slice()],
                |row| {
                    let kind_value: u8 = row.get(0)?;
                    let kind = ObjectKind::from_u8(kind_value).ok_or_else(|| {
                        rusqlite::Error::InvalidColumnType(
                            0,
                            "kind".into(),
                            rusqlite::types::Type::Integer,
                        )
                    })?;
                    Ok(ObjectLocation {
                        kind,
                        pack: row.get(1)?,
                        offset: row.get::<_, i64>(2)? as u64,
                        len: row.get::<_, i64>(3)? as u64,
                    })
                },
            )
            .optional()
            .map_err(Into::into)
    }

    pub fn object_count(&self) -> anyhow::Result<u64> {
        Ok(self
            .conn
            .query_row("SELECT COUNT(*) FROM objects", [], |row| {
                row.get::<_, i64>(0)
            })? as u64)
    }

    pub fn object_payload_bytes(&self) -> anyhow::Result<u64> {
        Ok(self
            .conn
            .query_row("SELECT COALESCE(SUM(len), 0) FROM objects", [], |row| {
                row.get::<_, i64>(0)
            })? as u64)
    }

    pub fn remove_workspace(&mut self, workspace_id: &str) -> anyhow::Result<()> {
        let tx = self.conn.transaction()?;
        tx.execute(
            "DELETE FROM stat_cache WHERE workspace_id = ?1",
            params![workspace_id],
        )?;
        tx.execute(
            "DELETE FROM directory_cache WHERE workspace_id = ?1",
            params![workspace_id],
        )?;
        tx.execute(
            "DELETE FROM timeline WHERE workspace_id = ?1",
            params![workspace_id],
        )?;
        tx.execute(
            "DELETE FROM workspaces WHERE id = ?1",
            params![workspace_id],
        )?;
        tx.commit()?;
        Ok(())
    }

    pub fn detach_workspace(&mut self, workspace_id: &str) -> anyhow::Result<()> {
        let tx = self.conn.transaction()?;
        tx.execute(
            "DELETE FROM stat_cache WHERE workspace_id = ?1",
            params![workspace_id],
        )?;
        tx.execute(
            "DELETE FROM directory_cache WHERE workspace_id = ?1",
            params![workspace_id],
        )?;
        tx.execute(
            "DELETE FROM workspaces WHERE id = ?1",
            params![workspace_id],
        )?;
        tx.commit()?;
        Ok(())
    }

    pub fn replace_objects_from_gc(
        &mut self,
        mark_database: &Path,
        pack: &str,
        checkpoint: &PackCheckpoint,
    ) -> anyhow::Result<()> {
        self.conn.execute(
            "ATTACH DATABASE ?1 AS gcmark",
            params![mark_database.as_os_str().to_string_lossy().as_ref()],
        )?;
        let result = (|| {
            let tx = self.conn.transaction()?;
            tx.execute("DELETE FROM objects", [])?;
            tx.execute(
                "INSERT INTO objects(id, kind, pack, offset, len)
                 SELECT id, kind, ?1, offset, len FROM gcmark.locations",
                params![pack],
            )?;
            tx.execute("DELETE FROM pack_checkpoints", [])?;
            tx.execute(
                "INSERT INTO pack_checkpoints(
                    pack, verified_len, object_count, last_object, last_record_start
                 ) VALUES(?1, ?2, ?3, ?4, ?5)",
                params![
                    pack,
                    checkpoint.verified_len as i64,
                    checkpoint.object_count as i64,
                    checkpoint.last_object.as_ref().map(|id| id.as_slice()),
                    checkpoint.last_record_start as i64,
                ],
            )?;
            tx.commit()?;
            Ok::<_, anyhow::Error>(())
        })();
        let detached = self.conn.execute("DETACH DATABASE gcmark", []);
        result?;
        detached?;
        Ok(())
    }

    pub fn pack_checkpoint(&self, pack: &str) -> anyhow::Result<Option<PackCheckpoint>> {
        self.conn
            .query_row(
                "SELECT verified_len, object_count, last_object, last_record_start
                 FROM pack_checkpoints WHERE pack = ?1",
                params![pack],
                |row| {
                    let last_object: Option<Vec<u8>> = row.get(2)?;
                    Ok((
                        row.get::<_, i64>(0)? as u64,
                        row.get::<_, i64>(1)? as u64,
                        last_object,
                        row.get::<_, i64>(3)? as u64,
                    ))
                },
            )
            .optional()?
            .map(
                |(verified_len, object_count, last_object, last_record_start)| {
                    Ok(PackCheckpoint {
                        verified_len,
                        object_count,
                        last_object: last_object.map(vec_to_id).transpose()?,
                        last_record_start,
                    })
                },
            )
            .transpose()
    }

    pub fn set_pack_checkpoint(
        &self,
        pack: &str,
        checkpoint: &PackCheckpoint,
    ) -> anyhow::Result<()> {
        self.conn.execute(
            "INSERT INTO pack_checkpoints(
                pack, verified_len, object_count, last_object, last_record_start
             ) VALUES(?1, ?2, ?3, ?4, ?5)
             ON CONFLICT(pack) DO UPDATE SET
                verified_len=excluded.verified_len,
                object_count=excluded.object_count,
                last_object=excluded.last_object,
                last_record_start=excluded.last_record_start",
            params![
                pack,
                checkpoint.verified_len as i64,
                checkpoint.object_count as i64,
                checkpoint.last_object.as_ref().map(|id| id.as_slice()),
                checkpoint.last_record_start as i64,
            ],
        )?;
        Ok(())
    }

    pub fn pack_prefix_object_count(&self, pack: &str, verified_len: u64) -> anyhow::Result<u64> {
        Ok(self.conn.query_row(
            "SELECT COUNT(*) FROM objects
             WHERE pack = ?1 AND offset + len + 4 <= ?2",
            params![pack, verified_len as i64],
            |row| row.get::<_, i64>(0),
        )? as u64)
    }

    pub fn pack_crossing_object_count(&self, pack: &str, verified_len: u64) -> anyhow::Result<u64> {
        Ok(self.conn.query_row(
            "SELECT COUNT(*) FROM objects
             WHERE pack = ?1 AND offset < ?2 AND offset + len + 4 > ?2",
            params![pack, verified_len as i64],
            |row| row.get::<_, i64>(0),
        )? as u64)
    }

    pub fn reset_pack_index(&mut self, pack: &str) -> anyhow::Result<()> {
        let tx = self.conn.transaction()?;
        tx.execute("DELETE FROM objects WHERE pack = ?1", params![pack])?;
        tx.execute(
            "DELETE FROM pack_checkpoints WHERE pack = ?1",
            params![pack],
        )?;
        tx.commit()?;
        Ok(())
    }

    pub fn insert_object(
        &self,
        id: &ObjectId,
        kind: ObjectKind,
        pack: &str,
        offset: u64,
        len: u64,
    ) -> anyhow::Result<()> {
        self.conn.execute(
            "INSERT OR IGNORE INTO objects(id, kind, pack, offset, len) VALUES(?1, ?2, ?3, ?4, ?5)",
            params![id.as_slice(), kind as u8, pack, offset as i64, len as i64],
        )?;
        Ok(())
    }

    pub fn delete_pack_objects_from(&self, pack: &str, payload_offset: u64) -> anyhow::Result<()> {
        self.conn.execute(
            "DELETE FROM objects WHERE pack = ?1 AND offset >= ?2",
            params![pack, payload_offset as i64],
        )?;
        Ok(())
    }

    pub fn ensure_workspace(&self, id: &str, root: &[u8]) -> anyhow::Result<()> {
        self.conn.execute(
            "INSERT OR IGNORE INTO workspaces(id, root) VALUES(?1, ?2)",
            params![id, root],
        )?;
        Ok(())
    }

    pub fn workspace_head(&self, id: &str) -> anyhow::Result<Option<ObjectId>> {
        let value: Option<Vec<u8>> = self
            .conn
            .query_row(
                "SELECT head FROM workspaces WHERE id = ?1",
                params![id],
                |row| row.get(0),
            )
            .optional()?
            .flatten();
        value.map(vec_to_id).transpose()
    }

    pub fn commit_snapshot(
        &mut self,
        workspace_id: &str,
        snapshot_id: &ObjectId,
        sealed_at: i64,
        label: Option<&str>,
        trigger: &str,
    ) -> anyhow::Result<()> {
        let tx = self.conn.transaction()?;
        tx.execute(
            "INSERT OR IGNORE INTO timeline(workspace_id, snapshot_id, sealed_at, label, trigger)
             VALUES(?1, ?2, ?3, ?4, ?5)",
            params![
                workspace_id,
                snapshot_id.as_slice(),
                sealed_at,
                label,
                trigger
            ],
        )?;
        tx.execute(
            "UPDATE workspaces SET head = ?2 WHERE id = ?1",
            params![workspace_id, snapshot_id.as_slice()],
        )?;
        tx.commit()?;
        Ok(())
    }

    pub fn timeline(&self, workspace_id: &str, limit: usize) -> anyhow::Result<Vec<TimelineRow>> {
        let mut stmt = self.conn.prepare(
            "SELECT snapshot_id, sealed_at, label, trigger FROM timeline
             WHERE workspace_id = ?1 ORDER BY sequence DESC LIMIT ?2",
        )?;
        let rows = stmt.query_map(params![workspace_id, limit as i64], |row| {
            let id: Vec<u8> = row.get(0)?;
            Ok((id, row.get(1)?, row.get(2)?, row.get(3)?))
        })?;
        let mut result = Vec::new();
        for row in rows {
            let (id, sealed_at, label, trigger) = row?;
            result.push(TimelineRow {
                id: vec_to_id(id)?,
                sealed_at,
                label,
                trigger,
            });
        }
        Ok(result)
    }
}

pub struct TimelineRow {
    pub id: ObjectId,
    pub sealed_at: i64,
    pub label: Option<String>,
    pub trigger: String,
}

/// One row of the stat cache: what a file looked like when it was last read,
/// and what reading it produced. A file whose stat still matches (and was not
/// touched within [`RACY_WINDOW_NANOS`] of being read) needs neither open nor
/// read nor xattr calls to be captured again.
#[derive(Debug, Clone)]
pub struct CachedFile {
    pub device: u64,
    pub inode: u64,
    pub size: u64,
    pub mtime_secs: i64,
    pub mtime_nanos: i64,
    pub ctime_secs: i64,
    pub ctime_nanos: i64,
    pub mode: u32,
    pub blob_id: ObjectId,
    pub xattrs: Option<ObjectId>,
    pub sqlite: bool,
    /// Wall clock, in nanoseconds, just before the content was read.
    pub cached_at_nanos: i64,
}

/// Timestamps closer than this to the moment a file was read cannot prove the
/// content unchanged: a same-tick rewrite would leave them equal. Two seconds
/// covers the coarsest filesystem clocks in use.
pub const RACY_WINDOW_NANOS: i64 = 2_000_000_000;

impl CachedFile {
    /// True when a write in the timestamp granularity after the read could
    /// still have left the stat unchanged; such a file is read again.
    pub fn is_racy(&self) -> bool {
        let newest = self.mtime_nanos_total().max(self.ctime_nanos_total());
        newest.saturating_add(RACY_WINDOW_NANOS) >= self.cached_at_nanos
    }

    fn mtime_nanos_total(&self) -> i64 {
        self.mtime_secs
            .saturating_mul(1_000_000_000)
            .saturating_add(self.mtime_nanos)
    }

    fn ctime_nanos_total(&self) -> i64 {
        self.ctime_secs
            .saturating_mul(1_000_000_000)
            .saturating_add(self.ctime_nanos)
    }
}

const SELECT_STAT: &str = "SELECT device, inode, size, mtime_secs, mtime_nanos,
        ctime_secs, ctime_nanos, mode, blob_id, xattrs, sqlite, cached_at_nanos
     FROM stat_cache WHERE workspace_id = ?1 AND path = ?2";

const UPSERT_STAT: &str = "INSERT INTO stat_cache(
        workspace_id, path, device, inode, size, mtime_secs, mtime_nanos,
        ctime_secs, ctime_nanos, mode, blob_id, xattrs, sqlite, cached_at_nanos
     ) VALUES(?1, ?2, ?3, ?4, ?5, ?6, ?7, ?8, ?9, ?10, ?11, ?12, ?13, ?14)
     ON CONFLICT(workspace_id, path) DO UPDATE SET
        device=excluded.device, inode=excluded.inode, size=excluded.size,
        mtime_secs=excluded.mtime_secs, mtime_nanos=excluded.mtime_nanos,
        ctime_secs=excluded.ctime_secs, ctime_nanos=excluded.ctime_nanos,
        mode=excluded.mode, blob_id=excluded.blob_id, xattrs=excluded.xattrs,
        sqlite=excluded.sqlite, cached_at_nanos=excluded.cached_at_nanos";

/// A stat-cache row exactly as SQLite stores it.
struct StatRow {
    file: CachedFile,
    blob: Vec<u8>,
    xattrs: Option<Vec<u8>>,
}

fn read_stat_row(row: &rusqlite::Row<'_>) -> rusqlite::Result<StatRow> {
    Ok(StatRow {
        file: CachedFile {
            device: row.get::<_, i64>(0)? as u64,
            inode: row.get::<_, i64>(1)? as u64,
            size: row.get::<_, i64>(2)? as u64,
            mtime_secs: row.get(3)?,
            mtime_nanos: row.get(4)?,
            ctime_secs: row.get(5)?,
            ctime_nanos: row.get(6)?,
            mode: row.get::<_, i64>(7)? as u32,
            blob_id: [0; 32],
            xattrs: None,
            sqlite: row.get(10)?,
            cached_at_nanos: row.get(11)?,
        },
        blob: row.get(8)?,
        xattrs: row.get(9)?,
    })
}

impl TryFrom<StatRow> for CachedFile {
    type Error = anyhow::Error;

    fn try_from(row: StatRow) -> anyhow::Result<Self> {
        Ok(CachedFile {
            blob_id: vec_to_id(row.blob)?,
            xattrs: row.xattrs.map(vec_to_id).transpose()?,
            ..row.file
        })
    }
}

fn vec_to_id(bytes: Vec<u8>) -> anyhow::Result<ObjectId> {
    anyhow::ensure!(bytes.len() == 32, "catalog contains an invalid object ID");
    let mut id = [0_u8; 32];
    id.copy_from_slice(&bytes);
    Ok(id)
}

#[cfg(test)]
mod tests {
    use super::*;

    fn catalog() -> (tempfile::TempDir, Catalog) {
        let dir = tempfile::tempdir().unwrap();
        let catalog = Catalog::open(&dir.path().join("catalog.sqlite3")).unwrap();
        (dir, catalog)
    }

    fn insert(catalog: &Catalog, byte: u8) -> anyhow::Result<()> {
        catalog.insert_object(&[byte; 32], ObjectKind::Chunk, "pack", 0, 1)
    }

    #[test]
    fn a_batch_commits_everything_or_nothing() {
        let (_dir, catalog) = catalog();
        let failed: anyhow::Result<()> = catalog.batch(|| {
            insert(&catalog, 1)?;
            anyhow::bail!("interrupted")
        });
        assert!(failed.is_err());
        assert!(catalog.object(&[1; 32]).unwrap().is_none());

        catalog
            .batch(|| {
                insert(&catalog, 2)?;
                insert(&catalog, 3)
            })
            .unwrap();
        assert!(catalog.object(&[2; 32]).unwrap().is_some());
        assert!(catalog.object(&[3; 32]).unwrap().is_some());
    }

    #[test]
    fn a_nested_batch_joins_the_outer_transaction() {
        let (_dir, catalog) = catalog();
        let outer: anyhow::Result<()> = catalog.batch(|| {
            catalog.batch(|| insert(&catalog, 4))?;
            anyhow::bail!("outer fails after the inner batch returned")
        });
        assert!(outer.is_err());
        assert!(catalog.object(&[4; 32]).unwrap().is_none());
    }

    #[test]
    fn a_racily_clean_row_is_told_apart_from_a_settled_one() {
        let row = |cached_at_nanos| CachedFile {
            device: 1,
            inode: 1,
            size: 1,
            mtime_secs: 100,
            mtime_nanos: 0,
            ctime_secs: 100,
            ctime_nanos: 0,
            mode: 0o100644,
            blob_id: [0; 32],
            xattrs: None,
            sqlite: false,
            cached_at_nanos,
        };
        assert!(row(100_500_000_000).is_racy());
        assert!(row(99_000_000_000).is_racy());
        assert!(!row(102_000_000_001).is_racy());
    }
}
