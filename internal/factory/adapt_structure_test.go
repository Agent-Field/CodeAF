package factory_test

import (
	"reflect"
	"strings"
	"testing"

	"github.com/Agent-Field/codeaf/internal/factory"
)

// ── REMOVING AND MOVING A STEP OF ONE JOB ───────────────────────────────────
//
// A person asked a job's manager to remove a step it had added (`pretest`); it
// could only skip it. These pin the two verbs [factory.Edit] now takes for one
// job, and every bound they share with the verbs it had: what has begun, proof,
// a fixed step, a step the policy names and a person's approval stay where they
// are, an edit is whole or nothing, and the recipe's adapt word holds.

// structureItem is adaptItem before anything runs, with the manager's own
// `pretest` added after write and since skipped (off), as in the report.
func structureItem() factory.Item {
	it := adaptItem()
	it.Stream = nil
	it.Stages = append(it.Stages[:2:2], append([]factory.Stage{{Name: "pretest", Kind: factory.StageChat, Ask: "run the old tests", On: false, By: factory.ByManager}}, it.Stages[2:]...)...)
	return it
}

// eightSteps is structureItem without its test step, so an edit that adds a
// step stays within the nine a job may have.
func eightSteps() factory.Item {
	it := structureItem()
	i := factory.StageIndex(it.Stages, "test")
	it.Stages = append(it.Stages[:i:i], it.Stages[i+1:]...)
	return it
}

func stepOrder(stages []factory.Stage) string {
	names := make([]string, len(stages))
	for i, st := range stages {
		names[i] = st.Name
	}
	return strings.Join(names, " ")
}

// THE REPORT'S CASE: a step the manager added and then skipped can be taken out
// of the job, not only left off, and the record says so.
func TestEditRemovesAStepFromTheJob(t *testing.T) {
	it := structureItem()
	got, lines, err := factory.Edit(it, factory.RunEdit{By: factory.ByManager, Remove: []string{"Pretest"}, Why: "the person asked"}, factory.DefaultRecipe(), factory.EditBeforeRun)
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{"manager removed pretest", "why: the person asked"}; !reflect.DeepEqual(lines, want) || !reflect.DeepEqual(got.Adapted, want) {
		t.Fatalf("lines = %q, record = %q", lines, got.Adapted)
	}
	if got := stepOrder(got.Stages); got != "plan write test review neaten security sign proof" {
		t.Fatalf("stages = %s", got)
	}
	if stepOrder(it.Stages) != "plan write pretest test review neaten security sign proof" {
		t.Fatal("the caller's item moved")
	}
}

// A MOVE PUTS A STEP AFTER ANOTHER, OR FIRST, carries who and why, and a move
// to where the step stands says nothing.
func TestEditMovesAStepAfterAnotherOrFirst(t *testing.T) {
	it := structureItem()
	got, lines, err := factory.Edit(it, factory.RunEdit{By: factory.ByManager, Move: []factory.Moved{{Stage: "neaten", After: "Write"}, {Stage: "review", After: ""}}, Why: "read it early"}, factory.DefaultRecipe(), factory.EditBeforeRun)
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{"manager moved neaten after write", "manager moved review first", "why: read it early"}; !reflect.DeepEqual(lines, want) {
		t.Fatalf("lines = %q", lines)
	}
	if got := stepOrder(got.Stages); got != "review plan write neaten pretest test security sign proof" {
		t.Fatalf("stages = %s", got)
	}
	for _, name := range []string{"neaten", "review"} {
		st := got.Stages[factory.StageIndex(got.Stages, name)]
		if st.By != factory.ByManager || st.Why != "read it early" {
			t.Fatalf("%s carries by %q why %q", name, st.By, st.Why)
		}
	}
	if line := factory.AdaptedLine(got); line != "manager moved neaten after write · moved review first · why: read it early" {
		t.Fatalf("drawn line = %q", line)
	}
	// Where it already stands: no change, no line, no record.
	same, lines, err := factory.Edit(it, factory.RunEdit{By: factory.ByManager, Move: []factory.Moved{{Stage: "write", After: "plan"}}}, factory.DefaultRecipe(), factory.EditBeforeRun)
	if err != nil || lines != nil || !reflect.DeepEqual(same, it) {
		t.Fatalf("a move to its own place = %q, %v", lines, err)
	}
}

