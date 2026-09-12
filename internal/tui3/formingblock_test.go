package tui3

import (
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/x/ansi"
)

// THE FORMING BLOCK BETWEEN A PROPOSAL'S YES AND ITS TASK.
//
// One claim, and every test here is a corner of it: the seconds between an
// approved proposal and the task existing are drawn as one small live block at
// the transcript tail — a name, a phase and a clock — and one block rather than
// a stack when several proposals are forming at once. Nothing on it opens,
// walks or answers a press: the preview that did went with the typed `/task`
// road that fed it (#936).

// formingApp is a surface with one approved proposal forming per name, and
// nothing else going on.
func formingApp(t *testing.T, names ...string) *app {
	t.Helper()
	a := newTestApp(&taskCommandFake{Agent: &fakeAgent{model: "m"}})
	base := time.Now()
	a.clock = func() time.Time { return base }
	for i, name := range names {
		approveProposal(a, uint64(41+i), name)
	}
	a.clock = func() time.Time { return base.Add(13 * time.Second) }
	if len(a.waits) != len(names) {
		t.Fatalf("%d approvals raised %d forming blocks", len(names), len(a.waits))
	}
	return a
}

// THE BLOCK IS THREE ROWS AND NOTHING ON IT IS A DOOR. The head, the card's
// name and the phase with its clock — and no row carries a hit, because there is
// no preview behind any of them to open and no wait among several to choose.
func TestTheFormingBlockIsThreeRowsAndNothingToOpen(t *testing.T) {
	a := formingApp(t, "adapter index")
	rows := a.preflightRows(72)
	if len(rows) != 3 {
		t.Fatalf("the block drew %d rows, want 3:\n%s", len(rows), plainRowsText(rows))
	}
	body := plainRowsText(rows)
	for _, want := range []string{"▏ task", "▏ adapter index", taskShapingNote, "· 13s"} {
		if !strings.Contains(body, want) {
			t.Fatalf("the block lost %q:\n%s", want, body)
		}
	}
	for _, r := range rows {
		if r.hit != hitNone || r.entry != -1 {
			t.Fatalf("a row of the block offers a door: %q (hit %v, entry %d)", plain(r.text), r.hit, r.entry)
		}
	}
}

// TWO PROPOSALS FORMING ARE ONE BLOCK: a head that counts them and one compact
// row each, every one with its own spinner and clock. Two three-row blocks
// stacked at the transcript tail would be a wall.
func TestSeveralTasksFormingShareOneBlock(t *testing.T) {
	a := newTestApp(&taskCommandFake{Agent: &fakeAgent{model: "m"}})
	base := time.Now()
	a.clock = func() time.Time { return base }
	approveProposal(a, 41, "adapter index")
	a.clock = func() time.Time { return base.Add(5 * time.Second) }
	approveProposal(a, 42, "nil-map crash")
	a.clock = func() time.Time { return base.Add(13 * time.Second) }

	rows := a.preflightRows(72)
	body := plainRowsText(rows)
	if !strings.Contains(body, "tasks · 2 forming") {
		t.Fatalf("two waits drew no head that counts them:\n%s", body)
	}
	if len(rows) != 1+2 {
		t.Fatalf("two waits drew %d rows, want a head and two rows:\n%s", len(rows), body)
	}
	if !strings.Contains(plain(rows[1].text), "adapter index · 13s") ||
		!strings.Contains(plain(rows[2].text), "nil-map crash · 8s") {
		t.Fatalf("each row does not carry its own name and clock:\n%s", body)
	}
	if strings.Contains(body, taskShapingNote) {
		t.Fatalf("the plural block repeats the phase on every row:\n%s", body)
	}
	for _, r := range rows {
		if r.hit != hitNone {
			t.Fatalf("a row of the plural block offers a door: %q", plain(r.text))
		}
	}
}

// WITH EXACTLY ONE TASK FORMING, NONE OF THE PLURAL MACHINERY DRAWS. No head
// that counts — the block is the singular one. It is the emptiness law read at
// the shape of a block rather than at a number.
func TestOneTaskFormingDrawsNoneOfThePluralMachinery(t *testing.T) {
	a := formingApp(t, "adapter index")
	body := plainRowsText(a.preflightRows(72))
	if strings.Contains(body, formingHeadWord) || strings.Contains(body, formingStateWord) {
		t.Fatalf("a single wait drew the head that counts several:\n%s", body)
	}
	if !strings.HasPrefix(body, "▏ task\n") {
		t.Fatalf("the single block is not the block it has always been:\n%s", body)
	}
}

// AND A SETTLE FINDS ITS OWN BLOCK. Two proposals are forming and one task's
// first update arrives; the other one is still forming and must still be on
// screen, saying so.
func TestASettleCollapsesItsOwnWaitAndNoOther(t *testing.T) {
	a := formingApp(t, "adapter index", "nil-map crash")
	a.settleProposalWait(41)
	if len(a.waits) != 1 {
		t.Fatalf("one settle left %d waits, want 1", len(a.waits))
	}
	if a.waits[0].name != "nil-map crash" {
		t.Fatalf("the settle collapsed the wrong wait: %q", a.waits[0].name)
	}
	// AND THE BLOCK IS THE SINGULAR ONE AGAIN, because there is one of them.
	if body := plainRowsText(a.preflightRows(72)); strings.Contains(body, formingHeadWord) {
		t.Fatalf("a block of one still counts:\n%s", body)
	}
}

// NOTHING OVERFLOWS AND NOTHING WRAPS ONTO A ROW OF ITS OWN, at any width the
// surface is drawn at, in either shape of the block.
func TestTheFormingBlockIsCutToEveryWidth(t *testing.T) {
	long := "an adapter index whose name goes on well past any phone's width"
	for _, width := range []int{phoneWidth, 30, 40, 60, 100} {
		for _, names := range [][]string{{long}, {long, "nil-map crash"}} {
			a := formingApp(t, names...)
			rows := a.preflightRows(width)
			want := 3
			if len(names) > 1 {
				want = 1 + len(names)
			}
			if len(rows) != want {
				t.Fatalf("width %d with %d forming drew %d rows, want %d:\n%s",
					width, len(names), len(rows), want, plainRowsText(rows))
			}
			for i, r := range rows {
				line := plain(r.text)
				if got := ansi.StringWidth(line); got > width {
					t.Fatalf("width %d row %d is %d cells: %q", width, i, got, line)
				}
				if !strings.HasPrefix(line, formingRail) {
					t.Fatalf("width %d row %d lost the hairline: %q", width, i, line)
				}
			}
		}
	}
}
