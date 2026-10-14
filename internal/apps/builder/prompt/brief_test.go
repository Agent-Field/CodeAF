package prompt

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/Agent-Field/codeaf/internal/apps/spec"
)

func TestBriefAcceptsObjectStringAndLists(t *testing.T) {
	tests := []struct{ name, raw, want string }{
		{"object in designer order", `{"agent":"drafts","users":"a lead","actions":["New deal","Draft nudge"],"extra":"x"}`,
			"Who uses it: a lead\nTop actions: New deal; Draft nudge\nApp agent: drafts\nextra: x"},
		{"plain text", `"  a short brief "`, "a short brief"},
		{"junk is empty, not an error", `42`, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var b Brief
			if err := b.UnmarshalJSON([]byte(tt.raw)); err != nil || string(b) != tt.want {
				t.Fatalf("got %q err %v, want %q", b, err, tt.want)
			}
		})
	}
}

// The brief rides in the PLAN reply beside the plan keys; plan.json never
// carries it, so ParsePlan must keep it and the plan must still validate.
func TestParsePlanKeepsTheBrief(t *testing.T) {
	ex, err := loadExample()
	if err != nil {
		t.Fatal(err)
	}
	plan, err := ParsePlan(ex.PlanReply)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(plan.Brief), "Daily view: Deals board by Stage") || len(plan.Screens) == 0 {
		t.Fatalf("brief lost: %+v", plan)
	}
	if strings.Contains(ex.Plan, "brief") {
		t.Error("example plan.json must stay schema-shaped, without a brief")
	}
}

func TestBriefReachesEveryCall(t *testing.T) {
	in := fullInputs()
	in.Brief = "Daily view: which bugs are oldest?"
	for _, kind := range []Kind{KindWrite} {
		got, err := Render(kind, in)
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(got, in.Brief) {
			t.Errorf("%s lacks the brief", kind)
		}
	}
	in.Request = "add a due date"
	got, err := Render(KindPlan, in)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(got, "Current brief:\n"+in.Brief) {
		t.Error("plan revision does not show the current brief")
	}
}

func TestLimitsComeFromThePlanSchema(t *testing.T) {
	l := loadPlanLimits()
	if l.Line <= 0 || l.Name <= 0 {
		t.Fatalf("limits not read from the schema: %+v", l)
	}
}

func TestKitHintsOnlyNameCatalogParts(t *testing.T) {
	got := kitHints(`[{"name":"StatStrip"},{"name":"RecordList"}]`)
	if !strings.Contains(got, "StatStrip:") || !strings.Contains(got, "RecordList:") || strings.Contains(got, "Chart") {
		t.Fatalf("hints = %q", got)
	}
}

func TestEveryKitHintNamesARealPart(t *testing.T) {
	raw, err := spec.KitJSON()
	if err != nil {
		t.Fatal(err)
	}
	for _, h := range partHints {
		if !strings.Contains(string(raw), `"name": "`+h.Part+`"`) {
			t.Errorf("hint for %s, which kit.json does not list", h.Part)
		}
	}
}

// The example is what the model imitates, so it must pass the same schema
// and kit validation Check starts from.
func TestExampleFilesValidate(t *testing.T) {
	files := spec.Files{}
	for _, path := range append([]string{"plan.json"}, exampleFiles...) {
		raw, err := exampleFS.ReadFile("example/" + path)
		if err != nil {
			t.Fatal(err)
		}
		files[path] = raw
	}
	var plan map[string]any
	_ = json.Unmarshal(files["plan.json"], &plan)
	plan["ask"] = "example"
	files["plan.json"], _ = json.Marshal(plan)
	for _, issue := range spec.Validate(files) {
		t.Errorf("%s", issue.String())
	}
}
