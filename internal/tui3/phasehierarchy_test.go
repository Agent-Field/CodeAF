package tui3

import (
	"strings"
	"testing"

	"github.com/Agent-Field/aforge-v2/internal/config"
)

func phaseHierarchyRoom(t *testing.T) *app {
	t.Helper()
	a := newTestApp(&fakeAgent{model: "m"})
	a.width, a.height, a.workMode = 100, 40, config.WorkFold
	a.room = a.newRoom(7, "Repair the parser migration")
	a.room.entries = []entry{
		{kind: entryUser, text: "Reproduce the parser migration regression and preserve the public API.", turn: 1},
		{kind: entryTool, tool: "read", text: "parser.go", status: toolOK, turn: 1},
		{kind: entryAssistant, text: "Checking backward compatibility.", settled: true, turn: 1},
	}
	if a.room.deck().lens.foldPast != foldPhases {
		t.Fatal("the regression must exercise a running room's phase folds")
	}
	return a
}

func phaseHierarchyRow(t *testing.T, rows []row, phrase string) row {
	t.Helper()
	for _, r := range rows {
		if strings.Contains(plain(r.text), phrase) {
			return r
		}
	}
	t.Fatalf("room has no row containing %q: %#v", phrase, rows)
	return row{}
}

// A settled paragraph can end yesterday's phase and introduce today's tool.
// Rendering once before the tool also pins invalidation of the provisional
// answer, rather than only classification of a newly loaded transcript.
func TestARoomPhaseEndpointBecomesACaptionWhenMoreWorkStarts(t *testing.T) {
	a := phaseHierarchyRoom(t)
	before, _ := a.deckRows(a.room.deck(), 90)
	if a.room.entries[2].demoted || phaseHierarchyRow(t, before, "Checking backward compatibility").hit == hitCaption {
		t.Fatal("the current trailing paragraph is not the provisional answer")
	}
	folds := a.deckFolds(a.room.deck())
	if f, ok := folds[1]; !ok || f.answer != 2 {
		t.Fatalf("the paragraph must end a real phase fold: %#v", folds)
	}

	a.room.entries = append(a.room.entries, entry{kind: entryTool, tool: "bash", text: "go test ./parser/...", status: toolRunning, turn: 1})
	after, _ := a.deckRows(a.room.deck(), 90)
	if !a.room.entries[2].demoted {
		t.Fatal("the preceding phase endpoint still claims to be an answer while the next tool runs")
	}
	caption := phaseHierarchyRow(t, after, "Checking backward compatibility")
	if caption.hit != hitCaption || !strings.Contains(caption.text, sgrOf(a.pal.narr)) {
		t.Fatalf("progress did not become the tool step's narration caption: %q (hit %v)", caption.text, caption.hit)
	}
	count := 0
	for _, r := range after {
		if strings.Contains(plain(r.text), "Checking backward compatibility") {
			count++
		}
	}
	if count != 1 {
		t.Fatalf("the progress paragraph was duplicated beside its caption: %d copies", count)
	}
}

func TestARoomPhaseEndpointKeepsItsAnswerAcrossReceiptsAndPersonBoundaries(t *testing.T) {
	for _, boundary := range []entryKind{entryNote, entryDivider, entryUser, entrySteer} {
		t.Run(itoa(int(boundary)), func(t *testing.T) {
			a := phaseHierarchyRoom(t)
			a.room.entries[2].text = "## Verified result\n\n**The public API is preserved.**"
			next := entry{kind: boundary, text: "Review the integration coverage next", turn: 1}
			if boundary == entrySteer {
				next.steer = &steerElbow{words: next.text, consumed: true}
			}
			a.room.entries = append(a.room.entries, next)
			if boundary == entryUser || boundary == entrySteer {
				// A correction can share the same turn number; its later tools
				// must not demote the answer to the preceding request.
				a.room.entries = append(a.room.entries, entry{kind: entryTool, tool: "bash", text: "go test ./integration/...", status: toolRunning, turn: 1})
			}
			drawn, _ := a.deckRows(a.room.deck(), 90)
			if a.room.entries[2].demoted || a.room.entries[2].capCut != 0 {
				t.Fatal("a receipt or a person's next request demoted the actual answer")
			}
			r := phaseHierarchyRow(t, drawn, "Verified result")
			if r.hit == hitCaption || strings.Contains(plain(r.text), "## Verified") {
				t.Fatalf("answer lost its markdown rendering: %q", r.text)
			}
			if boundary != entryDivider {
				phaseHierarchyRow(t, drawn, "Review the integration coverage next")
			}
		})
	}
}

