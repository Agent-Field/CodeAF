use crate::budget::{self, BudgetStatus};
use crate::catalog::{CachedFile, Catalog, PackCheckpoint, TimelineRow};
use crate::gc::{self, GcReport};
use crate::model::{ObjectId, ObjectKind, SnapshotTrigger};
use crate::refs::{RefLog, RefRecord};
use crate::retention::RetentionLog;
use anyhow::Context;
use fs2::FileExt;
use serde::{de::DeserializeOwned, Serialize};
use std::cell::{Cell, RefCell};
use std::fs::{self, File, OpenOptions};
use std::io::{Read, Seek, SeekFrom, Write};
use std::os::unix::ffi::OsStringExt;
use std::path::{Path, PathBuf};
use std::rc::Rc;

const INITIAL_PACK_NAME: &str = "pack-000001.agp";
const CURRENT_FILE: &str = "CURRENT";
const OBJECT_MAGIC: &[u8; 4] = b"AGOB";
const OBJECT_END: &[u8; 4] = b"AGND";
const OBJECT_VERSION: u8 = 1;
const HEADER_LEN: u64 = 4 + 1 + 1 + 8 + 32 + 32;
const MAX_OBJECT_LEN: u64 = 256 * 1024 * 1024;

pub struct ObjectStore {
    root: PathBuf,
    catalog: Catalog,
    active_pack: RefCell<String>,
    maintenance: Rc<MaintenanceState>,
}

struct MaintenanceState {
    path: PathBuf,
    depth: Cell<u32>,
    exclusive: Cell<bool>,
    file: RefCell<Option<File>>,
}

pub(crate) struct MaintenanceGuard {
    state: Rc<MaintenanceState>,
}

impl Drop for MaintenanceGuard {
    fn drop(&mut self) {
        let depth = self.state.depth.get();
        debug_assert!(depth > 0);
        self.state.depth.set(depth - 1);
        if depth == 1 {
            if let Some(file) = self.state.file.borrow_mut().take() {
                let _ = FileExt::unlock(&file);
            }
            self.state.exclusive.set(false);
        }
    }
}

#[derive(Debug, Clone, Copy, serde::Serialize)]
pub struct StoreStats {
    pub objects: u64,
    pub physical_bytes: u64,
}

impl ObjectStore {
    pub fn open(root: PathBuf) -> anyhow::Result<Self> {
        fs::create_dir_all(root.join("packs"))?;
        fs::create_dir_all(root.join("locks"))?;
        fs::create_dir_all(root.join("workspaces"))?;
        let lock = OpenOptions::new()
            .create(true)
            .truncate(false)
            .read(true)
            .write(true)
            .open(root.join("locks").join("maintenance.lock"))?;
        FileExt::lock_shared(&lock)?;
        let active_pack = read_or_initialize_current(&root)?;
        let catalog = Catalog::open(&root.join("catalog.sqlite3"))?;
        let maintenance = Rc::new(MaintenanceState {
            path: root.join("locks").join("maintenance.lock"),
            depth: Cell::new(1),
            exclusive: Cell::new(false),
            file: RefCell::new(Some(lock)),
        });
        let mut store = Self {
            root,
            catalog,
            active_pack: RefCell::new(active_pack),
            maintenance,
        };
        let startup_guard = MaintenanceGuard {
            state: Rc::clone(&store.maintenance),
        };
        let recovered = store.recover_pack();
        drop(startup_guard);
        recovered?;
        Ok(store)
    }

    pub fn put_bytes(&self, kind: ObjectKind, bytes: &[u8]) -> anyhow::Result<ObjectId> {
        let _maintenance = self.acquire_maintenance_shared()?;
        self.put_bytes_locked(kind, bytes)
    }

