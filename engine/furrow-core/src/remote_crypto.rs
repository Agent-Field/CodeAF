//! Opaque remote identity and authenticated encryption for self-hosted sync.

use crate::model::{ObjectId, ObjectKind};
use crate::store::object_id;
use anyhow::Context;
use chacha20poly1305::aead::{Aead, KeyInit, Payload};
use chacha20poly1305::{Key, XChaCha20Poly1305, XNonce};
use zeroize::{Zeroize, Zeroizing};

const OBJECT_MAGIC: &[u8; 5] = b"AGEO\x01";
const HEAD_MAGIC: &[u8; 5] = b"AGEH\x01";
const NONCE_LEN: usize = 24;
const MAX_METADATA_LEN: usize = 1024 * 1024;

pub struct RemoteCrypto {
    key: [u8; 32],
}

impl RemoteCrypto {
    pub fn new(key: [u8; 32]) -> Self {
        Self { key }
    }

    pub fn generate_key() -> anyhow::Result<[u8; 32]> {
        let mut key = [0; 32];
        getrandom::getrandom(&mut key)
            .map_err(|error| anyhow::anyhow!("generate sync key: {error}"))?;
        Ok(key)
    }

    pub fn remote_id(&self, id: &ObjectId) -> ObjectId {
        let mut hasher = blake3::Hasher::new_keyed(&self.key);
        hasher.update(b"furrow:remote-id:v1\0");
        hasher.update(id);
        *hasher.finalize().as_bytes()
    }

    pub fn encrypt_object(
        &self,
        id: &ObjectId,
        kind: ObjectKind,
        bytes: &[u8],
    ) -> anyhow::Result<Vec<u8>> {
        anyhow::ensure!(
            object_id(kind, bytes) == *id,
            "object ID does not match bytes"
        );
        let nonce = self.object_nonce(id);
        let remote_id = self.remote_id(id);
        let mut plaintext = Zeroizing::new(Vec::with_capacity(1 + id.len() + bytes.len()));
        plaintext.push(kind as u8);
        plaintext.extend_from_slice(id);
        plaintext.extend_from_slice(bytes);
        let ciphertext = self
            .cipher()
            .encrypt(
                XNonce::from_slice(&nonce),
                Payload {
                    msg: &plaintext,
                    aad: &remote_id,
                },
            )
            .map_err(|_| anyhow::anyhow!("encrypt remote object"))?;
        let mut framed = Vec::with_capacity(OBJECT_MAGIC.len() + NONCE_LEN + ciphertext.len());
        framed.extend_from_slice(OBJECT_MAGIC);
        framed.extend_from_slice(&nonce);
        framed.extend_from_slice(&ciphertext);
        Ok(framed)
    }

    pub fn decrypt_object(
        &self,
        expected_id: &ObjectId,
        framed: &[u8],
    ) -> anyhow::Result<(ObjectKind, Vec<u8>)> {
        anyhow::ensure!(
            framed.len() >= OBJECT_MAGIC.len() + NONCE_LEN + 16,
            "encrypted object is truncated"
        );
        anyhow::ensure!(
            &framed[..OBJECT_MAGIC.len()] == OBJECT_MAGIC,
            "invalid encrypted object header"
        );
        let nonce = &framed[OBJECT_MAGIC.len()..OBJECT_MAGIC.len() + NONCE_LEN];
        anyhow::ensure!(
            nonce == self.object_nonce(expected_id),
            "object nonce mismatch"
        );
        let remote_id = self.remote_id(expected_id);
        let plaintext = Zeroizing::new(
            self.cipher()
                .decrypt(
                    XNonce::from_slice(nonce),
                    Payload {
                        msg: &framed[OBJECT_MAGIC.len() + NONCE_LEN..],
                        aad: &remote_id,
                    },
                )
                .map_err(|_| anyhow::anyhow!("remote object authentication failed"))?,
        );
        anyhow::ensure!(plaintext.len() >= 33, "remote object payload is truncated");
        let kind = ObjectKind::from_u8(plaintext[0]).context("invalid remote object kind")?;
        anyhow::ensure!(
            &plaintext[1..33] == expected_id,
            "remote object ID mismatch"
        );
        let bytes = plaintext[33..].to_vec();
        anyhow::ensure!(
            object_id(kind, &bytes) == *expected_id,
            "remote object content failed verification"
        );
        Ok((kind, bytes))
    }

    pub fn encrypt_head(&self, snapshot: &ObjectId, context: &[u8]) -> anyhow::Result<Vec<u8>> {
        self.encrypt_metadata(snapshot, context)
    }

