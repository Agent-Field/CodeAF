package main

import (
	"strings"
	"testing"

	"github.com/Agent-Field/codeaf/internal/syncsetup"
)

// L4-B8 and stability: the remote/pairing override does not silently move chat
// syncing away from the service saved by pairing. Each command names its input.
func TestL4Fix2RemoteOverrideDoesNotMoveChatSync(t *testing.T) {
	local := t.TempDir()
	t.Setenv("CODEAF_HOME", local)
	t.Setenv("CODEAF_SYNC_URL", "")
	t.Setenv("CODEAF_RELAY", "https://remote.example")
	if err := syncsetup.SaveRelayURL(local, "https://sync.example"); err != nil {
		t.Fatal(err)
	}
	if got := syncsetup.Resolve(local).URL; got != "https://sync.example" {
		t.Errorf("remote override changed chat syncing: %q", got)
	}
	route, err := pairMailbox("")
	if err != nil || route.URL != "https://remote.example" {
		t.Fatalf("pairing route=%+v %v", route, err)
	}
	if command := (chatPairDoor{}).PairCommand(); !strings.Contains(command, "--via https://remote.example") {
		t.Fatal(command)
	}
	if got := newApprovalsDoor(local).SyncService(); got != "https://sync.example" {
		t.Errorf("devices named the wrong chat service: %q", got)
	}
	t.Setenv("CODEAF_SYNC_URL", "https://explicit-sync.example")
	route, err = pairMailbox("")
	if err != nil || route.URL != "https://explicit-sync.example" {
		t.Fatalf("explicit sync route=%+v %v", route, err)
	}
}
