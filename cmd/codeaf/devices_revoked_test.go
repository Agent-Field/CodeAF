package main

import (
	"context"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/directory"
)

// A device stopped from another computer is a "revoked" row in the device list the next time it is
// read, on every computer, with no way to take it for an ordinary one.
func TestRevokedDeviceReadsAsRevokedOnEveryComputer(t *testing.T) {
	m := directory.NewMemory(time.Now)
	ctx := context.Background()
	key := []byte("0123456789abcdef0123456789abcdef")
	for _, id := range []string{"dev_a", "dev_b", "dev_c"} {
		sealed, _ := directory.SealName(directory.MetadataKey(key), id)
		if err := m.For(id).PutDevice(ctx, id, directory.Device{V: 1, Name: sealed, Platform: "darwin"}); err != nil {
			t.Fatal(err)
		}
	}
	if err := m.For("dev_a").Revoke(ctx, "dev_b"); err != nil {
		t.Fatal(err)
	}
	for _, viewer := range []string{"dev_a", "dev_c"} {
		l, err := m.For(viewer).List(ctx)
		if err != nil {
			t.Fatal(err)
		}
		for _, row := range rowsOf(l, directory.MetadataKey(key), viewer) {
			if want := row.ID == "dev_b"; row.Revoked != want {
				t.Errorf("seen from %s, %s Revoked = %v", viewer, row.ID, row.Revoked)
			}
		}
	}
}

// A name sealed by a build from before devices were named carries the macOS ".local"; the device
// list reads it like every other label does.
func TestOpenNameDropsTheLocalSuffixLikeEveryOtherLabel(t *testing.T) {
	key := []byte("0123456789abcdef0123456789abcdef")
	sealed, err := directory.SealName(directory.MetadataKey(key), "studio.local")
	if err != nil {
		t.Fatal(err)
	}
	if got := openName(directory.MetadataKey(key), sealed); got != "studio" {
		t.Fatalf("openName = %q", got)
	}
}
