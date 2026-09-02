package session

// What a gated node inherits, as tests: the reports of the work before it
// share the room the task's own brief leaves, they are never dropped, and a
// second fit on the division road does not stack a second mark.

import (
	"fmt"
	"strings"
	"testing"
)

// inheritBrief is [TaskGraph.inheritedLocked] against a scripted sink: the
// node's own brief, and one finished prerequisite per report, in order. No
// frontier, no runner — this door is a function of the reports already in
// hand.
func inheritBrief(own string, reports []string) string {
	graph := newTaskGraph()
	sink := &TaskNode{
		graph: graph,
		id:    uint64(len(reports) + 1),
		spec:  taskSpec{title: "the sink", brief: own},
	}
	graph.nodes[sink.id] = sink
	for index, report := range reports {
		id := uint64(index + 1)
		graph.nodes[id] = &TaskNode{
			graph:  graph,
			id:     id,
			spec:   taskSpec{title: fmt.Sprintf("leaf %d", id)},
			report: report,
			state:  TaskDone,
		}
		sink.dependsOn = append(sink.dependsOn, id)
	}
	return graph.inheritedLocked(sink)
}

// ownBriefOf is the sink's own brief as inheritedLocked left it: everything
// above the learned heading, which is the one thing that law says is never
// cut.
func ownBriefOf(inherited string) string {
	own, _, _ := strings.Cut(inherited, "\n\n"+inheritedLearnedLead)
	return own
}

func TestInheritedLockedBoundsPrerequisiteReports(t *testing.T) {
	const own = "ASK: write the table\nWORK: rank the six\nPRODUCE: languages.md\nDONE WHEN: six rows"
	long := strings.Repeat("finding the shape of the bug in absorb.go. ", 400)

	for _, test := range []struct {
		name    string
		reports []string
		check   func(t *testing.T, inherited string)
	}{
		{
			name:    "a single very long report is clipped and marked",
			reports: []string{long},
			check: func(t *testing.T, inherited string) {
				if !strings.Contains(inherited, inheritedLearnedHeading(1)) {
					t.Fatalf("the heading does not name the one report: %q", inherited)
				}
				if !strings.Contains(inherited, "…") {
					t.Fatalf("a clipped report carries no mark: %q", inherited)
				}
				if strings.Contains(inherited, long) {
					t.Fatalf("the long report was handed over whole")
				}
				if !strings.Contains(inherited, long[:inheritedReportFloor]) {
					t.Fatalf("the clipped report lost its opening: %q", inherited)
				}
			},
		},
		{
			name: "reports that fit are passed through unclipped",
			reports: []string{
				"the reconciler writes through absorb.go",
				"the tests already cover the nil map",
			},
			check: func(t *testing.T, inherited string) {
				if strings.Contains(inherited, "…") {
					t.Fatalf("a report that fitted was marked: %q", inherited)
				}
				for _, report := range []string{
					"the reconciler writes through absorb.go",
					"the tests already cover the nil map",
				} {
					if !strings.Contains(inherited, report) {
						t.Fatalf("a fitting report was changed: missing %q in %q", report, inherited)
					}
				}
			},
		},
		{
			name: "the task's own brief is never cut",
			reports: []string{
				strings.Repeat("alpha ", 2000),
				strings.Repeat("bravo ", 2000),
				strings.Repeat("charlie ", 2000),
			},
			check: func(t *testing.T, inherited string) {
				if got := ownBriefOf(inherited); got != own {
					t.Fatalf("the own brief moved:\n got %q\nwant %q", got, own)
				}
			},
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			inherited := inheritBrief(own, test.reports)
			if got := ownBriefOf(inherited); got != own {
				t.Fatalf("the own brief was cut:\n got %q\nwant %q", got, own)
			}
			test.check(t, inherited)
		})
	}
}

