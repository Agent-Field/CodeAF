package factory_test

import (
	"reflect"
	"strings"
	"testing"

	"github.com/Agent-Field/codeaf/internal/factory"
)

// THE WORD AFTER THE KIND says how much plan may change that kind's stages;
// no word is adapt, and only a word that is not adapt is written back.
func TestRecipeAdaptWordParsesAndFormats(t *testing.T) {
	r, probs := factory.Parse("## issue · ask\n1. plan\n\n## pr · fixed\n1. read\n\n## ci · adapt\n1. bisect\n")
	if len(probs) != 0 {
		t.Fatalf("problems = %+v", probs)
	}
	if got := r.AdaptFor(factory.KindIssue); got != factory.AdaptAsk {
		t.Fatalf("issue = %q", got)
	}
	if got := r.AdaptFor(factory.KindPR); got != factory.AdaptFixed {
		t.Fatalf("pr = %q", got)
	}
	if got := r.AdaptFor(factory.KindCI); got != factory.AdaptFree {
		t.Fatalf("ci = %q", got)
	}
	if got := r.AdaptFor(factory.KindChore); got != factory.AdaptFree {
		t.Fatalf("a kind the file never named = %q", got)
	}
	out := factory.Format(r)
	for _, want := range []string{"\n## issue · ask\n", "\n## pr · fixed\n", "\n## ci\n"} {
		if !strings.Contains(out, want) {
			t.Fatalf("Format lost %q:\n%s", want, out)
		}
	}
	if strings.Contains(out, "· adapt") {
		t.Fatalf("Format wrote the default word:\n%s", out)
	}
	again, _ := factory.Parse(out)
	if !reflect.DeepEqual(r, again) {
		t.Fatalf("the adapt words drifted across a round trip:\n%s", out)
	}
	// A chore with a word and no stages of its own keeps its word on the way
	// through, carried by the stages it falls back to.
	c := factory.DefaultRecipe()
	c.Adapt = map[factory.Kind]factory.AdaptMode{factory.KindChore: factory.AdaptFixed}
	back, _ := factory.Parse(factory.Format(c))
	if back.AdaptFor(factory.KindChore) != factory.AdaptFixed {
		t.Fatalf("a chore's word was dropped:\n%s", factory.Format(c))
	}
}

func TestRecipeAdaptDefaultIsAdapt(t *testing.T) {
	if got := factory.DefaultRecipe().AdaptFor(factory.KindIssue); got != factory.AdaptFree {
		t.Fatalf("default = %q", got)
	}
	r, _, _ := factory.Load(t.TempDir())
	if r.AdaptFor(factory.KindPR) != factory.AdaptFree || r.Adapt != nil {
		t.Fatalf("Load fallback adapt = %+v", r.Adapt)
	}
	if strings.Contains(factory.Format(factory.DefaultRecipe()), "· adapt") {
		t.Fatal("the default recipe wrote its adapt word")
	}
}

func TestRecipeAdaptBadWordIsAProblem(t *testing.T) {
	r, probs := factory.Parse("## issue · loose\n1. plan\n\n## policy · ask\n- kept\n")
	if len(probs) != 2 || probs[0].Line != 1 || !strings.Contains(probs[0].Why, "adapt, ask, fixed") || probs[1].Line != 4 {
		t.Fatalf("problems = %+v", probs)
	}
	// The section still loads, under the default word.
	if r.AdaptFor(factory.KindIssue) != factory.AdaptFree || len(r.For(factory.KindIssue)) != 1 || len(r.Policy) != 1 {
		t.Fatalf("recipe = %+v", r)
	}
}

// A BANK CHANGES ONE SECTION AND KEEPS ITS HEADING, the word after it included.
func TestBankKeepsTheAdaptWord(t *testing.T) {
	dir := t.TempDir()
	if err := factory.Save(dir, mustParse(t, "## issue · fixed\n1. plan\n")); err != nil {
		t.Fatal(err)
	}
	if err := factory.BankRecipeStages(dir, factory.KindIssue, factory.DefaultRecipe().Stages[:2]); err != nil {
		t.Fatal(err)
	}
	r, probs, err := factory.Load(dir)
	if err != nil || len(probs) != 0 || r.AdaptFor(factory.KindIssue) != factory.AdaptFixed || len(r.Stages) != 2 {
		t.Fatalf("after a bank: %+v %+v %v", r, probs, err)
	}
}

func mustParse(t *testing.T, text string) factory.Recipe {
	t.Helper()
	r, probs := factory.Parse(text)
	if len(probs) != 0 {
		t.Fatalf("problems = %+v", probs)
	}
	return r
}

