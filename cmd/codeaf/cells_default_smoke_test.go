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
	"testing"
	"time"
)

// TestC26NewChatWithoutOptInHasNoCellOrStore proves C26 through the binary's
// chat door after a model turn, with both development and stable release stamps.
func TestC26NewChatWithoutOptInHasNoCellOrStore(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Path, "/models") {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"data":[]}`))
			return
		}
		if !strings.HasSuffix(r.URL.Path, "/chat/completions") {
			t.Errorf("unexpected service request with cells off: %s %s", r.Method, r.URL.Path)
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte("data: {\"choices\":[{\"delta\":{\"content\":\"" + smokeAnswer + "\"},\"finish_reason\":null}]}\n\n" +
			"data: {\"choices\":[{\"delta\":{},\"finish_reason\":\"stop\"}]}\n\ndata: [DONE]\n\n"))
	}))
	t.Cleanup(server.Close)
	for _, stamp := range []string{"dev-20261022-0123456789ab", "v0.9.0"} {
		t.Run(stamp, func(t *testing.T) {
			binary := buildCodeafStamped(t, stamp)
			environment, home := smokeEnv(t, server.URL)
			environment = append(environment, "CODEAF_NO_AUTOMATIONS=1", "CODEAF_SYNC_URL="+server.URL)
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
			var transcripts []string
			err := filepath.WalkDir(home, func(path string, entry fs.DirEntry, err error) error {
				if err != nil {
					return err
				}
				if entry.IsDir() && entry.Name() == ".cell" {
					t.Errorf("chat made a cell without opting in: %s", path)
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
			entries, err := os.ReadDir(filepath.Join(home, "v3", "stores"))
			if len(entries) != 0 || (err != nil && !os.IsNotExist(err)) {
				t.Fatalf("chat made a store without opting in: %v, %v", entries, err)
			}
			if _, err := os.Stat(filepath.Join(home, "identity.json")); !os.IsNotExist(err) {
				t.Errorf("chat made an identity without opting in: %v", err)
			}
			sockets, err := filepath.Glob(filepath.Join(home, "v3", "engine-*.sock"))
			if err != nil || len(sockets) != 0 {
				t.Errorf("chat started an engine without opting in: %v, %v", sockets, err)
			}
		})
	}
}
