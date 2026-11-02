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
