package api

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	"github.com/Agent-Field/codeaf/internal/factory"
)

func TestARubricTravelsWithItsSumAndBandAndComesBackWhole(t *testing.T) {
	r := &factory.Rubric{Scope: factory.Factor{Score: 2, Why: "three packages", Low: true}, Coupling: factory.Factor{Score: 2, Low: true},
		Proof: factory.Factor{Score: 1}, Novelty: factory.Factor{Score: 2}, From: factory.RubricFromRead}
	job := FromItem(factory.Item{ID: 1, Kind: factory.KindIssue, Triage: factory.Triage{Rubric: r}})
	raw, err := json.Marshal(job)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{`"rubric":{`, `"complexity":7`, `"band":"large"`, `"from":"read"`, `"why":"three packages","low":true`} {
		if !strings.Contains(string(raw), want) {
			t.Errorf("wire is missing %s", want)
		}
	}
	if strings.Contains(string(raw), `"est"`) {
		t.Error("the wire still carries a model's price")
	}
	if got := ToItem(job).Triage.Rubric; !reflect.DeepEqual(got, r) {
		t.Fatalf("round trip = %+v, want %+v", got, r)
	}
}

// A sum on a band's edge (3 or 4, 6 or 7) is one point from the next band,
// and two reads of one issue often land a point apart there, so the wire
// names that neighbour and the desk shows both bands.
func TestASumOnABandEdgeNamesTheNeighbouringBand(t *testing.T) {
	for sum, want := range map[int]string{0: "", 2: "", 3: factory.BandMedium, 4: factory.BandSmall, 5: "",
		6: factory.BandLarge, 7: factory.BandMedium, 8: "", 10: ""} {
		r := &factory.Rubric{Scope: factory.Factor{Score: min(sum, 2)}, Clarity: factory.Factor{Score: min(max(sum-2, 0), 2)},
			Coupling: factory.Factor{Score: min(max(sum-4, 0), 2)}, Proof: factory.Factor{Score: min(max(sum-6, 0), 2)},
			Novelty: factory.Factor{Score: min(max(sum-8, 0), 2)}, From: factory.RubricFromScout}
		raw, err := json.Marshal(FromItem(factory.Item{ID: 1, Kind: factory.KindIssue, Triage: factory.Triage{Rubric: r}}))
		if err != nil {
			t.Fatal(err)
		}
		has := strings.Contains(string(raw), `"band_near"`)
		if want == "" && has {
			t.Errorf("sum %d names a neighbour band: %s", sum, raw)
		}
		if want != "" && !strings.Contains(string(raw), `"band_near":"`+want+`"`) {
			t.Errorf("sum %d: wire is missing band_near %q: %s", sum, want, raw)
		}
	}
}
