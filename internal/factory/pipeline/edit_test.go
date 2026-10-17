package pipeline

import (
	"errors"
	"strings"
	"testing"
)

const editBase = `# our pipeline
default_budget: 5
start:
  labels: [codeaf]
  max_size: L
  max_cost: 4
  never: ["secrets/**"]
  ask_first: ["touching billing"]
size:
  pr_lines: [20, 300]
  issue: label
pipeline:
  issue: [plan, approve, write, test, lint-pass, proof]
  pr: [read, review, proof]
steps:
  plan:
    prompt: plan it well # keep this
    until: the plan names its files
  write:
    prompt: carry out the plan
    max: 3
  test:
    prompt: run the tests
    until: green
  review:
    prompt: review the diff
    until: clean
  read:
    prompt: read the change
  proof:
    prompt: show the evidence
    until: every claim has evidence
  lint-pass:
    prompt: run the linter
fixed: [approve, proof]
rules:
  - never rewrite history
`

func s(v string) *string   { return &v }
func i(v int) *int         { return &v }
func f(v float64) *float64 { return &v }

func mustApply(t *testing.T, base string, ops ...EditOp) Edited {
	t.Helper()
	got, err := ApplyEdits(base, ops)
	if err != nil {
		t.Fatalf("ApplyEdits: %v", err)
	}
	return got
}

func refusal(t *testing.T, base string, ops ...EditOp) *EditError {
	t.Helper()
	_, err := ApplyEdits(base, ops)
	var e *EditError
	if !errors.As(err, &e) {
		t.Fatalf("want an EditError, got %v", err)
	}
	return e
}

func TestInspectReadsTheFileNotTheDefaults(t *testing.T) {
	src, problems, ok := Inspect(editBase)
	if !ok || len(problems) > 0 {
		t.Fatalf("inspect: ok=%v %v", ok, problems)
	}
	if src.Budget != 5 || !src.BudgetInFile {
		t.Fatalf("budget %v inFile=%v", src.Budget, src.BudgetInFile)
	}
	if len(src.Kinds) != 3 || src.Kinds[0].Kind != "issue" || src.Kinds[1].Kind != "pr" || src.Kinds[2].Kind != "ci" {
		t.Fatalf("kinds %+v", src.Kinds)
	}
	if !src.Kinds[0].InFile || !src.Kinds[1].InFile || src.Kinds[2].InFile {
		t.Fatalf("a kind the file does not list is the shipped default: %+v", src.Kinds)
	}
	if got := strings.Join(src.Kinds[0].Steps, ","); got != "plan,approve,write,test,lint-pass,proof" {
		t.Fatalf("order %s", got)
	}
	by := map[string]StepView{}
	for _, st := range src.Steps {
		by[st.Name] = st
	}
	if !by["approve"].Gate || !by["approve"].Locked || !by["proof"].Locked || by["write"].Locked {
		t.Fatalf("locks %+v", by)
	}
	if by["write"].Max != 3 || by["plan"].Prompt != "plan it well" || !by["lint-pass"].InFile {
		t.Fatalf("steps %+v", by)
	}
	if len(src.Steps) != 8 {
		t.Fatalf("a step is listed once however many pipelines name it: %d", len(src.Steps))
	}
}

func TestInspectBudgetIsNotInFileWhenTheFileDoesNotSayIt(t *testing.T) {
	src, _, ok := Inspect("pipeline:\n  issue: [plan]\nsteps:\n  plan: {prompt: p}\n")
	if !ok || src.BudgetInFile || src.Budget <= 0 {
		t.Fatalf("%+v ok=%v", src, ok)
	}
}

func TestInspectRefusesWhatItCannotEdit(t *testing.T) {
	for name, text := range map[string]string{
		"empty":        "",
		"blank":        " \n\t\n",
		"syntax":       "pipeline: [",
		"unknown key":  "model: gpt\n",
		"bad budget":   "default_budget: -1\n",
		"two docs":     "rules: [a]\n---\nrules: [b]\n",
		"not utf8":     "rules: [\xff]\n",
		"too large":    "rules: [" + strings.Repeat("a", EditTextMax) + "]\n",
		"unknown step": "pipeline:\n  issue: [nothing-here]\n",
	} {
		if _, problems, ok := Inspect(text); ok || len(problems) == 0 {
			t.Errorf("%s: ok=%v problems=%v", name, ok, problems)
		}
	}
}