    fn put_bytes_locked(&self, kind: ObjectKind, bytes: &[u8]) -> anyhow::Result<ObjectId> {
        anyhow::ensure!(
            bytes.len() as u64 <= MAX_OBJECT_LEN,
            "object exceeds size limit"
        );
        let id = object_id(kind, bytes);
        if self.catalog.object(&id)?.is_some() {
            return Ok(id);
        }

        let lock_path = self.root.join("locks").join("pack.lock");
        let lock = OpenOptions::new()
            .create(true)
            .truncate(false)
            .read(true)
            .write(true)
            .open(lock_path)?;
        lock.lock_exclusive()?;

        if self.catalog.object(&id)?.is_none() {
            let active_pack = self.active_pack.borrow().clone();
            let pack_path = self.root.join("packs").join(&active_pack);
            let mut pack = OpenOptions::new()
                .create(true)
                .append(true)
                .read(true)
                .open(&pack_path)?;
            let record_start = pack.seek(SeekFrom::End(0))?;
            let checksum = blake3::hash(bytes);
            pack.write_all(OBJECT_MAGIC)?;
            pack.write_all(&[OBJECT_VERSION, kind as u8])?;
            pack.write_all(&(bytes.len() as u64).to_le_bytes())?;
            pack.write_all(&id)?;
            pack.write_all(checksum.as_bytes())?;
            let payload_offset = record_start + HEADER_LEN;
            pack.write_all(bytes)?;
            pack.write_all(OBJECT_END)?;
            self.catalog.insert_object(
                &id,
                kind,
                &active_pack,
                payload_offset,
                bytes.len() as u64,
            )?;
        }
        FileExt::unlock(&lock)?;
        Ok(id)
    }

    pub(crate) fn acquire_maintenance_shared(&self) -> anyhow::Result<MaintenanceGuard> {
        self.acquire_maintenance(false)
    }

    pub(crate) fn acquire_maintenance_exclusive(&self) -> anyhow::Result<MaintenanceGuard> {
        self.acquire_maintenance(true)
    }

    fn acquire_maintenance(&self, exclusive: bool) -> anyhow::Result<MaintenanceGuard> {
        let depth = self.maintenance.depth.get();
        if depth > 0 {
            anyhow::ensure!(
                !exclusive || self.maintenance.exclusive.get(),
                "cannot upgrade a shared maintenance section"
            );
            self.maintenance.depth.set(depth + 1);
            return Ok(MaintenanceGuard {
                state: Rc::clone(&self.maintenance),
            });
        }
        let file = OpenOptions::new()
            .create(true)
            .truncate(false)
            .read(true)
            .write(true)
            .open(&self.maintenance.path)?;
        if exclusive {
            file.lock_exclusive()?;
        } else {
            FileExt::lock_shared(&file)?;
        }
        *self.active_pack.borrow_mut() = read_or_initialize_current(&self.root)?;
        self.maintenance.depth.set(1);
        self.maintenance.exclusive.set(exclusive);
        *self.maintenance.file.borrow_mut() = Some(file);
        Ok(MaintenanceGuard {
            state: Rc::clone(&self.maintenance),
        })
    }

    pub fn put_struct<T: Serialize>(
        &self,
        kind: ObjectKind,
        value: &T,
    ) -> anyhow::Result<ObjectId> {
        let bytes = serde_json::to_vec(value)?;
        self.put_bytes(kind, &bytes)
    }

    pub fn read_bytes(&self, id: &ObjectId, expected: ObjectKind) -> anyhow::Result<Vec<u8>> {
        let _maintenance = self.acquire_maintenance_shared()?;
        self.read_bytes_unlocked(id, expected)
    }

    pub(crate) fn read_bytes_unlocked(
        &self,
        id: &ObjectId,
        expected: ObjectKind,
    ) -> anyhow::Result<Vec<u8>> {
        let location = self
            .catalog
            .object(id)?
            .with_context(|| format!("missing object {}", hex::encode(id)))?;
        anyhow::ensure!(location.kind == expected, "object kind mismatch");
        let mut pack = File::open(self.root.join("packs").join(location.pack))?;
        pack.seek(SeekFrom::Start(location.offset))?;
        let mut bytes = vec![0_u8; location.len as usize];
        pack.read_exact(&mut bytes)?;
        anyhow::ensure!(
            object_id(expected, &bytes) == *id,
            "object integrity check failed"
        );
        Ok(bytes)
    }

    pub fn read_struct<T: DeserializeOwned>(
        &self,
        id: &ObjectId,
        expected: ObjectKind,
    ) -> anyhow::Result<T> {
        Ok(serde_json::from_slice(&self.read_bytes(id, expected)?)?)
    }

    pub(crate) fn contains_object(&self, id: &ObjectId) -> anyhow::Result<bool> {
        let _maintenance = self.acquire_maintenance_shared()?;
        Ok(self.catalog.object(id)?.is_some())
    }

