package main

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Agent-Field/codeaf/internal/cell"
	"github.com/Agent-Field/codeaf/internal/config"
	"github.com/Agent-Field/codeaf/internal/home"
	"github.com/Agent-Field/codeaf/internal/remote"
	"github.com/Agent-Field/codeaf/internal/session"
)

func TestOlderChatGrowthNoticeOnOpen(t *testing.T) {
	t.Setenv(home.EnvVar, t.TempDir())
	original := filepath.Join(home.Dir(), "v3/projects/project/older")
	work := t.TempDir()
	if err := os.MkdirAll(original, 0700); err != nil {
		t.Fatal(err)
	}
	source := filepath.Join(original, "transcript.jsonl")
	initial := "{\"type\":\"session\",\"version\":1,\"id\":\"older\"}\n" + olderNoticeTurn("before copy")
	if err := os.WriteFile(source, []byte(initial), 0600); err != nil {
		t.Fatal(err)
	}
	c, err := cell.CopyLegacy(t.Context(), original, func(_ctx context.Context, c cell.Cell) error {
		return session.SaveMeta(c.Root, session.Meta{ID: c.ID, Workspace: work})
	})
	if err != nil {
		t.Fatal(err)
	}
	if line := v3UpgradeNotice(c.Root, true); line != "" {
		t.Fatalf("unchanged original: %q", line)
	}
	grown := []byte(initial + olderNoticeTurn("rollback one") + olderNoticeTurn("rollback two"))
	if err := os.WriteFile(source, grown, 0600); err != nil {
		t.Fatal(err)
	}
	if line := v3UpgradeNotice(c.Root, false); line != "" {
		t.Fatalf("headless: %q", line)
	}
	place, err := movablePlace(t.Context(), session.Place{Dir: original, Workspace: work})
	if err != nil || place.Dir != c.Root {
		t.Fatalf("open selected %q: %v", place.Dir, err)
	}
	line := v3UpgradeNotice(place.Dir, true)
	for _, want := range []string{"2 turns typed in an older codeaf", source, "are not shown here"} {
		if !strings.Contains(line, want) {
			t.Errorf("notice %q lacks %q", line, want)
		}
	}
	if line := hostEntryNotice(remote.Welcome{Note: line}); !strings.Contains(line, "2 turns") {
		t.Fatalf("welcome hid notice: %q", line)
	}
	after, _ := os.ReadFile(source)
	if !bytes.Equal(after, grown) {
		t.Fatal("original bytes changed")
	}
	place, err = movablePlace(t.Context(), session.Place{Dir: original, Workspace: work})
	if err != nil {
		t.Fatal(err)
	}
	if line := v3UpgradeNotice(place.Dir, true); line != "" {
		t.Fatalf("repeated: %q", line)
	}
	if err := os.WriteFile(source, append(grown, []byte(olderNoticeTurn("rollback three"))...), 0600); err != nil {
		t.Fatal(err)
	}
	if line := v3UpgradeNotice(c.Root, true); !strings.Contains(line, "3 turns") {
		t.Fatalf("new growth: %q", line)
	}
	if err := os.WriteFile(source, grown, 0600); err != nil {
		t.Fatal(err)
	}
	if line := v3UpgradeNotice(c.Root, true); line != "" {
		t.Fatalf("a shorter original repeated older news: %q", line)
	}
	if err := os.WriteFile(filepath.Join(c.Root, cell.CopyReadyPath), nil, 0600); err != nil {
		t.Fatal(err)
	}
	if line := v3UpgradeNotice(c.Root, true); line != "" {
		t.Fatalf("unstamped copy guessed: %q", line)
	}
}

func olderNoticeTurn(words string) string {
	return fmt.Sprintf("{\"type\":\"message\",\"role\":\"user\",\"content\":%q}\n{\"type\":\"message\",\"role\":\"assistant\",\"content\":\"answer\"}\n", words)
}

func TestStandingUpgradeUsesOwningHomeNotKeyboardProfile(t *testing.T) {
	owner, keyboard := t.TempDir(), t.TempDir()
	t.Setenv(home.EnvVar, owner)
	t.Setenv(config.ProfileDirEnv, keyboard)
	if err := os.CopyFS(filepath.Join(owner, "v3/standing"), os.DirFS("../../internal/upgradenotice/testdata/standing")); err != nil {
		t.Fatal(err)
	}
	if line := v3UpgradeNotice("", false); line != "" {
		t.Fatalf("headless launch said %q", line)
	}
	ack := "v3/standing-upgrade-noticed-v1.json"
	if _, err := os.Stat(filepath.Join(owner, ack)); !os.IsNotExist(err) {
		t.Fatal("headless launch acknowledged home")
	}
	line := hostEntryNotice(remote.Welcome{Note: v3UpgradeNotice("", true), ProfileDir: keyboard})
	if !strings.Contains(line, filepath.Join(owner, "v3/standing-upgrade-report.txt")) {
		t.Fatalf("notice did not name owning home's report: %q", line)
	}
	if _, err := os.Stat(filepath.Join(owner, ack)); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(keyboard, ack)); !os.IsNotExist(err) {
		t.Fatal("acknowledgement belongs to keyboard profile")
	}
	if line := v3UpgradeNotice("", true); line != "" {
		t.Fatalf("second launch said %q", line)
	}
}
