//go:build e2e

package e2e

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/config"
	"github.com/Agent-Field/codeaf/internal/session"
)

// Exercise the real history screen with many project buckets, so each bucket
// stays within the existing index read limit. No model work belongs in a hover.
func TestActivityScaleTerminalLatency(t *testing.T) {
	if _, err := exec.LookPath("tmux"); err != nil {
		t.Skip("tmux required")
	}
	began := time.Now()
	home := newHome(t, map[string]any{"model_pool": "off", "memory.enabled": false})
	ws := newWorkspace(t, "activity-scale", false)
	now := time.Now().UTC()
	for chat := 0; chat < 40; chat++ {
		workspace := filepath.Join(ws, fmt.Sprintf("project-%03d", chat))
		if err := os.MkdirAll(workspace, 0o700); err != nil {
			t.Fatal(err)
		}
		sid := fmt.Sprintf("%016x", 0x7100000000000000+chat)
		bucket := filepath.Join(home, "v3", "projects", strings.ReplaceAll(workspace, string(filepath.Separator), "-"))
		dir := filepath.Join(bucket, sid)
		if err := os.MkdirAll(dir, 0o700); err != nil {
			t.Fatal(err)
		}
		title := fmt.Sprintf("Mock history chat %03d", chat)
		if chat == 39 {
			title = "Mock history zephyr"
		}
		writeJSON(t, filepath.Join(dir, "meta.json"), map[string]any{"id": sid, "title": title, "workspace": workspace, "created": now, "lastUserAt": now.Add(-time.Duration(chat) * time.Minute)})
		journal := fmt.Sprintf("{\"type\":\"session\",\"version\":1,\"id\":%q,\"cwd\":%q}\n", sid, workspace)
		if err := os.WriteFile(filepath.Join(dir, "transcript.jsonl"), []byte(journal), 0o600); err != nil {
			t.Fatal(err)
		}
		var rows strings.Builder
		for task := 1; task <= 128; task++ {
			parent := ""
			if task > 1 {
				parent = fmt.Sprint((task-2)/4 + 1)
			}
			entry := session.TaskIndexEntry{SessionID: sid, ID: fmt.Sprint(task), Parent: parent,
				Title: fmt.Sprintf("Mock parser repair %03d %03d", chat, task), Status: string(session.TaskDone), EndedAt: now.Add(-time.Duration(task) * time.Minute)}
			data, err := json.Marshal(entry)
			if err != nil {
				t.Fatal(err)
			}
			rows.Write(data)
			rows.WriteByte('\n')
		}
		if err := os.WriteFile(filepath.Join(bucket, "tasks.jsonl"), []byte(rows.String()), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	setup := time.Since(began)
	var calls atomic.Int64
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if strings.HasSuffix(r.URL.Path, "/chat/completions") {
			calls.Add(1)
			http.Error(w, "navigation must not call a model", 500)
			return
		}
		fmt.Fprint(w, `{"data":[]}`)
	}))
	defer server.Close()
	r := startWithEnv(t, []string{config.APIKeyEnv + "=mock-scale-key", "CODEAF_BASE_URL=" + server.URL}, "activity_scale", home, ws, 100, 45, "chat", "--model", "deepseek/deepseek-v4.1-flash", "--one-model", "--no-host")
	r.skipSetup(t)
	send := func(text string) {
		t.Helper()
		if out, err := exec.Command("tmux", "send-keys", "-t", r.name, "-l", text).CombinedOutput(); err != nil {
			t.Fatalf("input: %v %s", err, out)
		}
	}
	r.lit("/history")
	opened := time.Now()
	if out, err := exec.Command("tmux", "send-keys", "-t", r.name, "Enter").CombinedOutput(); err != nil {
		t.Fatalf("open history: %v %s", err, out)
	}
	r.waitFor(30*time.Second, "Mock History Chat 000", "Mock parser repair 000")
	opening := time.Since(opened)
	// Select different kinds of rows and observe their distinct door hints.
	// Find each label again: the selected task can grow a reason line.
	r.keys("Down")
	r.waitFor(30*time.Second, "enter go inside it")
	targets := []struct{ label, hint string }{
		{"Mock History Chat 000", "enter go to that conversation"},
		{"Mock parser repair 000 001", "enter go inside it"},
	}
	var samples []float64
	for i := 0; i < 12; i++ {
		target := targets[i%2]
		y := -1
		for row, line := range strings.Split(r.capture(), "\n") {
			if strings.Contains(line, target.label) {
				y = row
				break
			}
		}
		if y < 0 {
			t.Fatalf("no hover target %q:\n%s", target.label, r.capture())
		}
		start := time.Now()
		send(fmt.Sprintf("\x1b[<35;12;%dM", y+1))
		changed := false
		for time.Since(start) < 30*time.Second {
			if strings.Contains(r.capture(), target.hint) {
				changed = true
				break
			}
			time.Sleep(5 * time.Millisecond)
		}
		if !changed {
			t.Fatalf("hover did not change door hint to %q:\n%s", target.hint, r.capture())
		}
		samples = append(samples, float64(time.Since(start))/float64(time.Millisecond))
	}
	// Filtering must retain the matching chat's full family; clearing restores it.
	send("zephyr")
	r.waitFor(30*time.Second, "Mock History Zephyr", "Mock parser repair 039")
	r.keys("C-u")
	r.waitFor(30*time.Second, "Mock History Chat 000", "Mock parser repair 000")
	r.keys("Right", "Down", "Left")
	r.resize(160, 55)
	r.waitFor(30*time.Second, strings.Repeat("─", 160), "Mock History Chat")
	if calls.Load() != 0 {
		t.Fatalf("navigation made %d model calls", calls.Load())
	}
	sort.Float64s(samples)
	t.Logf("chats=40 tasks=5120 setup=%s first_screen=%s hover_ms p50=%.3f p95=%.3f samples=%v model_calls=0", setup, opening, samples[len(samples)/2], samples[len(samples)-1], samples)
}