    pub fn ensure_workspace(&mut self, id: &str, root: &[u8]) -> anyhow::Result<()> {
        let workspace_dir = self.root.join("workspaces").join(id);
        fs::create_dir_all(&workspace_dir)?;
        let metadata_path = workspace_dir.join("root.path");
        if !metadata_path.exists() {
            let temporary = workspace_dir.join("root.path.tmp");
            let mut file = OpenOptions::new()
                .create(true)
                .truncate(true)
                .write(true)
                .open(&temporary)?;
            file.write_all(root)?;
            file.sync_all()?;
            fs::rename(&temporary, &metadata_path)?;
            File::open(&workspace_dir)?.sync_all()?;
        }
        self.catalog.ensure_workspace(id, root)?;
        let detached = workspace_dir.join("detached");
        if detached.exists() {
            fs::remove_file(detached)?;
            File::open(&workspace_dir)?.sync_all()?;
        }
        let refs = RefLog::open(&self.root, id)?;
        let ref_head = refs.head()?.map(|record| record.snapshot_id);
        if self.catalog.workspace_head(id)? != ref_head {
            for record in refs.records()? {
                self.index_ref_record(id, &record)?;
            }
        }
        Ok(())
    }

    pub fn find_workspace(&self, root: &[u8]) -> anyhow::Result<Option<String>> {
        for entry in fs::read_dir(self.root.join("workspaces"))? {
            let entry = entry?;
            if !entry.file_type()?.is_dir() {
                continue;
            }
            if entry.path().join("detached").exists() {
                continue;
            }
            let metadata_path = entry.path().join("root.path");
            if metadata_path.exists() && fs::read(metadata_path)? == root {
                return Ok(Some(entry.file_name().to_string_lossy().into_owned()));
            }
        }
        Ok(None)
    }

    pub fn workspace_head(&self, id: &str) -> anyhow::Result<Option<ObjectId>> {
        Ok(RefLog::open(&self.root, id)?
            .head()?
            .map(|record| record.snapshot_id))
    }

    pub fn detach_workspace(&mut self, id: &str) -> anyhow::Result<()> {
        let workspace_dir = self.workspace_data_dir(id);
        fs::create_dir_all(&workspace_dir)?;
        let marker = workspace_dir.join("detached");
        let file = OpenOptions::new()
            .create(true)
            .truncate(true)
            .write(true)
            .open(marker)?;
        file.sync_all()?;
        File::open(workspace_dir)?.sync_all()?;
        self.catalog.detach_workspace(id)?;
        Ok(())
    }

    pub fn purge_workspace(&mut self, id: &str) -> anyhow::Result<()> {
        self.catalog.remove_workspace(id)?;
        let workspace_dir = self.workspace_data_dir(id);
        if workspace_dir.exists() {
            fs::remove_dir_all(&workspace_dir)?;
            File::open(self.root.join("workspaces"))?.sync_all()?;
        }
        Ok(())
    }

    pub fn timeline(&self, id: &str, limit: usize) -> anyhow::Result<Vec<TimelineRow>> {
        let refs = RefLog::open(&self.root, id)?;
        let Some(head) = refs.head()? else {
            return Ok(Vec::new());
        };
        let state = RetentionLog::open(&self.root, id)?.state()?;
        let sequences = state.recent_sequences(head.sequence, limit);
        Ok(refs
            .records_at_sequences(&sequences)?
            .into_iter()
            .map(|record| TimelineRow {
                id: record.snapshot_id,
                sealed_at: record.sealed_at,
                label: record.label,
                trigger: trigger_name(&record.trigger).to_owned(),
            })
            .collect())
    }

    pub fn retained_snapshot_count(&self, id: &str) -> anyhow::Result<u64> {
        let refs = RefLog::open(&self.root, id)?;
        let head = refs.head()?.map_or(0, |record| record.sequence);
        Ok(RetentionLog::open(&self.root, id)?
            .state()?
            .retained_count(head))
    }

    pub fn pinned_snapshots(
        &self,
        workspace_id: &str,
    ) -> anyhow::Result<std::collections::BTreeSet<ObjectId>> {
        Ok(RetentionLog::open(&self.root, workspace_id)?
            .state()?
            .pins
            .into_iter()
            .map(|pin| pin.snapshot_id)
            .collect())
    }

    pub fn pin_snapshot(&self, workspace_id: &str, snapshot_id: ObjectId) -> anyhow::Result<bool> {
        let _maintenance = self.acquire_maintenance_exclusive()?;
        anyhow::ensure!(
            self.catalog.object(&snapshot_id)?.is_some(),
            "snapshot bytes are no longer available to pin"
        );
        let refs = RefLog::open(&self.root, workspace_id)?;
        RetentionLog::open(&self.root, workspace_id)?.pin(&refs, snapshot_id)
    }