// THE CONDITION ON A GATE IS THE GATE'S: the stage runs on every item, and
// its gate stops only the items the condition fits.
func TestGateWhenRoundTripsAndApplies(t *testing.T) {
	r := mustParse(t, "## issue\n1. plan · chat · say how · when thin · gate plan when large\n")
	p := r.Stages[0]
	if p.Gate != factory.GatePlan || p.GateWhen != "large" || p.When != "thin" {
		t.Fatalf("plan = %+v", p)
	}
	line := factory.StageLines(r.Stages)[0]
	if line != "1. plan · chat · say how · when thin · gate plan when large" {
		t.Fatalf("line = %q", line)
	}
	if again := mustParse(t, factory.Format(r)); !reflect.DeepEqual(again.Stages, r.Stages) {
		t.Fatalf("drifted: %+v", again.Stages)
	}
	gated := factory.Stage{Name: "plan", Gate: factory.GatePlan, GateWhen: "large", On: true}
	large := factory.Item{Triage: factory.Triage{Size: "L", Readiness: 90}}
	small := factory.Item{Triage: factory.Triage{Size: "S", Readiness: 90}}
	if !factory.Fits(gated, small) || !factory.Fits(gated, large) {
		t.Fatal("Fits read the gate's condition")
	}
	if !factory.GateApplies(gated, large) || factory.GateApplies(gated, small) {
		t.Fatal("GateApplies did not read the gate's condition")
	}
	if !factory.GateApplies(factory.Stage{Gate: factory.GateShip}, small) {
		t.Fatal("a gate with no condition applies always")
	}
	if factory.GateApplies(factory.Stage{Gate: factory.GateNone}, large) || factory.GateApplies(factory.Stage{}, large) {
		t.Fatal("no gate applied")
	}
	if !factory.GateApplies(factory.Stage{Gate: factory.GatePlan, GateWhen: "a word nobody taught"}, small) {
		t.Fatal("an unknown condition dropped a person's stop")
	}
}

// adaptItem is an issue with the fixture's codeaf stages, plan running.
func adaptItem() factory.Item {
	stages := []factory.Stage{
		{Name: "plan", Kind: factory.StageChat, Ask: "say how", On: true},
		{Name: "write", Kind: factory.StageChat, Ask: "do it", On: true},
		{Name: "test", Kind: factory.StageCheck, Ask: "go test ./...", On: true},
		{Name: "review", Kind: factory.StageChat, Ask: "read it", On: true},
		{Name: "neaten", Kind: factory.StageChat, Ask: "neater", On: true},
		{Name: "security", Kind: factory.StageChat, Ask: "secrets", On: false},
		{Name: "sign", Kind: factory.StageGate, On: true},
		{Name: "proof", Kind: factory.StageChat, Ask: "show it", On: true},
	}
	return factory.Item{ID: 1, Kind: factory.KindIssue, Gate: factory.GateShip, Cap: 5, Stages: stages,
		Stream: &factory.Stream{Phases: []factory.Phase{{Name: "plan", State: factory.PhaseRunning}, {Name: "write", State: factory.PhasePending}}}}
}

func TestAdaptRecordsItsLinesInOrder(t *testing.T) {
	it := adaptItem()
	edit := factory.PlanEdit{
		Add:  []factory.Stage{{Name: "migrate", Ask: "after test, check the migration reverses"}},
		On:   []string{"security"},
		Skip: []string{"neaten"},
		Why:  "touches billing",
	}
	got, lines, err := factory.Adapt(it, edit, factory.DefaultRecipe())
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"plan added migrate", "plan switched on security", "plan skipped neaten", "why: touches billing"}
	if !reflect.DeepEqual(lines, want) || !reflect.DeepEqual(got.Adapted, want) {
		t.Fatalf("lines = %q, record = %q", lines, got.Adapted)
	}
	if i := factory.StageIndex(got.Stages, "migrate"); i != 3 || got.Stages[i].Kind != factory.StageChat || got.Stages[i].Ask != "check the migration reverses" || !got.Stages[i].On {
		t.Fatalf("added stage = %+v", got.Stages)
	}
	if !got.Stages[factory.StageIndex(got.Stages, "security")].On || got.Stages[factory.StageIndex(got.Stages, "neaten")].On {
		t.Fatalf("switches = %+v", got.Stages)
	}
	if got.Gate != factory.GateShip || got.Cap != 5 {
		t.Fatalf("under adapt the gate and cap moved: %q %v", got.Gate, got.Cap)
	}
	// The caller's item is not reached through the copy.
	if it.Stages[4].On != true || len(it.Stages) != 8 || it.Adapted != nil {
		t.Fatalf("the original moved: %+v", it)
	}
	if line := factory.AdaptedLine(got); line != "plan added migrate · switched on security · skipped neaten · why: touches billing" {
		t.Fatalf("drawn line = %q", line)
	}
	if factory.AdaptedLine(it) != "" {
		t.Fatal("an item plan never changed has a line")
	}
}

