// Package keys is the minimal local secret store.
//
// What protects what: secrets live in one file, vault.enc, sealed with
// ChaCha20-Poly1305 under the identity's cell key (internal/identity, kept in
// identity.json; a machine's older vault.key becomes that key on first use).
// Both files are mode 0600 in the codeaf home, outside every workspace. The
// identity file guards against the vault file being copied or committed on its
// own; anyone who can read both files as this user can read the secrets. The
// plaintext envelope carries only the version, cell_key_id and nonce.
package keys