func TestEditAStepChangesOnlyThatStep(t *testing.T) {
	got := mustApply(t, editBase, EditOp{Op: OpSetStep, Name: "write", Prompt: s("  carry out the plan, carefully  "), Max: i(5)})
	if !got.Changed {
		t.Fatal("not changed")
	}
	before, _, _ := Inspect(editBase)
	after, problems, ok := Inspect(got.Text)
	if !ok {
		t.Fatalf("the preview must be a file the route accepts: %v", problems)
	}
	for k, b := range before.Steps {
		a := after.Steps[k]
		if b.Name == "write" {
			if a.Prompt != "carry out the plan, carefully" || a.Max != 5 || a.Until != b.Until {
				t.Fatalf("write %+v", a)
			}
			continue
		}
		if a != b {
			t.Fatalf("an unrelated step moved: %+v -> %+v", b, a)
		}
	}
	if after.Budget != before.Budget || strings.Join(after.Fixed, ",") != strings.Join(before.Fixed, ",") {
		t.Fatal("budget or fixed moved")
	}
	for _, want := range []string{"never rewrite history", "secrets/**", "touching billing", "max_size: L", "issue: label", "- 20", "lint-pass"} {
		if !strings.Contains(got.Text, want) {
			t.Errorf("lost %q:\n%s", want, got.Text)
		}
	}
	if len(got.Steps) != 1 || got.Steps[0].Name != "write" || strings.Join(got.Steps[0].Fields, ",") != "prompt,max" {
		t.Fatalf("summary %+v", got.Steps)
	}
	if got.Steps[0].Before.Max != 3 || got.Steps[0].After.Max != 5 || got.Budget != nil {
		t.Fatalf("summary %+v budget %+v", got.Steps[0], got.Budget)
	}
}

func TestTheOrderOfEveryPipelineIsKept(t *testing.T) {
	got := mustApply(t, editBase, EditOp{Op: OpSetStep, Name: "test", Until: s("the suite is green and quiet")})
	src, _, _ := Inspect(got.Text)
	if strings.Join(src.Kinds[0].Steps, ",") != "plan,approve,write,test,lint-pass,proof" || strings.Join(src.Kinds[1].Steps, ",") != "read,review,proof" {
		t.Fatalf("%+v", src.Kinds)
	}
}

func TestSetBudget(t *testing.T) {
	got := mustApply(t, editBase, EditOp{Op: OpSetBudget, Value: f(7.5)})
	src, _, _ := Inspect(got.Text)
	if src.Budget != 7.5 || !got.Changed || got.Budget == nil || got.Budget.Before != 5 || got.Budget.After != 7.5 || len(got.Steps) != 0 {
		t.Fatalf("%+v %+v", src, got)
	}
}

func TestTheTextIsExactlyWhatTheRouteWouldCommit(t *testing.T) {
	got := mustApply(t, editBase, EditOp{Op: OpSetBudget, Value: f(9)}, EditOp{Op: OpSetStep, Name: "review", Until: s("no findings")})
	file, err := Parse(got.Text)
	if err != nil || len(file.Problems) > 0 {
		t.Fatal(err, file.Problems)
	}
	data, err := Encode(file)
	if err != nil || string(data) != got.Text {
		t.Fatalf("the route re-encodes its input; the preview must already be that:\n%s\n---\n%s", got.Text, data)
	}
}

func TestNormalizationAloneIsNotAChange(t *testing.T) {
	// The base is not in the encoder's layout and carries a comment. Setting a
	// value to what it already is changes nothing, however differently the
	// encoder would lay the file out.
	for name, op := range map[string]EditOp{
		"same prompt": {Op: OpSetStep, Name: "write", Prompt: s("carry out the plan")},
		"same until":  {Op: OpSetStep, Name: "test", Until: s("green")},
		"same max":    {Op: OpSetStep, Name: "write", Max: i(3)},
		"same budget": {Op: OpSetBudget, Value: f(5)},
		"padded":      {Op: OpSetStep, Name: "write", Prompt: s("\n carry out the plan \n")},
		"two no-ops":  {Op: OpSetStep, Name: "review", Prompt: s("review the diff"), Until: s("clean")},
	} {
		got := mustApply(t, editBase, op)
		if got.Changed || got.Text != editBase || got.FormatRewritten || got.CommentsDropped || len(got.Steps) != 0 || got.Budget != nil {
			t.Errorf("%s: %+v", name, got)
		}
	}
}

