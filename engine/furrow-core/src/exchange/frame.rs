//! The AGEF frame: sealed objects packed with a JSON header naming each one.
//!
//! ```text
//! frame   = "AGEF" 0x01 ‖ u32le(header_len) ‖ header ‖ payload
//! header  = {"V":1,"cell_key_id":"<32 hex>","objects":[{"rid","off","len"}, …]}
//! payload = the objects, concatenated in header order
//! ```

use super::ledger::parse_rid;
use crate::model::{id_hex, ObjectId};
use anyhow::Context;
use serde::{Deserialize, Serialize};
use sha2::{Digest, Sha256};
use std::collections::HashSet;

const MAGIC: &[u8; 5] = b"AGEF\x01";
const OBJECT_MAGIC: &[u8; 5] = b"AGEO\x01";
const VERSION: u16 = 1;
/// The store refuses a header past this size, so the engine never writes one.
pub const MAX_HEADER: usize = 1 << 20;
/// The store refuses a frame past this size.
pub const MAX_FRAME: usize = 16 << 20;

#[derive(Serialize, Deserialize)]
struct Header {
    #[serde(rename = "V")]
    version: u16,
    cell_key_id: String,
    objects: Vec<ObjectRef>,
}

#[derive(Serialize, Deserialize)]
struct ObjectRef {
    rid: String,
    off: u64,
    len: u32,
}

/// One sealed object as a frame carries it.
pub struct Sealed<'a> {
    pub rid: ObjectId,
    pub bytes: &'a [u8],
}

/// A finished frame and the rids it holds, in frame order.
pub struct Frame {
    pub bytes: Vec<u8>,
    pub rids: Vec<ObjectId>,
}

impl Frame {
    /// A name that depends only on the content, so a re-export overwrites the
    /// frame it repeats instead of leaving a second copy.
    pub fn file_name(&self) -> String {
        format!("{}.agef", blake3::hash(&self.bytes).to_hex())
    }
}

/// The id a frame gives its cell key: the first 16 bytes of SHA-256, in hex.
pub fn cell_key_id(cell_key: &[u8; 32]) -> String {
    hex::encode(&Sha256::digest(cell_key)[..16])
}

/// Packs `objects` into frames of at most `max_frame` bytes, handing each
/// finished frame to `emit`. An object that cannot fit any frame gets its own.
pub fn pack(
    cell_key_id: &str,
    max_frame: usize,
    objects: impl Iterator<Item = anyhow::Result<(ObjectId, Vec<u8>)>>,
    mut emit: impl FnMut(Frame) -> anyhow::Result<()>,
) -> anyhow::Result<()> {
    let mut pending = Pending::new(cell_key_id, max_frame);
    for object in objects {
        let (rid, sealed) = object?;
        if !pending.fits(&rid, sealed.len()) {
            emit(pending.finish()?)?;
            pending = Pending::new(cell_key_id, max_frame);
        }
        pending.push(rid, &sealed);
    }
    if !pending.is_empty() {
        emit(pending.finish()?)?;
    }
    Ok(())
}

struct Pending<'a> {
    cell_key_id: &'a str,
    max_frame: usize,
    header_len: usize,
    refs: Vec<ObjectRef>,
    rids: Vec<ObjectId>,
    payload: Vec<u8>,
}

impl<'a> Pending<'a> {
    fn new(cell_key_id: &'a str, max_frame: usize) -> Self {
        let empty = Header {
            version: VERSION,
            cell_key_id: cell_key_id.to_owned(),
            objects: Vec::new(),
        };
        Self {
            cell_key_id,
            max_frame,
            header_len: encoded_len(&empty),
            refs: Vec::new(),
            rids: Vec::new(),
            payload: Vec::new(),
        }
    }

    fn is_empty(&self) -> bool {
        self.refs.is_empty()
    }

    fn reference(&self, rid: &ObjectId, len: usize) -> ObjectRef {
        ObjectRef {
            rid: id_hex(rid),
            off: self.payload.len() as u64,
            len: len as u32,
        }
    }