// THE FANIN6 SHAPE. Six leaves into one sink, each report far past the bound.
// A 4 KiB pot once fed this sink four of six sections and it wrote a confident
// four-row table. Every report must still be present, each at a share a worker
// can read, and the heading must say there are six.
func TestFanin6InheritedReportsAreAllRepresented(t *testing.T) {
	const own = "rank the six languages in one table"
	subjects := []string{"Rust", "Zig", "Elixir", "Haskell", "OCaml", "Erlang"}
	reports := make([]string, len(subjects))
	for i, name := range subjects {
		reports[i] = name + " concurrency: " + strings.Repeat("the runtime story goes on. ", 300)
	}

	inherited := inheritBrief(own, reports)
	if got := ownBriefOf(inherited); got != own {
		t.Fatalf("the own brief was cut:\n got %q\nwant %q", got, own)
	}
	if !strings.Contains(inherited, inheritedLearnedHeading(6)) {
		t.Fatalf("the heading does not say there are six reports: %q", inherited)
	}

	bodies := inherited
	if _, rest, ok := strings.Cut(inherited, inheritedLearnedHeading(6)); ok {
		bodies = rest
	}
	for i, name := range subjects {
		if !strings.Contains(bodies, name) {
			t.Fatalf("leaf %d (%s) was dropped from the inherited brief:\n%s", i+1, name, inherited)
		}
		header := fmt.Sprintf("leaf %d (task %d):", i+1, i+1)
		if !strings.Contains(bodies, header) {
			t.Fatalf("leaf %d lost its heading %q:\n%s", i+1, header, inherited)
		}
	}

	// Each report's body — between its heading and the next, or the end — is
	// a share a worker can act on, never a stub the model reads past.
	for i := range subjects {
		header := fmt.Sprintf("leaf %d (task %d):\n", i+1, i+1)
		_, rest, ok := strings.Cut(bodies, header)
		if !ok {
			t.Fatalf("leaf %d has no body under its heading", i+1)
		}
		body, _, _ := strings.Cut(rest, "\n\nleaf ")
		if len(body) < inheritedReportFloor && !strings.Contains(reports[i], body) {
			t.Fatalf("leaf %d was starved to %d bytes, under the floor %d: %q",
				i+1, len(body), inheritedReportFloor, body)
		}
		if body == "" {
			t.Fatalf("leaf %d was starved to nothing", i+1)
		}
	}

	// And a second fit — the division road's [familyOf] — must not stack a
	// mark on a cut that already carried one.
	if second := fit(inherited, len(inherited)/2); strings.Contains(second, "……") {
		t.Fatalf("a second fit stacked a second mark:\n%s", second)
	}
}

// When the own brief has already spent the bound, N times the floor will not
// fit in what remains. The bound yields: every report is still there, each at
// the floor, and the own brief is still byte-for-byte the one that was admitted.
func TestInheritedReportsStillAppearWhenTheOwnBriefFillsTheBound(t *testing.T) {
	own := strings.Repeat("W", taskShapeBriefLimit)
	subjects := []string{"Rust", "Zig", "Elixir", "Haskell", "OCaml", "Erlang"}
	reports := make([]string, len(subjects))
	for i, name := range subjects {
		reports[i] = name + " " + strings.Repeat("x", 2000)
	}
	inherited := inheritBrief(own, reports)
	if got := ownBriefOf(inherited); got != own {
		t.Fatalf("the own brief was cut even though it already filled the bound")
	}
	if !strings.Contains(inherited, inheritedLearnedHeading(6)) {
		t.Fatalf("the heading does not say there are six reports: %q", inherited)
	}
	for _, name := range subjects {
		if !strings.Contains(inherited, name) {
			t.Fatalf("%s vanished when the bound had to yield: %q", name, inherited)
		}
	}
}

func TestClipMovesATrailingMarkRatherThanStackingOne(t *testing.T) {
	already := clip(strings.Repeat("finding ", 80), 40)
	if !strings.HasSuffix(already, "…") {
		t.Fatalf("the first clip did not mark: %q", already)
	}
	// Room that keeps the existing mark and then wants to mark again is the
	// double-fit that used to print `……`: the cut lands on the old mark.
	second := clip(already+"xxxx", len(already)+len("…"))
	if strings.Contains(second, "……") {
		t.Fatalf("clip stacked a second mark: %q", second)
	}
	if strings.Count(second, "…") != 1 {
		t.Fatalf("want one mark, got %q", second)
	}
}
