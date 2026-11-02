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
	t.Setenv("GIT_CONFIG_GLOBAL", filepath.Join(t.TempDir(), "no-global-config"))
	t.Cleanup(cellstore.StopSpawned)
	for _, action := range []string{"bookkeeping", "git-status", "git-private", "git-stage", "files", "conversation", "tool"} {
		t.Run(action, func(t *testing.T) {
			t.Setenv(home.EnvVar, t.TempDir())
			work := t.TempDir()
			t.Cleanup(cellstore.StopSpawned)
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
