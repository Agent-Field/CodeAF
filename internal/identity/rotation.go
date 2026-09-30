package identity

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
)

// Mint makes an identity whose three secrets are all new: a new signing seed
// (so a new id), a new dedup secret and a new cell key. It never reads the
// home, so a legacy vault key cannot leak into it as ensureRoot's does. It is
// what a rotation replaces an identity with.
func Mint() (Identity, error) {
	cell, err := randomKey()
	if err != nil {
		return Identity{}, err
	}
	return generate(cell)
}

// NewDevice makes a device under id without writing it, for a caller that must
// register the device before it becomes this machine's own.
func NewDevice(id Identity) (Dev, error) { return newDevice(id) }

// Install makes id the identity under home and dev its device. Both writes are
// atomic and repeating the call changes nothing, so a caller that crashed
// between them calls it again.
func Install(home string, id Identity, dev Dev) error {
	if dev.Cert.Verify(id.PublicKey()) != nil {
		return errors.New("identity: the device was not made under this identity")
	}
	if err := Save(home, id); err != nil {
		return err
	}
	return write(filepath.Join(home, DeviceFile), dev.document(), os.Rename)
}

// Marshal is the device as one document in memory, for a journal that must
// keep it across a crash. It holds the device's private key.
func (d Dev) Marshal() ([]byte, error) { return json.Marshal(d.document()) }

// UnmarshalDev reads a document Dev.Marshal made.
func UnmarshalDev(raw []byte) (Dev, error) {
	var doc deviceDocument
	if err := json.Unmarshal(raw, &doc); err != nil {
		return Dev{}, err
	}
	d := Dev{Cert: doc.Cert}
	return d, decodeKey(doc.Seed, &d.seed)
}
