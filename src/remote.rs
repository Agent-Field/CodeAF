//! Opaque remote storage adapters for directory and persistent SSH sync.

use crate::model::ObjectId;
use crate::s3_remote::{S3Session, S3Spec};
use anyhow::Context;
use directories::ProjectDirs;
use fs2::FileExt;
use serde::{Deserialize, Serialize};
use std::collections::BTreeSet;
use std::fs::{self, OpenOptions};
use std::io::{BufReader, BufWriter, Read, Seek, SeekFrom, Write};
use std::path::{Path, PathBuf};
use std::process::{Child, ChildStdin, ChildStdout, Command, Stdio};
use std::time::{Duration, Instant, SystemTime};

const REQUEST_MAGIC: &[u8; 5] = b"AGRP\x01";
const RESPONSE_MAGIC: &[u8; 5] = b"AGRS\x01";
const OP_EXISTS: u8 = 1;
const OP_READ: u8 = 2;
const OP_WRITE: u8 = 3;
const OP_LOCK: u8 = 4;
const OP_HAS_OBJECTS: u8 = 5;
const OP_WRITE_FRAME: u8 = 6;
const OP_UNLOCK: u8 = 7;
const OP_PING: u8 = 8;
const OP_WAIT_HEAD: u8 = 9;
const STATUS_OK: u8 = 0;
const STATUS_ERROR: u8 = 1;
const MAX_KEY_BYTES: usize = 96;
const MAX_ERROR_BYTES: usize = 8 * 1024;
pub(crate) const MAX_OBJECT_BYTES: u64 = 256 * 1024 * 1024 + 1024;
pub(crate) const MAX_METADATA_BYTES: u64 = 1024 * 1024 + 1024;
const MAX_HAVE_BATCH: usize = 4096;
const MAX_FRAME_BYTES: u64 = 20 * 1024 * 1024;
const FRAME_POINTER_MAGIC: &[u8; 5] = b"AGFP\x01";

#[derive(Debug, Clone, Serialize, Deserialize)]
#[serde(untagged)]
pub enum RemoteSpec {
    Directory(PathBuf),
    Ssh { ssh: String },
    S3 { s3: S3Spec },
}

impl RemoteSpec {
    pub fn from_input(input: &Path) -> anyhow::Result<Self> {
        let value = input.to_string_lossy();
        if let Some(host) = value.strip_prefix("ssh://") {
            validate_ssh_host(host)?;
            return Ok(Self::Ssh {
                ssh: host.to_owned(),
            });
        }
        if value.starts_with("s3://") {
            return Ok(Self::S3 {
                s3: S3Spec::from_uri(&value)?,
            });
        }
        anyhow::ensure!(!value.contains("://"), "unsupported remote URI scheme");
        let path = if input.is_absolute() {
            input.to_owned()
        } else {
            std::env::current_dir()?.join(input)
        };
        ensure_durable_directory(&path)?;
        Ok(Self::Directory(path.canonicalize()?))
    }

    pub fn display(&self) -> String {
        match self {
            Self::Directory(path) => path.display().to_string(),
            Self::Ssh { ssh } => format!("ssh://{ssh}"),
            Self::S3 { s3 } => s3.display(),
        }
    }

    pub fn validate(&self) -> anyhow::Result<()> {
        match self {
            Self::Directory(path) => {
                anyhow::ensure!(path.is_absolute(), "remote directory must be absolute");
            }
            Self::Ssh { ssh } => validate_ssh_host(ssh)?,
            Self::S3 { s3 } => s3.validate()?,
        }
        Ok(())
    }
}

pub(crate) struct Session {
    inner: SessionInner,
    connect_ms: u64,
    operations: u64,
    last_head_hash: Option<[u8; 32]>,
}

pub(crate) struct HeadChange {
    pub changed: bool,
    pub notify_ms: Option<u64>,
}

enum SessionInner {
    Directory {
        root: PathBuf,
        lock: Option<fs::File>,
    },
    Ssh {
        child: Child,
        input: Option<BufWriter<ChildStdin>>,
        output: BufReader<ChildStdout>,
        locked: bool,
    },
    S3(S3Session),
}

