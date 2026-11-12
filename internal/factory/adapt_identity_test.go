package factory_test

import (
	"reflect"
	"strings"
	"testing"

	"github.com/Agent-Field/codeaf/internal/factory"
)

// namedStages is an item whose stages are conversations of exactly those
// names and efforts, spelled as given: a document an older build could have
// written, which no current door writes.
func namedStages(efforts map[int]string, names ...string) factory.Item {
	it := factory.Item{ID: 1, Kind: factory.KindIssue}
	for i, n := range names {
		it.Stages = append(it.Stages, factory.Stage{Name: n, Kind: factory.StageChat, Ask: "do " + n, On: true, Until: factory.UntilDone, Effort: efforts[i]})
	}
	return it
}

func stageNames(it factory.Item) []string {
	var out []string
	for _, st := range it.Stages {
		out = append(out, st.Name)
	}
	return out
}

// THE LOOKUP IS WITHOUT CASE AND THE STORED SPELLING IS KEPT: a legacy `Tidy`
// is reached by any spelling of its name and is never renamed by the edit.
func TestEditFindsALegacyCasedStageByAnyCase(t *testing.T) {
	for _, ask := range []string{"tidy", "TIDY", " Tidy ", "Tidy"} {
		it := namedStages(nil, "plan", "Tidy")
		got, lines, err := factory.Edit(it, factory.RunEdit{By: factory.ByYou,
			Ask: map[string]string{ask: "sweep the dead code"}, Thinking: map[string]string{ask: "strong"}},
			factory.DefaultRecipe(), factory.EditBeforeRun)
		if err != nil {
			t.Fatalf("%q: %v", ask, err)
		}
		if names := stageNames(got); !reflect.DeepEqual(names, []string{"plan", "Tidy"}) {
			t.Fatalf("%q renamed the stages: %v", ask, names)
		}
		if got.Stages[1].Effort != "strong" || got.Stages[1].Ask != "sweep the dead code" {
			t.Fatalf("%q: stage = %+v", ask, got.Stages[1])
		}
		if joined := strings.Join(lines, "|"); !strings.Contains(joined, "you set Tidy thinking strong") {
			t.Fatalf("%q: the receipt does not keep the stored spelling: %v", ask, lines)
		}
	}
	for _, c := range []factory.RunEdit{{On: []string{"TIDY"}}, {Skip: []string{"tidy"}}} {
		c.By = factory.ByYou
		it := namedStages(nil, "plan", "Tidy")
		if len(c.On) > 0 {
			it.Stages[1].On = false
		}
		got, _, err := factory.Edit(it, c, factory.DefaultRecipe(), factory.EditBeforeRun)
		if err != nil || got.Stages[1].On != (len(c.On) > 0) || got.Stages[1].Name != "Tidy" {
			t.Fatalf("%+v: stage = %+v, err = %v", c, got.Stages[1], err)
		}
	}
	// A name no stage has still says the old sentence.
	if _, _, err := factory.Edit(namedStages(nil, "Tidy"), factory.RunEdit{By: factory.ByYou, Skip: []string{"sweep"}},
		factory.DefaultRecipe(), factory.EditBeforeRun); err == nil || !strings.Contains(err.Error(), "there is no stage named sweep to skip") {
		t.Fatalf("an unknown name = %v", err)
	}
}