    pub fn unpin_snapshot(
        &self,
        workspace_id: &str,
        snapshot_id: &ObjectId,
    ) -> anyhow::Result<bool> {
        let _maintenance = self.acquire_maintenance_exclusive()?;
        RetentionLog::open(&self.root, workspace_id)?.unpin(snapshot_id)
    }

    pub fn cached_file(
        &self,
        workspace_id: &str,
        path: &[u8],
    ) -> anyhow::Result<Option<CachedFile>> {
        self.catalog.cached_file(workspace_id, path)
    }

    pub fn cached_files(
        &self,
        workspace_id: &str,
        paths: &[Vec<u8>],
    ) -> anyhow::Result<Vec<Option<CachedFile>>> {
        self.catalog.cached_files(workspace_id, paths)
    }

    pub fn cache_file(
        &self,
        workspace_id: &str,
        path: &[u8],
        file: &CachedFile,
    ) -> anyhow::Result<()> {
        self.catalog.cache_file(workspace_id, path, file)
    }

    pub fn cached_directory(
        &self,
        workspace_id: &str,
        path: &[u8],
    ) -> anyhow::Result<Option<ObjectId>> {
        self.catalog.cached_directory(workspace_id, path)
    }

    pub fn cache_directory(
        &self,
        workspace_id: &str,
        path: &[u8],
        tree_id: &ObjectId,
    ) -> anyhow::Result<()> {
        self.catalog.cache_directory(workspace_id, path, tree_id)
    }

    pub fn publish_snapshot(
        &mut self,
        workspace_id: &str,
        snapshot_id: ObjectId,
        sealed_at: i64,
        label: Option<String>,
        trigger: SnapshotTrigger,
    ) -> anyhow::Result<()> {
        // The append is the visibility boundary. Every referenced object was
        // sync'd before this call; SQLite is an advisory index rebuilt from it.
        self.sync_active_pack()?;
        let record = RefLog::open(&self.root, workspace_id)?.append(
            snapshot_id,
            sealed_at,
            label,
            trigger,
        )?;
        self.index_ref_record(workspace_id, &record)?;
        if let Err(error) = self.enforce_budget(false) {
            eprintln!("warning: automatic store-budget enforcement failed: {error:#}");
        }
        Ok(())
    }

    pub fn budget_status(&self) -> anyhow::Result<BudgetStatus> {
        let config = budget::load(&self.root)?;
        budget::status(&self.root, config, self.stats()?.physical_bytes)
    }

    pub fn configure_budget(
        &mut self,
        max_store_bytes: Option<u64>,
        reserved_free_bytes: Option<u64>,
    ) -> anyhow::Result<BudgetStatus> {
        let _maintenance = self.acquire_maintenance_exclusive()?;
        let mut config = budget::load(&self.root)?;
        if let Some(max_store_bytes) = max_store_bytes {
            config.max_store_bytes = max_store_bytes;
        }
        if let Some(reserved_free_bytes) = reserved_free_bytes {
            config.reserved_free_bytes = reserved_free_bytes;
        }
        budget::save(&self.root, config)?;
        self.enforce_budget(true)?;
        self.budget_status()
    }

    pub fn enforce_budget(&mut self, force: bool) -> anyhow::Result<Option<GcReport>> {
        let before = self.budget_status()?;
        if !before.pressured() {
            budget::record_attempt(&self.root, before.physical_bytes, true)?;
            return Ok(None);
        }
        if !force && !budget::should_retry(&self.root, before.physical_bytes)? {
            return Ok(None);
        }
        let report = gc::collect(self, false)?;
        let after = self.budget_status()?;
        budget::record_attempt(&self.root, after.physical_bytes, after.satisfied)?;
        Ok(Some(report))
    }

    pub fn root(&self) -> &Path {
        &self.root
    }

    pub fn workspace_data_dir(&self, workspace_id: &str) -> PathBuf {
        self.root.join("workspaces").join(workspace_id)
    }

