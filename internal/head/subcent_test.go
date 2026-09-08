package head

import (
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/aforge-v2/internal/store"
	"github.com/Agent-Field/aforge-v2/internal/tui2/tokens"
)

// #606 made a settled leaf's measured cost durable and put it in reach of the
// plan read, where this row rounded $0.00048 away beside three turns of real
// work. "$0.00" reads as free, not very small. The ordinary-price and unpriced
// controls belong in the same observation because rounding is not the only way
// to lie about money: making every row shout a figure would break the emptiness
// law in the other direction.
func TestASubCentLeafIsNotFreeInThePlanRead(t *testing.T) {
	graph := openHeadStore(t)
	seedPlannedMarket(t, graph)
	for _, priced := range []store.NodeUsage{
		{NodeID: "market-n1", Cost: 0.00048},
		{NodeID: "market-n2", Cost: 1.24},
	} {
		if err := graph.RecordUsage(priced); err != nil {
			t.Fatal(err)
		}
	}
	node := subcentNode(t, graph, "market")
	read, failed := New(nil, graph).renderPlanWithin(node, 0, 0)
	if failed {
		t.Fatalf("renderPlanWithin failed: %s", read)
	}
	if !strings.Contains(read, "$0.0005") || !strings.Contains(read, "$1.24") {
		t.Fatalf("the plan read lost a measured price:\n%s", read)
	}
	if hasZeroDollarFigure(read) {
		t.Fatalf("a real plan cost was rounded away to nothing:\n%s", read)
	}
	unpriced := subcentLine(t, read, "Write it up")
	if strings.Contains(unpriced, "$") || strings.Contains(unpriced, "| |") {
		t.Fatalf("an unpriced plan step grew a money segment: %q", unpriced)
	}
}

// The one-job result is a separate door for the same $0.00048 figure. The plan,
// result, and finished-window reads had three copies of the same format string,
// so a repair that reached only the reported plan row would leave this result
// saying that paid work was free; its ordinary and absent figures pin both
// directions of the money law here too.
func TestASubCentJobIsNotFreeInTheResultRead(t *testing.T) {
	graph := openHeadStore(t)
	for _, job := range []struct {
		id    string
		title string
		cost  float64
	}{
		{id: "subcent", title: "Sub-cent result", cost: 0.00048},
		{id: "control", title: "Control result", cost: 1.24},
		{id: "unpriced", title: "Unpriced result"},
	} {
		spliceSurgeryJob(t, graph, job.id, job.title, "write the result")
		completeNodeWith(t, graph, job.id, "The result is recorded.")
		if job.cost > 0 {
			if err := graph.RecordUsage(store.NodeUsage{NodeID: job.id, Cost: job.cost}); err != nil {
				t.Fatal(err)
			}
		}
	}
	head := New(nil, graph)
	subcent := subcentLine(t, head.renderResult(subcentNode(t, graph, "subcent")), "subcent")
	if !strings.Contains(subcent, "$0.0005") || hasZeroDollarFigure(subcent) {
		t.Fatalf("the sub-cent result header reads as free: %q", subcent)
	}
	control := subcentLine(t, head.renderResult(subcentNode(t, graph, "control")), "control")
	if !strings.Contains(control, "$1.24") {
		t.Fatalf("the ordinary result price moved: %q", control)
	}
	unpriced := subcentLine(t, head.renderResult(subcentNode(t, graph, "unpriced")), "unpriced")
	if strings.Contains(unpriced, "$") || strings.Contains(unpriced, "| |") {
		t.Fatalf("an unpriced result grew a money segment: %q", unpriced)
	}
}

// The finished-window history is the third door for the same $0.00048 figure.
// It had its own copy of the format string beside the plan and result reads, so
// it needs its own observation: fixing either neighbour must not leave settled
// work reading as free here, or turn an unpriced job into a zero-priced one.
func TestASubCentJobIsNotFreeInTheFinishedWindowRead(t *testing.T) {
	graph := openHeadStore(t)
	for _, job := range []struct {
		id    string
		title string
		cost  float64
	}{
		{id: "subcent", title: "Sub-cent history", cost: 0.00048},
		{id: "control", title: "Control history", cost: 1.24},
		{id: "unpriced", title: "Unpriced history"},
	} {
		spliceSurgeryJob(t, graph, job.id, job.title, "finish the history item")
		if job.cost > 0 {
			if err := graph.RecordUsage(store.NodeUsage{NodeID: job.id, Cost: job.cost}); err != nil {
				t.Fatal(err)
			}
		}
		completeNodeWith(t, graph, job.id, "The history item is recorded.")
	}
	lines, err := New(nil, graph).historyLines(time.Time{}, time.Time{})
	if err != nil {
		t.Fatal(err)
	}
	read := strings.Join(lines, "\n")
	subcent := subcentLine(t, read, "subcent")
	if !strings.Contains(subcent, "$0.0005") || hasZeroDollarFigure(read) {
		t.Fatalf("the finished-window read makes a real price look free:\n%s", read)
	}
	control := subcentLine(t, read, "control")
	if !strings.Contains(control, "$1.24") {
		t.Fatalf("the ordinary history price moved: %q", control)
	}
	unpriced := subcentLine(t, read, "unpriced")
	if strings.Contains(unpriced, "$") || strings.Contains(unpriced, "| |") {
		t.Fatalf("an unpriced history row grew a money segment: %q", unpriced)
	}
}

// The two money ladders deliberately part company outside these figures:
// moneyUSD(0.005) is "$0.0050" where tokens.Money(0.005) is "$0.01";
// moneyUSD(0.00009) is "under $0.0001" where tokens.Money(0.00009) is
// "$0.0001"; and moneyUSD(1234) is "$1234.00" where tokens.Money(1234) is
// "$1.2K". A table keeps this law on the figures they do share instead of
// inventing a range over spellings that intentionally differ.
func TestTheHeadSpellsMoneyTheWayTheSpendSurfaceDoes(t *testing.T) {
	for _, amount := range []float64{0.00048, 0.0017, 0.37, 18.4} {
		if got, want := moneyUSD(amount), tokens.Money(amount); got != want {
			t.Errorf("moneyUSD(%v) = %q, tokens.Money(%v) = %q", amount, got, amount, want)
		}
	}
}

func subcentNode(t *testing.T, graph *store.Store, id string) store.Node {
	t.Helper()
	node, found, err := graph.Node(id)
	if err != nil || !found {
		t.Fatalf("node %s found=%t err=%v", id, found, err)
	}
	return node
}

func subcentLine(t *testing.T, read, needle string) string {
	t.Helper()
	for _, line := range strings.Split(read, "\n") {
		if strings.Contains(line, needle) {
			return line
		}
	}
	t.Fatalf("read has no line containing %q:\n%s", needle, read)
	return ""
}

// The sub-cent spelling starts with the same five characters as zero, so the
// emptiness law is about a complete zero figure rather than that shared prefix.
var zeroDollarFigure = regexp.MustCompile(`\$0\.00(?:[^0-9]|$)`)

func hasZeroDollarFigure(read string) bool {
	return zeroDollarFigure.MatchString(read)
}