// A NAME TWO STAGES ANSWER TO REFUSES THE WHOLE EDIT, whichever way it is
// spelled, and nothing else the edit asked for is applied.
func TestEditRefusesAnAmbiguousNameWholly(t *testing.T) {
	const ambiguous = "more than one stage is named"
	for _, names := range [][]string{{"tidy", "tidy"}, {"Tidy", "tidy"}, {"Tidy", "TIDY"}} {
		for _, req := range []string{"tidy", "Tidy", "TIDY"} {
			for kind, e := range map[string]factory.RunEdit{
				"ask":      {Ask: map[string]string{req: "new words"}},
				"thinking": {Thinking: map[string]string{req: "strong"}},
				"skip":     {Skip: []string{req}},
				"on":       {On: []string{req}},
				"add":      {Add: []factory.Added{{Stage: factory.Stage{Name: "extra", Ask: "look again"}, After: req}}},
			} {
				// The same edit also carries a good change to plan.
				e.By = factory.ByYou
				if e.Thinking == nil {
					e.Thinking = map[string]string{}
				}
				e.Thinking["plan"] = "cheap"
				it := namedStages(map[int]string{1: "cheap", 2: "strong"}, append([]string{"plan"}, names...)...)
				it.Stages[2].On = false
				it.Cap, it.Notes = 5, []string{"first"}
				before := factory.CopyStages(it.Stages)
				got, lines, err := factory.Edit(it, e, factory.DefaultRecipe(), factory.EditBeforeRun)
				if err == nil || !strings.Contains(err.Error(), ambiguous) {
					t.Fatalf("%v asked as %q (%s): err = %v", names, req, kind, err)
				}
				if lines != nil || !reflect.DeepEqual(got, it) || !reflect.DeepEqual(it.Stages, before) || len(got.Adapted) != 0 {
					t.Fatalf("%v asked as %q (%s): a refused edit changed the item:\n%+v", names, req, kind, got)
				}
			}
		}
	}
	// An ambiguity the edit does not select stands in the way of nothing.
	it := namedStages(nil, "plan", "tidy", "Tidy")
	got, _, err := factory.Edit(it, factory.RunEdit{By: factory.ByYou, Thinking: map[string]string{"plan": "cheap"}},
		factory.DefaultRecipe(), factory.EditBeforeRun)
	if err != nil || got.Stages[0].Effort != "cheap" {
		t.Fatalf("an edit that never names the clash = %v, %+v", err, got.Stages)
	}
}

// NEW WRITES NEVER MINT THE CLASH: a stage cannot be added beside a legacy
// name that differs only in case, and a second approve step still gets its own.
func TestEditAddCannotCreateAFoldCollision(t *testing.T) {
	it := namedStages(nil, "plan", "Tidy")
	got, _, err := factory.Edit(it, factory.RunEdit{By: factory.ByYou,
		Add: []factory.Added{{Stage: factory.Stage{Name: "tidy", Ask: "sweep again"}, After: "plan"}}},
		factory.DefaultRecipe(), factory.EditBeforeRun)
	if err == nil || !strings.Contains(err.Error(), "there is already a stage named tidy") {
		t.Fatalf("add tidy beside Tidy = %v", err)
	}
	if !reflect.DeepEqual(got, it) {
		t.Fatalf("a refused add changed the item: %+v", got.Stages)
	}
	legacy := namedStages(nil, "plan", "Approve")
	got, _, err = factory.Edit(legacy, factory.RunEdit{By: factory.ByYou, Add: []factory.Added{{Stage: factory.Stage{Name: factory.ApproveName}, After: "plan"}}},
		factory.DefaultRecipe(), factory.EditBeforeRun)
	if err != nil || len(got.Stages) != 3 || got.Stages[1].Name != "approve2" {
		t.Fatalf("a second approve beside a legacy Approve = %v, %v", err, stageNames(got))
	}
}