// THE LINES KEEP THEIR DOCUMENTED ORDER: set, added, switched on, skipped,
// removed, moved, why.
func TestEditRecordsRemoveAndMoveAfterTheOtherParts(t *testing.T) {
	it := eightSteps()
	e := factory.RunEdit{
		By:     factory.ByYou,
		Move:   []factory.Moved{{Stage: "review", After: "plan"}},
		Remove: []string{"pretest"},
		Skip:   []string{"neaten"},
		On:     []string{"security"},
		Add:    []factory.Added{{Stage: factory.Stage{Name: "docs", Ask: "say it in the manual"}, After: "write"}},
		Ask:    map[string]string{"plan": "say how, briefly"},
		Why:    "a smaller run",
	}
	got, lines, err := factory.Edit(it, e, factory.DefaultRecipe(), factory.EditBeforeRun)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"you set plan: say how, briefly", "you added docs after write", "you switched on security", "you skipped neaten", "you removed pretest", "you moved review after plan", "why: a smaller run"}
	if !reflect.DeepEqual(lines, want) {
		t.Fatalf("lines = %q", lines)
	}
	if got := stepOrder(got.Stages); got != "plan review write docs neaten security sign proof" {
		t.Fatalf("stages = %s", got)
	}
}

// EVERY BOUND THE OTHER VERBS HOLD, HELD FOR REMOVE AND MOVE, each refused in
// its own sentence with the item exactly as it was.
func TestEditRemoveAndMoveKeepEveryBound(t *testing.T) {
	policy := factory.DefaultRecipe()
	policy.Policy = []string{"tests pass before anything posts"}
	fixedReview := structureItem()
	fixedReview.Stages[factory.StageIndex(fixedReview.Stages, "review")].Fixed = true
	running := structureItem()
	running.Stream = &factory.Stream{Phases: []factory.Phase{{Name: "plan", State: factory.PhaseDone}, {Name: "write", State: factory.PhaseRunning}}}
	for _, c := range []struct {
		name string
		it   factory.Item
		r    factory.Recipe
		e    factory.RunEdit
		mode factory.EditMode
		want string
	}{
		{"remove proof", structureItem(), factory.Recipe{}, factory.RunEdit{By: factory.ByYou, Remove: []string{"proof"}}, factory.EditBeforeRun, "you may not remove proof; proof vouches for the work before it"},
		{"move proof", structureItem(), factory.Recipe{}, factory.RunEdit{By: factory.ByYou, Move: []factory.Moved{{Stage: "proof", After: "plan"}}}, factory.EditBeforeRun, "you may not move proof; proof vouches for the work before it"},
		{"remove a fixed step", fixedReview, factory.DefaultRecipe(), factory.RunEdit{By: factory.ByYou, Remove: []string{"review"}}, factory.EditBeforeRun, "review is fixed by the recipe · change .codeaf/factory.yaml to change it"},
		{"move a fixed step", fixedReview, factory.DefaultRecipe(), factory.RunEdit{By: factory.ByYou, Move: []factory.Moved{{Stage: "review", After: "plan"}}}, factory.EditBeforeRun, "review is fixed by the recipe · change .codeaf/factory.yaml to change it"},
		{"remove the recipe's fixed proof", structureItem(), factory.DefaultRecipe(), factory.RunEdit{By: factory.ByYou, Remove: []string{"proof"}}, factory.EditBeforeRun, "proof is fixed by the recipe · change .codeaf/factory.yaml to change it"},
		{"remove a step the policy names", structureItem(), policy, factory.RunEdit{By: factory.ByYou, Remove: []string{"test"}}, factory.EditBeforeRun, "you may not remove test; the policy names it"},
		{"move a step the policy names", structureItem(), policy, factory.RunEdit{By: factory.ByManager, Move: []factory.Moved{{Stage: "test", After: "review"}}}, factory.EditBeforeRun, "manager may not move test; the policy names it"},
		{"the manager removes an approval", structureItem(), factory.DefaultRecipe(), factory.RunEdit{By: factory.ByManager, Remove: []string{"sign"}}, factory.EditBeforeRun, "only you may remove sign, where the run holds for you"},
		{"the manager moves an approval", structureItem(), factory.DefaultRecipe(), factory.RunEdit{By: factory.ByManager, Move: []factory.Moved{{Stage: "sign", After: "plan"}}}, factory.EditBeforeRun, "only you may move sign, where the run holds for you"},
		{"remove a step that ran", running, factory.DefaultRecipe(), factory.RunEdit{By: factory.ByYou, Remove: []string{"plan"}}, factory.EditInRun, "you may not remove plan, which has already run"},
		{"move a running step", running, factory.DefaultRecipe(), factory.RunEdit{By: factory.ByYou, Move: []factory.Moved{{Stage: "write", After: "review"}}}, factory.EditInRun, "you may not move write, which has already run"},
		{"move a step before one that ran", running, factory.DefaultRecipe(), factory.RunEdit{By: factory.ByYou, Move: []factory.Moved{{Stage: "review", After: "plan"}}}, factory.EditInRun, "you may not move review before write, which has already run"},
		{"move a step first before one that ran", running, factory.DefaultRecipe(), factory.RunEdit{By: factory.ByYou, Move: []factory.Moved{{Stage: "review"}}}, factory.EditInRun, "you may not move review before plan, which has already run"},
		{"move work after proof", structureItem(), factory.DefaultRecipe(), factory.RunEdit{By: factory.ByYou, Move: []factory.Moved{{Stage: "neaten", After: "proof"}}}, factory.EditBeforeRun, "you may not move neaten after proof; proof has to see it"},
		{"after itself", structureItem(), factory.DefaultRecipe(), factory.RunEdit{By: factory.ByYou, Move: []factory.Moved{{Stage: "neaten", After: "neaten"}}}, factory.EditBeforeRun, "a step does not go after itself; neaten stays where it is"},
		{"after a step the job lacks", structureItem(), factory.DefaultRecipe(), factory.RunEdit{By: factory.ByYou, Move: []factory.Moved{{Stage: "neaten", After: "lint"}}}, factory.EditBeforeRun, "there is no stage named lint to move after"},
		{"remove a step the job lacks", structureItem(), factory.DefaultRecipe(), factory.RunEdit{By: factory.ByYou, Remove: []string{"lint"}}, factory.EditBeforeRun, "there is no stage named lint to remove"},
		{"moved twice", structureItem(), factory.DefaultRecipe(), factory.RunEdit{By: factory.ByYou, Move: []factory.Moved{{Stage: "neaten", After: "plan"}, {Stage: "neaten", After: "write"}}}, factory.EditBeforeRun, "neaten is moved once in one edit"},
		{"removed and changed", structureItem(), factory.DefaultRecipe(), factory.RunEdit{By: factory.ByYou, Ask: map[string]string{"neaten": "neater still"}, Remove: []string{"neaten"}}, factory.EditBeforeRun, "neaten is changed by the same edit, so it is not removed"},
		{"removed after a step was added after it", eightSteps(), factory.DefaultRecipe(), factory.RunEdit{By: factory.ByYou, Add: []factory.Added{{Stage: factory.Stage{Name: "docs", Ask: "say it"}, After: "neaten"}}, Remove: []string{"neaten"}}, factory.EditBeforeRun, "neaten is changed by the same edit, so it is not removed"},
		{"moved after a removed step", structureItem(), factory.DefaultRecipe(), factory.RunEdit{By: factory.ByYou, Remove: []string{"pretest"}, Move: []factory.Moved{{Stage: "neaten", After: "pretest"}}}, factory.EditBeforeRun, "there is no stage named pretest to move after"},
	} {
		got, lines, err := factory.Edit(c.it, c.e, c.r, c.mode)
		if err == nil || err.Error() != c.want || lines != nil || !reflect.DeepEqual(got, c.it) {
			t.Errorf("%s = %v (lines %q), want %q", c.name, err, lines, c.want)
		}
	}
}

