package identity

import (
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
)

const (
	version = 1
	keySize = 32
)

type key [keySize]byte

// Identity is one person's root secrets. The zero value is not an identity.
type Identity struct {
	seed  key
	dedup key
	cell  key
}

// document is the persisted form: every secret as 64 hex characters.
type document struct {
	V           uint16 `json:"V"`
	SigningSeed string `json:"signing_seed"`
	DedupSecret string `json:"dedup_secret"`
	CellKey     string `json:"cell_key"`
}

func (i Identity) document() document {
	return document{V: version, SigningSeed: hex.EncodeToString(i.seed[:]),
		DedupSecret: hex.EncodeToString(i.dedup[:]), CellKey: hex.EncodeToString(i.cell[:])}
}

func fromDocument(d document) (Identity, error) {
	if d.V != version {
		return Identity{}, fmt.Errorf("identity: unsupported version %d", d.V)
	}
	var id Identity
	fields := []struct {
		text string
		into *key
	}{{d.SigningSeed, &id.seed}, {d.DedupSecret, &id.dedup}, {d.CellKey, &id.cell}}
	for _, f := range fields {
		if err := decodeKey(f.text, f.into); err != nil {
			return Identity{}, err
		}
	}
	return id, nil
}

func decodeKey(text string, into *key) error {
	raw, err := hex.DecodeString(text)
	if err != nil || len(raw) != keySize {
		return errors.New("identity: a secret is not 32 bytes of hex")
	}
	copy(into[:], raw)
	return nil
}

func randomKey() (key, error) {
	var k key
	_, err := rand.Read(k[:])
	return k, err
}

// generate makes a fresh identity around the given cell key.
func generate(cell key) (Identity, error) {
	seed, err1 := randomKey()
	dedup, err2 := randomKey()
	return Identity{seed: seed, dedup: dedup, cell: cell}, errors.Join(err1, err2)
}

// PublicKey is the ed25519 key the directory verifies signatures against.
func (i Identity) PublicKey() ed25519.PublicKey {
	return ed25519.NewKeyFromSeed(i.seed[:]).Public().(ed25519.PublicKey)
}

// Sign signs msg with the identity's signing key.
func (i Identity) Sign(msg []byte) []byte {
	return ed25519.Sign(ed25519.NewKeyFromSeed(i.seed[:]), msg)
}

// ID is the public identity id: "id_" and 32 hex characters of the hash of the
// public key.
func (i Identity) ID() string {
	sum := sha256.Sum256(i.PublicKey())
	return "id_" + hex.EncodeToString(sum[:16])
}

// Fingerprint is a short form of the id to compare by eye: four groups of four
// hex characters.
func (i Identity) Fingerprint() string {
	sum := sha256.Sum256(i.PublicKey())
	s := hex.EncodeToString(sum[:8])
	return fmt.Sprintf("%s-%s-%s-%s", s[0:4], s[4:8], s[8:12], s[12:16])
}

// CellKey is the key that seals structure and the vault in Stage 1.
func (i Identity) CellKey() []byte { return append([]byte(nil), i.cell[:]...) }

// DedupSecret is the input of the convergent chunk keys.
func (i Identity) DedupSecret() []byte { return append([]byte(nil), i.dedup[:]...) }

// CellKeyID names a cell key: the first 16 bytes of its SHA-256 as 32 hex
// characters, the shape of meta.cell_key_id.
func CellKeyID(cellKey []byte) string {
	sum := sha256.Sum256(cellKey)
	return hex.EncodeToString(sum[:16])
}

// CellKeyID names this identity's cell key.
func (i Identity) CellKeyID() string { return CellKeyID(i.cell[:]) }