    pub(crate) fn workspace_roots(&self) -> anyhow::Result<Vec<(String, PathBuf)>> {
        let mut roots = Vec::new();
        for entry in fs::read_dir(self.root.join("workspaces"))? {
            let entry = entry?;
            if !entry.file_type()?.is_dir() || entry.path().join("detached").exists() {
                continue;
            }
            let path = entry.path().join("root.path");
            if !path.is_file() {
                continue;
            }
            roots.push((
                entry.file_name().to_string_lossy().into_owned(),
                PathBuf::from(std::ffi::OsString::from_vec(fs::read(path)?)),
            ));
        }
        Ok(roots)
    }

    pub fn stats(&self) -> anyhow::Result<StoreStats> {
        let _maintenance = self.acquire_maintenance_shared()?;
        self.stats_unlocked()
    }

    pub(crate) fn stats_unlocked(&self) -> anyhow::Result<StoreStats> {
        let mut physical_bytes = 0_u64;
        for entry in fs::read_dir(self.root.join("packs"))? {
            let entry = entry?;
            if entry.file_type()?.is_file()
                && entry.path().extension() == Some(std::ffi::OsStr::new("agp"))
            {
                physical_bytes += entry.metadata()?.len();
            }
        }
        Ok(StoreStats {
            objects: self.catalog.object_count()?,
            physical_bytes,
        })
    }

    pub(crate) fn object_payload_bytes(&self) -> anyhow::Result<u64> {
        self.catalog.object_payload_bytes()
    }

    pub(crate) fn object_len(&self, id: &ObjectId) -> anyhow::Result<u64> {
        self.catalog
            .object(id)?
            .map(|location| location.len)
            .with_context(|| format!("missing object {}", hex::encode(id)))
    }

    pub(crate) fn object_kind(&self, id: &ObjectId) -> anyhow::Result<ObjectKind> {
        self.catalog
            .object(id)?
            .map(|location| location.kind)
            .with_context(|| format!("missing object {}", hex::encode(id)))
    }

    pub(crate) fn replace_objects_from_gc(
        &mut self,
        mark_database: &Path,
        pack: &str,
        checkpoint: &PackCheckpoint,
    ) -> anyhow::Result<()> {
        self.catalog
            .replace_objects_from_gc(mark_database, pack, checkpoint)
    }

    pub(crate) fn activate_pack(&mut self, pack: &str) -> anyhow::Result<()> {
        validate_pack_name(pack)?;
        let packs = self.root.join("packs");
        let temporary = packs.join("CURRENT.tmp");
        let mut file = OpenOptions::new()
            .create(true)
            .truncate(true)
            .write(true)
            .open(&temporary)?;
        writeln!(file, "{pack}")?;
        file.sync_all()?;
        fs::rename(temporary, packs.join(CURRENT_FILE))?;
        File::open(&packs)?.sync_all()?;
        *self.active_pack.borrow_mut() = pack.to_owned();
        Ok(())
    }

    fn index_ref_record(&mut self, workspace_id: &str, record: &RefRecord) -> anyhow::Result<()> {
        self.catalog.commit_snapshot(
            workspace_id,
            &record.snapshot_id,
            record.sealed_at,
            record.label.as_deref(),
            trigger_name(&record.trigger),
        )
    }

    fn sync_active_pack(&self) -> anyhow::Result<()> {
        let path = self
            .root
            .join("packs")
            .join(self.active_pack.borrow().as_str());
        if path.exists() {
            File::open(path)?.sync_data()?;
        }
        Ok(())
    }

