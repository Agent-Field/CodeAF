package verify

import (
	"reflect"
	"strings"
	"testing"
)

// happyDomStubRoster are the four names happy-dom's baseline reported and the gate
// cited as removed, verbatim from the delivery_gate event of
// bench/deepswe/results/happy-dom-deterministic-intersectionobserver-deepseek-deepseek-v4-flash-s13.
var happyDomStubRoster = []string{
	"IntersectionObserver disconnect() Does nothing",
	"IntersectionObserver observe() Does nothing",
	"IntersectionObserver takeRecords() Returns empty array",
	"IntersectionObserver unobserve() Does nothing",
}

// happyDomRewritten is the roster the run's own rewritten file reports, in the same
// spelling — the stubs replaced with real checks under the identical describe
// path.
var happyDomRewritten = []string{
	"IntersectionObserver disconnect() Does nothing when called without prior setup.",
	"IntersectionObserver disconnect() Clears every observed target.",
	"IntersectionObserver observe() Does nothing when called without prior setup.",
	"IntersectionObserver observe() Throws TypeError if target is not an Element.",
	"IntersectionObserver observe() Queues an initial entry for the target.",
	"IntersectionObserver takeRecords() Does nothing when called without prior setup.",
	"IntersectionObserver takeRecords() Returns pending records and clears them.",
	"IntersectionObserver unobserve() Does nothing when called without prior setup.",
	"IntersectionObserver unobserve() Stops future entries for the target.",
	"IntersectionObserver constructor Throws TypeError if callback is not a function.",
}

func TestTheSubjectIsTheParentPathAndTheLeadingCodeShapedTokens(t *testing.T) {
	for _, probe := range []struct {
		name    string
		subject string
		read    bool
	}{
		// A describe chain joined with spaces, which is what vitest's own
		// fullName is: the class and the call are the subject, and the sentence
		// after them is the clause.
		{"IntersectionObserver observe() Does nothing", "IntersectionObserver observe()", true},
		{"IntersectionObserver takeRecords() Returns empty array", "IntersectionObserver takeRecords()", true},
		{"IntersectionObserver rootMargin parsing Defaults to 0px.", "IntersectionObserver rootMargin", true},
		// A pytest node id: the file and the class are the path, and the leaf
		// is a sentence spelled in snake_case, which is not a code name.
		{"tests/test_observer.py::TestObserve::test_does_nothing", "tests/test_observer.py::TestObserve", true},
		{"tests/test_observer.py::test_does_nothing", "tests/test_observer.py", true},
		// A Go subtest, read the same way — and a whole Go test function, which
		// has no clause to drop and so is its own subject.
		{"github.com/x/y.TestObserve/does_nothing", "github.com/x/y.TestObserve", true},
		{"github.com/x/y.TestObserve", "github.com/x/y.TestObserve", true},
		{"TestObserve/does_nothing", "TestObserve", true},
		// A flat name carries no hierarchy at all: no separator, and a leaf
		// that opens with prose. There is nothing here to compare a sibling
		// against.
		{"the widget renders its children", "", false},
		{"returns empty array", "", false},
		// And a single leading code name with no separator behind it is the
		// top-level describe, which is a file and not a subject.
		{"IntersectionObserver Does nothing", "", false},
	} {
		subject, ok := CheckSubject(probe.name)
		if ok != probe.read || subject != probe.subject {
			t.Errorf("CheckSubject(%q) = %q, %v; want %q, %v",
				probe.name, subject, ok, probe.subject, probe.read)
		}
	}
}

