package directory_test

import (
	"context"
	"database/sql"
	"errors"
	"io"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/directory"
	"github.com/Agent-Field/codeaf/internal/directory/directorytest"
)

const (
	tCell = "01J0000000000000000000000B"
	tA    = "dev_aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	tB    = "dev_bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
	tHead = "1111111111111111111111111111111111111111111111111111111111111111"
)

var bg = context.Background()

func openAt(t *testing.T, path string, clock *directorytest.FakeClock) *directory.SQLite {
	t.Helper()
	d, err := directory.OpenSQLite(path, clock.Now)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { d.Close() })
	return d
}

func TestSQLiteConformance(t *testing.T) {
	directorytest.Run(t, func(t *testing.T, clock *directorytest.FakeClock) directorytest.Devices {
		return openAt(t, filepath.Join(t.TempDir(), "dir.db"), clock).For
	})
}

// A relay restart keeps the lease: the holder's heartbeat with the same fence
// succeeds, and once the lease has expired another device may take it.
func TestSQLiteLeaseSurvivesReopen(t *testing.T) {
	clock, path := directorytest.NewFakeClock(), filepath.Join(t.TempDir(), "dir.db")
	first := openAt(t, path, clock)
	if _, err := first.For(tA).Create(bg, tCell, directory.CellInit{Head: tHead, Class: "chat"}); err != nil {
		t.Fatal(err)
	}
	first.Close()

	second := openAt(t, path, clock)
	if _, err := second.For(tB).Acquire(bg, tCell, directory.AcquireOpts{}); !errors.Is(err, directory.ErrLeaseHeld) {
		t.Fatalf("live lease lost across reopen: %v", err)
	}
	v, err := second.For(tA).Heartbeat(bg, tCell, directory.Beat{Fence: 1})
	if err != nil || v.Cell.Lease.Fence != 1 {
		t.Fatalf("heartbeat after reopen: %v %+v", err, v)
	}
	clock.Advance(directory.LeaseTTL + time.Second)
	if v, err = second.For(tB).Acquire(bg, tCell, directory.AcquireOpts{}); err != nil || v.Cell.Lease.Fence != 2 {
		t.Fatalf("expired lease not takeable: %v %+v", err, v)
	}
}

// Two connections to one file model two relay processes: exactly one acquire
// wins and the rest see the lease held.
func TestSQLiteConcurrentAcquireTwoConnections(t *testing.T) {
	clock, path := directorytest.NewFakeClock(), filepath.Join(t.TempDir(), "dir.db")
	one, two := openAt(t, path, clock), openAt(t, path, clock)
	if _, err := one.For(tA).Create(bg, tCell, directory.CellInit{Head: tHead, Class: "chat"}); err != nil {
		t.Fatal(err)
	}
	clock.Advance(directory.LeaseTTL + time.Second)

	var wins, held int
	var mu sync.Mutex
	var wg sync.WaitGroup
	for i := 0; i < 40; i++ {
		d, dev := one, tA
		if i%2 == 1 {
			d, dev = two, tB
		}
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := d.For(dev).Acquire(bg, tCell, directory.AcquireOpts{})
			mu.Lock()
			defer mu.Unlock()
			switch {
			case err == nil:
				wins++
			case errors.Is(err, directory.ErrLeaseHeld):
				held++
			default:
				t.Errorf("unexpected: %v", err)
			}
		}()
	}
	wg.Wait()
	// The same device may re-take its own live lease, so wins are per device: the
	// losing device must never have won once the other held.
	v, _ := one.For(tA).Cell(bg, tCell)
	if wins == 0 || wins+held != 40 {
		t.Fatalf("wins=%d held=%d", wins, held)
	}
	other := tB
	if v.Cell.Lease.Device == tB {
		other = tA
	}
	if _, err := one.For(other).Acquire(bg, tCell, directory.AcquireOpts{}); !errors.Is(err, directory.ErrLeaseHeld) {
		t.Fatalf("loser can acquire: %v", err)
	}
}

// A transaction that dies before commit leaves the old record in place.
func TestSQLiteCrashMidTransactionKeepsOldRecord(t *testing.T) {
	clock, path := directorytest.NewFakeClock(), filepath.Join(t.TempDir(), "dir.db")
	d := openAt(t, path, clock)
	if _, err := d.For(tA).Create(bg, tCell, directory.CellInit{Head: tHead, Class: "chat"}); err != nil {
		t.Fatal(err)
	}
	d.Close()

	raw, err := sql.Open("sqlite", "file:"+path)
	if err != nil {
		t.Fatal(err)
	}
	defer raw.Close()
	conn, err := raw.Conn(bg)
	if err != nil {
		t.Fatal(err)
	}
	for _, q := range []string{`BEGIN IMMEDIATE`, `UPDATE cells SET rec = '{"corrupt":true}'`} {
		if _, err := conn.ExecContext(bg, q); err != nil {
			t.Fatal(err)
		}
	}
	// Closing the driver connection with the transaction open is what a dying
	// process does: nothing was committed.
	if err := conn.Raw(func(dc any) error { return dc.(io.Closer).Close() }); err != nil {
		t.Fatal(err)
	}

	v, err := openAt(t, path, clock).For(tA).Cell(bg, tCell)
	if err != nil || v.Cell.Head != tHead || v.Cell.Lease.Fence != 1 {
		t.Fatalf("old record lost: %v %+v", err, v)
	}
}

// Devices and the vault pointer survive a reopen too.
func TestSQLiteDevicesAndVaultSurviveReopen(t *testing.T) {
	clock, path := directorytest.NewFakeClock(), filepath.Join(t.TempDir(), "dir.db")
	d := openAt(t, path, clock)
	must := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}
	must(d.For(tA).PutDevice(bg, tA, directory.Device{V: 1, Name: "n"}))
	must(d.For(tA).SetVault(bg, "", "rid1"))
	d.Close()

	l, err := openAt(t, path, clock).For(tA).List(bg)
	must(err)
	if l.Identity.Vault != "rid1" || l.Devices[tA].Name != "n" {
		t.Fatalf("lost on reopen: %+v", l)
	}
}
