package main

import (
	"context"
	"github.com/Agent-Field/codeaf/internal/devname"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/directory"
	"github.com/Agent-Field/codeaf/internal/pair"
)

// L4-M10a, L4-M15a through L4-M15d: rows retain this computer, facts and selectable names.
func TestL4DeviceListAndSelectors(t *testing.T) {
	devices := map[string]directory.Device{}
	key := []byte("01234567890123456789012345678901")
	for _, id := range []string{"dev_8381", "dev_9cf9"} {
		sealed, err := directory.SealName(key, "desk-b")
		if err != nil {
			t.Fatal(err)
		}
		devices[id] = directory.Device{Name: sealed, Platform: "linux", LastSeen: time.Date(2026, 10, 30, 12, 0, 0, 0, time.UTC).UnixMilli(), Revoked: id == "dev_9cf9"}
	}
	c := chatsKind{dir: &l4DeviceDirectory{listing: directory.Listing{Devices: devices}}, key: key, self: "dev_8381"}
	now := time.Date(2026, 10, 30, 12, 1, 0, 0, time.UTC)
	table, err := c.table(now)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"desk-b #8381", "desk-b #9cf9", "Linux", "last seen", "this device", "removed"} {
		if !strings.Contains(table, want) {
			t.Fatalf("missing %q: %s", want, table)
		}
	}
	if strings.Index(table, "removed") > strings.Index(table, "desk-b #9cf9") {
		t.Fatal(table)
	}
	for _, name := range []string{"desk-b#8381", "desk-b #8381"} {
		rows, err := c.called(name)
		if err != nil || len(rows) != 1 || rows[0].id != "dev_8381" {
			t.Fatalf("%v %v", rows, err)
		}
	}
	_, err = c.stop("desk-b")
	if err == nil || !strings.Contains(err.Error(), "desk-b #8381, desk-b #9cf9") || !strings.Contains(err.Error(), "codeaf devices revoke desk-b#8381") {
		t.Fatal(err)
	}
	single := pair.ChatsDevicesListAt([]pair.ChatsDevice{{Name: "here", This: true}}, now)
	if strings.Contains(single, "last seen") || strings.Contains(single, "0 ") {
		t.Fatal(single)
	}
}

type l4DeviceDirectory struct {
	directory.Client
	listing directory.Listing
}

func (d *l4DeviceDirectory) List(context.Context) (directory.Listing, error) { return d.listing, nil }

func (d *l4DeviceDirectory) Presence(context.Context) (directory.PresenceView, error) {
	return directory.PresenceView{}, nil
}

// L4-M15a, L4-M15c: one sync permission and one remote permission with the same name both get selectors.
func TestL4CrossKindDeviceSelectors(t *testing.T) {
	t.Setenv("CODEAF_HOME", t.TempDir())
	key := []byte("01234567890123456789012345678901")
	sealed, err := directory.SealName(key, "laptop-a")
	if err != nil {
		t.Fatal(err)
	}
	c := chatsKind{dir: &l4DeviceDirectory{listing: directory.Listing{Devices: map[string]directory.Device{"dev_8381": {Name: sealed}}}}, key: key}
	b := pair.BookAt(filepath.Join(t.TempDir(), "devices.json"))
	remote := pair.Paired{Label: "laptop-a", Key: "AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA"}
	if err := b.Admit(remote); err != nil {
		t.Fatal(err)
	}
	out, err := os.CreateTemp(t.TempDir(), "list")
	if err != nil {
		t.Fatal(err)
	}
	defer out.Close()
	old := os.Stdout
	os.Stdout = out
	defer func() { os.Stdout = old }()
	if err := listDevices([]deviceKind{c, bookKind{b}}, time.Time{}); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(out.Name())
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"laptop-a #8381", devname.DeviceLabel("laptop-a", remote.ID(), true)} {
		if !strings.Contains(string(raw), want) {
			t.Fatalf("missing %s: %s", want, raw)
		}
	}
	_, err = kindOf([]deviceKind{c, bookKind{b}}, "laptop-a")
	if err == nil || !strings.Contains(err.Error(), "laptop-a #8381") || !strings.Contains(err.Error(), "codeaf devices revoke laptop-a#8381") {
		t.Fatal(err)
	}
}