    fn recover_pack(&mut self) -> anyhow::Result<()> {
        let pack_name = self.active_pack.borrow().clone();
        let pack_path = self.root.join("packs").join(&pack_name);
        if !pack_path.exists() {
            return Ok(());
        }
        let lock = OpenOptions::new()
            .create(true)
            .truncate(false)
            .read(true)
            .write(true)
            .open(self.root.join("locks").join("pack.lock"))?;
        lock.lock_exclusive()?;
        let mut pack = OpenOptions::new().read(true).write(true).open(&pack_path)?;
        let file_len = pack.metadata()?.len();
        let checkpoint = self.catalog.pack_checkpoint(&pack_name)?;
        let checkpoint_valid = checkpoint
            .as_ref()
            .map(|checkpoint| self.validate_checkpoint(&mut pack, file_len, checkpoint))
            .transpose()?
            .unwrap_or(false);
        if !checkpoint_valid {
            self.catalog.reset_pack_index(&pack_name)?;
        }

        let mut offset = checkpoint
            .as_ref()
            .filter(|_| checkpoint_valid)
            .map_or(0, |checkpoint| checkpoint.verified_len);
        let mut object_count = checkpoint
            .as_ref()
            .filter(|_| checkpoint_valid)
            .map_or(0, |checkpoint| checkpoint.object_count);
        let mut last_object = checkpoint
            .as_ref()
            .filter(|_| checkpoint_valid)
            .and_then(|checkpoint| checkpoint.last_object);
        let mut last_record_start = checkpoint
            .as_ref()
            .filter(|_| checkpoint_valid)
            .map_or(0, |checkpoint| checkpoint.last_record_start);
        pack.seek(SeekFrom::Start(offset))?;
        let mut buffer = vec![0_u8; 1024 * 1024];
        while offset < file_len {
            let record_start = offset;
            let mut magic = [0_u8; 4];
            if pack.read_exact(&mut magic).is_err() {
                self.truncate_pack_tail(&pack, record_start)?;
                break;
            }
            anyhow::ensure!(
                &magic == OBJECT_MAGIC,
                "object pack corruption at byte {record_start}"
            );
            let mut meta = [0_u8; 2];
            if pack.read_exact(&mut meta).is_err() {
                self.truncate_pack_tail(&pack, record_start)?;
                break;
            }
            anyhow::ensure!(meta[0] == OBJECT_VERSION, "unsupported object pack version");
            let kind = ObjectKind::from_u8(meta[1]).context("invalid object kind in pack")?;
            let mut len_bytes = [0_u8; 8];
            if pack.read_exact(&mut len_bytes).is_err() {
                self.truncate_pack_tail(&pack, record_start)?;
                break;
            }
            let len = u64::from_le_bytes(len_bytes);
            anyhow::ensure!(
                len <= MAX_OBJECT_LEN,
                "object pack record exceeds size limit"
            );
            let mut id = [0_u8; 32];
            let mut checksum = [0_u8; 32];
            if pack.read_exact(&mut id).is_err() || pack.read_exact(&mut checksum).is_err() {
                self.truncate_pack_tail(&pack, record_start)?;
                break;
            }
            let payload_offset = record_start + HEADER_LEN;
            if file_len.saturating_sub(payload_offset) < len + OBJECT_END.len() as u64 {
                self.truncate_pack_tail(&pack, record_start)?;
                break;
            }
            let mut remaining = len;
            let mut content_hasher = blake3::Hasher::new();
            content_hasher.update(kind.domain());
            let mut checksum_hasher = blake3::Hasher::new();
            while remaining > 0 {
                let take = remaining.min(buffer.len() as u64) as usize;
                pack.read_exact(&mut buffer[..take])?;
                content_hasher.update(&buffer[..take]);
                checksum_hasher.update(&buffer[..take]);
                remaining -= take as u64;
            }
            let mut end = [0_u8; 4];
            pack.read_exact(&mut end)?;
            anyhow::ensure!(&end == OBJECT_END, "object pack trailer mismatch");
            anyhow::ensure!(
                checksum_hasher.finalize().as_bytes() == &checksum,
                "object payload checksum mismatch"
            );
            anyhow::ensure!(
                content_hasher.finalize().as_bytes() == &id,
                "object ID mismatch"
            );
            self.catalog
                .insert_object(&id, kind, &pack_name, payload_offset, len)?;
            offset = pack.stream_position()?;
            object_count += 1;
            last_object = Some(id);
            last_record_start = record_start;
        }
        pack.sync_data()?;
        self.catalog.set_pack_checkpoint(
            &pack_name,
            &PackCheckpoint {
                verified_len: offset,
                object_count,
                last_object,
                last_record_start,
            },
        )?;
        FileExt::unlock(&lock)?;
        Ok(())
    }