impl Session {
    pub fn open(spec: &RemoteSpec, namespace: &str) -> anyhow::Result<Self> {
        let started = Instant::now();
        validate_namespace(namespace)?;
        spec.validate()?;
        let inner = match spec {
            RemoteSpec::Directory(path) => {
                let root = path.join(namespace);
                ensure_durable_directory(&root.join("objects"))?;
                SessionInner::Directory { root, lock: None }
            }
            RemoteSpec::Ssh { ssh } => {
                let program = std::env::var_os("AGIT_SSH_COMMAND").unwrap_or_else(|| "ssh".into());
                let mut child = Command::new(program)
                    .arg("-T")
                    .arg("-o")
                    .arg("BatchMode=yes")
                    .arg("--")
                    .arg(ssh)
                    .arg("agit")
                    .arg("__remote")
                    .arg(namespace)
                    .stdin(Stdio::piped())
                    .stdout(Stdio::piped())
                    .stderr(Stdio::inherit())
                    .spawn()
                    .with_context(|| format!("start SSH sync helper on {ssh}"))?;
                let input = BufWriter::new(child.stdin.take().context("SSH helper has no stdin")?);
                let output =
                    BufReader::new(child.stdout.take().context("SSH helper has no stdout")?);
                SessionInner::Ssh {
                    child,
                    input: Some(input),
                    output,
                    locked: false,
                }
            }
            RemoteSpec::S3 { s3 } => SessionInner::S3(S3Session::open(s3, namespace)?),
        };
        let mut session = Self {
            inner,
            connect_ms: 0,
            operations: 0,
            last_head_hash: None,
        };
        if let SessionInner::Ssh { input, output, .. } = &mut session.inner {
            request(
                input.as_mut().context("SSH helper input is closed")?,
                OP_PING,
                "",
                &[],
            )?;
            read_ok_response(output, MAX_ERROR_BYTES as u64)?;
        }
        session.connect_ms = elapsed_ms(started);
        Ok(session)
    }

    pub fn begin_operation(&mut self) -> (u64, bool) {
        let reused = self.operations > 0;
        self.operations += 1;
        (if reused { 0 } else { self.connect_ms }, reused)
    }

    pub fn begin_writer(&mut self) -> anyhow::Result<()> {
        match &mut self.inner {
            SessionInner::Directory { root, lock } => {
                anyhow::ensure!(lock.is_none(), "remote writer lock is already held");
                let path = root.join("LEASE.lock");
                let file = OpenOptions::new()
                    .create(true)
                    .truncate(false)
                    .read(true)
                    .write(true)
                    .open(path)?;
                file.try_lock_exclusive()
                    .context("another sync operation is updating the remote")?;
                *lock = Some(file);
                Ok(())
            }
            SessionInner::Ssh {
                input,
                output,
                locked,
                ..
            } => {
                anyhow::ensure!(!*locked, "remote writer lock is already held");
                request(
                    input.as_mut().context("SSH helper input is closed")?,
                    OP_LOCK,
                    "",
                    &[],
                )?;
                read_ok_response(output, MAX_ERROR_BYTES as u64)?;
                *locked = true;
                Ok(())
            }
            SessionInner::S3(session) => session.begin_writer(),
        }
    }

    pub fn end_writer(&mut self) -> anyhow::Result<()> {
        match &mut self.inner {
            SessionInner::Directory { lock, .. } => {
                if let Some(file) = lock.take() {
                    FileExt::unlock(&file)?;
                }
                Ok(())
            }
            SessionInner::Ssh {
                input,
                output,
                locked,
                ..
            } => {
                if !*locked {
                    return Ok(());
                }
                request(
                    input.as_mut().context("SSH helper input is closed")?,
                    OP_UNLOCK,
                    "",
                    &[],
                )?;
                read_ok_response(output, MAX_ERROR_BYTES as u64)?;
                *locked = false;
                Ok(())
            }
            SessionInner::S3(session) => {
                session.end_writer();
                Ok(())
            }
        }
    }

    pub fn exists(&mut self, key: &str) -> anyhow::Result<bool> {
        validate_key(key)?;
        match &mut self.inner {
            SessionInner::Directory { root, .. } => Ok(root.join(key).is_file()),
            SessionInner::Ssh { input, output, .. } => {
                request(
                    input.as_mut().context("SSH helper input is closed")?,
                    OP_EXISTS,
                    key,
                    &[],
                )?;
                let payload = read_ok_response(output, 1)?;
                anyhow::ensure!(payload.len() == 1, "invalid SSH exists response");
                Ok(payload[0] != 0)
            }
            SessionInner::S3(session) => session.exists(key),
        }
    }

    pub fn read(&mut self, key: &str, limit: u64) -> anyhow::Result<Vec<u8>> {
        validate_key(key)?;
        anyhow::ensure!(limit <= MAX_OBJECT_BYTES, "invalid remote read limit");
        let bytes = match &mut self.inner {
            SessionInner::Directory { root, .. } => read_remote_value(root, key, limit),
            SessionInner::Ssh { input, output, .. } => {
                request(
                    input.as_mut().context("SSH helper input is closed")?,
                    OP_READ,
                    key,
                    &limit.to_le_bytes(),
                )?;
                read_ok_response(output, limit)
            }
            SessionInner::S3(session) => session.read(key, limit),
        }?;
        if key == "HEAD" {
            self.last_head_hash = Some(*blake3::hash(&bytes).as_bytes());
        }
        Ok(bytes)
    }

