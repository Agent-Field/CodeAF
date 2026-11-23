package main

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/cell"
	"github.com/Agent-Field/codeaf/internal/cell/celltest"
	"github.com/Agent-Field/codeaf/internal/cellstore"
	cellstoretest "github.com/Agent-Field/codeaf/internal/cellstore/celltest"
	"github.com/Agent-Field/codeaf/internal/config"
	"github.com/Agent-Field/codeaf/internal/home"
	"github.com/Agent-Field/codeaf/internal/remote"
	"github.com/Agent-Field/codeaf/internal/session"
	"github.com/Agent-Field/codeaf/internal/tui3"
)

// TestC29LocalHomeHasFiftyChatsBeforeItsWireCacheAnswers proves C29.2/C31.2:
// bare local startup can greet with older chats and its continuation offer even
// when the engine's asynchronous world read has not answered yet.
func TestC29LocalHomeHasFiftyChatsBeforeItsWireCacheAnswers(t *testing.T) {
	t.Setenv(home.EnvVar, t.TempDir())
	root := session.PlacesRoot()
	for i := range 50 {
		id := fmt.Sprintf("older-%02d", i)
		dir := filepath.Join(root, "older", id)
		if err := os.MkdirAll(dir, 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, "transcript.jsonl"), []byte(fmt.Sprintf("{\"type\":\"session\",\"version\":1,\"id\":%q}\n", id)), 0600); err != nil {
			t.Fatal(err)
		}
		if err := session.SaveMeta(dir, session.Meta{ID: id, Title: id, LastUserAt: time.Unix(1700000000, 0)}); err != nil {
			t.Fatal(err)
		}
	}
	wireReads := 0
	wireKnown := false
	options := tui3.Options{World: func() (session.World, bool) {
		wireReads++
		return session.World{}, wireKnown
	}}
	localDoors(&options, remote.Welcome{PlacesRoot: root}, config.Config{})
	world, known := options.World()
	if !known || len(world.Sessions()) != 50 || wireReads != 0 {
		t.Fatalf("first local reading known=%v, chats=%d, wire reads=%d", known, len(world.Sessions()), wireReads)
	}
	// Once the engine answers, later removal of all rows must be visible too.
	wireKnown = true
	world, known = options.World()
	if !known || len(world.Sessions()) != 0 || wireReads != 1 {
		t.Fatal("the initial local reading hid the engine's later answer")
	}
}

