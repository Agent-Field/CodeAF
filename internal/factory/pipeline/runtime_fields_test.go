package pipeline

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/Agent-Field/codeaf/internal/factory"
)

const runtimeYAML = `pipeline:
  issue: [plan, approve, write, test, review, post-it]
steps:
  plan:
    prompt: plan it
    kind: chat
    when: large
    effort: strong
    fanout: one
  write:
    prompt: write it
    fanout: per-file
    effort: cheap
    max: 2
  test:
    prompt: go test ./...
    kind: check
    until: green
  review:
    prompt: review it
    when: touches auth
    fanout: per-finding
    until: clean
  post-it:
    prompt: comment on the pull request
    kind: post
fixed: [review]
`

func parseClean(t *testing.T, text string) File {
	t.Helper()
	f, err := Parse(text)
	if err != nil || len(f.Problems) > 0 {
		t.Fatalf("parse: %v %v", err, f.Problems)
	}
	return f
}

func TestRuntimeWordsRoundTripThroughEncodeAndRecipe(t *testing.T) {
	f := parseClean(t, runtimeYAML)
	data, err := Encode(f)
	if err != nil {
		t.Fatal(err)
	}
	g := parseClean(t, string(data))
	if !reflect.DeepEqual(f.Recipe(), g.Recipe()) {
		t.Fatalf("recipe changed over a round trip:\n%+v\n%+v", f.Recipe().Stages, g.Recipe().Stages)
	}
	st := map[string]factory.Stage{}
	for _, s := range g.Recipe().Stages {
		st[s.Name] = s
	}
	want := map[string]factory.Stage{
		"plan":   {Kind: factory.StageChat, When: "large", Effort: "strong", Fanout: "one"},
		"write":  {Kind: factory.StageChat, Effort: "cheap", Fanout: "per-file"},
		"test":   {Kind: factory.StageCheck},
		"review": {Kind: factory.StageChat, When: "touches auth", Fanout: "per-finding", Fixed: true},
		"postit": {Kind: factory.StagePost},
	}
	for name, w := range want {
		got := st[name]
		if got.Kind != w.Kind || got.When != w.When || got.Effort != w.Effort || got.Fanout != w.Fanout || got.Fixed != w.Fixed {
			t.Errorf("%s = %+v, want %+v", name, got, w)
		}
	}
	if st["approve"].Kind != factory.StageGate {
		t.Fatalf("approve = %+v", st["approve"])
	}
}

func TestAllFourKindsParse(t *testing.T) {
	for _, kind := range []string{"chat", "check", "gate", "post", "CHECK "} {
		f := parseClean(t, "pipeline:\n  issue: [lint]\nsteps:\n  lint:\n    prompt: run lint\n    kind: \""+kind+"\"\n")
		want := factory.StageKind(strings.ToLower(strings.TrimSpace(kind)))
		if got := f.Recipe().Stages[0].Kind; got != want {
			t.Errorf("kind %q = %q", kind, got)
		}
	}
}

func TestLegacyYAMLMeansTheSameAndWritesNoNewKeys(t *testing.T) {
	const legacy = "pipeline:\n  issue: [plan, write]\nsteps:\n  write:\n    prompt: do it\n    until: green\n    max: 2\n"
	f := parseClean(t, legacy)
	for _, s := range f.Recipe().Stages {
		if s.Kind != factory.StageChat || s.When != "" || s.Effort != "" || s.Fanout != "" {
			t.Fatalf("legacy stage changed: %+v", s)
		}
	}
	data, _ := Encode(f)
	for _, k := range []string{"kind:", "when:", "effort:", "fanout:"} {
		if strings.Contains(string(data), k) {
			t.Fatalf("legacy file gained %s:\n%s", k, data)
		}
	}
	// An explicit empty word is the default, not a refusal.
	e := parseClean(t, "steps:\n  write:\n    prompt: do it\n    when: \"\"\n    kind: \"\"\n    effort: \"\"\n    fanout: \"\"\n")
	if s := e.Steps["write"]; s.Kind != "" || s.When != "" || s.Effort != "" || s.Fanout != "" {
		t.Fatalf("empty words = %+v", s)
	}
}

