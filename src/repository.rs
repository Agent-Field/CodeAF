use crate::budget::{self, BudgetStatus};
use crate::catalog::CachedFile;
use crate::chunker::ChunkStream;
use crate::claims;
use crate::content_class;
use crate::coord;
use crate::estimate::{self, CaptureEstimate};
use crate::fork::{fork_workspace_excluding, ForkReport, ForkTier};
use crate::gc::{self, GcReport};
use crate::merge::{self, MergeAction, MergeConflict};
use crate::model::{
    id_hex, parse_id, Blob, ChunkRef, ClaimRecord, EntryKind, ObjectId, ObjectKind, SealQuality,
    Snapshot, SnapshotTrigger, SqliteBackup, TreeEntry, XattrEntry, Xattrs,
};
use crate::path_index::{PathIndex, CHILD_BATCH};
use crate::policy::{CapturePolicy, POLICY_FILE_BYTES};
use crate::radar::{EventPage, Radar, UniverseRegistration};
use crate::sorted_dir::SortedDirectory;
use crate::sqlite_adapter;
use crate::store::ObjectStore;
use crate::sync;
use crate::tree;
use anyhow::{bail, Context};
use directories::ProjectDirs;
use filetime::FileTime;
use fs2::FileExt;
use serde::Serialize;
use serde::{Deserialize, Serialize as SerdeSerialize};
use std::collections::{BTreeMap, BTreeSet};
use std::ffi::{OsStr, OsString};
use std::fs::{self, File};
use std::io::{BufReader, Write};
use std::os::unix::ffi::{OsStrExt, OsStringExt};
use std::os::unix::fs::{FileTypeExt, MetadataExt, PermissionsExt};
use std::path::{Component, Path, PathBuf};
use std::process::Stdio;
use std::time::{SystemTime, UNIX_EPOCH};

const WORKSPACE_FILE: &str = ".agit/workspace-id";
const WORKSPACE_FILE_BYTES: &[u8] = b".agit/workspace-id";
const FAMILY_FILE: &str = ".agit/family-id";
const FORK_FILE: &str = ".agit/fork-id";
const FORK_FILE_BYTES: &[u8] = b".agit/fork-id";

pub struct AgitRepository {
    root: PathBuf,
    workspace_id: String,
    family_id: String,
    store: ObjectStore,
}

#[derive(Debug, Clone, Serialize)]
pub struct SnapshotSummary {
    pub id: String,
    pub sealed_at: i64,
    pub label: Option<String>,
    pub trigger: String,
    pub materialization: MaterializationReport,
    pub pinned: bool,
}

#[derive(Debug, Clone, Serialize)]
pub struct MaterializationReport {
    pub grade: &'static str,
    pub partial_classes: Vec<&'static str>,
    pub missing_paths: Vec<MissingMaterializationPath>,
}

#[derive(Debug, Clone, Serialize)]
pub struct MissingMaterializationPath {
    pub path: String,
    pub class: &'static str,
    pub recovery: &'static str,
    #[serde(skip)]
    raw_path: Vec<u8>,
}

#[derive(Debug, Clone, Serialize)]
pub struct RewindChange {
    pub path: String,
    pub action: &'static str,
    #[serde(skip)]
    raw_path: Vec<u8>,
}

#[derive(Debug, Clone, Serialize)]
pub struct RewindPlan {
    pub target: String,
    #[serde(skip_serializing_if = "Option::is_none")]
    pub current_tree: Option<String>,
    pub changes: Vec<RewindChange>,
    pub preview_digest: String,
}

#[derive(Debug, Clone, Serialize)]
pub struct DiffChange {
    pub path: String,
    pub action: &'static str,
}

#[derive(Debug, Clone, Serialize)]
pub struct DiffSummary {
    pub target: String,
    pub base_snapshot: String,
    pub target_snapshot: String,
    pub changes: Vec<DiffChange>,
}

#[derive(Debug, Clone, Serialize)]
pub struct BisectCheck {
    pub snapshot: String,
    pub label: Option<String>,
    pub passed: bool,
    pub exit_code: Option<i32>,
    pub signal: Option<i32>,
    pub elapsed_ms: u64,
    pub probe_fork_ms: u64,
}

#[derive(Debug, Clone, Serialize)]
pub struct BisectOutcome {
    pub candidates: usize,
    pub good_snapshot: String,
    pub first_bad_snapshot: String,
    pub checks: Vec<BisectCheck>,
}

#[derive(Debug, Clone, Serialize)]
pub struct RepositoryStatus {
    pub workspace: PathBuf,
    pub store: PathBuf,
    pub head: Option<String>,
    pub snapshots: usize,
    pub objects: u64,
    pub physical_bytes: u64,
    pub watcher_running: bool,
    pub budget: BudgetStatus,
}

#[derive(Debug, Clone, Serialize)]
pub struct FidelityAspect {
    pub aspect: &'static str,
    pub fidelity: &'static str,
    pub detail: &'static str,
}

#[derive(Debug, Clone, Serialize)]
pub struct FidelityReport {
    pub platform: &'static str,
    pub grade: &'static str,
    pub excluded_subtrees: Vec<String>,
    pub aspects: Vec<FidelityAspect>,
}

#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct ForkSummary {
    #[serde(default)]
    pub fork_id: String,
    pub name: String,
    pub destination: PathBuf,
    pub base_snapshot: String,
    pub head_snapshot: String,
    pub tier: ForkTier,
    pub files: u64,
    pub directories: u64,
    pub symlinks: u64,
    pub fifos: u64,
    pub skipped_special: u64,
    pub logical_bytes: u64,
    pub cloned_bytes: u64,
    pub copied_bytes: u64,
    pub hardlinked_files: u64,
    pub elapsed_ms: u64,
    #[serde(default)]
    pub atomic_hierarchy: bool,
    pub created_at: i64,
    #[serde(default)]
    pub conflicts: u64,
    #[serde(default)]
    pub conflict_paths: Vec<String>,
    #[serde(default)]
    pub radar_stale: bool,
    #[serde(default)]
    pub radar_updated_at: Option<i64>,
}

#[derive(Debug, Clone, Serialize)]
pub struct ForkPlan {
    pub fork_id: String,
    pub name: String,
    pub destination: PathBuf,
    pub base_snapshot: String,
    pub files: u64,
    pub directories: u64,
    pub symlinks: u64,
    pub fifos: u64,
    pub skipped_special: u64,
    pub logical_bytes: u64,
    pub worst_case_copied_bytes: u64,
    pub projected_native_cow_ms: u64,
    pub projected_streaming_copy_ms: u64,
}

#[derive(Debug, Clone, Serialize)]
pub struct ForkRemoval {
    pub name: String,
    pub destination: PathBuf,
    pub files_removed: bool,
}

#[derive(Debug, Clone, Serialize)]
pub struct ClaimOutcome {
    pub claim: ClaimRecord,
    pub snapshot: String,
}

#[derive(Debug, Clone, Serialize)]
pub struct ReleaseOutcome {
    pub released: Vec<ClaimRecord>,
    pub snapshot: String,
}

#[derive(Debug, Clone, Serialize)]
pub struct CoordOutcome {
    pub operation: &'static str,
    pub propagation: coord::CoordPropagation,
    pub snapshot: String,
}

#[derive(Debug, Clone, Serialize)]
pub struct ForkUpdates {
    pub fork: String,
    pub head: Option<String>,
    pub cursor_found: bool,
    pub snapshots: Vec<SnapshotSummary>,
}

#[derive(Debug, Clone, Serialize)]
pub struct MergeOutcome {
    pub fork: String,
    pub base_snapshot: String,
    pub ours_snapshot: String,
    pub theirs_snapshot: String,
    pub ours_tree: String,
    pub theirs_tree: String,
    pub result_snapshot: Option<String>,
    pub changes: usize,
    pub conflicts: Vec<MergeConflict>,
    pub check: Option<String>,
    pub check_output: Option<String>,
    pub preview_digest: String,
}

#[derive(Debug, Clone, Serialize, PartialEq, Eq)]
#[serde(rename_all = "snake_case")]
pub enum SyncDisposition {
    UpToDate,
    FastForwarded,
    Bootstrapped,
    Diverged,
}

#[derive(Debug, Clone, Serialize)]
pub struct SyncPullOutcome {
    pub disposition: SyncDisposition,
    pub local_snapshot: String,
    pub remote_snapshot: String,
    pub remote_base_root: Option<String>,
    pub fetched_objects: u64,
    pub reused_objects: u64,
    pub fetched_bytes: u64,
}

#[derive(Clone)]
struct FlatEntry {
    entry: TreeEntry,
}

#[derive(Debug, SerdeSerialize, Deserialize)]
struct RestoreIntent {
    pre_snapshot: ObjectId,
    target_snapshot: ObjectId,
    paths: Vec<Vec<u8>>,
}

#[derive(Debug, SerdeSerialize, Deserialize)]
struct SyncState {
    remote_root: ObjectId,
}

impl AgitRepository {
    pub fn watch(root: &Path) -> anyhow::Result<(Self, ObjectId)> {
        Self::attach_and_snapshot(
            root,
            Some("initial protection".to_owned()),
            SnapshotTrigger::Manual,
        )
    }

    pub fn attach_and_snapshot(
        root: &Path,
        label: Option<String>,
        existing_trigger: SnapshotTrigger,
    ) -> anyhow::Result<(Self, ObjectId)> {
        let root = root
            .canonicalize()
            .with_context(|| format!("open {}", root.display()))?;
        anyhow::ensure!(
            root.join(".git").exists(),
            "agit currently requires a Git repository"
        );
        fs::create_dir_all(root.join(".agit"))?;
        let workspace_path = root.join(WORKSPACE_FILE);
        let workspace_id = if workspace_path.exists() {
            fs::read_to_string(&workspace_path)?.trim().to_owned()
        } else {
            let id = new_workspace_id(&root);
            atomic_write(&workspace_path, format!("{id}\n").as_bytes())?;
            id
        };

        let store_root = data_root()?.join("store-v1");
        anyhow::ensure!(
            !root.starts_with(&store_root),
            "workspace cannot contain the agit store"
        );
        let mut store = ObjectStore::open(store_root)?;
        store.ensure_workspace(&workspace_id, root.as_os_str().as_bytes())?;
        let family_id = ensure_family_id(&root, &store, &workspace_id)?;
        coord::reconcile(&root, &store, &family_id)?;
        let mut repository = Self {
            root,
            workspace_id,
            family_id,
            store,
        };
        repository.recover_interrupted_rewind()?;
        let trigger = if repository
            .store
            .workspace_head(&repository.workspace_id)?
            .is_some()
        {
            existing_trigger
        } else {
            SnapshotTrigger::Initial
        };
        let id = repository.snapshot(label, trigger)?;
        Ok((repository, id))
    }

    pub fn open(root: &Path) -> anyhow::Result<Self> {
        let root = root
            .canonicalize()
            .with_context(|| format!("open {}", root.display()))?;
        let mut store = ObjectStore::open(data_root()?.join("store-v1"))?;
        let workspace_path = root.join(WORKSPACE_FILE);
        let workspace_id = if workspace_path.exists() {
            fs::read_to_string(&workspace_path)?.trim().to_owned()
        } else {
            let id = store
                .find_workspace(root.as_os_str().as_bytes())?
                .context("this repository is not watched; run `agit watch` first")?;
            fs::create_dir_all(root.join(".agit"))?;
            atomic_write(&workspace_path, format!("{id}\n").as_bytes())?;
            id
        };
        store.ensure_workspace(&workspace_id, root.as_os_str().as_bytes())?;
        let family_id = ensure_family_id(&root, &store, &workspace_id)?;
        coord::reconcile(&root, &store, &family_id)?;
        let repository = Self {
            root,
            workspace_id,
            family_id,
            store,
        };
        repository.recover_interrupted_rewind()?;
        Ok(repository)
    }

    pub fn root(&self) -> &Path {
        &self.root
    }

    pub(crate) fn filter_capture_paths(
        &self,
        paths: impl IntoIterator<Item = PathBuf>,
    ) -> anyhow::Result<Vec<PathBuf>> {
        let policy = CapturePolicy::load(&self.root)?;
        Ok(paths
            .into_iter()
            .filter(|path| {
                !policy.excludes_path(&self.root, path)
                    && !changed_path_matches(&self.root, path, WORKSPACE_FILE_BYTES)
                    && !changed_path_matches(&self.root, path, FORK_FILE_BYTES)
            })
            .collect())
    }

    pub fn store_root(&self) -> &Path {
        self.store.root()
    }

    pub fn store_physical_bytes(&self) -> anyhow::Result<u64> {
        Ok(self.store.stats()?.physical_bytes)
    }

    pub fn workspace_data_dir(&self) -> PathBuf {
        self.store.workspace_data_dir(&self.workspace_id)
    }

    pub fn snapshot(
        &mut self,
        label: Option<String>,
        trigger: SnapshotTrigger,
    ) -> anyhow::Result<ObjectId> {
        self.snapshot_internal(label, trigger, None, Vec::new())
    }

    pub fn snapshot_changed_paths(
        &mut self,
        label: Option<String>,
        trigger: SnapshotTrigger,
        changed_paths: &[PathBuf],
    ) -> anyhow::Result<ObjectId> {
        self.snapshot_internal(label, trigger, Some(changed_paths), Vec::new())
    }

    fn snapshot_internal(
        &mut self,
        label: Option<String>,
        trigger: SnapshotTrigger,
        changed_paths: Option<&[PathBuf]>,
        merge_parents: Vec<ObjectId>,
    ) -> anyhow::Result<ObjectId> {
        let _maintenance = self.store.acquire_maintenance_shared()?;
        let parent = self.store.workspace_head(&self.workspace_id)?;
        let policy = CapturePolicy::load(&self.root)?;
        let root_tree = match changed_paths {
            Some(paths)
                if !paths.is_empty()
                    && !paths
                        .iter()
                        .any(|path| changed_path_matches(&self.root, path, POLICY_FILE_BYTES)) =>
            {
                self.capture_changed_paths_retry_with_policy(paths, &policy)?
            }
            _ => self.capture_root_retry_with_policy(&policy)?,
        };
        // Continuous watcher seals keep raw database/WAL/SHM bytes (L0) and
        // avoid a second whole-tree database discovery pass. Forced boundaries
        // attach the logically consistent SQLite image (L1).
        let sqlite_backups = if trigger == SnapshotTrigger::Watcher {
            Vec::new()
        } else {
            self.capture_sqlite_backups(&policy)?
        };
        let (secs, nanos) = now();
        let claims = self.active_claims()?;
        let snapshot = Snapshot {
            root_tree,
            parent,
            merge_parents,
            sealed_at_secs: secs,
            sealed_at_nanos: nanos,
            quality: SealQuality::Quiescent,
            trigger: trigger.clone(),
            label: label.clone(),
            sqlite_backups,
            claims,
            excluded_paths: policy.rule_strings(),
        };
        let id = self.store.put_struct(ObjectKind::Snapshot, &snapshot)?;
        self.store
            .publish_snapshot(&self.workspace_id, id, secs, label, trigger)?;
        Ok(id)
    }

