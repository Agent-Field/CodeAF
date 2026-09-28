package tui3

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"

	"github.com/Agent-Field/codeaf/internal/session"
)

// managerWake is the note the session wakes a manager with, whole, as a
// transcript holds it when it carries no team lines.
const managerWake = "Your team's replies started this turn; the person did not speak. Act on them: hand out what comes next, or tell the person where the work stands.\n\nWhat your members in \"harbor\" did:\n@api finished"

// memberWake is the note the session wakes a member with, carrying the
// manager's directive as a delivery.
const memberWake = "Your manager started this turn (a directive, or the answer to your question); the person did not speak.\n\nTeam traffic in \"harbor\" (@api), from your manager:\ndirective from manager: check the parser"

func wallTileText(a *app, entries []session.DisplayEntry) string {
	return ansi.Strip(strings.Join(a.wallDraw(entries, 60), "\n"))
}

// Contract 3.1 and 3.2: a tile never draws a wake note's sentence. A manager's
// wake with no team lines draws nothing; the person's words and the reply stay.
func TestATileNeverDrawsAManagersWakeSentence(t *testing.T) {
	a, _, _ := tabApp(t)
	got := wallTileText(a, []session.DisplayEntry{
		{Role: "user", Text: "sort out the parser"},
		{Role: "aside", Text: managerWake},
		{Role: "assistant", Text: "api is done; nothing else is pending."},
	})
	if strings.Contains(got, "started this turn") || strings.Contains(got, "did not speak") {
		t.Fatalf("the tile drew the wake sentence:\n%s", got)
	}
	if !strings.Contains(got, "sort out the parser") || !strings.Contains(got, "nothing else is pending") {
		t.Fatalf("the tile lost the person's words or the reply:\n%s", got)
	}
}

// Contract 3.2: a member's wake that carries its manager's directive draws the
// directive, as the conversation's card does, and not the sentence above it.
func TestATileDrawsAMembersWakeAsItsDirective(t *testing.T) {
	a, _, _ := tabApp(t)
	lines := []session.TeamLine{{Team: "harbor", From: "manager", Kind: "directive", Text: "check the parser"}}
	got := wallTileText(a, []session.DisplayEntry{{Role: "aside", Text: memberWake, Team: lines}})
	if strings.Contains(got, "started this turn") {
		t.Fatalf("the tile drew the wake sentence:\n%s", got)
	}
	if !strings.Contains(got, "check the parser") || !strings.Contains(got, "manager") {
		t.Fatalf("the tile did not draw the manager's directive:\n%s", got)
	}
}

// Contract 3.4: every other session aside draws as it did, its first line.
func TestATileStillDrawsATaskLanding(t *testing.T) {
	a, _, _ := tabApp(t)
	got := wallTileText(a, []session.DisplayEntry{{Role: "aside", Text: "Task 3 finished: the parser is fixed.\nmore"}})
	if !strings.Contains(got, "Task 3 finished") {
		t.Fatalf("the tile lost a task landing:\n%s", got)
	}
}

// Contract 3.3: a reopened conversation draws a manager's wake note the way the
// live one does: not at all. A task landing still draws its line.
func TestAReopenedConversationDoesNotDrawAManagersWakeSentence(t *testing.T) {
	a, _, _ := tabApp(t)
	blocks, _ := a.replayBlocks([]session.DisplayEntry{
		{Role: "user", Text: "sort out the parser"},
		{Role: "aside", Text: managerWake},
		{Role: "assistant", Text: "api is done."},
		{Role: "aside", Text: "Task 3 finished: the parser is fixed."},
	}, chatReplay(0))
	var all []string
	for _, b := range blocks {
		all = append(all, b.text)
	}
	got := strings.Join(all, "\n")
	if strings.Contains(got, "started this turn") {
		t.Fatalf("the reopened conversation drew the wake sentence:\n%s", got)
	}
	if !strings.Contains(got, "Task 3 finished") {
		t.Fatalf("the reopened conversation lost a task landing:\n%s", got)
	}
}
