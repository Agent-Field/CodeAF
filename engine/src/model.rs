use crate::content_class::ContentClass;
use serde::{Deserialize, Serialize};

pub type ObjectId = [u8; 32];

#[derive(Debug, Clone, Copy, PartialEq, Eq)]
#[repr(u8)]
pub enum ObjectKind {
    Chunk = 1,
    Blob = 2,
    Tree = 3,
    Snapshot = 4,
    Xattrs = 5,
}

impl ObjectKind {
    pub fn from_u8(value: u8) -> Option<Self> {
        match value {
            1 => Some(Self::Chunk),
            2 => Some(Self::Blob),
            3 => Some(Self::Tree),
            4 => Some(Self::Snapshot),
            5 => Some(Self::Xattrs),
            _ => None,
        }
    }

    pub fn domain(self) -> &'static [u8] {
        match self {
            Self::Chunk => b"furrow:chunk:v1\0",
            Self::Blob => b"furrow:blob:v1\0",
            Self::Tree => b"furrow:tree:v1\0",
            Self::Snapshot => b"furrow:snapshot:v1\0",
            Self::Xattrs => b"furrow:xattrs:v1\0",
        }
    }
}

#[derive(Debug, Clone, Serialize, Deserialize, PartialEq, Eq)]
pub struct ChunkRef {
    pub id: ObjectId,
    pub len: u32,
}

#[derive(Debug, Clone, Serialize, Deserialize, PartialEq, Eq)]
pub struct Blob {
    pub chunks: Vec<ChunkRef>,
    pub total_len: u64,
}

#[derive(Debug, Clone, Serialize, Deserialize, PartialEq, Eq)]
pub enum EntryKind {
    File,
    Directory,
    Symlink,
    Fifo,
    SocketMarker,
}

#[derive(Debug, Clone, Serialize, Deserialize, PartialEq, Eq)]
pub struct TreeEntry {
    #[serde(with = "serde_bytes")]
    pub name: Vec<u8>,
    pub kind: EntryKind,
    pub target: Option<ObjectId>,
    #[serde(default, with = "serde_bytes")]
    pub link_target: Vec<u8>,
    pub mode: u32,
    pub size: u64,
    pub mtime_secs: i64,
    pub mtime_nanos: u32,
    pub xattrs: Option<ObjectId>,
    #[serde(default)]
    pub class: ContentClass,
}

#[derive(Debug, Clone, Serialize, Deserialize, PartialEq, Eq)]
pub struct Tree {
    #[serde(default, skip_serializing_if = "Vec::is_empty")]
    pub entries: Vec<TreeEntry>,
    #[serde(default, skip_serializing_if = "Vec::is_empty")]
    pub pages: Vec<TreePage>,
}

#[derive(Debug, Clone, Serialize, Deserialize, PartialEq, Eq)]
pub struct TreePage {
    #[serde(with = "serde_bytes")]
    pub first_name: Vec<u8>,
    #[serde(with = "serde_bytes")]
    pub last_name: Vec<u8>,
    pub entry_count: u64,
    pub target: ObjectId,
}

#[derive(Debug, Clone, Serialize, Deserialize, PartialEq, Eq)]
pub enum SealQuality {
    Quiescent,
    Turbulent,
}

#[derive(Debug, Clone, Serialize, Deserialize, PartialEq, Eq)]
pub enum SnapshotTrigger {
    Initial,
    Manual,
    Watcher,
    PreRewind,
    ForkBase,
    AgentRun,
    MergeSource,
    PreMerge,
    Merge,
    SyncLocal,
    SyncPush,
    SyncPull,
    Inspection,
    Claim,
    Release,
    Coord,
}

#[derive(Debug, Clone, Serialize, Deserialize, PartialEq, Eq)]
pub struct ClaimRecord {
    pub id: String,
    pub pattern: String,
    pub owner: String,
    pub workspace_id: String,
    pub created_at: i64,
    pub expires_at: i64,
}

#[derive(Debug, Clone, Serialize, Deserialize, PartialEq, Eq)]
pub struct Snapshot {
    pub root_tree: ObjectId,
    pub parent: Option<ObjectId>,
    #[serde(default)]
    pub merge_parents: Vec<ObjectId>,
    pub sealed_at_secs: i64,
    pub sealed_at_nanos: u32,
    pub quality: SealQuality,
    pub trigger: SnapshotTrigger,
    pub label: Option<String>,
    #[serde(default)]
    pub sqlite_backups: Vec<SqliteBackup>,
    #[serde(default)]
    pub claims: Vec<ClaimRecord>,
    #[serde(default)]
    pub excluded_paths: Vec<String>,
}

#[derive(Debug, Clone, Serialize, Deserialize, PartialEq, Eq)]
pub struct SqliteBackup {
    #[serde(with = "serde_bytes")]
    pub path: Vec<u8>,
    pub blob: ObjectId,
    pub integrity_ok: bool,
}

#[derive(Debug, Clone, Serialize, Deserialize, PartialEq, Eq)]
pub struct XattrEntry {
    #[serde(with = "serde_bytes")]
    pub name: Vec<u8>,
    #[serde(with = "serde_bytes")]
    pub value: Vec<u8>,
}

#[derive(Debug, Clone, Serialize, Deserialize, PartialEq, Eq)]
pub struct Xattrs {
    pub entries: Vec<XattrEntry>,
}

pub fn id_hex(id: &ObjectId) -> String {
    hex::encode(id)
}

pub fn parse_id(value: &str) -> anyhow::Result<ObjectId> {
    let bytes = hex::decode(value)?;
    anyhow::ensure!(
        bytes.len() == 32,
        "snapshot ID must contain 64 hexadecimal characters"
    );
    let mut id = [0_u8; 32];
    id.copy_from_slice(&bytes);
    Ok(id)
}
