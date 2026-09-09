//go:build e2e

package e2e

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/x/ansi"
)

// TestTUIWaitingFamily drives the same parent/child transition as a real task,
// so terminal composition and model tool availability are exercised together.
func TestTUIWaitingFamily(t *testing.T) {
	requireTmuxAndKey(t)
	home := newHome(t, nil)
	ws := newWorkspace(t, "familylayout", false)
	r := start(t, "afe2e_familylayout", home, ws, 180, 40, "chat", "--one-model", "--no-host")
	r.waitForAny(20*time.Second, say(t, "homeFootWord"), say(t, "starterTaskWord"), say(t, "setupTitleWord"), say(t, "landingKeysWord"))
	r.keys("Escape")
	r.lit("/task solo Delegate two independent file checks. You MUST call propose_task twice before doing anything else. Give the first child the title 'Check alpha document and record the available task inspection capabilities'; give the second child the title 'Check beta document and record the available task delegation capabilities'. Each child must first run bash sleep 60, then write its own file alpha.txt or beta.txt containing hello and whether propose_task and tasks are actually present in its tool list. Do not ask children to call missing tools. Acceptance: both files exist and honestly describe their tool availability. Wait for both children, then report their results. Do not do their work yourself.")
	r.keys("Enter")
	r.waitFor(3*time.Minute, "#1")
	r.keys("Right")
	screen := r.waitFor(5*time.Minute, say(t, "roomKinSpawnedWord"), "waiting")
	t.Logf("waiting parent on %s:\n%s", e2eModel, screen)
	for _, width := range []int{180, 120} {
		r.resize(width, 40)
		time.Sleep(500 * time.Millisecond)
		screen = r.capture()
		seam := -1
		for _, line := range strings.Split(screen, "\n") {
			if at := strings.Index(line, "│"); at >= 0 && strings.Contains(line[at:], "tasks") {
				seam = ansi.StringWidth(line[:at])
				break
			}
		}
		if seam < 0 {
			t.Fatalf("no task roster boundary at %d cells:\n%s", width, screen)
		}
		found := false
		for _, line := range strings.Split(screen, "\n") {
			if strings.Contains(line, say(t, "roomKinSpawnedWord")) {
				found = true
				if got := ansi.StringWidth(strings.TrimRight(line, " ")); got > seam {
					t.Fatalf("child summary crossed roster boundary: %d > %d:\n%s", got, seam, screen)
				}
			}
		}
		if !found {
			t.Fatalf("parent lost its child summary at %d cells:\n%s", width, screen)
		}
		t.Logf("parent at %d cells:\n%s", width, screen)
	}
	r.keys("Right")
	child := r.waitFor(30*time.Second, "Check", say(t, "roomBackWord"))
	t.Logf("child room:\n%s", child)
	models := waitForTaskCalls(t, home, 2*time.Minute)
	if len(models) == 0 {
		t.Fatal("no real task model calls recorded")
	}
	for _, model := range models {
		if model != e2eModel {
			t.Errorf("task used %s, wanted %s", model, e2eModel)
		}
	}
	t.Logf("real task models: %s", fmt.Sprint(models))
	r.keys("Escape")
	bucket := waitForRecord(t, home, 5*time.Minute)
	deadline := time.Now().Add(5 * time.Minute)
	finished := false
	for time.Now().Before(deadline) && !finished {
		raw, err := os.ReadFile(filepath.Join(bucket, "tasks.jsonl"))
		if err != nil {
			t.Fatal(err)
		}
		for _, line := range strings.Split(string(raw), "\n") {
			var record struct {
				ID      string `json:"id"`
				Status  string `json:"status"`
				Outcome string `json:"outcome"`
				Where   string `json:"where"`
			}
			if json.Unmarshal([]byte(line), &record) == nil && record.ID == "1" {
				if record.Status != "done" && record.Status != "unverified" && record.Status != "failed" {
					t.Fatalf("unexpected terminal task state %s: %s", record.Status, record.Outcome)
				}
				t.Logf("parent execution ended %s: %s", record.Status, record.Outcome)
				// Right cycles running tasks only. A completed parent is opened
				// through its roster row, the same pointer door a person uses.
				opened := false
				for i, line := range strings.Split(r.capture(), "\n") {
					if at := strings.Index(line, "│ tasks"); at >= 0 {
						x, y := ansi.StringWidth(line[:at])+6, i+2
						r.lit(fmt.Sprintf("\x1b[<0;%d;%dM\x1b[<0;%d;%dm", x, y, x, y))
						opened = true
						break
					}
				}
				if !opened {
					t.Fatal("completed parent has no roster row to open")
				}
				// Model checks can finish or ask for review. The terminal must
				// expose that ending rather than leave the parent spinning.
				shown := false
				until := time.Now().Add(30 * time.Second)
				for time.Now().Before(until) && !shown {
					screen := r.capture()
					for _, line := range strings.Split(screen, "\n") {
						if strings.HasPrefix(strings.TrimSpace(line), "─") &&
							(strings.Contains(line, "done") || strings.Contains(line, "your call") || strings.Contains(line, "incomplete")) {
							shown = true
							t.Logf("parent's final header: %s", line)
						}
					}
					if !shown {
						time.Sleep(250 * time.Millisecond)
					}
				}
				if !shown {
					t.Fatalf("parent ending was never shown:\n%s", r.capture())
				}
				finished = true
			}
		}
		if !finished {
			time.Sleep(time.Second)
		}
	}
	if !finished {
		t.Fatal("the parent never finished after its children")
	}
	for _, model := range taskCallModels(t, home) {
		if model != e2eModel {
			t.Errorf("task used %s, wanted %s", model, e2eModel)
		}
	}
	t.Logf("task record written; final screen:\n%s", r.capture())
}
