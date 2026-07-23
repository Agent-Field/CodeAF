//! Encrypted, delta-only synchronization through a directory or direct SSH helper.

use crate::bundle;
use crate::model::{ObjectId, ObjectKind, Snapshot};
use crate::remote::{self, RemoteSpec, Session};
use crate::remote_crypto::RemoteCrypto;
use crate::store::ObjectStore;
use anyhow::Context;
use rusqlite::{params, Connection};
use serde::{Deserialize, Serialize};
use std::fs;
use std::os::unix::fs::PermissionsExt;
use std::path::Path;
use std::time::{Instant, SystemTime, UNIX_EPOCH};

const LEASE_SECONDS: u64 = 60 * 60;
const HAVE_BATCH: usize = 1024;
const HAVE_BATCH_BYTES: usize = 16 * 1024 * 1024;
const PULL_BATCH: usize = 128;

#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct PairConfig {
    pub remote: RemoteSpec,
    pub namespace: String,
    #[serde(default, skip_serializing_if = "Option::is_none")]
    pub remote_namespace: Option<String>,
    pub key: [u8; 32],
    pub machine_id: [u8; 16],
}

#[derive(Debug, Clone, Serialize)]
pub struct PairSummary {
    pub remote: String,
    pub namespace: String,
    pub key_hex: String,
    pub machine_id: String,
}

#[derive(Debug, Clone, Serialize)]
pub struct PushReport {
    pub snapshot: String,
    pub root: String,
    pub objects: u64,
    pub uploaded_objects: u64,
    pub reused_objects: u64,
    pub uploaded_bytes: u64,
    pub timings: TransportTimings,
}

#[derive(Debug, Clone, Serialize)]
pub struct PullReport {
    pub snapshot: String,
    pub fetched_objects: u64,
    pub reused_objects: u64,
    pub fetched_bytes: u64,
    pub timings: TransportTimings,
}

#[derive(Debug, Clone, Default, Serialize)]
pub struct TransportTimings {
    pub connect_auth_ms: u64,
    pub negotiate_ms: u64,
    pub stream_ms: u64,
    pub durability_ms: u64,
    pub notify_ms: Option<u64>,
    pub total_ms: u64,
    pub connection_reused: bool,
}

struct OperationTiming {
    connect_auth_ms: u64,
    connection_reused: bool,
    total_started: Instant,
    lock_ms: u64,
}

#[derive(Debug, Clone, Serialize, Deserialize)]
struct RemoteHead {
    version: u8,
    snapshot: ObjectId,
    root: ObjectId,
    base_root: Option<ObjectId>,
    publisher: [u8; 16],
}

#[derive(Debug, Clone)]
pub struct PulledHead {
    pub snapshot: ObjectId,
    pub root: ObjectId,
    pub base_root: Option<ObjectId>,
    pub report: PullReport,
}

pub fn pair(
    config_path: &Path,
    remote: &Path,
    namespace: &str,
    key_hex: Option<&str>,
) -> anyhow::Result<PairSummary> {
    remote::validate_namespace(namespace)?;
    let remote = RemoteSpec::from_input(remote)?;
    let key = match key_hex {
        Some(key) => parse_key(key)?,
        None => RemoteCrypto::generate_key()?,
    };
    let mut machine_id = [0; 16];
    getrandom::getrandom(&mut machine_id)
        .map_err(|error| anyhow::anyhow!("generate machine ID: {error}"))?;
    let config = PairConfig {
        remote: remote.clone(),
        namespace: namespace.to_owned(),
        remote_namespace: Some(opaque_namespace(&key, namespace)),
        key,
        machine_id,
    };
    let parent = config_path.parent().context("sync config has no parent")?;
    fs::create_dir_all(parent)?;
    local_atomic_write(config_path, &serde_json::to_vec_pretty(&config)?)?;
    fs::set_permissions(config_path, fs::Permissions::from_mode(0o600))?;
    // Opening verifies SSH availability and creates the namespace through the
    // same opaque helper protocol used by normal transfers.
    drop(Session::open(&config.remote, storage_namespace(&config))?);
    Ok(PairSummary {
        remote: remote.display(),
        namespace: namespace.to_owned(),
        key_hex: hex::encode(key),
        machine_id: hex::encode(machine_id),
    })
}

