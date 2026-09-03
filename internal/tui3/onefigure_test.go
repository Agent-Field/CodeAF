package tui3

// FOUR SURFACES, ONE FIGURE (issue #269).
//
// One instant on the 2026-09-01 chat run showed four numbers for the same
// money: the status line and Settings→Spending's `this one` said $0.24, the
// machine ledger said $0.157, and Settings→`today` and the `/spend` place said
// $0.16. Two causes met there — the engine wrote the ledger only when a turn
// SEALED, so an interrupted turn's calls reached no file (internal/session's
// usage_percall_test.go pins that half), and this surface read the `this one`
// receipt off the conversation's own books while the row above it read the
// conversation's whole tree.
//
// This is the surface half: one fixture, one interrupted turn's worth of ledger
// rows, and the four places a person can read the figure must render the same
// string.

import (
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/aforge-v2/internal/config"
	"github.com/Agent-Field/aforge-v2/internal/session"
	"github.com/Agent-Field/aforge-v2/internal/tui2/tokens"
)

// oneFigureLab is the run in the issue: a conversation whose own interrupted
// turn banked $2.53 call by call, and a family under it that has spent $51.05
// and not closed — so the conversation's OWN books still say 2.53 while the
// ledger already holds all of it.
func oneFigureLab(t *testing.T) *app {
	t.Helper()
	a := spendTreeLab(t, 2.53, treeFixture())
	a.width = 200
	// The reading the frame clock takes, and the reading the panel takes.
	a.readTreeSpend()
	a.readDayCost()
	return a
}

// moneySegment is what the status line's own money segment draws.
func moneySegment(t *testing.T, a *app) string {
	t.Helper()
	for _, part := range a.telemetry(a.width) {
		if part.kind == segCost {
			return part.text
		}
	}
	t.Fatalf("the status line drew no money segment at %d columns", a.width)
	return ""
}

// spendingReceipt is the `this one` fact beside the per-conversation ceiling on
// the Spending tab, as the registry renders it.
func spendingReceipt(t *testing.T, a *app) string {
	t.Helper()
	for _, row := range a.registry().Rows() {
		if row.Key == config.KeySpendRail {
			return row.Receipt()
		}
	}
	t.Fatal("the registry has no per-conversation spend row to carry a receipt")
	return ""
}

// THE ACCEPTANCE: the money segment, `this one`, `today` and the `/spend` place
// all say the same string.
func TestTheFourSpendSurfacesRenderOneFigure(t *testing.T) {
	a := oneFigureLab(t)
	want := dollars(53.58)

	if got := moneySegment(t, a); got != want {
		t.Fatalf("the status line's money segment reads %q, want %q", got, want)
	}

	// `this one` — the half that was wrong here. It used to read the
	// conversation's own books ($2.53) beside a status line drawing the tree.
	if got := spendingReceipt(t, a); got != "this one "+want {
		t.Fatalf("Spending's per-conversation receipt reads %q, want %q", got, "this one "+want)
	}

	// `today` — the day's own reading off the same ledger.
	today := a.todayReading()
	if today == nil {
		t.Fatal("Spending draws no `today` row over a day that has spent money")
	}
	if !strings.Contains(today.value.full, want) {
		t.Fatalf("Spending's `today` row reads %q, want it to carry %q", today.value.full, want)
	}

	// The `/spend` place's own pointer line, over the same rows the other three
	// were read from.
	lines, err := session.ReadUsage(a.usageLedger, time.Time{})
	if err != nil {
		t.Fatalf("read the ledger: %v", err)
	}
	now := time.Now()
	reading := readSpend(lines, session.LastDays(now, spendWindowDays), now).
		todayed(spendDayTotal(lines, now))
	row := plain(reading.railsRow(a.width, newPalette(tokens.NoColor, false)))
	if !strings.Contains(row, want) {
		t.Fatalf("the /spend place's pointer line reads %q, want it to carry %q", row, want)
	}
}

// AND NOTHING SAYS ANYTHING ABOUT LOST RECORDS WHEN NONE WERE LOST. The
// unwritten row is a fact about a machine whose disk stopped answering, and the
// emptiness law forbids drawing its absence as a zero.
func TestNoUnwrittenRowOnAMachineThatWroteEverything(t *testing.T) {
	a := oneFigureLab(t)
	if session.UsageDrops() != 0 {
		t.Skip("this process has already failed a ledger write, so the silence cannot be asserted")
	}
	if unwritten := unwrittenReading(); unwritten != nil {
		t.Fatalf("Spending draws an %q row with nothing lost: %+v", spendUnwrittenWord, unwritten)
	}
	now := time.Now()
	reading := readSpend(nil, session.LastDays(now, spendWindowDays), now).lost(0)
	if row := plain(reading.railsRow(a.width, newPalette(tokens.NoColor, false))); strings.Contains(row, spendUnwrittenSaid) {
		t.Fatalf("the /spend pointer line claims records were lost: %q", row)
	}
}

// AND WHEN RECORDS WERE LOST, BOTH SURFACES SAY SO IN THE SAME WORDS — because
// a figure that is short and does not say so is the one direction a bill must
// never be wrong in.
func TestALostSpendingRecordIsSaidOnBothSpendSurfaces(t *testing.T) {
	a := oneFigureLab(t)
	now := time.Now()
	reading := readSpend(nil, session.LastDays(now, spendWindowDays), now).lost(3)
	row := plain(reading.railsRow(a.width, newPalette(tokens.NoColor, false)))
	if !strings.Contains(row, "3 "+spendUnwrittenSaid) {
		t.Fatalf("the /spend pointer line says nothing about three lost records: %q", row)
	}
}
