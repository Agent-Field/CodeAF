package session

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/cell"
	"github.com/Agent-Field/codeaf/internal/home"
	"github.com/Agent-Field/codeaf/internal/store"
)

// TestC29CopyCarriesTruthAndReplacesTheListRow proves C29.1/C29.3: the new
// build lists the copy once, with the same title/order, dials, journals and memory.
func TestC29CopyCarriesTruthAndReplacesTheListRow(t *testing.T) {
	bucket := t.TempDir()
	original := filepath.Join(bucket, "old-session")
	if err := os.MkdirAll(filepath.Join(original, "tasks"), 0700); err != nil {
		t.Fatal(err)
	}
	transcript := `{"type":"session","version":1,"id":"old-session"}` + "\n" + `{"type":"message","role":"user","content":"all my earlier words"}` + "\n"
	if err := os.WriteFile(filepath.Join(original, "transcript.jsonl"), []byte(transcript), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(original, "tasks", "one.jsonl"), []byte("whole task journal"), 0600); err != nil {
		t.Fatal(err)
	}
	at := time.Unix(1700000000, 0)
	if err := SaveMeta(original, Meta{ID: "old-session", Title: "The older conversation", Effort: "high", Approval: "allow", LastUserAt: at, LastReplyAt: at.Add(time.Minute), Workspace: bucket}); err != nil {
		t.Fatal(err)
	}
	brain, err := store.Open(filepath.Join(t.TempDir(), "graph.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer brain.Close()
	_, err = brain.AddMemory(store.Memory{Owner: store.OwnerUser, Text: "An older chat's remembered preference.", SourceSession: "old-session"})
	if err != nil {
		t.Fatal(err)
	}
	c, err := cell.CopyLegacy(t.Context(), original, func(ctx context.Context, c cell.Cell) error { return PrepareCopy(ctx, original, c, brain) })
	if err != nil {
		t.Fatal(err)
	}
	meta, err := LoadMeta(c.Root)
	if err != nil {
		t.Fatal(err)
	}
	if meta.Effort != "high" || meta.Approval != "allow" || meta.Title != "The older conversation" || !meta.LastUserAt.Equal(at) {
		t.Fatalf("copy lost truth: %+v", meta)
	}
	// The older chat's last answer is not the copy's: a copy is never born
	// with something new for the person.
	if !meta.LastReplyAt.IsZero() {
		t.Fatalf("the copy carries the older chat's answer time %v", meta.LastReplyAt)
	}
	events, err := ReadMemoryEvents(c.Root)
	if err != nil || len(events) != 1 {
		t.Fatalf("memory did not copy: %v %v", events, err)
	}
	raw, err := os.ReadFile(filepath.Join(c.Root, ".cell", "tasks", "one.jsonl"))
	if err != nil || string(raw) != "whole task journal" {
		t.Fatal("task journal did not copy")
	}
	rows := ReadWorld(filepath.Dir(bucket)).Sessions()
	_ = rows
	recent := RecentSessions(bucket, 10)
	if len(recent) != 1 || recent[0].File != filepath.Join(c.Root, cell.TranscriptPath) {
		t.Fatalf("list did not replace original: %+v", recent)
	}
	if got, err := os.ReadFile(filepath.Join(original, "transcript.jsonl")); err != nil || string(got) != transcript {
		t.Fatal("copy edited original")
	}
}

// TestBucketSessionDirsKeepsMovingAndLegacyChatsOnce verifies that ReadWorld,
// Adopt, RecentSessions, SessionWorkspaces and knowledgeHistory.Inventory list
// standalone `.moving` chats alongside copied `.moving` chats and uncopied
// legacy chats, each once, while hiding unfinished `.moving` copy scratch folders.
func TestBucketSessionDirsKeepsMovingAndLegacyChatsOnce(t *testing.T) {
	homeDir := t.TempDir()
	t.Setenv(home.EnvVar, homeDir)
	root := PlacesRoot()
	bucket := filepath.Join(root, "proj")
	workspace := t.TempDir()
	now := time.Unix(1700001000, 0)

	writeChat := func(dir, id, title string, at time.Time) {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			t.Fatal(err)
		}
		place := Place{Dir: dir}
		if err := os.MkdirAll(filepath.Dir(place.Transcript()), 0o700); err != nil {
			t.Fatal(err)
		}
		body := `{"type":"session","version":1,"id":"` + id + `"}` + "\n" +
			`{"type":"message","role":"user","content":"` + title + `"}` + "\n"
		if err := os.WriteFile(place.Transcript(), []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
		if err := SaveMeta(dir, Meta{ID: id, Title: title, Workspace: workspace, Created: at, LastUserAt: at}); err != nil {
			t.Fatal(err)
		}
	}

	// 1. Legacy chat with no copy.
	legacyDir := filepath.Join(bucket, "legacy-one")
	writeChat(legacyDir, "legacy-one", "Legacy One", now.Add(-3*time.Minute))

	// 2. Legacy chat with a completed `.moving` copy.
	copiedOrig := filepath.Join(bucket, "copied-orig")
	writeChat(copiedOrig, "copied-orig", "Stale Original", now.Add(-10*time.Minute))
	copiedCell, err := cell.CopyLegacy(t.Context(), copiedOrig, func(ctx context.Context, c cell.Cell) error {
		return PrepareCopy(ctx, copiedOrig, c, nil)
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := SaveMeta(copiedCell.Root, Meta{ID: copiedCell.ID, Title: "Copied Moving", Workspace: workspace, Created: now.Add(-10 * time.Minute), LastUserAt: now.Add(-2 * time.Minute)}); err != nil {
		t.Fatal(err)
	}

	// 3. Standalone `.moving` chat with no top-level legacy folder.
	standaloneCell, err := cell.CreateIn(filepath.Join(bucket, ".moving"), cell.Options{Class: cell.HostBound})
	if err != nil {
		t.Fatal(err)
	}
	writeChat(standaloneCell.Root, standaloneCell.ID, "Standalone Moving", now.Add(-1*time.Minute))

	// 4. Unfinished `.moving` scratch copy without `copy-ready`.
	unfinishedOrig := filepath.Join(bucket, "unfinished-orig")
	writeChat(unfinishedOrig, "unfinished-orig", "Unfinished Original", now)
	scratch := cell.CopyRoot(unfinishedOrig)
	if err := os.WriteFile(scratch+".lock", nil, 0o600); err != nil {
		t.Fatal(err)
	}
	writeChat(scratch, filepath.Base(scratch), "Half Made Scratch", now)

	world := ReadWorld(root)
	rows := world.Sessions()
	if len(rows) != 4 {
		t.Fatalf("ReadWorld returned %d sessions, want 4: %+v", len(rows), rows)
	}
	recent := RecentSessions(bucket, 10)
	if len(recent) != 4 {
		t.Fatalf("RecentSessions returned %d summaries, want 4: %+v", len(recent), recent)
	}

	// Adopt an unspoken open `.moving` shell into the bucket's project.
	shellCell, err := cell.CreateIn(filepath.Join(bucket, ".moving"), cell.Options{Class: cell.HostBound})
	if err != nil {
		t.Fatal(err)
	}
	if err := SaveMeta(shellCell.Root, Meta{ID: shellCell.ID, Workspace: workspace, Created: now}); err != nil {
		t.Fatal(err)
	}
	shellTranscript := Place{Dir: shellCell.Root}.Transcript()
	if !world.Adopt(root, SessionRow{Transcript: shellTranscript, Workspace: workspace}, now) {
		t.Fatal("World.Adopt rejected an open unspoken .moving session")
	}
	if len(world.Sessions()) != 5 {
		t.Fatalf("World.Sessions after Adopt = %d, want 5", len(world.Sessions()))
	}

	workspaces := SessionWorkspaces()
	for _, wantID := range []string{"legacy-one", copiedCell.ID, standaloneCell.ID, "unfinished-orig", shellCell.ID} {
		if workspaces[wantID] != workspace {
			t.Fatalf("SessionWorkspaces[%q] = %q, want %q", wantID, workspaces[wantID], workspace)
		}
	}
	if got, exists := workspaces[filepath.Base(scratch)]; exists {
		t.Fatalf("SessionWorkspaces answered %q for unfinished .moving scratch copy %q", got, filepath.Base(scratch))
	}
	if got, exists := workspaces["copied-orig"]; exists {
		t.Fatalf("SessionWorkspaces answered %q for replaced legacy original \"copied-orig\"", got)
	}

	journals, err := (knowledgeHistory{root: root}).Inventory(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if len(journals) != 4 {
		t.Fatalf("knowledgeHistory.Inventory returned %d journals, want 4: %+v", len(journals), journals)
	}
}

// TestBucketWithoutMovingMakesNoCopyLookups counts copy-root and copy-resolution
// lookups through seams so a bucket with many chats and no `.moving` directory
// pays zero per-folder copy work, and a bucket with `.moving` resolves only the
// originals whose copy roots exist in `.moving` without resolving a second time
// in readSessionRow.
func TestBucketWithoutMovingMakesNoCopyLookups(t *testing.T) {
	homeDir := t.TempDir()
	t.Setenv(home.EnvVar, homeDir)
	root := PlacesRoot()
	bucket := filepath.Join(root, "many-chats")
	workspace := t.TempDir()
	now := time.Unix(1700010000, 0)

	const totalChats = 200
	var sampleTranscripts []string
	for i := 0; i < totalChats; i++ {
		id := fmt.Sprintf("chat-%04d", i)
		dir := filepath.Join(bucket, id)
		if err := os.MkdirAll(dir, 0o700); err != nil {
			t.Fatal(err)
		}
		place := Place{Dir: dir}
		body := fmt.Sprintf("{\"type\":\"session\",\"version\":1,\"id\":%q}\n{\"type\":\"message\",\"role\":\"user\",\"content\":\"chat %d\"}\n", id, i)
		if err := os.WriteFile(place.Transcript(), []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
		at := now.Add(time.Duration(i) * time.Second)
		if err := SaveMeta(dir, Meta{ID: id, Title: fmt.Sprintf("Chat %d", i), Workspace: workspace, Created: at, LastUserAt: at}); err != nil {
			t.Fatal(err)
		}
		if i < 10 {
			sampleTranscripts = append(sampleTranscripts, place.Transcript())
		}
	}

	var copyRootCalls, copiedCalls, openAtCalls int
	prevCopyRoot, prevCopied, prevOpenAt := bucketCopyRoot, bucketCopied, bucketOpenAt
	bucketCopyRoot = func(original string) string {
		copyRootCalls++
		return prevCopyRoot(original)
	}
	bucketCopied = func(original string) string {
		copiedCalls++
		return prevCopied(original)
	}
	bucketOpenAt = func(root, id string) (cell.Cell, error) {
		openAtCalls++
		return prevOpenAt(root, id)
	}
	t.Cleanup(func() {
		bucketCopyRoot = prevCopyRoot
		bucketCopied = prevCopied
		bucketOpenAt = prevOpenAt
	})

	t.Run("no_moving_folder", func(t *testing.T) {
		copyRootCalls, copiedCalls, openAtCalls = 0, 0, 0
		dirs := BucketSessionDirs(bucket)
		if len(dirs) != totalChats {
			t.Fatalf("BucketSessionDirs returned %d dirs, want %d", len(dirs), totalChats)
		}
		world := ReadWorld(root)
		if got := len(world.Sessions()); got != totalChats {
			t.Fatalf("ReadWorld returned %d sessions, want %d", got, totalChats)
		}
		workspaces := SessionWorkspaces()
		if len(workspaces) != totalChats {
			t.Fatalf("SessionWorkspaces returned %d entries, want %d", len(workspaces), totalChats)
		}
		rows := ReadRows(sampleTranscripts)
		if len(rows) != len(sampleTranscripts) {
			t.Fatalf("ReadRows returned %d rows, want %d", len(rows), len(sampleTranscripts))
		}
		if copyRootCalls != 0 || copiedCalls != 0 || openAtCalls != 0 {
			t.Fatalf("bucket without .moving made copy lookups: copyRoot=%d copied=%d openAt=%d, want 0/0/0",
				copyRootCalls, copiedCalls, openAtCalls)
		}
	})

	t.Run("one_copy_among_many_chats", func(t *testing.T) {
		copiedOrig := filepath.Join(bucket, "chat-0000")
		copiedCell, err := cell.CopyLegacy(t.Context(), copiedOrig, func(ctx context.Context, c cell.Cell) error {
			return PrepareCopy(ctx, copiedOrig, c, nil)
		})
		if err != nil {
			t.Fatal(err)
		}
		if err := SaveMeta(copiedCell.Root, Meta{
			ID:         copiedCell.ID,
			Title:      "Copied Chat 0",
			Workspace:  workspace,
			Created:    now,
			LastUserAt: now.Add(time.Hour),
		}); err != nil {
			t.Fatal(err)
		}

		copyRootCalls, copiedCalls, openAtCalls = 0, 0, 0
		world := ReadWorld(root)
		if got := len(world.Sessions()); got != totalChats {
			t.Fatalf("ReadWorld returned %d sessions, want %d", got, totalChats)
		}
		if copiedCalls != 1 {
			t.Fatalf("ReadWorld called cell.Copied %d times for 1 copy among %d chats, want 1 (no uncopied lookups and no double lookup in readSessionRow)",
				copiedCalls, totalChats)
		}
		if openAtCalls != 0 {
			t.Fatalf("ReadWorld called extra cell.OpenAt %d times, want 0", openAtCalls)
		}
	})
}
