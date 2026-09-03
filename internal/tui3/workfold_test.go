package tui3

import (
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/aforge-v2/internal/config"
)

func foldFixture() []entry {
	base := time.Unix(100, 0)
	return []entry{
		{kind: entryUser, text: "do it", turn: 1, began: base},
		{kind: entryThinking, text: "checking", turn: 1, began: base, ended: base.Add(6 * time.Second), settled: true},
		{kind: entryAssistant, text: "I will inspect it.", turn: 1, settled: true},
		// Overlapping clocks — one parallel step under one caption.
		{kind: entryTool, tool: "read", turn: 1, status: toolOK, began: base.Add(6 * time.Second), ended: base.Add(8 * time.Second)},
		{kind: entryTool, tool: "bash", turn: 1, status: toolOK, began: base.Add(6 * time.Second), ended: base.Add(10 * time.Second)},
		{kind: entryAssistant, text: "Done.", turn: 1, settled: true},
	}
}

func TestWorkfoldRendersOneCountedChipAndFlushAnswer(t *testing.T) {
	a := newTestApp(&fakeAgent{model: "m"})
	a.entries, a.workMode = foldFixture(), config.WorkFold
	a.stamps = map[int]turnStamp{1: {took: 47 * time.Second}}
	a.touch()
	got := strings.Join(plainRows(a), "\n")
	if !strings.Contains(got, "  ▸ worked 47s · thought 6.0s · 2 tool calls · ctrl+e") {
		t.Fatalf("missing counted workfold:\n%s", got)
	}
	if strings.Contains(got, "read") || strings.Contains(got, "checking") || !strings.Contains(got, "\nDone.") {
		t.Fatalf("fold did not leave only its flush answer:\n%s", got)
	}
}

func TestWorkIndentReclassifiesAndDropsAtPhoneFloor(t *testing.T) {
	a := newTestApp(&fakeAgent{model: "m"})
	a.entries = foldFixture()
	a.entries[len(a.entries)-1].settled = false // live trailing answer
	a.workMode = config.WorkOpen
	a.touch()
	wide := strings.Join(plainRows(a), "\n")
	if !strings.Contains(wide, "  ▾ I will inspect it") || !strings.Contains(wide, "\nDone.") {
		t.Fatalf("intermediate/trailing classification is wrong:\n%s", wide)
	}
	a.width = 59
	a.touch()
	if got := strings.Join(plainRows(a), "\n"); strings.Contains(got, "  ▾ I will inspect it") {
		t.Fatalf("phone tier kept the work gutter:\n%s", got)
	}
}

func TestWorkfoldNeverHidesTextOnlyFailureOrNoAnswer(t *testing.T) {
	cases := [][]entry{
		{{kind: entryUser, text: "hi", turn: 1}, {kind: entryAssistant, text: "hello", turn: 1, settled: true}},
		{{kind: entryUser, text: "do", turn: 1}, {kind: entryTool, tool: "bash", turn: 1}, {kind: entryNote, text: "error: boom", turn: 1}},
		{{kind: entryUser, text: "do", turn: 1}, {kind: entryTool, tool: "bash", turn: 1}},
	}
	for _, entries := range cases {
		if got := deriveWorkfolds(entries, 0); len(got) != 0 {
			t.Fatalf("ineligible turn derived a fold: %#v", got)
		}
	}
}

func TestWorkfoldKeyPreservesSubfoldStateAndIsEphemeral(t *testing.T) {
	a := newTestApp(&fakeAgent{model: "m"})
	a.entries, a.workMode = foldFixture(), config.WorkFold
	a.entries[1].open = false
	if !a.toggleLatestWorkfold() || !a.workOpen[1] {
		t.Fatal("ctrl+e resolution did not open the latest fold")
	}
	if a.entries[1].open {
		t.Fatal("opening the workfold changed its thinking sub-fold")
	}
	b := newTestApp(&fakeAgent{model: "m"})
	b.entries, b.workMode = append([]entry(nil), a.entries...), config.WorkFold
	if b.workOpen[1] {
		t.Fatal("workfold expansion persisted into a new window")
	}
}