    pub fn timeline(&self, limit: usize) -> anyhow::Result<Vec<SnapshotSummary>> {
        let pinned = self.store.pinned_snapshots(&self.workspace_id)?;
        self.store
            .timeline(&self.workspace_id, limit)?
            .into_iter()
            .map(|row| {
                Ok(SnapshotSummary {
                    id: id_hex(&row.id),
                    sealed_at: row.sealed_at,
                    label: row.label,
                    trigger: row.trigger,
                    materialization: self.materialization(&row.id)?,
                    pinned: pinned.contains(&row.id),
                })
            })
            .collect()
    }

    pub fn status(&self) -> anyhow::Result<RepositoryStatus> {
        let stats = self.store.stats()?;
        let head = self.store.workspace_head(&self.workspace_id)?;
        Ok(RepositoryStatus {
            workspace: self.root.clone(),
            store: self.store.root().to_owned(),
            head: head.map(|id| id_hex(&id)),
            snapshots: self.store.retained_snapshot_count(&self.workspace_id)? as usize,
            objects: stats.objects,
            physical_bytes: stats.physical_bytes,
            watcher_running: self.watcher_running(),
            budget: self.store.budget_status()?,
        })
    }

    pub fn fidelity(&self) -> anyhow::Result<FidelityReport> {
        let policy = CapturePolicy::load(&self.root)?;
        Ok(FidelityReport {
            platform: std::env::consts::OS,
            grade: "partial",
            excluded_subtrees: policy.rule_strings(),
            aspects: vec![
                FidelityAspect {
                    aspect: "regular_file_bytes",
                    fidelity: "exact",
                    detail: "content-addressed chunks are verified before restoration",
                },
                FidelityAspect {
                    aspect: "directories_and_symlinks",
                    fidelity: "exact",
                    detail: "directory structure and symlink target bytes are preserved",
                },
                FidelityAspect {
                    aspect: "mode_and_mtime",
                    fidelity: "exact",
                    detail: "permission bits and nanosecond modification times are restored",
                },
                FidelityAspect {
                    aspect: "extended_attributes",
                    fidelity: "best_effort",
                    detail: "readable xattrs are preserved; permission-denied namespaces are omitted",
                },
                FidelityAspect {
                    aspect: "sqlite",
                    fidelity: "exact_plus_logical",
                    detail: "raw database/WAL/SHM bytes are exact; forced seals also attach a checked logical backup",
                },
                FidelityAspect {
                    aspect: "hard_link_groups",
                    fidelity: "not_preserved_by_rewind",
                    detail: "forks retain link groups, but snapshot restoration currently materializes independent files",
                },
                FidelityAspect {
                    aspect: "sparse_holes",
                    fidelity: "not_preserved",
                    detail: "file bytes are exact but sparse files currently restore densely",
                },
                FidelityAspect {
                    aspect: "ownership",
                    fidelity: "not_captured",
                    detail: "uid and gid are not stored in the current snapshot schema",
                },
                FidelityAspect {
                    aspect: "acls",
                    fidelity: "best_effort",
                    detail: "only ACL state exposed through readable extended attributes is retained",
                },
                FidelityAspect {
                    aspect: "fifos",
                    fidelity: "exact_entry",
                    detail: "FIFO entries and modes are recreated; FIFOs have no retained content",
                },
                FidelityAspect {
                    aspect: "sockets",
                    fidelity: "marker_only",
                    detail: "runtime sockets are recorded as markers and intentionally not recreated",
                },
                FidelityAspect {
                    aspect: "device_nodes",
                    fidelity: "not_captured",
                    detail: "device nodes are skipped with a capture-time warning",
                },
                FidelityAspect {
                    aspect: "external_git_storage",
                    fidelity: "out_of_scope",
                    detail: "Git object stores referenced outside the watched root are not captured",
                },
            ],
        })
    }

    pub fn gc(&mut self, dry_run: bool) -> anyhow::Result<GcReport> {
        let report = gc::collect(&mut self.store, dry_run)?;
        if !dry_run {
            let status = self.store.budget_status()?;
            budget::record_attempt(self.store.root(), status.physical_bytes, status.satisfied)?;
        }
        Ok(report)
    }

    pub fn pin(&mut self, snapshot: &ObjectId) -> anyhow::Result<bool> {
        let _maintenance = self.store.acquire_maintenance_exclusive()?;
        let materialization = self.materialization(snapshot)?;
        anyhow::ensure!(
            materialization.grade == "exact",
            "cannot pin a partial snapshot; {} path(s) are missing bytes",
            materialization.missing_paths.len()
        );
        self.store.pin_snapshot(&self.workspace_id, *snapshot)
    }

    pub fn unpin(&mut self, snapshot: &ObjectId) -> anyhow::Result<bool> {
        self.store.unpin_snapshot(&self.workspace_id, snapshot)
    }

    pub fn materialization(&self, snapshot_id: &ObjectId) -> anyhow::Result<MaterializationReport> {
        derive_materialization(&self.store, snapshot_id)
    }

    pub fn gc_global(dry_run: bool) -> anyhow::Result<GcReport> {
        let mut store = ObjectStore::open(data_root()?.join("store-v1"))?;
        let report = gc::collect(&mut store, dry_run)?;
        if !dry_run {
            let status = store.budget_status()?;
            budget::record_attempt(store.root(), status.physical_bytes, status.satisfied)?;
        }
        Ok(report)
    }

    pub fn budget_global(
        max_store_bytes: Option<u64>,
        reserved_free_bytes: Option<u64>,
    ) -> anyhow::Result<BudgetStatus> {
        let mut store = ObjectStore::open(data_root()?.join("store-v1"))?;
        if max_store_bytes.is_some() || reserved_free_bytes.is_some() {
            store.configure_budget(max_store_bytes, reserved_free_bytes)
        } else {
            store.budget_status()
        }
    }

    pub fn global_store_physical_bytes() -> anyhow::Result<u64> {
        Ok(ObjectStore::open(data_root()?.join("store-v1"))?
            .stats()?
            .physical_bytes)
    }

    pub fn estimate(root: &Path) -> anyhow::Result<CaptureEstimate> {
        let root = root.canonicalize()?;
        anyhow::ensure!(
            root.join(".git").exists(),
            "agit currently requires a Git repository"
        );
        let store = ObjectStore::open(data_root()?.join("store-v1"))?;
        estimate::calculate(&root, &store)
    }

    pub fn claims(&self) -> anyhow::Result<Vec<ClaimRecord>> {
        self.active_claims()
    }

    pub fn claim(
        &mut self,
        pattern: &str,
        owner: &str,
        ttl_seconds: u64,
    ) -> anyhow::Result<ClaimOutcome> {
        let mut registry = claims::Registry::open(self.store.root(), &self.family_id)?;
        let claim = registry.claim(pattern, owner, &self.workspace_id, ttl_seconds)?;
        let snapshot = self.snapshot(
            Some(format!("{} claimed {}", claim.owner, claim.pattern)),
            SnapshotTrigger::Claim,
        )?;
        Ok(ClaimOutcome {
            claim,
            snapshot: id_hex(&snapshot),
        })
    }

    pub fn release_claim(&mut self, selector: &str, owner: &str) -> anyhow::Result<ReleaseOutcome> {
        let mut registry = claims::Registry::open(self.store.root(), &self.family_id)?;
        let released = registry.release(selector, owner, &self.workspace_id)?;
        let snapshot = match self.snapshot(
            Some(format!("{owner} released {selector}")),
            SnapshotTrigger::Release,
        ) {
            Ok(snapshot) => snapshot,
            Err(error) => {
                registry.restore(&released)?;
                return Err(error.context("release snapshot failed; claims were restored"));
            }
        };
        Ok(ReleaseOutcome {
            released,
            snapshot: id_hex(&snapshot),
        })
    }

    pub fn default_claim_owner(&self) -> String {
        std::env::var("AGIT_AGENT_ID")
            .or_else(|_| std::env::var("AGIT_FORK_NAME"))
            .unwrap_or_else(|_| format!("workspace-{}", &self.workspace_id[..12]))
    }

    pub fn coord_write(
        &mut self,
        path: &Path,
        bytes: &[u8],
        owner: &str,
    ) -> anyhow::Result<CoordOutcome> {
        let propagation = coord::write(&self.root, &self.store, &self.family_id, path, bytes)?;
        let snapshot = self.snapshot(
            Some(format!("{owner} wrote coord/{}", propagation.path)),
            SnapshotTrigger::Coord,
        )?;
        Ok(CoordOutcome {
            operation: "write",
            propagation,
            snapshot: id_hex(&snapshot),
        })
    }

    pub fn coord_remove(&mut self, path: &Path, owner: &str) -> anyhow::Result<CoordOutcome> {
        let propagation = coord::remove(&self.root, &self.store, &self.family_id, path)?;
        let snapshot = self.snapshot(
            Some(format!("{owner} removed coord/{}", propagation.path)),
            SnapshotTrigger::Coord,
        )?;
        Ok(CoordOutcome {
            operation: "remove",
            propagation,
            snapshot: id_hex(&snapshot),
        })
    }

    pub fn coord_read(&self, path: &Path) -> anyhow::Result<Vec<u8>> {
        coord::reconcile(&self.root, &self.store, &self.family_id)?;
        coord::read(&self.root, path)
    }

    pub fn coord_list(&self) -> anyhow::Result<Vec<coord::CoordEntry>> {
        coord::reconcile(&self.root, &self.store, &self.family_id)?;
        coord::list(&self.root)
    }

    pub fn fork_updates(
        &self,
        name: &str,
        after: Option<&str>,
        limit: usize,
    ) -> anyhow::Result<ForkUpdates> {
        anyhow::ensure!(
            (1..=1000).contains(&limit),
            "limit must be between 1 and 1000"
        );
        let fork = self
            .forks()?
            .into_iter()
            .find(|fork| fork.name == name)
            .with_context(|| format!("fork `{name}` was not found"))?;
        anyhow::ensure!(fork.destination.exists(), "fork directory no longer exists");
        let repository = AgitRepository::open(&fork.destination)?;
        let timeline = repository.timeline(limit.saturating_add(1))?;
        let head = timeline.first().map(|snapshot| snapshot.id.clone());
        let (cursor_found, mut snapshots) = match after {
            None => (true, timeline.into_iter().take(1).collect::<Vec<_>>()),
            Some(cursor) => {
                anyhow::ensure!(
                    cursor.len() == 64 && cursor.bytes().all(|byte| byte.is_ascii_hexdigit()),
                    "fork update cursor must be a full snapshot ID"
                );
                let position = timeline.iter().position(|snapshot| snapshot.id == cursor);
                match position {
                    Some(position) => (true, timeline.into_iter().take(position).collect()),
                    None => (false, timeline.into_iter().take(limit).collect()),
                }
            }
        };
        snapshots.reverse();
        Ok(ForkUpdates {
            fork: name.to_owned(),
            head,
            cursor_found,
            snapshots,
        })
    }

    pub fn pair(
        &self,
        remote: &Path,
        namespace: &str,
        key: Option<&str>,
    ) -> anyhow::Result<sync::PairSummary> {
        let summary = sync::pair(&self.sync_config_path(), remote, namespace, key)?;
        let key_bytes = hex::decode(&summary.key_hex)?;
        let mut key = [0_u8; 32];
        key.copy_from_slice(&key_bytes);
        let mut hasher = blake3::Hasher::new_keyed(&key);
        hasher.update(b"agit:paired-family:v1\0");
        hasher.update(namespace.as_bytes());
        let family_id = hex::encode(&hasher.finalize().as_bytes()[..16]);
        write_family_id(&self.root, &self.store, &self.workspace_id, &family_id)?;
        let state = self.sync_state_path();
        if state.exists() {
            fs::remove_file(state)?;
        }
        Ok(summary)
    }

    pub fn sync_push(&mut self, takeover: bool) -> anyhow::Result<sync::PushReport> {
        let snapshot = self.snapshot(
            Some("sync push boundary".to_owned()),
            SnapshotTrigger::SyncPush,
        )?;
        let config = sync::load(&self.sync_config_path())?;
        let expected = self.read_sync_state()?.map(|state| state.remote_root);
        let report = sync::push(&self.store, snapshot, &config, expected, takeover)?;
        self.write_sync_state(parse_id(&report.root)?)?;
        Ok(report)
    }

    pub fn sync_pull(&mut self, bootstrap: bool) -> anyhow::Result<SyncPullOutcome> {
        let local = self.snapshot(
            Some("sync pull boundary".to_owned()),
            SnapshotTrigger::SyncLocal,
        )?;
        let local_snapshot: Snapshot = self.store.read_struct(&local, ObjectKind::Snapshot)?;
        let config = sync::load(&self.sync_config_path())?;
        let pulled = {
            // Imported objects are not reachable until incoming.json is
            // durable. Holding this guard closes the assembly/publication GC
            // race while keeping memory bounded by the disk-backed queue.
            let _maintenance = self.store.acquire_maintenance_shared()?;
            let pulled = sync::pull(&self.store, &config)?;
            let incoming = serde_json::json!({"snapshot": pulled.snapshot});
            let path = self.sync_incoming_path();
            if let Some(parent) = path.parent() {
                fs::create_dir_all(parent)?;
            }
            atomic_write(&path, &serde_json::to_vec(&incoming)?)?;
            pulled
        };
        let remote_snapshot: Snapshot = self
            .store
            .read_struct(&pulled.snapshot, ObjectKind::Snapshot)?;
        if !remote_snapshot.claims.is_empty() {
            claims::Registry::open(self.store.root(), &self.family_id)?
                .restore(&remote_snapshot.claims)?;
        }
        let disposition = if local_snapshot.root_tree == remote_snapshot.root_tree {
            self.write_sync_state(pulled.root)?;
            self.clear_sync_incoming()?;
            SyncDisposition::UpToDate
        } else if pulled.base_root == Some(local_snapshot.root_tree) || bootstrap {
            self.rewind(&pulled.snapshot, &[], false)?;
            let synced = self.snapshot(
                Some(format!(
                    "fast-forwarded from remote {}",
                    &id_hex(&pulled.snapshot)[..12]
                )),
                SnapshotTrigger::SyncPull,
            )?;
            self.write_sync_state(pulled.root)?;
            self.clear_sync_incoming()?;
            return Ok(SyncPullOutcome {
                disposition: if bootstrap {
                    SyncDisposition::Bootstrapped
                } else {
                    SyncDisposition::FastForwarded
                },
                local_snapshot: id_hex(&synced),
                remote_snapshot: id_hex(&pulled.snapshot),
                remote_base_root: pulled.base_root.map(|id| id_hex(&id)),
                fetched_objects: pulled.report.fetched_objects,
                reused_objects: pulled.report.reused_objects,
                fetched_bytes: pulled.report.fetched_bytes,
            });
        } else {
            SyncDisposition::Diverged
        };
        Ok(SyncPullOutcome {
            disposition,
            local_snapshot: id_hex(&local),
            remote_snapshot: id_hex(&pulled.snapshot),
            remote_base_root: pulled.base_root.map(|id| id_hex(&id)),
            fetched_objects: pulled.report.fetched_objects,
            reused_objects: pulled.report.reused_objects,
            fetched_bytes: pulled.report.fetched_bytes,
        })
    }

