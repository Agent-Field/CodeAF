//go:build e2e

package e2e

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/session"
	teamstore "github.com/Agent-Field/codeaf/internal/teams"
)

func TestAllShowsTheCurrentTabsOnTheTerminal(t *testing.T) {
	requireTmuxAndKey(t)
	const model = "deepseek/deepseek-v4.1-flash"
	if e2eModel != model {
		t.Fatalf("requires CODEAF_E2E_MODEL=%s", model)
	}
	home := newHome(t, map[string]any{"memory.enabled": "off", "models.fallbacks": "", "model_pool": "off"})
	workspace := newWorkspace(t, "open-tabs", false)
	var members []teamstore.Member
	for i, title := range []string{"Planning notes", "Design review", "Testing notes", "Saved research"} {
		dir := filepath.Join(home, "v3", "projects", strings.ReplaceAll(workspace, string(filepath.Separator), "-"), fmt.Sprintf("aa55aa55aa55%04d", i))
		if err := os.MkdirAll(dir, 0700); err != nil {
			t.Fatal(err)
		}
		file := filepath.Join(dir, "transcript.jsonl")
		header, _ := json.Marshal(map[string]any{"type": "session", "version": 1, "id": filepath.Base(dir), "cwd": workspace, "model": model, "timestamp": time.Now().UTC()})
		named, _ := json.Marshal(map[string]any{"type": "title", "title": title})
		message, _ := json.Marshal(map[string]any{"type": "message", "role": "user", "content": title})
		reply, _ := json.Marshal(map[string]any{"type": "message", "role": "assistant", "content": "Saved notes for " + title + "."})
		data := string(header) + "\n" + string(named) + "\n" + string(message) + "\n" + string(reply) + "\n"
		if err := os.WriteFile(file, []byte(data), 0600); err != nil {
			t.Fatal(err)
		}
		if err := session.SaveMeta(dir, session.Meta{ID: filepath.Base(dir), Title: title, Workspace: workspace}); err != nil {
			t.Fatal(err)
		}
		members = append(members, teamstore.Member{Key: file, File: file, Word: title, Where: workspace})
	}
	if err := teamstore.Save(home, []teamstore.Team{{ID: teamstore.NewID(), Name: "Product", Members: members[:2]}, {ID: teamstore.NewID(), Name: "Quality", Members: members[2:3]}}); err != nil {
		t.Fatal(err)
	}
	stopRecoveryEngineOnCleanup(t, nil, home, workspace)
	r := startDefault(t, "open-tabs-wall", home, workspace, 140, 42, "chat", "--session", members[0].File, "--model", model, "--one-model", "--debug")
	if out, err := guardedCommand(t, context.Background(), home, os.Environ(), "tmux", "set-option", "-t", r.name, "status", "off").CombinedOutput(); err != nil {
		t.Fatalf("terminal status: %v: %s", err, out)
	}
	r.lit("Reply with only OPEN_TABS_READY. Do not change files.")
	r.keys("Enter")
	cleanChatWaitAnswer(t, r, "OPEN_TABS_READY", time.Minute)
	for _, title := range []string{"Design review", "Testing notes"} {
		r.lit("/new")
		r.keys("Enter")
		r.lit("/resume")
		r.keys("Enter")
		r.lit(title)
		r.keys("Enter")
		r.waitFor(20*time.Second, title)
	}
	capture := func(stage string) {
		t.Helper()
		cleanChatCapture(t, r, stage)
		if hook := os.Getenv("CODEAF_E2E_SCREENSHOT_HOOK"); hook != "" {
			if out, err := guardedCommand(t, context.Background(), home, os.Environ(), hook, r.name, stage).CombinedOutput(); err != nil {
				t.Fatalf("screenshot: %v: %s", err, out)
			}
		}
	}
	// Compare the visible strip with the grid, not with the saved-session count.
	strip := strings.Split(r.capture(), "\n")[2]
	openCount := 0
	for _, title := range []string{"Planning notes", "Design review", "Testing notes"} {
		if strings.Contains(strip, title) {
			openCount++
		}
	}
	if openCount < 2 || !strings.Contains(strip, "Design review") || !strings.Contains(strip, "Testing notes") {
		t.Fatalf("fixture needs its two resumed tabs on the strip: %s", strip)
	}
	r.mouseClick(138, 3)
	normal := r.waitFor(20*time.Second, "open tabs", fmt.Sprintf("%d conversations", openCount))
	if strings.Contains(normal, "Saved research") {
		t.Fatal("saved history appeared in All")
	}
	capture("01-normal-open-tabs")
	r.keys("Escape")
	r.lit("/quit")
	r.keys("Enter")
	r.mouseClick(138, 3)
	closed := r.waitFor(20*time.Second, "open tabs", fmt.Sprintf("%d conversations", openCount-1))
	if strings.Contains(closed, "Testing notes") || strings.Contains(closed, "Saved research") {
		t.Fatal("All retained the closed tab or included saved history")
	}
	capture("03-after-closing-tab")
	r.keys("Escape")
	// The team dropdown is the leftmost control on the tab strip.
	r.mouseClick(4, 3)
	r.waitFor(10*time.Second, "Product")
	r.keys("Enter")
	r.mouseClick(138, 3)
	team := r.waitFor(20*time.Second, "open tabs", "in Product", keyedWord("2", "conversations"))
	if !strings.Contains(team, "in Product") || !strings.Contains(team, "2 conversations") {
		t.Fatal("All did not show the selected team's two tabs")
	}
	if strings.Contains(team, "Testing notes") || strings.Contains(team, "Saved research") {
		t.Fatal("All included a conversation outside the selected team's tabs")
	}
	capture("02-team-open-tabs")
	cleanChatAuditModels(t, home, model, "open-tabs-wall")
}
