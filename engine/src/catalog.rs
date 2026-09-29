use crate::model::{ObjectId, ObjectKind};
use anyhow::Context;
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
            CREATE TABLE IF NOT EXISTS file_cache (
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
        self.conn
            .query_row(
                "SELECT device, inode, size, mtime_secs, mtime_nanos,
                        ctime_secs, ctime_nanos, mode, blob_id
                 FROM file_cache WHERE workspace_id = ?1 AND path = ?2",
                params![workspace_id, path],
                |row| {
                    let id: Vec<u8> = row.get(8)?;
                    Ok((
                        row.get::<_, i64>(0)? as u64,
                        row.get::<_, i64>(1)? as u64,
                        row.get::<_, i64>(2)? as u64,
                        row.get(3)?,
                        row.get::<_, i64>(4)?,
                        row.get(5)?,
                        row.get::<_, i64>(6)?,
                        row.get::<_, i64>(7)? as u32,
                        id,
                    ))
                },
            )
            .optional()?
            .map(
                |(
                    device,
                    inode,
                    size,
                    mtime_secs,
                    mtime_nanos,
                    ctime_secs,
                    ctime_nanos,
                    mode,
                    id,
                )| {
                    Ok(CachedFile {
                        device,
                        inode,
                        size,
                        mtime_secs,
                        mtime_nanos,
                        ctime_secs,
                        ctime_nanos,
                        mode,
                        blob_id: vec_to_id(id)?,
                    })
                },
            )
            .transpose()
    }

    pub fn cached_files(
        &self,
        workspace_id: &str,
        paths: &[Vec<u8>],
    ) -> anyhow::Result<Vec<Option<CachedFile>>> {
        let mut statement = self.conn.prepare(
            "SELECT device, inode, size, mtime_secs, mtime_nanos,
                    ctime_secs, ctime_nanos, mode, blob_id
             FROM file_cache WHERE workspace_id = ?1 AND path = ?2",
        )?;
        let mut result = Vec::with_capacity(paths.len());
        for path in paths {
            let raw = statement
                .query_row(params![workspace_id, path], |row| {
                    Ok((
                        row.get::<_, i64>(0)? as u64,
                        row.get::<_, i64>(1)? as u64,
                        row.get::<_, i64>(2)? as u64,
                        row.get(3)?,
                        row.get::<_, i64>(4)?,
                        row.get(5)?,
                        row.get::<_, i64>(6)?,
                        row.get::<_, i64>(7)? as u32,
                        row.get::<_, Vec<u8>>(8)?,
                    ))
                })
                .optional()?;
            result.push(
                raw.map(
                    |(
                        device,
                        inode,
                        size,
                        mtime_secs,
                        mtime_nanos,
                        ctime_secs,
                        ctime_nanos,
                        mode,
                        id,
                    )| {
                        Ok::<CachedFile, anyhow::Error>(CachedFile {
                            device,
                            inode,
                            size,
                            mtime_secs,
                            mtime_nanos,
                            ctime_secs,
                            ctime_nanos,
                            mode,
                            blob_id: vec_to_id(id)?,
                        })
                    },
                )
                .transpose()?,
            );
        }
        Ok(result)
    }

    pub fn cache_file(
        &self,
        workspace_id: &str,
        path: &[u8],
        file: &CachedFile,
    ) -> anyhow::Result<()> {
        self.conn.execute(
            "INSERT INTO file_cache(
                workspace_id, path, device, inode, size, mtime_secs, mtime_nanos,
                ctime_secs, ctime_nanos, mode, blob_id
             ) VALUES(?1, ?2, ?3, ?4, ?5, ?6, ?7, ?8, ?9, ?10, ?11)
             ON CONFLICT(workspace_id, path) DO UPDATE SET
                device=excluded.device, inode=excluded.inode, size=excluded.size,
                mtime_secs=excluded.mtime_secs, mtime_nanos=excluded.mtime_nanos,
                ctime_secs=excluded.ctime_secs, ctime_nanos=excluded.ctime_nanos,
                mode=excluded.mode, blob_id=excluded.blob_id",
            params![
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
            ],
        )?;
        Ok(())
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
            "DELETE FROM file_cache WHERE workspace_id = ?1",
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
            "DELETE FROM file_cache WHERE workspace_id = ?1",
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
}

fn vec_to_id(bytes: Vec<u8>) -> anyhow::Result<ObjectId> {
    anyhow::ensure!(bytes.len() == 32, "catalog contains an invalid object ID");
    let mut id = [0_u8; 32];
    id.copy_from_slice(&bytes);
    Ok(id)
}
