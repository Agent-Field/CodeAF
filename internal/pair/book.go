package pair

// The two books, and they are deliberately two.
//
// THE ENGINE MACHINE'S BOOK IS THE ONE THAT DECIDES. It lists the devices that
// may open a conversation here, and a device that is not in it is refused no
// matter what it believes about itself. REVOCATION IS ALWAYS THIS MACHINE'S
// DECISION — a surface cannot un-pair itself from here, cannot add itself here,
// and cannot ask this machine to forget somebody else. That is the same law the
// rest of aforge already keeps about remote surfaces: trust roots on the machine
// that runs the tools.
//
// THE SURFACE'S BOOK IS A MEMORY, NOT A PERMISSION. It records which key each
// machine name belongs to, so that a later connection can be pinned to the key
// this device actually paired with rather than to whatever answers to a name.
// Deleting it costs a person nothing except having to pair again.

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

// Known is one machine this device has paired with, as the surface remembers it.
type Known struct {
	// Name is what a person types after --at.
	Name string `json:"name"`
	// Key is the machine's long-term public key, base64 raw-url. THIS IS THE
	// PINNED THING: a connection is to this key, and the name is only how the
	// relay finds it.
	Key string `json:"key"`
	// Service is the relay this machine was paired through, so a device that
	// has used two relays reaches each machine through the right one.
	Service string `json:"service"`
	// Since is when the pairing happened.
	Since time.Time `json:"since"`
}

// Paired is one device this machine has let in, as the engine remembers it.
type Paired struct {
	// Label is what the device called itself when it paired — its host name.
	// It is a convenience for the person reading `aforge devices` and is
	// NEVER what a connection is checked against.
	Label string `json:"label"`
	// Key is the device's long-term public key, base64 raw-url. This is what a
	// connection is checked against.
	Key string `json:"key"`
	// Since is when it paired, and Seen is when it last opened a connection.
	Since time.Time `json:"since"`
	Seen  time.Time `json:"seen,omitempty"`
}

// Book is one of the two files, held against the disk.
type Book struct {
	path string

	mu sync.Mutex
}

// MachineBook is what this device remembers about the machines it can reach.
func MachineBook() *Book { return &Book{path: filepath.Join(Dir(), "machines.json")} }

// DeviceBook is what this machine remembers about the devices it lets in.
func DeviceBook() *Book { return &Book{path: filepath.Join(Dir(), "devices.json")} }

// BookAt is either book, put somewhere else — which is what tests want and
// nothing in the product does.
func BookAt(path string) *Book { return &Book{path: path} }

// Machines is every machine this device is paired with, oldest first.
func (b *Book) Machines() ([]Known, error) {
	var known []Known
	if err := b.read(&known); err != nil {
		return nil, err
	}
	sort.Slice(known, func(i, j int) bool { return known[i].Since.Before(known[j].Since) })
	return known, nil
}

// Machine finds one by name.
func (b *Book) Machine(name string) (Known, bool, error) {
	known, err := b.Machines()
	if err != nil {
		return Known{}, false, err
	}
	for _, one := range known {
		if one.Name == name {
			return one, true, nil
		}
	}
	return Known{}, false, nil
}

// Remember writes a machine into this device's book, replacing an older
// pairing with the same name.
//
// A RE-PAIRING REPLACES THE KEY, and that is not a hole: getting here means the
// person read a fresh code off that machine's own screen and the PAKE agreed.
// Refusing to replace would mean a machine that was rebuilt could never be
// reached again from a device that remembered its old key.
func (b *Book) Remember(one Known) error {
	return b.update(func(known []Known) []Known {
		kept := known[:0]
		for _, existing := range known {
			if existing.Name != one.Name {
				kept = append(kept, existing)
			}
		}
		return append(kept, one)
	})
}

// Forget drops a machine from this device's book.
func (b *Book) Forget(name string) (bool, error) {
	found := false
	err := b.update(func(known []Known) []Known {
		kept := known[:0]
		for _, existing := range known {
			if existing.Name == name {
				found = true
				continue
			}
			kept = append(kept, existing)
		}
		return kept
	})
	return found, err
}

// Devices is every device this machine lets in, oldest first.
func (b *Book) Devices() ([]Paired, error) {
	var paired []Paired
	if err := b.read(&paired); err != nil {
		return nil, err
	}
	sort.Slice(paired, func(i, j int) bool { return paired[i].Since.Before(paired[j].Since) })
	return paired, nil
}

// Admit writes a device into this machine's book. A device that pairs twice
// keeps its first Since, because that is when this machine first let it in.
func (b *Book) Admit(one Paired) error {
	return b.updateDevices(func(paired []Paired) []Paired {
		for i, existing := range paired {
			if existing.Key == one.Key {
				paired[i].Label = one.Label
				return paired
			}
		}
		return append(paired, one)
	})
}