pub fn load(config_path: &Path) -> anyhow::Result<PairConfig> {
    let config: PairConfig = serde_json::from_slice(&fs::read(config_path).with_context(|| {
        format!(
            "repository is not paired; run `furrow pair <directory>` first ({})",
            config_path.display()
        )
    })?)
    .context("decode sync configuration")?;
    remote::validate_namespace(&config.namespace)?;
    config.remote.validate()?;
    Ok(config)
}

pub fn push(
    store: &ObjectStore,
    snapshot: ObjectId,
    config: &PairConfig,
    expected_remote_root: Option<ObjectId>,
    takeover: bool,
) -> anyhow::Result<PushReport> {
    let mut remote = open_session(config, None)?;
    push_on(
        store,
        snapshot,
        config,
        expected_remote_root,
        takeover,
        &mut remote,
    )
}

pub(crate) fn open_session(config: &PairConfig, ref_name: Option<&str>) -> anyhow::Result<Session> {
    // Validate before paying for a connection so an invalid `--ref` fails
    // fast and identically across all three transports.
    if let Some(name) = ref_name {
        remote::validate_ref(name)?;
    }
    let mut session = Session::open(&config.remote, storage_namespace(config))?;
    session.set_ref(ref_name)?;
    Ok(session)
}

pub(crate) fn push_on(
    store: &ObjectStore,
    snapshot: ObjectId,
    config: &PairConfig,
    expected_remote_root: Option<ObjectId>,
    takeover: bool,
    remote: &mut Session,
) -> anyhow::Result<PushReport> {
    let total_started = Instant::now();
    let (connect_auth_ms, connection_reused) = remote.begin_operation();
    let negotiate_started = Instant::now();
    remote.begin_writer()?;
    let lock_ms = elapsed_ms(negotiate_started);
    let result = push_locked(
        store,
        snapshot,
        config,
        expected_remote_root,
        takeover,
        remote,
        OperationTiming {
            connect_auth_ms,
            connection_reused,
            total_started,
            lock_ms,
        },
    );
    let durability_started = Instant::now();
    let unlock = remote.end_writer();
    match (result, unlock) {
        (Err(error), _) => Err(error),
        (Ok(_), Err(error)) => Err(error).context("release remote writer lock"),
        (Ok(mut report), Ok(())) => {
            report.timings.durability_ms = report
                .timings
                .durability_ms
                .saturating_add(elapsed_ms(durability_started));
            report.timings.total_ms = connect_auth_ms.saturating_add(elapsed_ms(total_started));
            Ok(report)
        }
    }
}

