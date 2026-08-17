package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Agent-Field/aforge-v2/internal/home"
	"github.com/Agent-Field/aforge-v2/internal/session"
)

// writeFlatSession lays out one session the way the flat layout wrote it: the
// transcript, its two stem-derived sidecars, and the node journals in the
// parallel tree keyed by session id.
func writeFlatSession(t *testing.T, bucket, stem, id string, lines ...string) string {
	t.Helper()
	if err := os.MkdirAll(bucket, 0o700); err != nil {
		t.Fatal(err)
	}
	transcript := filepath.Join(bucket, stem+".jsonl")
	if err := os.WriteFile(transcript, []byte(strings.Join(lines, "\n")+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	for name, body := range map[string]string{
		stem + ".state.json": `{"type":"state","version":1,"records":[]}`,
		stem + ".tasks.json": `{"type":"tasks","version":1,"seq":0,"nodes":[]}`,
	} {
		if err := os.WriteFile(filepath.Join(bucket, name), []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	journals := home.Join("v3", "tasks", id)
	if err := os.MkdirAll(journals, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(journals, "1.jsonl"), []byte("{}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	return transcript
}

// The whole of a flat session folds into one folder named by its header id: the
// journal, both sidecars, the node journals from the parallel tree, and a
// meta.json built from what the header remembered (docs/CHAT-V3.md, Decision
// 26's last law).
func TestTheBootPassFoldsAFlatSessionIntoItsFolder(t *testing.T) {
	t.Setenv("AFORGE_HOME", filepath.Join(t.TempDir(), "state"))
	old := home.Join("v3", "sessions", "-home-p-code")
	writeFlatSession(t, old, "20260815-090102_b7c1", "0123456789abcdef",
		`{"type":"session","version":1,"id":"0123456789abcdef","cwd":"/home/p/code","model":"test/model","timestamp":"2026-08-15T09:01:02Z"}`,
		`{"type":"message","role":"user","content":"port the resume picker","timestamp":"2026-08-15T09:01:03Z"}`,
		`{"type":"title","title":"port the resume picker","timestamp":"2026-08-15T09:01:04Z"}`)
	if err := os.WriteFile(filepath.Join(old, "tasks.jsonl"), []byte("{}\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	migrateV3Tree()

	dir := home.Join("v3", "projects", "-home-p-code", "0123456789abcdef")
	place := session.Place{Dir: dir}
	for _, want := range []string{
		place.Transcript(),
		place.State(),
		place.Tasks(),
		filepath.Join(place.NodeJournals(), "1.jsonl"),
		home.Join("v3", "projects", "-home-p-code", "tasks.jsonl"),
	} {
		if _, err := os.Stat(want); err != nil {
			t.Fatalf("%s did not move: %v", want, err)
		}
	}
	meta, err := session.LoadMeta(dir)
	if err != nil {
		t.Fatal(err)
	}
	if meta.ID != "0123456789abcdef" {
		t.Fatalf("the folded session is called %q", meta.ID)
	}
	if meta.Workspace != "/home/p/code" {
		t.Fatalf("the workspace was recovered as %q, want the header's cwd", meta.Workspace)
	}
	if meta.Title != "port the resume picker" {
		t.Fatalf("the folded session's row says %q", meta.Title)
	}
	if meta.LastUserAt.IsZero() {
		t.Fatal("a folded conversation has no last-active stamp, so it can never be resumed by default")
	}
	// The old tree is gone, which is what makes this one-way.
	if _, err := os.Stat(home.Join("v3", "sessions")); err == nil {
		t.Fatal("the old session directory survived a complete migration")
	}
}

// A transcript whose header cannot be read has no name to be given, so it stays
// exactly where it is with one line on the log. A corrupt old session must not
// cost anybody their new one.
func TestTheBootPassLeavesAnUnreadableSessionWhereItIs(t *testing.T) {
	t.Setenv("AFORGE_HOME", filepath.Join(t.TempDir(), "state"))
	old := home.Join("v3", "sessions", "-home-p-code")
	if err := os.MkdirAll(old, 0o700); err != nil {
		t.Fatal(err)
	}
	broken := filepath.Join(old, "20260815-090102_dead.jsonl")
	if err := os.WriteFile(broken, []byte("this is not json\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	migrateV3Tree()

	if _, err := os.Stat(broken); err != nil {
		t.Fatalf("the unreadable transcript was moved or lost: %v", err)
	}
}

// A session another window is holding open is not touched at all: moving a live
// transcript out from under a running aforge would cost somebody the
// conversation they are in the middle of.
func TestTheBootPassSkipsALiveSession(t *testing.T) {
	t.Setenv("AFORGE_HOME", filepath.Join(t.TempDir(), "state"))
	old := home.Join("v3", "sessions", "-home-p-code")
	live := writeFlatSession(t, old, "20260815-090102_b7c1", "0123456789abcdef",
		`{"type":"session","version":1,"id":"0123456789abcdef","cwd":"/home/p/code","timestamp":"2026-08-15T09:01:02Z"}`,
		`{"type":"message","role":"user","content":"still typing","timestamp":"2026-08-15T09:01:03Z"}`)

	// The other window, holding the journal exactly as internal/session does.
	held, err := session.New(session.Config{
		Workspace:   t.TempDir(),
		Model:       "test/model",
		APIKey:      "k",
		BaseURL:     "https://example.invalid/v1",
		SessionFile: live,
	})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = held.Close() }()

	migrateV3Tree()

	if _, err := os.Stat(live); err != nil {
		t.Fatalf("a live session was folded out from under its window: %v", err)
	}
	if _, err := os.Stat(home.Join("v3", "projects", "-home-p-code", "0123456789abcdef")); err == nil {
		t.Fatal("a live session was folded into the new layout")
	}
}