func TestAdaptUnderFixedRefusesEveryChange(t *testing.T) {
	r := factory.DefaultRecipe()
	r.Adapt = map[factory.Kind]factory.AdaptMode{factory.KindIssue: factory.AdaptFixed}
	it := adaptItem()
	for _, edit := range []factory.PlanEdit{{Skip: []string{"neaten"}}, {On: []string{"security"}}, {Add: []factory.Stage{{Ask: "check the docs"}}}} {
		got, lines, err := factory.Adapt(it, edit, r)
		if err == nil || err.Error() != "the recipe for issue is fixed; plan may not change the stages" || lines != nil || !reflect.DeepEqual(got, it) {
			t.Fatalf("fixed let %+v through: %v %q", edit, err, lines)
		}
	}
	// An edit that changes nothing is not a change, even under fixed.
	if _, lines, err := factory.Adapt(it, factory.PlanEdit{Why: "nothing to do"}, r); err != nil || lines != nil {
		t.Fatalf("an empty edit = %q, %v", lines, err)
	}
}

func TestAdaptNeverSkipsAGateProofOrAPolicyStage(t *testing.T) {
	r := factory.DefaultRecipe()
	r.Policy = []string{"tests pass before anything posts"}
	it := adaptItem()
	for _, name := range []string{"sign", "proof", "test"} {
		got, _, err := factory.Adapt(it, factory.PlanEdit{Skip: []string{name}}, r)
		if err == nil || !strings.Contains(err.Error(), name) || !reflect.DeepEqual(got, it) {
			t.Fatalf("skipped %s: %v", name, err)
		}
	}
	// One refused part refuses the whole edit.
	got, lines, err := factory.Adapt(it, factory.PlanEdit{Skip: []string{"neaten", "proof"}}, r)
	if err == nil || lines != nil || !got.Stages[4].On {
		t.Fatalf("half an edit was applied: %+v %v", got.Stages, err)
	}
}

func TestAdaptNeverTouchesAStageThatHasRun(t *testing.T) {
	it := adaptItem()
	it.Stream.Phases = []factory.Phase{{Name: "plan", State: factory.PhaseDone}, {Name: "write", State: factory.PhaseDone}, {Name: "test", State: factory.PhaseRunning}}
	it.Stages[1].On = false // write, done, then switched off by hand
	for _, edit := range []factory.PlanEdit{
		{Skip: []string{"test"}},
		{On: []string{"write"}},
		{Add: []factory.Stage{{Ask: "after plan, sketch it first"}}},
		{Skip: []string{"nothing-by-this-name"}},
	} {
		if got, _, err := factory.Adapt(it, edit, factory.DefaultRecipe()); err == nil || !reflect.DeepEqual(got, it) {
			t.Fatalf("%+v was applied over a stage that has run", edit)
		}
	}
	// The stages still to come are plan's to change.
	if _, lines, err := factory.Adapt(it, factory.PlanEdit{Skip: []string{"neaten"}}, factory.DefaultRecipe()); err != nil || len(lines) != 1 {
		t.Fatalf("skip a stage to come = %q, %v", lines, err)
	}
}

func TestAdaptUnderAskSetsThePlanGate(t *testing.T) {
	r := factory.DefaultRecipe()
	r.Adapt = map[factory.Kind]factory.AdaptMode{factory.KindIssue: factory.AdaptAsk}
	for _, g := range []factory.Gate{factory.GateNone, factory.GateShip, factory.GatePlan} {
		it := adaptItem()
		it.Gate = g
		got, lines, err := factory.Adapt(it, factory.PlanEdit{Skip: []string{"neaten"}}, r)
		if err != nil || len(lines) != 1 || got.Gate != factory.GatePlan {
			t.Fatalf("from %q: gate %q, %q, %v", g, got.Gate, lines, err)
		}
	}
}

func TestAdaptAddsOnlyConversationsWithAnAsk(t *testing.T) {
	it := adaptItem()
	for _, add := range []factory.Stage{{Name: "x", Kind: factory.StageCheck, Ask: "rm -rf"}, {Name: "y"}, {Ask: "after review"}, {Name: "neaten", Ask: "again"}} {
		if _, _, err := factory.Adapt(it, factory.PlanEdit{Add: []factory.Stage{add}}, factory.DefaultRecipe()); err == nil {
			t.Fatalf("plan added %+v", add)
		}
	}
	got, _, err := factory.Adapt(it, factory.PlanEdit{Add: []factory.Stage{{Ask: "check the docs say so", Gate: factory.GateNone}}}, factory.DefaultRecipe())
	if err != nil {
		t.Fatal(err)
	}
	i := factory.StageIndex(got.Stages, "check the")
	if i < 0 || got.Stages[i+1].Name != "proof" || got.Stages[i].Gate != "" || got.Stages[i].Until != factory.UntilDone {
		t.Fatalf("placed = %+v", got.Stages)
	}
}