// THE STORED IDENTITY IS BYTE-EXACT and the runner's guards are too: a phase
// begun under the stage's own bytes still stops the edit, and StageIndex is not
// folded.
func TestEditKeepsTheRunnersExactStageContract(t *testing.T) {
	if got := factory.StageIndex(namedStages(nil, "Tidy").Stages, "tidy"); got != -1 {
		t.Fatalf("StageIndex folded case: %d", got)
	}
	it := namedStages(nil, "plan", "Tidy")
	it.Stream = &factory.Stream{Phases: []factory.Phase{{Name: "Tidy", State: factory.PhaseDone}}}
	for kind, e := range map[string]factory.RunEdit{
		"ask":      {Ask: map[string]string{"tidy": "new"}},
		"thinking": {Thinking: map[string]string{"TIDY": "strong"}},
		"skip":     {Skip: []string{"tidy"}},
	} {
		e.By = factory.ByYou
		if _, _, err := factory.Edit(it, e, factory.DefaultRecipe(), factory.EditBeforeRun); err == nil || !strings.Contains(err.Error(), "already run") {
			t.Errorf("%s of a begun legacy stage = %v", kind, err)
		}
	}
	// A stage that has not begun stays editable while another has.
	if _, _, err := factory.Edit(it, factory.RunEdit{By: factory.ByYou, Thinking: map[string]string{"plan": "strong"}},
		factory.DefaultRecipe(), factory.EditBeforeRun); err != nil {
		t.Fatalf("an unbegun stage beside a begun one = %v", err)
	}
}

// A PLACEMENT NAMES A STAGE THE WAY EVERY OTHER PART OF AN EDIT DOES, and the
// stage lands where the person pointed, however either side spells the name:
// explicit After and the ask's own `after X` / `before X`. The exact lookup
// inside PlaceStage used to miss a legacy `Tidy` for a lowercased anchor and
// silently drop the stage at the default place instead.
func TestEditPlacementResolvesALegacyCasedAnchor(t *testing.T) {
	for name, tc := range map[string]struct {
		add  factory.Added
		want []string
		line string
	}{
		"explicit after":        {factory.Added{Stage: factory.Stage{Name: "inspect", Ask: "read only"}, After: "tidy"}, []string{"Tidy", "inspect", "review", "proof"}, "added inspect after Tidy"},
		"explicit spaced":       {factory.Added{Stage: factory.Stage{Name: "inspect", Ask: "read only"}, After: " TIDY "}, []string{"Tidy", "inspect", "review", "proof"}, "added inspect after Tidy"},
		"textual after":         {factory.Added{Stage: factory.Stage{Name: "inspect", Ask: "after tidy, read only"}}, []string{"Tidy", "inspect", "review", "proof"}, "added inspect"},
		"textual before":        {factory.Added{Stage: factory.Stage{Name: "inspect", Ask: "before REVIEW: read only"}}, []string{"Tidy", "inspect", "review", "proof"}, "added inspect"},
		"textual before legacy": {factory.Added{Stage: factory.Stage{Name: "inspect", Ask: "before tidy, read only"}}, []string{"inspect", "Tidy", "review", "proof"}, "added inspect"},
	} {
		it := namedStages(nil, "Tidy", "review", "proof")
		it.Stages[2].Until = factory.UntilProven
		got, lines, err := factory.Edit(it, factory.RunEdit{By: factory.ByYou, Add: []factory.Added{tc.add}}, factory.DefaultRecipe(), factory.EditBeforeRun)
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if !reflect.DeepEqual(stageNames(got), tc.want) {
			t.Errorf("%s: order = %v, want %v", name, stageNames(got), tc.want)
		}
		if len(lines) == 0 || !strings.HasSuffix(lines[0], tc.line) {
			t.Errorf("%s: receipt = %v, want it to end %q", name, lines, tc.line)
		}
		if got.Stages[0].Name == "tidy" {
			t.Errorf("%s: the stored name was renamed", name)
		}
	}
}

