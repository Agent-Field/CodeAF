package tui3

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Agent-Field/codeaf/internal/claudemodel"
	"github.com/Agent-Field/codeaf/internal/config"
	"github.com/Agent-Field/codeaf/internal/crewroute"
)

// TestCrewSeatPickerListsConnectedClaudeCodeModels2681 verifies that when
// claude-code is connected, the /crew seat picker offers the three family rows
// and six version rows (including claude-code/claude-haiku-5-5), each marked
// "on your Claude plan", and when claude-code is not connected none of those
// rows appear.
func TestCrewSeatPickerListsConnectedClaudeCodeModels2681(t *testing.T) {
	a, dir := crewLab(t)
	j := &crewJourney{t: t, a: a}

	// Control: without claude-code connected, no claude-code/* rows are offered.
	j.open()
	j.keys("enter")
	if a.crewUI.view != crewPicking || a.crewUI.pick.seat != crewroute.Worker {
		t.Fatalf("enter on worker opened %v", a.crewUI.view)
	}
	for _, row := range a.crewUI.pick.rows {
		if strings.HasPrefix(row.offer.Model.ID, claudemodel.ID+"/") {
			t.Fatalf("disconnected profile offered %s", row.offer.Model.ID)
		}
	}
	j.typed("claude-haiku")
	if screen := crewScreen(a); !strings.Contains(screen, crewNothingMatches) {
		t.Fatalf("disconnected profile did not show %q for claude-haiku:\n%s", crewNothingMatches, screen)
	}
	j.keys("esc", "esc")

	// Connect claude-code using a fake claude binary on PATH.
	binDir := t.TempDir()
	fakeClaude := filepath.Join(binDir, "claude")
	script := "#!/bin/sh\ncase \"$1\" in\n  --version) echo \"2.1.0 (Claude)\";;\n  auth) echo '{\"loggedIn\":true,\"authMethod\":\"claude.ai\",\"subscriptionType\":\"max\"}';;\n  *) echo '{\"type\":\"result\",\"subtype\":\"success\",\"result\":\"ok\"}';;\nesac\n"
	if err := os.WriteFile(fakeClaude, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", binDir+string(os.PathListSeparator)+os.Getenv("PATH"))
	if _, err := config.ConnectClaudeCode(t.Context(), dir); err != nil {
		t.Fatalf("ConnectClaudeCode: %v", err)
	}

	// Open /crew and press enter on worker.
	j.open()
	j.keys("enter")
	if a.crewUI.view != crewPicking || a.crewUI.pick.seat != crewroute.Worker {
		t.Fatalf("enter on worker opened %v", a.crewUI.view)
	}

	offered := map[string]bool{}
	for _, row := range a.crewUI.pick.rows {
		if !row.auto {
			offered[row.offer.Model.ID] = true
		}
	}
	for _, want := range []string{
		"claude-code/opus",
		"claude-code/sonnet",
		"claude-code/haiku",
		"claude-code/claude-haiku-5-5",
		"claude-code/claude-haiku-4-5",
		"claude-code/claude-sonnet-5-5",
		"claude-code/claude-sonnet-5",
		"claude-code/claude-opus-5-5",
		"claude-code/claude-opus-5",
	} {
		if !offered[want] {
			t.Errorf("seat picker did not offer %s (offered=%v)", want, offered)
		}
	}

	// Filter by "claude-haiku": claude-code/claude-haiku-5-5 is listed and marked
	// "on your Claude plan", and "nothing matches" is not shown.
	j.typed("claude-haiku")
	screen := crewScreen(a)
	if strings.Contains(screen, crewNothingMatches) {
		t.Fatalf("connected profile showed %q for claude-haiku:\n%s", crewNothingMatches, screen)
	}
	if !strings.Contains(screen, "claude-code/claude-haiku-5-5") {
		t.Fatalf("seat picker did not list claude-code/claude-haiku-5-5:\n%s", screen)
	}
	if !strings.Contains(screen, "on your Claude plan") {
		t.Fatalf("seat picker did not mark row 'on your Claude plan':\n%s", screen)
	}

	// Typing a pin by hand still works.
	j.keys("esc", "esc")
	typeLine(t, a, "/crew pin worker claude-code/claude-haiku-5-5")
	pin, ok := config.CrewPinAt(dir, crewroute.Worker)
	if !ok || pin.Model != "claude-code/claude-haiku-5-5" {
		t.Fatalf("worker pin = %+v, %v; want claude-code/claude-haiku-5-5", pin, ok)
	}
}
