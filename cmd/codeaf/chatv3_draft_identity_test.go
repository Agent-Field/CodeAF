package main

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/Agent-Field/codeaf/internal/remote"
)

// Contract J23.2 gives copied chats separate draft keys that survive folder moves.
func TestCopiedChatDoorsKeepDistinctStableDraftKeys(t *testing.T) {
	profile := t.TempDir()
	t.Setenv("CODEAF_HOME", profile)
	t.Setenv("CODEAF_PROFILE_DIR", profile)
	fleet := onePipeFleet("", hostedClient(t))
	t.Cleanup(fleet.closeAll)
	welcome := remote.Welcome{Version: remote.Version, ProfileDir: profile}
	keyOf := func(path string) string {
		t.Helper()
		parts := strings.Split(filepath.Base(path), "-")
		if len(parts) != 4 {
			t.Fatalf("unexpected draft name: %q", path)
		}
		return parts[1]
	}
	keyFor := func(file string) string {
		t.Helper()
		welcome.SessionFile = file
		opts, _ := hostOptions(fleet, welcome, false)
		return keyOf(opts.DraftFile)
	}
	first := keyFor("/copies/ws9/01J0000000000000000000000A/.cell/transcript.jsonl")
	second := keyFor("/copies/ws/01J0000000000000000000000B/.cell/transcript.jsonl")
	if first == second {
		t.Fatal("two copied chats with no reported workspace share a draft key")
	}
	if moved := keyFor("/copies/another-project/01J0000000000000000000000A/.cell/transcript.jsonl"); moved != first {
		t.Fatal("moving a copied chat changed its draft key")
	}
	welcome.SessionFile = "/copies/ws9/01J0000000000000000000000A/.cell/transcript.jsonl"
	openedFirst := fleet.bundle(fleet.boot, welcome)
	welcome.SessionFile = "/copies/ws/01J0000000000000000000000B/.cell/transcript.jsonl"
	openedSecond := fleet.bundle(fleet.boot, welcome)
	if keyOf(openedFirst.DraftFile) != first || keyOf(openedSecond.DraftFile) != second || openedFirst.DraftFile == openedSecond.DraftFile {
		t.Fatal("conversations opened beside each other reused the boot chat's draft")
	}
}