    pub fn read_many(
        &mut self,
        values: &[(String, u64)],
        mut handle: impl FnMut(usize, Vec<u8>) -> anyhow::Result<()>,
    ) -> anyhow::Result<()> {
        for (key, limit) in values {
            validate_key(key)?;
            anyhow::ensure!(*limit <= key_limit(key)?, "invalid remote read limit");
        }
        match &mut self.inner {
            SessionInner::Directory { root, .. } => {
                for (index, (key, limit)) in values.iter().enumerate() {
                    handle(index, read_remote_value(root, key, *limit)?)?;
                }
            }
            SessionInner::Ssh { input, output, .. } => {
                let input = input.as_mut().context("SSH helper input is closed")?;
                for (key, limit) in values {
                    request(input, OP_READ, key, &limit.to_le_bytes())?;
                }
                for (index, (_, limit)) in values.iter().enumerate() {
                    handle(index, read_ok_response(output, *limit)?)?;
                }
            }
            SessionInner::S3(session) => {
                for (index, (key, limit)) in values.iter().enumerate() {
                    handle(index, session.read(key, *limit)?)?;
                }
            }
        }
        Ok(())
    }

    pub fn write(&mut self, key: &str, bytes: &[u8]) -> anyhow::Result<()> {
        validate_key(key)?;
        let limit = key_limit(key)?;
        anyhow::ensure!(
            bytes.len() as u64 <= limit,
            "remote value exceeds its size limit"
        );
        let result = match &mut self.inner {
            SessionInner::Directory { root, lock } => {
                anyhow::ensure!(lock.is_some(), "remote write requires the writer lock");
                atomic_write(&root.join(key), bytes)
            }
            SessionInner::Ssh { input, output, .. } => {
                request(
                    input.as_mut().context("SSH helper input is closed")?,
                    OP_WRITE,
                    key,
                    bytes,
                )?;
                read_ok_response(output, MAX_ERROR_BYTES as u64)?;
                Ok(())
            }
            SessionInner::S3(session) => session.write(key, bytes),
        };
        if result.is_ok() && key == "HEAD" {
            self.last_head_hash = Some(*blake3::hash(bytes).as_bytes());
        }
        result
    }

    pub fn write_many(&mut self, values: &[(String, Vec<u8>)]) -> anyhow::Result<()> {
        for (key, bytes) in values {
            validate_key(key)?;
            anyhow::ensure!(
                bytes.len() as u64 <= key_limit(key)?,
                "remote value exceeds its size limit"
            );
        }
        match &mut self.inner {
            SessionInner::Directory { root, lock } => {
                anyhow::ensure!(lock.is_some(), "remote write requires the writer lock");
                for (key, bytes) in values {
                    atomic_write(&root.join(key), bytes)?;
                }
                Ok(())
            }
            SessionInner::Ssh { input, output, .. } => {
                let input = input.as_mut().context("SSH helper input is closed")?;
                let capacity = 4 + values
                    .iter()
                    .map(|(key, bytes)| 2 + 8 + key.len() + bytes.len())
                    .sum::<usize>();
                let mut frame = Vec::with_capacity(capacity);
                frame.extend_from_slice(&(values.len() as u32).to_le_bytes());
                for (key, bytes) in values {
                    frame.extend_from_slice(&(key.len() as u16).to_le_bytes());
                    frame.extend_from_slice(&(bytes.len() as u64).to_le_bytes());
                    frame.extend_from_slice(key.as_bytes());
                    frame.extend_from_slice(bytes);
                }
                anyhow::ensure!(
                    frame.len() as u64 <= MAX_FRAME_BYTES,
                    "remote frame is too large"
                );
                request(input, OP_WRITE_FRAME, "", &frame)?;
                read_ok_response(output, MAX_ERROR_BYTES as u64)?;
                Ok(())
            }
            SessionInner::S3(session) => {
                for (key, bytes) in values {
                    session.write(key, bytes)?;
                }
                Ok(())
            }
        }
    }