    pub fn prepare_fork(&mut self, name: &str, destination: &Path) -> anyhow::Result<ForkPlan> {
        validate_fork_name(name)?;
        let destination = absolute_destination(destination)?;
        anyhow::ensure!(
            !destination.exists(),
            "fork destination already exists: {}",
            destination.display()
        );
        if let Some(parent) = destination.parent() {
            fs::create_dir_all(parent)
                .with_context(|| format!("create fork parent {}", parent.display()))?;
        }

        let base = self.snapshot(
            Some(format!("fork base: {name}")),
            SnapshotTrigger::ForkBase,
        )?;
        let usage = self.open_path_index()?.usage()?;
        let entries = usage
            .files
            .saturating_add(usage.directories)
            .saturating_add(usage.symlinks)
            .saturating_add(usage.fifos)
            .saturating_add(usage.special);
        let metadata_entries_per_second = if cfg!(target_os = "macos") {
            7_500
        } else {
            2_500
        };
        let projected_native_cow_ms =
            ceiling_div(entries.saturating_mul(1_000), metadata_entries_per_second).max(10);
        let projected_streaming_copy_ms = projected_native_cow_ms.saturating_add(ceiling_div(
            usage.logical_bytes.saturating_mul(1_000),
            250 * 1024 * 1024,
        ));
        Ok(ForkPlan {
            fork_id: new_fork_id()?,
            name: name.to_owned(),
            destination,
            base_snapshot: id_hex(&base),
            files: usage.files,
            directories: usage.directories,
            symlinks: usage.symlinks,
            fifos: usage.fifos,
            skipped_special: usage.special,
            logical_bytes: usage.logical_bytes,
            worst_case_copied_bytes: usage.logical_bytes,
            projected_native_cow_ms,
            projected_streaming_copy_ms,
        })
    }

    pub fn materialize_fork(&mut self, plan: ForkPlan) -> anyhow::Result<ForkSummary> {
        validate_fork_name(&plan.name)?;
        validate_fork_id(&plan.fork_id)?;
        let destination = absolute_destination(&plan.destination)?;
        anyhow::ensure!(
            destination == plan.destination,
            "prepared fork destination changed"
        );
        anyhow::ensure!(
            !destination.exists(),
            "fork destination already exists: {}",
            destination.display()
        );
        let base = parse_id(&plan.base_snapshot)?;
        let _: Snapshot = self.store.read_struct(&base, ObjectKind::Snapshot)?;
        let report = fork_workspace_excluding(
            &self.root,
            &destination,
            &[Path::new(WORKSPACE_FILE), Path::new(FORK_FILE)],
        )?;

        // A copied workspace identity would alias two mutable directories onto
        // one timeline. It is transport metadata, not captured user state.
        let copied_identity = destination.join(WORKSPACE_FILE);
        if copied_identity.exists() {
            fs::remove_file(&copied_identity)?;
        }
        let (fork_repository, fork_head) = if report.atomic_hierarchy {
            self.verify_atomic_fork(&base, &destination, &report)?;
            let fork_repository = AgitRepository::attach_existing_snapshot(&destination, base)?;
            self.open_path_index()?
                .backup_to(&fork_repository.workspace_data_dir().join("paths.sqlite3"))?;
            (fork_repository, base)
        } else {
            let (fork_repository, fork_head) = AgitRepository::watch(&destination)?;
            if !self.snapshots_match_fork(&base, &fork_repository, &fork_head)? {
                bail!(
                    "source changed while the fork was being created; the isolated copy remains at {} for inspection",
                    destination.display()
                );
            }
            (fork_repository, fork_head)
        };
        write_fork_id(&fork_repository, &plan.fork_id)?;

        let summary = fork_summary(
            &plan.fork_id,
            &plan.name,
            destination,
            base,
            fork_head,
            report,
        );
        let record_path = self.forks_dir().join(format!("{}.json", plan.name));
        fs::create_dir_all(self.forks_dir())?;
        atomic_write(&record_path, &serde_json::to_vec_pretty(&summary)?)?;
        Ok(summary)
    }

    pub fn fork(&mut self, name: &str, destination: &Path) -> anyhow::Result<ForkSummary> {
        let plan = self.prepare_fork(name, destination)?;
        self.materialize_fork(plan)
    }

    fn attach_existing_snapshot(root: &Path, snapshot_id: ObjectId) -> anyhow::Result<Self> {
        let root = root.canonicalize()?;
        fs::create_dir_all(root.join(".agit"))?;
        let workspace_id = new_workspace_id(&root);
        atomic_write(
            &root.join(WORKSPACE_FILE),
            format!("{workspace_id}\n").as_bytes(),
        )?;
        let mut store = ObjectStore::open(data_root()?.join("store-v1"))?;
        let snapshot: Snapshot = store.read_struct(&snapshot_id, ObjectKind::Snapshot)?;
        store.ensure_workspace(&workspace_id, root.as_os_str().as_bytes())?;
        let family_id = ensure_family_id(&root, &store, &workspace_id)?;
        store.publish_snapshot(
            &workspace_id,
            snapshot_id,
            snapshot.sealed_at_secs,
            Some("atomic fork base".to_owned()),
            SnapshotTrigger::ForkBase,
        )?;
        Ok(Self {
            root,
            workspace_id,
            family_id,
            store,
        })
    }

    fn verify_atomic_fork(
        &self,
        base: &ObjectId,
        destination: &Path,
        report: &ForkReport,
    ) -> anyhow::Result<()> {
        let snapshot: Snapshot = self.store.read_struct(base, ObjectKind::Snapshot)?;
        let index = self.open_path_index()?;
        anyhow::ensure!(
            index.root(&self.store_generation()?)? == Some(snapshot.root_tree),
            "source path index no longer describes the fork base"
        );
        let usage = index.usage()?;
        let expected_entries = usage
            .files
            .saturating_add(usage.directories.saturating_sub(1))
            .saturating_add(usage.symlinks)
            .saturating_add(usage.fifos)
            .saturating_add(usage.special);
        let policy = CapturePolicy::load(&self.root)?;
        let counts_match = report.files == usage.files
            && report.directories == usage.directories
            && report.symlinks == usage.symlinks
            && report.fifos == usage.fifos
            && report.skipped_special == usage.special;
        if policy.rule_strings().is_empty() && counts_match {
            let mut after: Option<Vec<u8>> = None;
            loop {
                let entries = index.entries_after(after.as_deref(), CHILD_BATCH)?;
                if entries.is_empty() {
                    break;
                }
                let paths: Vec<_> = entries.iter().map(|(path, _)| path.clone()).collect();
                let cached = self.store.cached_files(&self.workspace_id, &paths)?;
                for ((relative, indexed), cached) in entries.iter().zip(&cached) {
                    self.verify_atomic_entry(destination, relative, indexed, cached.as_ref())?;
                }
                after = entries.last().map(|(path, _)| path.clone());
            }
            return Ok(());
        }

        let mut visited = 0_u64;
        let mut stack = vec![destination.to_owned()];
        while let Some(directory) = stack.pop() {
            for entry in fs::read_dir(&directory)? {
                let entry = entry?;
                let path = entry.path();
                let relative = path.strip_prefix(destination)?.as_os_str().as_bytes();
                if is_identity_path(relative) || policy.excludes_bytes(relative) {
                    continue;
                }
                let indexed = index.entry(relative)?.with_context(|| {
                    format!(
                        "atomic fork added `{}` after its base seal",
                        String::from_utf8_lossy(relative)
                    )
                })?;
                let cached = if indexed.kind == EntryKind::File {
                    self.store.cached_file(&self.workspace_id, relative)?
                } else {
                    None
                };
                self.verify_atomic_entry(destination, relative, &indexed, cached.as_ref())?;
                if indexed.kind == EntryKind::Directory {
                    stack.push(path);
                }
                visited += 1;
            }
        }
        anyhow::ensure!(
            visited == expected_entries,
            "atomic fork is missing {} base path(s)",
            expected_entries.saturating_sub(visited)
        );
        Ok(())
    }

    fn verify_atomic_entry(
        &self,
        destination: &Path,
        relative: &[u8],
        indexed: &TreeEntry,
        cached: Option<&CachedFile>,
    ) -> anyhow::Result<()> {
        let path = safe_join(destination, relative)?;
        let metadata = fs::symlink_metadata(&path)?;
        anyhow::ensure!(
            entry_matches_metadata(indexed, &metadata),
            "atomic fork metadata changed for `{}`",
            String::from_utf8_lossy(relative)
        );
        match indexed.kind {
            EntryKind::Directory => anyhow::ensure!(
                self.capture_xattrs(&path)? == indexed.xattrs,
                "atomic fork xattrs changed for `{}`",
                String::from_utf8_lossy(relative)
            ),
            EntryKind::File => {
                let source = safe_join(&self.root, relative)?;
                let source_metadata = fs::symlink_metadata(source)?;
                let cached = cached.context("fork base file cache is incomplete")?;
                anyhow::ensure!(
                    cached_matches(cached, &source_metadata)
                        && indexed.target == Some(cached.blob_id),
                    "source changed while its atomic fork was created"
                );
            }
            EntryKind::Symlink => anyhow::ensure!(
                fs::read_link(&path)?.as_os_str().as_bytes() == indexed.link_target,
                "atomic fork symlink changed for `{}`",
                String::from_utf8_lossy(relative)
            ),
            EntryKind::Fifo | EntryKind::SocketMarker => {}
        }
        Ok(())
    }

    pub fn forks(&self) -> anyhow::Result<Vec<ForkSummary>> {
        let directory = self.forks_dir();
        let mut forks = Vec::new();
        if directory.exists() {
            for entry in fs::read_dir(directory)? {
                let entry = entry?;
                if entry.file_type()?.is_file()
                    && entry.path().extension() == Some(OsStr::new("json"))
                {
                    let mut fork: ForkSummary = serde_json::from_slice(&fs::read(entry.path())?)?;
                    if fork.fork_id.is_empty() {
                        fork.fork_id = legacy_fork_id(&fork.name, &fork.base_snapshot);
                    }
                    // Radar fields are derived from family state at read time.
                    fork.conflicts = 0;
                    fork.conflict_paths.clear();
                    fork.radar_stale = false;
                    fork.radar_updated_at = None;
                    forks.push(fork);
                }
            }
        }
        forks.sort_by(|left: &ForkSummary, right: &ForkSummary| {
            right
                .created_at
                .cmp(&left.created_at)
                .then_with(|| left.name.cmp(&right.name))
        });
        let mut radar = Radar::open(self.store.root(), &self.family_id)?;
        let claims = self.active_claims()?;
        radar.retain_universes(
            &forks
                .iter()
                .map(|fork| fork.fork_id.clone())
                .collect::<Vec<_>>(),
            &claims,
        )?;
        for fork in &mut forks {
            fork.radar_stale = !fork.destination.exists();
            if !fork.destination.exists() {
                continue;
            }
            let fork_repository = match AgitRepository::open(&fork.destination) {
                Ok(repository) => repository,
                Err(_) => {
                    fork.radar_stale = true;
                    continue;
                }
            };
            let Some(head) = fork_repository
                .store
                .workspace_head(&fork_repository.workspace_id)?
            else {
                continue;
            };
            let base = parse_id(&fork.base_snapshot)?;
            radar.observe(
                &self.store,
                UniverseRegistration {
                    fork_id: &fork.fork_id,
                    name: &fork.name,
                    workspace_id: &fork_repository.workspace_id,
                    base_snapshot: base,
                    head_snapshot: head,
                },
            )?;
            fork.head_snapshot = id_hex(&head);
        }
        radar.reconcile(&claims)?;
        let conflicts = radar.summaries()?;
        let updated_at = radar.updated_at()?;
        for fork in &mut forks {
            fork.radar_updated_at = updated_at.get(&fork.fork_id).copied();
            if let Some(summary) = conflicts.get(&fork.fork_id) {
                fork.conflicts = summary.count;
                fork.conflict_paths = summary
                    .paths
                    .iter()
                    .map(|path| display_relative(path))
                    .collect();
            }
        }
        Ok(forks)
    }

    pub fn events(&self, after: Option<&str>, limit: usize) -> anyhow::Result<EventPage> {
        // Inventory refresh is also the reconciliation path for seals published
        // before a crash or while no event consumer was connected.
        let _ = self.forks()?;
        Radar::open(self.store.root(), &self.family_id)?.events_after(after, limit)
    }

    pub fn diff(&mut self, target: &str) -> anyhow::Result<DiffSummary> {
        if let Some(mut fork) = self.forks()?.into_iter().find(|fork| fork.name == target) {
            anyhow::ensure!(
                fork.destination.exists(),
                "fork directory no longer exists: {}",
                fork.destination.display()
            );
            let mut fork_repository = AgitRepository::open(&fork.destination)?;
            let head = fork_repository.snapshot(
                Some(format!("inspection boundary for {}", fork.name)),
                SnapshotTrigger::Inspection,
            )?;
            fork.head_snapshot = id_hex(&head);
            atomic_write(
                &self.forks_dir().join(format!("{}.json", fork.name)),
                &serde_json::to_vec_pretty(&fork)?,
            )?;
            let base = parse_id(&fork.base_snapshot)?;
            return self.diff_snapshot_pair(fork.name, base, head);
        }

        let base = self.resolve_snapshot(target)?;
        let head = self.snapshot(
            Some(format!("inspection since {}", &id_hex(&base)[..12])),
            SnapshotTrigger::Inspection,
        )?;
        self.diff_snapshot_pair(target.to_owned(), base, head)
    }

    pub fn bisect(
        &mut self,
        command: &[OsString],
        good: Option<&str>,
        bad: Option<&str>,
        limit: usize,
    ) -> anyhow::Result<BisectOutcome> {
        anyhow::ensure!(!command.is_empty(), "bisect command cannot be empty");
        anyhow::ensure!(
            (2..=10_000).contains(&limit),
            "bisect limit must be 2..=10000"
        );
        anyhow::ensure!(
            good.is_some() == bad.is_some(),
            "--good and --bad must be provided together"
        );

        let boundary = self.snapshot(
            Some("bisect source boundary".to_owned()),
            SnapshotTrigger::Inspection,
        )?;
        let mut candidates: Vec<_> = self
            .store
            .timeline(&self.workspace_id, limit)?
            .into_iter()
            .map(|row| (row.id, row.label))
            .collect();
        candidates.reverse();
        anyhow::ensure!(
            candidates.len() >= 2,
            "bisect requires at least two retained snapshots"
        );

        let (good_index, bad_index) = match (good, bad) {
            (Some(good), Some(bad)) => {
                let good = self.resolve_snapshot(good)?;
                let bad = self.resolve_snapshot(bad)?;
                let good_index = candidates
                    .iter()
                    .position(|(id, _)| *id == good)
                    .context("good snapshot is outside the retained bisect window")?;
                let bad_index = candidates
                    .iter()
                    .position(|(id, _)| *id == bad)
                    .context("bad snapshot is outside the retained bisect window")?;
                anyhow::ensure!(
                    good_index < bad_index,
                    "good snapshot must precede bad snapshot"
                );
                (good_index, bad_index)
            }
            (None, None) => (0, candidates.len() - 1),
            _ => unreachable!("paired options were validated"),
        };

        let parent = self.root.parent().unwrap_or_else(|| Path::new("."));
        let owner = tempfile::Builder::new()
            .prefix(".agit-bisect-")
            .tempdir_in(parent)?;
        let scratch = owner.path().join("workspace");
        fork_workspace_excluding(
            &self.root,
            &scratch,
            &[Path::new(WORKSPACE_FILE), Path::new(FORK_FILE)],
        )?;
        let (scratch_repository, scratch_head) = AgitRepository::watch(&scratch)?;
        let operation = (|| {
            anyhow::ensure!(
                self.snapshots_match_fork(&boundary, &scratch_repository, &scratch_head)?,
                "source changed while the bisect scratch workspace was being created"
            );
            self.bisect_search(
                &scratch,
                boundary,
                &candidates,
                good_index,
                bad_index,
                command,
            )
        })();
        let cleanup = scratch_repository.forget_internal(true, false);
        match (operation, cleanup) {
            (Ok(outcome), Ok(())) => Ok(outcome),
            (Err(error), Ok(())) => Err(error),
            (Ok(_), Err(error)) => {
                Err(error.context("bisect succeeded but scratch cleanup failed"))
            }
            (Err(error), Err(cleanup)) => Err(error.context(format!(
                "bisect failed; scratch cleanup also failed: {cleanup:#}"
            ))),
        }
    }