// TestC31AStartupSaveIsNotAChoice proves C31.2/C31.3: saving unchanged
// bookkeeping on startup is not local work, while files edited before that
// startup save must still be kept as a branch rather than silently replaced.
func TestC31AStartupSaveIsNotAChoice(t *testing.T) {
	celltest.RequireEngine(t)
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")
	t.Setenv("GIT_CONFIG_GLOBAL", filepath.Join(cellstoretest.EngineDir(t), "no-global-config"))

	for _, action := range []string{"bookkeeping", "git-status", "git-private", "git-stage", "files", "conversation", "tool"} {
		t.Run(action, func(t *testing.T) {
			t.Setenv(home.EnvVar, cellstoretest.EngineDir(t))
			work := t.TempDir()
			file := filepath.Join(work, "work.txt")
			if err := os.WriteFile(file, []byte("already shared"), 0600); err != nil {
				t.Fatal(err)
			}
			c, err := cell.CreateIn(home.Join("v3/projects/fixture"), cell.Options{Class: cell.HostBound})
			if err != nil {
				t.Fatal(err)
			}
			// The grouped durability worker owns the daemon until this cell's
			// records and captured files have reached their closing boundary.
			t.Cleanup(func() {
				ctx, cancel := context.WithTimeout(context.Background(), cellstore.SealWithin)
				defer cancel()
				if err := cellstore.Await(ctx, c); err != nil {
					t.Error(err)
				}
			})
			if err := session.SaveMeta(c.Root, session.Meta{ID: c.ID, Workspace: work}); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(c.Root, cell.TranscriptPath), []byte(fmt.Sprintf("{\"type\":\"session\",\"version\":1,\"id\":%q}\n", c.ID)), 0600); err != nil {
				t.Fatal(err)
			}
			git := func(args ...string) string {
				t.Helper()
				command := exec.Command("git", args...)
				command.Dir = work
				command.Env = append(os.Environ(), "GIT_CONFIG_NOSYSTEM=1", "GIT_CONFIG_GLOBAL="+filepath.Join(work, "no-global-config"))
				raw, err := command.CombinedOutput()
				if err != nil {
					t.Fatalf("git fixture: %s %v", raw, err)
				}
				return string(raw)
			}
			if strings.HasPrefix(action, "git-") {
				git("init", "-q")
				git("config", "--local", "maintenance.auto", "false")
				git("config", "--local", "gc.auto", "0")
				git("add", "work.txt")
				git("-c", "user.name=Fixture", "-c", "user.email=fixture@example.invalid", "commit", "-qm", "initial")
				if action == "git-private" {
					if err := os.Chmod(filepath.Join(work, ".git"), 0700); err != nil {
						t.Fatal(err)
					}
				}
				if action == "git-stage" {
					if err := os.WriteFile(file, []byte("already shared but not staged"), 0600); err != nil {
						t.Fatal(err)
					}
				}
				at := time.Unix(1700000000, 0)
				if err := os.Chtimes(file, at, at); err != nil {
					t.Fatal(err)
				}
			}
			e := cellstore.EngineFor(work)
			at := time.Unix(1700000000, 0)
			e.Now = func() time.Time { at = at.Add(time.Second); return at }
			first, err := e.Seal(t.Context(), c, cellstore.TurnInfo{})
			if err != nil {
				t.Fatal(err)
			}
			if err := cellstore.NotePublished(c, first.Turn.ID); err != nil {
				t.Fatal(err)
			}
			info := cellstore.TurnInfo{}
			switch action {
			case "git-status", "git-private":
				git("status", "--porcelain=v2", "--branch")
			case "git-stage":
				git("add", "work.txt")
			case "files":
				if err := os.WriteFile(file, []byte("edited before startup"), 0600); err != nil {
					t.Fatal(err)
				}
			case "conversation":
				f, err := os.OpenFile(filepath.Join(c.Root, cell.TranscriptPath), os.O_APPEND|os.O_WRONLY, 0)
				if err != nil {
					t.Fatal(err)
				}
				_, err = f.WriteString("{\"type\":\"message\",\"role\":\"user\",\"content\":\"keep this turn\"}\n")
				_ = f.Close()
				if err != nil {
					t.Fatal(err)
				}
			case "tool":
				info.Calls = []cellstore.Executed{{Call: cellstore.Call{Tool: "bash", SideEffect: "external"}, Exact: true}}
			}
			if _, err := e.Seal(t.Context(), c, info); err != nil {
				t.Fatal(err)
			}
			indexFile := filepath.Join(work, ".git", "index")
			indexBefore, _ := os.ReadFile(indexFile)
			clean, err := (takeover{}).SafeToContinue(t.Context(), c.ID, "", false)
			if err != nil {
				t.Fatal(err)
			}
			indexAfter, _ := os.ReadFile(indexFile)
			if !bytes.Equal(indexBefore, indexAfter) {
				t.Fatal("checking continuation rewrote this computer's Git index")
			}
			if clean != (action == "bookkeeping" || action == "git-status" || action == "git-private") {
				t.Fatalf("clean=%v after %s", clean, action)
			}
		})
	}
}

