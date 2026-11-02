package main

import (
	"context"
	"fmt"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/cell"
	"github.com/Agent-Field/codeaf/internal/cell/celltest"
	"github.com/Agent-Field/codeaf/internal/cellstore"
	"github.com/Agent-Field/codeaf/internal/home"
	"github.com/Agent-Field/codeaf/internal/identity"
	"github.com/Agent-Field/codeaf/internal/session"
	"github.com/Agent-Field/codeaf/internal/syncsetup"
)

// TestC29FirstSavedTurnIncludesTheEarlierConversation proves C29.1/C29.4 with
// the supplied engine: rewind reaches the copy's first folder state and retains
// the entire earlier conversation, while the original remains unchanged.
func TestC29FirstSavedTurnIncludesTheEarlierConversation(t *testing.T) {
	celltest.RequireEngine(t)
	t.Setenv(home.EnvVar, t.TempDir())
	t.Cleanup(cellstore.StopSpawned)
	work := t.TempDir()
	if err := os.WriteFile(filepath.Join(work, "hello.txt"), []byte("at copy time"), 0600); err != nil {
		t.Fatal(err)
	}
	original := filepath.Join(home.Dir(), "v3", "projects", "old", "old-session")
	if err := os.MkdirAll(original, 0700); err != nil {
		t.Fatal(err)
	}
	journal := `{"type":"session","version":1,"id":"old-session"}` + "\n" + `{"type":"message","role":"user","content":"my whole earlier conversation"}` + "\n"
	if err := os.WriteFile(filepath.Join(original, "transcript.jsonl"), []byte(journal), 0600); err != nil {
		t.Fatal(err)
	}
	if err := session.SaveMeta(original, session.Meta{ID: "old-session", Title: "Older words", LastUserAt: time.Now(), Workspace: work}); err != nil {
		t.Fatal(err)
	}
	c, err := copyOlderChat(t.Context(), original)
	if err != nil {
		t.Fatal(err)
	}
	head, err := cellstore.Head(c)
	if err != nil || head == nil {
		t.Fatalf("copy has no saved first turn: %v", err)
	}
	if err := os.WriteFile(filepath.Join(work, "hello.txt"), []byte("later"), 0600); err != nil {
		t.Fatal(err)
	}
	engine := cellstore.EngineFor(work)
	if _, err := engine.Seal(t.Context(), c, cellstore.TurnInfo{}); err != nil {
		t.Fatal(err)
	}
	if _, err := engine.Rewind(t.Context(), c, head.Turn.ID); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(work, "hello.txt"))
	if err != nil || string(data) != "at copy time" {
		t.Fatalf("rewind lost first folder state: %s %v", data, err)
	}
	data, err = os.ReadFile(filepath.Join(c.Root, cell.TranscriptPath))
	if err != nil || !strings.Contains(string(data), "my whole earlier conversation") {
		t.Fatal("rewind lost the earlier conversation")
	}
	data, err = os.ReadFile(filepath.Join(original, "transcript.jsonl"))
	if err != nil || string(data) != journal {
		t.Fatal("using copy changed original")
	}
	// Retire only this fixture's engine routes before TempDir cleanup.
	_ = cellstore.Await(context.Background(), c)
}

func newShortOlderChatHome(t *testing.T) string {
	t.Helper()
	dir, err := os.MkdirTemp("", "cf29-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		sockets, _ := filepath.Glob(filepath.Join(dir, "v3", "engine-*.sock"))
		for _, socket := range sockets {
			_ = (cellstore.Daemon{Socket: socket}).Stop(context.Background())
		}
		_ = os.RemoveAll(dir)
	})
	return dir
}

