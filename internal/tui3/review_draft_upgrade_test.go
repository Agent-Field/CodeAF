package tui3

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/Agent-Field/codeaf/internal/session"
)

// reviewDraftUpgrade writes the filename and owner bytes of a pre-upgrade
// window directly. Calling today's mint would fail to exercise an upgrade.
func reviewDraftUpgrade(t *testing.T) (string, string, string, string) {
	t.Helper()
	t.Setenv("CODEAF_HOME", t.TempDir())
	target := t.TempDir()
	alias := filepath.Join(t.TempDir(), "project")
	if err := os.Symlink(target, alias); err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	sum := sha256.Sum256([]byte(filepath.Clean(alias)))
	old := filepath.Join(dir, fmt.Sprintf("draft-%s-0-%d.txt", hex.EncodeToString(sum[:8]), deadPid(t)))
	// The existing borrowed conversation is proof of the alias even when the
	// new launch only knows the canonical path.
	conversation := filepath.Join(os.Getenv("CODEAF_HOME"), "v3", "projects", "old-bucket", "00000000000000f1")
	if err := os.MkdirAll(conversation, 0700); err != nil {
		t.Fatal(err)
	}
	if err := session.SaveMeta(conversation, session.Meta{ID: "00000000000000f1", Workspace: alias}); err != nil {
		t.Fatal(err)
	}
	transcript := (session.Place{Dir: conversation}).Transcript()
	return target, alias, old, transcript
}

func TestReviewExistingDraftPrefixSurvivesUpgrade(t *testing.T) {
	workspace, _, old, transcript := reviewDraftUpgrade(t)
	if err := os.WriteFile(old, []byte("unfinished contract amendments"), 0600); err != nil {
		t.Fatal(err)
	}
	next, _, _ := roomApp(t)
	next.workspace, next.file = workspace, transcript
	next.draftFile = DraftFile(filepath.Dir(old), workspace)
	next.restoreDraft()
	if got := next.input.String(); got != "unfinished contract amendments" {
		t.Fatalf("restored %q", got)
	}
	if _, err := os.Stat(old); !os.IsNotExist(err) {
		t.Fatalf("adopted plain draft was not moved: %v", err)
	}
}

func TestReviewStructuredDraftSurvivesUpgrade(t *testing.T) {
	workspace, alias, old, transcript := reviewDraftUpgrade(t)
	owner := "\x00" + filepath.Clean(alias) + "\x00" + filepath.Clean(transcript)
	attachment := picture(t, "kept.png")
	keep := draftKeep{Version: 1, Owner: owner, At: "2026-10-20T12:00:00Z", Slots: []draftKeepSlot{
		{Owner: owner, Text: "review [paste 1 · 2 lines]", Caret: 6, Pastes: []draftKeepPaste{{N: 1, Text: "archived\ncontract"}}, Chips: []draftKeepChip{{Path: attachment}}},
		{Owner: owner, Task: 7, Text: "widen the pipe", Caret: 4},
	}}
	raw, err := json.Marshal(keep)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(draftKeepPath(old), raw, 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(old, []byte("stale export must not return"), 0600); err != nil {
		t.Fatal(err)
	}
	next, _, _ := roomApp(t)
	next.workspace, next.file = workspace, transcript
	next.draftFile = DraftFile(filepath.Dir(old), workspace)
	next.restoreDraft()
	if next.input.String() != keep.Slots[0].Text || next.input.cursor != 6 {
		t.Fatalf("main composer = %+v", next.input)
	}
	restored, how := readDraftKeep(draftKeepPath(next.draftFile))
	if how != draftKeepFound || len(restored.Slots) != 2 {
		t.Fatalf("restored record: %+v (%v)", restored, how)
	}
	main, ok := keptSlotFor(t, draftKeepPath(next.draftFile), mainRecipient)
	if !ok || len(main.Pastes) != 1 || main.Pastes[0].Text != "archived\ncontract" || len(main.Chips) != 1 || main.Chips[0].Path != attachment {
		t.Fatalf("documents/tray: %+v", main)
	}
	next.openRoom(7, "Fix the pipe")
	if got := next.input.String(); got != "widen the pipe" {
		t.Fatalf("task composer: %q", got)
	}
	if slot, held := keptSlotFor(t, draftKeepPath(old), taskRecipient(7)); held {
		t.Fatalf("reunion copied the slot: %+v", slot)
	}
	if _, err := os.Stat(old); !os.IsNotExist(err) {
		t.Fatalf("stale legacy export survived reunion: %v", err)
	}
}

func TestReviewAliasDraftKeepsIdentityAndNewestGuards(t *testing.T) {
	workspace, alias, old, transcript := reviewDraftUpgrade(t)
	owner := "\x00" + alias + "\x00" + transcript
	write := func(path string, keep draftKeep) {
		raw, err := json.Marshal(keep)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(draftKeepPath(path), raw, 0600); err != nil {
			t.Fatal(err)
		}
	}
	write(old, draftKeep{Version: 1, Owner: owner, At: "2026-10-20T12:00:00Z", Slots: []draftKeepSlot{{Owner: owner, Task: 7, Text: "old correction"}}})
	other := "\x00" + alias + "\x00" + filepath.Join(filepath.Dir(transcript), "other.jsonl")
	stranger := filepath.Join(filepath.Dir(old), filepath.Base(old)+"-other-999999998.txt")
	write(stranger, draftKeep{Version: 1, Owner: other, At: "2026-10-20T14:00:00Z", Slots: []draftKeepSlot{{Owner: other, Task: 7, Text: "somebody else's correction"}}})
	next, _, _ := roomApp(t)
	next.workspace, next.file = workspace, transcript
	next.draftFile = DraftFile(filepath.Dir(old), workspace)
	// A newer canonical record says this conversation's box and pages are clear.
	newer := filepath.Join(filepath.Dir(old), fmt.Sprintf("%s42-%d.txt", draftPrefix(workspace), deadPid(t)))
	write(newer, draftKeep{Version: 1, Owner: "\x00" + workspace + "\x00" + transcript, At: "2026-10-20T13:00:00Z"})
	next.restoreDraft()
	next.openRoom(7, "Fix the pipe")
	if got := next.input.String(); got != "" {
		t.Fatalf("newer clear restored %q", got)
	}
	if slot, held := keptSlotFor(t, draftKeepPath(stranger), taskRecipient(7)); !held || slot.Text != "somebody else's correction" {
		t.Fatalf("stranger's slot changed: %+v", slot)
	}
	// A live alias record remains owned by that window even if it is newest.
	live := filepath.Join(filepath.Dir(old), fmt.Sprintf("%s99-%d.txt", draftPrefix(alias), os.Getpid()))
	write(live, draftKeep{Version: 1, Owner: owner, At: "2026-10-20T15:00:00Z", Slots: []draftKeepSlot{{Owner: owner, Task: 7, Text: "still typing"}}})
	another, _, _ := roomApp(t)
	another.workspace, another.file = workspace, transcript
	another.draftFile = DraftFile(filepath.Dir(old), workspace)
	another.restoreDraft()
	if slot, held := keptSlotFor(t, draftKeepPath(live), taskRecipient(7)); !held || slot.Text != "still typing" {
		t.Fatalf("live window lost its slot: %+v", slot)
	}
}