// AN EDIT IS WHOLE OR NOTHING: one refused removal or move leaves every other
// part of the same edit unapplied.
func TestEditRemoveAndMoveAreWholeOrNothing(t *testing.T) {
	it := structureItem()
	got, lines, err := factory.Edit(it, factory.RunEdit{By: factory.ByManager, Skip: []string{"neaten"}, Remove: []string{"pretest", "sign"}, Move: []factory.Moved{{Stage: "review", After: "plan"}}}, factory.DefaultRecipe(), factory.EditBeforeRun)
	if err == nil || lines != nil || !reflect.DeepEqual(got, it) {
		t.Fatalf("half an edit was applied: %s %v", stepOrder(got.Stages), err)
	}
}

// ONLY THE PERSON REMOVES OR MOVES WHERE THE RUN HOLDS FOR THEM, and may put
// their approval after proof, where it does no work; a conversation may cross
// an approval.
func TestEditThePersonRemovesAndMovesTheirApproval(t *testing.T) {
	it := structureItem()
	got, lines, err := factory.Edit(it, factory.RunEdit{By: factory.ByYou, Move: []factory.Moved{{Stage: "sign", After: "proof"}}}, factory.DefaultRecipe(), factory.EditBeforeRun)
	if err != nil || len(lines) != 1 || lines[0] != "you moved sign after proof" || stepOrder(got.Stages) != "plan write pretest test review neaten security proof sign" {
		t.Fatalf("move = %q %s %v", lines, stepOrder(got.Stages), err)
	}
	got, lines, err = factory.Edit(it, factory.RunEdit{By: factory.ByYou, Remove: []string{"sign"}}, factory.DefaultRecipe(), factory.EditBeforeRun)
	if err != nil || len(lines) != 1 || factory.StageIndex(got.Stages, "sign") >= 0 {
		t.Fatalf("remove = %q %v", lines, err)
	}
	got, _, err = factory.Edit(it, factory.RunEdit{By: factory.ByManager, Move: []factory.Moved{{Stage: "neaten", After: "sign"}}}, factory.DefaultRecipe(), factory.EditBeforeRun)
	if err != nil || stepOrder(got.Stages) != "plan write pretest test review security sign neaten proof" {
		t.Fatalf("a conversation crossing an approval = %s %v", stepOrder(got.Stages), err)
	}
}

