package syncsetup

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/Agent-Field/codeaf/internal/cell"
	"github.com/Agent-Field/codeaf/internal/cellstore"
	"github.com/Agent-Field/codeaf/internal/cellsync"
	"github.com/Agent-Field/codeaf/internal/identity"
)

// SPEC-3 item 5: eligibility needs only the first turn, then no per-seal reads.
func TestFixRound5PublicationEligibilityReadsFirstTurnOnce(t *testing.T) {
	h := t.TempDir()
	id, err := identity.Ensure(h)
	if err != nil {
		t.Fatal(err)
	}
	dev, err := identity.Device(h)
	if err != nil {
		t.Fatal(err)
	}
	s := &Sync{Home: h, Identity: id, Device: dev, Ledger: "local"}
	for _, eligible := range []bool{false, true} {
		t.Run(map[bool]string{false: "ineligible", true: "eligible"}[eligible], func(t *testing.T) {
			c, err := cell.Create(h, cell.Options{Class: cell.HostBound})
			if err != nil {
				t.Fatal(err)
			}
			device := "before pairing"
			if eligible {
				device = dev.Cert.Device
			}
			first, _ := json.Marshal(cellstore.Turn{Device: device})
			path := filepath.Join(c.Root, filepath.FromSlash(cellstore.TurnsPath))
			if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
				t.Fatal(err)
			}
			// Malformed trailing bytes must never be read to decide who made the first seal.
			if err := os.WriteFile(path, append(append(first, '\n'), []byte(strings.Repeat("unneeded", 1<<17))...), 0600); err != nil {
				t.Fatal(err)
			}
			d := &Drive{sync: s, cell: c}
			if err := d.expectPublication(); err != nil {
				t.Fatal(err)
			}
			if got := s.publicationRecorded(c); got != eligible {
				t.Fatalf("publication recorded=%v, want %v", got, eligible)
			}
			if err := os.Remove(path); err != nil {
				t.Fatal(err)
			}
			if err := d.expectPublication(); err != nil {
				t.Fatalf("decision reread chain: %v", err)
			}
			allocs := testing.AllocsPerRun(100, func() {
				if err := d.expectPublication(); err != nil {
					panic(err)
				}
			})
			if allocs != 0 {
				t.Fatalf("cached decision allocated %g times", allocs)
			}
			t.Logf("cached decision: %g allocations per seal; fixture tail=%d bytes", allocs, 8<<17)
		})
	}
}

// SPEC-3 item 5 and PERF.md: repeated decisions do no allocation work as history grows.
func TestFixRound5IneligiblePublicationSealWork(t *testing.T) {
	h := t.TempDir()
	s := &Sync{Home: h, Ledger: "local"}
	c, err := cell.Create(h, cell.Options{Class: cell.HostBound})
	if err != nil {
		t.Fatal(err)
	}
	first, _ := json.Marshal(cellstore.Turn{Device: "before pairing"})
	path := filepath.Join(c.Root, filepath.FromSlash(cellstore.TurnsPath))
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, bytes.Repeat(append(first, '\n'), 4096), 0600); err != nil {
		t.Fatal(err)
	}
	d := &Drive{sync: s, cell: c}
	if err := d.expectPublication(); err != nil {
		t.Fatal(err)
	}
	allocs := testing.AllocsPerRun(100, func() {
		if err := d.expectPublication(); err != nil {
			panic(err)
		}
	})
	t.Logf("%s %s/%s: 4096 turns, cached decision allocations=%g", runtime.Version(), runtime.GOOS, runtime.GOARCH, allocs)
	if allocs != 0 {
		t.Fatalf("history added per-seal allocation work: %g", allocs)
	}
}

// SPEC-3 item 9: a failed recovery record is logged, silent in chat, and still queues the turn.
func TestFixRound9PublicationMarkerFailureIsSilent(t *testing.T) {
	h := t.TempDir()
	id, err := identity.Ensure(h)
	if err != nil {
		t.Fatal(err)
	}
	dev, err := identity.Device(h)
	if err != nil {
		t.Fatal(err)
	}
	s := &Sync{Home: h, Identity: id, Device: dev, Ledger: "local"}
	c, err := cell.Create(h, cell.Options{Class: cell.HostBound})
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(c.Root, filepath.FromSlash(cellstore.TurnsPath))
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		t.Fatal(err)
	}
	first, _ := json.Marshal(cellstore.Turn{ID: "first", Device: dev.Cert.Device})
	if err := os.WriteFile(path, append(first, '\n'), 0600); err != nil {
		t.Fatal(err)
	}
	parent := filepath.Dir(s.publicationPath(c))
	if err := os.MkdirAll(filepath.Dir(parent), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(parent, []byte("blocked"), 0600); err != nil {
		t.Fatal(err)
	}
	var notices []string
	batch := &cellsync.Batcher{Driving: &cellsync.Driving{Cell: c}}
	d := &Drive{sync: s, cell: c, spoken: true, batcher: batch, opt: DriveOptions{OnNotice: func(line string) { notices = append(notices, line) }}}
	d.note(cellstore.Sealed{Turn: cellstore.Turn{ID: "first"}})
	if len(notices) != 0 {
		t.Fatalf("marker failure reached chat: %v", notices)
	}
	logs, err := os.ReadFile(filepath.Join(h, "chat.log"))
	if err != nil || !strings.Contains(string(logs), "sync: recording chat publication:") {
		t.Fatalf("marker failure not in chat log: %s %v", logs, err)
	}
	if batch.Pending() != 1 {
		t.Fatal("marker failure stopped ordinary publication")
	}
}
