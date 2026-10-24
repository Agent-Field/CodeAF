//go:build e2e

package e2e

import (
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
	teamstore "github.com/Agent-Field/codeaf/internal/teams"
)

// Screen timings include input delivery and observed terminal output, but not
// fixture setup or provider latency. No chat request is needed to move a pointer.
func TestTeamsAllScaleTerminalLatency(t *testing.T) {
	runTeamsScaleTerminal(t, 100, 100)
}

func TestTeamsAllScaleTerminalLargeCapacity(t *testing.T) {
	runTeamsScaleTerminal(t, 200, 200)
}

func runTeamsScaleTerminal(t *testing.T, count, members int) {
	if _, err := exec.LookPath("tmux"); err != nil {
		t.Skip("tmux required")
	}
	began := time.Now()
	home := newHome(t, map[string]any{"model_pool": "off", "memory.enabled": false})
	ws := newWorkspace(t, "teams-scale", false)
	var teams []teamstore.Team
	for i := 0; i < count; i++ {
		tm := teamstore.Team{ID: fmt.Sprintf("%012x", i+1), Name: fmt.Sprintf("Scale team %03d", i)}
		for j := 0; j < members; j++ {
			sid := fmt.Sprintf("%016x", 0x6000000000000000+i*members+j)
			dir := filepath.Join(home, "v3", "projects", strings.ReplaceAll(ws, string(filepath.Separator), "-"), sid)
			if err := os.MkdirAll(dir, 0o700); err != nil {
				t.Fatal(err)
			}
			at := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
			writeJSON(t, filepath.Join(dir, "meta.json"), map[string]any{"id": sid, "title": fmt.Sprintf("Mock member %03d %03d", i, j), "workspace": ws, "created": at, "lastUserAt": at})
			file := filepath.Join(dir, "transcript.jsonl")
			journal := fmt.Sprintf("{\"type\":\"session\",\"version\":1,\"id\":%q,\"cwd\":%q}\n{\"type\":\"message\",\"role\":\"user\",\"content\":\"Review this mock change\",\"timestamp\":\"2026-01-01T00:00:00Z\"}\n{\"type\":\"message\",\"role\":\"assistant\",\"content\":\"Mock change reviewed\",\"timestamp\":\"2026-01-01T00:00:01Z\"}\n", sid, ws)
			if err := os.WriteFile(file, []byte(journal), 0o600); err != nil {
				t.Fatal(err)
			}
			tm.Members = append(tm.Members, teamstore.Member{Key: file, File: file, Where: ws, Handle: fmt.Sprintf("mock-%03d-%03d", i, j), Word: "Mock member", Home: j != 0, HandleBy: teamstore.HandleByTyped})
			if j == 0 {
				tm.Manager = file
			}
		}
		teams = append(teams, tm)
	}
	writeJSON(t, teamstore.Path(home), teamstore.File{Version: teamstore.Version, Teams: teams})
	seed := time.Since(began)
	var chats atomic.Int64
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if strings.HasSuffix(r.URL.Path, "/chat/completions") {
			chats.Add(1)
			http.Error(w, "no model calls belong in this scenario", 500)
			return
		}
		fmt.Fprint(w, `{"data":[]}`)
	}))
	defer server.Close()
	r := startWithEnv(t, []string{config.APIKeyEnv + "=mock-scale-key", "CODEAF_BASE_URL=" + server.URL}, "teams_scale", home, ws, 160, 55, "chat", "--model", "deepseek/deepseek-v4.1-flash", "--one-model", "--no-host")
	r.skipSetup(t)
	send := func(text string) {
		t.Helper()
		if out, err := exec.Command("tmux", "send-keys", "-t", r.name, "-l", text).CombinedOutput(); err != nil {
			t.Fatalf("input: %v %s", err, out)
		}
	}
	wait := func(start time.Time, text string) (string, time.Duration) {
		t.Helper()
		for time.Since(start) < 30*time.Second {
			screen := r.capture()
			if strings.Contains(screen, text) {
				return screen, time.Since(start)
			}
			time.Sleep(5 * time.Millisecond)
		}
		t.Fatalf("screen never displayed %q:\n%s", text, r.capture())
		return "", 0
	}
	r.lit("/teams")
	opened := time.Now()
	if out, err := exec.Command("tmux", "send-keys", "-t", r.name, "Enter").CombinedOutput(); err != nil {
		t.Fatalf("open teams: %v %s", err, out)
	}
	screen, opening := wait(opened, "Scale team 000")
	// These two pinned controls do not move when the activity reading sorts
	// team rows. Each sample waits for its distinct, actually rendered hint.
	targets := []struct{ label, hint string }{
		{"+ New AI team", "Create a top-level team"},
		{"All teams", "Every team, and what waits on you from any of them"},
	}
	var samples []float64
	for i := 0; i < 12; i++ {
		target := targets[i%2]
		screen = r.capture()
		y := 0
		for row, line := range strings.Split(screen, "\n") {
			if col := strings.Index(line, target.label); col >= 0 && col < 30 && row > 2 {
				y = row + 1
				break
			}
		}
		if y == 0 {
			t.Fatalf("no pinned hover target %q:\n%s", target.label, screen)
		}
		start := time.Now()
		send(fmt.Sprintf("\x1b[<35;8;%dM", y))
		_, elapsed := wait(start, target.hint)
		samples = append(samples, float64(elapsed)/float64(time.Millisecond))
	}
	sort.Float64s(samples)
	if chats.Load() != 0 {
		t.Fatalf("pointer navigation made %d model calls", chats.Load())
	}
	t.Logf("teams=%d memberships=%d setup=%s first_screen=%s hover_ms p50=%.3f p95=%.3f max=%.3f samples=%v model_calls=0", count, count*members, seed, opening, samples[len(samples)/2], samples[len(samples)-1], samples[len(samples)-1], samples)
}