func TestARoomPhaseCaptionDoesNotHideAFailedNextStep(t *testing.T) {
	a := phaseHierarchyRoom(t)
	a.room.entries = append(a.room.entries,
		entry{kind: entryTool, tool: "bash", text: "integration assertion failed", status: toolFailed, turn: 1},
		entry{kind: entryAssistant, text: "The migration still fails its compatibility check.", settled: true, turn: 1},
	)
	drawn, _ := a.deckRows(a.room.deck(), 90)
	if !a.room.entries[2].demoted || a.room.entries[4].demoted {
		t.Fatal("the failed step did not separate progress from its final explanation")
	}
	for _, f := range a.deckFolds(a.room.deck()) {
		if f.start <= 3 && f.answer > 3 {
			t.Fatalf("a fold hid the failed step: %#v", f)
		}
	}
	phaseHierarchyRow(t, drawn, "integration assertion failed")
	phaseHierarchyRow(t, drawn, "The migration still fails its compatibility check")
}

// A completed conversation can receive a landed task or harness card later,
// without a new turn. Those cards must not reclassify its settled answer.
func TestACompletedTurnAnswerKeepsItsHierarchyWhenACardLands(t *testing.T) {
	for _, kind := range []entryKind{entryDone, entryHarness} {
		t.Run(itoa(int(kind)), func(t *testing.T) {
			a := newTestApp(&fakeAgent{model: "m"})
			a.entries = []entry{
				{kind: entryUser, text: "Repair the parser migration", turn: 1},
				{kind: entryTool, tool: "bash", text: "go test ./parser/...", status: toolOK, turn: 1},
				{kind: entryAssistant, text: "The parser API is preserved.", settled: true, turn: 1},
				{kind: kind, turn: 1},
				{kind: entryNote, text: "cost receipt", turn: 1},
			}
			folds := a.deckFolds(a.conversation())
			if f, ok := folds[1]; !ok || f.answer != 2 {
				t.Fatalf("the completed turn must have its own answer fold: %#v", folds)
			}
			stampHierarchy(a.entries, folds)
			if a.entries[2].demoted {
				t.Fatal("a later card demoted an already completed answer")
			}
		})
	}
}

func TestASettledRoomPhaseKeepsItsNarrationReachableThroughItsCaption(t *testing.T) {
	a := phaseHierarchyRoom(t)
	a.room.entries = append(a.room.entries,
		entry{kind: entryTool, tool: "bash", text: "go test ./parser/...", status: toolOK, turn: 1},
		entry{kind: entryAssistant, text: "The parser API is preserved.", settled: true, turn: 1},
	)
	folds := a.deckFolds(a.room.deck())
	f, ok := folds[2]
	if !ok || f.answer != 4 || f.key != 2 {
		t.Fatalf("the second phase did not settle: %#v", folds)
	}
	closed, _ := a.deckRows(a.room.deck(), 90)
	for _, r := range closed {
		if strings.Contains(plain(r.text), "Checking backward compatibility") {
			t.Fatal("a closed phase left its narration outside the work it introduces")
		}
	}
	a.openWorkfold(f.key)
	drawn, _ := a.deckRows(a.room.deck(), 90)
	caption := phaseHierarchyRow(t, drawn, "Checking backward compatibility")
	if caption.hit != hitCaption {
		t.Fatalf("opening the phase did not expose its progress caption: %#v", caption)
	}
	a.toggleCap(caption.turn)
	drawn, _ = a.deckRows(a.room.deck(), 90)
	phaseHierarchyRow(t, drawn, "go test")
	phaseHierarchyRow(t, drawn, "The parser API is preserved")
}

