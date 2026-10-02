package syncsetup

import (
	"context"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/chatlist"
	"github.com/Agent-Field/codeaf/internal/directory"
)

// A RENAME ON ONE HOME SHOWS ON THE OTHER. Two devices of one identity share
// nothing but the relay; A names itself and B, which does nothing, reads the new
// name on its device list (what /devices draws) and on the home row that names
// the machine a chat is running on.
func TestRenameOnOneHomeShowsOnTheOther(t *testing.T) {
	r := newDriveRig(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err := r.a.Rename(ctx, nameA); err != nil {
		t.Fatal(err)
	}
	if err := r.b.Rename(ctx, nameB); err != nil {
		t.Fatal(err)
	}
	if got := rosterNames(t, ctx, r.b)[r.a.Device.ID()]; got != nameA {
		t.Fatalf("B sees A as %q before the rename, want %q", got, nameA)
	}

	if err := r.a.Rename(ctx, "the studio desk"); err != nil {
		t.Fatal(err)
	}

	names := rosterNames(t, ctx, r.b)
	if names[r.a.Device.ID()] != "the studio desk" || names[r.b.Device.ID()] != nameB {
		t.Fatalf("B's device list = %v, want A renamed and B itself unchanged", names)
	}
	list, err := r.b.Dir.List(ctx)
	if err != nil {
		t.Fatal(err)
	}
	open := func(sealed string) (string, error) {
		return directory.OpenName(directory.MetadataKey(r.b.Identity.CellKey()), sealed)
	}
	if got := chatlist.DeviceName(list.Devices, r.a.Device.ID(), open); got != "the studio desk" {
		t.Fatalf("the home row names A %q, want the new name", got)
	}
}

// rosterNames is the device list as one Sync reads it, by device id.
func rosterNames(t *testing.T, ctx context.Context, s *Sync) map[string]string {
	t.Helper()
	list, err := s.Dir.List(ctx)
	if err != nil {
		t.Fatal(err)
	}
	key := directory.MetadataKey(s.Identity.CellKey())
	names := map[string]string{}
	for id, d := range list.Devices {
		names[id], _ = directory.OpenName(key, d.Name)
	}
	return names
}