// THE RECIPE'S ADAPT WORD HOLDS: under fixed only the person removes or moves;
// under ask a run's change gets an approve step after plan; and a job keeps at
// least one step.
func TestEditRemoveAndMoveUnderTheAdaptWord(t *testing.T) {
	fixed := factory.DefaultRecipe()
	fixed.Adapt = map[factory.Kind]factory.AdaptMode{factory.KindIssue: factory.AdaptFixed}
	it := structureItem()
	for _, e := range []factory.RunEdit{{By: factory.ByManager, Remove: []string{"pretest"}}, {By: factory.ByManager, Move: []factory.Moved{{Stage: "neaten", After: "plan"}}}} {
		if got, _, err := factory.Edit(it, e, fixed, factory.EditBeforeRun); err == nil || err.Error() != "the recipe for issue is fixed; manager may not change the stages" || !reflect.DeepEqual(got, it) {
			t.Fatalf("under fixed the manager's %+v = %v", e, err)
		}
		you := e
		you.By = factory.ByYou
		if _, lines, err := factory.Edit(it, you, fixed, factory.EditBeforeRun); err != nil || len(lines) != 1 {
			t.Fatalf("under fixed the person's %+v = %q %v", you, lines, err)
		}
	}
	ask := factory.DefaultRecipe()
	ask.Adapt = map[factory.Kind]factory.AdaptMode{factory.KindIssue: factory.AdaptAsk}
	run := structureItem()
	run.Stream = &factory.Stream{Phases: []factory.Phase{{Name: "plan", State: factory.PhaseRunning}}}
	got, lines, err := factory.Edit(run, factory.RunEdit{By: factory.ByManager, Remove: []string{"pretest"}}, ask, factory.EditInRun)
	if err != nil || len(lines) != 1 || got.Stages[1].Name != factory.ApproveName || got.Stages[1].Kind != factory.StageGate {
		t.Fatalf("under ask = %q %s %v", lines, stepOrder(got.Stages), err)
	}
	one := factory.Item{ID: 2, Kind: factory.KindIssue, Stages: []factory.Stage{{Name: "write", Kind: factory.StageChat, Ask: "do it", On: true}}}
	if _, _, err := factory.Edit(one, factory.RunEdit{By: factory.ByYou, Remove: []string{"write"}}, factory.Recipe{}, factory.EditBeforeRun); err == nil || err.Error() != "a job keeps at least one step; write is its last" {
		t.Fatalf("the last step = %v", err)
	}
}

// ONCE PROOF HAS STARTED, OR THE JOB HAS LANDED, NOTHING IS REMOVED OR MOVED,
// by the same sentences as every other edit; a dropped job is never edited.
func TestEditRemoveAndMoveAreFrozenAfterProofStarts(t *testing.T) {
	it := structureItem()
	it.Stream = &factory.Stream{Phases: []factory.Phase{{Name: "plan", State: factory.PhaseDone}, {Name: "proof", State: factory.PhaseRunning}}}
	for _, e := range []factory.RunEdit{{By: factory.ByYou, Remove: []string{"neaten"}}, {By: factory.ByYou, Move: []factory.Moved{{Stage: "neaten", After: "write"}}}} {
		if _, _, err := factory.Edit(it, e, factory.DefaultRecipe(), factory.EditInRun); err == nil || err.Error() != factory.ProofFrozenRefusal {
			t.Fatalf("%+v after proof started = %v", e, err)
		}
	}
	done := structureItem()
	done.State = factory.StateLanded
	if _, _, err := factory.Edit(done, factory.RunEdit{By: factory.ByYou, Remove: []string{"neaten"}}, factory.DefaultRecipe(), factory.EditBeforeRun); err == nil || !strings.Contains(err.Error(), "landed") {
		t.Fatalf("a landed job = %v", err)
	}
}

