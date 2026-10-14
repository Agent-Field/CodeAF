package prompt

import (
	"fmt"
	"strings"
	"testing"
)

const (
	fixtureCatalog = "RecordList(type, query, columns) · Panel · RecordPage · SectionTabs(items) · Button(label) · BulkBar · Row · Stack"
	fixtureSchemas = `{"app/1":{"required":["id","records"]},"screen/1":{"required":["screen","body"]}}`
	fixturePlan    = `{"data":[{"id":"deal","line":"Deal · name"}]}`
)

func fullInputs() Inputs {
	return Inputs{
		Paragraph: "A tiny tracker for our team's open bugs, ranked by age.",
		Space:     "acme", Budget: "up to $20 a month",
		Connectors: []string{"gmail"},
		Catalog:    fixtureCatalog, Schemas: fixtureSchemas, Plan: fixturePlan,
		Files:  map[string]string{"screens/b.json": "{}", "app.json": "{\"id\":\"x\"}"},
		Report: "kit/no-raw-style screens/b.json:4",
	}
}

func TestRenderIncludesWhatEachCallNeeds(t *testing.T) {
	tests := []struct {
		kind Kind
		want []string
	}{
		{KindPlan, []string{"PLAN call", "ten rules", "tracker for our team", fixtureCatalog, "up to $20 a month", "Connected accounts: gmail", `"needs":"computed"`, "aim ≤ 120, hard limit 200 characters", "hard limit 60 characters", `"brief":`, "daily_view", "one short clause", "detail goes in the files, not the plan line"}},
		{KindWrite, []string{"WRITE call", "ten rules", fixtureSchemas, fixturePlan, `space ("acme")`, "That changes the app", "strict JSON"}},
		{KindFix, []string{"FIX call", "ten rules", "----- app.json -----", "kit/no-raw-style screens/b.json:4", "changed files only"}},
	}
	for _, tt := range tests {
		t.Run(string(tt.kind), func(t *testing.T) {
			got, err := Render(tt.kind, fullInputs())
			if err != nil {
				t.Fatal(err)
			}
			for _, w := range tt.want {
				if !strings.Contains(strings.ToLower(got), strings.ToLower(w)) {
					t.Errorf("prompt lacks %q", w)
				}
			}
		})
	}
}

func TestRenderDemandsStrictJSONAndAllTenRules(t *testing.T) {
	for _, kind := range []Kind{KindPlan, KindWrite, KindFix} {
		got, err := Render(kind, fullInputs())
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(got, "no code fence") {
			t.Errorf("%s: no strict-JSON demand", kind)
		}
		for n := 1; n <= 10; n++ {
			if !strings.Contains(got, fmt.Sprintf("\n%d. ", n)) {
				t.Errorf("%s: rule %d missing", kind, n)
			}
		}
	}
}

func TestRenderReportsMissingInputs(t *testing.T) {
	tests := []struct {
		name string
		kind Kind
		mut  func(*Inputs)
		want string
	}{
		{"plan no paragraph", KindPlan, func(i *Inputs) { i.Paragraph = "" }, "Paragraph"},
		{"plan no catalog", KindPlan, func(i *Inputs) { i.Catalog = "" }, "Catalog"},
		{"write no plan", KindWrite, func(i *Inputs) { i.Plan = "" }, "Plan"},
		{"write no schemas", KindWrite, func(i *Inputs) { i.Schemas = " " }, "Schemas"},
		{"fix no files", KindFix, func(i *Inputs) { i.Files = nil }, "Files"},
		{"fix no report", KindFix, func(i *Inputs) { i.Report = "" }, "Report"},
		{"unknown kind", Kind("nope"), func(*Inputs) {}, "unknown kind"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			in := fullInputs()
			tt.mut(&in)
			_, err := Render(tt.kind, in)
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("err = %v, want mention of %q", err, tt.want)
			}
		})
	}
}

func TestPlanRevisionNeedsNoParagraph(t *testing.T) {
	in := fullInputs()
	in.Paragraph, in.Request = "", "stage should also have Negotiation"
	got, err := Render(KindPlan, in)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(got, "Rewrite the brief and plan") || !strings.Contains(got, in.Request) {
		t.Error("revision request not rendered")
	}
}

func TestFixListsFilesInPathOrder(t *testing.T) {
	got, err := Render(KindFix, fullInputs())
	if err != nil {
		t.Fatal(err)
	}
	if strings.Index(got, "----- app.json") > strings.Index(got, "----- screens/b.json") {
		t.Error("files not sorted by path")
	}
}

func TestWriteAndFixExplainRunRulesAndAgentTools(t *testing.T) {
	for _, kind := range []Kind{KindWrite, KindFix} {
		t.Run(string(kind), func(t *testing.T) {
			in := fullInputs()
			content := "run: triage\nparams:\n  bug: {type: ref, to: bug}\nsteps:\n  - id: load\n    records.query: {type: bug, query: 'id:{{bug}}'}\n"
			in.Files["runs/triage.yaml"] = content
			in.Report = "runs/triage.yaml/steps/0 schema/run-reference; fix: use {{params.bug}}"
			text, err := Render(kind, in)
			if err != nil {
				t.Fatal(err)
			}
			for _, want := range []string{"{{params.bug}}", "id: $params.bug", "No gate-only steps", "records.create", "delete", "capabilities"} {
				if !strings.Contains(text, want) {
					t.Errorf("%s lacks %s", kind, want)
				}
			}
			if kind == KindFix && (!strings.Contains(text, content) || !strings.Contains(text, in.Report)) {
				t.Fatal("FIX must show the full failing file and its report")
			}
		})
	}
}

// The catalog and schemas are inputs, so what must stay small is the fixed
// text: ~4k tokens leaves most of the 12k WRITE budget for them.
func TestWriteStaticTextFitsTheBudget(t *testing.T) {
	in := fullInputs()
	in.Catalog, in.Schemas = "c", "s"
	got, err := Render(KindWrite, in)
	if err != nil {
		t.Fatal(err)
	}
	const maxChars = 16000
	if len(got) > maxChars {
		t.Errorf("static WRITE prompt is %d chars, over %d", len(got), maxChars)
	}
}

func TestMachineryWordsAreAbsent(t *testing.T) {
	for _, kind := range []Kind{KindPlan, KindWrite, KindFix} {
		got, _ := Render(kind, fullInputs())
		for _, bad := range []string{"auditor", "verdict", "refuted"} {
			if strings.Contains(got, bad) {
				t.Errorf("%s prompt contains %q", kind, bad)
			}
		}
	}
}