    pub fn remove_fork(&mut self, name: &str, keep_files: bool) -> anyhow::Result<ForkRemoval> {
        validate_fork_name(name)?;
        let record_path = self.forks_dir().join(format!("{name}.json"));
        let fork: ForkSummary = serde_json::from_slice(
            &fs::read(&record_path).with_context(|| format!("fork `{name}` was not found"))?,
        )?;
        if !keep_files && fork.destination.exists() {
            let metadata = fs::symlink_metadata(&fork.destination)?;
            anyhow::ensure!(
                metadata.is_dir() && !metadata.file_type().is_symlink(),
                "refusing to remove a fork path that is not a real directory"
            );
            let destination = fork.destination.canonicalize()?;
            anyhow::ensure!(
                destination != self.root
                    && !destination.starts_with(&self.root)
                    && !self.root.starts_with(&destination),
                "refusing to remove an unsafe fork destination"
            );
            let fork_repository = AgitRepository::open(&destination)
                .context("fork identity is missing or no longer matches its recorded directory")?;
            fork_repository.forget(true)?;
            fs::remove_dir_all(&destination)?;
            if let Some(parent) = destination.parent() {
                File::open(parent)?.sync_all()?;
            }
        }
        fs::remove_file(&record_path)?;
        File::open(self.forks_dir())?.sync_all()?;
        Ok(ForkRemoval {
            name: name.to_owned(),
            destination: fork.destination,
            files_removed: !keep_files,
        })
    }

    pub fn merge(
        &mut self,
        fork_name: &str,
        check: Option<&str>,
        dry_run: bool,
    ) -> anyhow::Result<MergeOutcome> {
        let mutation = self.acquire_mutation_lock()?;
        let fork = self
            .forks()?
            .into_iter()
            .find(|fork| fork.name == fork_name)
            .with_context(|| format!("fork `{fork_name}` was not found"))?;
        anyhow::ensure!(fork.destination.exists(), "fork directory no longer exists");

        let mut fork_repository = AgitRepository::open(&fork.destination)?;
        let theirs = fork_repository.snapshot(
            Some(format!("merge source for {fork_name}")),
            SnapshotTrigger::MergeSource,
        )?;
        let ours = self.snapshot(
            Some(format!("before merge from {fork_name}")),
            SnapshotTrigger::PreMerge,
        )?;
        let base = parse_id(&fork.base_snapshot)?;
        let ours_snapshot: Snapshot = self.store.read_struct(&ours, ObjectKind::Snapshot)?;
        let theirs_snapshot: Snapshot = self.store.read_struct(&theirs, ObjectKind::Snapshot)?;
        let base_entries = self.snapshot_entry_map(&base)?;
        let ours_entries = self.snapshot_entry_map(&ours)?;
        let theirs_entries = self.snapshot_entry_map(&theirs)?;
        let mut merge_plan = merge::plan(&base_entries, &ours_entries, &theirs_entries);
        let policy = CapturePolicy::load(&self.root)?;
        merge_plan
            .changes
            .retain(|change| !policy.excludes_bytes(&change.path));
        merge_plan
            .conflicts
            .retain(|conflict| !policy.excludes_bytes(&conflict.path));
        let ours_tree = id_hex(&ours_snapshot.root_tree);
        let theirs_tree = id_hex(&theirs_snapshot.root_tree);
        let preview_digest =
            merge_preview_digest(&fork.base_snapshot, &ours_tree, &theirs_tree, &merge_plan);
        let mut outcome = MergeOutcome {
            fork: fork_name.to_owned(),
            base_snapshot: id_hex(&base),
            ours_snapshot: id_hex(&ours),
            theirs_snapshot: id_hex(&theirs),
            ours_tree,
            theirs_tree,
            result_snapshot: None,
            changes: merge_plan.changes.len(),
            conflicts: merge_plan.conflicts,
            check: check.map(str::to_owned),
            check_output: None,
            preview_digest,
        };
        if dry_run || !outcome.conflicts.is_empty() {
            FileExt::unlock(&mutation)?;
            return Ok(outcome);
        }
        let check = check.context("merge requires --check <command>")?;
        anyhow::ensure!(
            !check.trim().is_empty(),
            "merge check command cannot be empty"
        );

        let (rewind_plan, target_entries) = merge_rewind_plan(&ours_entries, &merge_plan.changes);
        let scratch_parent = self.root.parent().unwrap_or_else(|| Path::new("."));
        let scratch_owner = tempfile::Builder::new()
            .prefix(".agit-merge-")
            .tempdir_in(scratch_parent)?;
        let scratch = scratch_owner.path().join("workspace");
        fork_workspace_excluding(
            &self.root,
            &scratch,
            &[Path::new(WORKSPACE_FILE), Path::new(FORK_FILE)],
        )?;
        let scratch_identity = scratch.join(WORKSPACE_FILE);
        if scratch_identity.exists() {
            fs::remove_file(scratch_identity)?;
        }
        self.apply_plan_at(&scratch, &target_entries, &rewind_plan, &[])?;
        let verification = std::process::Command::new("/bin/sh")
            .args(["-c", check])
            .current_dir(&scratch)
            .output()
            .with_context(|| format!("run merge check `{check}`"))?;
        let check_output = command_output(&verification.stdout, &verification.stderr);
        anyhow::ensure!(
            verification.status.success(),
            "merge check failed with {}\n{}",
            verification.status,
            check_output
        );
        outcome.check_output = Some(check_output);

        let ours_snapshot: Snapshot = self.store.read_struct(&ours, ObjectKind::Snapshot)?;
        let current_root = self.capture_root_retry()?;
        anyhow::ensure!(
            current_root == ours_snapshot.root_tree,
            "source workspace changed while the merge was being verified; retry"
        );
        self.write_restore_intent(&RestoreIntent {
            pre_snapshot: ours,
            target_snapshot: theirs,
            paths: Vec::new(),
        })?;

        let applied = self
            .apply_plan_at(&self.root, &target_entries, &rewind_plan, &[])
            .and_then(|_| {
                self.invalidate_path_index()?;
                self.snapshot_internal(
                    Some(format!("merged fork {fork_name}")),
                    SnapshotTrigger::Merge,
                    None,
                    vec![ours, theirs],
                )
            });
        let result = match applied {
            Ok(result) => result,
            Err(error) => {
                let rollback_plan = self.plan_rewind(&ours, &[])?;
                let rollback_entries =
                    self.entries_for_plan(&ours_snapshot.root_tree, &rollback_plan)?;
                self.apply_plan_at(&self.root, &rollback_entries, &rollback_plan, &[])
                    .context("merge failed and rollback also failed")?;
                self.invalidate_path_index()?;
                self.clear_restore_intent()?;
                FileExt::unlock(&mutation)?;
                return Err(error.context("merge aborted; source workspace was restored"));
            }
        };
        self.clear_restore_intent()?;
        FileExt::unlock(&mutation)?;
        outcome.result_snapshot = Some(id_hex(&result));
        Ok(outcome)
    }

    fn forks_dir(&self) -> PathBuf {
        self.workspace_data_dir().join("forks")
    }

    fn sync_config_path(&self) -> PathBuf {
        self.workspace_data_dir().join("sync/config.json")
    }

    fn active_claims(&self) -> anyhow::Result<Vec<ClaimRecord>> {
        if !claims::registry_path(self.store.root(), &self.family_id).exists() {
            return Ok(Vec::new());
        }
        claims::Registry::open(self.store.root(), &self.family_id)?.active()
    }

    fn bisect_search(
        &self,
        scratch: &Path,
        boundary: ObjectId,
        candidates: &[(ObjectId, Option<String>)],
        mut good_index: usize,
        mut bad_index: usize,
        command: &[OsString],
    ) -> anyhow::Result<BisectOutcome> {
        let mut current = boundary;
        let mut checks = BTreeMap::new();
        anyhow::ensure!(
            self.ensure_bisect_check(
                scratch,
                &mut current,
                candidates,
                good_index,
                command,
                &mut checks,
            )?,
            "selected good snapshot fails the bisect command"
        );
        anyhow::ensure!(
            !self.ensure_bisect_check(
                scratch,
                &mut current,
                candidates,
                bad_index,
                command,
                &mut checks,
            )?,
            "selected bad snapshot passes the bisect command"
        );

        while bad_index - good_index > 1 {
            let middle = good_index + (bad_index - good_index) / 2;
            if self.ensure_bisect_check(
                scratch,
                &mut current,
                candidates,
                middle,
                command,
                &mut checks,
            )? {
                good_index = middle;
            } else {
                bad_index = middle;
            }
        }

        Ok(BisectOutcome {
            candidates: candidates.len(),
            good_snapshot: id_hex(&candidates[good_index].0),
            first_bad_snapshot: id_hex(&candidates[bad_index].0),
            checks: checks.into_values().collect(),
        })
    }

    #[allow(clippy::too_many_arguments)]
    fn ensure_bisect_check(
        &self,
        scratch: &Path,
        current: &mut ObjectId,
        candidates: &[(ObjectId, Option<String>)],
        index: usize,
        command: &[OsString],
        checks: &mut BTreeMap<usize, BisectCheck>,
    ) -> anyhow::Result<bool> {
        if let Some(check) = checks.get(&index) {
            return Ok(check.passed);
        }
        let (snapshot, label) = &candidates[index];
        if current != snapshot {
            self.apply_snapshot_transition_at(scratch, current, snapshot)?;
            *current = *snapshot;
        }

        let parent = scratch.parent().context("bisect scratch has no parent")?;
        let owner = tempfile::Builder::new()
            .prefix("probe-")
            .tempdir_in(parent)?;
        let probe = owner.path().join("workspace");
        let fork = fork_workspace_excluding(
            scratch,
            &probe,
            &[Path::new(WORKSPACE_FILE), Path::new(FORK_FILE)],
        )?;
        let started = std::time::Instant::now();
        let (program, arguments) = command
            .split_first()
            .context("bisect command cannot be empty")?;
        let status = std::process::Command::new(program)
            .args(arguments)
            .current_dir(&probe)
            .stdin(Stdio::null())
            .stdout(Stdio::null())
            .stderr(Stdio::null())
            .status()
            .with_context(|| format!("run bisect command at {}", &id_hex(snapshot)[..12]))?;
        use std::os::unix::process::ExitStatusExt;
        let check = BisectCheck {
            snapshot: id_hex(snapshot),
            label: label.clone(),
            passed: status.success(),
            exit_code: status.code(),
            signal: status.signal(),
            elapsed_ms: started.elapsed().as_millis().min(u64::MAX as u128) as u64,
            probe_fork_ms: fork.elapsed.as_millis().min(u64::MAX as u128) as u64,
        };
        let passed = check.passed;
        checks.insert(index, check);
        Ok(passed)
    }

    fn apply_snapshot_transition_at(
        &self,
        root: &Path,
        current: &ObjectId,
        target: &ObjectId,
    ) -> anyhow::Result<()> {
        let current: Snapshot = self.store.read_struct(current, ObjectKind::Snapshot)?;
        let target_snapshot: Snapshot = self.store.read_struct(target, ObjectKind::Snapshot)?;
        let mut changes = Vec::new();
        self.diff_directory(
            Some(current.root_tree),
            Some(target_snapshot.root_tree),
            Vec::new(),
            &[],
            &mut changes,
        )?;
        let current_policy = CapturePolicy::from_rules(&current.excluded_paths)?;
        let target_policy = CapturePolicy::from_rules(&target_snapshot.excluded_paths)?;
        let protected = current_policy.union(&target_policy);
        changes.retain(|change| !protected.excludes_bytes(&change.raw_path));
        let target_id = id_hex(target);
        let current_tree = id_hex(&current.root_tree);
        let plan = RewindPlan {
            preview_digest: rewind_preview_digest(&target_id, Some(&current_tree), &changes),
            target: target_id,
            current_tree: Some(current_tree),
            changes,
        };
        let entries = self.entries_for_plan(&target_snapshot.root_tree, &plan)?;
        self.apply_plan_at(root, &entries, &plan, &[])
    }

    fn diff_snapshot_pair(
        &self,
        target: String,
        base: ObjectId,
        head: ObjectId,
    ) -> anyhow::Result<DiffSummary> {
        let base_snapshot: Snapshot = self.store.read_struct(&base, ObjectKind::Snapshot)?;
        let head_snapshot: Snapshot = self.store.read_struct(&head, ObjectKind::Snapshot)?;
        let mut raw = Vec::new();
        self.diff_directory(
            Some(base_snapshot.root_tree),
            Some(head_snapshot.root_tree),
            Vec::new(),
            &[],
            &mut raw,
        )?;
        let mut changes = Vec::with_capacity(raw.len());
        for change in raw {
            let before = self.lookup_tree_path(&base_snapshot.root_tree, &change.raw_path)?;
            let after = self.lookup_tree_path(&head_snapshot.root_tree, &change.raw_path)?;
            if directory_target_only_difference(before.as_ref(), after.as_ref()) {
                continue;
            }
            changes.push(DiffChange {
                path: change.path,
                action: match change.action {
                    "restore" => "add",
                    "remove" => "delete",
                    "replace" => "modify",
                    _ => unreachable!(),
                },
            });
        }
        Ok(DiffSummary {
            target,
            base_snapshot: id_hex(&base),
            target_snapshot: id_hex(&head),
            changes,
        })
    }

    fn sync_incoming_path(&self) -> PathBuf {
        self.workspace_data_dir().join("sync/incoming.json")
    }

    fn sync_state_path(&self) -> PathBuf {
        self.workspace_data_dir().join("sync/state.json")
    }

    fn read_sync_state(&self) -> anyhow::Result<Option<SyncState>> {
        let path = self.sync_state_path();
        if !path.exists() {
            return Ok(None);
        }
        Ok(Some(serde_json::from_slice(&fs::read(path)?)?))
    }

