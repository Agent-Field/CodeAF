package plan

import (
	"encoding/json"
	"strings"
	"sync/atomic"
	"testing"
)

// TestSettlementRendersTheBoundValues is the typed settlement's whole claim: the
// variable and what it is bound to are separate fields, and they render as the
// one line the frozen preamble has always carried.
func TestSettlementRendersTheBoundValues(t *testing.T) {
	for _, testcase := range []struct {
		name       string
		settlement Settlement
		want       string
		bound      bool
	}{
		{
			name:       "an enumeration",
			settlement: Settlement{Variable: "The three cities", Values: []string{"Berlin", "Lisbon", "Warsaw"}},
			want:       "The three cities: Berlin, Lisbon, Warsaw",
			bound:      true,
		},
		{
			name:       "a single value",
			settlement: Settlement{Variable: "The period covered", Values: []string{"2023-2025"}},
			want:       "The period covered: 2023-2025",
			bound:      true,
		},
		{
			name:       "a variable nobody bound",
			settlement: Settlement{Variable: "The vendors compared"},
			want:       "The vendors compared",
			bound:      false,
		},
	} {
		t.Run(testcase.name, func(t *testing.T) {
			if got := testcase.settlement.Line(); got != testcase.want {
				t.Errorf("Line() = %q, want %q", got, testcase.want)
			}
			if got := testcase.settlement.Bound(); got != testcase.bound {
				t.Errorf("Bound() = %v, want %v", got, testcase.bound)
			}
		})
	}
}

// TestSettlementDecodesTheOlderSpelling is the compatibility half. A graph
// written before the schema was typed carries settled points as bare strings,
// and it is loaded and re-planned long after it was written.
func TestSettlementDecodesTheOlderSpelling(t *testing.T) {
	var graph Graph
	const document = `{"goal":"ship it","settled":["The three cities are Berlin, Lisbon and Warsaw."],"open":["which wins"]}`
	if err := json.Unmarshal([]byte(document), &graph); err != nil {
		t.Fatalf("a legacy graph no longer loads: %v", err)
	}
	if len(graph.Settled) != 1 {
		t.Fatalf("settled = %#v, want one point", graph.Settled)
	}
	if got, want := graph.Settled[0].Line(), "The three cities are Berlin, Lisbon and Warsaw."; got != want {
		t.Errorf("a legacy point renders as %q, want %q unchanged", got, want)
	}
	if Enumerated(graph.Settled) {
		t.Error("a legacy point claims an enumeration it never carried, which would arm the fan-out check on it")
	}
}

// TestSettlementRoundTripsThroughTheGraph pins that a typed settlement survives
// the file a plan is persisted to.
func TestSettlementRoundTripsThroughTheGraph(t *testing.T) {
	original := &Graph{Goal: "compare them", NextID: 1, Settled: []Settlement{
		{Variable: "The six vendors", Values: []string{"a", "b", "c", "d", "e", "f"}},
	}}
	original.Add(Node{Stage: 1, Kind: KindWork, Title: "Profile", Summary: "profile them"})
	encoded, err := original.JSON()
	if err != nil {
		t.Fatal(err)
	}
	reloaded, err := Load(encoded)
	if err != nil {
		t.Fatal(err)
	}
	if len(reloaded.Settled) != 1 || len(reloaded.Settled[0].Values) != 6 {
		t.Fatalf("settled did not survive the round trip: %#v", reloaded.Settled)
	}
	if !Enumerated(reloaded.Settled) {
		t.Error("the reloaded graph lost the enumeration the fan-out check is keyed on")
	}
}

// TestGroundRefusesAPointThatBindsNothing is the mode-collapse fix at its
// source. A settled list of tautologies used to reach the fan-out looking
// exactly like a binding; now it is a schema-level miss, it buys one free retry,
// and what still binds nothing does not enter the graph.
func TestGroundRefusesAPointThatBindsNothing(t *testing.T) {
	var calls atomic.Int32
	client := &stubClient{reply: func(system, _ string) string {
		if !strings.Contains(system, "You settle what a goal leaves unsaid") {
			return `{}`
		}
		if calls.Add(1) == 1 {
			return `{"settled":[{"variable":"The vendors compared","values":[]}],"open":[],"evidence":"Read and cite."}`
		}
		return `{"settled":[{"variable":"The vendors compared","values":["Acme","Globex","Initech"]}],
		         "open":[],"evidence":"Read and cite."}`
	}}
	grounding, usage, err := Ground(t.Context(), client, "compare the leading vendors")
	if err != nil {
		t.Fatalf("Ground: %v", err)
	}
	if calls.Load() != 2 {
		t.Fatalf("the ground pass made %d calls, want 2 — an unbound point buys exactly one free retry", calls.Load())
	}
	if usage.Calls != 2 {
		t.Errorf("usage.Calls = %d, want 2 — the retry has to be billed", usage.Calls)
	}
	if len(grounding.Settled) != 1 || len(grounding.Settled[0].Values) != 3 {
		t.Fatalf("settled = %#v, want the retry's enumeration", grounding.Settled)
	}
}

// TestGroundDropsAPointThatStillBindsNothing covers the retry that does not
// help. The tautology is dropped rather than rendered, because a point that
// binds nothing in the shared preamble is the ambiguity handed back to every
// parallel call at once.
func TestGroundDropsAPointThatStillBindsNothing(t *testing.T) {
	client := &stubClient{reply: func(_, _ string) string {
		return `{"settled":[{"variable":"The vendors compared","values":[]},
		                    {"variable":"The period","values":["2025"]}],
		         "open":[],"evidence":"Read and cite."}`
	}}
	grounding, _, err := Ground(t.Context(), client, "compare the leading vendors")
	if err != nil {
		t.Fatalf("Ground: %v", err)
	}
	if len(grounding.Settled) != 1 || grounding.Settled[0].Variable != "The period" {
		t.Fatalf("settled = %#v, want only the point that actually bound something", grounding.Settled)
	}
	graph := &Graph{Goal: "compare the leading vendors", Settled: grounding.Settled}
	if strings.Contains(graph.context(), "The vendors compared") {
		t.Error("a point that binds nothing reached the frozen preamble every parallel call shares")
	}
}

// TestGroundSchemaTypesTheSettledPoint pins the schema itself, because it is the
// half of this fix that no amount of prompting replaces.
func TestGroundSchemaTypesTheSettledPoint(t *testing.T) {
	schema := string(groundSchema)
	for _, want := range []string{`"variable"`, `"values"`, `"required": ["variable", "values"]`} {
		if !strings.Contains(schema, want) {
			t.Errorf("ground schema no longer types the settled point: missing %s\n%s", want, schema)
		}
	}
}