fn push_locked(
    store: &ObjectStore,
    snapshot: ObjectId,
    config: &PairConfig,
    expected_remote_root: Option<ObjectId>,
    takeover: bool,
    remote: &mut Session,
    timing: OperationTiming,
) -> anyhow::Result<PushReport> {
    let negotiate_started = Instant::now();
    let ref_name = remote.ref_name().map(str::to_owned);
    let head_key = remote::head_key(ref_name.as_deref());
    let lease_key = remote::lease_key(ref_name.as_deref());
    let new_lease = prepare_writer_lease(remote, config, takeover)?;
    let crypto = RemoteCrypto::new(config.key);
    let snapshot_value: Snapshot = store.read_struct(&snapshot, ObjectKind::Snapshot)?;
    let base_root = if remote.exists(&head_key)? {
        let current = read_remote_head(remote, config, &crypto)?;
        anyhow::ensure!(
            current.snapshot == snapshot || Some(current.root) == expected_remote_root,
            "remote workspace changed since this machine last synchronized; pull before pushing"
        );
        Some(current.root)
    } else {
        anyhow::ensure!(
            expected_remote_root.is_none(),
            "paired remote lost its published head; refusing to recreate it without re-pairing"
        );
        None
    };
    // Publish ownership only after the locked expected-head check. A stale
    // takeover attempt must not disrupt the current writer when no new HEAD
    // can be committed.
    remote.write(&lease_key, &new_lease)?;
    let negotiate_ms = timing.lock_ms.saturating_add(elapsed_ms(negotiate_started));
    let stream_started = Instant::now();
    let mut uploaded_objects = 0_u64;
    let mut reused_objects = 0_u64;
    let mut uploaded_bytes = 0_u64;
    let mut pending = Vec::with_capacity(HAVE_BATCH);
    let mut pending_bytes = 0_usize;
    let objects = bundle::for_each_reachable(store, snapshot, |id, kind, bytes| {
        if bytes.len() >= HAVE_BATCH_BYTES {
            flush_push_batch(
                &crypto,
                remote,
                &mut pending,
                &mut uploaded_objects,
                &mut reused_objects,
                &mut uploaded_bytes,
            )?;
            pending_bytes = 0;
            let remote_id = crypto.remote_id(id);
            if remote.has_objects(&[remote_id])?[0] {
                reused_objects += 1;
            } else {
                let encrypted = crypto.encrypt_object(id, kind, bytes)?;
                remote.write(&remote::object_key(&remote_id), &encrypted)?;
                uploaded_objects += 1;
                uploaded_bytes += encrypted.len() as u64;
            }
            return Ok(());
        }
        if pending.len() == HAVE_BATCH
            || pending_bytes.saturating_add(bytes.len()) > HAVE_BATCH_BYTES
        {
            flush_push_batch(
                &crypto,
                remote,
                &mut pending,
                &mut uploaded_objects,
                &mut reused_objects,
                &mut uploaded_bytes,
            )?;
            pending_bytes = 0;
        }
        pending_bytes = pending_bytes.saturating_add(bytes.len());
        pending.push((*id, kind, bytes.to_vec()));
        Ok(())
    })?;
    flush_push_batch(
        &crypto,
        remote,
        &mut pending,
        &mut uploaded_objects,
        &mut reused_objects,
        &mut uploaded_bytes,
    )?;
    let stream_ms = elapsed_ms(stream_started);
    let durability_started = Instant::now();
    let head = RemoteHead {
        version: 1,
        snapshot,
        root: snapshot_value.root_tree,
        base_root,
        publisher: config.machine_id,
    };
    let encrypted_head = crypto.encrypt_metadata(
        &serde_json::to_vec(&head)?,
        head_context(&config.namespace, ref_name.as_deref()).as_bytes(),
    )?;
    remote.write(&head_key, &encrypted_head)?;
    let durability_ms = elapsed_ms(durability_started);
    Ok(PushReport {
        snapshot: hex::encode(snapshot),
        root: hex::encode(snapshot_value.root_tree),
        objects,
        uploaded_objects,
        reused_objects,
        uploaded_bytes,
        timings: TransportTimings {
            connect_auth_ms: timing.connect_auth_ms,
            negotiate_ms,
            stream_ms,
            durability_ms,
            notify_ms: None,
            total_ms: timing
                .connect_auth_ms
                .saturating_add(elapsed_ms(timing.total_started)),
            connection_reused: timing.connection_reused,
        },
    })
}

fn flush_push_batch(
    crypto: &RemoteCrypto,
    remote: &mut Session,
    pending: &mut Vec<(ObjectId, ObjectKind, Vec<u8>)>,
    uploaded_objects: &mut u64,
    reused_objects: &mut u64,
    uploaded_bytes: &mut u64,
) -> anyhow::Result<()> {
    if pending.is_empty() {
        return Ok(());
    }
    let remote_ids: Vec<_> = pending
        .iter()
        .map(|(id, _, _)| crypto.remote_id(id))
        .collect();
    let existing = remote.has_objects(&remote_ids)?;
    anyhow::ensure!(
        existing.len() == pending.len(),
        "invalid remote have response"
    );
    let mut uploads = Vec::new();
    for (((id, kind, bytes), remote_id), exists) in pending.drain(..).zip(remote_ids).zip(existing)
    {
        if exists {
            *reused_objects += 1;
            continue;
        }
        let encrypted = crypto.encrypt_object(&id, kind, &bytes)?;
        *uploaded_objects += 1;
        *uploaded_bytes += encrypted.len() as u64;
        uploads.push((remote::object_key(&remote_id), encrypted));
    }
    remote.write_many(&uploads)
}

pub fn pull(store: &ObjectStore, config: &PairConfig) -> anyhow::Result<PulledHead> {
    let mut remote = open_session(config, None)?;
    pull_on(store, config, &mut remote)
}