func TestAChangedPreviewSaysWhatItRewrites(t *testing.T) {
	got := mustApply(t, editBase, EditOp{Op: OpSetBudget, Value: f(6)})
	if !got.FormatRewritten || !got.CommentsDropped {
		t.Fatalf("a commented, hand-laid file loses both: %+v", got)
	}
	// Starting from the encoder's own layout, nothing but the edit differs.
	again := mustApply(t, got.Text, EditOp{Op: OpSetBudget, Value: f(8)})
	if !again.Changed || again.FormatRewritten || again.CommentsDropped {
		t.Fatalf("%+v", again)
	}
	// A comment-free file in a hand layout is reformatted but loses no comment.
	plain := mustApply(t, "default_budget: 5\nrules: [ a ]\npipeline: {issue: [plan]}\nsteps: {plan: {prompt: p}}\n", EditOp{Op: OpSetBudget, Value: f(6)})
	if !plain.FormatRewritten || plain.CommentsDropped {
		t.Fatalf("%+v", plain)
	}
}

func TestASparseFileIsEditedWithoutInventingTheDefaultsAsItsOwn(t *testing.T) {
	base := "pipeline:\n  issue: [plan, approve, write]\n"
	got := mustApply(t, base, EditOp{Op: OpSetStep, Name: "write", Max: i(2)})
	if !got.Changed || !got.FormatRewritten || got.CommentsDropped {
		t.Fatalf("%+v", got)
	}
	src, _, ok := Inspect(got.Text)
	if !ok || src.Steps[2].Name != "write" || src.Steps[2].Max != 2 {
		t.Fatalf("%+v", src)
	}
}

