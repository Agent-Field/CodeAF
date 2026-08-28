package pair

// This machine's own long-term key, and where it is kept.
//
// ONE KEY PER MACHINE, USED FOR THREE THINGS: the name the relay knows this
// machine by is derived from its public half; the Noise handshake that carries
// a conversation is between it and the other end's; and the relay's
// registration challenge is answered with it. Each of the three derives from it
// under its own label, which is what makes one key safe to spend three ways.
//
// A DEVICE IS NOT A PERSON AND NOT AN ACCOUNT. There is no sign-in here, no
// server that knows who anybody is, and nothing to recover if the file is lost —
// a lost key means a device that is no longer paired, which is un-paired from
// the engine machine and paired again. That is a smaller blast radius than an
// account, and it is why there is no account.

import (
	"crypto/ecdh"
	"crypto/rand"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/Agent-Field/aforge-v2/internal/home"
	"github.com/Agent-Field/aforge-v2/internal/relay"
	"github.com/flynn/noise"
)

// Device is this machine's identity on the relay.
type Device struct {
	private *ecdh.PrivateKey
}

// Public is the 32 bytes the other end pins.
func (d Device) Public() []byte { return d.private.PublicKey().Bytes() }

// Name is what this machine would be reachable as, which is a fact about the
// key and not about whether anything is running.
func (d Device) Name() string { return relay.NameFor(d.Public()) }

// Private is the key the relay's registration challenge is answered with.
func (d Device) Private() *ecdh.PrivateKey { return d.private }

// noiseKey is this device as the Noise library wants it.
//
// THE PUBLIC HALF IS SUPPLIED RATHER THAN RECOMPUTED, so that the bytes another
// machine pinned and the bytes this handshake presents are the same bytes by
// construction — two libraries agreeing about X25519 is a thing to assert in a
// test (device_test.go does), never a thing to assume in a handshake.
func (d Device) noiseKey() noise.DHKey {
	return noise.DHKey{Private: d.private.Bytes(), Public: d.Public()}
}

// Keeper is where a device key lives. It is an interface because THIS IS THE
// SEAM THE OS KEYCHAIN GOES BEHIND — on a Mac the key should be a keychain item
// whose release the platform can gate on Touch ID, so that a fingerprint
// unlocks a connection and aforge never sees a biometric.
//
// THAT IS NOT BUILT. The only implementation in this build is [FileKeeper], and
// [OpenKeeper] returns it on every platform. Nothing in this package pretends
// otherwise: [Keeper.Where] is the sentence a person is shown, and today it
// always says the key is a file.
type Keeper interface {
	// Where is the person-facing answer to "where is that key kept", in a
	// person's words rather than a path.
	Where() string
	// Load answers the key, or false when this machine has never made one.
	Load() ([]byte, bool, error)
	// Save writes a newly made key.
	Save(seed []byte) error
	// Forget removes it. The machine loses its name and every pairing that
	// named it, which is why nothing calls this except a person who asked.
	Forget() error
}

// OpenKeeper is where this build keeps a device key.
//
// IT IS THE FILE KEEPER ON EVERY PLATFORM, INCLUDING MACS. See [Keeper].
func OpenKeeper() Keeper {
	return FileKeeper{Path: DeviceKeyPath()}
}

// Dir is where everything this package writes lives, moved wholesale by
// AFORGE_HOME like the rest of aforge's state.
func Dir() string { return home.Join("v3", "remote") }

// DeviceKeyPath is the file the device key is in.
func DeviceKeyPath() string { return filepath.Join(Dir(), "device.key") }

// FileKeeper keeps the key in a file only its owner can read.
type FileKeeper struct{ Path string }

// Where is the sentence a person reads. It says a file because it is a file.
func (f FileKeeper) Where() string {
	return "a file on this machine, readable only by you (" + f.Path + ")"
}

func (f FileKeeper) Load() ([]byte, bool, error) {
	raw, err := os.ReadFile(f.Path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, err
	}
	seed, err := decodeSeed(raw)
	if err != nil {
		return nil, false, fmt.Errorf("%s is not a device key any more — remove it and this machine will make a new one, which un-pairs every device it was paired with", f.Path)
	}
	return seed, true, nil
}

func (f FileKeeper) Save(seed []byte) error {
	if err := os.MkdirAll(filepath.Dir(f.Path), 0o700); err != nil {
		return err
	}
	// 0600 AND WRITTEN THROUGH A TEMPORARY FILE, because a half-written key is
	// a machine that has lost its name, and a key that was ever world-readable
	// is a key that has to be assumed read.
	temporary := f.Path + ".new"
	if err := os.WriteFile(temporary, []byte(encodeSeed(seed)), 0o600); err != nil {
		return err
	}
	return os.Rename(temporary, f.Path)
}

func (f FileKeeper) Forget() error {
	err := os.Remove(f.Path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	return err
}

// ThisDevice is this machine's key, made on first use.
//
// MAKING ONE IS NOT AN EVENT A PERSON IS TOLD ABOUT, because it is not a
// decision they made: the first time anything needs this machine's name, the
// key that name comes from has to exist. What they are told about is the name,
// which is the part that means something.
func ThisDevice(keeper Keeper) (Device, error) {
	seed, found, err := keeper.Load()
	if err != nil {
		return Device{}, err
	}
	if !found {
		fresh, err := ecdh.X25519().GenerateKey(rand.Reader)
		if err != nil {
			return Device{}, err
		}
		if err := keeper.Save(fresh.Bytes()); err != nil {
			return Device{}, err
		}
		return Device{private: fresh}, nil
	}
	private, err := ecdh.X25519().NewPrivateKey(seed)
	if err != nil {
		return Device{}, errors.New("this machine's device key is not usable — remove " + DeviceKeyPath() + " and it will make a new one, which un-pairs every device it was paired with")
	}
	return Device{private: private}, nil
}

// The key is stored as hex on one line rather than as 32 raw bytes, so that a
// person who opens the file sees something that is obviously a key and not
// something that is obviously a mistake.
func encodeSeed(seed []byte) string {
	var text strings.Builder
	text.WriteString("aforge-device-key ")
	for _, b := range seed {
		text.WriteString(hexDigits[b>>4 : b>>4+1])
		text.WriteString(hexDigits[b&15 : b&15+1])
	}
	text.WriteString("\n")
	return text.String()
}

const hexDigits = "0123456789abcdef"

func decodeSeed(raw []byte) ([]byte, error) {
	text := strings.TrimSpace(string(raw))
	text = strings.TrimPrefix(text, "aforge-device-key ")
	text = strings.TrimSpace(text)
	if len(text) != 64 {
		return nil, errors.New("wrong length")
	}
	seed := make([]byte, 32)
	for i := range seed {
		high := strings.IndexByte(hexDigits, text[2*i])
		low := strings.IndexByte(hexDigits, text[2*i+1])
		if high < 0 || low < 0 {
			return nil, errors.New("not hex")
		}
		seed[i] = byte(high<<4 | low)
	}
	return seed, nil
}
