//go:build e2e

package e2e

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	teamstore "github.com/Agent-Field/codeaf/internal/teams"
	"github.com/charmbracelet/x/ansi"
)

// This checks the real input parser, overview and conversation door together.
// Historical excerpts are fixtures; the manager's final exchange is live.
func TestTeamsLargeOverviewPointerAndLivePreview(t *testing.T) {
	requireTmuxAndKey(t)
	const model = "deepseek/deepseek-v4.1-flash"
	if e2eModel != model {
		t.Fatalf("set CODEAF_E2E_MODEL=%s to pin auxiliary roles too", model)
	}
	home := newHome(t, map[string]any{"models.fallbacks": "", "model_pool": "off", "daily_budget_usd": 0})
	ws := newWorkspace(t, "teams-performance", false)
	quiet := false
	team := teamstore.Team{ID: teamstore.NewID(), Name: "Performance fixture", Settings: teamstore.Settings{Wake: &quiet}}
	var manager string
	for i := 0; i < 120; i++ {
		sid := fmt.Sprintf("%016x", 0x5000000000000000+i)
		bucket := strings.ReplaceAll(filepath.Clean(ws), string(filepath.Separator), "-")
		dir := filepath.Join(home, "v3", "projects", bucket, sid)
		if err := os.MkdirAll(dir, 0o700); err != nil {
			t.Fatal(err)
		}
		at := time.Now().Add(-time.Hour)
		writeJSON(t, filepath.Join(dir, "meta.json"), map[string]any{"id": sid, "title": fmt.Sprintf("Review member %03d", i), "workspace": ws, "created": at, "lastUserAt": at})
		var journal strings.Builder
		for _, row := range []map[string]any{
			{"type": "session", "version": 1, "id": sid, "cwd": ws, "model": model, "timestamp": at},
			{"type": "message", "role": "user", "content": fmt.Sprintf("Please review fixture %03d", i), "timestamp": at},
			{"type": "message", "role": "assistant", "content": strings.Repeat("## Review\n\nThe **rendering** preserves `layout` and navigation.\n\n", 800), "timestamp": at.Add(time.Second)},
		} {
			raw, err := json.Marshal(row)
			if err != nil {
				t.Fatal(err)
			}
			journal.Write(raw)
			journal.WriteByte('\n')
		}
		file := filepath.Join(dir, "transcript.jsonl")
		if err := os.WriteFile(file, []byte(journal.String()), 0o600); err != nil {
			t.Fatal(err)
		}
		team.Members = append(team.Members, teamstore.Member{Key: file, File: file, Where: ws, Handle: fmt.Sprintf("member-%03d", i), Word: fmt.Sprintf("Review member %03d", i), HandleBy: teamstore.HandleByTyped})
		if i == 0 {
			manager, team.Manager = file, file
		}
	}
	if err := teamstore.Save(home, []teamstore.Team{team}); err != nil {
		t.Fatal(err)
	}
	r := start(t, "teams_perf", home, ws, 160, 55, "chat", "--session", manager, "--model", model, "--one-model", "--no-host")
	if strings.Contains(r.capture(), "Tasks 0") {
		r.keys("M-l")
	}
	r.lit("Reply with exactly these words: Teams live check passed. Do not use tools or delegate.")
	r.keys("Enter")
	cleanChatWaitAnswer(t, r, "Teams live check passed", 3*time.Minute)
	cleanChatAuditModels(t, home, model, "teams-live")
	r.lit("/teams")
	r.keys("Enter")
	screen := r.waitFor(20*time.Second, "Performance fixture")
	selected := false
	for row, line := range strings.Split(screen, "\n") {
		if row > 2 && strings.Contains(line, "Performance fixture") {
			r.mouseClick(5, row+1)
			selected = true
			break
		}
	}
	if !selected {
		t.Fatal("team has no sidebar door")
	}
	r.waitFor(20*time.Second, "Teams live check passed", "@member-001", "Please review fixture 001")
	var sweep strings.Builder
	for i := 0; i < 600; i++ {
		fmt.Fprintf(&sweep, "\x1b[<35;%d;%dM", 35+i%110, 30+i%12)
	}
	r.lit(sweep.String())
	// One burst owes its complete physical scroll distance after coalescing.
	var wheel strings.Builder
	for i := 0; i < 12; i++ {
		wheel.WriteString("\x1b[<65;100;40M")
	}
	r.lit(wheel.String())
	r.waitFor(20*time.Second, "@member-010")
	r.resize(90, 32)
	screen = r.waitFor(20*time.Second, "Performance fixture", "@member-")
	// Open a visible member after scrolling and resizing: hits must follow the
	// actual card geometry, and chat navigation must survive the pointer burst.
	for row, line := range strings.Split(screen, "\n") {
		if row < 3 || !strings.Contains(line, "@member-") {
			continue
		}
		col := ansi.StringWidth(line[:strings.Index(line, "@member-")]) + 1
		r.mouseClick(col+1, row+1)
		r.waitFor(20*time.Second, "Please review fixture", "Review")
		if strings.Contains(r.capture(), "tab next place") {
			t.Fatal("member click stayed in the overview")
		}
		if evidence := os.Getenv("CODEAF_E2E_EVIDENCE_DIR"); evidence != "" {
			if err := os.MkdirAll(evidence, 0o700); err != nil {
				t.Fatal(err)
			}
			frame, err := exec.Command("tmux", "capture-pane", "-p", "-e", "-t", r.name).Output()
			if err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(evidence, "teams-member-door.ansi"), frame, 0o600); err != nil {
				t.Fatal(err)
			}
		}
		t.Log("120-member overview: live manager preview, pointer burst, pane wheel, resize and member click passed")
		return
	}
	t.Fatal("no visible member after resize")
}
