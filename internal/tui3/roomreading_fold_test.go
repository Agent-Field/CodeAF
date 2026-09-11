package tui3

import (
	"testing"

	"github.com/Agent-Field/aforge-v2/internal/config"
)

func readingFoldJournal(t *testing.T, earlierPhase, laterTurn bool) string {
	t.Helper()
	// A follow-up constraint adds an entry without adding a work phase. Keep
	// phase keys distinct from entry indexes even when narration joins its fold.
	lines := []string{
		`{"type":"message","role":"user","content":"Fix cancellation of a parser worker without losing pending requests."}`,
		`{"type":"message","role":"user","content":"Preserve the callers' public API while fixing the worker."}`,
	}
	if earlierPhase {
		lines = append(lines,
			`{"type":"message","role":"assistant","content":"Checking the module boundary.","toolCalls":[{"id":"earlier-call","function":{"name":"read","arguments":"{\"path\":\"go.mod\"}"}}]}`,
			`{"type":"message","role":"tool","toolCallId":"earlier-call","content":"module example/parser"}`,
			`{"type":"message","role":"assistant","content":"The module owns its worker lifecycle."}`)
	}
	lines = append(lines,
		`{"type":"message","role":"assistant","content":"","toolCalls":[{"id":"inspect-call","function":{"name":"read","arguments":"{\"path\":\"worker.go\"}"}}]}`,
		`{"type":"message","role":"tool","toolCallId":"inspect-call","content":"pending requests belong to the worker"}`,
		`{"type":"message","role":"assistant","content":"Cancellation leaves pending callers waiting."}`,
		`{"type":"message","role":"assistant","content":"Repairing cancellation and adding coverage.","toolCalls":[{"id":"change-call","function":{"name":"edit","arguments":"{\"path\":\"worker.go\",\"old\":\"wait()\",\"new\":\"cancelPending()\"}"}}]}`,
		`{"type":"message","role":"tool","toolCallId":"change-call","content":"pending callers are now cancelled"}`,
		`{"type":"message","role":"assistant","content":"Cancellation now resolves every waiting request."}`)
	if laterTurn {
		lines = append(lines,
			`{"type":"message","role":"user","content":"Also check retry after cancellation."}`,
			`{"type":"message","role":"assistant","content":"Checking retry.","toolCalls":[{"id":"retry-call","function":{"name":"bash","arguments":"{\"command\":\"go test ./...\"}"}}]}`,
			`{"type":"message","role":"tool","toolCallId":"retry-call","content":"ok example/parser"}`,
			`{"type":"message","role":"assistant","content":"Retries pass after cancellation."}`)
	}
	return roomJournal(t, lines...)
}

func readingFoldForCall(t *testing.T, a *app, call string) workfold {
	t.Helper()
	for _, fold := range a.deckFolds(a.room.deck()) {
		for i := fold.start; i < fold.answer; i++ {
			if a.room.entries[i].callID == call {
				return fold
			}
		}
	}
	t.Fatalf("no derived fold contains call %q", call)
	return workfold{}
}

// Adding an earlier settled phase changes both entry indexes and phase keys.
// Returning from a sibling must reopen only the phase the reader chose.
func TestTaskReadingRestoresLiveFoldByStartFingerprint(t *testing.T) {
	a, fake, _ := roomApp(t)
	a.workMode = config.WorkFold
	fake.journal = readingFoldJournal(t, false, false)
	a.openRoom(7, "Fix worker cancellation")
	_ = roomText(a)
	chosen := readingFoldForCall(t, a, "change-call")
	if chosen.key == chosen.start {
		t.Fatal("fixture must separate a phase key from its entry index")
	}
	a.toggleWorkfold(chosen.key)
	a.closeRoom()
	fake.journal = readingFoldJournal(t, true, false)
	a.openRoom(7, "Fix worker cancellation")
	_ = roomText(a)
	restored := readingFoldForCall(t, a, "change-call")
	if restored.key == chosen.key || restored.start == chosen.start {
		t.Fatal("fixture did not move both fold coordinates")
	}
	if !a.room.workOpen[restored.key] {
		t.Fatal("returning lost the expanded live phase")
	}
	for _, fold := range a.deckFolds(a.room.deck()) {
		if fold.key != restored.key && a.room.workOpen[fold.key] {
			t.Fatalf("returning expanded unrelated phase %d", fold.key)
		}
	}
}

// Reading a completed second exchange must not treat its turn number as an
// entry index. Replaying more retained work shifts its start without losing it.
func TestTaskReadingRestoresCompletedFoldAfterAnotherExchange(t *testing.T) {
	a, fake, _ := roomApp(t)
	a.workMode = config.WorkFold
	fake.journal = readingFoldJournal(t, false, true)
	a.openRoom(7, "Fix worker cancellation")
	a.room.done = true
	_ = roomText(a)
	if a.room.deck().lens.foldPast != foldTurns {
		t.Fatal("completed task must use turn folds")
	}
	chosen := readingFoldForCall(t, a, "retry-call")
	if chosen.key == chosen.start {
		t.Fatal("fixture must separate completed turn key and entry index")
	}
	a.toggleWorkfold(chosen.key)
	a.closeRoom()
	fake.journal = readingFoldJournal(t, true, true)
	a.openRoom(7, "Fix worker cancellation")
	a.room.done = true
	_ = roomText(a)
	restored := readingFoldForCall(t, a, "retry-call")
	if restored.start == chosen.start {
		t.Fatal("fixture did not shift the retained fold start")
	}
	if !a.room.workOpen[restored.key] {
		t.Fatal("returning lost the expanded completed exchange")
	}
	first := readingFoldForCall(t, a, "inspect-call")
	if a.room.workOpen[first.key] {
		t.Fatal("bookmark expanded the unrelated first exchange")
	}
}

// The same initial block can head a live phase and the completed turn. Opening
// that small phase is not permission to reopen the whole finished exchange.
func TestTaskReadingDoesNotApplyLivePhaseExpansionToCompletedTurn(t *testing.T) {
	a, fake, _ := roomApp(t)
	a.workMode = config.WorkFold
	fake.journal = readingFoldJournal(t, false, false)
	a.openRoom(7, "Fix worker cancellation")
	_ = roomText(a)
	phase := readingFoldForCall(t, a, "inspect-call")
	a.toggleWorkfold(phase.key)
	a.closeRoom()
	a.openRoom(7, "Fix worker cancellation")
	a.room.done = true
	_ = roomText(a)
	whole := readingFoldForCall(t, a, "inspect-call")
	if a.room.deck().lens.foldPast != foldTurns || whole.start != phase.start {
		t.Fatal("fixture did not reuse a start block across fold styles")
	}
	if a.room.workOpen[whole.key] {
		t.Fatal("live-phase bookmark opened the completed turn")
	}
}
