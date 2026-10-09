//go:build !windows

package app

import (
	"context"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Agent-Field/codeaf/internal/seniordev/session/sessioncore"
)

// THE STORE IS OPENED BY ITS REAL PATH, ONCE. A benchmark rig anchored the
// store outside the folder through a link at <folder>/.senior-dev; something
// removed the link an hour into the run, and the next model turn ended it with
// `open <folder>/.senior-dev/projection.lock: no such file or directory` while
// the store it pointed at was intact. Every later open goes to the store
// itself, so the link may go away without the run noticing, and nothing is
// made again at the link's place: a fresh directory there would be a second
// store beside the one the database still writes to.
func TestDurableSurvivesTheWorkspaceLinkGoingAway(t *testing.T) {
	ctx := context.Background()
	workspace := t.TempDir()
	store := t.TempDir()
	link := filepath.Join(workspace, seniorDevDataDirectory)
	if err := os.Symlink(store, link); err != nil {
		t.Fatal(err)
	}
	durable, err := openDurableSessions(ctx, workspace)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(durable.Close)
	session, err := durable.CreateSession(ctx, sessioncore.CreateInput{Title: "anchored"})
	if err != nil {
		t.Fatal(err)
	}

	if err := os.Remove(link); err != nil {
		t.Fatal(err)
	}
	if err := durable.TouchSession(ctx, session.ID); err != nil {
		t.Fatalf("a touch after the link went away: %v", err)
	}
	if _, err := persistTurnPrompt(ctx, durable, session.ID, "msg_after_link", turn{
		Agent: "coder", ProviderID: "p", ModelID: "m", Prompt: "still here",
	}); err != nil {
		t.Fatalf("a message after the link went away: %v", err)
	}

	stored := filepath.Join(store, "storage", "message", session.ID, "msg_after_link.json")
	if _, err := os.Stat(stored); err != nil {
		t.Fatalf("the message is not in the store the link pointed at: %v", err)
	}
	var projected int
	if err := durable.db.QueryRow("SELECT COUNT(*) FROM message WHERE id = 'msg_after_link'").Scan(&projected); err != nil || projected != 1 {
		t.Fatalf("projected message = %d, %v; want 1", projected, err)
	}
	if _, err := os.Lstat(link); !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("%s was made again after the link went away (%v): a second store beside the real one", link, err)
	}
}

// A STATE DIRECTORY HOLDS THE WHOLE STORE, AND THE FOLDER NONE OF IT. Named on
// the command line or in SENIOR_DEV_STATE_DIR — the flag winning when both
// are set — the database, the flat records and the lock all live there, and
// the folder's .senior-dev is left to the files the model works with.
func TestStateDirRootsTheStoreOutsideTheWorkspace(t *testing.T) {
	for _, testCase := range []struct {
		name     string
		flagged  bool
		variable bool
	}{
		{name: "flag", flagged: true},
		{name: "variable", variable: true},
		{name: "flag over variable", flagged: true, variable: true},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			ctx := context.Background()
			workspace := t.TempDir()
			stateDir := filepath.Join(t.TempDir(), "run-1", "store")
			flagged, other := "", t.TempDir()
			t.Setenv(StateDirEnv, "")
			switch {
			case testCase.flagged && testCase.variable:
				flagged = stateDir
				t.Setenv(StateDirEnv, other)
			case testCase.flagged:
				flagged = stateDir
			case testCase.variable:
				t.Setenv(StateDirEnv, stateDir)
			}
			dir, refusal := stateDirectory(flagged, workspace)
			if refusal != "" {
				t.Fatalf("refused: %s", refusal)
			}
			if want := realDirectory(stateDir); dir != want {
				t.Fatalf("state directory = %q, want %q", dir, want)
			}
			durable, err := openDurableSessionsIn(ctx, workspace, dir)
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(durable.Close)
			session, err := durable.CreateSession(ctx, sessioncore.CreateInput{Title: "elsewhere"})
			if err != nil {
				t.Fatal(err)
			}
			if _, err := persistTurnPrompt(ctx, durable, session.ID, "msg_elsewhere", turn{
				Agent: "coder", ProviderID: "p", ModelID: "m", Prompt: "kept outside",
			}); err != nil {
				t.Fatal(err)
			}
			for _, name := range []string{seniorDevDatabaseFile, "storage", "projection.lock"} {
				if _, err := os.Stat(filepath.Join(stateDir, name)); err != nil {
					t.Errorf("%s is not in the state directory: %v", name, err)
				}
			}
			if _, err := os.Stat(filepath.Join(stateDir, "storage", "message", session.ID, "msg_elsewhere.json")); err != nil {
				t.Errorf("the conversation is not in the state directory: %v", err)
			}
			if _, err := os.Lstat(filepath.Join(workspace, seniorDevDataDirectory)); !errors.Is(err, fs.ErrNotExist) {
				t.Errorf("the folder holds a .senior-dev the store made (%v)", err)
			}
			if entries, _ := os.ReadDir(other); len(entries) != 0 {
				t.Errorf("the variable's directory holds %d entries although the flag named another", len(entries))
			}
		})
	}
}