// THE PLAN'S OLD SHAPE CARRIES BOTH VERBS (the item card's road), as plan.
func TestPlanEditCarriesRemoveAndMove(t *testing.T) {
	e := factory.PlanEdit{Remove: []string{"pretest"}, Move: []factory.Moved{{Stage: "neaten", After: "write"}}}
	if e.Empty() {
		t.Fatal("a removal and a move are a change")
	}
	r := e.RunEdit()
	if r.By != factory.ByPlan || !reflect.DeepEqual(r.Remove, e.Remove) || !reflect.DeepEqual(r.Move, e.Move) {
		t.Fatalf("run edit = %+v", r)
	}
}

// A POST IS THE PERSON'S, as the repository's pipeline editor locks one: only
// they remove or move it, and nobody moves it before the proof or the approve
// step it stood after, nor moves that proof or approval after it, because
// nothing posts before what holds it. After proof it may stand anywhere: that
// is where a proved change is published.
func TestEditOnlyThePersonRemovesOrMovesAPost(t *testing.T) {
	posting := func() factory.Item {
		it := structureItem()
		it.Stages = append(it.Stages, factory.Stage{Name: "label", Kind: factory.StagePost, Ask: "label: ready", On: true}, factory.Stage{Name: "pr", Kind: factory.StagePost, Ask: "pr", On: true})
		return it
	}
	for _, c := range []struct {
		name string
		e    factory.RunEdit
		want string
	}{
		{"the manager removes a post", factory.RunEdit{By: factory.ByManager, Remove: []string{"pr"}}, "only you may remove pr, which posts outward"},
		{"the plan moves a post", factory.RunEdit{By: factory.ByPlan, Move: []factory.Moved{{Stage: "pr", After: "label"}}}, "only you may move pr, which posts outward"},
		{"a post before proof", factory.RunEdit{By: factory.ByYou, Move: []factory.Moved{{Stage: "pr", After: "neaten"}}}, "you may not move pr before sign; nothing posts before what holds it"},
		{"a post first", factory.RunEdit{By: factory.ByYou, Move: []factory.Moved{{Stage: "label"}}}, "you may not move label before sign; nothing posts before what holds it"},
		// The other way round is the same post going first: the approval
		// moved after the posts it held.
		{"an approval after its post", factory.RunEdit{By: factory.ByYou, Move: []factory.Moved{{Stage: "sign", After: "pr"}}}, "you may not move sign after label; nothing posts before what holds it"},
		{"an approval between its posts", factory.RunEdit{By: factory.ByYou, Move: []factory.Moved{{Stage: "sign", After: "label"}}}, "you may not move sign after label; nothing posts before what holds it"},
	} {
		it := posting()
		got, lines, err := factory.Edit(it, c.e, factory.Recipe{}, factory.EditBeforeRun)
		if err == nil || err.Error() != c.want || lines != nil || !reflect.DeepEqual(got, it) {
			t.Errorf("%s = %v (lines %q), want %q", c.name, err, lines, c.want)
		}
	}
	it := posting()
	got, lines, err := factory.Edit(it, factory.RunEdit{By: factory.ByYou, Move: []factory.Moved{{Stage: "pr", After: "proof"}}}, factory.Recipe{}, factory.EditBeforeRun)
	if err != nil || stepOrder(got.Stages) != "plan write pretest test review neaten security sign proof pr label" || strings.Join(lines, "|") != "you moved pr after proof" {
		t.Fatalf("the person's own move = %s %q %v", stepOrder(got.Stages), lines, err)
	}
	// An approval moved after proof, still before the posts, holds them yet.
	got, lines, err = factory.Edit(it, factory.RunEdit{By: factory.ByYou, Move: []factory.Moved{{Stage: "sign", After: "proof"}}}, factory.Recipe{}, factory.EditBeforeRun)
	if err != nil || stepOrder(got.Stages) != "plan write pretest test review neaten security proof sign label pr" || strings.Join(lines, "|") != "you moved sign after proof" {
		t.Fatalf("the approval after proof = %s %q %v", stepOrder(got.Stages), lines, err)
	}
	got, lines, err = factory.Edit(it, factory.RunEdit{By: factory.ByYou, Remove: []string{"label"}}, factory.Recipe{}, factory.EditBeforeRun)
	if err != nil || stepOrder(got.Stages) != "plan write pretest test review neaten security sign proof pr" || strings.Join(lines, "|") != "you removed label" {
		t.Fatalf("the person's own removal = %s %q %v", stepOrder(got.Stages), lines, err)
	}
}
