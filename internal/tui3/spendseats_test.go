package tui3

// THE SEAT BLOCK: what a run's own tasks spent, rolled up by seat and drawn
// last on the spend page. Its reading is the plan store's rather than the
// machine-wide ledger's, so these tests hand the page a fake that answers
// PlanSpend the way the session does and stand it over a ledger that has
// something on it — the block is a second reading beside the ledger's, and it
// meets the page only where both are drawn.

import (
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/session"
)

// seatFake is [fakeAgent] widened by the plan-spend seam the spend page
// asserts: the seats a run's store rolled up, and the moment each read was
// asked for, so a test can prove the block was read over the window's own
// `since` and nothing else.
type seatFake struct {
	*fakeAgent
	seats []session.PlanSpendLine
	asked []time.Time
}

func (f *seatFake) PlanSpend(since time.Time) []session.PlanSpendLine {
	f.asked = append(f.asked, since)
	return f.seats
}

// seatLab is [spendLab] with the page's agent widened by the seat seam: the
// agent is swapped BEFORE the place is opened, so the one read the open makes
// is the one the fake answers.
func seatLab(t *testing.T, seats []session.PlanSpendLine) *app {
	t.Helper()
	a := placeApp(t)
	a.agent = &seatFake{fakeAgent: a.agent.(*fakeAgent), seats: seats}
	return spendLabOn(t, a, writeSpendLedger(t, spendFixture()))
}

// TWO SEATS DRAW TWO LINES, each with its own dollars and no nought: the money
// is this page's own column spelling ([spendMoneyWord]), so an amount under a
// cent is a cent rather than `$0.00`.
func TestSpendPageDrawsTaskSpendBySeat(t *testing.T) {
	a := seatLab(t, []session.PlanSpendLine{
		{Seat: "plan", Model: "vendor/plan-seat", USD: 1.35, Calls: 3},
		{Seat: "work", Model: "vendor/work-seat", USD: 0.40, Calls: 1},
	})
	text := placeFrameText(a)
	var rows []string
	for _, row := range strings.Split(text, "\n") {
		if strings.Contains(row, "plan-seat") || strings.Contains(row, "work-seat") {
			rows = append(rows, row)
		}
	}
	if len(rows) != 2 {
		t.Fatalf("the seat block drew %d lines, want 2:\n%s", len(rows), text)
	}
	if !strings.Contains(rows[0], "plan") || !strings.Contains(rows[0], "$1.35") {
		t.Errorf("the dearest seat line is %q, want the plan seat and its $1.35", rows[0])
	}
	if !strings.Contains(rows[1], "work") || !strings.Contains(rows[1], "$0.40") {
		t.Errorf("the second seat line is %q, want the work seat and its $0.40", rows[1])
	}
	if strings.Contains(text, "$0.00") {
		t.Errorf("the seat block drew a nought figure:\n%s", text)
	}
	// AND THE READ IS THE PAGE'S OWN WINDOW, the same `since` the ledger's
	// figures use rather than a fresh clock.
	fake := a.agent.(*seatFake)
	if len(fake.asked) == 0 || fake.asked[len(fake.asked)-1] != a.spend.win.From {
		t.Errorf("the seat block read over %#v, want the window's own %s", fake.asked, a.spend.win.From)
	}
}

// WITH NOTHING UNDER IT the block keeps its heading and one dim line naming
// what arrives there — never a sentence saying it is empty, and never a zero.
func TestSpendSeatBlockWhispersWhenNothingHasSpent(t *testing.T) {
	a := seatLab(t, nil)
	text := placeFrameText(a)
	lines := strings.Split(text, "\n")
	head := -1
	for i, row := range lines {
		if strings.TrimSpace(row) == spendSeatsWord {
			head = i
		}
	}
	if head < 0 {
		t.Fatalf("the block drew no heading %q:\n%s", spendSeatsWord, text)
	}
	if head+1 >= len(lines) || strings.TrimSpace(lines[head+1]) != spendSeatsWhisper {
		t.Fatalf("under the heading is %q, want the whisper %q", lines[min(head+1, len(lines)-1)], spendSeatsWhisper)
	}
}

// THE BLOCK SITS AFTER THE CREW BLOCK: the models table (with the role each
// model is bound to) above it says which model ANSWERS each seat, and the seat
// block below says what each seat has spent.
func TestSpendSeatBlockSitsAfterTheCrewBlock(t *testing.T) {
	a := seatLab(t, []session.PlanSpendLine{
		{Seat: "work", Model: "vendor/work-seat", USD: 0.40, Calls: 1},
	})
	// The by-model cut is the one that draws the crew block.
	a.stepSpendSlice(1)
	text := placeFrameText(a)
	crew, seats := -1, -1
	for i, row := range strings.Split(text, "\n") {
		if crew < 0 && strings.Contains(row, spendModelsWord) {
			crew = i
		}
		if strings.TrimSpace(row) == spendSeatsWord {
			seats = i
		}
	}
	if crew < 0 || seats < 0 {
		t.Fatalf("the crew block is at %d and the seat block at %d:\n%s", crew, seats, text)
	}
	if seats <= crew {
		t.Fatalf("the seat block stands at line %d, not after the crew block at %d:\n%s", seats, crew, text)
	}
}

// THE BLOCK IS READ OVER THE WINDOW THE PAGE DRAWS, and the window arrows move
// it. Every other figure on this page is cut from lines already in memory, so
// an arrow is arithmetic; the seat rollup arrives already summed over a window
// and a sum cannot be cut down to a narrower one, so the arrow asks again —
// over the window's own `since` and never over a fresh clock. A page that kept
// the old sum would draw a fortnight's seat dollars under a month's heading,
// which is a wrong figure on the one page a person reads to find one.
func TestSpendSeatBlockFollowsTheWindowArrows(t *testing.T) {
	a := seatLab(t, []session.PlanSpendLine{
		{Seat: "work", Model: "vendor/work-seat", USD: 0.40, Calls: 1},
	})
	fake := a.agent.(*seatFake)
	asked := func() time.Time {
		t.Helper()
		if len(fake.asked) == 0 {
			t.Fatal("the seat block was never read")
		}
		return fake.asked[len(fake.asked)-1]
	}
	if got := asked(); got != a.spend.win.From {
		t.Fatalf("walking in read over %s, want the window's own %s", got, a.spend.win.From)
	}
	reads, was := len(fake.asked), a.spend.win
	drive(t, a, key("shift+left"))
	if a.spend.win == was {
		t.Fatal("shift+← did not move the window")
	}
	if got := asked(); got != a.spend.win.From {
		t.Errorf("the arrow left the seat block read over %s, want the window it now draws %s", got, a.spend.win.From)
	}
	if len(fake.asked) == reads {
		t.Error("the arrow re-cut the seat rollup from the old window's sum rather than asking again")
	}
	// AND THE LEDGER'S OWN LAW STILL HOLDS: the lines in memory are not read
	// again, because the arrow's arithmetic over them is what lets a person hold
	// the key down.
	drive(t, a, key("shift+right"))
	if a.spend.win != was {
		t.Fatalf("shift+→ did not come back to %v", was)
	}
	if got := asked(); got != was.From {
		t.Errorf("coming back read over %s, want the window's own %s", got, was.From)
	}
}
