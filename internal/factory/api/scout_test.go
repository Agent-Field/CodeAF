package api

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/factory"
)

func TestAJobCarriesItsScoutAndSaysWhenTheIssueMovedOn(t *testing.T) {
	at := time.Date(2026, 11, 2, 9, 0, 0, 0, time.UTC)
	it := factory.Item{ID: 3, Kind: factory.KindIssue, Title: "retry", Body: "back off"}
	s := &factory.Scout{Files: []factory.ScoutFile{{Path: "src/a.go", Why: "Retry lives here"}}, Plan: []string{"a", "b", "c"},
		Unknowns: []string{"Which limit?"}, Tests: []string{"src/a_test.go"}, Rubric: &factory.Rubric{From: factory.RubricFromScout},
		Cited: 5, Dropped: 1, Calls: 4, USD: 0.012, Model: "m", Took: 9 * time.Second, Body: factory.TextHash(it), Head: "abc", At: at}
	it.Scout = s
	job := FromItem(it)
	raw, _ := json.Marshal(job)
	for _, want := range []string{`"scout":{`, `"files":[{"path":"src/a.go","why":"Retry lives here"}]`, `"unknowns":["Which limit?"]`,
		`"cited":5`, `"dropped":1`, `"calls":4`, `"usd":0.012`, `"took_ms":9000`, `"head":"abc"`} {
		if !strings.Contains(string(raw), want) {
			t.Errorf("wire is missing %s in %s", want, raw)
		}
	}
	if job.Scout.Stale {
		t.Error("a scout of this very text reads as stale")
	}
	if got := ToItem(job).Scout; !reflect.DeepEqual(got, s) {
		t.Fatalf("round trip = %+v, want %+v", got, s)
	}
	it.Body = "back off, and say so"
	if !FromItem(it).Scout.Stale {
		t.Error("an edited issue's scout does not read as stale")
	}
}

func TestTheQueueQuotesWhatALookCloserTypicallyCosts(t *testing.T) {
	q := FromSnapshot(factory.Snapshot{ScoutPrice: 0.02}, "")
	if q.Desk.ScoutPrice != 0.02 {
		t.Fatalf("scout price = %v", q.Desk.ScoutPrice)
	}
}
