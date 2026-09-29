// Package keys is the minimal local secret store.
//
// What protects what: secrets live in one file, vault.enc, sealed with
// ChaCha20-Poly1305 under a random 32-byte key kept in vault.key. Both files
// are mode 0600 in the codeaf home, outside every workspace. The key file
// guards against the vault file being copied or committed on its own; anyone
// who can read both files as this user can read the secrets. The plaintext
// envelope carries only the version, key id and nonce.
package keys
