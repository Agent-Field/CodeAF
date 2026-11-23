package syncsetup

import (
	"context"
	"runtime"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/directory"
	"github.com/Agent-Field/codeaf/internal/identity"
)

type countedDeviceDirectory struct {
	directory.Client
	puts int
}

func (d *countedDeviceDirectory) PutDevice(ctx context.Context, id string, rec directory.Device) error {
	d.puts++
	return d.Client.PutDevice(ctx, id, rec)
}

func TestA1PutOwnDeviceDoesNotRewriteUnchangedRecord(t *testing.T) {
	home := t.TempDir()
	id, err := identity.Ensure(home)
	mustNil(t, err)
	dev, err := identity.Device(home)
	mustNil(t, err)
	dir := &countedDeviceDirectory{Client: directory.NewMemory(time.Now).For(dev.ID())}
	for _, name := range []string{"desk", "desk", "renamed", "renamed"} {
		mustNil(t, PutOwnDevice(t.Context(), dir, id, dev, name))
	}
	if dir.puts != 2 {
		t.Fatalf("A1: %d PUTs, want one per real name change", dir.puts)
	}
}

// A device's record names the system it runs on, so the device list can say so.
func TestDeviceRecordCarriesThePlatform(t *testing.T) {
	id, err := identity.Mint()
	if err != nil {
		t.Fatal(err)
	}
	rec, err := deviceRecord(id, "tower")
	if err != nil {
		t.Fatal(err)
	}
	if rec.Platform != runtime.GOOS {
		t.Fatalf("platform = %q, want %q", rec.Platform, runtime.GOOS)
	}
}

// PutOwnDevice is the one record-writing path every chat start and every
// finished pairing shares: the name goes up sealed and the id it lands under
// is this device's own.
func TestPutOwnDeviceUpsertsThisDevicesOwnRecord(t *testing.T) {
	home := t.TempDir()
	id, err := identity.Ensure(home)
	if err != nil {
		t.Fatal(err)
	}
	dev, err := identity.Device(home)
	if err != nil {
		t.Fatal(err)
	}
	mem := directory.NewMemory(time.Now)
	dir := mem.For(dev.ID())
	ctx := context.Background()
	for _, name := range []string{"desk", "renamed"} {
		if err := PutOwnDevice(ctx, dir, id, dev, name); err != nil {
			t.Fatal(err)
		}
	}
	l, err := dir.List(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(l.Devices) != 1 {
		t.Fatalf("the directory holds %d devices, want this one alone", len(l.Devices))
	}
	rec := l.Devices[dev.ID()]
	opened, err := directory.OpenName(directory.MetadataKey(id.CellKey()), rec.Name)
	if err != nil {
		t.Fatal(err)
	}
	if opened != "renamed" {
		t.Fatalf("the record names %q, want the upserted name", opened)
	}
	if rec.AddedBy != id.ID() || rec.Platform != runtime.GOOS {
		t.Fatalf("the record is %+v", rec)
	}
}
