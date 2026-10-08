package factory_test

import (
	"encoding/json"
	"fmt"
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
	i := factory.StageIndex(got.Stages, "check")
	if i < 0 || got.Stages[i+1].Name != "proof" || got.Stages[i].Gate != "" || got.Stages[i].Until != factory.UntilDone {
		t.Fatalf("placed = %+v", got.Stages)
	}
}

// THE MANAGER'S PROGRAM, before anything runs: a new ask on a stage, a stage
// added after a named one, a skip, each stage it touched carrying who and why,
// and the record saying so in one line. The recipe's adapt word is a bound on
// a run, not on the program written before one.
func TestEditBeforeRunSetsAnAskAndAddsAfterANamedStage(t *testing.T) {
	it := adaptItem()
	it.Stream = nil
	r := factory.DefaultRecipe()
	r.Adapt = map[factory.Kind]factory.AdaptMode{factory.KindIssue: factory.AdaptFixed}
	e := factory.RunEdit{
		By:   factory.ByManager,
		Ask:  map[string]string{"review": "thorough on security, code and architecture"},
		Add:  []factory.Added{{Stage: factory.Stage{Name: "arch", Ask: "read it for the architecture"}, After: "review"}},
		Skip: []string{"neaten"},
		Why:  "touches the call row",
	}
	got, lines, err := factory.Edit(it, e, r, factory.EditBeforeRun)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"manager set review: thorough on security, code and architecture", "manager added arch after review", "manager skipped neaten", "why: touches the call row"}
	if !reflect.DeepEqual(lines, want) || !reflect.DeepEqual(got.Adapted, want) {
		t.Fatalf("lines = %q", lines)
	}
	if line := factory.AdaptedLine(got); line != "manager set review: thorough on security, code and architecture · added arch after review · skipped neaten · why: touches the call row" {
		t.Fatalf("line = %q", line)
	}
	a := factory.StageIndex(got.Stages, "arch")
	if a != 4 || got.Stages[a-1].Name != "review" || got.Stages[a].Kind != factory.StageChat || !got.Stages[a].On {
		t.Fatalf("arch placed = %+v", got.Stages)
	}
	for _, name := range []string{"review", "arch", "neaten"} {
		st := got.Stages[factory.StageIndex(got.Stages, name)]
		if st.By != factory.ByManager || st.Why != "touches the call row" {
			t.Fatalf("%s carries by %q why %q", name, st.By, st.Why)
		}
	}
	if st := got.Stages[factory.StageIndex(got.Stages, "write")]; st.By != "" || st.Why != "" {
		t.Fatalf("an untouched stage carries %q %q", st.By, st.Why)
	}
	if got.Stages[factory.StageIndex(got.Stages, "review")].Ask != "thorough on security, code and architecture" {
		t.Fatal("the ask did not land")
	}
	// The added stage's own why wins over the edit's.
	e2 := factory.RunEdit{By: factory.ByManager, Why: "the edit's", Add: []factory.Added{{Stage: factory.Stage{Name: "docs", Ask: "say it in the manual", Why: "the manual is law"}, After: "write"}}}
	got2, _, err := factory.Edit(it, e2, r, factory.EditBeforeRun)
	if err != nil || got2.Stages[2].Name != "docs" || got2.Stages[2].Why != "the manual is law" {
		t.Fatalf("own why = %+v %v", got2.Stages, err)
	}
	// The same edit during a run is held to the recipe's fixed word.
	it.Stream = adaptItem().Stream
	if _, _, err := factory.Edit(it, e, r, factory.EditInRun); err == nil || err.Error() != "the recipe for issue is fixed; manager may not change the stages" {
		t.Fatalf("in a run under fixed = %v", err)
	}
}

