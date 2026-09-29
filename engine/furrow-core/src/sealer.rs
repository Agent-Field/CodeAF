//! Stage 1 sealing: one `Sealer` per identity, with a scheme per object kind.
//!
//! Structure objects (blob, tree, snapshot, xattrs) are sealed under the cell key.
//! Chunks are sealed convergently: id, key and nonce all derive from the dedup
//! secret, so one identity stores a chunk once and no other identity can open it.

use crate::model::{ObjectId, ObjectKind};
use crate::remote_crypto::RemoteCrypto;
use crate::store::object_id;
use zeroize::{Zeroize, Zeroizing};

const NONCE_LEN: usize = 24;
const CHUNK_RID: &[u8] = b"codeaf:chunk-rid:v1\0";
const CHUNK_KEY: &[u8] = b"codeaf:chunk-key:v1\0";
const CHUNK_NONCE: &[u8] = b"codeaf:chunk-nonce:v1\0";

/// The two secrets of one identity.
pub struct Keys {
    pub cell_key: [u8; 32],
    pub dedup: [u8; 32],
}

impl Drop for Keys {
    fn drop(&mut self) {
        self.cell_key.zeroize();
        self.dedup.zeroize();
    }
}

pub trait Sealer {
    fn remote_id(&self, kind: ObjectKind, id: &ObjectId) -> ObjectId;
    fn seal(
        &self,
        kind: ObjectKind,
        id: &ObjectId,
        bytes: &[u8],
    ) -> anyhow::Result<(ObjectId, Vec<u8>)>;
    /// Opens `sealed`, verifying that `rid` names `id` and that the bytes hash to `id`.
    fn open(
        &self,
        kind: ObjectKind,
        id: &ObjectId,
        rid: &ObjectId,
        sealed: &[u8],
    ) -> anyhow::Result<Vec<u8>>;
}

/// How one object is identified, keyed and nonced on the remote.
trait Scheme {
    fn crypto(&self) -> &RemoteCrypto;
    fn remote_id(&self) -> ObjectId;
    fn nonce(&self) -> [u8; NONCE_LEN];
}

struct Structure<'a> {
    cell: &'a RemoteCrypto,
    id: &'a ObjectId,
}

impl Scheme for Structure<'_> {
    fn crypto(&self) -> &RemoteCrypto {
        self.cell
    }
    fn remote_id(&self) -> ObjectId {
        self.cell.remote_id(self.id)
    }
    fn nonce(&self) -> [u8; NONCE_LEN] {
        self.cell.object_nonce(self.id)
    }
}

struct Convergent<'a> {
    dedup: &'a [u8; 32],
    id: &'a ObjectId,
    crypto: RemoteCrypto,
}

impl<'a> Convergent<'a> {
    fn new(dedup: &'a [u8; 32], id: &'a ObjectId) -> Self {
        let key = Zeroizing::new(keyed(dedup, CHUNK_KEY, id));
        Self {
            dedup,
            id,
            crypto: RemoteCrypto::new(*key),
        }
    }
}

impl Scheme for Convergent<'_> {
    fn crypto(&self) -> &RemoteCrypto {
        &self.crypto
    }
    fn remote_id(&self) -> ObjectId {
        keyed(self.dedup, CHUNK_RID, self.id)
    }
    fn nonce(&self) -> [u8; NONCE_LEN] {
        let key = keyed(self.dedup, CHUNK_KEY, self.id);
        let mut nonce = [0; NONCE_LEN];
        keyed_xof(&key, CHUNK_NONCE, &[], &mut nonce);
        nonce
    }
}

fn keyed(key: &[u8; 32], domain: &[u8], id: &ObjectId) -> [u8; 32] {
    let mut out = [0; 32];
    keyed_xof(key, domain, id, &mut out);
    out
}

fn keyed_xof(key: &[u8; 32], domain: &[u8], id: &[u8], out: &mut [u8]) {
    let mut hasher = blake3::Hasher::new_keyed(key);
    hasher.update(domain);
    hasher.update(id);
    hasher.finalize_xof().fill(out);
}

pub struct CellSealer {
    cell: RemoteCrypto,
    dedup: Zeroizing<[u8; 32]>,
}

impl CellSealer {
    pub fn new(keys: &Keys) -> Self {
        Self {
            cell: RemoteCrypto::new(keys.cell_key),
            dedup: Zeroizing::new(keys.dedup),
        }
    }