// TestC31AFolderGoneIsNoLocalFiles proves the launch walk's check for a chat
// whose project folder was deleted on this computer. A folder that is not
// there holds no local files, so only the conversation can be work: the check
// answers clean when nothing was left unsaved and not clean when something was,
// and in neither case an error, which a launch used to show on every start as
// a chat that "could not be put in this folder". Folders that ARE there keep
// their edits, and a path that is not a folder is still a refusal.
func TestC31AFolderGoneIsNoLocalFiles(t *testing.T) {
	celltest.RequireEngine(t)
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")
	t.Setenv("GIT_CONFIG_GLOBAL", filepath.Join(cellstoretest.EngineDir(t), "no-global-config"))

	appendRecord := func(t *testing.T, c cell.Cell) {
		t.Helper()
		f, err := os.OpenFile(filepath.Join(c.Root, cell.TranscriptPath), os.O_APPEND|os.O_WRONLY, 0)
		if err != nil {
			t.Fatal(err)
		}
		_, err = f.WriteString("{\"type\":\"message\",\"role\":\"user\",\"content\":\"keep this turn\"}\n")
		_ = f.Close()
		if err != nil {
			t.Fatal(err)
		}
	}
	cases := []struct {
		name string
		// last is what the chat did after its shared turn, before the folder changed.
		last cellstore.TurnInfo
		// change happens after the second seal, to the chat and to its folder.
		change    func(t *testing.T, c cell.Cell, work string)
		wantClean bool
		wantErr   bool
	}{
		{name: "gone and nothing unsaved", change: func(t *testing.T, _ cell.Cell, work string) { os.RemoveAll(work) }, wantClean: true},
		{name: "gone and a turn not yet saved", change: func(t *testing.T, c cell.Cell, work string) { os.RemoveAll(work); appendRecord(t, c) }},
		{name: "gone and a tool call not yet saved", last: cellstore.TurnInfo{Calls: []cellstore.Executed{{Call: cellstore.Call{Tool: "bash", SideEffect: "external"}, Exact: true}}}, change: func(t *testing.T, _ cell.Cell, work string) { os.RemoveAll(work) }},
		{name: "gone and the saved conversation rewritten in place", change: func(t *testing.T, c cell.Cell, work string) {
			os.RemoveAll(work)
			path := filepath.Join(c.Root, cell.TranscriptPath)
			raw, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(path, bytes.Replace(raw, []byte("\"version\":1"), []byte("\"version\":2"), 1), 0600); err != nil {
				t.Fatal(err)
			}
		}},
		{name: "there and a file edited", change: func(t *testing.T, _ cell.Cell, work string) {
			if err := os.WriteFile(filepath.Join(work, "work.txt"), []byte("edited before startup"), 0600); err != nil {
				t.Fatal(err)
			}
		}},
		{name: "there and untouched", change: func(*testing.T, cell.Cell, string) {}, wantClean: true},
		{name: "a file where the folder was", change: func(t *testing.T, _ cell.Cell, work string) {
			os.RemoveAll(work)
			if err := os.WriteFile(work, []byte("not a folder"), 0600); err != nil {
				t.Fatal(err)
			}
		}, wantErr: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv(home.EnvVar, cellstoretest.EngineDir(t))
			work := t.TempDir()
			if err := os.WriteFile(filepath.Join(work, "work.txt"), []byte("already shared"), 0600); err != nil {
				t.Fatal(err)
			}
			c, err := cell.CreateIn(home.Join("v3/projects/fixture"), cell.Options{Class: cell.HostBound})
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() {
				ctx, cancel := context.WithTimeout(context.Background(), cellstore.SealWithin)
				defer cancel()
				_ = cellstore.Await(ctx, c)
			})
			if err := session.SaveMeta(c.Root, session.Meta{ID: c.ID, Workspace: work}); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(c.Root, cell.TranscriptPath), []byte(fmt.Sprintf("{\"type\":\"session\",\"version\":1,\"id\":%q}\n", c.ID)), 0600); err != nil {
				t.Fatal(err)
			}
			e := cellstore.EngineFor(work)
			at := time.Unix(1700000000, 0)
			e.Now = func() time.Time { at = at.Add(time.Second); return at }
			first, err := e.Seal(t.Context(), c, cellstore.TurnInfo{})
			if err != nil {
				t.Fatal(err)
			}
			if err := cellstore.NotePublished(c, first.Turn.ID); err != nil {
				t.Fatal(err)
			}
			if _, err := e.Seal(t.Context(), c, tc.last); err != nil {
				t.Fatal(err)
			}
			tc.change(t, c, work)

			clean, err := (takeover{}).SafeToContinue(t.Context(), c.ID, "", false)
			switch {
			case tc.wantErr && err == nil:
				t.Fatalf("clean=%v with no error; a path that is not a folder must still be refused", clean)
			case !tc.wantErr && err != nil:
				t.Fatalf("SafeToContinue = %v; want an answer, not an error", err)
			case !tc.wantErr && clean != tc.wantClean:
				t.Fatalf("clean=%v, want %v", clean, tc.wantClean)
			}
		})
	}
}