pub(crate) fn pull_on(
    store: &ObjectStore,
    config: &PairConfig,
    remote: &mut Session,
) -> anyhow::Result<PulledHead> {
    let total_started = Instant::now();
    let (connect_auth_ms, connection_reused) = remote.begin_operation();
    let negotiate_started = Instant::now();
    let crypto = RemoteCrypto::new(config.key);
    let head = read_remote_head(remote, config, &crypto)?;
    let negotiate_ms = elapsed_ms(negotiate_started);
    let stream_started = Instant::now();
    let queue_file = tempfile::NamedTempFile::new()?;
    let queue = Connection::open(queue_file.path())?;
    queue.execute_batch(
        "PRAGMA journal_mode=OFF;
         PRAGMA synchronous=OFF;
         CREATE TABLE queue(
            id BLOB PRIMARY KEY,
            kind INTEGER NOT NULL,
            processed INTEGER NOT NULL DEFAULT 0
         ) WITHOUT ROWID;
         CREATE INDEX queue_pending ON queue(processed, id);
         BEGIN;",
    )?;
    enqueue(&queue, &head.snapshot, ObjectKind::Snapshot)?;
    let mut fetched_objects = 0_u64;
    let mut reused_objects = 0_u64;
    let mut fetched_bytes = 0_u64;
    loop {
        let batch = next_batch(&queue, PULL_BATCH)?;
        if batch.is_empty() {
            break;
        }
        let mut missing = Vec::new();
        for (id, expected) in batch {
            if !store.contains_object(&id)? {
                missing.push((id, expected));
                continue;
            }
            reused_objects += 1;
            let bytes = store.read_bytes(&id, expected)?;
            process_pulled_object(&queue, id, expected, &bytes)?;
        }
        let requests: Vec<_> = missing
            .iter()
            .map(|(id, _)| {
                (
                    remote::object_key(&crypto.remote_id(id)),
                    remote::MAX_OBJECT_BYTES,
                )
            })
            .collect();
        remote.read_many(&requests, |index, encrypted| {
            let (id, expected) = missing[index];
            let (kind, bytes) = crypto.decrypt_object(&id, &encrypted)?;
            anyhow::ensure!(kind == expected, "remote object kind mismatch");
            anyhow::ensure!(
                store.put_bytes(kind, &bytes)? == id,
                "remote import ID mismatch"
            );
            fetched_objects += 1;
            fetched_bytes += encrypted.len() as u64;
            process_pulled_object(&queue, id, expected, &bytes)
                .with_context(|| format!("import remote object {}", hex::encode(id)))
        })?;
    }
    queue.execute_batch("COMMIT;")?;
    // Existing objects are trusted only because the local completeness gate
    // made them visible. A final traversal proves the assembled remote root.
    bundle::for_each_reachable(store, head.snapshot, |_id, _kind, _bytes| Ok(()))?;
    let remote_snapshot: Snapshot = store.read_struct(&head.snapshot, ObjectKind::Snapshot)?;
    anyhow::ensure!(
        remote_snapshot.root_tree == head.root,
        "authenticated remote head does not match its snapshot root"
    );
    let stream_ms = elapsed_ms(stream_started);
    Ok(PulledHead {
        snapshot: head.snapshot,
        root: head.root,
        base_root: head.base_root,
        report: PullReport {
            snapshot: hex::encode(head.snapshot),
            fetched_objects,
            reused_objects,
            fetched_bytes,
            timings: TransportTimings {
                connect_auth_ms,
                negotiate_ms,
                stream_ms,
                durability_ms: 0,
                notify_ms: None,
                total_ms: connect_auth_ms.saturating_add(elapsed_ms(total_started)),
                connection_reused,
            },
        },
    })
}

pub fn published_root(config: &PairConfig) -> anyhow::Result<Option<ObjectId>> {
    let mut remote = open_session(config, None)?;
    published_root_on(config, &mut remote)
}

pub(crate) fn published_root_on(
    config: &PairConfig,
    remote: &mut Session,
) -> anyhow::Result<Option<ObjectId>> {
    remote.begin_operation();
    let head_key = remote::head_key(remote.ref_name());
    if !remote.exists(&head_key)? {
        return Ok(None);
    }
    let crypto = RemoteCrypto::new(config.key);
    Ok(Some(read_remote_head(remote, config, &crypto)?.root))
}