// TestCopiedLegacyChatKeepsCopyReadyAfterRewind verifies from the person's side
// that copying a legacy chat into `.moving` and rewinding to an earlier turn
// preserves `.cell/copy-ready` (along with local `.cell/` bookkeeping such as
// `copy-offered`, `published.json`, and `adopted.json`) and keeps Home listing
// the `.moving` copy rather than the stale original.
func TestCopiedLegacyChatKeepsCopyReadyAfterRewind(t *testing.T) {
	celltest.RequireEngine(t)
	t.Cleanup(cellstore.StopSpawned)

	homeDir := newShortOlderChatHome(t)
	t.Setenv(home.EnvVar, homeDir)
	t.Setenv("CODEAF_PROFILE_DIR", homeDir)
	if _, err := identity.Ensure(homeDir); err != nil {
		t.Fatal(err)
	}
	t.Setenv(syncsetup.URLVar, "http://127.0.0.1:1")

	work := t.TempDir()
	if err := os.WriteFile(filepath.Join(work, "readme.txt"), []byte("turn one"), 0o600); err != nil {
		t.Fatal(err)
	}
	bucket, err := v3ProjectDir(work)
	if err != nil {
		t.Fatal(err)
	}
	original := filepath.Join(bucket, "legacy-rewind-chat")
	if err := os.MkdirAll(original, 0o700); err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC().Truncate(time.Second)
	t1Stamp := now.Add(-10 * time.Minute).Format(time.RFC3339)
	t2Stamp := now.Add(-1 * time.Minute).Format(time.RFC3339)
	origTranscript := fmt.Sprintf(
		"{\"type\":\"session\",\"version\":1,\"id\":\"legacy-rewind-chat\",\"timestamp\":%q}\n"+
			"{\"type\":\"message\",\"role\":\"user\",\"content\":\"turn one before copy\",\"timestamp\":%q}\n"+
			"{\"type\":\"message\",\"role\":\"assistant\",\"content\":\"reply one before copy\",\"timestamp\":%q}\n",
		t1Stamp, t1Stamp, t1Stamp,
	)
	if err := os.WriteFile(filepath.Join(original, "transcript.jsonl"), []byte(origTranscript), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := session.SaveMeta(original, session.Meta{
		ID:         "legacy-rewind-chat",
		Title:      "Stale Pre-Copy Original",
		Workspace:  work,
		Created:    now.Add(-10 * time.Minute),
		LastUserAt: now.Add(-10 * time.Minute),
	}); err != nil {
		t.Fatal(err)
	}

	copied, err := copyOlderChat(t.Context(), original)
	if err != nil {
		t.Fatalf("copyOlderChat: %v", err)
	}
	defer func() { _ = cellstore.Await(context.Background(), copied) }()

	firstHead, err := cellstore.Head(copied)
	if err != nil || firstHead == nil {
		t.Fatalf("Head after copyOlderChat: %v", err)
	}

	// Update the `.moving` copy with a second turn and local `.cell/` bookkeeping.
	copyTranscriptPath := filepath.Join(copied.Root, filepath.FromSlash(cell.TranscriptPath))
	turnTwoTranscript := fmt.Sprintf(
		"{\"type\":\"session\",\"version\":1,\"id\":%q,\"timestamp\":%q}\n"+
			"{\"type\":\"message\",\"role\":\"user\",\"content\":\"turn one before copy\",\"timestamp\":%q}\n"+
			"{\"type\":\"message\",\"role\":\"assistant\",\"content\":\"reply one before copy\",\"timestamp\":%q}\n"+
			"{\"type\":\"message\",\"role\":\"user\",\"content\":\"turn two in copy\",\"timestamp\":%q}\n"+
			"{\"type\":\"message\",\"role\":\"assistant\",\"content\":\"reply two in copy\",\"timestamp\":%q}\n",
		copied.ID, t1Stamp, t1Stamp, t1Stamp, t2Stamp, t2Stamp,
	)
	if err := os.WriteFile(filepath.Join(work, "readme.txt"), []byte("turn two"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(copyTranscriptPath, []byte(turnTwoTranscript), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := session.SaveMeta(copied.Root, session.Meta{
		ID:         copied.ID,
		Title:      "Live Moving Copy",
		Workspace:  work,
		Created:    now.Add(-10 * time.Minute),
		LastUserAt: now.Add(-1 * time.Minute),
	}); err != nil {
		t.Fatal(err)
	}
	engine := cellstore.EngineFor(work)
	secondHead, err := engine.Seal(t.Context(), copied, cellstore.TurnInfo{})
	if err != nil {
		t.Fatalf("Seal turn 2: %v", err)
	}
	if err := cellstore.Await(t.Context(), copied); err != nil {
		t.Fatalf("Await turn 2: %v", err)
	}
	if err := cellstore.NotePublished(copied, secondHead.Turn.ID); err != nil {
		t.Fatalf("NotePublished: %v", err)
	}
	if err := os.WriteFile(filepath.Join(copied.Root, filepath.FromSlash(cell.CopyOfferedPath)), nil, 0o600); err != nil {
		t.Fatal(err)
	}
	adoptedPath := filepath.Join(copied.Root, ".cell", "adopted.json")
	if err := os.WriteFile(adoptedPath, []byte(fmt.Sprintf(`{"V":1,"head":%q,"turns_size":1,"transcript_end":1,"from":"laptop"}`+"\n", firstHead.Turn.ID)), 0o600); err != nil {
		t.Fatal(err)
	}

	// Rewind to the first turn (whose snapshot predates `copy-ready`, `copy-offered`, `published.json`, and `adopted.json`).
	if _, err := engine.Rewind(t.Context(), copied, firstHead.Turn.ID); err != nil {
		t.Fatalf("Rewind: %v", err)
	}

	copyReadyFile := filepath.Join(copied.Root, ".cell", "copy-ready")
	if _, err := os.Stat(copyReadyFile); err != nil {
		t.Fatalf("rewind removed .cell/copy-ready: %v", err)
	}
	if _, err := os.Stat(filepath.Join(copied.Root, filepath.FromSlash(cell.CopyOfferedPath))); err != nil {
		t.Fatalf("rewind removed .cell/copy-offered: %v", err)
	}
	if shared, err := cellstore.Shared(copied); err != nil || !shared {
		t.Fatalf("rewind lost local publication/adoption bookkeeping: shared=%v err=%v", shared, err)
	}
	rawPublished, err := os.ReadFile(filepath.Join(copied.Root, ".cell", "published.json"))
	if err != nil || !strings.Contains(string(rawPublished), secondHead.Turn.ID) {
		t.Fatalf("rewind altered .cell/published.json: err=%v body=%q", err, string(rawPublished))
	}
	if _, err := os.Stat(adoptedPath); err != nil {
		t.Fatalf("rewind removed .cell/adopted.json: %v", err)
	}
	if got := cell.Copied(original); filepath.Clean(got) != filepath.Clean(copied.Root) {
		t.Fatalf("cell.Copied(original) after rewind = %s, want %s", got, copied.Root)
	}
	rewoundTranscript, err := os.ReadFile(copyTranscriptPath)
	if err != nil || !strings.Contains(string(rewoundTranscript), "turn one before copy") || strings.Contains(string(rewoundTranscript), "turn two in copy") {
		t.Fatalf("rewound transcript = %q, err = %v", string(rewoundTranscript), err)
	}

	t.Setenv(syncsetup.URLVar, "off")
	rows := session.ReadHome().Sessions()
	if len(rows) != 1 {
		t.Fatalf("ReadHome() after rewind returned %d chats, want 1: %+v", len(rows), rows)
	}
	if rows[0].ID != copied.ID || filepath.Clean(session.DirOf(rows[0].Transcript)) != filepath.Clean(copied.Root) {
		t.Fatalf("Home listed stale original instead of rewound copy: got id=%q dir=%q, want id=%q dir=%q",
			rows[0].ID, session.DirOf(rows[0].Transcript), copied.ID, copied.Root)
	}
}

// TestCopiedLegacyChatKeepsCopyReadyAfterTakeBack verifies from the person's
// side that copying a legacy chat into `.moving`, continuing it on another
// computer, and taking it back onto this computer's `.moving` copy root keeps
// `.cell/copy-ready` and lists the updated copy (never the stale original) on
// Home — both for borrowed-project (`InPlace`) and owned (`swap`) chats.
func TestCopiedLegacyChatKeepsCopyReadyAfterTakeBack(t *testing.T) {
	celltest.RequireEngine(t)
	t.Cleanup(cellstore.StopSpawned)

	for _, tc := range []struct {
		name  string
		owned bool
	}{
		{name: "borrowed_project", owned: false},
		{name: "owned_workspace", owned: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx := t.Context()
			srv := httptest.NewServer(relayserveForMove(t))
			t.Cleanup(srv.Close)

			home1 := newShortOlderChatHome(t)
			home2 := newShortOlderChatHome(t)
			t.Setenv(syncsetup.URLVar, srv.URL)
			t.Setenv(home.EnvVar, home1)
			t.Setenv("CODEAF_PROFILE_DIR", home1)

			id, err := identity.Ensure(home1)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := identity.Adopt(home2, id, false); err != nil {
				t.Fatal(err)
			}
			sync1, ok, err := syncsetup.Open(home1)
			if err != nil || !ok {
				t.Fatalf("Open(home1) = (%v, %v)", ok, err)
			}
			sync2, ok, err := syncsetup.Open(home2)
			if err != nil || !ok {
				t.Fatalf("Open(home2) = (%v, %v)", ok, err)
			}
			if err := syncsetup.PutOwnDevice(ctx, sync1.Dir, sync1.Identity, sync1.Device, "desk"); err != nil {
				t.Fatal(err)
			}
			if err := syncsetup.PutOwnDevice(ctx, sync2.Dir, sync2.Identity, sync2.Device, "laptop"); err != nil {
				t.Fatal(err)
			}

			projectWork := t.TempDir()
			bucket, err := v3ProjectDir(projectWork)
			if err != nil {
				t.Fatal(err)
			}
			original := filepath.Join(bucket, "legacy-takeback-orig")
			origWork := projectWork
			if tc.owned {
				origWork = filepath.Join(original, "work")
			}
			if err := os.MkdirAll(origWork, 0o700); err != nil {
				t.Fatal(err)
			}
			if err := os.MkdirAll(original, 0o700); err != nil {
				t.Fatal(err)
			}
			now := time.Now().UTC().Truncate(time.Second)
			deskStamp := now.Add(-15 * time.Minute).Format(time.RFC3339)
			laptopStamp := now.Add(-1 * time.Minute).Format(time.RFC3339)
			origJournal := fmt.Sprintf(
				"{\"type\":\"session\",\"version\":1,\"id\":\"legacy-takeback-orig\",\"timestamp\":%q}\n"+
					"{\"type\":\"message\",\"role\":\"user\",\"content\":\"started on desk before copy\",\"timestamp\":%q}\n"+
					"{\"type\":\"message\",\"role\":\"assistant\",\"content\":\"desk initial answer\",\"timestamp\":%q}\n",
				deskStamp, deskStamp, deskStamp,
			)
			if err := os.WriteFile(filepath.Join(original, "transcript.jsonl"), []byte(origJournal), 0o600); err != nil {
				t.Fatal(err)
			}
			if err := session.SaveMeta(original, session.Meta{
				ID:         "legacy-takeback-orig",
				Title:      "Stale Original On Desk",
				Workspace:  origWork,
				Owned:      tc.owned,
				Created:    now.Add(-15 * time.Minute),
				LastUserAt: now.Add(-15 * time.Minute),
			}); err != nil {
				t.Fatal(err)
			}

			// 1. Copy the legacy chat into `.moving` on Computer 1 and publish it.
			copied, err := copyOlderChat(ctx, original)
			if err != nil {
				t.Fatalf("copyOlderChat: %v", err)
			}
			copiedMeta, err := session.LoadMeta(copied.Root)
			if err != nil {
				t.Fatal(err)
			}
			copyWork1 := copiedMeta.Workspace
			if copiedMeta.Owned {
				copyWork1 = filepath.Join(copied.Root, "work")
			}
			engine1 := cellstore.EngineFor(copyWork1)
			engine1.DataRoot = filepath.Join(home1, "v3", "stores")
			if err := sync1.PublishCopy(ctx, engine1, copied, "desk", "Copied Chat From Desk"); err != nil {
				t.Fatalf("PublishCopy: %v", err)
			}
			if err := os.WriteFile(filepath.Join(copied.Root, filepath.FromSlash(cell.CopyOfferedPath)), nil, 0o600); err != nil {
				t.Fatal(err)
			}

			// 2. Continue the chat on Computer 2 (`home2`), add a turn, and publish it.
			t.Setenv(home.EnvVar, home2)
			t.Setenv("CODEAF_PROFILE_DIR", home2)
			awayPath := projectWork + ".away"
			if !tc.owned {
				if err := os.Rename(projectWork, awayPath); err != nil {
					t.Fatal(err)
				}
			}
			restoreProjectWork := func() {
				if !tc.owned {
					if _, err := os.Stat(awayPath); err == nil {
						_ = os.Rename(awayPath, projectWork)
					}
				}
			}
			defer restoreProjectWork()

			engine2 := cellstore.EngineFor("")
			engine2.DataRoot = filepath.Join(home2, "v3", "stores")
			cont2 := sync2.Continuer(engine2, syncsetup.TakeOptions{
				DeviceName: "laptop",
				RootFor:    func(cid string) string { return filepath.Join(home2, "v3", "projects", "copies", cid) },
				BucketFor:  func(string) string { return filepath.Join(home2, "v3", "projects", "copy--project") },
			})
			on2, err := cont2.Take(ctx, copied.ID)
			if err != nil {
				t.Fatalf("Computer 2 Take: %v", err)
			}
			cell2, err := cell.OpenAt(on2.Taken.Cell.Root, copied.ID)
			if err != nil {
				t.Fatal(err)
			}
			transcript2 := session.Place{Dir: cell2.Root}.Transcript()
			f2, err := os.OpenFile(transcript2, os.O_APPEND|os.O_WRONLY, 0o600)
			if err != nil {
				t.Fatal(err)
			}
			_, err = fmt.Fprintf(f2,
				"{\"type\":\"message\",\"role\":\"user\",\"content\":\"question on laptop\",\"timestamp\":%q}\n"+
					"{\"type\":\"message\",\"role\":\"assistant\",\"content\":\"turn added on laptop\",\"timestamp\":%q}\n",
				laptopStamp, laptopStamp,
			)
			_ = f2.Close()
			if err != nil {
				t.Fatal(err)
			}
			meta2, err := session.LoadMeta(cell2.Root)
			if err != nil {
				t.Fatal(err)
			}
			meta2.Title = "Continued On Laptop"
			meta2.LastUserAt = now.Add(-1 * time.Minute)
			if err := session.SaveMeta(cell2.Root, meta2); err != nil {
				t.Fatal(err)
			}
			work2 := meta2.Workspace
			if meta2.Owned {
				work2 = filepath.Join(cell2.Root, "work")
			}
			driveEngine2 := engine2
			driveEngine2.Workspace = work2
			title2 := meta2.Title
			drive2, err := sync2.Drive(ctx, driveEngine2, cell2, syncsetup.DriveOptions{
				DeviceName: "laptop",
				Title:      func() string { return title2 },
			})
			if err != nil {
				t.Fatalf("Computer 2 Drive: %v", err)
			}
			if _, err := drive2.Store(driveEngine2).Seal(ctx, cell2, cellstore.TurnInfo{}); err != nil {
				_ = drive2.Close(ctx)
				t.Fatalf("Computer 2 Seal: %v", err)
			}
			drive2.Idle()
			if err := drive2.Close(ctx); err != nil {
				t.Fatalf("Computer 2 Drive.Close: %v", err)
			}

			// 3. Back on Computer 1 (`home1`), take the chat back onto `<bucket>/.moving/<hash>`.
			restoreProjectWork()
			t.Setenv(home.EnvVar, home1)
			t.Setenv("CODEAF_PROFILE_DIR", home1)
			if gotRoot := takeRootFor(copied.ID); filepath.Clean(gotRoot) != filepath.Clean(copied.Root) {
				t.Fatalf("takeRootFor(%s) = %s, want %s", copied.ID, gotRoot, copied.Root)
			}
			takeBackEngine1 := cellstore.EngineFor("")
			takeBackEngine1.DataRoot = filepath.Join(home1, "v3", "stores")
			cont1 := sync1.Continuer(takeBackEngine1, syncsetup.TakeOptions{
				DeviceName: "desk",
				RootFor:    takeRootFor,
				BucketFor:  takeCopyBucket,
			})
			back1, err := cont1.Take(ctx, copied.ID)
			if err != nil {
				t.Fatalf("Computer 1 Take back: %v", err)
			}
			if filepath.Clean(back1.Taken.Cell.Root) != filepath.Clean(copied.Root) {
				t.Fatalf("Take back landed at %s, want %s", back1.Taken.Cell.Root, copied.Root)
			}

			if _, err := os.Stat(filepath.Join(copied.Root, ".cell", "copy-ready")); err != nil {
				t.Fatalf("take-back lost .cell/copy-ready: %v", err)
			}
			if got := cell.Copied(original); filepath.Clean(got) != filepath.Clean(copied.Root) {
				t.Fatalf("cell.Copied(original) after take-back = %s, want %s", got, copied.Root)
			}

			// Verify Home (including with sync turned off) lists the updated `.moving` copy once, not the stale original.
			t.Setenv(syncsetup.URLVar, "off")
			rows := session.ReadHome().Sessions()
			if len(rows) != 1 {
				t.Fatalf("ReadHome() after take-back returned %d chats, want 1: %+v", len(rows), rows)
			}
			if rows[0].ID != copied.ID || filepath.Clean(session.DirOf(rows[0].Transcript)) != filepath.Clean(copied.Root) {
				t.Fatalf("Home listed stale original instead of taken-back copy: got {ID:%q Transcript:%q}, want {ID:%q Root:%q}",
					rows[0].ID, rows[0].Transcript, copied.ID, copied.Root)
			}
			homeRowBody, err := os.ReadFile(rows[0].Transcript)
			if err != nil || !strings.Contains(string(homeRowBody), "turn added on laptop") {
				t.Fatalf("Home row transcript missing laptop turn: err=%v body=%s", err, string(homeRowBody))
			}

			// Opening the stale original path resolves to the taken-back copy without losing Computer 2's turn.
			opened, err := movablePlace(ctx, session.Place{Dir: original, Workspace: origWork, Owned: tc.owned})
			if err != nil {
				t.Fatalf("movablePlace(original) after take-back: %v", err)
			}
			if filepath.Clean(opened.Dir) != filepath.Clean(copied.Root) {
				t.Fatalf("movablePlace(original) = %s, want %s", opened.Dir, copied.Root)
			}
			rawTranscript, err := os.ReadFile(opened.Transcript())
			if err != nil || !strings.Contains(string(rawTranscript), "turn added on laptop") {
				t.Fatalf("taken-back copy transcript lost laptop turn: err=%v body=%s", err, string(rawTranscript))
			}
		})
	}
}

// TestOpeningStaleOriginalNeverDeletesPopulatedCopy verifies the second line of
// defence from the person's side: even if `.cell/copy-ready` was missing on a
// `.moving` copy that already holds sealed turns, opening the stale original
// never wipes the populated copy.
func TestOpeningStaleOriginalNeverDeletesPopulatedCopy(t *testing.T) {
	celltest.RequireEngine(t)
	t.Cleanup(cellstore.StopSpawned)

	homeDir := newShortOlderChatHome(t)
	t.Setenv(home.EnvVar, homeDir)
	t.Setenv("CODEAF_PROFILE_DIR", homeDir)
	if _, err := identity.Ensure(homeDir); err != nil {
		t.Fatal(err)
	}
	t.Setenv(syncsetup.URLVar, "http://127.0.0.1:1")

	work := t.TempDir()
	if err := os.WriteFile(filepath.Join(work, "readme.txt"), []byte("turn one"), 0o600); err != nil {
		t.Fatal(err)
	}
	bucket, err := v3ProjectDir(work)
	if err != nil {
		t.Fatal(err)
	}
	original := filepath.Join(bucket, "legacy-never-wipe")
	if err := os.MkdirAll(original, 0o700); err != nil {
		t.Fatal(err)
	}
	origTranscript := `{"type":"session","version":1,"id":"legacy-never-wipe"}` + "\n" +
		`{"type":"message","role":"user","content":"pre-copy words"}` + "\n"
	if err := os.WriteFile(filepath.Join(original, "transcript.jsonl"), []byte(origTranscript), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := session.SaveMeta(original, session.Meta{
		ID:         "legacy-never-wipe",
		Title:      "Stale Original",
		Workspace:  work,
		LastUserAt: time.Now(),
	}); err != nil {
		t.Fatal(err)
	}

	copied, err := copyOlderChat(t.Context(), original)
	if err != nil {
		t.Fatalf("copyOlderChat: %v", err)
	}
	defer func() { _ = cellstore.Await(context.Background(), copied) }()

	// Add a precious new turn to the `.moving` copy and seal it.
	copyTranscriptPath := filepath.Join(copied.Root, filepath.FromSlash(cell.TranscriptPath))
	preciousTranscript := fmt.Sprintf(
		"{\"type\":\"session\",\"version\":1,\"id\":%q}\n"+
			"{\"type\":\"message\",\"role\":\"user\",\"content\":\"pre-copy words\"}\n"+
			"{\"type\":\"message\",\"role\":\"user\",\"content\":\"precious turn only in moving copy\"}\n"+
			"{\"type\":\"message\",\"role\":\"assistant\",\"content\":\"precious answer that must never be wiped\"}\n",
		copied.ID,
	)
	if err := os.WriteFile(copyTranscriptPath, []byte(preciousTranscript), 0o600); err != nil {
		t.Fatal(err)
	}
	engine := cellstore.EngineFor(work)
	if _, err := engine.Seal(t.Context(), copied, cellstore.TurnInfo{}); err != nil {
		t.Fatalf("Seal precious turn: %v", err)
	}

	// Simulate an installation where an earlier build already stripped `.cell/copy-ready`.
	copyReadyFile := filepath.Join(copied.Root, ".cell", "copy-ready")
	if err := os.Remove(copyReadyFile); err != nil {
		t.Fatal(err)
	}
	if cell.Copied(original) != original {
		t.Fatal("expected Copied(original) == original once copy-ready was removed")
	}

	// Opening the stale original must NEVER os.RemoveAll(copied.Root) or wipe the precious turn.
	opened, err := movablePlace(t.Context(), session.Place{Dir: original, Workspace: work})
	if err != nil {
		t.Fatalf("movablePlace(original) failed: %v", err)
	}
	if filepath.Clean(opened.Dir) != filepath.Clean(copied.Root) {
		t.Fatalf("movablePlace(original) = %s, want %s", opened.Dir, copied.Root)
	}
	afterRaw, err := os.ReadFile(copyTranscriptPath)
	if err != nil {
		t.Fatalf("copy transcript unreadable: %v", err)
	}
	if !strings.Contains(string(afterRaw), "precious answer that must never be wiped") {
		t.Fatalf("opening stale original wiped the populated copy! transcript:\n%s", string(afterRaw))
	}
	if _, err := os.Stat(copyReadyFile); err != nil {
		t.Fatalf("expected CopyLegacy to restore .cell/copy-ready on populated copy: %v", err)
	}
}