// A textual anchor two stages answer to refuses the whole edit, and an anchor
// no stage answers to keeps PlaceStage's meaning: just before proof.
func TestEditTextualPlacementAmbiguousRefusesAndUnknownKeepsItsDefault(t *testing.T) {
	dup := namedStages(nil, "Tidy", "tidy", "proof")
	dup.Stages[2].Until = factory.UntilProven
	for _, ask := range []string{"after tidy, read only", "before Tidy, read only"} {
		got, _, err := factory.Edit(dup, factory.RunEdit{By: factory.ByYou, Add: []factory.Added{{Stage: factory.Stage{Name: "inspect", Ask: ask}}}}, factory.DefaultRecipe(), factory.EditBeforeRun)
		if err == nil || !strings.Contains(err.Error(), "more than one stage is named") {
			t.Errorf("%q: err = %v", ask, err)
		}
		if !reflect.DeepEqual(stageNames(got), []string{"Tidy", "tidy", "proof"}) {
			t.Errorf("%q: a refused edit moved the stages: %v", ask, stageNames(got))
		}
	}
	it := namedStages(nil, "Tidy", "proof")
	it.Stages[1].Until = factory.UntilProven
	got, _, err := factory.Edit(it, factory.RunEdit{By: factory.ByYou, Add: []factory.Added{{Stage: factory.Stage{Name: "inspect", Ask: "after nothing, read only"}}}}, factory.DefaultRecipe(), factory.EditBeforeRun)
	if err != nil || !reflect.DeepEqual(stageNames(got), []string{"Tidy", "inspect", "proof"}) {
		t.Fatalf("unknown anchor: %v %v", stageNames(got), err)
	}
}

// A STAGE THE RECIPE FIXES STAYS FIXED UNDER ANY SPELLING the edit can now
// reach: the recipe says `review`, the item holds `Review` (or the reverse), and
// ask, thinking and skip all refuse with the one fixed sentence. Switching on
// is still allowed, and a stage the recipe does not fix is still editable.
func TestEditFixedRecipeStageBindsAnyCasedItemStage(t *testing.T) {
	for _, spelling := range [][2]string{{"review", "Review"}, {"Review", "review"}, {" review", "REVIEW"}} {
		r := factory.DefaultRecipe()
		for i := range r.Stages {
			if r.Stages[i].Name == "review" {
				r.Stages[i].Name, r.Stages[i].Fixed = spelling[0], true
			}
		}
		for kind, e := range map[string]factory.RunEdit{
			"ask":      {Ask: map[string]string{"review": "changed"}},
			"thinking": {Thinking: map[string]string{"review": "strong"}},
			"skip":     {Skip: []string{"review"}},
		} {
			it := namedStages(nil, spelling[1], "other")
			e.By = factory.ByYou
			got, _, err := factory.Edit(it, e, r, factory.EditBeforeRun)
			if err == nil || !strings.Contains(err.Error(), "is fixed by the recipe") {
				t.Errorf("%v %s = %v", spelling, kind, err)
			}
			if !reflect.DeepEqual(got, it) {
				t.Errorf("%v %s: a refused edit moved the item", spelling, kind)
			}
		}
		it := namedStages(nil, spelling[1], "other")
		it.Stages[0].On = false
		if _, _, err := factory.Edit(it, factory.RunEdit{By: factory.ByYou, On: []string{"review"}}, r, factory.EditBeforeRun); err != nil {
			t.Errorf("%v: switching a fixed stage on = %v", spelling, err)
		}
		if _, _, err := factory.Edit(namedStages(nil, spelling[1], "other"), factory.RunEdit{By: factory.ByYou, Thinking: map[string]string{"other": "strong"}}, r, factory.EditBeforeRun); err != nil {
			t.Errorf("%v: an unfixed stage = %v", spelling, err)
		}
	}
}

// StageFixed itself: exact stays fixed, a fold or trim match is fixed too, a
// different name is not, and no stage in the recipe means not fixed.
func TestStageFixedReadsTheRecipeConservatively(t *testing.T) {
	r := factory.DefaultRecipe()
	for i := range r.Stages {
		if r.Stages[i].Name == "review" {
			r.Stages[i].Fixed = true
		}
	}
	for name, want := range map[string]bool{"review": true, "Review": true, " REVIEW ": true, "reviewer": false, "Tidy": false} {
		st := []factory.Stage{{Name: name}}
		if got := factory.StageFixed(st, 0, r, factory.KindIssue); got != want {
			t.Errorf("%q fixed = %v, want %v", name, got, want)
		}
	}
}

