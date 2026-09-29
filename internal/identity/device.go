package identity

import (
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"time"
)

// DeviceFile holds this machine's device key and its certificate. It is never
// exported: the root identity travels, the device does not.
const DeviceFile = "device.json"

const certLabel = "codeaf/device-cert/v1\n"

// Cert is the identity's signature over one device: its public key and when it
// was made. It carries no name, because a relay reads it in a request header;
// the device's name lives only sealed in the directory record. It is what a relay checks to tell devices of one identity
// apart, and what would let a lost one be dropped.
type Cert struct {
	V      uint16 `json:"V"`
	Device string `json:"device"`  // device public key, hex
	Made   int64  `json:"created"` // unix ms
	Sig    string `json:"sig"`     // hex ed25519 over the other fields
}

// body is what the signature covers: the cert without its signature.
func (c Cert) body() []byte {
	c.Sig = ""
	raw, _ := json.Marshal(c)
	return append([]byte(certLabel), raw...)
}

// Verify checks the cert was signed by the identity owning identityKey.
func (c Cert) Verify(identityKey ed25519.PublicKey) error {
	sig, err := hex.DecodeString(c.Sig)
	if err != nil || !ed25519.Verify(identityKey, c.body(), sig) {
		return errors.New("identity: device certificate does not verify")
	}
	return nil
}

// DeviceID is "dev_" and 32 hex characters of the hash of the device key.
func (c Cert) DeviceID() string {
	pub, _ := hex.DecodeString(c.Device)
	sum := sha256.Sum256(pub)
	return "dev_" + hex.EncodeToString(sum[:16])
}

// Dev is this machine's device: its key and the cert the identity signed.
type Dev struct {
	Cert Cert
	seed key
}

// ID is the device id.
func (d Dev) ID() string { return d.Cert.DeviceID() }

// Sign signs msg with the device key.
func (d Dev) Sign(msg []byte) []byte { return ed25519.Sign(ed25519.NewKeyFromSeed(d.seed[:]), msg) }

// deviceDocument is the persisted form.
type deviceDocument struct {
	V    uint16 `json:"V"`
	Seed string `json:"device_seed"`
	Cert Cert   `json:"cert"`
}

// Device returns this machine's device under its identity, making either on
// first use.
func Device(home string) (Dev, error) {
	id, err := Ensure(home)
	if err != nil {
		return Dev{}, err
	}
	return deviceFor(home, id)
}

// deviceFor returns the stored device when its cert verifies under id, else a
// fresh device. A replaced identity therefore never keeps a stale device.
func deviceFor(home string, id Identity) (Dev, error) {
	if d, err := loadDevice(home); err == nil && d.Cert.Verify(id.PublicKey()) == nil {
		return d, nil
	}
	d, err := newDevice(id)
	if err != nil {
		return Dev{}, err
	}
	return d, write(filepath.Join(home, DeviceFile), d.document(), os.Rename)
}

func newDevice(id Identity) (Dev, error) {
	seed, err := randomKey()
	if err != nil {
		return Dev{}, err
	}
	pub := ed25519.NewKeyFromSeed(seed[:]).Public().(ed25519.PublicKey)
	cert := Cert{V: version, Device: hex.EncodeToString(pub), Made: time.Now().UnixMilli()}
	cert.Sig = hex.EncodeToString(id.Sign(cert.body()))
	return Dev{Cert: cert, seed: seed}, nil
}

func (d Dev) document() deviceDocument {
	return deviceDocument{V: version, Seed: hex.EncodeToString(d.seed[:]), Cert: d.Cert}
}

func loadDevice(home string) (Dev, error) {
	raw, err := os.ReadFile(filepath.Join(home, DeviceFile))
	if err != nil {
		return Dev{}, err
	}
	var doc deviceDocument
	if err := json.Unmarshal(raw, &doc); err != nil {
		return Dev{}, err
	}
	d := Dev{Cert: doc.Cert}
	return d, decodeKey(doc.Seed, &d.seed)
}
