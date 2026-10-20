package api

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	"github.com/Agent-Field/codeaf/internal/factory"
)

// workerJobItem is a run whose phases stand out of the pipeline's order and
// include one the pipeline has dropped, so a report that went to the wrong
// step would show.
func workerJobItem() factory.Item {
	result := func(word string) *factory.StageResult {
		var r factory.StageResult
		fill(reflect.ValueOf(&r).Elem(), word, 0)
		return &r
	}
	return factory.Item{ID: 1, State: factory.StateRunning,
		Stages: []factory.Stage{{Name: "a", On: true}, {Name: "b", On: true}, {Name: "c"}},
		Stream: &factory.Stream{Cur: 1, Phases: []factory.Phase{
			{Name: "b", State: factory.PhaseDone, Result: result("b")},
			{Name: "x", State: factory.PhaseRunning},
			{Name: "a", State: factory.PhaseDone, Result: result("a")},
		}}}
}

// THE WORKER'S PULL CARRIES EVERY STEP'S WHOLE REPORT: what a runner taking
// the job on rebuilds each earlier step from comes back field for field, its
// done, findings, claims, notes, exit and chat among them, each on its own
// step. What stays behind is named in [stayBehind], and a field added to
// StageResult fails here until it crosses or is named there.
func TestTheWorkerPullCarriesEveryStepsWholeReport(t *testing.T) {
	it := workerJobItem()
	raw, err := json.Marshal(WorkerPull{Job: WorkerJob(it)})
	if err != nil {
		t.Fatal(err)
	}
	var pulled WorkerPull
	if err := json.Unmarshal(raw, &pulled); err != nil {
		t.Fatal(err)
	}
	back := ToItem(pulled.Job)
	for i, ph := range it.Stream.Phases {
		got := back.Stream.Phases[i]
		var want *factory.StageResult
		if ph.Result != nil {
			w := *ph.Result
			for field := range stayBehind {
				f := reflect.ValueOf(&w).Elem().FieldByName(field)
				if !f.IsValid() {
					t.Fatalf("stayBehind names StageResult.%s, which does not exist", field)
				}
				f.Set(reflect.Zero(f.Type()))
			}
			want = &w
		}
		if got.Name != ph.Name || !reflect.DeepEqual(got.Result, want) {
			t.Errorf("phase %d (%s) came back with %+v, want %+v", i, ph.Name, got.Result, want)
		}
	}
}

// stayBehind are the StageResult fields a pull does not carry, and why. Each
// is read only where the round is folded, on the runner that ran it, and what
// a person reads of it crosses already as the round's summary line
// (steps[].rounds). They cannot simply be added to StepResult: that shape is
// also a worker's report, which a factory older than a field refuses whole.
var stayBehind = map[string]string{
	"Edit":     "a plan's proposal is applied as its round ends, and the kept result drops it",
	"Reports":  "how many times the step reported is folded into the round line (`· updated 2 times`) where it ran",
	"Inferred": "a report the factory made is folded into the round line (`reported for the step · …`) where it ran",
}

// A READER'S JOB IS AS IT WAS: no step carries a report, so the queue and a
// job read keep their size and never name the chat's path, and what a reader
// reads back is the result line.
func TestAReadersJobCarriesNoReport(t *testing.T) {
	j := FromItem(workerJobItem())
	raw, err := json.Marshal(j)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), `"report"`) {
		t.Fatalf("a reader's job carries a report: %s", raw)
	}
	if r := ToItem(j).Stream.Phases[0].Result; r == nil || r.Done || r.Notes != nil || r.Claims != nil || r.Output != workerJobItem().Stream.Phases[0].Result.Output {
		t.Fatalf("a reader's result is %+v", r)
	}
}

// AN OLDER FACTORY'S PULL IS STILL READ: a step with only its result line,
// from a server that predates the report, comes back as it always did.
func TestAPullWithoutReportsIsReadFromTheResultLine(t *testing.T) {
	raw := `{"lease":{"id":"l","job":1,"worker":"w","handle":"sam","expires_at":"2026-10-20T00:00:00Z"},
		"job":{"id":"api#1","document_id":1,"state":"running","steps":[
		{"name":"plan","state":"done","result":"the plan","cost":0.5,"model":"m","in_pipeline":true,"in_run":true}],
		"desk":{"run":{"started":"2026-10-20T00:00:00Z","ended":"0001-01-01T00:00:00Z"}}}}`
	var pulled WorkerPull
	if err := json.Unmarshal([]byte(raw), &pulled); err != nil {
		t.Fatal(err)
	}
	it := ToItem(pulled.Job)
	if it.Stream == nil || len(it.Stream.Phases) != 1 {
		t.Fatalf("the old pull's run is %+v", it.Stream)
	}
	want := &factory.StageResult{Output: "the plan", Spent: 0.5, Model: "m"}
	if got := it.Stream.Phases[0].Result; !reflect.DeepEqual(got, want) {
		t.Fatalf("the old pull's plan is %+v, want %+v", got, want)
	}
}