    fn write_sync_state(&self, remote_root: ObjectId) -> anyhow::Result<()> {
        let path = self.sync_state_path();
        if let Some(parent) = path.parent() {
            fs::create_dir_all(parent)?;
        }
        atomic_write(&path, &serde_json::to_vec(&SyncState { remote_root })?)
    }

    fn clear_sync_incoming(&self) -> anyhow::Result<()> {
        let path = self.sync_incoming_path();
        if path.exists() {
            fs::remove_file(&path)?;
            File::open(path.parent().context("sync root has no parent")?)?.sync_all()?;
        }
        Ok(())
    }

    fn snapshot_entry_map(
        &self,
        snapshot_id: &ObjectId,
    ) -> anyhow::Result<BTreeMap<Vec<u8>, TreeEntry>> {
        let snapshot: Snapshot = self.store.read_struct(snapshot_id, ObjectKind::Snapshot)?;
        let mut entries: BTreeMap<Vec<u8>, TreeEntry> = self
            .flatten_tree(&snapshot.root_tree)?
            .into_iter()
            .map(|(path, entry)| (path, entry.entry))
            .collect();
        // The pointer file is already excluded from snapshots; the containing
        // directory mtime is likewise transport metadata, while its policy and
        // hook children remain ordinary merge inputs.
        entries.remove(b".agit".as_slice());
        // Git remains canonical for repository history. Fork refs, indexes,
        // worktree locks, and object-store mutations are never merged as files.
        entries.retain(|path, _| path != b".git" && !path.starts_with(b".git/"));
        Ok(entries)
    }

    fn snapshots_match_fork(
        &self,
        base: &ObjectId,
        fork_repository: &AgitRepository,
        fork_head: &ObjectId,
    ) -> anyhow::Result<bool> {
        let base_snapshot: Snapshot = self.store.read_struct(base, ObjectKind::Snapshot)?;
        let fork_snapshot: Snapshot = fork_repository
            .store
            .read_struct(fork_head, ObjectKind::Snapshot)?;
        let mut base_entries = self.flatten_tree(&base_snapshot.root_tree)?;
        let mut fork_entries = fork_repository.flatten_tree(&fork_snapshot.root_tree)?;

        // Attaching the destination updates the .agit directory timestamp, but
        // its actual policy and hook children still participate in comparison.
        for internal in [b".agit".as_slice(), WORKSPACE_FILE_BYTES, FORK_FILE_BYTES] {
            base_entries.remove(internal);
            fork_entries.remove(internal);
        }
        base_entries.retain(|_, entry| entry.entry.kind != EntryKind::SocketMarker);
        fork_entries.retain(|_, entry| entry.entry.kind != EntryKind::SocketMarker);
        Ok(base_entries
            .iter()
            .map(|(path, entry)| (path, &entry.entry))
            .eq(fork_entries
                .iter()
                .map(|(path, entry)| (path, &entry.entry))))
    }

    pub fn resolve_snapshot(&self, value: &str) -> anyhow::Result<ObjectId> {
        if value.len() == 64 {
            let id = parse_id(value)?;
            self.store.read_bytes(&id, ObjectKind::Snapshot)?;
            return Ok(id);
        }
        anyhow::ensure!(
            value.len() >= 8,
            "snapshot prefix must contain at least 8 characters"
        );
        let matches: Vec<ObjectId> = self
            .store
            .timeline(&self.workspace_id, 100_000)?
            .into_iter()
            .filter(|row| id_hex(&row.id).starts_with(value))
            .map(|row| row.id)
            .collect();
        match matches.as_slice() {
            [id] => Ok(*id),
            [] => bail!("snapshot `{value}` was not found"),
            _ => bail!("snapshot prefix `{value}` is ambiguous"),
        }
    }

    pub fn plan_rewind(&self, target: &ObjectId, paths: &[PathBuf]) -> anyhow::Result<RewindPlan> {
        let _maintenance = self.store.acquire_maintenance_shared()?;
        let materialization = self.materialization(target)?;
        let unavailable: Vec<_> = materialization
            .missing_paths
            .iter()
            .filter(|missing| selected(&missing.raw_path, paths))
            .collect();
        anyhow::ensure!(
            unavailable.is_empty(),
            "snapshot is partial and cannot restore {} selected path(s): {}",
            unavailable.len(),
            unavailable
                .iter()
                .map(|missing| missing.path.as_str())
                .collect::<Vec<_>>()
                .join(", ")
        );
        let target_snapshot: Snapshot = self.store.read_struct(target, ObjectKind::Snapshot)?;
        let current_policy = CapturePolicy::load(&self.root)?;
        let target_policy = CapturePolicy::from_rules(&target_snapshot.excluded_paths)?;
        let protected = current_policy.union(&target_policy);
        let current_tree = self.capture_root_retry_with_policy(&current_policy)?;
        let mut changes = Vec::new();
        self.diff_directory(
            Some(current_tree),
            Some(target_snapshot.root_tree),
            Vec::new(),
            paths,
            &mut changes,
        )?;
        changes.retain(|change| !protected.excludes_bytes(&change.raw_path));
        let target_id = id_hex(target);
        let current_tree = id_hex(&current_tree);
        Ok(RewindPlan {
            preview_digest: rewind_preview_digest(&target_id, Some(&current_tree), &changes),
            target: target_id,
            current_tree: Some(current_tree),
            changes,
        })
    }

    pub fn rewind(
        &mut self,
        target: &ObjectId,
        paths: &[PathBuf],
        sqlite_consistent: bool,
    ) -> anyhow::Result<(ObjectId, RewindPlan)> {
        let lock = self.acquire_mutation_lock()?;
        let plan = self.plan_rewind(target, paths)?;
        let pre = self.snapshot(
            Some(format!("before rewind to {}", &id_hex(target)[..12])),
            SnapshotTrigger::PreRewind,
        )?;
        let pre_snapshot: Snapshot = self.store.read_struct(&pre, ObjectKind::Snapshot)?;
        let target_snapshot: Snapshot = self.store.read_struct(target, ObjectKind::Snapshot)?;
        let pre_entries = self.entries_for_changed_paths(&pre_snapshot.root_tree, &plan)?;
        let target_entries = self.entries_for_plan(&target_snapshot.root_tree, &plan)?;
        test_pause("AGIT_TEST_REWIND_PAUSE_BEFORE_PRECONDITION_MS");
        if let Err(error) = self.verify_plan_state(&self.root, &pre_entries, &plan, paths) {
            FileExt::unlock(&lock)?;
            return Err(error
                .context("rewind cancelled before mutation; concurrent changes remain untouched"));
        }
        self.write_restore_intent(&RestoreIntent {
            pre_snapshot: pre,
            target_snapshot: *target,
            paths: paths
                .iter()
                .map(|path| path.as_os_str().as_bytes().to_vec())
                .collect(),
        })?;

        let result = self
            .apply_plan_at(&self.root, &target_entries, &plan, paths)
            .and_then(|_| {
                test_pause("AGIT_TEST_REWIND_PAUSE_AFTER_APPLY_MS");
                self.verify_plan_state(&self.root, &target_entries, &plan, paths)?;
                if sqlite_consistent {
                    self.restore_sqlite_backups(&target_snapshot.sqlite_backups, paths)?;
                }
                Ok(())
            });
        if let Err(error) = result {
            let rescue = self
                .snapshot(
                    Some("rewind interference rescue".to_owned()),
                    SnapshotTrigger::Inspection,
                )
                .ok();
            let rollback_plan = self.plan_rewind(&pre, paths)?;
            let pre_entries = self.entries_for_plan(&pre_snapshot.root_tree, &rollback_plan)?;
            self.apply_plan_at(&self.root, &pre_entries, &rollback_plan, paths)
                .context("rewind failed and rollback also failed")?;
            self.verify_plan_state(&self.root, &pre_entries, &rollback_plan, paths)
                .context("rewind rollback did not converge to the pre-rewind state")?;
            self.invalidate_path_index()?;
            self.clear_restore_intent()?;
            FileExt::unlock(&lock)?;
            let detail = rescue.map_or_else(
                || "rewind aborted; the pre-rewind state was restored".to_owned(),
                |snapshot| {
                    format!(
                        "rewind aborted; interference was preserved as snapshot {}; the pre-rewind state was restored",
                        id_hex(&snapshot)
                    )
                },
            );
            return Err(error.context(detail));
        }
        self.invalidate_path_index()?;
        self.clear_restore_intent()?;
        FileExt::unlock(&lock)?;
        Ok((pre, plan))
    }

    pub fn forget(self, purge: bool) -> anyhow::Result<()> {
        self.forget_internal(purge, true)
    }

    fn forget_internal(mut self, purge: bool, announce: bool) -> anyhow::Result<()> {
        self.stop_watcher()?;
        if claims::registry_path(self.store.root(), &self.family_id).exists() {
            claims::Registry::open(self.store.root(), &self.family_id)?
                .release_workspace(&self.workspace_id)?;
        }
        let _maintenance = self.store.acquire_maintenance_exclusive()?;
        self.store.detach_workspace(&self.workspace_id)?;
        let workspace_file = self.root.join(WORKSPACE_FILE);
        if workspace_file.exists() {
            fs::remove_file(&workspace_file)?;
            File::open(
                workspace_file
                    .parent()
                    .context("workspace file has no parent")?,
            )?
            .sync_all()?;
        }
        if purge {
            self.store.purge_workspace(&self.workspace_id)?;
            if announce {
                eprintln!("workspace detached; unreachable data will be removed by `agit gc`");
            }
        }
        Ok(())
    }

    fn watcher_running(&self) -> bool {
        let pid_path = self.workspace_data_dir().join("daemon.pid");
        let Ok(pid) = fs::read_to_string(pid_path) else {
            return false;
        };
        let Ok(pid) = pid.trim().parse::<i32>() else {
            return false;
        };
        unsafe { libc::kill(pid, 0) == 0 }
    }

    fn stop_watcher(&self) -> anyhow::Result<()> {
        let pid_path = self.workspace_data_dir().join("daemon.pid");
        if let Ok(pid) = fs::read_to_string(&pid_path) {
            if let Ok(pid) = pid.trim().parse::<i32>() {
                let result = unsafe { libc::kill(pid, libc::SIGTERM) };
                if result != 0 {
                    let error = std::io::Error::last_os_error();
                    if error.raw_os_error() != Some(libc::ESRCH) {
                        return Err(error.into());
                    }
                }
            }
        }
        if pid_path.exists() {
            fs::remove_file(pid_path)?;
        }
        Ok(())
    }

    fn capture_root_retry(&self) -> anyhow::Result<ObjectId> {
        let policy = CapturePolicy::load(&self.root)?;
        self.capture_root_retry_with_policy(&policy)
    }

    fn capture_root_retry_with_policy(&self, policy: &CapturePolicy) -> anyhow::Result<ObjectId> {
        let mut index = self.open_path_index()?;
        let mut last_error = None;
        for _ in 0..3 {
            index.begin()?;
            index.reset()?;
            match self.capture_directory_impl(&self.root, &index, policy) {
                Ok(id) => {
                    index.set_root(&id, &self.store_generation()?)?;
                    index.commit()?;
                    return Ok(id);
                }
                Err(error) if is_not_found(&error) => {
                    index.rollback()?;
                    last_error = Some(error);
                }
                Err(error) => {
                    index.rollback()?;
                    return Err(error);
                }
            }
        }
        Err(last_error
            .context("capture retry failed without an error")?
            .context("workspace kept changing while it was captured"))
    }

    fn capture_changed_paths_retry_with_policy(
        &self,
        changed_paths: &[PathBuf],
        policy: &CapturePolicy,
    ) -> anyhow::Result<ObjectId> {
        let mut index = self.open_path_index()?;
        let generation = self.store_generation()?;
        if index.root(&generation)?.is_none() {
            return self.capture_root_retry_with_policy(policy);
        }

        let mut last_error = None;
        for _ in 0..3 {
            index.begin()?;
            match self.capture_changed_paths_impl(&index, changed_paths, policy) {
                Ok(id) => {
                    index.set_root(&id, &generation)?;
                    index.commit()?;
                    return Ok(id);
                }
                Err(error) if is_not_found(&error) => {
                    index.rollback()?;
                    last_error = Some(error);
                }
                Err(error) => {
                    index.rollback()?;
                    return Err(error);
                }
            }
        }
        Err(last_error
            .context("incremental capture retry failed without an error")?
            .context("workspace kept changing while its delta was captured"))
    }

    fn capture_changed_paths_impl(
        &self,
        index: &PathIndex,
        changed_paths: &[PathBuf],
        policy: &CapturePolicy,
    ) -> anyhow::Result<ObjectId> {
        let mut paths = BTreeSet::new();
        for changed in changed_paths {
            let absolute = if changed.is_absolute() {
                changed.clone()
            } else {
                self.root.join(changed)
            };
            let Ok(relative) = absolute.strip_prefix(&self.root) else {
                continue;
            };
            let relative = relative.as_os_str().as_bytes().to_vec();
            if relative.is_empty() {
                index.reset()?;
                return self.capture_directory_impl(&self.root, index, policy);
            }
            if is_identity_path(&relative) {
                continue;
            }
            validate_relative_path(&relative)?;
            paths.insert(relative);
        }

        let Some(existing_root) = index.root(&self.store_generation()?)? else {
            bail!("path index was invalidated during incremental capture")
        };
        if paths.is_empty() {
            return Ok(existing_root);
        }

        let mut dirty_directories = BTreeSet::new();
        for relative in paths {
            index.remove_subtree(&relative)?;
            let absolute = safe_join(&self.root, &relative)?;
            if !policy.excludes_bytes(&relative) && fs::symlink_metadata(&absolute).is_ok() {
                if let Some(entry) = self.capture_path_entry(&absolute, index, policy)? {
                    let parent = relative_parent(&relative);
                    index.upsert(&relative, &parent, &entry)?;
                }
            }
            let mut parent = relative_parent(&relative);
            loop {
                dirty_directories.insert(parent.clone());
                if parent.is_empty() {
                    break;
                }
                parent = relative_parent(&parent);
            }
        }

        let mut dirty_directories: Vec<_> = dirty_directories.into_iter().collect();
        dirty_directories.sort_by(|left, right| {
            path_depth(right)
                .cmp(&path_depth(left))
                .then_with(|| right.cmp(left))
        });
        let mut root_tree = existing_root;
        for relative in dirty_directories {
            if policy.excludes_bytes(&relative) {
                index.remove_subtree(&relative)?;
                continue;
            }
            let absolute = safe_join(&self.root, &relative)?;
            let metadata = match fs::symlink_metadata(&absolute) {
                Ok(metadata) => metadata,
                Err(error) if error.kind() == std::io::ErrorKind::NotFound => {
                    if !relative.is_empty() {
                        index.remove_subtree(&relative)?;
                    }
                    continue;
                }
                Err(error) => {
                    return Err(error)
                        .with_context(|| format!("stat indexed directory {}", absolute.display()))
                }
            };
            if !metadata.is_dir() || metadata.file_type().is_symlink() {
                continue;
            }
            let tree_id = self.rebuild_indexed_directory(index, &relative)?;
            if relative.is_empty() {
                root_tree = tree_id;
            } else {
                let entry = self.directory_entry(&absolute, tree_id)?;
                let parent = relative_parent(&relative);
                index.upsert(&relative, &parent, &entry)?;
            }
        }
        Ok(root_tree)
    }