// A STATE DIRECTORY INSIDE THE FOLDER IS REFUSED BEFORE ANYTHING IS SPENT. The
// run's checkpoints and the change it hands in are taken from the folder's
// tree, and would carry the database and the conversation with them — a
// checkpoint restored under an open database would rewrite it. The folder's
// own .senior-dev is the one place in it they already leave alone, and a link
// is judged by where it leads.
func TestAStateDirInsideTheFolderIsRefused(t *testing.T) {
	workspace := t.TempDir()
	outside := t.TempDir()
	if err := os.Mkdir(filepath.Join(workspace, "kept"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(workspace, "kept"), filepath.Join(outside, "into-the-folder")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(workspace, "out-of-the-folder")); err != nil {
		t.Fatal(err)
	}
	t.Setenv(StateDirEnv, "")
	for _, refusedDir := range []string{
		workspace,
		filepath.Join(workspace, "state"),
		filepath.Join(outside, "into-the-folder"),
	} {
		if _, refusal := stateDirectory(refusedDir, workspace); !strings.Contains(refusal, "--state-dir "+refusedDir+" is inside the folder") {
			t.Errorf("--state-dir %s: refusal = %q, want it refused as inside the folder", refusedDir, refusal)
		}
	}
	t.Setenv(StateDirEnv, filepath.Join(workspace, "state"))
	if _, refusal := stateDirectory("", workspace); !strings.HasPrefix(refusal, StateDirEnv+" ") {
		t.Errorf("refusal = %q, want it to name %s, which is where the directory came from", refusal, StateDirEnv)
	}
	t.Setenv(StateDirEnv, "")
	for _, kept := range []string{
		filepath.Join(workspace, seniorDevDataDirectory),
		filepath.Join(workspace, seniorDevDataDirectory, "store"),
		filepath.Join(workspace, "out-of-the-folder", "store"),
	} {
		if _, refusal := stateDirectory(kept, workspace); refusal != "" {
			t.Errorf("--state-dir %s was refused: %s", kept, refusal)
		}
	}
	blocker := filepath.Join(outside, "a-file")
	if err := os.WriteFile(blocker, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	if _, refusal := stateDirectory(filepath.Join(blocker, "store"), workspace); !strings.Contains(refusal, "cannot be made") {
		t.Errorf("refusal = %q, want a directory that cannot be made refused as one", refusal)
	}
}

// A STORE THAT WAS REMOVED ENDS THE RUN, NAMED BY ITS REAL PATH, AND NO OTHER
// STANDS IN FOR IT. Its records went with it; writing on into a directory made
// again in its place would let the next turn make the session again under the
// same id and hand the model a conversation that starts where the removal
// happened. In the folder's own .senior-dev the directory is made again by
// whatever writes the model's files there next, so the lock is not what can
// be trusted to notice: the store checks it is still the directory it opened.
func TestARemovedStoreEndsTheRunWithoutAFreshOne(t *testing.T) {
	ctx := context.Background()
	for _, testCase := range []struct {
		name string
		// linked keeps the store behind a link given as its state directory;
		// false keeps it in the folder's own .senior-dev.
		linked bool
		// remade is what is made where the store was, after it is removed.
		remade string
		said   string
	}{
		{name: "gone", linked: true, said: ": stat {store}: no such file or directory"},
		{name: "replaced", linked: true, remade: ".", said: "; the directory there now is a new one"},
		{name: "in the folder, remade by a write under it", remade: "tool-output", said: "; the directory there now is a new one"},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			workspace := t.TempDir()
			stateDir := ""
			if testCase.linked {
				stateDir = filepath.Join(t.TempDir(), "anchor")
				if err := os.Symlink(t.TempDir(), stateDir); err != nil {
					t.Fatal(err)
				}
			}
			durable, err := openDurableSessionsIn(ctx, workspace, stateDir)
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(durable.Close)
			session, err := durable.CreateSession(ctx, sessioncore.CreateInput{Title: "removed"})
			if err != nil {
				t.Fatal(err)
			}
			store := durable.storeDir
			if err := os.RemoveAll(store); err != nil {
				t.Fatal(err)
			}
			if testCase.remade != "" {
				if err := os.MkdirAll(filepath.Join(store, testCase.remade), 0o755); err != nil {
					t.Fatal(err)
				}
			}

			err = durable.TouchSession(ctx, session.ID)
			want := "senior-dev sessions: its store " + store + " was removed while the run was working, with the conversation in it" +
				strings.ReplaceAll(testCase.said, "{store}", store)
			if err == nil || err.Error() != want {
				t.Fatalf("touch after the store was removed = %v, want %q", err, want)
			}
			if _, err := persistTurnPrompt(ctx, durable, session.ID, "msg_amnesia", turn{
				Agent: "coder", ProviderID: "p", ModelID: "m", Prompt: "a conversation with no past",
			}); err == nil {
				t.Fatal("a message was written after the store was removed")
			}
			if count, _ := countStoredJSON(store); count != 0 {
				t.Fatalf("%d records were written where the removed store was", count)
			}
			if _, err := os.Lstat(filepath.Join(store, "projection.lock")); !errors.Is(err, fs.ErrNotExist) {
				t.Fatalf("a lock was made where the removed store was (%v)", err)
			}
		})
	}
}