    pub fn has_objects(&mut self, ids: &[ObjectId]) -> anyhow::Result<Vec<bool>> {
        anyhow::ensure!(
            ids.len() <= MAX_HAVE_BATCH,
            "remote have batch is too large"
        );
        match &mut self.inner {
            SessionInner::Directory { root, .. } => Ok(ids
                .iter()
                .map(|id| root.join(object_key(id)).is_file())
                .collect()),
            SessionInner::Ssh { input, output, .. } => {
                let mut payload = Vec::with_capacity(4 + ids.len() * 32);
                payload.extend_from_slice(&(ids.len() as u32).to_le_bytes());
                for id in ids {
                    payload.extend_from_slice(id);
                }
                request(
                    input.as_mut().context("SSH helper input is closed")?,
                    OP_HAS_OBJECTS,
                    "",
                    &payload,
                )?;
                let response = read_ok_response(output, ids.len() as u64)?;
                anyhow::ensure!(response.len() == ids.len(), "invalid SSH have response");
                Ok(response.into_iter().map(|value| value != 0).collect())
            }
            SessionInner::S3(session) => session.has_objects(ids),
        }
    }

    pub fn wait_for_head_change(&mut self, timeout: Duration) -> anyhow::Result<HeadChange> {
        let timeout = if matches!(&self.inner, SessionInner::S3(_)) {
            timeout
        } else {
            timeout.min(Duration::from_secs(1))
        };
        let timeout_ms = timeout.as_millis().min(30_000) as u64;
        let Some(known) = self.last_head_hash else {
            std::thread::sleep(timeout);
            return Ok(HeadChange {
                changed: true,
                notify_ms: None,
            });
        };
        match &mut self.inner {
            SessionInner::Directory { root, .. } => {
                let notify_ms = wait_for_head_hash_change(root, &known, timeout_ms)?;
                Ok(HeadChange {
                    changed: notify_ms.is_some(),
                    notify_ms,
                })
            }
            SessionInner::Ssh { input, output, .. } => {
                let mut payload = Vec::with_capacity(40);
                payload.extend_from_slice(&timeout_ms.to_le_bytes());
                payload.extend_from_slice(&known);
                request(
                    input.as_mut().context("SSH helper input is closed")?,
                    OP_WAIT_HEAD,
                    "",
                    &payload,
                )?;
                let response = read_ok_response(output, 9)?;
                anyhow::ensure!(response.len() == 9, "invalid SSH notification response");
                let changed = response[0] != 0;
                let measured = u64::from_le_bytes(response[1..].try_into().unwrap());
                Ok(HeadChange {
                    changed,
                    notify_ms: (changed && measured != u64::MAX).then_some(measured),
                })
            }
            SessionInner::S3(session) => {
                std::thread::sleep(timeout);
                let bytes = session.read("HEAD", MAX_METADATA_BYTES)?;
                Ok(HeadChange {
                    changed: blake3::hash(&bytes).as_bytes() != &known,
                    notify_ms: None,
                })
            }
        }
    }
}

impl Drop for Session {
    fn drop(&mut self) {
        match &mut self.inner {
            SessionInner::Directory { lock, .. } => {
                if let Some(file) = lock.take() {
                    let _ = FileExt::unlock(&file);
                }
            }
            SessionInner::Ssh { child, input, .. } => {
                if let Some(mut input) = input.take() {
                    let _ = input.flush();
                    drop(input);
                }
                for _ in 0..50 {
                    if child.try_wait().ok().flatten().is_some() {
                        return;
                    }
                    std::thread::sleep(std::time::Duration::from_millis(10));
                }
                let _ = child.kill();
                let _ = child.wait();
            }
            SessionInner::S3(_) => {}
        }
    }
}

pub fn serve(namespace: &str) -> anyhow::Result<()> {
    validate_namespace(namespace)?;
    let root = remote_helper_root()?.join(namespace);
    ensure_durable_directory(&root.join("objects"))?;
    let mut input = BufReader::new(std::io::stdin().lock());
    let mut output = BufWriter::new(std::io::stdout().lock());
    let mut lock: Option<fs::File> = None;
    loop {
        let Some(frame) = read_request(&mut input)? else {
            break;
        };
        let result = handle_request(&root, &mut lock, frame.op, &frame.key, &frame.payload);
        match result {
            Ok(payload) => write_response(&mut output, STATUS_OK, &payload)?,
            Err(error) => {
                let message = error.to_string();
                let bytes = message.as_bytes();
                write_response(
                    &mut output,
                    STATUS_ERROR,
                    &bytes[..bytes.len().min(MAX_ERROR_BYTES)],
                )?;
            }
        }
    }
    Ok(())
}

struct RequestFrame {
    op: u8,
    key: String,
    payload: Vec<u8>,
}