    fn validate_checkpoint(
        &self,
        pack: &mut File,
        file_len: u64,
        checkpoint: &PackCheckpoint,
    ) -> anyhow::Result<bool> {
        if checkpoint.verified_len > file_len
            || self.catalog.pack_prefix_object_count(
                self.active_pack.borrow().as_str(),
                checkpoint.verified_len,
            )? != checkpoint.object_count
            || self.catalog.pack_crossing_object_count(
                self.active_pack.borrow().as_str(),
                checkpoint.verified_len,
            )? != 0
        {
            return Ok(false);
        }
        if checkpoint.object_count == 0 {
            return Ok(checkpoint.verified_len == 0 && checkpoint.last_object.is_none());
        }
        let Some(last_id) = checkpoint.last_object else {
            return Ok(false);
        };
        let Some(location) = self.catalog.object(&last_id)? else {
            return Ok(false);
        };
        if location.pack != *self.active_pack.borrow()
            || location.offset != checkpoint.last_record_start + HEADER_LEN
            || location.offset + location.len + OBJECT_END.len() as u64 != checkpoint.verified_len
        {
            return Ok(false);
        }

        pack.seek(SeekFrom::Start(checkpoint.last_record_start))?;
        let mut header = [0_u8; HEADER_LEN as usize];
        if pack.read_exact(&mut header).is_err()
            || &header[..4] != OBJECT_MAGIC
            || header[4] != OBJECT_VERSION
            || ObjectKind::from_u8(header[5]) != Some(location.kind)
        {
            return Ok(false);
        }
        let mut len_bytes = [0_u8; 8];
        len_bytes.copy_from_slice(&header[6..14]);
        if u64::from_le_bytes(len_bytes) != location.len || header[14..46] != last_id {
            return Ok(false);
        }
        pack.seek(SeekFrom::Start(checkpoint.verified_len - 4))?;
        let mut trailer = [0_u8; 4];
        Ok(pack.read_exact(&mut trailer).is_ok() && &trailer == OBJECT_END)
    }

    fn truncate_pack_tail(&self, pack: &File, record_start: u64) -> anyhow::Result<()> {
        pack.set_len(record_start)?;
        self.catalog.delete_pack_objects_from(
            self.active_pack.borrow().as_str(),
            record_start.saturating_add(HEADER_LEN),
        )?;
        Ok(())
    }
}

fn read_or_initialize_current(root: &Path) -> anyhow::Result<String> {
    let packs = root.join("packs");
    let current = packs.join(CURRENT_FILE);
    if current.exists() {
        let pack = fs::read_to_string(&current)?.trim().to_owned();
        validate_pack_name(&pack)?;
        return Ok(pack);
    }
    let temporary = packs.join("CURRENT.tmp");
    let mut file = OpenOptions::new()
        .create(true)
        .truncate(true)
        .write(true)
        .open(&temporary)?;
    writeln!(file, "{INITIAL_PACK_NAME}")?;
    file.sync_all()?;
    fs::rename(temporary, current)?;
    File::open(packs)?.sync_all()?;
    Ok(INITIAL_PACK_NAME.to_owned())
}

fn validate_pack_name(pack: &str) -> anyhow::Result<()> {
    anyhow::ensure!(
        !pack.is_empty()
            && pack.ends_with(".agp")
            && Path::new(pack).file_name() == Some(std::ffi::OsStr::new(pack)),
        "invalid active pack name"
    );
    Ok(())
}

fn trigger_name(trigger: &SnapshotTrigger) -> &'static str {
    match trigger {
        SnapshotTrigger::Initial => "initial",
        SnapshotTrigger::Manual => "manual",
        SnapshotTrigger::Watcher => "watcher",
        SnapshotTrigger::PreRewind => "pre_rewind",
        SnapshotTrigger::ForkBase => "fork_base",
        SnapshotTrigger::AgentRun => "agent_run",
        SnapshotTrigger::MergeSource => "merge_source",
        SnapshotTrigger::PreMerge => "pre_merge",
        SnapshotTrigger::Merge => "merge",
        SnapshotTrigger::SyncLocal => "sync_local",
        SnapshotTrigger::SyncPush => "sync_push",
        SnapshotTrigger::SyncPull => "sync_pull",
        SnapshotTrigger::Inspection => "inspection",
        SnapshotTrigger::Claim => "claim",
        SnapshotTrigger::Release => "release",
        SnapshotTrigger::Coord => "coord",
    }
}

pub fn object_id(kind: ObjectKind, bytes: &[u8]) -> ObjectId {
    let mut hasher = blake3::Hasher::new();
    hasher.update(kind.domain());
    hasher.update(bytes);
    *hasher.finalize().as_bytes()
}

#[cfg(test)]
mod tests {
    use super::*;
    use rusqlite::Connection;