fn elapsed_ms(started: Instant) -> u64 {
    started.elapsed().as_millis().try_into().unwrap_or(u64::MAX)
}

fn prepare_writer_lease(
    remote: &mut Session,
    config: &PairConfig,
    takeover: bool,
) -> anyhow::Result<Vec<u8>> {
    let crypto = RemoteCrypto::new(config.key);
    let now = SystemTime::now().duration_since(UNIX_EPOCH)?.as_secs();
    let ref_name = remote.ref_name().map(str::to_owned);
    let lease_key = remote::lease_key(ref_name.as_deref());
    let context = lease_context(ref_name.as_deref());
    if remote.exists(&lease_key)? {
        let lease = crypto.decrypt_head(
            &remote.read(&lease_key, remote::MAX_METADATA_BYTES)?,
            context.as_bytes(),
        )?;
        let mut owner = [0; 16];
        owner.copy_from_slice(&lease[..16]);
        let expires = u64::from_le_bytes(lease[16..24].try_into().unwrap());
        anyhow::ensure!(
            owner == config.machine_id || expires <= now || takeover,
            "another machine holds the writer lease until {expires}; use --takeover only after confirming it is offline"
        );
    }
    let mut lease = [0; 32];
    lease[..16].copy_from_slice(&config.machine_id);
    lease[16..24].copy_from_slice(&(now + LEASE_SECONDS).to_le_bytes());
    crypto.encrypt_head(&lease, context.as_bytes())
}

fn head_context(namespace: &str, ref_name: Option<&str>) -> String {
    match ref_name {
        None => format!("furrow:remote-head:v1:{namespace}"),
        Some(name) => format!("furrow:remote-head:v1:{namespace}:ref:{name}"),
    }
}

fn lease_context(ref_name: Option<&str>) -> String {
    match ref_name {
        None => "furrow:writer-lease:v1".to_owned(),
        Some(name) => format!("furrow:writer-lease:v1:ref:{name}"),
    }
}

fn read_remote_head(
    remote: &mut Session,
    config: &PairConfig,
    crypto: &RemoteCrypto,
) -> anyhow::Result<RemoteHead> {
    let ref_name = remote.ref_name().map(str::to_owned);
    let head_key = remote::head_key(ref_name.as_deref());
    let encrypted = remote
        .read(&head_key, remote::MAX_METADATA_BYTES)
        .with_context(|| match &ref_name {
            None => "sync remote has no published HEAD".to_owned(),
            Some(name) => format!("sync remote has no published ref '{name}'"),
        })?;
    let head: RemoteHead = serde_json::from_slice(&crypto.decrypt_metadata(
        &encrypted,
        head_context(&config.namespace, ref_name.as_deref()).as_bytes(),
    )?)
    .context("decode authenticated remote head")?;
    anyhow::ensure!(head.version == 1, "unsupported remote head version");
    Ok(head)
}

fn parse_key(value: &str) -> anyhow::Result<[u8; 32]> {
    let bytes = hex::decode(value).context("sync key must be hexadecimal")?;
    anyhow::ensure!(bytes.len() == 32, "sync key must contain 64 hex characters");
    let mut key = [0; 32];
    key.copy_from_slice(&bytes);
    Ok(key)
}

fn storage_namespace(config: &PairConfig) -> &str {
    config
        .remote_namespace
        .as_deref()
        .unwrap_or(&config.namespace)
}

fn opaque_namespace(key: &[u8; 32], namespace: &str) -> String {
    let mut hasher = blake3::Hasher::new_keyed(key);
    hasher.update(b"furrow remote namespace v1\0");
    hasher.update(namespace.as_bytes());
    hex::encode(&hasher.finalize().as_bytes()[..16])
}

fn local_atomic_write(path: &Path, bytes: &[u8]) -> anyhow::Result<()> {
    let parent = path.parent().context("sync config has no parent")?;
    let mut temporary = tempfile::NamedTempFile::new_in(parent)?;
    std::io::Write::write_all(&mut temporary, bytes)?;
    temporary.as_file().sync_all()?;
    temporary.persist(path).map_err(|error| error.error)?;
    fs::File::open(parent)?.sync_all()?;
    Ok(())
}