fn read_request(input: &mut impl Read) -> anyhow::Result<Option<RequestFrame>> {
    let mut magic = [0_u8; 5];
    match input.read_exact(&mut magic) {
        Ok(()) => {}
        Err(error) if error.kind() == std::io::ErrorKind::UnexpectedEof => return Ok(None),
        Err(error) => return Err(error.into()),
    }
    anyhow::ensure!(&magic == REQUEST_MAGIC, "invalid remote request header");
    let mut fixed = [0_u8; 11];
    input.read_exact(&mut fixed)?;
    let op = fixed[0];
    let key_len = u16::from_le_bytes(fixed[1..3].try_into().unwrap()) as usize;
    let payload_len = u64::from_le_bytes(fixed[3..11].try_into().unwrap());
    anyhow::ensure!(key_len <= MAX_KEY_BYTES, "remote request key is too long");
    let max_payload = match op {
        OP_EXISTS | OP_LOCK | OP_UNLOCK | OP_PING => 0,
        OP_WAIT_HEAD => 40,
        OP_READ => 8,
        OP_WRITE => MAX_OBJECT_BYTES,
        OP_HAS_OBJECTS => 4 + (MAX_HAVE_BATCH * 32) as u64,
        OP_WRITE_FRAME => MAX_FRAME_BYTES,
        _ => anyhow::bail!("unknown remote operation"),
    };
    anyhow::ensure!(
        payload_len <= max_payload,
        "remote request payload is too large"
    );
    let mut key = vec![0_u8; key_len];
    input.read_exact(&mut key)?;
    let key = String::from_utf8(key).context("remote request key is not UTF-8")?;
    if !key.is_empty() {
        validate_key(&key)?;
    }
    if op == OP_WRITE {
        anyhow::ensure!(payload_len <= key_limit(&key)?, "remote value is too large");
    }
    let mut payload = vec![0_u8; payload_len as usize];
    input.read_exact(&mut payload)?;
    Ok(Some(RequestFrame { op, key, payload }))
}

fn handle_request(
    root: &Path,
    lock: &mut Option<fs::File>,
    op: u8,
    key: &str,
    payload: &[u8],
) -> anyhow::Result<Vec<u8>> {
    match op {
        OP_EXISTS => Ok(vec![root.join(key).is_file() as u8]),
        OP_READ => {
            let limit = u64::from_le_bytes(payload.try_into().context("invalid read limit")?);
            anyhow::ensure!(limit <= key_limit(key)?, "invalid remote read limit");
            read_remote_value(root, key, limit)
        }
        OP_WRITE => {
            anyhow::ensure!(lock.is_some(), "remote write requires the writer lock");
            anyhow::ensure!(
                payload.len() as u64 <= key_limit(key)?,
                "remote value is too large"
            );
            atomic_write(&root.join(key), payload)?;
            Ok(Vec::new())
        }
        OP_LOCK => {
            anyhow::ensure!(lock.is_none(), "remote writer lock is already held");
            let file = OpenOptions::new()
                .create(true)
                .truncate(false)
                .read(true)
                .write(true)
                .open(root.join("LEASE.lock"))?;
            file.try_lock_exclusive()
                .context("another sync operation is updating the remote")?;
            *lock = Some(file);
            Ok(Vec::new())
        }
        OP_UNLOCK => {
            if let Some(file) = lock.take() {
                FileExt::unlock(&file)?;
            }
            Ok(Vec::new())
        }
        OP_PING => Ok(Vec::new()),
        OP_WAIT_HEAD => {
            anyhow::ensure!(payload.len() == 40, "invalid wait request");
            let timeout_ms = u64::from_le_bytes(payload[..8].try_into().unwrap()).min(30_000);
            let known: [u8; 32] = payload[8..].try_into().unwrap();
            let notify_ms = wait_for_head_hash_change(root, &known, timeout_ms)?;
            let mut response = Vec::with_capacity(9);
            response.push(notify_ms.is_some() as u8);
            response.extend_from_slice(&notify_ms.unwrap_or(u64::MAX).to_le_bytes());
            Ok(response)
        }
        OP_HAS_OBJECTS => {
            anyhow::ensure!(payload.len() >= 4, "invalid have request");
            let count = u32::from_le_bytes(payload[..4].try_into().unwrap()) as usize;
            anyhow::ensure!(count <= MAX_HAVE_BATCH, "have request is too large");
            anyhow::ensure!(
                payload.len() == 4 + count * 32,
                "invalid have request length"
            );
            Ok(payload[4..]
                .chunks_exact(32)
                .map(|id| {
                    let mut object = [0_u8; 32];
                    object.copy_from_slice(id);
                    root.join(object_key(&object)).is_file() as u8
                })
                .collect())
        }
        OP_WRITE_FRAME => {
            anyhow::ensure!(lock.is_some(), "remote write requires the writer lock");
            let values = decode_write_frame(payload)?;
            write_packed_frame(root, payload, &values)?;
            Ok(Vec::new())
        }
        _ => anyhow::bail!("unknown remote operation"),
    }
}