    pub fn encrypt_metadata(&self, plaintext: &[u8], context: &[u8]) -> anyhow::Result<Vec<u8>> {
        anyhow::ensure!(
            plaintext.len() <= MAX_METADATA_LEN,
            "sync metadata exceeds the 1 MiB limit"
        );
        let mut nonce = [0; NONCE_LEN];
        getrandom::getrandom(&mut nonce)
            .map_err(|error| anyhow::anyhow!("generate sync-head nonce: {error}"))?;
        let ciphertext = self
            .cipher()
            .encrypt(
                XNonce::from_slice(&nonce),
                Payload {
                    msg: plaintext,
                    aad: context,
                },
            )
            .map_err(|_| anyhow::anyhow!("encrypt sync head"))?;
        let mut framed = Vec::with_capacity(HEAD_MAGIC.len() + NONCE_LEN + ciphertext.len());
        framed.extend_from_slice(HEAD_MAGIC);
        framed.extend_from_slice(&nonce);
        framed.extend_from_slice(&ciphertext);
        Ok(framed)
    }

    pub fn decrypt_head(&self, framed: &[u8], context: &[u8]) -> anyhow::Result<ObjectId> {
        let plaintext = self.decrypt_metadata(framed, context)?;
        anyhow::ensure!(plaintext.len() == 32, "invalid sync-head snapshot ID");
        let mut snapshot = [0; 32];
        snapshot.copy_from_slice(&plaintext);
        Ok(snapshot)
    }

    pub fn decrypt_metadata(&self, framed: &[u8], context: &[u8]) -> anyhow::Result<Vec<u8>> {
        anyhow::ensure!(
            framed.len() >= HEAD_MAGIC.len() + NONCE_LEN + 16,
            "encrypted sync head is truncated"
        );
        anyhow::ensure!(
            &framed[..HEAD_MAGIC.len()] == HEAD_MAGIC,
            "invalid encrypted sync-head header"
        );
        let nonce = &framed[HEAD_MAGIC.len()..HEAD_MAGIC.len() + NONCE_LEN];
        anyhow::ensure!(
            framed.len() <= HEAD_MAGIC.len() + NONCE_LEN + 16 + MAX_METADATA_LEN,
            "encrypted sync metadata exceeds the 1 MiB limit"
        );
        let plaintext = Zeroizing::new(
            self.cipher()
                .decrypt(
                    XNonce::from_slice(nonce),
                    Payload {
                        msg: &framed[HEAD_MAGIC.len() + NONCE_LEN..],
                        aad: context,
                    },
                )
                .map_err(|_| anyhow::anyhow!("sync-head authentication failed"))?,
        );
        Ok(plaintext.to_vec())
    }

    fn cipher(&self) -> XChaCha20Poly1305 {
        XChaCha20Poly1305::new(Key::from_slice(&self.key))
    }

    fn object_nonce(&self, id: &ObjectId) -> [u8; NONCE_LEN] {
        let mut hasher = blake3::Hasher::new_keyed(&self.key);
        hasher.update(b"furrow:object-nonce:v1\0");
        hasher.update(id);
        let mut nonce = [0; NONCE_LEN];
        hasher.finalize_xof().fill(&mut nonce);
        nonce
    }
}

impl Drop for RemoteCrypto {
    fn drop(&mut self) {
        self.key.zeroize();
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn object_round_trip_is_opaque_deterministic_and_authenticated() {
        let crypto = RemoteCrypto::new([7; 32]);
        let bytes = b"private workspace bytes";
        let id = object_id(ObjectKind::Chunk, bytes);
        let encrypted = crypto
            .encrypt_object(&id, ObjectKind::Chunk, bytes)
            .unwrap();
        assert!(!encrypted.windows(bytes.len()).any(|window| window == bytes));
        assert_eq!(
            encrypted,
            crypto
                .encrypt_object(&id, ObjectKind::Chunk, bytes)
                .unwrap()
        );
        let (kind, decrypted) = crypto.decrypt_object(&id, &encrypted).unwrap();
        assert_eq!(kind, ObjectKind::Chunk);
        assert_eq!(decrypted, bytes);

        let mut damaged = encrypted;
        *damaged.last_mut().unwrap() ^= 1;
        assert!(crypto.decrypt_object(&id, &damaged).is_err());
        assert!(RemoteCrypto::new([8; 32])
            .decrypt_object(&id, &damaged)
            .is_err());
    }

    #[test]
    fn head_round_trip_uses_random_authenticated_nonces() {
        let crypto = RemoteCrypto::new([9; 32]);
        let snapshot = [3; 32];
        let first = crypto.encrypt_head(&snapshot, b"project").unwrap();
        let second = crypto.encrypt_head(&snapshot, b"project").unwrap();
        assert_ne!(first, second);
        assert_eq!(crypto.decrypt_head(&first, b"project").unwrap(), snapshot);
        assert!(crypto.decrypt_head(&first, b"wrong").is_err());
    }
}
