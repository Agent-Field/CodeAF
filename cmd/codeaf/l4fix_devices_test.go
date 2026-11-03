package main

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Agent-Field/codeaf/internal/directory"
	"github.com/Agent-Field/codeaf/internal/identity"
	"github.com/Agent-Field/codeaf/internal/pair"
)

type l4BrokenRoster struct{ directory.Client }

func (l4BrokenRoster) List(context.Context) (directory.Listing, error) {
	return directory.Listing{}, errors.New("unreadable roster")
}

// L4-R1: A failed roster cannot authorize removing a suffixed selector from the other roster.
func TestL4FixRevokeNeedsBothRosters(t *testing.T) {
	t.Run("sync list", func(t *testing.T) {
		book := bookWith(t, "laptop")
		rows, _ := book.Devices()
		selector := "laptop#" + rows[0].ID()[len(rows[0].ID())-4:]
		_, err := captureStdout(t, func() error {
			return stopDevice([]deviceKind{chatsKind{dir: l4BrokenRoster{}}, bookKind{book}}, []string{selector})
		})
		left, _ := book.Devices()
		if err == nil || !strings.Contains(err.Error(), "devices that sync your chats") || !strings.Contains(err.Error(), "nothing was removed") || len(left) != 1 {
			t.Fatalf("err=%v left=%+v", err, left)
		}
	})
	t.Run("remote list", func(t *testing.T) {
		chats, dir := chatsWith(t, "laptop")
		path := filepath.Join(t.TempDir(), "devices.json")
		if err := os.WriteFile(path, []byte("broken"), 0600); err != nil {
			t.Fatal(err)
		}
		_, err := captureStdout(t, func() error {
			return stopDevice([]deviceKind{chats, bookKind{pair.BookAt(path)}}, []string{"laptop#bbbb"})
		})
		list, _ := dir.List(devicesCtx)
		if err == nil || !strings.Contains(err.Error(), "devices that can use this machine") || !strings.Contains(err.Error(), "nothing was removed") || list.Devices[otherComputer].Revoked {
			t.Fatalf("err=%v list=%+v", err, list)
		}
	})
	t.Run("colliding suffix", func(t *testing.T) {
		chats, dir := chatsWith(t, "laptop")
		book := bookWith(t, "laptop")
		peers, _ := book.Devices()
		// Match the real remote key's suffix in the sync roster too.
		id := "dev_" + peers[0].ID()
		sealed, _ := directory.SealName(chats.key, "laptop")
		if err := dir.PutDevice(devicesCtx, id, directory.Device{V: 1, Name: sealed}); err != nil {
			t.Fatal(err)
		}
		selector := "laptop#" + id[len(id)-4:]
		_, err := captureStdout(t, func() error { return stopDevice([]deviceKind{chats, bookKind{book}}, []string{selector}) })
		peers, _ = book.Devices()
		list, _ := dir.List(devicesCtx)
		if err == nil || !strings.Contains(err.Error(), "more than one device") || !strings.Contains(err.Error(), "codeaf devices revoke") || len(peers) != 1 || list.Devices[id].Revoked {
			t.Fatalf("err=%v peers=%v list=%+v", err, peers, list)
		}
	})
}

// L4-R1: The public revoke command cannot silently drop a roster whose local setup is unreadable.
func TestL4FixPublicRevokeNeedsReadableSetup(t *testing.T) {
	for _, bad := range []string{"identity", "service address"} {
		t.Run(bad, func(t *testing.T) {
			h := t.TempDir()
			t.Setenv("CODEAF_HOME", h)
			t.Setenv("CODEAF_SYNC_URL", "http://127.0.0.1:8901")
			if _, err := identity.Ensure(h); err != nil {
				t.Fatal(err)
			}
			peer := pair.Paired{Label: "laptop", Key: "remote-key"}
			if err := pair.DeviceBook().Admit(peer); err != nil {
				t.Fatal(err)
			}
			if bad == "identity" {
				if err := os.WriteFile(filepath.Join(h, "identity.json"), []byte("broken"), 0600); err != nil {
					t.Fatal(err)
				}
			} else {
				t.Setenv("CODEAF_SYNC_URL", "://not-a-service")
			}
			selector := "laptop#" + peer.ID()[len(peer.ID())-4:]
			_, err := captureStdout(t, func() error { return runDevices([]string{"revoke", selector}) })
			left, _ := pair.DeviceBook().Devices()
			if err == nil || !strings.Contains(err.Error(), "devices that sync your chats") || !strings.Contains(err.Error(), "nothing was removed") || len(left) != 1 {
				t.Fatalf("err=%v left=%+v", err, left)
			}
		})
	}
}