// IN A RUN, A STAGE THAT HAS RUN IS NOT TOUCHED: not its ask, not its
// thinking, and nothing goes in before it.
func TestEditInRunRefusesTouchingADoneStage(t *testing.T) {
	it := adaptItem()
	it.Stream.Phases = []factory.Phase{{Name: "plan", State: factory.PhaseDone}, {Name: "write", State: factory.PhaseRunning}}
	for _, c := range []struct {
		e    factory.RunEdit
		want string
	}{
		{factory.RunEdit{Ask: map[string]string{"plan": "say it again"}}, "plan may not change plan, which has already run"},
		{factory.RunEdit{Thinking: map[string]string{"write": "strong"}}, "plan may not change write, which has already run"},
		{factory.RunEdit{Add: []factory.Added{{Stage: factory.Stage{Name: "sketch", Ask: "sketch it"}, After: "plan"}}}, "plan may not add sketch before write, which has already run"},
		{factory.RunEdit{Skip: []string{"plan"}}, "plan may not skip plan, which has already run"},
	} {
		got, lines, err := factory.Edit(it, c.e, factory.DefaultRecipe(), factory.EditInRun)
		if err == nil || err.Error() != c.want || lines != nil || !reflect.DeepEqual(got, it) {
			t.Fatalf("%+v = %v, want %q", c.e, err, c.want)
		}
	}
	// What is still to come is the plan's to change.
	got, lines, err := factory.Edit(it, factory.RunEdit{Ask: map[string]string{"review": "read it twice"}, Thinking: map[string]string{"review": "strong"}}, factory.DefaultRecipe(), factory.EditInRun)
	if err != nil || len(lines) != 2 || lines[1] != "plan set review thinking strong" || got.Stages[3].Effort != "strong" || got.Stages[3].By != factory.ByPlan {
		t.Fatalf("to come = %q %v %+v", lines, err, got.Stages[3])
	}
}

// ONE WORD PER STAGE on all three roads that make a name: an edit, the recipe
// file, and a typed sentence.
func TestStageNamesAreOneWordOnEveryRoad(t *testing.T) {
	const want = `a stage is one word · "do through" is two`
	it := adaptItem()
	if _, _, err := factory.Edit(it, factory.RunEdit{Add: []factory.Added{{Stage: factory.Stage{Name: "do through", Ask: "walk it end to end"}}}}, factory.DefaultRecipe(), factory.EditBeforeRun); err == nil || err.Error() != want {
		t.Fatalf("edit = %v", err)
	}
	if _, _, err := factory.Edit(it, factory.RunEdit{Add: []factory.Added{{Stage: factory.Stage{Ask: "after review, do through: walk it end to end"}}}}, factory.DefaultRecipe(), factory.EditBeforeRun); err == nil || err.Error() != want {
		t.Fatalf("edit by sentence = %v", err)
	}
	r, probs := factory.Parse("## issue\n1. plan\n2. do through · chat · walk it end to end\n3. proof\n")
	if len(probs) != 1 || probs[0].Line != 3 || probs[0].Why != want || len(r.Stages) != 2 {
		t.Fatalf("recipe file = %+v %+v", probs, r.Stages)
	}
	if _, _, err := factory.StageSentence("after review, do through: walk it end to end"); err == nil || err.Error() != want {
		t.Fatalf("sentence = %v", err)
	}
	// A sentence with no name of its own is named by one word of its ask.
	got, _, err := factory.Adapt(it, factory.PlanEdit{Add: []factory.Stage{{Ask: "after test, check the migration reverses"}}}, factory.DefaultRecipe())
	if err != nil || factory.StageIndex(got.Stages, "check") != 3 {
		t.Fatalf("unnamed = %+v %v", got.Stages, err)
	}
}

// AT MOST NINE STAGES: an edit, and a recipe file, stop at nine.
func TestNineStagesAtMost(t *testing.T) {
	it := adaptItem() // eight stages
	add := func(names ...string) factory.RunEdit {
		e := factory.RunEdit{By: factory.ByManager}
		for _, n := range names {
			e.Add = append(e.Add, factory.Added{Stage: factory.Stage{Name: n, Ask: "look at " + n}, After: "review"})
		}
		return e
	}
	if _, _, err := factory.Edit(it, add("arch"), factory.DefaultRecipe(), factory.EditBeforeRun); err != nil {
		t.Fatalf("the ninth = %v", err)
	}
	got, _, err := factory.Edit(it, add("arch", "docs"), factory.DefaultRecipe(), factory.EditBeforeRun)
	if err == nil || err.Error() != "the run has nine stages already" || len(got.Stages) != 8 {
		t.Fatalf("the tenth = %v", err)
	}
	text := "## issue\n"
	for i, n := range []string{"plan", "write", "test", "review", "neaten", "security", "docs", "arch", "proof", "extra"} {
		text += fmt.Sprintf("%d. %s · chat · do %s\n", i+1, n, n)
	}
	r, probs := factory.Parse(text)
	if len(probs) != 1 || probs[0].Line != 11 || probs[0].Why != "the run has nine stages already" || len(r.Stages) != 9 {
		t.Fatalf("recipe file = %+v", probs)
	}
}