    fn capture_directory_impl(
        &self,
        path: &Path,
        index: &PathIndex,
        policy: &CapturePolicy,
    ) -> anyhow::Result<ObjectId> {
        let relative = path.strip_prefix(&self.root)?.as_os_str().as_bytes();
        let children = SortedDirectory::open(path)
            .with_context(|| format!("read directory {}", path.display()))?;
        let mut tree = tree::Builder::new(&self.store);

        for child_name in children {
            let child_name = child_name?;
            let child_path = path.join(&child_name);
            if child_path.starts_with(self.store.root()) {
                continue;
            }
            if child_path == self.root.join(WORKSPACE_FILE)
                || child_path == self.root.join(FORK_FILE)
            {
                continue;
            }
            let child_relative = child_path
                .strip_prefix(&self.root)?
                .as_os_str()
                .as_bytes()
                .to_vec();
            if policy.excludes_bytes(&child_relative) {
                continue;
            }
            let Some(entry) = self.capture_path_entry(&child_path, index, policy)? else {
                continue;
            };
            index.upsert(&child_relative, relative, &entry)?;
            tree.push(entry)?;
        }
        let tree_id = tree.finish()?;
        self.store
            .cache_directory(&self.workspace_id, relative, &tree_id)?;
        Ok(tree_id)
    }

    fn capture_path_entry(
        &self,
        path: &Path,
        index: &PathIndex,
        policy: &CapturePolicy,
    ) -> anyhow::Result<Option<TreeEntry>> {
        let metadata =
            fs::symlink_metadata(path).with_context(|| format!("stat {}", path.display()))?;
        let file_type = metadata.file_type();
        let (secs, nanos) = metadata_time(&metadata);
        let name = path
            .file_name()
            .context("captured path has no filename")?
            .as_bytes()
            .to_vec();
        let mode = metadata.permissions().mode();
        let relative = path.strip_prefix(&self.root)?.as_os_str().as_bytes();
        let class = content_class::classify(relative);
        let xattrs = if file_type.is_file() || file_type.is_dir() {
            self.capture_xattrs(path)
                .with_context(|| format!("capture xattrs for {}", path.display()))?
        } else {
            None
        };

        let entry = if file_type.is_dir() {
            let target = self
                .capture_directory_impl(path, index, policy)
                .with_context(|| format!("capture directory {}", path.display()))?;
            TreeEntry {
                name,
                kind: EntryKind::Directory,
                target: Some(target),
                link_target: Vec::new(),
                mode,
                size: 0,
                mtime_secs: secs,
                mtime_nanos: nanos,
                xattrs,
                class,
            }
        } else if file_type.is_file() {
            let target = self
                .capture_file(path, Some(relative))
                .with_context(|| format!("capture file {}", path.display()))?;
            TreeEntry {
                name,
                kind: EntryKind::File,
                target: Some(target),
                link_target: Vec::new(),
                mode,
                size: metadata.len(),
                mtime_secs: secs,
                mtime_nanos: nanos,
                xattrs,
                class,
            }
        } else if file_type.is_symlink() {
            TreeEntry {
                name,
                kind: EntryKind::Symlink,
                target: None,
                link_target: fs::read_link(path)?.as_os_str().as_bytes().to_vec(),
                mode,
                size: metadata.len(),
                mtime_secs: secs,
                mtime_nanos: nanos,
                xattrs: None,
                class,
            }
        } else if file_type.is_fifo() {
            special_entry(name, EntryKind::Fifo, mode, secs, nanos)
        } else if file_type.is_socket() {
            special_entry(name, EntryKind::SocketMarker, mode, secs, nanos)
        } else {
            eprintln!(
                "warning: unsupported special file skipped: {}",
                path.display()
            );
            return Ok(None);
        };
        Ok(Some(entry))
    }

    fn directory_entry(&self, path: &Path, target: ObjectId) -> anyhow::Result<TreeEntry> {
        let metadata = fs::symlink_metadata(path)?;
        let (mtime_secs, mtime_nanos) = metadata_time(&metadata);
        Ok(TreeEntry {
            name: path
                .file_name()
                .context("directory has no filename")?
                .as_bytes()
                .to_vec(),
            kind: EntryKind::Directory,
            target: Some(target),
            link_target: Vec::new(),
            mode: metadata.permissions().mode(),
            size: 0,
            mtime_secs,
            mtime_nanos,
            xattrs: self.capture_xattrs(path)?,
            class: content_class::classify(path.strip_prefix(&self.root)?.as_os_str().as_bytes()),
        })
    }

    fn rebuild_indexed_directory(
        &self,
        index: &PathIndex,
        relative: &[u8],
    ) -> anyhow::Result<ObjectId> {
        let mut tree = tree::Builder::new(&self.store);
        let mut after_name: Option<Vec<u8>> = None;
        loop {
            let entries = index.children_after(relative, after_name.as_deref(), CHILD_BATCH)?;
            if entries.is_empty() {
                break;
            }
            for entry in entries {
                after_name = Some(entry.name.clone());
                tree.push(entry)?;
            }
        }
        let tree_id = tree.finish()?;
        self.store
            .cache_directory(&self.workspace_id, relative, &tree_id)?;
        Ok(tree_id)
    }

    fn open_path_index(&self) -> anyhow::Result<PathIndex> {
        PathIndex::open(&self.workspace_data_dir().join("paths.sqlite3"))
    }

    fn store_generation(&self) -> anyhow::Result<String> {
        Ok(format!(
            "{}:tree-entry-class-v1",
            fs::read_to_string(self.store.root().join("packs/CURRENT"))?.trim()
        ))
    }

    fn invalidate_path_index(&self) -> anyhow::Result<()> {
        let mut index = self.open_path_index()?;
        index.begin()?;
        index.reset()?;
        index.commit()
    }

    fn capture_file(&self, path: &Path, cache_key: Option<&[u8]>) -> anyhow::Result<ObjectId> {
        for _ in 0..3 {
            let file = File::open(path).with_context(|| format!("open {}", path.display()))?;
            let before = file
                .metadata()
                .with_context(|| format!("stat open file {}", path.display()))?;
            if let Some(key) = cache_key {
                if let Some(cached) = self.store.cached_file(&self.workspace_id, key)? {
                    if cached_matches(&cached, &before) {
                        return Ok(cached.blob_id);
                    }
                }
            }
            let mut stream = ChunkStream::new(BufReader::with_capacity(256 * 1024, file));
            let mut chunks = Vec::new();
            let mut total_len = 0_u64;
            while let Some(chunk) = stream.next_chunk()? {
                let id = self.store.put_bytes(ObjectKind::Chunk, &chunk)?;
                total_len += chunk.len() as u64;
                chunks.push(ChunkRef {
                    id,
                    len: chunk.len() as u32,
                });
            }
            let after = fs::metadata(path).with_context(|| format!("restat {}", path.display()))?;
            if stable_metadata(&before, &after) {
                let blob_id = self
                    .store
                    .put_struct(ObjectKind::Blob, &Blob { chunks, total_len })?;
                if let Some(key) = cache_key {
                    self.store.cache_file(
                        &self.workspace_id,
                        key,
                        &cached_from_metadata(&after, blob_id),
                    )?;
                }
                return Ok(blob_id);
            }
        }
        bail!(
            "file changed repeatedly while being captured: {}",
            path.display()
        )
    }

    fn capture_xattrs(&self, path: &Path) -> anyhow::Result<Option<ObjectId>> {
        let mut entries = Vec::new();
        match xattr::list(path) {
            Ok(names) => {
                for name in names {
                    if let Some(value) = xattr::get(path, &name)
                        .with_context(|| format!("read xattr {:?} on {}", name, path.display()))?
                    {
                        entries.push(XattrEntry {
                            name: name.as_os_str().as_bytes().to_vec(),
                            value,
                        });
                    }
                }
            }
            Err(error) if error.kind() == std::io::ErrorKind::PermissionDenied => return Ok(None),
            Err(error) => return Err(error.into()),
        }
        entries.sort_by(|a, b| a.name.cmp(&b.name));
        if entries.is_empty() {
            Ok(None)
        } else {
            Ok(Some(
                self.store
                    .put_struct(ObjectKind::Xattrs, &Xattrs { entries })?,
            ))
        }
    }

    fn capture_sqlite_backups(&self, policy: &CapturePolicy) -> anyhow::Result<Vec<SqliteBackup>> {
        let mut candidates = Vec::new();
        collect_sqlite_candidates(&self.root, &self.root, policy, &mut candidates)?;
        let mut backups = Vec::new();
        let temp_dir = self.store.root().join("tmp");
        for path in candidates {
            match sqlite_adapter::consistent_backup(&path, &temp_dir) {
                Ok(backup) => {
                    let blob = self.capture_file(backup.file.path(), None)?;
                    let relative = path
                        .strip_prefix(&self.root)?
                        .as_os_str()
                        .as_bytes()
                        .to_vec();
                    backups.push(SqliteBackup {
                        path: relative,
                        blob,
                        integrity_ok: backup.integrity_ok,
                    });
                }
                Err(error) => {
                    eprintln!(
                        "warning: SQLite consistent backup unavailable for {}: {error:#}; raw bytes remain protected",
                        path.display()
                    );
                }
            }
        }
        backups.sort_by(|a, b| a.path.cmp(&b.path));
        Ok(backups)
    }

    fn restore_sqlite_backups(
        &self,
        backups: &[SqliteBackup],
        selected_paths: &[PathBuf],
    ) -> anyhow::Result<()> {
        for backup in backups {
            if !selected(&backup.path, selected_paths) {
                continue;
            }
            anyhow::ensure!(
                backup.integrity_ok,
                "refusing a SQLite backup that failed integrity_check"
            );
            let destination = safe_join(&self.root, &backup.path)?;
            let entry = TreeEntry {
                name: destination
                    .file_name()
                    .context("SQLite path has no filename")?
                    .as_bytes()
                    .to_vec(),
                kind: EntryKind::File,
                target: Some(backup.blob),
                link_target: Vec::new(),
                mode: 0o100600,
                size: 0,
                mtime_secs: now().0,
                mtime_nanos: 0,
                xattrs: None,
                class: crate::content_class::ContentClass::Database,
            };
            self.restore_file(&destination, &entry)?;
            let path_bytes = destination.as_os_str().as_bytes();
            for suffix in [b"-wal".as_slice(), b"-shm".as_slice()] {
                let mut sidecar = path_bytes.to_vec();
                sidecar.extend_from_slice(suffix);
                let sidecar = PathBuf::from(OsString::from_vec(sidecar));
                if sidecar.exists() {
                    fs::remove_file(sidecar)?;
                }
            }
        }
        Ok(())
    }

    fn flatten_tree(&self, root: &ObjectId) -> anyhow::Result<BTreeMap<Vec<u8>, FlatEntry>> {
        let mut result = BTreeMap::new();
        self.flatten_into(root, Vec::new(), &mut result)?;
        Ok(result)
    }

    fn diff_directory(
        &self,
        before: Option<ObjectId>,
        after: Option<ObjectId>,
        prefix: Vec<u8>,
        selections: &[PathBuf],
        changes: &mut Vec<RewindChange>,
    ) -> anyhow::Result<()> {
        match (before, after) {
            (Some(before), Some(after)) => {
                tree::diff_entries(&self.store, &before, &after, &mut |before, after| {
                    self.record_tree_difference(before, after, &prefix, selections, changes)
                })
            }
            (Some(before), None) => tree::for_each_entry(&self.store, &before, |entry| {
                self.record_tree_difference(Some(entry), None, &prefix, selections, changes)
            }),
            (None, Some(after)) => tree::for_each_entry(&self.store, &after, |entry| {
                self.record_tree_difference(None, Some(entry), &prefix, selections, changes)
            }),
            (None, None) => Ok(()),
        }
    }

    fn record_tree_difference(
        &self,
        before: Option<TreeEntry>,
        after: Option<TreeEntry>,
        prefix: &[u8],
        selections: &[PathBuf],
        changes: &mut Vec<RewindChange>,
    ) -> anyhow::Result<()> {
        let name = before
            .as_ref()
            .or(after.as_ref())
            .context("tree difference has no entry")?
            .name
            .clone();
        validate_name(&name)?;
        let mut path = prefix.to_vec();
        if !path.is_empty() {
            path.push(b'/');
        }
        path.extend_from_slice(&name);

        if selected(&path, selections) {
            changes.push(RewindChange {
                path: display_relative(&path),
                action: match (&before, &after) {
                    (None, Some(_)) => "restore",
                    (Some(_), None) => "remove",
                    (Some(_), Some(_)) => "replace",
                    (None, None) => unreachable!(),
                },
                raw_path: path.clone(),
            });
        }

        if subtree_intersects(&path, selections) {
            let before_tree = before
                .as_ref()
                .filter(|entry| entry.kind == EntryKind::Directory)
                .and_then(|entry| entry.target);
            let after_tree = after
                .as_ref()
                .filter(|entry| entry.kind == EntryKind::Directory)
                .and_then(|entry| entry.target);
            if before_tree.is_some() || after_tree.is_some() {
                self.diff_directory(before_tree, after_tree, path, selections, changes)?;
            }
        }
        Ok(())
    }

    fn entries_for_plan(
        &self,
        root: &ObjectId,
        plan: &RewindPlan,
    ) -> anyhow::Result<BTreeMap<Vec<u8>, FlatEntry>> {
        let mut entries = BTreeMap::new();
        for change in &plan.changes {
            if change.action == "remove" {
                continue;
            }
            let entry = self
                .lookup_tree_path(root, &change.raw_path)?
                .with_context(|| format!("target snapshot is missing {}", change.path))?;
            entries.insert(change.raw_path.clone(), FlatEntry { entry });
        }
        Ok(entries)
    }

    fn entries_for_changed_paths(
        &self,
        root: &ObjectId,
        plan: &RewindPlan,
    ) -> anyhow::Result<BTreeMap<Vec<u8>, FlatEntry>> {
        let mut entries = BTreeMap::new();
        for change in &plan.changes {
            if let Some(entry) = self.lookup_tree_path(root, &change.raw_path)? {
                entries.insert(change.raw_path.clone(), FlatEntry { entry });
            }
        }
        Ok(entries)
    }

    fn verify_plan_state(
        &self,
        root: &Path,
        expected: &BTreeMap<Vec<u8>, FlatEntry>,
        plan: &RewindPlan,
        selected_paths: &[PathBuf],
    ) -> anyhow::Result<()> {
        for change in &plan.changes {
            if !selected(&change.raw_path, selected_paths) {
                continue;
            }
            let path = safe_join(root, &change.raw_path)?;
            ensure_safe_parent(root, &path)?;
            let matches = match expected.get(&change.raw_path) {
                Some(flat) => self.path_matches_entry(&path, &flat.entry)?,
                None => fs::symlink_metadata(&path).is_err_and(|error| {
                    error.kind() == std::io::ErrorKind::NotFound
                        || error.raw_os_error() == Some(libc::ENOTDIR)
                }),
            };
            anyhow::ensure!(
                matches,
                "workspace path changed during rewind: {}",
                change.path
            );
        }
        Ok(())
    }