// askRecipe is the default recipe with issues under `ask`, where a change by
// the manager or plan waits for the person at an approve step after plan.
func askRecipe() factory.Recipe {
	r := factory.DefaultRecipe()
	r.Adapt = map[factory.Kind]factory.AdaptMode{factory.KindIssue: factory.AdaptAsk}
	return r
}

// legacyAskItem is a job an older build saved with every stage in capitals,
// its Plan done and the rest still to come.
func legacyAskItem(names ...string) factory.Item {
	it := namedStages(nil, names...)
	it.Stages[len(it.Stages)-1].Until = factory.UntilProven
	it.Stream = &factory.Stream{Phases: []factory.Phase{{Name: names[0], State: factory.PhaseDone}, {Name: names[1], State: factory.PhasePending}}}
	return it
}

// UNDER ASK A LEGACY `Plan` IS STILL WHERE THE PERSON RATIFIES. Once an edit
// could reach a legacy `Write` without case, the approve step's own lookup of
// plan had to fold too: an exact lookup missed `Plan` and the manager's or
// plan's change landed with nobody asked. Nothing is renamed, and the runner's
// exact StageIndex still does not answer `plan` for `Plan`.
func TestAdaptUnderAskRatifiesAfterALegacyCasedPlan(t *testing.T) {
	for name, e := range map[string]factory.RunEdit{
		"manager thinking": {By: factory.ByManager, Thinking: map[string]string{"write": "strong"}},
		"manager skip":     {By: factory.ByManager, Skip: []string{"TIDY"}},
		"plan add after":   factory.PlanEdit{Add: []factory.Stage{{Name: "inspect", Ask: "after write, read only"}}}.RunEdit(),
	} {
		it := legacyAskItem("Plan", "Write", "Tidy", "Proof")
		got, lines, err := factory.Edit(it, e, askRecipe(), factory.EditInRun)
		if err != nil || len(lines) == 0 {
			t.Fatalf("%s: lines = %v, err = %v", name, lines, err)
		}
		if approve := got.Stages[1]; approve.Name != factory.ApproveName || approve.Kind != factory.StageGate || !approve.On {
			t.Fatalf("%s: no approve step right after Plan: %v", name, stageNames(got))
		}
		for _, stored := range []string{"Plan", "Write", "Tidy", "Proof"} {
			if factory.StageIndex(got.Stages, stored) < 0 {
				t.Fatalf("%s: %s was renamed: %v", name, stored, stageNames(got))
			}
		}
		if factory.StageIndex(got.Stages, "plan") != -1 {
			t.Fatalf("%s: StageIndex folded case", name)
		}
	}
}

// TWO STAGES THAT ANSWER TO PLAN REFUSE THE WHOLE EDIT UNDER ASK: putting the
// approve step after one of them would be a guess, and leaving it out would be
// no ratification at all. The edit's other, valid parts are not applied.
func TestAdaptUnderAskRefusesAnAmbiguousPlanWholly(t *testing.T) {
	for name, e := range map[string]factory.RunEdit{
		"manager thinking": {By: factory.ByManager, Thinking: map[string]string{"write": "strong"}, Skip: []string{"tidy"}},
		"plan add after":   factory.PlanEdit{Add: []factory.Stage{{Name: "inspect", Ask: "after write, read only"}}, Why: "risky"}.RunEdit(),
	} {
		it := legacyAskItem("Plan", "plan", "Write", "Tidy", "Proof")
		before := factory.CopyStages(it.Stages)
		got, lines, err := factory.Edit(it, e, askRecipe(), factory.EditInRun)
		if err == nil || !strings.Contains(err.Error(), "more than one stage is named plan") {
			t.Fatalf("%s: err = %v", name, err)
		}
		if lines != nil || !reflect.DeepEqual(got, it) || !reflect.DeepEqual(it.Stages, before) || len(got.Adapted) != 0 {
			t.Fatalf("%s: a refused edit changed the item: %v", name, stageNames(got))
		}
	}
}
