package pipeline

import (
	"maps"
	"strings"
	"testing"

	"github.com/Agent-Field/codeaf/internal/factory"
)

const adaptKindsFile = `pipeline:
  issue: [plan, write, proof]
  docs: [write, proof]
steps:
  plan:
    prompt: plan it
  write:
    prompt: write it
  proof:
    prompt: prove it
adapt:
  issue: ask
  docs: fixed
`

func adaptOf(f File) map[string]string {
	out := map[string]string{}
	for k, m := range f.AdaptKinds {
		out[string(k)] = string(m)
	}
	return out
}

// ADAPT MAY BE ONE WORD PER PIPELINE: a map of kind to word, which the runner
// reads per kind; a kind it does not name is adapt.
func TestAdaptAsAMapIsReadPerKindAndWrittenBack(t *testing.T) {
	f, err := Parse(adaptKindsFile)
	if err != nil || len(f.Problems) > 0 {
		t.Fatalf("%v %v", err, f.Problems)
	}
	r := f.Recipe()
	if r.AdaptFor(factory.KindIssue) != factory.AdaptAsk || r.AdaptFor("docs") != factory.AdaptFixed || r.AdaptFor(factory.KindPR) != factory.AdaptFree {
		t.Fatalf("%v", r.Adapt)
	}
	data, err := Encode(f)
	if err != nil || !strings.Contains(string(data), "adapt:\n    docs: fixed\n    issue: ask\n") {
		t.Fatalf("%v\n%s", err, data)
	}
	if back := reread(t, string(data)); !maps.Equal(adaptOf(back), map[string]string{"issue": "ask", "docs": "fixed"}) || back.Adapt != "" {
		t.Fatalf("%+v %q", back.AdaptKinds, back.Adapt)
	}
	// A reader older than the map drops it and lets every plan reshape, so
	// the map is a key that reader cannot be trusted with; one word is not.
	if !RuntimeKeysIn(adaptKindsFile) {
		t.Fatal("a per-kind adapt is a key an older reader would drop")
	}
	if RuntimeKeysIn("pipeline:\n  issue: [plan]\nsteps:\n  plan:\n    prompt: x\nadapt: ask\n") {
		t.Fatal("one adapt word is read by every reader")
	}
	for _, bad := range []string{"adapt:\n  issue: maybe\n", "adapt:\n  Issue!: ask\n", "adapt: [ask]\n"} {
		if g, _ := Parse("pipeline:\n  issue: [plan]\nsteps:\n  plan:\n    prompt: x\n" + bad); len(g.Problems) == 0 || len(g.AdaptKinds) != 0 {
			t.Errorf("%q: %+v %v", bad, g.Problems, g.AdaptKinds)
		}
	}
}

// A PERSONAL WORD TIGHTENS EVERY PIPELINE OF A PER-KIND FILE, never loosens one.
func TestAPersonalAdaptTightensEveryKindOfAMap(t *testing.T) {
	repo, _ := Parse(adaptKindsFile)
	personal, _ := ParsePersonal("adapt: ask\n")
	got, _ := Merge(repo, personal)
	r := got.Recipe()
	if r.AdaptFor(factory.KindIssue) != factory.AdaptAsk || r.AdaptFor("docs") != factory.AdaptFixed {
		t.Fatalf("%v", r.Adapt)
	}
}

func adaptKind(kind, word string) EditOp {
	return EditOp{Op: OpSetAdapt, Pipeline: kind, Adapt: s(word)}
}

func TestSetAdaptForOnePipeline(t *testing.T) {
	got := mustApply(t, editBase, adaptKind("pr", "fixed"))
	back := reread(t, got.Text)
	if !maps.Equal(adaptOf(back), map[string]string{"pr": "fixed"}) || back.Recipe().AdaptFor(factory.KindIssue) != factory.AdaptFree {
		t.Fatalf("%v", back.AdaptKinds)
	}
	if len(got.Settings) != 1 || got.Settings[0] != (SettingChange{Setting: SettingAdapt, Before: "adapt", After: "pr: fixed"}) {
		t.Fatalf("%+v", got.Settings)
	}
	// From one word, every other pipeline keeps it.
	base := strings.Replace(editBase, "fixed: [approve, proof]", "fixed: [approve, proof]\nadapt: ask", 1)
	back = reread(t, mustApply(t, base, adaptKind("ci", "fixed")).Text)
	if !maps.Equal(adaptOf(back), map[string]string{"issue": "ask", "pr": "ask", "ci": "fixed"}) {
		t.Fatalf("%v", back.AdaptKinds)
	}
	// Every pipeline on one word again is that word.
	back = reread(t, mustApply(t, base, adaptKind("ci", "ask")).Text)
	if back.Adapt != factory.AdaptAsk || len(back.AdaptKinds) != 0 {
		t.Fatalf("%q %v", back.Adapt, back.AdaptKinds)
	}
	// Back to the default for the only kind named writes no key.
	again := mustApply(t, got.Text, adaptKind("pr", "adapt"))
	if strings.Contains(again.Text, "adapt:") {
		t.Fatal(again.Text)
	}
	// One word for the whole file clears every pipeline's.
	whole := reread(t, mustApply(t, got.Text, EditOp{Op: OpSetAdapt, Adapt: s("ask")}).Text)
	if whole.Adapt != factory.AdaptAsk || len(whole.AdaptKinds) != 0 {
		t.Fatalf("%q %v", whole.Adapt, whole.AdaptKinds)
	}
}

func TestSetAdaptForOnePipelineRefusals(t *testing.T) {
	if _, bad := CheckEdit(0, adaptKind("Docs!", "ask")); bad == nil {
		t.Fatal("a bad pipeline name was taken")
	}
	if e := refusal(t, editBase, adaptKind("docs", "ask")); e.Code != EditUnknownStep {
		t.Fatalf("%+v", e)
	}
	if e := refusal(t, editBase, adaptKind("pr", "ask"), adaptKind("pr", "fixed")); e.Code != EditBadOp {
		t.Fatalf("%+v", e)
	}
}

func TestInspectCarriesEachPipelinesAdapt(t *testing.T) {
	src, _, ok := Inspect(adaptKindsFile)
	if !ok || src.Adapt != "" || !maps.Equal(src.AdaptKinds, map[string]string{"issue": "ask", "docs": "fixed"}) {
		t.Fatalf("%v %q %v", ok, src.Adapt, src.AdaptKinds)
	}
	if src, _, _ := Inspect(editBase); src.AdaptKinds == nil || len(src.AdaptKinds) != 0 {
		t.Fatalf("a file with one word or none carries an empty map: %v", src.AdaptKinds)
	}
}