    fn checkpointed_store() -> (tempfile::TempDir, ObjectId, ObjectId, u64) {
        let directory = tempfile::tempdir().unwrap();
        let store = ObjectStore::open(directory.path().to_owned()).unwrap();
        let first = store
            .put_bytes(ObjectKind::Chunk, &vec![b'a'; 128 * 1024])
            .unwrap();
        let second = store.put_bytes(ObjectKind::Chunk, b"second").unwrap();
        drop(store);

        // The first reopen verifies the legacy/uncheckpointed pack and records
        // the durable boundary used by subsequent startups.
        let store = ObjectStore::open(directory.path().to_owned()).unwrap();
        let verified_len = fs::metadata(directory.path().join("packs").join(INITIAL_PACK_NAME))
            .unwrap()
            .len();
        assert_eq!(
            store
                .catalog
                .pack_checkpoint(INITIAL_PACK_NAME)
                .unwrap()
                .unwrap()
                .verified_len,
            verified_len
        );
        drop(store);
        (directory, first, second, verified_len)
    }

    #[test]
    fn long_lived_store_refreshes_current_after_external_pack_activation() {
        let directory = tempfile::tempdir().unwrap();
        let first = ObjectStore::open(directory.path().to_owned()).unwrap();
        first.put_bytes(ObjectKind::Chunk, b"before gc").unwrap();
        let initial = directory.path().join("packs/pack-000001.agp");
        let initial_len = fs::metadata(&initial).unwrap().len();

        let mut gc_process = ObjectStore::open(directory.path().to_owned()).unwrap();
        let _exclusive = gc_process.acquire_maintenance_exclusive().unwrap();
        gc_process.activate_pack("pack-gc-test.agp").unwrap();
        drop(_exclusive);

        let id = first.put_bytes(ObjectKind::Chunk, b"after gc").unwrap();
        assert_eq!(fs::metadata(initial).unwrap().len(), initial_len);
        assert!(directory.path().join("packs/pack-gc-test.agp").exists());
        assert_eq!(
            first.catalog.object(&id).unwrap().unwrap().pack,
            "pack-gc-test.agp"
        );
    }

    #[test]
    fn valid_checkpoint_does_not_rehash_the_pack_prefix() {
        let (directory, first, second, _) = checkpointed_store();
        let catalog = Catalog::open(&directory.path().join("catalog.sqlite3")).unwrap();
        let first_location = catalog.object(&first).unwrap().unwrap();
        drop(catalog);

        let pack_path = directory.path().join("packs").join(INITIAL_PACK_NAME);
        let mut pack = OpenOptions::new().write(true).open(pack_path).unwrap();
        pack.seek(SeekFrom::Start(first_location.offset + 17))
            .unwrap();
        pack.write_all(b"Z").unwrap();
        pack.sync_data().unwrap();
        drop(pack);

        // Constant-size boundary validation succeeds without reading the old
        // payload. Reads retain their independent object-integrity check.
        let store = ObjectStore::open(directory.path().to_owned()).unwrap();
        assert!(store.read_bytes(&first, ObjectKind::Chunk).is_err());
        assert_eq!(
            store.read_bytes(&second, ObjectKind::Chunk).unwrap(),
            b"second"
        );
    }

    #[test]
    fn empty_catalog_index_forces_a_full_verified_rebuild() {
        let (directory, first, second, _) = checkpointed_store();
        let connection = Connection::open(directory.path().join("catalog.sqlite3")).unwrap();
        connection.execute("DELETE FROM objects", []).unwrap();
        drop(connection);

        let store = ObjectStore::open(directory.path().to_owned()).unwrap();
        assert_eq!(
            store.read_bytes(&first, ObjectKind::Chunk).unwrap(),
            vec![b'a'; 128 * 1024]
        );
        assert_eq!(
            store.read_bytes(&second, ObjectKind::Chunk).unwrap(),
            b"second"
        );
        assert_eq!(store.catalog.object_count().unwrap(), 2);
    }

    #[test]
    fn checkpointed_recovery_verifies_and_truncates_only_the_new_tail() {
        let (directory, first, second, verified_len) = checkpointed_store();
        let pack_path = directory.path().join("packs").join(INITIAL_PACK_NAME);
        let mut pack = OpenOptions::new().append(true).open(&pack_path).unwrap();
        pack.write_all(b"AGOB\x01").unwrap();
        pack.sync_data().unwrap();
        drop(pack);

        let store = ObjectStore::open(directory.path().to_owned()).unwrap();
        assert_eq!(fs::metadata(pack_path).unwrap().len(), verified_len);
        assert!(store.catalog.object(&first).unwrap().is_some());
        assert!(store.catalog.object(&second).unwrap().is_some());
        assert_eq!(
            store
                .catalog
                .pack_checkpoint(INITIAL_PACK_NAME)
                .unwrap()
                .unwrap()
                .verified_len,
            verified_len
        );
    }
}