    fn scheme<'a>(&'a self, kind: ObjectKind, id: &'a ObjectId) -> Box<dyn Scheme + 'a> {
        match kind {
            ObjectKind::Chunk => Box::new(Convergent::new(&self.dedup, id)),
            _ => Box::new(Structure {
                cell: &self.cell,
                id,
            }),
        }
    }
}

impl Sealer for CellSealer {
    fn remote_id(&self, kind: ObjectKind, id: &ObjectId) -> ObjectId {
        self.scheme(kind, id).remote_id()
    }

    fn seal(
        &self,
        kind: ObjectKind,
        id: &ObjectId,
        bytes: &[u8],
    ) -> anyhow::Result<(ObjectId, Vec<u8>)> {
        anyhow::ensure!(
            object_id(kind, bytes) == *id,
            "object ID does not match bytes"
        );
        let scheme = self.scheme(kind, id);
        let rid = scheme.remote_id();
        let sealed = scheme
            .crypto()
            .seal_with(&rid, &scheme.nonce(), id, kind, bytes)?;
        Ok((rid, sealed))
    }

    fn open(
        &self,
        kind: ObjectKind,
        id: &ObjectId,
        rid: &ObjectId,
        sealed: &[u8],
    ) -> anyhow::Result<Vec<u8>> {
        let scheme = self.scheme(kind, id);
        anyhow::ensure!(scheme.remote_id() == *rid, "remote id does not name object");
        let (opened, bytes) = scheme
            .crypto()
            .open_with(rid, &scheme.nonce(), id, sealed)?;
        anyhow::ensure!(opened == kind, "object kind mismatch");
        Ok(bytes)
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    use serde_json::{json, Value};
    use std::path::PathBuf;

    const MAGIC: &[u8] = b"AGEO\x01";

    fn keys(cell: u8, dedup: u8) -> Keys {
        Keys {
            cell_key: [cell; 32],
            dedup: [dedup; 32],
        }
    }

    fn sealer(cell: u8, dedup: u8) -> CellSealer {
        CellSealer::new(&keys(cell, dedup))
    }

    fn sealed(s: &CellSealer, kind: ObjectKind, bytes: &[u8]) -> (ObjectId, ObjectId, Vec<u8>) {
        let id = object_id(kind, bytes);
        let (rid, blob) = s.seal(kind, &id, bytes).unwrap();
        (id, rid, blob)
    }

    #[test]
    fn same_chunk_same_identity_is_one_rid() {
        let s = sealer(1, 2);
        let (_, first, blob) = sealed(&s, ObjectKind::Chunk, b"chunk bytes");
        let (_, second, again) = sealed(&sealer(9, 2), ObjectKind::Chunk, b"chunk bytes");
        assert_eq!(first, second);
        assert_eq!(blob, again);
    }

    #[test]
    fn same_chunk_two_identities_cannot_open_each_other() {
        let (mine, theirs) = (sealer(1, 2), sealer(1, 3));
        let (id, rid, blob) = sealed(&mine, ObjectKind::Chunk, b"chunk bytes");
        let (_, other_rid, _) = sealed(&theirs, ObjectKind::Chunk, b"chunk bytes");
        assert_ne!(rid, other_rid);
        assert!(theirs.open(ObjectKind::Chunk, &id, &rid, &blob).is_err());
        assert!(theirs
            .open(ObjectKind::Chunk, &id, &other_rid, &blob)
            .is_err());
    }

    #[test]
    fn every_kind_round_trips_with_the_object_magic() {
        let s = sealer(1, 2);
        for kind in [
            ObjectKind::Chunk,
            ObjectKind::Blob,
            ObjectKind::Tree,
            ObjectKind::Snapshot,
            ObjectKind::Xattrs,
        ] {
            let (id, rid, blob) = sealed(&s, kind, b"payload");
            assert!(blob.starts_with(MAGIC));
            assert_eq!(s.remote_id(kind, &id), rid);
            assert_eq!(s.open(kind, &id, &rid, &blob).unwrap(), b"payload");
        }
    }

    #[test]
    fn structure_objects_match_the_remote_crypto_framing() {
        let s = sealer(1, 2);
        let (id, rid, blob) = sealed(&s, ObjectKind::Tree, b"tree bytes");
        let cell = RemoteCrypto::new([1; 32]);
        assert_eq!(rid, cell.remote_id(&id));
        assert_eq!(
            blob,
            cell.encrypt_object(&id, ObjectKind::Tree, b"tree bytes")
                .unwrap()
        );
    }

    #[test]
    fn wrong_key_fails() {
        let (id, rid, blob) = sealed(&sealer(1, 2), ObjectKind::Tree, b"tree bytes");
        assert!(sealer(4, 2)
            .open(ObjectKind::Tree, &id, &rid, &blob)
            .is_err());
    }

    #[test]
    fn tampered_byte_fails() {
        let s = sealer(1, 2);
        for kind in [ObjectKind::Chunk, ObjectKind::Tree] {
            let (id, rid, mut blob) = sealed(&s, kind, b"payload");
            *blob.last_mut().unwrap() ^= 1;
            assert!(s.open(kind, &id, &rid, &blob).is_err());
        }
    }

    #[test]
    fn swapped_rid_or_id_fails() {
        let s = sealer(1, 2);
        let (id, rid, blob) = sealed(&s, ObjectKind::Chunk, b"one");
        let (other_id, other_rid, _) = sealed(&s, ObjectKind::Chunk, b"two");
        assert!(s.open(ObjectKind::Chunk, &id, &other_rid, &blob).is_err());
        assert!(s.open(ObjectKind::Chunk, &other_id, &rid, &blob).is_err());
    }

    #[test]
    fn wrong_kind_fails() {
        let s = sealer(1, 2);
        let (id, rid, blob) = sealed(&s, ObjectKind::Blob, b"payload");
        assert!(s.open(ObjectKind::Tree, &id, &rid, &blob).is_err());
    }

    #[test]
    fn seal_refuses_bytes_that_do_not_hash_to_the_id() {
        assert!(sealer(1, 2)
            .seal(ObjectKind::Chunk, &[0; 32], b"payload")
            .is_err());
    }

    fn vectors_path() -> PathBuf {
        PathBuf::from(env!("CARGO_MANIFEST_DIR")).join("../testdata/stage1-vectors.json")
    }

    fn case(s: &CellSealer, name: &str, kind: ObjectKind, bytes: &[u8]) -> Value {
        let (id, rid, blob) = sealed(s, kind, bytes);
        json!({
            "kind": name,
            "id": hex::encode(id),
            "plaintext_hex": hex::encode(bytes),
            "rid": hex::encode(rid),
            "sealed_hex": hex::encode(blob),
        })
    }

    fn vector_file() -> Value {
        let k = Keys {
            cell_key: [0x11; 32],
            dedup: [0x22; 32],
        };
        let s = CellSealer::new(&k);
        json!({
            "V": 1,
            "cell_key": hex::encode(k.cell_key),
            "dedup": hex::encode(k.dedup),
            "cases": [
                case(&s, "chunk", ObjectKind::Chunk, b"stage one chunk vector"),
                case(&s, "tree", ObjectKind::Tree, b"stage one tree vector"),
            ],
        })
    }

    #[test]
    #[ignore = "rewrites testdata/stage1-vectors.json"]
    fn regenerate_vectors() {
        let text = serde_json::to_string_pretty(&vector_file()).unwrap();
        std::fs::write(vectors_path(), text + "\n").unwrap();
    }

    fn unhex<const N: usize>(v: &Value) -> [u8; N] {
        hex::decode(v.as_str().unwrap())
            .unwrap()
            .try_into()
            .unwrap()
    }

    #[test]
    fn pinned_vectors_are_reproduced_and_open() {
        let file: Value = serde_json::from_slice(&std::fs::read(vectors_path()).unwrap()).unwrap();
        assert_eq!(file["V"], 1);
        let s = CellSealer::new(&Keys {
            cell_key: unhex(&file["cell_key"]),
            dedup: unhex(&file["dedup"]),
        });
        let kinds = [("chunk", ObjectKind::Chunk), ("tree", ObjectKind::Tree)];
        let cases = file["cases"].as_array().unwrap();
        assert_eq!(cases.len(), kinds.len());
        for (c, (name, kind)) in cases.iter().zip(kinds) {
            assert_eq!(c["kind"], name);
            let (id, rid): (ObjectId, ObjectId) = (unhex(&c["id"]), unhex(&c["rid"]));
            let plain = hex::decode(c["plaintext_hex"].as_str().unwrap()).unwrap();
            let want = hex::decode(c["sealed_hex"].as_str().unwrap()).unwrap();
            assert_eq!(s.seal(kind, &id, &plain).unwrap(), (rid, want.clone()));
            assert_eq!(s.open(kind, &id, &rid, &want).unwrap(), plain);
        }
    }
}
