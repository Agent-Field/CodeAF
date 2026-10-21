// Package identity is the one person-level root on a machine.
//
// An identity is three independent 32-byte secrets kept in one file,
// identity.json, mode 0600 in the codeaf home, outside every workspace:
//
//   - a signing seed (ed25519) whose public key names the person to the
//     directory; the public id is a hash of that key;
//   - a dedup secret, the input of the convergent chunk keys;
//   - the cell key, which in Stage 1 is the one key every cell names (the
//     degenerate derivation, ARCHITECTURE section 9).
//
// There is no account. A second machine gets the same identity by importing a
// passphrase-wrapped export (argon2id, XChaCha20-Poly1305); the public id and
// cell_key_id then match on both. Each machine also holds its own device key
// (device.go) that never leaves it; the identity signs a certificate for it. The vault in internal/keys seals under the
// cell key, so there is exactly one root secret. An older machine's vault.key
// becomes the cell key the first time an identity is created there, so its
// vault keeps decrypting.
package identity
