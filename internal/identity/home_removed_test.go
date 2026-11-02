package identity

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	statehome "github.com/Agent-Field/codeaf/internal/home"
)

// C4.1: every identity and device writer refuses a removed or replaced root.
func TestIdentityRefusesRemovedHome(t *testing.T) {
	for _, replaced := range []bool{false, true} {
		t.Run(map[bool]string{false: "missing", true: "replaced"}[replaced], func(t *testing.T) {
			h := filepath.Join(t.TempDir(), "home")
			id, err := EnsureSolo(h)
			if err != nil {
				t.Fatal(err)
			}
			if err := os.Rename(h, h+".old"); err != nil {
				t.Fatal(err)
			}
			if replaced {
				if err := os.Mkdir(h, 0o700); err != nil {
					t.Fatal(err)
				}
			}
			writers := map[string]func() error{
				"solo":   func() error { _, err := EnsureSolo(h); return err },
				"ensure": func() error { _, err := Ensure(h); return err },
				"root":   func() error { _, err := ensureRoot(h); return err },
				"save":   func() error { return Save(h, id) },
				"device": func() error { _, err := deviceFor(h, id); return err },
			}
			for name, write := range writers {
				if err := write(); !errors.Is(err, ErrHomeRemoved) {
					t.Errorf("%s: %v", name, err)
				}
			}
			if replaced {
				entries, err := os.ReadDir(h)
				if err != nil || len(entries) != 0 {
					t.Fatalf("replacement changed: %v, %v", entries, err)
				}
			} else if _, err := os.Stat(h); !os.IsNotExist(err) {
				t.Fatalf("home recreated: %v", err)
			}
		})
	}
}

// C4.2: first use still creates keys, and witnessing an intact home permits loads and saves.
func TestIdentityFirstUseAndIntactWitness(t *testing.T) {
	h := filepath.Join(t.TempDir(), "home")
	statehome.Witness(h)
	id, err := EnsureSolo(h)
	if err != nil {
		t.Fatal(err)
	}
	if !Solo(h) {
		t.Fatal("first use lost its solo marker")
	}
	statehome.Witness(h)
	loaded, err := EnsureSolo(h)
	if err != nil || loaded.ID() != id.ID() {
		t.Fatalf("identity changed: %v", err)
	}
	dev, err := Device(h)
	if err != nil {
		t.Fatal(err)
	}
	if err := Save(h, id); err != nil {
		t.Fatal(err)
	}
	again, err := Device(h)
	if err != nil || again.ID() != dev.ID() {
		t.Fatalf("device changed: %v", err)
	}
	if loaded, err := Load(h); err != nil || loaded.ID() != id.ID() {
		t.Fatalf("saved identity: %v", err)
	}
}

// SPEC-3 item 1: removal after the writer's check is still a removed-home error.
func TestFixRound1IdentityWriteMapsConcurrentRemoval(t *testing.T) {
	for _, replaced := range []bool{false, true} {
		t.Run(map[bool]string{false: "missing", true: "replaced"}[replaced], func(t *testing.T) {
			h := filepath.Join(t.TempDir(), "home")
			if _, err := Ensure(h); err != nil {
				t.Fatal(err)
			}
			err := write(filepath.Join(h, DeviceFile), removeHomeDuringMarshal{home: h, replaced: replaced}, os.Rename)
			if !errors.Is(err, ErrHomeRemoved) {
				t.Fatalf("concurrent removal = %v", err)
			}
			if replaced {
				entries, err := os.ReadDir(h)
				if err != nil || len(entries) != 0 {
					t.Fatalf("replacement changed: %v %v", entries, err)
				}
			} else if _, err := os.Stat(h); !os.IsNotExist(err) {
				t.Fatalf("home recreated: %v", err)
			}
		})
	}
}

type removeHomeDuringMarshal struct {
	home     string
	replaced bool
}

func (v removeHomeDuringMarshal) MarshalJSON() ([]byte, error) {
	if err := os.Rename(v.home, v.home+".old"); err != nil {
		return nil, err
	}
	if v.replaced {
		if err := os.Mkdir(v.home, 0700); err != nil {
			return nil, err
		}
	}
	return []byte(`{}`), nil
}

// SPEC-3 item 1: removal at publication maps the filesystem failure to the home error.
func TestFixRound1IdentityWriteMapsRemovalDuringPublish(t *testing.T) {
	for _, replaced := range []bool{false, true} {
		t.Run(map[bool]string{false: "missing", true: "replaced"}[replaced], func(t *testing.T) {
			h := filepath.Join(t.TempDir(), "home")
			if _, err := Ensure(h); err != nil {
				t.Fatal(err)
			}
			err := write(filepath.Join(h, DeviceFile), struct{}{}, func(from, to string) error {
				if err := os.Rename(h, h+".old"); err != nil {
					return err
				}
				if replaced {
					if err := os.Mkdir(h, 0700); err != nil {
						return err
					}
				}
				return os.Rename(from, to)
			})
			if !errors.Is(err, ErrHomeRemoved) {
				t.Fatalf("publication after removal = %v", err)
			}
			if replaced {
				entries, err := os.ReadDir(h)
				if err != nil || len(entries) != 0 {
					t.Fatalf("replacement changed: %v %v", entries, err)
				}
			} else if _, err := os.Stat(h); !os.IsNotExist(err) {
				t.Fatalf("home recreated: %v", err)
			}
		})
	}
}