func TestRefusals(t *testing.T) {
	long := strings.Repeat("x", EditPromptMax+1)
	cases := []struct {
		name string
		base string
		ops  []EditOp
		code string
		op   int
	}{
		{"locked gate", editBase, []EditOp{{Op: OpSetStep, Name: "approve", Prompt: s("x")}}, EditLocked, 0},
		{"locked fixed", editBase, []EditOp{{Op: OpSetStep, Name: "proof", Prompt: s("x")}}, EditLocked, 0},
		{"unknown name", editBase, []EditOp{{Op: OpSetStep, Name: "deploy", Prompt: s("x")}}, EditUnknownStep, 0},
		{"builtin not in a pipeline", editBase, []EditOp{{Op: OpSetStep, Name: "security", Prompt: s("x")}}, EditUnknownStep, 0},
		{"upper case", editBase, []EditOp{{Op: OpSetStep, Name: "Write", Prompt: s("x")}}, EditUnknownStep, 0},
		{"padded name", editBase, []EditOp{{Op: OpSetStep, Name: " write", Prompt: s("x")}}, EditUnknownStep, 0},
		{"path-like name", editBase, []EditOp{{Op: OpSetStep, Name: "../write", Prompt: s("x")}}, EditUnknownStep, 0},
		{"empty name", editBase, []EditOp{{Op: OpSetStep, Prompt: s("x")}}, EditUnknownStep, 0},
		{"empty prompt", editBase, []EditOp{{Op: OpSetStep, Name: "write", Prompt: s("  ")}}, EditBadValue, 0},
		{"long prompt", editBase, []EditOp{{Op: OpSetStep, Name: "write", Prompt: s(long)}}, EditBadValue, 0},
		{"nul in prompt", editBase, []EditOp{{Op: OpSetStep, Name: "write", Prompt: s("a\x00b")}}, EditBadValue, 0},
		{"bad utf8 prompt", editBase, []EditOp{{Op: OpSetStep, Name: "write", Prompt: s("a\xffb")}}, EditBadValue, 0},
		{"multi-line until", editBase, []EditOp{{Op: OpSetStep, Name: "write", Until: s("a\nb")}}, EditBadValue, 0},
		{"long until", editBase, []EditOp{{Op: OpSetStep, Name: "write", Until: s(strings.Repeat("u", EditUntilMax+1))}}, EditBadValue, 0},
		{"max zero", editBase, []EditOp{{Op: OpSetStep, Name: "write", Max: i(0)}}, EditBadValue, 0},
		{"max negative", editBase, []EditOp{{Op: OpSetStep, Name: "write", Max: i(-2)}}, EditBadValue, 0},
		{"max huge", editBase, []EditOp{{Op: OpSetStep, Name: "write", Max: i(EditRoundsMax + 1)}}, EditBadValue, 0},
		{"budget zero", editBase, []EditOp{{Op: OpSetBudget, Value: f(0)}}, EditBadValue, 0},
		{"budget negative", editBase, []EditOp{{Op: OpSetBudget, Value: f(-1)}}, EditBadValue, 0},
		{"budget huge", editBase, []EditOp{{Op: OpSetBudget, Value: f(EditBudgetMax + 1)}}, EditBadValue, 0},
		{"budget missing", editBase, []EditOp{{Op: OpSetBudget}}, EditBadValue, 0},
		{"budget with a name", editBase, []EditOp{{Op: OpSetBudget, Name: "write", Value: f(2)}}, EditBadOp, 0},
		{"step with a value", editBase, []EditOp{{Op: OpSetStep, Name: "write", Prompt: s("x"), Value: f(2)}}, EditBadOp, 0},
		{"step with nothing", editBase, []EditOp{{Op: OpSetStep, Name: "write"}}, EditBadOp, 0},
		{"unknown verb", editBase, []EditOp{{Op: "move", Name: "write"}}, EditBadOp, 0},
		{"no verb", editBase, []EditOp{{Name: "write", Prompt: s("x")}}, EditBadOp, 0},
		{"no ops", editBase, nil, EditBadOp, -1},
		{"twice the same step", editBase, []EditOp{{Op: OpSetStep, Name: "write", Max: i(2)}, {Op: OpSetStep, Name: "write", Max: i(4)}}, EditBadOp, 1},
		{"twice the budget", editBase, []EditOp{{Op: OpSetBudget, Value: f(2)}, {Op: OpSetBudget, Value: f(3)}}, EditBadOp, 1},
		{"second edit bad", editBase, []EditOp{{Op: OpSetStep, Name: "write", Max: i(2)}, {Op: OpSetStep, Name: "approve", Max: i(2)}}, EditLocked, 1},
		{"malformed base", "pipeline: [", []EditOp{{Op: OpSetBudget, Value: f(2)}}, EditBaseInvalid, -1},
		{"empty base", "", []EditOp{{Op: OpSetBudget, Value: f(2)}}, EditBaseInvalid, -1},
		{"base with a problem", "model: x\n", []EditOp{{Op: OpSetBudget, Value: f(2)}}, EditBaseInvalid, -1},
		// A built-in step's until cannot be cleared: the reader puts the shipped
		// one back, so the file could not hold the edit.
		{"clear a built-in until", editBase, []EditOp{{Op: OpSetStep, Name: "plan", Until: s("")}}, EditNotRepresentable, -1},
	}
	for _, c := range cases {
		e := refusal(t, c.base, c.ops...)
		if e.Code != c.code || e.Op != c.op {
			t.Errorf("%s: %s op %d (%s), want %s op %d", c.name, e.Code, e.Op, e.Why, c.code, c.op)
		}
	}
	many := make([]EditOp, EditOpsMax+1)
	for k := range many {
		many[k] = EditOp{Op: OpSetBudget, Value: f(2)}
	}
	if e := refusal(t, editBase, many...); e.Code != EditBadOp {
		t.Fatal(e)
	}
}

func TestOneRefusalAppliesNothing(t *testing.T) {
	_, err := ApplyEdits(editBase, []EditOp{{Op: OpSetBudget, Value: f(9)}, {Op: OpSetStep, Name: "deploy", Prompt: s("x")}})
	if err == nil {
		t.Fatal("accepted")
	}
}

func TestAClearedUntilOnACustomStepRoundTrips(t *testing.T) {
	base := "pipeline:\n  issue: [plan, lint-pass]\nsteps:\n  plan: {prompt: p}\n  lint-pass: {prompt: lint, until: quiet}\n"
	got := mustApply(t, base, EditOp{Op: OpSetStep, Name: "lint-pass", Until: s("")})
	src, _, _ := Inspect(got.Text)
	if src.Steps[1].Until != "" || strings.Contains(got.Text, "quiet") {
		t.Fatalf("%s", got.Text)
	}
}

