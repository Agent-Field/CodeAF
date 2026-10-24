package tui3

import (
	"strings"
	"testing"

	"github.com/Agent-Field/codeaf/internal/crewroute"
	"github.com/Agent-Field/codeaf/internal/session"
)

// Quiet landings must retain the result rather than clearing the entry that
// existing task and record doors use to find it.
func TestQuietCompletionPreservesDataAcrossRepeatedUpdates(t *testing.T) {
	a, _, _ := taskApp(t)
	for _, state := range []session.TaskState{session.TaskDone, session.TaskFailed} {
		drive(t, a, streamEventMsg{gen: a.gen, ev: update(73, "Preserved result", state, session.TaskNotice{Report: "Landing account", Result: "Actual deliverable", CostUSD: 0.17})})
		card := a.doneCardAt(a.doneEntryFor(73))
		if card == nil || card.result != "Actual deliverable" || card.report != "Landing account" {
			t.Fatalf("completion lost its data: %+v", card)
		}
		if rows := a.doneRows(card, 100, false); len(rows) != 0 {
			t.Fatalf("routine completion occupies %d rows: %v", len(rows), rows)
		}
		drive(t, a, streamEventMsg{gen: a.gen, ev: update(73, "Preserved result", state, session.TaskNotice{Report: "Landing account", Result: "Actual deliverable", CostUSD: 0.19})})
		if got := taskText(a); strings.Contains(got, "Preserved result") || strings.Contains(got, "Landing account") {
			t.Fatalf("metadata refresh raised a completion again: %s", got)
		}
	}
}

func TestQuietCompletionCannotHideNewPendingDecision(t *testing.T) {
	a, _, _ := taskApp(t)
	drive(t, a, streamEventMsg{gen: a.gen, ev: update(74, "Review parser", session.TaskFailed, session.TaskNotice{Report: "Partial result", Result: "Parser implementation"})})
	if card := a.doneCardAt(a.doneEntryFor(74)); card == nil || !card.hidden() {
		t.Fatalf("routine failure not quiet: %+v", card)
	}
	drive(t, a, streamEventMsg{gen: a.gen, ev: update(74, "Review parser", session.TaskFailed, session.TaskNotice{Report: "Check failed", Result: "Parser implementation", ResultHeld: true})})
	card := a.doneCardAt(a.doneEntryFor(74))
	if card == nil || card.hidden() || card.status.Tier != session.TaskTierYourCall {
		t.Fatalf("new decision hidden: %+v", card)
	}
	if got := taskText(a); !strings.Contains(got, "Review parser") || !strings.Contains(got, card.status.Ask.Reason) {
		t.Fatalf("decision not visible with reason: %s", got)
	}
	if a.dismissDone(a.doneEntryFor(74)) {
		t.Fatal("pending decision can be dismissed")
	}
}

func TestCrewLifecycleDoesNotCreateIndependentTranscriptReceipts(t *testing.T) {
	a, _, _ := taskApp(t)
	count := len(a.entries)
	crew := &crewroute.Decision{Class: crewroute.Bugfix, EstUSD: 0.02, Crew: []crewroute.Pick{{Seat: crewroute.Worker, Model: "deepseek/deepseek-v4.1-flash", Provider: "openrouter"}}}
	for _, state := range []session.TaskState{session.TaskRunning, session.TaskDone, session.TaskDone} {
		a.sayTaskCrew(session.TaskNotice{ID: 75, State: state, Crew: crew, CostUSD: 0.018})
	}
	if len(a.entries) != count {
		t.Fatalf("crew lifecycle added %d transcript receipts", len(a.entries)-count)
	}
}

// Notices can be the only surviving result or a work-stopping explanation, so
// grouping completion receipts must never blanket-filter their event kind.
func TestQuietCompletionPreservesSubstantiveNotices(t *testing.T) {
	a, _, _ := taskApp(t)
	for _, text := range []string{"The time limit stopped this task; its partial output is saved.", "Fallback result: the comparison favours the parser implementation.", "Reminder: review the parser experiment."} {
		a.taskEvent(session.Event{Kind: session.EventNotice, Text: text})
		if got := taskText(a); !strings.Contains(got, text) {
			t.Fatalf("substantive notice was hidden: %q\n%s", text, got)
		}
	}
}

func TestQuietCompletionSidebarContainsOnlyActiveWorkAndDecisions(t *testing.T) {
	a, _, _ := taskApp(t)
	drive(t, a, streamEventMsg{gen: a.gen, ev: update(76, "Finished parser experiment", session.TaskDone, session.TaskNotice{Report: "Completed result"})})
	got := plain(strings.Join(a.railRows(20), "\n"))
	for _, hidden := range []string{"Finished parser experiment", "Done 1", "Ended 1", "Tasks 1"} {
		if strings.Contains(got, hidden) {
			t.Fatalf("completed work still occupies the sidebar (%q):\n%s", hidden, got)
		}
	}
	drive(t, a, streamEventMsg{gen: a.gen, ev: update(77, "Choose parser result", session.TaskUnverified, unverifiedNotice("needs review"))})
	got = plain(strings.Join(a.railRows(20), "\n"))
	if !strings.Contains(got, "Choose parser result") {
		t.Fatalf("pending decision missing from sidebar:\n%s", got)
	}
}