func TestPhaseNarrationOwnershipDoesNotInventWorkOrCrossProtectedRows(t *testing.T) {
	prose := []entry{
		{kind: entryUser, text: "Diagnose the parser", turn: 1},
		{kind: entryAssistant, text: "First finding.", settled: true, turn: 1},
		{kind: entryAssistant, text: "Second finding.", settled: true, turn: 1},
	}
	if folds := derivePhaseFolds(prose); len(folds) != 0 {
		t.Fatalf("prose-only stretches acquired folds: %#v", folds)
	}
	for _, protected := range []entry{
		{kind: entryUser, text: "A correction", turn: 1},
		{kind: entrySteer, turn: 1, steer: &steerElbow{words: "A correction", consumed: true}},
		{kind: entryTool, tool: "bash", status: toolFailed, turn: 1},
		{kind: entryTask, turn: 1},
	} {
		es := append([]entry(nil), prose...)
		es = append(es, protected,
			entry{kind: entryTool, tool: "read", status: toolOK, turn: 1},
			entry{kind: entryAssistant, text: "Final answer.", settled: true, turn: 1},
		)
		folds := derivePhaseFolds(es)
		if f, ok := folds[4]; !ok || f.start != 4 || f.answer != 5 {
			t.Fatalf("phase narration crossed protected entry kind %v: %#v", protected.kind, folds)
		}
	}
}

// The visible row is the actual disclosure target. A closed older phase can
// have an earlier caption in the complete entry list without exposing it here.
func openVisiblePhaseCaption(t *testing.T, a *app) {
	t.Helper()
	drawn, _ := a.deckRows(a.room.deck(), 90)
	for _, r := range drawn {
		if r.hit == hitCaption {
			a.toggleCap(r.turn)
			return
		}
	}
	t.Fatal("the opened phase has no visible caption to click")
}

func TestReasoningBetweenPhasesCannotStealTheirNarrationOrKeys(t *testing.T) {
	a := phaseHierarchyRoom(t)
	a.room.entries = []entry{
		{kind: entryTool, tool: "read", text: "parser.go", status: toolOK, turn: 1},
		{kind: entryAssistant, text: "Comparing parser contracts.", settled: true, turn: 1},
		{kind: entryThinking, text: "The old API accepts partial tokens.", settled: true, turn: 1},
		{kind: entryAssistant, text: "Running compatibility checks.", settled: true, turn: 1},
		{kind: entryTool, tool: "bash", text: "go test ./parser/...", status: toolOK, turn: 1},
		{kind: entryAssistant, text: "The parser API is preserved.", settled: true, turn: 1},
	}
	folds := a.deckFolds(a.room.deck())
	if len(folds) != 3 {
		t.Fatalf("a reasoning phase was overwritten: %#v", folds)
	}
	for key, span := range [][2]int{{0, 1}, {1, 3}, {3, 5}} {
		if f, ok := folds[span[0]]; !ok || f.answer != span[1] || f.key != key+1 {
			t.Fatalf("phase %d overlaps or changed its key: %#v", key+1, folds)
		}
	}
	a.openWorkfold(3)
	drawn, _ := a.deckRows(a.room.deck(), 90)
	caption := phaseHierarchyRow(t, drawn, "Running compatibility checks")
	if caption.hit != hitCaption {
		t.Fatalf("the tool phase did not keep its own narration: %#v", caption)
	}
	for _, r := range drawn {
		if strings.Contains(plain(r.text), "Comparing parser contracts") {
			t.Fatal("a tool phase borrowed the prior reasoning phase's narration")
		}
	}
}