fn enqueue(connection: &Connection, id: &ObjectId, kind: ObjectKind) -> anyhow::Result<()> {
    connection.execute(
        "INSERT OR IGNORE INTO queue(id, kind) VALUES(?1, ?2)",
        params![id.as_slice(), kind as u8],
    )?;
    let actual: u8 = connection.query_row(
        "SELECT kind FROM queue WHERE id = ?1",
        params![id.as_slice()],
        |row| row.get(0),
    )?;
    anyhow::ensure!(
        actual == kind as u8,
        "object reached with conflicting kinds"
    );
    Ok(())
}

fn next_batch(
    connection: &Connection,
    limit: usize,
) -> anyhow::Result<Vec<(ObjectId, ObjectKind)>> {
    let mut statement = connection
        .prepare("SELECT id, kind FROM queue WHERE processed = 0 ORDER BY id LIMIT ?1")?;
    let rows = statement.query_map([limit as u64], |row| {
        Ok((row.get::<_, Vec<u8>>(0)?, row.get::<_, u8>(1)?))
    })?;
    rows.map(|row| {
        let (bytes, kind) = row?;
        anyhow::ensure!(bytes.len() == 32, "invalid queued object ID");
        let mut id = [0; 32];
        id.copy_from_slice(&bytes);
        Ok((
            id,
            ObjectKind::from_u8(kind).context("invalid queued object kind")?,
        ))
    })
    .collect()
}

fn process_pulled_object(
    connection: &Connection,
    id: ObjectId,
    expected: ObjectKind,
    bytes: &[u8],
) -> anyhow::Result<()> {
    for (child, child_kind) in bundle::object_edges(expected, bytes)? {
        enqueue(connection, &child, child_kind)?;
    }
    connection.execute(
        "UPDATE queue SET processed = 1 WHERE id = ?1",
        params![id.as_slice()],
    )?;
    Ok(())
}

#[cfg(test)]
mod tests {
    use super::*;
    use crate::model::{Blob, ChunkRef, EntryKind, SealQuality, SnapshotTrigger, Tree, TreeEntry};

    fn snapshot_fixture(store: &ObjectStore) -> ObjectId {
        let chunk = store
            .put_bytes(ObjectKind::Chunk, b"resumable remote data")
            .unwrap();
        let blob = store
            .put_struct(
                ObjectKind::Blob,
                &Blob {
                    chunks: vec![ChunkRef { id: chunk, len: 21 }],
                    total_len: 21,
                },
            )
            .unwrap();
        let tree = store
            .put_struct(
                ObjectKind::Tree,
                &Tree {
                    entries: vec![TreeEntry {
                        name: b"state.txt".to_vec(),
                        kind: EntryKind::File,
                        target: Some(blob),
                        link_target: Vec::new(),
                        mode: 0o100644,
                        size: 21,
                        mtime_secs: 0,
                        mtime_nanos: 0,
                        xattrs: None,
                        class: Default::default(),
                    }],
                    pages: Vec::new(),
                },
            )
            .unwrap();
        store
            .put_struct(
                ObjectKind::Snapshot,
                &Snapshot {
                    root_tree: tree,
                    parent: None,
                    merge_parents: Vec::new(),
                    sealed_at_secs: 0,
                    sealed_at_nanos: 0,
                    quality: SealQuality::Quiescent,
                    trigger: SnapshotTrigger::Manual,
                    label: None,
                    sqlite_backups: Vec::new(),
                    claims: Vec::new(),
                    excluded_paths: Vec::new(),
                },
            )
            .unwrap()
    }

    fn snapshot_fixture_with_content(store: &ObjectStore, content: &[u8]) -> ObjectId {
        let chunk = store.put_bytes(ObjectKind::Chunk, content).unwrap();
        let blob = store
            .put_struct(
                ObjectKind::Blob,
                &Blob {
                    chunks: vec![ChunkRef {
                        id: chunk,
                        len: content.len() as u32,
                    }],
                    total_len: content.len() as u64,
                },
            )
            .unwrap();
        let tree = store
            .put_struct(
                ObjectKind::Tree,
                &Tree {
                    entries: vec![TreeEntry {
                        name: b"state.txt".to_vec(),
                        kind: EntryKind::File,
                        target: Some(blob),
                        link_target: Vec::new(),
                        mode: 0o100644,
                        size: content.len() as u64,
                        mtime_secs: 0,
                        mtime_nanos: 0,
                        xattrs: None,
                        class: Default::default(),
                    }],
                    pages: Vec::new(),
                },
            )
            .unwrap();
        store
            .put_struct(
                ObjectKind::Snapshot,
                &Snapshot {
                    root_tree: tree,
                    parent: None,
                    merge_parents: Vec::new(),
                    sealed_at_secs: 0,
                    sealed_at_nanos: 0,
                    quality: SealQuality::Quiescent,
                    trigger: SnapshotTrigger::Manual,
                    label: None,
                    sqlite_backups: Vec::new(),
                    claims: Vec::new(),
                    excluded_paths: Vec::new(),
                },
            )
            .unwrap()
    }