func TestAwkwardTextSurvivesOrIsRefusedNeverCorrupted(t *testing.T) {
	for _, p := range []string{"true", "null", "123", "yes", "- a", "key: value", "# not a comment", "a: b\nc: d", "tab\there", "quote ' and \"", "{flow}", "multi\nline\n\nwith blank", "ünïcode ✓ 日本語", "*alias", "&anchor x", "!tag x", "| pipe", "> fold"} {
		got, err := ApplyEdits(editBase, []EditOp{{Op: OpSetStep, Name: "write", Prompt: s(p)}})
		if err != nil {
			var e *EditError
			if !errors.As(err, &e) || e.Code != EditNotRepresentable {
				t.Errorf("%q: %v", p, err)
			}
			continue
		}
		src, problems, ok := Inspect(got.Text)
		if !ok {
			t.Errorf("%q: %v", p, problems)
			continue
		}
		for _, st := range src.Steps {
			if st.Name == "write" && st.Prompt != strings.TrimSpace(p) {
				t.Errorf("%q came back as %q", p, st.Prompt)
			}
		}
	}
}

func TestPreviewIsDeterministic(t *testing.T) {
	ops := []EditOp{{Op: OpSetStep, Name: "write", Max: i(4)}, {Op: OpSetBudget, Value: f(6)}}
	first := mustApply(t, editBase, ops...)
	for k := 0; k < 20; k++ {
		if again := mustApply(t, editBase, ops...); again.Text != first.Text {
			t.Fatal("the encoder is not deterministic")
		}
	}
}

func TestHasComments(t *testing.T) {
	if !hasComments("# a\nrules: [x]\n") || !hasComments("rules: [x] # a\n") || !hasComments("rules: [x]\n# end\n") {
		t.Fatal("missed a comment")
	}
	if hasComments("rules: ['# not one']\n") || hasComments("rules: [x]\n") {
		t.Fatal("invented a comment")
	}
}

// Parse accepts these names and File.stages silently drops them at run time, so
// an editor must not offer them as editable.
func TestABaseWhoseStepTheRunnerWouldDropIsNotEditable(t *testing.T) {
	long := "abcdefghijklm" // 13 letters
	for name, c := range map[string]struct{ text, want string }{
		"13 letters":                       {"pipeline:\n  issue: [" + long + "]\nsteps:\n  " + long + ": {prompt: p}\n", `"` + long + `"`},
		"hyphenated 13 letters":            {"pipeline:\n  issue: [abcdefg-hijklm]\nsteps:\n  abcdefg-hijklm: {prompt: p}\n", "abcdefg-hijklm"},
		"lint-pass collides with lintpass": {"pipeline:\n  issue: [lint-pass, lintpass]\nsteps:\n  lint-pass: {prompt: p}\n  lintpass: {prompt: q}\n", `"lintpass" becomes the same stage "lintpass"`},
		"collision in pr kind":             {"pipeline:\n  pr: [lintpass, lint-pass]\nsteps:\n  lint-pass: {prompt: p}\n  lintpass: {prompt: q}\n", "pr pipeline"},
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := Parse(c.text); err != nil {
				t.Skipf("Parse itself refuses this: %v", err) // the fixture must be one Parse accepts
			}
			_, problems, ok := Inspect(c.text)
			if ok || len(problems) != 1 || !strings.Contains(problems[0], c.want) || !strings.HasPrefix(problems[0], RepoFile+": ") {
				t.Fatalf("ok=%v %q", ok, problems)
			}
			e := refusal(t, c.text, EditOp{Op: OpSetStep, Name: "plan", Prompt: s("x")})
			if e.Code != EditBaseInvalid {
				t.Fatalf("%+v", e)
			}
		})
	}
	// Parse and what it hands the runner are untouched: the same text still parses.
	f, err := Parse("pipeline:\n  issue: [lint-pass, lintpass]\nsteps:\n  lint-pass: {prompt: p}\n  lintpass: {prompt: q}\n")
	if err != nil || len(f.Pipeline["issue"]) != 2 {
		t.Fatalf("the legacy parse changed: %v %v", err, f.Pipeline)
	}
}

func TestOrdinaryHyphenatedAndTwelveLetterNamesStayEditable(t *testing.T) {
	text := "pipeline:\n  issue: [lint-pass, abcdefghijkl]\nsteps:\n  lint-pass: {prompt: p}\n  abcdefghijkl: {prompt: q}\n"
	if _, problems, ok := Inspect(text); !ok {
		t.Fatalf("%v", problems)
	}
	if got := mustApply(t, text, EditOp{Op: OpSetStep, Name: "lint-pass", Prompt: s("run it")}); !got.Changed {
		t.Fatalf("%+v", got)
	}
}