// TestAStubReplacedUnderItsOwnDescribePathIsNotARemoval is the run this whole
// mechanism comes from: four stubs rewritten in place, zero removals.
func TestAStubReplacedUnderItsOwnDescribePathIsNotARemoval(t *testing.T) {
	removed, replaced := SplitReplaced(happyDomStubRoster, happyDomRewritten)
	if len(removed) != 0 {
		t.Fatalf("removed = %q; want none", removed)
	}
	if len(replaced) != len(happyDomStubRoster) {
		t.Fatalf("replaced %d checks; want %d", len(replaced), len(happyDomStubRoster))
	}
	if replaced[1].Before != "IntersectionObserver observe() Does nothing" {
		t.Fatalf("second replacement is of %q", replaced[1].Before)
	}
	for _, heir := range replaced[1].After {
		if !strings.HasPrefix(heir, "IntersectionObserver observe()") {
			t.Errorf("observe() was replaced by %q, which is not under its path", heir)
		}
	}
}

// TestAStubWithNoAfterSiblingIsStillRemoved is the other half, and it is the
// door this mechanism must not open: taking the failing test out is still the
// cheapest way there is to make a suite green.
func TestAStubWithNoAfterSiblingIsStillRemoved(t *testing.T) {
	gone := append([]string{"IntersectionObserver root() Does nothing"}, happyDomStubRoster...)
	removed, replaced := SplitReplaced(gone, happyDomRewritten)
	if !reflect.DeepEqual(removed, []string{"IntersectionObserver root() Does nothing"}) {
		t.Fatalf("removed = %q; want the one check nothing covers", removed)
	}
	if len(replaced) != len(happyDomStubRoster) {
		t.Fatalf("replaced %d checks; want %d", len(replaced), len(happyDomStubRoster))
	}
}

// TestAPytestNodeIdIsReplacedWithinItsClassAndRemovedOutsideIt reads the same
// rule off pytest's grammar rather than a describe chain's.
func TestAPytestNodeIdIsReplacedWithinItsClassAndRemovedOutsideIt(t *testing.T) {
	after := []string{
		"tests/test_observer.py::TestObserve::test_queues_an_initial_entry",
		"tests/test_observer.py::TestDisconnect::test_clears_targets",
	}
	removed, replaced := SplitReplaced([]string{
		"tests/test_observer.py::TestObserve::test_does_nothing",
		"tests/test_observer.py::TestUnobserve::test_does_nothing",
	}, after)
	if !reflect.DeepEqual(removed, []string{"tests/test_observer.py::TestUnobserve::test_does_nothing"}) {
		t.Fatalf("removed = %q; want the check whose class is gone", removed)
	}
	if len(replaced) != 1 || replaced[0].After[0] != after[0] {
		t.Fatalf("replaced = %+v", replaced)
	}
}

// TestFlatNamesKeepTodaysBehaviour is the fail-safe. A runner whose names carry
// no hierarchy — TAP prints a sentence and nothing else — has nothing for this
// to read, so the subtraction stands exactly as it always did.
func TestFlatNamesKeepTodaysBehaviour(t *testing.T) {
	removed, replaced := SplitReplaced(
		[]string{"the widget renders its children"},
		[]string{"the widget renders its children and their siblings"},
	)
	if !reflect.DeepEqual(removed, []string{"the widget renders its children"}) {
		t.Fatalf("removed = %q; want the flat name, unchanged", removed)
	}
	if len(replaced) != 0 {
		t.Fatalf("replaced = %+v; want none from a flat roster", replaced)
	}
}

// TestTheReadingReportsReplacementsInsteadOfRemovals is the same rule where the
// gate reaches it, through the pair of readings rather than through two lists.
func TestTheReadingReportsReplacementsInsteadOfRemovals(t *testing.T) {
	reading := Reading{
		Taken: true, AfterTaken: true,
		Before: Result{Reported: append(append([]string{}, happyDomStubRoster...),
			"IntersectionObserver root() Does nothing")},
		After: Result{Reported: happyDomRewritten},
	}
	if vanished := reading.Vanished(); !reflect.DeepEqual(vanished,
		[]string{"IntersectionObserver root() Does nothing"}) {
		t.Fatalf("Vanished() = %q; want only the check nothing covers", vanished)
	}
	if replaced := reading.Replaced(); len(replaced) != len(happyDomStubRoster) {
		t.Fatalf("Replaced() named %d; want %d", len(replaced), len(happyDomStubRoster))
	}
}