// PROOF IS NEVER SKIPPED, by anyone, in either mode; nor a gate.
func TestEditNeverSkipsProofOrAGate(t *testing.T) {
	it := adaptItem()
	it.Stream = nil
	for _, by := range []string{factory.ByManager, factory.ByPlan, factory.ByYou} {
		for _, mode := range []factory.EditMode{factory.EditBeforeRun, factory.EditInRun} {
			if _, _, err := factory.Edit(it, factory.RunEdit{By: by, Skip: []string{"proof"}}, factory.DefaultRecipe(), mode); err == nil || err.Error() != by+" may not skip proof" {
				t.Fatalf("%s %s skipped proof: %v", by, mode, err)
			}
			if _, _, err := factory.Edit(it, factory.RunEdit{By: by, Skip: []string{"sign"}}, factory.DefaultRecipe(), mode); err == nil || !strings.Contains(err.Error(), "person's gate") {
				t.Fatalf("%s %s skipped the gate: %v", by, mode, err)
			}
		}
	}
}

// AN ASK IS AT MOST 240 CELLS and thinking is one of three words; one refused
// part refuses the whole edit.
func TestEditCapsTheAskAndTheThinking(t *testing.T) {
	it := adaptItem()
	it.Stream = nil
	long := strings.Repeat("a", 241)
	for _, e := range []factory.RunEdit{
		{Ask: map[string]string{"review": long}},
		{Add: []factory.Added{{Stage: factory.Stage{Name: "arch", Ask: long}}}},
	} {
		if _, _, err := factory.Edit(it, e, factory.DefaultRecipe(), factory.EditBeforeRun); err == nil || err.Error() != "an ask is at most 240 cells" {
			t.Fatalf("long ask = %v", err)
		}
	}
	if _, _, err := factory.Edit(it, factory.RunEdit{Ask: map[string]string{"review": strings.Repeat("a", 240)}}, factory.DefaultRecipe(), factory.EditBeforeRun); err != nil {
		t.Fatalf("240 cells = %v", err)
	}
	got, _, err := factory.Edit(it, factory.RunEdit{Thinking: map[string]string{"review": "furious"}, Skip: []string{"neaten"}}, factory.DefaultRecipe(), factory.EditBeforeRun)
	if err == nil || err.Error() != "thinking is cheap, strong, or nothing" || !got.Stages[4].On {
		t.Fatalf("thinking = %v", err)
	}
}

// THE RECORD SAYS WHO FIRST, and says it again only when who changes.
func TestAdaptedLineSaysWhoOnce(t *testing.T) {
	it := factory.Item{Adapted: []string{"manager added arch after review", "manager skipped neaten", "why: touches the call row", "plan set review thinking strong", "you switched on neaten"}}
	if got := factory.AdaptedLine(it); got != "manager added arch after review · skipped neaten · why: touches the call row · plan set review thinking strong · you switched on neaten" {
		t.Fatalf("line = %q", got)
	}
}

// THE OLD EDIT STILL COMPILES and is the plan's RunEdit.
func TestPlanEditIsTheRunEditByPlan(t *testing.T) {
	old := factory.PlanEdit{Add: []factory.Stage{{Ask: "after test, check it"}}, On: []string{"security"}, Skip: []string{"neaten"}, Why: "why"}
	e := old.RunEdit()
	if e.By != factory.ByPlan || len(e.Add) != 1 || e.Add[0].Stage.Ask != "after test, check it" || e.Add[0].After != "" || e.Why != "why" || old.Empty() {
		t.Fatalf("run edit = %+v", e)
	}
	var _ = factory.Adapt
}

// WHY AND BY ARE IN THE DOCUMENT THE STORE WRITES, and a document written
// before them reads as the recipe's own.
func TestStageWhyAndByRoundTrip(t *testing.T) {
	data, err := json.Marshal(factory.Stage{Name: "arch", Why: "touches the call row", By: factory.ByManager})
	if err != nil || !strings.Contains(string(data), `"Why":"touches the call row"`) || !strings.Contains(string(data), `"By":"manager"`) {
		t.Fatalf("marshal = %s %v", data, err)
	}
	var old factory.Stage
	if err := json.Unmarshal([]byte(`{"Name":"review","Ask":"read it","On":true}`), &old); err != nil || old.Why != "" || old.By != "" || old.Name != "review" {
		t.Fatalf("old document = %+v %v", old, err)
	}
}