    /// True when adding the object keeps the frame within `max_frame`; the
    /// first object of a frame always fits.
    fn fits(&self, rid: &ObjectId, len: usize) -> bool {
        if self.is_empty() {
            return true;
        }
        let comma = 1;
        let entry = encoded_len(&self.reference(rid, len));
        let grown = MAGIC.len() + 4 + self.header_len + comma + entry + self.payload.len() + len;
        grown <= self.max_frame
    }

    fn push(&mut self, rid: ObjectId, sealed: &[u8]) {
        let entry = self.reference(&rid, sealed.len());
        self.header_len += encoded_len(&entry) + usize::from(!self.is_empty());
        self.refs.push(entry);
        self.rids.push(rid);
        self.payload.extend_from_slice(sealed);
    }

    fn finish(self) -> anyhow::Result<Frame> {
        let header = serde_json::to_vec(&Header {
            version: VERSION,
            cell_key_id: self.cell_key_id.to_owned(),
            objects: self.refs,
        })?;
        anyhow::ensure!(header.len() <= MAX_HEADER, "frame header is too large");
        let mut bytes = Vec::with_capacity(MAGIC.len() + 4 + header.len() + self.payload.len());
        bytes.extend_from_slice(MAGIC);
        bytes.extend_from_slice(&(header.len() as u32).to_le_bytes());
        bytes.extend_from_slice(&header);
        bytes.extend_from_slice(&self.payload);
        Ok(Frame {
            bytes,
            rids: self.rids,
        })
    }
}

fn encoded_len(value: &impl Serialize) -> usize {
    serde_json::to_vec(value).map_or(0, |bytes| bytes.len())
}

/// Splits a frame into its objects, refusing anything the store would refuse.
pub fn decode(frame: &[u8]) -> anyhow::Result<(String, Vec<Sealed<'_>>)> {
    anyhow::ensure!(
        frame.len() <= MAX_FRAME,
        "frame is larger than the store allows"
    );
    anyhow::ensure!(frame.starts_with(MAGIC), "not a frame: bad magic");
    let rest = &frame[MAGIC.len()..];
    let (length, rest) = rest.split_at_checked(4).context("frame is truncated")?;
    let header_len = u32::from_le_bytes(length.try_into()?) as usize;
    anyhow::ensure!(header_len <= MAX_HEADER, "frame header is too large");
    let (header, payload) = rest
        .split_at_checked(header_len)
        .context("frame is truncated")?;
    let header: Header = serde_json::from_slice(header).context("frame header")?;
    anyhow::ensure!(header.version == VERSION, "unsupported frame version");
    anyhow::ensure!(
        is_key_id(&header.cell_key_id),
        "frame cell_key_id is malformed"
    );
    let objects = objects_of(&header.objects, payload)?;
    Ok((header.cell_key_id, objects))
}

fn is_key_id(text: &str) -> bool {
    text.len() == 32
        && text
            .bytes()
            .all(|b| b.is_ascii_digit() || (b'a'..=b'f').contains(&b))
}

fn objects_of<'a>(refs: &[ObjectRef], payload: &'a [u8]) -> anyhow::Result<Vec<Sealed<'a>>> {
    anyhow::ensure!(!refs.is_empty(), "frame holds no objects");
    let mut seen = HashSet::new();
    let mut expected = 0_u64;
    let mut objects = Vec::with_capacity(refs.len());
    for entry in refs {
        anyhow::ensure!(entry.off == expected, "frame objects are not contiguous");
        let rid = parse_rid(&entry.rid).context("frame holds a malformed rid")?;
        anyhow::ensure!(seen.insert(rid), "frame repeats rid {}", entry.rid);
        let end = expected + u64::from(entry.len);
        let bytes = payload
            .get(expected as usize..end as usize)
            .context("frame objects overrun the payload")?;
        anyhow::ensure!(
            bytes.starts_with(OBJECT_MAGIC),
            "frame object {} is not sealed",
            entry.rid
        );
        objects.push(Sealed { rid, bytes });
        expected = end;
    }
    anyhow::ensure!(
        expected == payload.len() as u64,
        "frame payload has trailing bytes"
    );
    Ok(objects)
}