    fn path_matches_entry(&self, path: &Path, entry: &TreeEntry) -> anyhow::Result<bool> {
        let metadata = match fs::symlink_metadata(path) {
            Ok(metadata) => metadata,
            Err(error) if error.kind() == std::io::ErrorKind::NotFound => return Ok(false),
            Err(error) => return Err(error.into()),
        };
        let file_type = metadata.file_type();
        let kind_matches = match entry.kind {
            EntryKind::File => file_type.is_file(),
            EntryKind::Directory => file_type.is_dir() && !file_type.is_symlink(),
            EntryKind::Symlink => file_type.is_symlink(),
            EntryKind::Fifo => file_type.is_fifo(),
            EntryKind::SocketMarker => return Ok(true),
        };
        if !kind_matches {
            return Ok(false);
        }
        if entry.kind == EntryKind::Symlink {
            return Ok(fs::read_link(path)?.as_os_str().as_bytes() == entry.link_target);
        }
        if metadata.permissions().mode() != entry.mode
            || metadata.mtime() != entry.mtime_secs
            || metadata.mtime_nsec().max(0) as u32 != entry.mtime_nanos
        {
            return Ok(false);
        }
        if entry.kind == EntryKind::File {
            if metadata.len() != entry.size {
                return Ok(false);
            }
            let blob = self.capture_file(path, None)?;
            if Some(blob) != entry.target {
                return Ok(false);
            }
        }
        if matches!(entry.kind, EntryKind::File | EntryKind::Directory) {
            let xattrs = self.capture_xattrs(path)?;
            if xattrs != entry.xattrs {
                return Ok(false);
            }
        }
        Ok(true)
    }

    fn lookup_tree_path(&self, root: &ObjectId, path: &[u8]) -> anyhow::Result<Option<TreeEntry>> {
        let mut tree_id = *root;
        let mut components = path.split(|byte| *byte == b'/').peekable();
        while let Some(name) = components.next() {
            validate_name(name)?;
            let Some(entry) = tree::find_entry(&self.store, &tree_id, name)? else {
                return Ok(None);
            };
            if components.peek().is_none() {
                return Ok(Some(entry));
            }
            if entry.kind != EntryKind::Directory {
                return Ok(None);
            }
            tree_id = entry.target.context("directory missing tree ID")?;
        }
        Ok(None)
    }

    fn flatten_into(
        &self,
        tree_id: &ObjectId,
        prefix: Vec<u8>,
        output: &mut BTreeMap<Vec<u8>, FlatEntry>,
    ) -> anyhow::Result<()> {
        tree::for_each_entry(&self.store, tree_id, |entry| {
            validate_name(&entry.name)?;
            let mut path = prefix.clone();
            if !path.is_empty() {
                path.push(b'/');
            }
            path.extend_from_slice(&entry.name);
            output.insert(
                path.clone(),
                FlatEntry {
                    entry: entry.clone(),
                },
            );
            if entry.kind == EntryKind::Directory {
                self.flatten_into(
                    &entry.target.context("directory missing tree ID")?,
                    path,
                    output,
                )?;
            }
            Ok(())
        })
    }

    fn apply_plan_at(
        &self,
        root: &Path,
        target: &BTreeMap<Vec<u8>, FlatEntry>,
        plan: &RewindPlan,
        selected_paths: &[PathBuf],
    ) -> anyhow::Result<()> {
        let mut applied_operations = 0_usize;
        let changed: BTreeSet<Vec<u8>> = plan
            .changes
            .iter()
            .map(|change| change.raw_path.clone())
            .collect();

        // Directories must exist before files are written.
        for (path, flat) in target {
            if !changed.contains(path)
                || !selected(path, selected_paths)
                || flat.entry.kind != EntryKind::Directory
            {
                continue;
            }
            let destination = safe_join(root, path)?;
            ensure_safe_parent(root, &destination)?;
            if let Ok(metadata) = destination.symlink_metadata() {
                if !metadata.file_type().is_dir() || metadata.file_type().is_symlink() {
                    remove_path(&destination)?;
                }
            }
            fs::create_dir_all(&destination)?;
            fs::set_permissions(&destination, fs::Permissions::from_mode(flat.entry.mode))?;
        }

        for (path, flat) in target {
            if !changed.contains(path)
                || !selected(path, selected_paths)
                || flat.entry.kind == EntryKind::Directory
            {
                continue;
            }
            let destination = safe_join(root, path)?;
            ensure_safe_parent(root, &destination)?;
            if let Some(parent) = destination.parent() {
                fs::create_dir_all(parent)?;
            }
            let must_remove = destination
                .symlink_metadata()
                .map(|metadata| {
                    metadata.is_dir()
                        || flat.entry.kind != EntryKind::File
                        || metadata.file_type().is_symlink()
                })
                .unwrap_or(false);
            if must_remove {
                remove_path(&destination)?;
            }
            match flat.entry.kind {
                EntryKind::File => self.restore_file(&destination, &flat.entry)?,
                EntryKind::Symlink => {
                    std::os::unix::fs::symlink(
                        OsString::from_vec(flat.entry.link_target.clone()),
                        &destination,
                    )?;
                    let mtime =
                        FileTime::from_unix_time(flat.entry.mtime_secs, flat.entry.mtime_nanos);
                    filetime::set_symlink_file_times(&destination, mtime, mtime)?;
                }
                EntryKind::Fifo => {
                    let cpath = std::ffi::CString::new(destination.as_os_str().as_bytes())?;
                    let result =
                        unsafe { libc::mkfifo(cpath.as_ptr(), flat.entry.mode as libc::mode_t) };
                    if result != 0 {
                        return Err(std::io::Error::last_os_error().into());
                    }
                }
                EntryKind::SocketMarker => {}
                EntryKind::Directory => unreachable!(),
            }
            applied_operations += 1;
            if applied_operations == 1
                && std::env::var_os("AGIT_FAILPOINT").as_deref()
                    == Some(OsStr::new("rewind_after_first_change"))
            {
                std::process::exit(86);
            }
        }

        // Remove paths absent from the target, deepest first.
        let mut removals: Vec<_> = plan
            .changes
            .iter()
            .filter(|change| change.action == "remove")
            .map(|change| change.raw_path.clone())
            .collect();
        removals.sort_by_key(|path| std::cmp::Reverse(path.len()));
        for path in removals {
            if selected(&path, selected_paths) {
                let destination = safe_join(root, &path)?;
                ensure_safe_parent(root, &destination)?;
                if destination.symlink_metadata().is_ok() {
                    remove_path(&destination)?;
                }
            }
        }

        // Apply directory mtimes after child operations so materialization does
        // not overwrite the captured timestamp.
        for (path, flat) in target.iter().rev() {
            if !changed.contains(path)
                || !selected(path, selected_paths)
                || flat.entry.kind != EntryKind::Directory
            {
                continue;
            }
            let destination = safe_join(root, path)?;
            let mtime = FileTime::from_unix_time(flat.entry.mtime_secs, flat.entry.mtime_nanos);
            filetime::set_file_mtime(destination, mtime)?;
        }
        Ok(())
    }

    fn restore_file(&self, destination: &Path, entry: &TreeEntry) -> anyhow::Result<()> {
        let blob: Blob = self.store.read_struct(
            &entry.target.context("file missing blob ID")?,
            ObjectKind::Blob,
        )?;
        let parent = destination.parent().context("file has no parent")?;
        let mut temp = tempfile::NamedTempFile::new_in(parent)?;
        for chunk in blob.chunks {
            let bytes = self.store.read_bytes(&chunk.id, ObjectKind::Chunk)?;
            anyhow::ensure!(bytes.len() == chunk.len as usize, "chunk length mismatch");
            temp.write_all(&bytes)?;
        }
        temp.as_file().sync_all()?;
        fs::set_permissions(temp.path(), fs::Permissions::from_mode(entry.mode))?;
        if let Some(xattrs_id) = entry.xattrs {
            let xattrs: Xattrs = self.store.read_struct(&xattrs_id, ObjectKind::Xattrs)?;
            for xattr in xattrs.entries {
                xattr::set(temp.path(), OsStr::from_bytes(&xattr.name), &xattr.value)?;
            }
        }
        temp.persist(destination).map_err(|error| error.error)?;
        let mtime = FileTime::from_unix_time(entry.mtime_secs, entry.mtime_nanos);
        filetime::set_file_mtime(destination, mtime)?;
        Ok(())
    }

    fn acquire_mutation_lock(&self) -> anyhow::Result<File> {
        let path = self
            .store
            .workspace_data_dir(&self.workspace_id)
            .join("mutation.lock");
        let file = fs::OpenOptions::new()
            .create(true)
            .truncate(false)
            .read(true)
            .write(true)
            .open(path)?;
        file.lock_exclusive()?;
        Ok(file)
    }

    fn restore_intent_path(&self) -> PathBuf {
        self.store
            .workspace_data_dir(&self.workspace_id)
            .join("restore.intent")
    }

    fn write_restore_intent(&self, intent: &RestoreIntent) -> anyhow::Result<()> {
        let path = self.restore_intent_path();
        atomic_write(&path, &serde_json::to_vec(intent)?)?;
        File::open(path.parent().context("restore intent has no parent")?)?.sync_all()?;
        Ok(())
    }

    fn clear_restore_intent(&self) -> anyhow::Result<()> {
        let path = self.restore_intent_path();
        if path.exists() {
            fs::remove_file(&path)?;
            File::open(path.parent().context("restore intent has no parent")?)?.sync_all()?;
        }
        Ok(())
    }

    fn recover_interrupted_rewind(&self) -> anyhow::Result<()> {
        let path = self.restore_intent_path();
        if !path.exists() {
            return Ok(());
        }
        let lock = self.acquire_mutation_lock()?;
        let intent: RestoreIntent = serde_json::from_slice(&fs::read(&path)?)?;
        eprintln!(
            "agit: recovering interrupted rewind; restoring pre-rewind snapshot {}",
            &id_hex(&intent.pre_snapshot)[..12]
        );
        let snapshot: Snapshot = self
            .store
            .read_struct(&intent.pre_snapshot, ObjectKind::Snapshot)?;
        let paths: Vec<PathBuf> = intent
            .paths
            .into_iter()
            .map(|path| PathBuf::from(OsString::from_vec(path)))
            .collect();
        let plan = self.plan_rewind(&intent.pre_snapshot, &paths)?;
        let entries = self.entries_for_plan(&snapshot.root_tree, &plan)?;
        self.apply_plan_at(&self.root, &entries, &plan, &paths)?;
        self.invalidate_path_index()?;
        self.clear_restore_intent()?;
        FileExt::unlock(&lock)?;
        Ok(())
    }
}

fn data_root() -> anyhow::Result<PathBuf> {
    if let Some(path) = std::env::var_os("AGIT_DATA_DIR") {
        return Ok(PathBuf::from(path));
    }
    let dirs = ProjectDirs::from("dev", "agit", "agit")
        .context("cannot determine application data directory")?;
    Ok(dirs.data_dir().to_owned())
}

fn ensure_family_id(
    root: &Path,
    store: &ObjectStore,
    workspace_id: &str,
) -> anyhow::Result<String> {
    let repository_path = root.join(FAMILY_FILE);
    let external_path = store.workspace_data_dir(workspace_id).join("family.id");
    let family_id = if external_path.exists() {
        fs::read_to_string(&external_path)?.trim().to_owned()
    } else if repository_path.exists() {
        fs::read_to_string(&repository_path)?.trim().to_owned()
    } else {
        let mut random = [0_u8; 16];
        getrandom::getrandom(&mut random)
            .map_err(|error| anyhow::anyhow!("generate workspace family ID: {error}"))?;
        hex::encode(random)
    };
    write_family_id(root, store, workspace_id, &family_id)?;
    Ok(family_id)
}

fn write_family_id(
    root: &Path,
    store: &ObjectStore,
    workspace_id: &str,
    family_id: &str,
) -> anyhow::Result<()> {
    anyhow::ensure!(
        family_id.len() == 32
            && family_id
                .bytes()
                .all(|byte| byte.is_ascii_hexdigit() && !byte.is_ascii_uppercase()),
        "invalid workspace family ID"
    );
    let repository_path = root.join(FAMILY_FILE);
    let external_path = store.workspace_data_dir(workspace_id).join("family.id");
    if !external_path.exists() || fs::read_to_string(&external_path)?.trim() != family_id {
        atomic_write(&external_path, format!("{family_id}\n").as_bytes())?;
    }
    if !repository_path.exists() || fs::read_to_string(&repository_path)?.trim() != family_id {
        fs::create_dir_all(
            repository_path
                .parent()
                .context("family file has no parent")?,
        )?;
        atomic_write(&repository_path, format!("{family_id}\n").as_bytes())?;
    }
    Ok(())
}

fn new_workspace_id(root: &Path) -> String {
    let mut hasher = blake3::Hasher::new();
    hasher.update(root.as_os_str().as_bytes());
    let (secs, nanos) = now();
    hasher.update(&secs.to_le_bytes());
    hasher.update(&nanos.to_le_bytes());
    hasher.update(&std::process::id().to_le_bytes());
    hex::encode(&hasher.finalize().as_bytes()[..16])
}

fn now() -> (i64, u32) {
    let duration = SystemTime::now()
        .duration_since(UNIX_EPOCH)
        .unwrap_or_default();
    (duration.as_secs() as i64, duration.subsec_nanos())
}

fn metadata_time(metadata: &fs::Metadata) -> (i64, u32) {
    (metadata.mtime(), metadata.mtime_nsec().max(0) as u32)
}

fn stable_metadata(before: &fs::Metadata, after: &fs::Metadata) -> bool {
    before.dev() == after.dev()
        && before.ino() == after.ino()
        && before.len() == after.len()
        && before.mtime() == after.mtime()
        && before.mtime_nsec() == after.mtime_nsec()
        && before.ctime() == after.ctime()
        && before.ctime_nsec() == after.ctime_nsec()
        && before.mode() == after.mode()
}

fn directory_target_only_difference(before: Option<&TreeEntry>, after: Option<&TreeEntry>) -> bool {
    let (Some(before), Some(after)) = (before, after) else {
        return false;
    };
    if before.kind != EntryKind::Directory || after.kind != EntryKind::Directory {
        return false;
    }
    let mut before = before.clone();
    let mut after = after.clone();
    before.target = None;
    after.target = None;
    before == after
}

fn recovery_route(class: crate::content_class::ContentClass) -> &'static str {
    use crate::content_class::ContentClass;
    match class {
        ContentClass::Dependency | ContentClass::BuildOutput | ContentClass::Scratch => {
            "regenerate-or-fetch"
        }
        ContentClass::Database => "replica-or-database-backup",
        ContentClass::Source
        | ContentClass::VcsMeta
        | ContentClass::ConfigSecret
        | ContentClass::Lockfile => "replica-or-none",
    }
}