fn request(output: &mut impl Write, op: u8, key: &str, payload: &[u8]) -> anyhow::Result<()> {
    anyhow::ensure!(key.len() <= MAX_KEY_BYTES, "remote key is too long");
    output.write_all(REQUEST_MAGIC)?;
    output.write_all(&[op])?;
    output.write_all(&(key.len() as u16).to_le_bytes())?;
    output.write_all(&(payload.len() as u64).to_le_bytes())?;
    output.write_all(key.as_bytes())?;
    output.write_all(payload)?;
    output.flush()?;
    Ok(())
}

fn write_response(output: &mut impl Write, status: u8, payload: &[u8]) -> anyhow::Result<()> {
    output.write_all(RESPONSE_MAGIC)?;
    output.write_all(&[status])?;
    output.write_all(&(payload.len() as u64).to_le_bytes())?;
    output.write_all(payload)?;
    output.flush()?;
    Ok(())
}

fn read_ok_response(input: &mut impl Read, limit: u64) -> anyhow::Result<Vec<u8>> {
    let mut header = [0_u8; 14];
    input
        .read_exact(&mut header)
        .context("SSH sync helper exited")?;
    anyhow::ensure!(
        &header[..5] == RESPONSE_MAGIC,
        "invalid SSH response header"
    );
    let status = header[5];
    let length = u64::from_le_bytes(header[6..14].try_into().unwrap());
    let allowed = if status == STATUS_OK {
        limit
    } else {
        MAX_ERROR_BYTES as u64
    };
    anyhow::ensure!(length <= allowed, "SSH response exceeds its size limit");
    let mut payload = vec![0_u8; length as usize];
    input.read_exact(&mut payload)?;
    if status == STATUS_OK {
        Ok(payload)
    } else {
        anyhow::bail!("remote helper: {}", String::from_utf8_lossy(&payload))
    }
}

fn elapsed_ms(started: Instant) -> u64 {
    started.elapsed().as_millis().try_into().unwrap_or(u64::MAX)
}

fn wait_for_head_hash_change(
    root: &Path,
    known: &[u8; 32],
    timeout_ms: u64,
) -> anyhow::Result<Option<u64>> {
    let deadline = Instant::now() + Duration::from_millis(timeout_ms);
    loop {
        if let Ok(bytes) = read_remote_value(root, "HEAD", MAX_METADATA_BYTES) {
            if blake3::hash(&bytes).as_bytes() != known {
                let modified = fs::metadata(root.join("HEAD"))?.modified()?;
                let notify_ms = SystemTime::now()
                    .duration_since(modified)
                    .unwrap_or_default()
                    .as_millis()
                    .try_into()
                    .unwrap_or(u64::MAX);
                return Ok(Some(notify_ms));
            }
        }
        if Instant::now() >= deadline {
            return Ok(None);
        }
        std::thread::sleep(Duration::from_millis(20));
    }
}

pub(crate) fn object_key(id: &ObjectId) -> String {
    let value = hex::encode(id);
    format!("objects/{}/{}", &value[..2], &value[2..])
}

fn key_limit(key: &str) -> anyhow::Result<u64> {
    validate_key(key)?;
    Ok(if key.starts_with("objects/") {
        MAX_OBJECT_BYTES
    } else {
        MAX_METADATA_BYTES
    })
}

fn validate_key(key: &str) -> anyhow::Result<()> {
    if matches!(key, "HEAD" | "LEASE") {
        return Ok(());
    }
    let Some(value) = key.strip_prefix("objects/") else {
        anyhow::bail!("invalid remote key")
    };
    anyhow::ensure!(
        value.len() == 65
            && value.as_bytes()[2] == b'/'
            && value.bytes().enumerate().all(|(index, byte)| index == 2
                || byte.is_ascii_hexdigit() && !byte.is_ascii_uppercase()),
        "invalid remote object key"
    );
    Ok(())
}

pub fn validate_namespace(namespace: &str) -> anyhow::Result<()> {
    anyhow::ensure!(
        !namespace.is_empty() && namespace.len() <= 96,
        "invalid sync namespace"
    );
    anyhow::ensure!(
        namespace
            .bytes()
            .all(|byte| byte.is_ascii_alphanumeric() || matches!(byte, b'-' | b'_' | b'.')),
        "sync namespace may contain only letters, numbers, dot, dash, and underscore"
    );
    anyhow::ensure!(
        namespace != "." && namespace != "..",
        "invalid sync namespace"
    );
    Ok(())
}

