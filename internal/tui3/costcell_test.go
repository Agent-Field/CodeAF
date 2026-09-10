package tui3

// THE BILL'S OWN WIDTH, AND THE ROW THAT STANDS AROUND IT.
//
// The live status row keeps `$0.00` rather than drawing nothing, and that is the
// emptiness law's one sanctioned exception. It was granted for exactly one
// reason: so the segments on this row do not jump sideways while a person is
// reading them. It held the segment's PRESENCE still and let its WIDTH move —
// one ordinary turn walks the bill through `$0.00` (five cells), `<$0.0001`
// (eight), `$0.0052` (seven) and `$0.01` (five), and every step shoved what
// stood beside it two and three columns.
//
// So the fix is that law carried further and not a second exception: the figure
// is right-aligned in the room its own spellings need. Nothing new is drawn,
// nothing stands in for anything unknown, and `$0.00` is still the only zero.

import (
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/x/ansi"
)

// aTurnsBills is the ladder one turn actually climbs, in the order it climbs it:
// nothing spent, a cost too small for four places, a real sub-cent cost, the
// last sub-cent cost, the first whole cent, and a session that has run a while.
var aTurnsBills = []float64{0, 0.000004, 0.0052, 0.0099, 0.01, 0.47, 12.34}

// billNeighbour is the segment standing immediately AFTER the money on this
// row. It is what a widening bill shoves.
//
// It used to be the crew word in FRONT of the bill, because the whole telemetry
// cluster was flushed against the frame's right edge and a segment that grew
// pushed everything to its left. Since 2026-09-09 the ledger is laid from the
// LEFT edge and the bill is the first thing on it, so what a widening figure
// would shove is the meter behind it (foot.go).
const billNeighbour = "12.4k/128k"

// billEdgesIn is where the money segment's two edges fall on a real status row,
// in display cells: the column the figure itself ends at, and the column the
// segment AFTER it starts at.
//
// BOTH ARE ASSERTED. The figure is right-aligned inside a cell of fixed width
// ([costCell]), so its own end is what holds still as it grows from `<$0.0001`
// to `$12.34`; and the neighbour's start is what proves the CELL held its width
// rather than the figure merely being padded. A test that watched only one of
// them would pass against the very defect it is named for. It was written that
// way first and it did pass against the reverted fix; this is the rewrite.
func billEdgesIn(t *testing.T, row, figure string) (end, after int) {
	t.Helper()
	at := strings.Index(row, figure)
	if at < 0 {
		t.Fatalf("the status row does not carry the bill %q at all:\n%q", figure, row)
	}
	mark := strings.Index(row, billNeighbour)
	if mark < 0 {
		t.Fatalf("the status row is missing the segment after the bill (%q):\n%q", billNeighbour, row)
	}
	return ansi.StringWidth(row[:at+len(figure)]), ansi.StringWidth(row[:mark])
}

// THE MONEY SEGMENT HOLDS ONE WIDTH WHILE A TURN SPENDS, so nothing beside it
// moves while a person is reading it.
func TestTheMoneySegmentReservesTheRoomItWillNeedAndNeverShovesTheCluster(t *testing.T) {
	now := time.Date(2026, 9, 3, 9, 0, 0, 0, time.UTC)
	for _, width := range []int{160, 120} {
		var firstAfter, firstEnd int
		var firstBill float64
		var firstRow string
		for at, spent := range aTurnsBills {
			a := phaseApp(t, now)
			a.title = "porting the parser"
			// The meter is the segment behind the bill, and the thing a widening
			// figure would shove.
			a.ctxWindow, a.ctxTokens = 128_000, 12_400
			a.cost, a.shownCost = spent, spent
			row := plain(a.status(width))
			end, after := billEdgesIn(t, row, dollars(spent))
			if at == 0 {
				firstEnd, firstAfter, firstBill, firstRow = end, after, spent, row
				continue
			}
			if end != firstEnd || after != firstAfter {
				t.Fatalf("at %d columns the money segment ended at column %d with the meter at %d at $%.6f "+
					"and ended at column %d with the meter at %d at $%.6f, so the row moved around it:\n%q\n%q",
					width, firstEnd, firstAfter, firstBill, end, after, spent, firstRow, row)
			}
		}
	}
}

// AND THE ROOM IT RESERVES IS THE ROOM ITS OWN SPELLINGS NEED — the sub-cent
// floor's eight cells, asked of the function that prints it, and more only when
// the bill has genuinely grown past that.
func TestTheBillIsRightAlignedInTheRoomItsOwnSpellingsNeed(t *testing.T) {
	if want := ansi.StringWidth("<$0.0001"); costFloorCells != want {
		t.Fatalf("the reservation is %d cells and the sub-cent floor is %d — one source of truth, and they differ",
			costFloorCells, want)
	}
	for _, spent := range aTurnsBills {
		cell := costCell(spent)
		if cells := ansi.StringWidth(cell); cells != costFloorCells {
			t.Fatalf("$%.6f draws as %q, %d cells, in a segment reserving %d", spent, cell, cells, costFloorCells)
		}
		if strings.TrimLeft(cell, " ") != dollars(spent) {
			t.Fatalf("$%.6f draws as %q, which is not %q with room in front of it", spent, cell, dollars(spent))
		}
	}
	// A BILL BIGGER THAN THE RESERVATION TAKES WHAT IT NEEDS AND KEEPS IT: the
	// two things the room is the larger of never shrink, so the segment cannot
	// narrow again as the session goes on.
	big := costCell(123456.78)
	if want := "$123456.78"; big != want {
		t.Fatalf("a bill past the reservation draws as %q, want %q with no padding at all", big, want)
	}
	if ansi.StringWidth(big) <= costFloorCells {
		t.Fatalf("a bill of %q is meant to be wider than the %d-cell reservation", big, costFloorCells)
	}
	// AND THE ONE SANCTIONED ZERO IS UNTOUCHED. `$0.00` is still the figure, and
	// the emptiness law is neither extended nor repealed by giving it room.
	if got := strings.TrimSpace(costCell(0)); got != "$0.00" {
		t.Fatalf("a session that has spent nothing draws %q on the live line, want %q", got, "$0.00")
	}
}