func TestWorkfoldSettlementAnchorsBottomAndScrolledReader(t *testing.T) {
	makeApp := func() *app {
		a := newTestApp(&fakeAgent{model: "m"})
		for i := 0; i < 15; i++ {
			a.entries = append(a.entries, entry{kind: entryDivider, text: "earlier", turn: 0})
		}
		start := len(a.entries)
		a.entries, a.workMode = append(a.entries, foldFixture()...), config.WorkFold
		a.entries[start+1].open = true
		a.entries[start+1].text = strings.Repeat("one two three four five six seven eight\n", 8)
		a.entries[start+5].settled = false
		a.live, a.turn, a.state = start+5, 1, stateWorking
		a.turnBegan = time.Unix(100, 0)
		a.clock = func() time.Time { return time.Unix(147, 0) }
		a.height = 10
		a.timestamps = "off"
		a.touch()
		return a
	}

	t.Run("following", func(t *testing.T) {
		a := makeApp()
		a.stick = true
		before, _ := a.window(a.bodyWidth(), a.viewHeight())
		answer := a.live
		a.settle()
		after, _ := a.window(a.bodyWidth(), a.viewHeight())
		rowOf := func(rows []row) int {
			for i, r := range rows {
				if r.entry == answer {
					return i
				}
			}
			return -1
		}
		beforeAt, afterAt := rowOf(before), rowOf(after)
		// Settlement adds the receipt below the answer. Relative to the live
		// edge excluding that new receipt, collapse itself has not moved it.
		if beforeAt < 0 || len(before)-1-beforeAt != len(after)-2-afterAt {
			t.Fatalf("the answer left the bottom anchor: before=%v after=%v", before, after)
		}
	})

	t.Run("scrolled", func(t *testing.T) {
		a := makeApp()
		a.stick = false
		a.offset = 5
		before, _ := a.window(a.bodyWidth(), a.viewHeight())
		a.settle()
		after, _ := a.window(a.bodyWidth(), a.viewHeight())
		if len(before) == 0 || len(after) == 0 || before[0].entry != after[0].entry {
			t.Fatalf("the scrolled row moved: before=%v after=%v offset=%d", before, after, a.offset)
		}
	})
}

// ── ONE WORD FOR ONE NUMBER ─────────────────────────────────────────────────

// ONE TURN COUNTED ITS CALLS IN TWO WORDS SIX ROWS APART: `2 tool calls` on the
// fold chip and `2 tools` on the receipt under the same turn — and the rewind
// sheet said `2 tools` for a third time. Two spellings of one number invite the
// reader to check whether they are two numbers. [toolCallWord] is the one
// spelling, and the noun is the CALL because that is what is counted.
func TestOneTurnCountsItsToolCallsInOneWord(t *testing.T) {
	a := newTestApp(&fakeAgent{model: "m"})
	a.entries, a.workMode = foldFixture(), config.WorkFold
	a.stamps = map[int]turnStamp{1: {took: 47 * time.Second, tools: 2, at: time.Unix(100, 0)}}
	a.timestamps = timestampsFooters
	a.touch()

	// The frame carries both readings of the same number: the fold chip over the
	// turn, and the receipt under it.
	frame := strings.Join(plainRows(a), "\n")
	if strings.Contains(frame, "2 tools") {
		t.Fatalf("the turn still counts its calls two ways:\n%s", frame)
	}
	if want, got := "2 tool calls", strings.Count(frame, "2 tool calls"); got != 2 {
		t.Fatalf("the chip and the receipt say %q %d times, want twice — one word for one number:\n%s",
			want, got, frame)
	}
	if receipt := plain(a.stampRow(1, 80)); !strings.Contains(receipt, "2 tool calls") {
		t.Fatalf("the receipt reads %q, want the same word the chip uses", receipt)
	}

	// And the singular is a sentence: `1 tool call`, never `1 tool calls`.
	if got := toolCallWord(1); got != "1 tool call" {
		t.Fatalf("one call is spelled %q, want %q", got, "1 tool call")
	}
	if got := toolCallWord(14); got != "14 tool calls" {
		t.Fatalf("fourteen calls are spelled %q, want %q", got, "14 tool calls")
	}
}