pub(crate) fn derive_materialization(
    store: &ObjectStore,
    snapshot_id: &ObjectId,
) -> anyhow::Result<MaterializationReport> {
    let snapshot: Snapshot = store.read_struct(snapshot_id, ObjectKind::Snapshot)?;
    let mut stack = vec![(snapshot.root_tree, Vec::<u8>::new())];
    let mut missing_paths = Vec::new();
    let mut partial_classes = BTreeSet::new();
    while let Some((tree_id, prefix)) = stack.pop() {
        tree::for_each_entry(store, &tree_id, |entry| {
            let mut path = prefix.clone();
            if !path.is_empty() {
                path.push(b'/');
            }
            path.extend_from_slice(&entry.name);
            match (entry.kind, entry.target) {
                (EntryKind::Directory, Some(target)) => stack.push((target, path)),
                (EntryKind::File, Some(target)) => {
                    let blob: Blob = store.read_struct(&target, ObjectKind::Blob)?;
                    let mut missing = false;
                    for chunk in &blob.chunks {
                        if !store.contains_object(&chunk.id)? {
                            missing = true;
                            break;
                        }
                    }
                    if missing {
                        partial_classes.insert(entry.class);
                        missing_paths.push(MissingMaterializationPath {
                            path: String::from_utf8_lossy(&path).into_owned(),
                            class: entry.class.as_str(),
                            recovery: recovery_route(entry.class),
                            raw_path: path,
                        });
                    }
                }
                _ => {}
            }
            Ok(())
        })?;
    }
    missing_paths.sort_by(|left, right| left.path.as_bytes().cmp(right.path.as_bytes()));
    Ok(MaterializationReport {
        grade: if missing_paths.is_empty() {
            "exact"
        } else {
            "partial"
        },
        partial_classes: partial_classes
            .into_iter()
            .map(|class| class.as_str())
            .collect(),
        missing_paths,
    })
}

fn cached_matches(cached: &CachedFile, metadata: &fs::Metadata) -> bool {
    cached.device == metadata.dev()
        && cached.inode == metadata.ino()
        && cached.size == metadata.len()
        && cached.mtime_secs == metadata.mtime()
        && cached.mtime_nanos == metadata.mtime_nsec()
        && cached.ctime_secs == metadata.ctime()
        && cached.ctime_nanos == metadata.ctime_nsec()
        && cached.mode == metadata.mode()
}

fn entry_matches_metadata(entry: &TreeEntry, metadata: &fs::Metadata) -> bool {
    let file_type = metadata.file_type();
    let kind_matches = match entry.kind {
        EntryKind::File => file_type.is_file(),
        EntryKind::Directory => file_type.is_dir(),
        EntryKind::Symlink => file_type.is_symlink(),
        EntryKind::Fifo => file_type.is_fifo(),
        EntryKind::SocketMarker => file_type.is_socket(),
    };
    let (secs, nanos) = metadata_time(metadata);
    kind_matches
        && entry.mode == metadata.mode()
        && entry.mtime_secs == secs
        && entry.mtime_nanos == nanos
        && (entry.kind != EntryKind::File || entry.size == metadata.len())
}

fn cached_from_metadata(metadata: &fs::Metadata, blob_id: ObjectId) -> CachedFile {
    CachedFile {
        device: metadata.dev(),
        inode: metadata.ino(),
        size: metadata.len(),
        mtime_secs: metadata.mtime(),
        mtime_nanos: metadata.mtime_nsec(),
        ctime_secs: metadata.ctime(),
        ctime_nanos: metadata.ctime_nsec(),
        mode: metadata.mode(),
        blob_id,
    }
}

fn special_entry(name: Vec<u8>, kind: EntryKind, mode: u32, secs: i64, nanos: u32) -> TreeEntry {
    TreeEntry {
        name,
        kind,
        target: None,
        link_target: Vec::new(),
        mode,
        size: 0,
        mtime_secs: secs,
        mtime_nanos: nanos,
        xattrs: None,
        class: crate::content_class::ContentClass::Scratch,
    }
}

fn atomic_write(path: &Path, bytes: &[u8]) -> anyhow::Result<()> {
    let parent = path.parent().context("path has no parent")?;
    fs::create_dir_all(parent)?;
    let mut temp = tempfile::NamedTempFile::new_in(parent)?;
    temp.write_all(bytes)?;
    temp.as_file().sync_all()?;
    temp.persist(path).map_err(|error| error.error)?;
    Ok(())
}

fn validate_fork_name(name: &str) -> anyhow::Result<()> {
    anyhow::ensure!(!name.is_empty(), "fork name cannot be empty");
    anyhow::ensure!(name.len() <= 96, "fork name cannot exceed 96 bytes");
    anyhow::ensure!(
        name.bytes()
            .all(|byte| byte.is_ascii_alphanumeric() || matches!(byte, b'-' | b'_' | b'.')),
        "fork name may contain only letters, numbers, dot, dash, and underscore"
    );
    anyhow::ensure!(name != "." && name != "..", "invalid fork name");
    Ok(())
}

fn ceiling_div(value: u64, divisor: u64) -> u64 {
    value / divisor + u64::from(value % divisor != 0)
}

fn absolute_destination(destination: &Path) -> anyhow::Result<PathBuf> {
    if destination.is_absolute() {
        Ok(destination.to_owned())
    } else {
        Ok(std::env::current_dir()?.join(destination))
    }
}

fn fork_summary(
    fork_id: &str,
    name: &str,
    destination: PathBuf,
    base: ObjectId,
    head: ObjectId,
    report: ForkReport,
) -> ForkSummary {
    ForkSummary {
        fork_id: fork_id.to_owned(),
        name: name.to_owned(),
        destination,
        base_snapshot: id_hex(&base),
        head_snapshot: id_hex(&head),
        tier: report.tier,
        files: report.files,
        directories: report.directories,
        symlinks: report.symlinks,
        fifos: report.fifos,
        skipped_special: report.skipped_special,
        logical_bytes: report.logical_bytes,
        cloned_bytes: report.cloned_bytes,
        copied_bytes: report.copied_bytes,
        hardlinked_files: report.hardlinked_files,
        elapsed_ms: report.elapsed.as_millis().min(u64::MAX as u128) as u64,
        atomic_hierarchy: report.atomic_hierarchy,
        created_at: now().0,
        conflicts: 0,
        conflict_paths: Vec::new(),
        radar_stale: false,
        radar_updated_at: None,
    }
}

pub fn new_fork_id() -> anyhow::Result<String> {
    let mut bytes = [0_u8; 16];
    getrandom::getrandom(&mut bytes)
        .map_err(|error| anyhow::anyhow!("generate fork ID: {error}"))?;
    Ok(hex::encode(bytes))
}

fn validate_fork_id(value: &str) -> anyhow::Result<()> {
    anyhow::ensure!(
        value.len() == 32 && value.bytes().all(|byte| byte.is_ascii_hexdigit()),
        "invalid fork ID"
    );
    Ok(())
}

fn legacy_fork_id(name: &str, base_snapshot: &str) -> String {
    let mut hasher = blake3::Hasher::new();
    hasher.update(b"agit:legacy-fork-id:v1\0");
    hasher.update(name.as_bytes());
    hasher.update(b"\0");
    hasher.update(base_snapshot.as_bytes());
    hex::encode(&hasher.finalize().as_bytes()[..16])
}

fn write_fork_id(repository: &AgitRepository, fork_id: &str) -> anyhow::Result<()> {
    validate_fork_id(fork_id)?;
    atomic_write(
        &repository.root.join(FORK_FILE),
        format!("{fork_id}\n").as_bytes(),
    )?;
    atomic_write(
        &repository.workspace_data_dir().join("fork.id"),
        format!("{fork_id}\n").as_bytes(),
    )
}

fn merge_rewind_plan(
    ours: &BTreeMap<Vec<u8>, TreeEntry>,
    changes: &[merge::MergeChange],
) -> (RewindPlan, BTreeMap<Vec<u8>, FlatEntry>) {
    let mut rewind_changes = Vec::with_capacity(changes.len());
    let mut target = BTreeMap::new();
    for change in changes {
        let (action, entry) = match &change.action {
            MergeAction::Set(entry) => (
                if ours.contains_key(&change.path) {
                    "replace"
                } else {
                    "restore"
                },
                Some(entry.clone()),
            ),
            MergeAction::Remove => ("remove", None),
        };
        rewind_changes.push(RewindChange {
            path: display_relative(&change.path),
            action,
            raw_path: change.path.clone(),
        });
        if let Some(entry) = entry {
            target.insert(change.path.clone(), FlatEntry { entry });
        }
    }
    (
        RewindPlan {
            preview_digest: rewind_preview_digest("merge", None, &rewind_changes),
            target: "merge".to_owned(),
            current_tree: None,
            changes: rewind_changes,
        },
        target,
    )
}

fn rewind_preview_digest(
    target: &str,
    current_tree: Option<&str>,
    changes: &[RewindChange],
) -> String {
    let mut hasher = blake3::Hasher::new();
    hasher.update(b"agit:rewind-preview:v1\0");
    hasher.update(target.as_bytes());
    hasher.update(b"\0");
    hasher.update(current_tree.unwrap_or_default().as_bytes());
    for change in changes {
        hasher.update(b"\0");
        hasher.update(change.action.as_bytes());
        hasher.update(b"\0");
        hasher.update(&change.raw_path);
    }
    hex::encode(hasher.finalize().as_bytes())
}

fn merge_preview_digest(
    base: &str,
    ours_tree: &str,
    theirs_tree: &str,
    plan: &merge::MergePlan,
) -> String {
    let mut hasher = blake3::Hasher::new();
    hasher.update(b"agit:merge-preview:v1\0");
    for value in [base, ours_tree, theirs_tree] {
        hasher.update(value.as_bytes());
        hasher.update(b"\0");
    }
    hasher.update(&serde_json::to_vec(plan).expect("merge plan is serializable"));
    hex::encode(hasher.finalize().as_bytes())
}

fn command_output(stdout: &[u8], stderr: &[u8]) -> String {
    const LIMIT: usize = 16 * 1024;
    let mut combined = Vec::with_capacity(stdout.len().saturating_add(stderr.len()).min(LIMIT));
    combined.extend_from_slice(stdout);
    if !stdout.is_empty() && !stderr.is_empty() {
        combined.push(b'\n');
    }
    combined.extend_from_slice(stderr);
    if combined.len() > LIMIT {
        combined.drain(..combined.len() - LIMIT);
    }
    String::from_utf8_lossy(&combined).trim().to_owned()
}

fn validate_name(name: &[u8]) -> anyhow::Result<()> {
    anyhow::ensure!(!name.is_empty(), "empty path component in snapshot");
    anyhow::ensure!(
        name != b"." && name != b"..",
        "unsafe path component in snapshot"
    );
    anyhow::ensure!(
        !name.contains(&0) && !name.contains(&b'/'),
        "invalid path component in snapshot"
    );
    Ok(())
}

fn validate_relative_path(path: &[u8]) -> anyhow::Result<()> {
    let path = Path::new(OsStr::from_bytes(path));
    anyhow::ensure!(
        path.components()
            .all(|component| matches!(component, Component::Normal(_))),
        "invalid changed path"
    );
    Ok(())
}

fn relative_parent(path: &[u8]) -> Vec<u8> {
    Path::new(OsStr::from_bytes(path))
        .parent()
        .map_or_else(Vec::new, |parent| parent.as_os_str().as_bytes().to_vec())
}

fn path_depth(path: &[u8]) -> usize {
    Path::new(OsStr::from_bytes(path)).components().count()
}

fn safe_join(root: &Path, relative: &[u8]) -> anyhow::Result<PathBuf> {
    let path = PathBuf::from(OsString::from_vec(relative.to_vec()));
    for component in path.components() {
        anyhow::ensure!(
            matches!(component, Component::Normal(_)),
            "unsafe rewind path"
        );
    }
    Ok(root.join(path))
}

fn ensure_safe_parent(root: &Path, destination: &Path) -> anyhow::Result<()> {
    let relative = destination
        .strip_prefix(root)
        .context("rewind path escaped workspace")?;
    let mut current = root.to_path_buf();
    if let Some(parent) = relative.parent() {
        for component in parent.components() {
            let Component::Normal(name) = component else {
                bail!("unsafe rewind path")
            };
            current.push(name);
            if let Ok(metadata) = fs::symlink_metadata(&current) {
                anyhow::ensure!(
                    !metadata.file_type().is_symlink(),
                    "refusing to traverse symlink parent during rewind: {}",
                    current.display()
                );
            }
        }
    }
    Ok(())
}

fn selected(path: &[u8], selections: &[PathBuf]) -> bool {
    if selections.is_empty() {
        return true;
    }
    let candidate = Path::new(OsStr::from_bytes(path));
    selections
        .iter()
        .any(|selection| candidate == selection || candidate.starts_with(selection))
}

fn changed_path_matches(root: &Path, path: &Path, expected: &[u8]) -> bool {
    let absolute = if path.is_absolute() {
        path.to_owned()
    } else {
        root.join(path)
    };
    absolute
        .strip_prefix(root)
        .ok()
        .is_some_and(|relative| relative.as_os_str().as_bytes() == expected)
}

fn is_identity_path(path: &[u8]) -> bool {
    path == WORKSPACE_FILE_BYTES || path == FORK_FILE_BYTES
}

fn test_pause(variable: &str) {
    let Some(milliseconds) = std::env::var_os(variable)
        .and_then(|value| value.to_str().and_then(|value| value.parse::<u64>().ok()))
    else {
        return;
    };
    std::thread::sleep(std::time::Duration::from_millis(milliseconds.min(5_000)));
}

fn subtree_intersects(path: &[u8], selections: &[PathBuf]) -> bool {
    if selections.is_empty() {
        return true;
    }
    let candidate = Path::new(OsStr::from_bytes(path));
    selections
        .iter()
        .any(|selection| candidate.starts_with(selection) || selection.starts_with(candidate))
}

fn display_relative(path: &[u8]) -> String {
    String::from_utf8_lossy(path).into_owned()
}

fn remove_path(path: &Path) -> anyhow::Result<()> {
    let metadata = fs::symlink_metadata(path)?;
    if metadata.file_type().is_dir() && !metadata.file_type().is_symlink() {
        fs::remove_dir_all(path)?;
    } else {
        fs::remove_file(path)?;
    }
    Ok(())
}

fn collect_sqlite_candidates(
    root: &Path,
    directory: &Path,
    policy: &CapturePolicy,
    output: &mut Vec<PathBuf>,
) -> anyhow::Result<()> {
    for child in fs::read_dir(directory)? {
        let child = child?;
        let path = child.path();
        let relative = path.strip_prefix(root)?.as_os_str().as_bytes();
        if policy.excludes_bytes(relative) {
            continue;
        }
        let metadata = fs::symlink_metadata(&path)?;
        if metadata.file_type().is_symlink() {
            continue;
        }
        if metadata.is_dir() {
            collect_sqlite_candidates(root, &path, policy, output)?;
        } else if metadata.is_file() && sqlite_adapter::is_sqlite(&path) {
            output.push(path);
        }
    }
    Ok(())
}

fn is_not_found(error: &anyhow::Error) -> bool {
    error.chain().any(|cause| {
        cause
            .downcast_ref::<std::io::Error>()
            .is_some_and(|io| io.kind() == std::io::ErrorKind::NotFound)
    })
}
