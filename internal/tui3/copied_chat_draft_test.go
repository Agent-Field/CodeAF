package tui3

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

// Contract J23.2 restores a copy's own words after its project folder changes.
func TestCopiedChatDraftFollowsItsChatAcrossFolders(t *testing.T) {
	root, drafts, profile := t.TempDir(), t.TempDir(), t.TempDir()
	open := func(work, file, draft string) *app {
		return newApp(context.Background(), Options{Agent: &fakeAgent{model: "m"}, Workspace: work, SessionFile: file, DraftFile: draft, ProfileDir: profile, UsageLedger: labLedger()})
	}
	file := filepath.Join(root, "copy-ws9", "01J0000000000000000000000A", ".cell", "transcript.jsonl")
	work := filepath.Join(filepath.Dir(filepath.Dir(file)), "work")
	draft := asWindow(t, deadPid(t), func() string { return DraftFile(drafts, work) })
	a := open(work, file, draft)
	a.input.setText("words saved only for A1")
	a.writeDraftsNow(a.leavingDraft())
	// This record uses the format written before chat identities survived moves.
	owner := "\x00" + work + "\x00" + file
	old := draftKeep{Version: 1, Owner: owner, At: "2026-10-27T12:00:00Z", Slots: []draftKeepSlot{{Owner: owner, Text: "words saved only for A1", Caret: 5}}}
	raw, err := json.Marshal(old)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(draftKeepPath(draft), raw, 0600); err != nil {
		t.Fatal(err)
	}
	movedFile := filepath.Join(root, "copy-new-ws9", "01J0000000000000000000000A", ".cell", "transcript.jsonl")
	movedWork := filepath.Join(filepath.Dir(filepath.Dir(movedFile)), "work")
	b := open(movedWork, movedFile, DraftFile(drafts, movedWork))
	if got := b.input.String(); got != "words saved only for A1" {
		t.Fatalf("moved A1 restored %q", got)
	}
	otherFile := filepath.Join(root, "copy-ws", "01J0000000000000000000000B", ".cell", "transcript.jsonl")
	other := open(movedWork, otherFile, DraftFile(drafts, movedWork))
	if got := other.input.String(); got != "" {
		t.Fatalf("A2 restored A1's words: %q", got)
	}
}

// Contract J23.2 never adopts another named chat's plain draft export.
func TestCopiedChatCannotAdoptAnotherChatsDraftExport(t *testing.T) {
	drafts, work := t.TempDir(), t.TempDir()
	first := newTestApp(&fakeAgent{model: "m"})
	first.workspace = work
	first.file = filepath.Join(work, "01J0000000000000000000000A", ".cell", "transcript.jsonl")
	first.draftFile = asWindow(t, deadPid(t), func() string { return DraftFile(drafts, work) })
	first.input.setText("words saved only for A1")
	first.writeDraftsNow(first.leavingDraft())
	other := newApp(context.Background(), Options{Agent: &fakeAgent{model: "m"}, Workspace: work,
		SessionFile: filepath.Join(work, "01J0000000000000000000000B", ".cell", "transcript.jsonl"),
		DraftFile:   DraftFile(drafts, work), ProfileDir: t.TempDir(), UsageLedger: labLedger()})
	if got := other.input.String(); got != "" {
		t.Errorf("A2 adopted A1's words: %q", got)
	}
	if _, err := os.Stat(first.draftFile); err != nil {
		t.Errorf("A1's draft export was taken: %v", err)
	}
}
