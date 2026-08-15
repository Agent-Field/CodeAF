package plan

import (
	"strings"
	"sync/atomic"
	"testing"
)

var sixVendors = []Settlement{{
	Variable: "The six vendors compared",
	Values:   []string{"Acme", "Globex", "Initech", "Umbrella", "Soylent", "Hooli"},
}}

// TestWorthSplittingIsSilentWithoutAnEnumeration is the compatibility half: an
// ordinary goal binds nothing to several values, so nothing here fires and the
// fan-out accepts exactly what it always accepted.
func TestWorthSplittingIsSilentWithoutAnEnumeration(t *testing.T) {
	parts := []Node{{Title: "One"}, {Title: "Two"}}
	if refusal := worthSplitting(parts, false); refusal != "" {
		t.Errorf("a split with no enumeration in force was refused: %s", refusal)
	}
	if refusal := worthSplitting(parts[:1], true); refusal != "" {
		t.Errorf("a single part was refused: %s", refusal)
	}
}

func TestWorthSplittingRefusesEmptyAndOverlappingSources(t *testing.T) {
	for _, testcase := range []struct {
		name   string
		parts  []Node
		refuse string
	}{
		{
			name: "one unit each",
			parts: []Node{
				{Title: "Acme", Sources: []string{"Acme's pricing page"}},
				{Title: "Globex", Sources: []string{"Globex's pricing page"}},
			},
		},
		{
			name: "no unit at all",
			parts: []Node{
				{Title: "Part one", Sources: []string{"Acme's pricing page"}},
				{Title: "Part two"},
			},
			refuse: "names no unit",
		},
		{
			name: "every part unnamed",
			parts: []Node{
				{Title: "Vendor A"}, {Title: "Vendor B"}, {Title: "Vendor C"},
				{Title: "Vendor D"}, {Title: "Vendor E"}, {Title: "Vendor F"},
			},
			refuse: "names no unit",
		},
		{
			name: "two parts owning the same unit",
			parts: []Node{
				{Title: "First half", Sources: []string{"Acme's pricing page", "Globex's pricing page"}},
				{Title: "Second half", Sources: []string{"globex's PRICING page"}},
			},
			refuse: "both own",
		},
	} {
		t.Run(testcase.name, func(t *testing.T) {
			refusal := worthSplitting(testcase.parts, true)
			switch {
			case testcase.refuse == "" && refusal != "":
				t.Errorf("a disjoint split was refused: %s", refusal)
			case testcase.refuse != "" && !strings.Contains(refusal, testcase.refuse):
				t.Errorf("refusal = %q, want it to say %q", refusal, testcase.refuse)
			}
		})
	}
}

// TestFanOutRetriesAndAcceptsADisjointSplit is the acceptance path end to end:
// the first reply names no unit, one free retry is spent, and the split that
// names one unit each is taken.
func TestFanOutRetriesAndAcceptsADisjointSplit(t *testing.T) {
	var calls atomic.Int32
	client := &stubClient{reply: func(_, _ string) string {
		if calls.Add(1) == 1 {
			return `{"parts":[{"title":"Part one","summary":"profile a vendor","sources":[]},
			                  {"title":"Part two","summary":"profile a vendor","sources":[]}]}`
		}
		return `{"parts":[{"title":"Acme","summary":"profile Acme","sources":["Acme's pricing page"]},
		                  {"title":"Globex","summary":"profile Globex","sources":["Globex's pricing page"]}]}`
	}}
	nodes, usage, err := FanOutWith(t.Context(), client, "Goal:\ncompare them",
		[]Stage{{Title: "Profile", Summary: "profile each vendor"}}, sixVendors)
	if err != nil {
		t.Fatalf("FanOutWith: %v", err)
	}
	if calls.Load() != 2 {
		t.Fatalf("the stage made %d calls, want 2 — a refused split buys exactly one free retry", calls.Load())
	}
	if usage.Calls != 2 {
		t.Errorf("usage.Calls = %d, want 2 — the retry has to be billed", usage.Calls)
	}
	if len(nodes) != 2 || nodes[0].Title != "Acme" || nodes[1].Title != "Globex" {
		t.Fatalf("nodes = %#v, want the retry's split", nodes)
	}
}

// TestFanOutCollapsesASplitThatNamesNothing is the measured failure. Six parts
// with empty source lists were accepted, briefed identically and run six times;
// now the stage is left whole, which is the answer the prompt itself calls
// correct for a stage nobody could divide.
func TestFanOutCollapsesASplitThatNamesNothing(t *testing.T) {
	var calls atomic.Int32
	client := &stubClient{reply: func(_, _ string) string {
		calls.Add(1)
		return `{"parts":[{"title":"Vendor A","summary":"profile a vendor","sources":[]},
		                  {"title":"Vendor B","summary":"profile a vendor","sources":[]},
		                  {"title":"Vendor C","summary":"profile a vendor","sources":[]},
		                  {"title":"Vendor D","summary":"profile a vendor","sources":[]},
		                  {"title":"Vendor E","summary":"profile a vendor","sources":[]},
		                  {"title":"Vendor F","summary":"profile a vendor","sources":[]}]}`
	}}
	nodes, _, err := FanOutWith(t.Context(), client, "Goal:\ncompare six vendors",
		[]Stage{{Title: "Profiles", Summary: "profile each vendor"}}, sixVendors)
	if err != nil {
		t.Fatalf("FanOutWith: %v", err)
	}
	if calls.Load() != 2 {
		t.Fatalf("the stage made %d calls, want 2 — one attempt and one free retry", calls.Load())
	}
	if len(nodes) != 1 {
		t.Fatalf("nodes = %d, want the stage left whole rather than six identical parts", len(nodes))
	}
	if nodes[0].Title != "Profiles" || nodes[0].Undivided == "" {
		t.Errorf("the undivided node does not carry the stage or say why it stayed whole: %#v", nodes[0])
	}
}

// TestFanOutWithoutSettlementsIsUnchanged pins that the check costs nothing
// where nothing was enumerated: the same reply that is collapsed above is
// accepted here, one call, six parts, exactly as before this existed.
func TestFanOutWithoutSettlementsIsUnchanged(t *testing.T) {
	var calls atomic.Int32
	client := &stubClient{reply: func(_, _ string) string {
		calls.Add(1)
		return `{"parts":[{"title":"Vendor A","summary":"profile a vendor","sources":[]},
		                  {"title":"Vendor B","summary":"profile a vendor","sources":[]}]}`
	}}
	nodes, _, err := FanOut(t.Context(), client, "Goal:\nwrite something",
		[]Stage{{Title: "Draft", Summary: "draft it"}})
	if err != nil {
		t.Fatalf("FanOut: %v", err)
	}
	if calls.Load() != 1 || len(nodes) != 2 {
		t.Fatalf("calls = %d, nodes = %d — an ungrounded fan-out must be untouched", calls.Load(), len(nodes))
	}
}