func TestInvalidRuntimeWordsAreProblemsNotSettings(t *testing.T) {
	for key, bad := range map[string][]string{
		"kind":   {"loop", "model", "3"},
		"when":   {"sometimes", "touches  money", "when auth"},
		"effort": {"auto", "max", "high"},
		"fanout": {"per file", "3", "per-thing", "many"},
	} {
		for _, v := range bad {
			f, err := Parse("steps:\n  write:\n    prompt: do it\n    " + key + ": " + v + "\n")
			if err != nil {
				t.Fatal(err)
			}
			if len(f.Problems) != 1 || !strings.Contains(f.Problems[0].Where, key) {
				t.Errorf("%s: %s -> %v", key, v, f.Problems)
			}
			s := f.Steps["write"]
			if s.Kind != "" || s.When != "" || s.Effort != "" || s.Fanout != "" {
				t.Errorf("%s: %s leaked into %+v", key, v, s)
			}
		}
	}
	for _, y := range []string{"kind: null", "when: [a]", "fanout: {a: b}"} {
		f, _ := Parse("steps:\n  write:\n    prompt: do it\n    " + y + "\n")
		if len(f.Problems) != 1 {
			t.Errorf("%s -> %v", y, f.Problems)
		}
	}
	f, _ := Parse("steps:\n  write:\n    prompt: do it\n    model: x\n    ask: y\n")
	if len(f.Problems) != 2 {
		t.Fatalf("unknown keys must not be silent: %v", f.Problems)
	}
}

func TestApproveTakesNoRuntimeWords(t *testing.T) {
	f, _ := Parse("steps:\n  approve:\n    when: large\n    effort: strong\n")
	if len(f.Problems) == 0 {
		t.Fatal("approve took a runtime word")
	}
	if s := f.Steps["approve"]; s.When != "" || s.Effort != "" {
		t.Fatalf("approve = %+v", s)
	}
}

func TestSetStepEditsRuntimeWordsAndNamesTheChange(t *testing.T) {
	base := runtimeYAML
	str := func(s string) *string { return &s }
	got, err := ApplyEdits(base, []EditOp{{Op: OpSetStep, Name: "write", When: str("thin"), Fanout: str(""), Kind: str("chat"), Effort: str("strong")}})
	if err != nil {
		t.Fatal(err)
	}
	if !got.Changed || len(got.Steps) != 1 || !reflect.DeepEqual(got.Steps[0].Fields, []string{"when", "effort", "fanout"}) {
		t.Fatalf("change = %+v", got.Steps)
	}
	a := got.Steps[0].After
	if a.Kind != "" || a.When != "thin" || a.Effort != "strong" || a.Fanout != "" || got.Steps[0].Before.Fanout != "per-file" {
		t.Fatalf("view = %+v", a)
	}
	back := parseClean(t, got.Text)
	if s := back.Steps["write"]; s.Kind != "" || s.When != "thin" || s.Effort != "strong" || s.Fanout != "" || s.Max != 2 || s.Prompt != "write it" {
		t.Fatalf("written = %+v", s)
	}
	// Words the edit did not name survive: only the edited step changed.
	if !reflect.DeepEqual(back.Recipe().Stages[0], parseClean(t, base).Recipe().Stages[0]) {
		t.Fatal("another step moved")
	}
	// A prompt-only edit keeps every runtime word.
	got, err = ApplyEdits(base, []EditOp{{Op: OpSetStep, Name: "plan", Prompt: str("plan harder")}})
	if err != nil {
		t.Fatal(err)
	}
	if s := parseClean(t, got.Text).Steps["plan"]; s.Kind != "" || s.When != "large" || s.Effort != "strong" || s.Fanout != "one" {
		t.Fatalf("prompt edit lost words: %+v", s)
	}
}