// Allows says whether a key may open a conversation on this machine, and is the
// one question the connection path asks of this book.
func (b *Book) Allows(publicKey []byte) (Paired, bool, error) {
	paired, err := b.Devices()
	if err != nil {
		return Paired{}, false, err
	}
	encoded := base64.RawURLEncoding.EncodeToString(publicKey)
	for _, one := range paired {
		if one.Key == encoded {
			return one, true, nil
		}
	}
	return Paired{}, false, nil
}

// Touch records that a device connected. It is best-effort: a connection is not
// refused because a note about it could not be written.
func (b *Book) Touch(publicKey []byte, when time.Time) {
	encoded := base64.RawURLEncoding.EncodeToString(publicKey)
	_ = b.updateDevices(func(paired []Paired) []Paired {
		for i := range paired {
			if paired[i].Key == encoded {
				paired[i].Seen = when
			}
		}
		return paired
	})
}

// Revoke takes a device out of this machine's book. It matches on the label a
// person can see, and refuses an ambiguous one rather than guessing which of
// two laptops called `laptop` was meant.
func (b *Book) Revoke(label string) (Paired, error) {
	paired, err := b.Devices()
	if err != nil {
		return Paired{}, err
	}
	var hits []Paired
	for _, one := range paired {
		if strings.EqualFold(one.Label, label) {
			hits = append(hits, one)
		}
	}
	switch len(hits) {
	case 0:
		return Paired{}, fmt.Errorf("no device called %q is paired with this machine — `aforge devices` lists the ones that are", label)
	case 1:
	default:
		return Paired{}, fmt.Errorf("%d devices are called %q — this build can only stop one by name, so revoke them all with `aforge devices revoke --all %s`", len(hits), label, label)
	}
	gone := hits[0]
	err = b.updateDevices(func(paired []Paired) []Paired {
		kept := paired[:0]
		for _, one := range paired {
			if one.Key != gone.Key {
				kept = append(kept, one)
			}
		}
		return kept
	})
	return gone, err
}

// RevokeAll takes every device with a label out, which is the honest answer to
// two laptops with the same host name.
func (b *Book) RevokeAll(label string) (int, error) {
	count := 0
	err := b.updateDevices(func(paired []Paired) []Paired {
		kept := paired[:0]
		for _, one := range paired {
			if strings.EqualFold(one.Label, label) {
				count++
				continue
			}
			kept = append(kept, one)
		}
		return kept
	})
	if err == nil && count == 0 {
		return 0, fmt.Errorf("no device called %q is paired with this machine — `aforge devices` lists the ones that are", label)
	}
	return count, err
}

// ── the disk ────────────────────────────────────────────────────────────────

func (b *Book) read(into any) error {
	raw, err := os.ReadFile(b.path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	if len(strings.TrimSpace(string(raw))) == 0 {
		return nil
	}
	if err := json.Unmarshal(raw, into); err != nil {
		return fmt.Errorf("%s is not readable any more: %w", b.path, err)
	}
	return nil
}

func (b *Book) write(what any) error {
	if err := os.MkdirAll(filepath.Dir(b.path), 0o700); err != nil {
		return err
	}
	raw, err := json.MarshalIndent(what, "", "  ")
	if err != nil {
		return err
	}
	// The book names keys, and a key that is world-readable is a key somebody
	// can pin against — so the file is the owner's, written through a temporary
	// so a torn write cannot leave half a book behind.
	temporary := b.path + ".new"
	if err := os.WriteFile(temporary, append(raw, '\n'), 0o600); err != nil {
		return err
	}
	return os.Rename(temporary, b.path)
}

func (b *Book) update(change func([]Known) []Known) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	var known []Known
	if err := b.read(&known); err != nil {
		return err
	}
	return b.write(change(known))
}

func (b *Book) updateDevices(change func([]Paired) []Paired) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	var paired []Paired
	if err := b.read(&paired); err != nil {
		return err
	}
	return b.write(change(paired))
}

// since is the plain-words age of a moment, for the list `aforge devices`
// prints. It stops at days, because a pairing older than that is a date and
// nobody counts weeks.
func since(at, now time.Time) string {
	if at.IsZero() {
		return ""
	}
	span := now.Sub(at)
	switch {
	case span < time.Minute:
		return "just now"
	case span < time.Hour:
		return fmt.Sprintf("%dm ago", int(span/time.Minute))
	case span < 24*time.Hour:
		return fmt.Sprintf("%dh ago", int(span/time.Hour))
	case span < 30*24*time.Hour:
		return fmt.Sprintf("%dd ago", int(span/(24*time.Hour)))
	default:
		return at.Format("2 Jan 2006")
	}
}