fn validate_ssh_host(host: &str) -> anyhow::Result<()> {
    anyhow::ensure!(!host.is_empty() && host.len() <= 255, "invalid SSH host");
    anyhow::ensure!(!host.starts_with('-'), "invalid SSH host");
    anyhow::ensure!(
        host.bytes().all(|byte| {
            byte.is_ascii_alphanumeric()
                || matches!(byte, b'@' | b'.' | b'-' | b'_' | b':' | b'[' | b']' | b'%')
        }),
        "SSH host contains unsupported characters"
    );
    Ok(())
}

fn remote_helper_root() -> anyhow::Result<PathBuf> {
    if let Some(root) = std::env::var_os("AGIT_REMOTE_DATA_DIR") {
        return Ok(PathBuf::from(root));
    }
    let directories = ProjectDirs::from("dev", "agit", "agit")
        .context("could not determine agit remote data directory")?;
    Ok(directories.data_local_dir().join("remote-v1"))
}

fn read_bounded(path: &Path, limit: u64) -> anyhow::Result<Vec<u8>> {
    let file = fs::File::open(path)?;
    let length = file.metadata()?.len();
    anyhow::ensure!(length <= limit, "remote file exceeds its size limit");
    let mut bytes = Vec::with_capacity(length as usize);
    file.take(limit + 1).read_to_end(&mut bytes)?;
    anyhow::ensure!(
        bytes.len() as u64 <= limit,
        "remote file exceeds its size limit"
    );
    Ok(bytes)
}

fn decode_write_frame(payload: &[u8]) -> anyhow::Result<Vec<(String, &[u8])>> {
    anyhow::ensure!(payload.len() >= 4, "invalid remote write frame");
    let count = u32::from_le_bytes(payload[..4].try_into().unwrap()) as usize;
    anyhow::ensure!(
        count <= MAX_HAVE_BATCH,
        "remote write frame has too many objects"
    );
    let mut cursor = 4_usize;
    let mut values = Vec::with_capacity(count);
    for _ in 0..count {
        anyhow::ensure!(cursor + 10 <= payload.len(), "truncated remote write frame");
        let key_len = u16::from_le_bytes(payload[cursor..cursor + 2].try_into().unwrap()) as usize;
        let value_len = u64::from_le_bytes(payload[cursor + 2..cursor + 10].try_into().unwrap());
        cursor += 10;
        anyhow::ensure!(key_len <= MAX_KEY_BYTES, "remote frame key is too long");
        let value_len = usize::try_from(value_len).context("remote frame value is too large")?;
        let end = cursor
            .checked_add(key_len)
            .and_then(|value| value.checked_add(value_len))
            .context("remote frame length overflow")?;
        anyhow::ensure!(end <= payload.len(), "truncated remote write frame");
        let key = std::str::from_utf8(&payload[cursor..cursor + key_len])?.to_owned();
        cursor += key_len;
        validate_key(&key)?;
        anyhow::ensure!(
            value_len as u64 <= key_limit(&key)?,
            "remote frame value is too large"
        );
        values.push((key, &payload[cursor..cursor + value_len]));
        cursor += value_len;
    }
    anyhow::ensure!(
        cursor == payload.len(),
        "remote write frame has trailing bytes"
    );
    Ok(values)
}

fn atomic_write_relaxed(path: &Path, bytes: &[u8]) -> anyhow::Result<()> {
    let parent = path.parent().context("remote object has no parent")?;
    ensure_durable_directory(parent)?;
    let mut temporary = tempfile::NamedTempFile::new_in(parent)?;
    temporary.write_all(bytes)?;
    temporary.persist(path).map_err(|error| error.error)?;
    Ok(())
}

fn write_packed_frame(
    root: &Path,
    payload: &[u8],
    values: &[(String, &[u8])],
) -> anyhow::Result<()> {
    let frame_id = hex::encode(blake3::hash(payload).as_bytes());
    let frame_key = format!("frames/{frame_id}");
    let frame_path = root.join(&frame_key);
    if !frame_path.is_file() {
        atomic_write(&frame_path, payload)?;
    }
    let payload_start = payload.as_ptr() as usize;
    let mut parents = BTreeSet::new();
    for (key, bytes) in values {
        let offset = (bytes.as_ptr() as usize)
            .checked_sub(payload_start)
            .context("frame value is outside its payload")? as u64;
        let mut pointer = Vec::with_capacity(85);
        pointer.extend_from_slice(FRAME_POINTER_MAGIC);
        pointer.extend_from_slice(frame_id.as_bytes());
        pointer.extend_from_slice(&offset.to_le_bytes());
        pointer.extend_from_slice(&(bytes.len() as u64).to_le_bytes());
        let path = root.join(key);
        atomic_write_relaxed(&path, &pointer)?;
        parents.insert(
            path.parent()
                .context("remote pointer has no parent")?
                .to_owned(),
        );
    }
    for parent in parents {
        fs::File::open(parent)?.sync_all()?;
    }
    fs::File::open(root)?.sync_all()?;
    Ok(())
}