func TestSetStepRefusesBadWordsAndLockedSteps(t *testing.T) {
	str := func(s string) *string { return &s }
	cases := []struct {
		op   EditOp
		code string
	}{
		{EditOp{Op: OpSetStep, Name: "write", Kind: str("loop")}, EditBadValue},
		{EditOp{Op: OpSetStep, Name: "write", When: str("sometimes")}, EditBadValue},
		{EditOp{Op: OpSetStep, Name: "write", Effort: str("auto")}, EditBadValue},
		{EditOp{Op: OpSetStep, Name: "write", Fanout: str("per file")}, EditBadValue},
		{EditOp{Op: OpSetStep, Name: "review", Kind: str("gate")}, EditBadValue},
		{EditOp{Op: OpSetStep, Name: "review", Kind: str("chat")}, EditLocked},
		{EditOp{Op: OpSetStep, Name: "review", Effort: str("")}, EditLocked},
		{EditOp{Op: OpSetStep, Name: "approve", When: str("large")}, EditLocked},
		{EditOp{Op: OpSetBudget, Kind: str("chat"), Value: new(float64)}, EditBadOp},
	}
	for _, c := range cases {
		_, err := ApplyEdits(runtimeYAML, []EditOp{c.op})
		ee, ok := err.(*EditError)
		if !ok || ee.Code != c.code {
			t.Errorf("%+v -> %v, want %s", c.op, err, c.code)
		}
	}
}

func TestBudgetEditKeepsEveryRuntimeWord(t *testing.T) {
	v := 7.5
	got, err := ApplyEdits(runtimeYAML, []EditOp{{Op: OpSetBudget, Value: &v}})
	if err != nil {
		t.Fatal(err)
	}
	before, after := parseClean(t, runtimeYAML), parseClean(t, got.Text)
	if !reflect.DeepEqual(before.Recipe().Stages, after.Recipe().Stages) || after.DefaultBudget != 7.5 || got.Steps != nil {
		t.Fatalf("budget edit moved stages: %+v", after.Recipe().Stages)
	}
}

func TestWritersKeepRuntimeWordsAcrossBankAndSave(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, RepoFile)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(runtimeYAML), 0o644); err != nil {
		t.Fatal(err)
	}
	before, err := Working(dir)
	if err != nil {
		t.Fatal(err)
	}
	stages := before.Recipe().For(factory.KindIssue)
	if err := (Files{}).BankStages(dir, factory.KindIssue, stages); err != nil {
		t.Fatal(err)
	}
	r := before.Recipe()
	r.DefaultBudget = 9 // a budget-only change is not a stage change
	r.Stages[2].Max = 4 // write
	if err := (Files{}).Save(dir, r); err != nil {
		t.Fatal(err)
	}
	if err := (Files{}).BankRule(dir, "be kind"); err != nil {
		t.Fatal(err)
	}
	after, err := Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	want := before.Recipe().Stages
	got := after.Recipe().Stages
	want[2].Max = 4
	if !reflect.DeepEqual(want, got) {
		t.Fatalf("stages changed:\n%+v\n%+v", want, got)
	}
}

func TestMarkdownCarriesRuntimeWordsAndStillWarnsOnTheRest(t *testing.T) {
	f := FromMarkdown("# factory recipe · codeaf\n\n## issue\n1. write · chat · do it · when thin · effort strong · fanout per file\n2. lint · check · run lint\n3. fan · chat · many · fanout 3\n")
	w, l := f.Steps["write"], f.Steps["lint"]
	if w.When != "thin" || w.Effort != "strong" || w.Fanout != "per-file" || strings.Contains(w.Prompt, "when") || l.Kind != "check" {
		t.Fatalf("steps = %+v %+v", w, l)
	}
	if f.Steps["fan"].Fanout != "" || !strings.Contains(problems(f), "fanout 3") {
		t.Fatalf("a count must warn, not vanish: %v", f.Problems)
	}
}

func TestPersonalFileCannotChangeRuntimeWords(t *testing.T) {
	repo := parseClean(t, runtimeYAML)
	personal, err := ParsePersonal("steps:\n  plan:\n    effort: strong\n    when: large\n  write:\n    effort: strong\n    kind: check\n    max: 1\n")
	if err != nil || len(personal.Problems) > 0 {
		t.Fatal(err, personal.Problems)
	}
	merged, probs := Merge(repo, personal)
	if len(probs) != 2 {
		t.Fatalf("problems = %v", probs)
	}
	if merged.Steps["write"].Effort != "cheap" || merged.Steps["write"].Kind != "" || merged.Steps["write"].Max != 1 || merged.Steps["plan"].When != "large" {
		t.Fatalf("merged = %+v", merged.Steps)
	}
}

func problems(f File) string {
	var out []string
	for _, p := range f.Problems {
		out = append(out, p.Where+" "+p.Why)
	}
	return strings.Join(out, "\n")
}