    #[test]
    fn pair_config_is_private_and_lease_rejects_a_second_writer() {
        let temporary = tempfile::tempdir().unwrap();
        let config_one = temporary.path().join("one.json");
        let remote = temporary.path().join("remote");
        let first = pair(&config_one, &remote, "project", None).unwrap();
        assert_eq!(
            fs::metadata(&config_one).unwrap().permissions().mode() & 0o777,
            0o600
        );
        let first_config = load(&config_one).unwrap();
        let mut first_remote =
            Session::open(&first_config.remote, storage_namespace(&first_config)).unwrap();
        first_remote.begin_writer().unwrap();
        let first_lease = prepare_writer_lease(&mut first_remote, &first_config, false).unwrap();
        first_remote.write("LEASE", &first_lease).unwrap();
        drop(first_remote);

        let config_two = temporary.path().join("two.json");
        pair(&config_two, &remote, "project", Some(&first.key_hex)).unwrap();
        let second_config = load(&config_two).unwrap();
        let mut second_remote =
            Session::open(&second_config.remote, storage_namespace(&second_config)).unwrap();
        second_remote.begin_writer().unwrap();
        assert!(prepare_writer_lease(&mut second_remote, &second_config, false).is_err());
        drop(second_remote);
        let mut takeover_remote =
            Session::open(&second_config.remote, storage_namespace(&second_config)).unwrap();
        takeover_remote.begin_writer().unwrap();
        let takeover = prepare_writer_lease(&mut takeover_remote, &second_config, true).unwrap();
        takeover_remote.write("LEASE", &takeover).unwrap();
    }

    #[test]
    fn pull_resumes_when_an_existing_snapshot_is_missing_descendants() {
        let temporary = tempfile::tempdir().unwrap();
        let source = ObjectStore::open(temporary.path().join("source-store")).unwrap();
        let snapshot = snapshot_fixture(&source);
        let config_path = temporary.path().join("pair.json");
        let remote = temporary.path().join("remote");
        pair(&config_path, &remote, "project", None).unwrap();
        let config = load(&config_path).unwrap();
        push(&source, snapshot, &config, None, false).unwrap();

        let destination = ObjectStore::open(temporary.path().join("destination-store")).unwrap();
        let snapshot_bytes = source.read_bytes(&snapshot, ObjectKind::Snapshot).unwrap();
        assert_eq!(
            destination
                .put_bytes(ObjectKind::Snapshot, &snapshot_bytes)
                .unwrap(),
            snapshot
        );
        let pulled = pull(&destination, &config).unwrap();
        assert_eq!(pulled.snapshot, snapshot);
        assert_eq!(pulled.report.reused_objects, 1);
        assert_eq!(pulled.report.fetched_objects, 3);
        assert_eq!(
            bundle::for_each_reachable(&destination, snapshot, |_, _, _| Ok(())).unwrap(),
            4
        );
    }

    #[test]
    fn default_push_uses_the_well_known_head_and_lease_keys() {
        let temporary = tempfile::tempdir().unwrap();
        let source = ObjectStore::open(temporary.path().join("source-store")).unwrap();
        let snapshot = snapshot_fixture(&source);
        let config_path = temporary.path().join("pair.json");
        let remote = temporary.path().join("remote");
        pair(&config_path, &remote, "project", None).unwrap();
        let config = load(&config_path).unwrap();
        push(&source, snapshot, &config, None, false).unwrap();

        let namespace_root = remote.join(storage_namespace(&config));
        assert!(namespace_root.join("HEAD").is_file());
        assert!(namespace_root.join("LEASE").is_file());
        assert!(!namespace_root.join("refs").exists());
    }

