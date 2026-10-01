package identity

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"

	"golang.org/x/crypto/argon2"
	"golang.org/x/crypto/chacha20poly1305"
)

// ErrWrongPassphrase reports an export that did not open. A wrong passphrase
// and a damaged blob look the same by design.
var ErrWrongPassphrase = errors.New("identity: wrong passphrase, or the export is damaged")

// wrapped is the export: the identity document sealed under a key stretched
// from a passphrase. The cost parameters travel with it so they can grow.
type wrapped struct {
	V       uint16 `json:"V"`
	KDF     string `json:"kdf"`
	Time    uint32 `json:"t"`
	MemKiB  uint32 `json:"m_kib"`
	Threads uint8  `json:"p"`
	Salt    string `json:"salt"`
	Nonce   string `json:"nonce"`
	Data    string `json:"data"`
}

const (
	kdfName   = "argon2id"
	wrapLabel = "codeaf/identity-export/v1"
	maxMemKiB = 1 << 20 // refuse an export that asks for more than 1 GiB
	maxTime   = 16
)

// cost is what new exports use; imports honour whatever the blob names, within
// the maxima above.
var cost = wrapped{Time: 3, MemKiB: 64 * 1024, Threads: 4}

func (w wrapped) stretch(passphrase string, salt []byte) []byte {
	return argon2.IDKey([]byte(passphrase), salt, w.Time, w.MemKiB, w.Threads, keySize)
}

func (w wrapped) valid() error {
	if w.V != version || w.KDF != kdfName {
		return errors.New("identity: not an identity export")
	}
	if w.Time == 0 || w.Time > maxTime || w.MemKiB == 0 || w.MemKiB > maxMemKiB || w.Threads == 0 {
		return errors.New("identity: export asks for unreasonable key stretching")
	}
	return nil
}

// Export wraps id under passphrase into a blob that is safe to copy anywhere.
func Export(id Identity, passphrase string) ([]byte, error) {
	plain, err := json.Marshal(id.document())
	if err != nil {
		return nil, err
	}
	w := cost
	w.V, w.KDF = version, kdfName
	salt, nonce := make([]byte, 16), make([]byte, chacha20poly1305.NonceSizeX)
	if _, err := rand.Read(salt); err != nil {
		return nil, err
	}
	if _, err := rand.Read(nonce); err != nil {
		return nil, err
	}
	aead, err := chacha20poly1305.NewX(w.stretch(passphrase, salt))
	if err != nil {
		return nil, err
	}
	w.Salt, w.Nonce = hex.EncodeToString(salt), hex.EncodeToString(nonce)
	w.Data = hex.EncodeToString(aead.Seal(nil, nonce, plain, []byte(wrapLabel)))
	return json.Marshal(w)
}

// Import opens an export made by Export.
func Import(blob []byte, passphrase string) (Identity, error) {
	var w wrapped
	if err := json.Unmarshal(blob, &w); err != nil {
		return Identity{}, fmt.Errorf("identity: not an identity export: %w", err)
	}
	if err := w.valid(); err != nil {
		return Identity{}, err
	}
	plain, err := w.open(passphrase)
	if err != nil {
		return Identity{}, err
	}
	var d document
	if err := json.Unmarshal(plain, &d); err != nil {
		return Identity{}, ErrWrongPassphrase
	}
	return fromDocument(d)
}

func (w wrapped) open(passphrase string) ([]byte, error) {
	salt, err1 := hex.DecodeString(w.Salt)
	nonce, err2 := hex.DecodeString(w.Nonce)
	data, err3 := hex.DecodeString(w.Data)
	if err := errors.Join(err1, err2, err3); err != nil || len(nonce) != chacha20poly1305.NonceSizeX {
		return nil, errors.New("identity: malformed export")
	}
	aead, err := chacha20poly1305.NewX(w.stretch(passphrase, salt))
	if err != nil {
		return nil, err
	}
	plain, err := aead.Open(nil, nonce, data, []byte(wrapLabel))
	if err != nil {
		return nil, ErrWrongPassphrase
	}
	return plain, nil
}

// ErrDifferent reports that the home already holds another identity.
var ErrDifferent = errors.New("identity: this machine already has a different identity; pass --replace to overwrite it")

// Adopt makes next the identity under home. It refuses to overwrite a
// different identity unless replace is set, and says whether it did.
func Adopt(home string, next Identity, replace bool) (replaced bool, err error) {
	cur, err := Load(home)
	if errors.Is(err, ErrNone) && hasLegacyKey(home) {
		cur, err = ensureRoot(home) // an unmigrated vault key is an identity too
	}
	if err != nil && !errors.Is(err, ErrNone) {
		return false, err
	}
	replaced = err == nil && cur.ID() != next.ID()
	if replaced && !replace {
		return false, ErrDifferent
	}
	if err := Save(home, next); err != nil {
		return false, err
	}
	if _, err := deviceFor(home, next); err != nil { // this machine's own device under the imported root
		return replaced, err
	}
	return replaced, EndSolo(home) // taking another's identity is joining a fleet
}
