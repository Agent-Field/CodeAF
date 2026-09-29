package keys

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"

	"golang.org/x/crypto/chacha20poly1305"

	"github.com/Agent-Field/codeaf/internal/identity"
)

// envelope is the plaintext wrapper: readers pick the key by CellKeyID
// (32 hex = 16 bytes, SCHEMAS.md). Stage 0 has one key per identity, so the id
// names that key and is derived from it; Stage 5 names a fresh key per cell in
// the same field. KeyID is the pre-freeze spelling (16 hex), read once and
// never written.
type envelope struct {
	V         uint16 `json:"V"`
	CellKeyID string `json:"cell_key_id"`
	Nonce     string `json:"nonce"`
	Data      string `json:"data"`
	KeyID     string `json:"key_id,omitempty"`
}

// idOf is the key id an envelope names, in either spelling.
func (e envelope) idOf() string {
	if e.CellKeyID != "" {
		return e.CellKeyID
	}
	return e.KeyID
}

// namesKey reports whether id is the current id of key or its legacy form,
// the first 8 bytes of the same hash.
func namesKey(id string, key []byte) bool {
	cur := identity.CellKeyID(key)
	return id == cur || id == cur[:16]
}

func seal(key, plain []byte) ([]byte, error) {
	aead, err := chacha20poly1305.New(key)
	if err != nil {
		return nil, err
	}
	nonce := make([]byte, aead.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return nil, err
	}
	env := envelope{V: 1, CellKeyID: identity.CellKeyID(key), Nonce: hex.EncodeToString(nonce),
		Data: hex.EncodeToString(aead.Seal(nil, nonce, plain, []byte(identity.CellKeyID(key))))}
	return json.Marshal(env)
}

func open(key, blob []byte) ([]byte, error) {
	var env envelope
	if err := json.Unmarshal(blob, &env); err != nil {
		return nil, err
	}
	if !namesKey(env.idOf(), key) {
		return nil, errors.New("keys: vault was sealed under a different key")
	}
	aead, err := chacha20poly1305.New(key)
	if err != nil {
		return nil, err
	}
	nonce, err1 := hex.DecodeString(env.Nonce)
	data, err2 := hex.DecodeString(env.Data)
	if err := errors.Join(err1, err2); err != nil || len(nonce) != aead.NonceSize() {
		return nil, errors.New("keys: malformed vault envelope")
	}
	return aead.Open(nil, nonce, data, []byte(env.idOf()))
}

// writeAtomic writes data at mode 0600 via a temp file and rename.
func writeAtomic(path string, data []byte) error {
	tmp, err := os.CreateTemp(filepath.Dir(path), ".tmp-*")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	if err := tmp.Chmod(0o600); err != nil {
		tmp.Close()
		return err
	}
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), path)
}
