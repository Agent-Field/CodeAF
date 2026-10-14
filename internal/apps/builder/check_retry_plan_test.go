package builder

import (
	"testing"

	"github.com/Agent-Field/codeaf/internal/apps/spec"
)

func TestPlanFixKeptOnlyWhenNamed(t *testing.T) {
	report := "app.json/name: schema: x\nplan.json/data/0/line: schema: length must be <= 200, but got 215"
	if !reportNames(report, spec.PlanFile) || reportNames("app.json: x", spec.PlanFile) {
		t.Fatal("reportNames misreads the report")
	}
	plan := []byte(`{"ask":"a","name":"N","data":[{"id":"deal","line":"Deal · stage"}],"screens":[{"id":"deals","line":"Deals"}],"agent":{"line":"Helps"}}`)
	b := &build{}
	changed := map[string][]byte{spec.PlanFile: plan}
	keepPlanFix(b, changed, true)
	if changed[spec.PlanFile] == nil || b.currentPlan().Data[0].Line != "Deal · stage" {
		t.Fatal("named plan fix was not kept on the plan card")
	}
	changed = map[string][]byte{spec.PlanFile: plan}
	keepPlanFix(&build{}, changed, false)
	if changed[spec.PlanFile] != nil {
		t.Fatal("unnamed plan change kept")
	}
	changed = map[string][]byte{spec.PlanFile: []byte("not json")}
	keepPlanFix(&build{}, changed, true)
	if changed[spec.PlanFile] != nil {
		t.Fatal("unparsable plan kept")
	}
}
