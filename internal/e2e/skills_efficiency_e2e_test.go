//go:build e2e

package e2e

import (
	"context"
	"database/sql"
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
	"github.com/Agent-Field/codeaf/internal/store"
	teamstore "github.com/Agent-Field/codeaf/internal/teams"
)

// Navigation and startup measurements exclude provider work entirely. The real
// binary still uses its ordinary configured model roles and rejects chat calls.
func efficiencyWire(t *testing.T) (string, *atomic.Int64) {
	t.Helper()
	calls := &atomic.Int64{}
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if strings.HasSuffix(r.URL.Path, "/chat/completions") {
			calls.Add(1)
			http.Error(w, "no model work belongs in this measurement", 500)
			return
		}
		fmt.Fprint(w, `{"data":[]}`)
	}))
	t.Cleanup(s.Close)
	return s.URL, calls
}

func efficiencyWait(t *testing.T, r *rig, start time.Time, text string) time.Duration {
	t.Helper()
	for time.Since(start) < 30*time.Second {
		if strings.Contains(r.capture(), text) {
			return time.Since(start)
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("screen did not display %q:\n%s", text, r.capture())
	return 0
}

func TestTeamsDisbandScaleTerminalLatency(t *testing.T) {
	for _, size := range []struct{ teams, members int }{{100, 100}, {200, 200}} {
		t.Run(fmt.Sprintf("%dteams_%dmembers", size.teams, size.members), func(t *testing.T) {
			if _, err := exec.LookPath("tmux"); err != nil {
				t.Skip("tmux required")
			}
			began := time.Now()
			home := newHome(t, map[string]any{"memory.enabled": false, "model_pool": "off"})
			ws := newWorkspace(t, "disband-scale", false)
			seedTeamsScale(t, size.teams, size.members, home, ws)
			setup := time.Since(began)
			url, calls := efficiencyWire(t)
			r := startWithEnv(t, []string{config.APIKeyEnv + "=mock-efficiency-key", "CODEAF_BASE_URL=" + url}, "disband_scale", home, ws, 160, 55, "chat", "--model", "deepseek/deepseek-v4.1-flash", "--one-model", "--no-host")
			r.skipSetup(t)
			r.lit("/teams")
			r.keys("Enter")
			screen := r.waitFor(30*time.Second, "Scale team 000")
			selected := false
			for y, line := range strings.Split(screen, "\n") {
				if x := strings.Index(line, "Scale team 000"); y > 2 && x >= 0 && x < 30 {
					r.mouseClick(x+2, y+1)
					selected = true
					break
				}
			}
			if !selected {
				t.Fatal("no team sidebar target")
			}
			r.waitFor(30*time.Second, "Disband")
			r.keys("c")
			r.waitFor(30*time.Second, "Disband these teams:", "Cancel")
			r.keys("Down")
			start := time.Now()
			if out, err := exec.Command("tmux", "send-keys", "-t", r.name, "Enter").CombinedOutput(); err != nil {
				t.Fatalf("confirm: %v %s", err, out)
			}
			elapsed := efficiencyWait(t, r, start, "Scale team 000 is disbanded; conversations and current work continue")
			f, err := teamstore.Load(home)
			if err != nil {
				t.Fatal(err)
			}
			closed, _ := f.Team("000000000001")
			other, _ := f.Team("000000000002")
			if !closed.Closed() || other.Closed() || len(closed.Members) != size.members || len(other.Members) != size.members {
				t.Fatal("disband lost history or changed an unrelated team")
			}
			if calls.Load() != 0 {
				t.Fatalf("disband made %d model calls", calls.Load())
			}
			t.Logf("teams=%d memberships=%d setup=%s confirm_to_saved_notice=%s model_calls=0", size.teams, size.teams*size.members, setup, elapsed)
			r.quit()
		})
	}
}

func TestSettledStoreTerminalStartup(t *testing.T) {
	if _, err := exec.LookPath("tmux"); err != nil {
		t.Skip("tmux required")
	}
	began := time.Now()
	home := newHome(t, map[string]any{"memory.enabled": false, "model_pool": "off"})
	ws := newWorkspace(t, "settled-store", false)
	path := filepath.Join(home, "graph.db")
	s, err := store.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	_, err = db.Exec(`WITH RECURSIVE fixture(i) AS (VALUES(1) UNION ALL SELECT i+1 FROM fixture WHERE i < 1000000)
INSERT INTO events(ts,node_id,kind,payload)
SELECT '2026-01-01T00:00:00Z','','message_posted',json_object('session_id','mock-'||(i%1000),'role','user','body','Mock message') FROM fixture`)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO messages(seq,ts,session_id,role,body)
SELECT seq,ts,json_extract(payload,'$.session_id'),'user','Mock message' FROM events WHERE kind='message_posted'`); err != nil {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	s, err = store.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	rooms, err := s.Sessions()
	if err != nil || len(rooms) != 1000 {
		t.Fatalf("fixture has %d rooms: %v", len(rooms), err)
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	setup := time.Since(began)
	url, calls := efficiencyWire(t)
	var samples []float64
	for i := 0; i < 3; i++ {
		name := fmt.Sprintf("settled_start_%d", i)
		target := fmt.Sprintf("p%d-%s", os.Getpid(), name)
		ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
		defer cancel()
		ready := make(chan time.Time, 1)
		start := time.Now()
		// Observe the real first interactive surface without the rig's coarser
		// startup polling. Fixture creation and later setup dismissal are separate.
		go func() {
			for ctx.Err() == nil {
				out, _ := exec.CommandContext(ctx, "tmux", "capture-pane", "-p", "-t", target).Output()
				screen := string(out)
				if strings.Contains(screen, "alt+e effort") || strings.Contains(screen, "esc skips setup") || strings.Contains(screen, "↑↓ moves") || strings.Contains(screen, "needs you") {
					ready <- time.Now()
					return
				}
				time.Sleep(5 * time.Millisecond)
			}
		}()
		r := startWithEnv(t, []string{config.APIKeyEnv + "=mock-efficiency-key", "CODEAF_BASE_URL=" + url}, name, home, ws, 160, 55, "chat", "--model", "deepseek/deepseek-v4.1-flash", "--one-model", "--no-host")
		select {
		case shown := <-ready:
			samples = append(samples, float64(shown.Sub(start))/float64(time.Millisecond))
		case <-ctx.Done():
			t.Fatalf("no observed interactive surface:\n%s", r.capture())
		}
		cancel()
		r.kill()
	}
	if calls.Load() != 0 {
		t.Fatalf("startup made %d model calls", calls.Load())
	}
	sort.Float64s(samples)
	t.Logf("messages=1000000 sessions=1000 setup=%s launch_to_first_surface_ms median=%.3f samples=%v model_calls=0", setup, samples[len(samples)/2], samples)
}
