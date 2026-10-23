package main

import (
	"context"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/cell"
	"github.com/Agent-Field/codeaf/internal/cell/celltest"
	"github.com/Agent-Field/codeaf/internal/cellstore"
)

// TestC27NewChatByDefaultStaysQuiet proves C27.1 and C23 through the binary
// --once door on every channel, with a logging relay and a model turn.
// Its C27.4 assertions cover this door; interactive startup wires sync separately.
func TestC27NewChatByDefaultStaysQuiet(t *testing.T) {
	celltest.RequireEngine(t)
	var relayRequests atomic.Int64
	relay := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		relayRequests.Add(1)
		http.Error(w, "unpaired home contacted relay", http.StatusForbidden)
	}))
	defer relay.Close()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Path, "/models") {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"data":[]}`))
			return
		}
		if !strings.HasSuffix(r.URL.Path, "/chat/completions") {
			t.Errorf("unexpected service request on an unpaired home: %s %s", r.Method, r.URL.Path)
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte("data: {\"choices\":[{\"delta\":{\"content\":\"" + smokeAnswer + "\"},\"finish_reason\":null}]}\n\n" +
			"data: {\"choices\":[{\"delta\":{},\"finish_reason\":\"stop\"}]}\n\ndata: [DONE]\n\n"))
	}))
	t.Cleanup(server.Close)
	for _, stamp := range []string{"", "source-0123456789ab", "dev-20261022-0123456789ab", "staging-20261022-0123456789ab", "v0.9.0-rc.1", "v0.9.0"} {
		t.Run(stamp, func(t *testing.T) {
			binary := buildCodeafStamped(t, stamp)
			environment, _ := smokeEnv(t, server.URL)
			// C27.1 now reaches the real daemon, whose Unix socket needs a
			// short home independently of Go's descriptive subtest directory.
			home, err := os.MkdirTemp("", "c27")
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() {
				sockets, _ := filepath.Glob(filepath.Join(home, "v3", "engine-*.sock"))
				for _, socket := range sockets {
					_ = (cellstore.Daemon{Socket: socket}).Stop(context.Background())
				}
				_ = os.RemoveAll(home)
			})
			environment = append(environment, "HOME="+home, "CODEAF_HOME="+home, "CODEAF_PROFILE_DIR="+home)
			environment = append(environment, "CODEAF_NO_AUTOMATIONS=1", "CODEAF_SYNC_URL="+relay.URL, "CODEAF_FURROW="+os.Getenv("CODEAF_FURROW"))
			workspace := t.TempDir()
			// A minimal empty repository makes this a project chat without running
			// git mutations; no committed history is needed to mint a conversation.
			for _, dir := range []string{"objects", "refs/heads"} {
				if err := os.MkdirAll(filepath.Join(workspace, ".git", dir), 0o700); err != nil {
					t.Fatal(err)
				}
			}
			if err := os.WriteFile(filepath.Join(workspace, ".git", "HEAD"), []byte("ref: refs/heads/main\n"), 0o600); err != nil {
				t.Fatal(err)
			}
			ctx, cancel := context.WithTimeout(t.Context(), time.Minute)
			defer cancel()
			cmd := exec.CommandContext(ctx, binary, "chat", "--once", "Say ok.", "--one-model")
			cmd.Env, cmd.Dir = environment, workspace
			if out, err := cmd.CombinedOutput(); err != nil || !strings.Contains(string(out), smokeAnswer) {
				t.Fatalf("new chat: %v\n%s", err, out)
			}
			var transcripts, cells []string
			err = filepath.WalkDir(home, func(path string, entry fs.DirEntry, err error) error {
				if err != nil {
					return err
				}
				if entry.IsDir() && entry.Name() == ".cell" {
					cells = append(cells, filepath.Dir(path))
				}
				if entry.Name() == "transcript.jsonl" {
					transcripts = append(transcripts, path)
				}
				return nil
			})
			if err != nil || len(transcripts) != 1 {
				t.Fatalf("chat transcripts = %v, %v; want one", transcripts, err)
			}
			raw, err := os.ReadFile(transcripts[0])
			if err != nil || !strings.Contains(string(raw), smokeAnswer) {
				t.Fatalf("chat did not persist the model turn: %s, %v", raw, err)
			}
			if len(cells) != 1 {
				t.Fatalf("cells = %v, want one", cells)
			}
			c, err := cell.OpenAt(cells[0], filepath.Base(cells[0]))
			if err != nil {
				t.Fatal(err)
			}
			if turns, err := os.ReadFile(filepath.Join(c.Root, cellstore.TurnsPath)); err != nil || len(turns) == 0 {
				t.Fatalf("chat did not seal its first turn: %s, %v", turns, err)
			}
			if c.Meta().Class != cell.HostBound {
				t.Fatalf("class = %s, want host-bound", c.Meta().Class)
			}
			if !strings.Contains(transcripts[0], string(filepath.Separator)+".cell"+string(filepath.Separator)) {
				t.Fatalf("transcript is not in cell: %s", transcripts[0])
			}
			if got := relayRequests.Load(); got != 0 {
				t.Fatalf("unpaired home sent %d relay requests", got)
			}

			if _, err := os.Stat(filepath.Join(home, "identity.json")); !os.IsNotExist(err) {
				t.Errorf("unpaired chat made an identity: %v", err)
			}
		})
	}
}
