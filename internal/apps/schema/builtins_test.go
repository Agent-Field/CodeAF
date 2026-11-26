package schema

import "testing"

func TestPlanChecksLocalApprovalBeforeEveryWrite(t *testing.T) {
	const path = "runs/follow-up.yaml"
	const approved = `run: follow-up
steps:
  - id: save
    records.create: {type: deal, set: {name: Follow-up}}
    gate: {approval: proof}
`
	for _, test := range []struct {
		name, run, target string
		want              bool
	}{
		{"protected", approved, "follow-up", true},
		{"no gate", "run: follow-up\nsteps:\n  - id: save\n    records.create: {type: deal, set: {name: Follow-up}}\n", "follow-up", false},
		{"nothing to protect", "run: follow-up\nsteps:\n  - id: review\n    gate: {approval: proof}\n", "follow-up", false},
		{"missing run", approved, "missing", false},
	} {
		t.Run(test.name, func(t *testing.T) {
			def := load(t, "testdata/crm")
			def.Files[path] = []byte(test.run)
			jsonEdit(PlanFile, func(plan map[string]any) {
				plan["automation"] = append(plan["automation"].([]any), map[string]any{"id": "follow-up", "line": "Follow up"})
				plan["builtins"] = []any{map[string]any{"id": "before-save", "kind": BuiltinApproval, "run": test.target}}
			})(t, def.Files)
			hasFinding := false
			report := Validate(def, nil)
			for _, problem := range report.Problems {
				hasFinding = hasFinding || problem.File == PlanFile && problem.Path == "/builtins/0/run"
			}
			if hasFinding == test.want || test.want && !report.OK() {
				t.Fatalf("Check report = %s", report)
			}
		})
	}
}

func TestPlanChecksNativeImportWritesAndLifecycleTransition(t *testing.T) {
	def := load(t, "testdata/crm")
	jsonEdit(PlanFile, func(plan map[string]any) {
		plan["builtins"] = []any{
			map[string]any{"id": "import", "kind": BuiltinCSV, "record": "missing"},
			map[string]any{"id": "ship", "kind": BuiltinLifecycle, "record": "deal", "transition": "ship"},
		}
	})(t, def.Files)
	findings := map[string]bool{}
	for _, problem := range Validate(def, nil).Problems {
		if problem.File == PlanFile {
			findings[problem.Path] = true
		}
	}
	if !findings["/builtins/0/record"] || !findings["/builtins/1/record"] {
		t.Fatalf("missing findings for unavailable native work: %v", findings)
	}
}

func TestPlanChecksEveryReservedRunHasAFile(t *testing.T) {
	def := load(t, "testdata/crm")
	delete(def.Files, "runs/follow-up.yaml")
	for _, finding := range Validate(def, nil).Problems {
		if finding.File == PlanFile && finding.Path == "/automation/0/id" && finding.Code == CodePlanUnknownID {
			return
		}
	}
	t.Fatal("Check accepted a missing reserved run")
}