fn read_remote_value(root: &Path, key: &str, limit: u64) -> anyhow::Result<Vec<u8>> {
    let pointer = read_bounded(&root.join(key), limit.max(85))?;
    if !pointer.starts_with(FRAME_POINTER_MAGIC) {
        anyhow::ensure!(
            pointer.len() as u64 <= limit,
            "remote file exceeds its size limit"
        );
        return Ok(pointer);
    }
    anyhow::ensure!(pointer.len() == 85, "invalid remote frame pointer");
    let frame_id = std::str::from_utf8(&pointer[5..69])?;
    anyhow::ensure!(
        frame_id.len() == 64
            && frame_id
                .bytes()
                .all(|byte| byte.is_ascii_hexdigit() && !byte.is_ascii_uppercase()),
        "invalid remote frame ID"
    );
    let offset = u64::from_le_bytes(pointer[69..77].try_into().unwrap());
    let length = u64::from_le_bytes(pointer[77..85].try_into().unwrap());
    anyhow::ensure!(length <= limit, "remote frame value exceeds its size limit");
    let mut frame = fs::File::open(root.join("frames").join(frame_id))?;
    let frame_len = frame.metadata()?.len();
    anyhow::ensure!(
        offset
            .checked_add(length)
            .is_some_and(|end| end <= frame_len),
        "remote frame pointer is out of bounds"
    );
    frame.seek(SeekFrom::Start(offset))?;
    let mut bytes = vec![0; length as usize];
    frame.read_exact(&mut bytes)?;
    Ok(bytes)
}

fn atomic_write(path: &Path, bytes: &[u8]) -> anyhow::Result<()> {
    let parent = path.parent().context("remote object has no parent")?;
    ensure_durable_directory(parent)?;
    let mut temporary = tempfile::NamedTempFile::new_in(parent)?;
    temporary.write_all(bytes)?;
    temporary.as_file().sync_all()?;
    temporary.persist(path).map_err(|error| error.error)?;
    fs::File::open(parent)?.sync_all()?;
    Ok(())
}

fn ensure_durable_directory(path: &Path) -> anyhow::Result<()> {
    if path.is_dir() {
        return Ok(());
    }
    let parent = path.parent().context("directory has no parent")?;
    ensure_durable_directory(parent)?;
    match fs::create_dir(path) {
        Ok(()) => {}
        Err(error) if error.kind() == std::io::ErrorKind::AlreadyExists && path.is_dir() => {}
        Err(error) => return Err(error.into()),
    }
    fs::File::open(path)?.sync_all()?;
    fs::File::open(parent)?.sync_all()?;
    Ok(())
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn helper_protocol_round_trips_and_rejects_unlocked_writes() {
        let temporary = tempfile::tempdir().unwrap();
        let root = temporary.path().join("remote");
        ensure_durable_directory(&root.join("objects")).unwrap();
        let mut lock = None;
        assert!(handle_request(&root, &mut lock, OP_WRITE, "HEAD", b"x").is_err());
        handle_request(&root, &mut lock, OP_LOCK, "", &[]).unwrap();
        handle_request(&root, &mut lock, OP_WRITE, "HEAD", b"value").unwrap();
        assert_eq!(
            handle_request(
                &root,
                &mut lock,
                OP_READ,
                "HEAD",
                &MAX_METADATA_BYTES.to_le_bytes()
            )
            .unwrap(),
            b"value"
        );
    }

    #[test]
    fn validates_opaque_keys_and_ssh_hosts() {
        let id = [9_u8; 32];
        assert!(validate_key(&object_key(&id)).is_ok());
        assert!(validate_key("objects/../../HEAD").is_err());
        assert!(validate_ssh_host("developer@example.com").is_ok());
        assert!(validate_ssh_host("-oProxyCommand=bad").is_err());
    }

    #[test]
    fn directory_sessions_hold_the_writer_lock_until_drop() {
        let temporary = tempfile::tempdir().unwrap();
        let spec = RemoteSpec::Directory(temporary.path().to_owned());
        let mut first = Session::open(&spec, "project").unwrap();
        let mut second = Session::open(&spec, "project").unwrap();
        assert!(first.write("HEAD", b"unlocked").is_err());
        first.begin_writer().unwrap();
        assert!(second.begin_writer().is_err());
        first.write("HEAD", b"locked").unwrap();
        drop(first);
        second.begin_writer().unwrap();
        assert_eq!(second.read("HEAD", 32).unwrap(), b"locked");
    }
}