    #[test]
    fn different_refs_on_one_directory_remote_do_not_contend_and_both_heads_are_readable() {
        let temporary = tempfile::tempdir().unwrap();
        let remote = temporary.path().join("remote");
        let config_path = temporary.path().join("pair.json");
        pair(&config_path, &remote, "project", None).unwrap();
        let config = load(&config_path).unwrap();

        // Holding the writer lease on ref "team-a" must not block a
        // concurrent writer on ref "team-b" against the same directory
        // remote: they use independent lease/lock keys.
        let mut lock_a = open_session(&config, Some("team-a")).unwrap();
        lock_a.begin_writer().unwrap();
        let mut lock_b = open_session(&config, Some("team-b")).unwrap();
        lock_b.begin_writer().unwrap();
        // A second writer on the SAME ref is still correctly refused.
        let mut lock_a_again = open_session(&config, Some("team-a")).unwrap();
        assert!(lock_a_again.begin_writer().is_err());
        lock_a.end_writer().unwrap();
        lock_b.end_writer().unwrap();
        drop(lock_a);
        drop(lock_b);
        drop(lock_a_again);

        let store_a = ObjectStore::open(temporary.path().join("store-a")).unwrap();
        let snapshot_a = snapshot_fixture_with_content(&store_a, b"ref team-a state");
        let store_b = ObjectStore::open(temporary.path().join("store-b")).unwrap();
        let snapshot_b = snapshot_fixture_with_content(&store_b, b"ref team-b state");

        let mut push_a = open_session(&config, Some("team-a")).unwrap();
        push_on(&store_a, snapshot_a, &config, None, false, &mut push_a).unwrap();
        let mut push_b = open_session(&config, Some("team-b")).unwrap();
        push_on(&store_b, snapshot_b, &config, None, false, &mut push_b).unwrap();

        // The default, ref-less HEAD/LEASE remain untouched by either push.
        assert_eq!(published_root(&config).unwrap(), None);

        let mut check_a = open_session(&config, Some("team-a")).unwrap();
        let root_a = published_root_on(&config, &mut check_a).unwrap().unwrap();
        let mut check_b = open_session(&config, Some("team-b")).unwrap();
        let root_b = published_root_on(&config, &mut check_b).unwrap().unwrap();
        assert_ne!(root_a, root_b);

        let destination_a = ObjectStore::open(temporary.path().join("dest-a")).unwrap();
        let mut pull_a = open_session(&config, Some("team-a")).unwrap();
        let pulled_a = pull_on(&destination_a, &config, &mut pull_a).unwrap();
        assert_eq!(pulled_a.snapshot, snapshot_a);

        let destination_b = ObjectStore::open(temporary.path().join("dest-b")).unwrap();
        let mut pull_b = open_session(&config, Some("team-b")).unwrap();
        let pulled_b = pull_on(&destination_b, &config, &mut pull_b).unwrap();
        assert_eq!(pulled_b.snapshot, snapshot_b);
    }

    #[test]
    fn pulling_a_nonexistent_ref_fails_with_a_clear_error() {
        let temporary = tempfile::tempdir().unwrap();
        let remote = temporary.path().join("remote");
        let config_path = temporary.path().join("pair.json");
        pair(&config_path, &remote, "project", None).unwrap();
        let config = load(&config_path).unwrap();

        let destination = ObjectStore::open(temporary.path().join("dest")).unwrap();
        let mut session = open_session(&config, Some("does-not-exist")).unwrap();
        let error = pull_on(&destination, &config, &mut session).unwrap_err();
        let message = format!("{error:#}");
        assert!(
            message.contains("does-not-exist"),
            "unexpected error: {message}"
        );
    }

    #[test]
    fn invalid_ref_names_are_rejected_before_connecting() {
        let temporary = tempfile::tempdir().unwrap();
        let remote = temporary.path().join("remote");
        let config_path = temporary.path().join("pair.json");
        pair(&config_path, &remote, "project", None).unwrap();
        let config = load(&config_path).unwrap();
        assert!(open_session(&config, Some("../escape")).is_err());
        assert!(open_session(&config, Some("")).is_err());
    }
}
