package factory

import (
	"reflect"
	"strings"
	"testing"
)

// A HOUSE RULE IS ONE LINE: trimmed, said once whatever its case, and never a
// paragraph, a line break or an overlong list, because every step's brief
// quotes it.
func TestHouseRulesAreCleanedAndBounded(t *testing.T) {
	got, err := HouseRules{
		Rules:    []string{"  don't touch auth without checking with me ", "Don't touch auth without checking with me", ""},
		AskFirst: nil,
		Never:    []string{"infra/"},
	}.Clean()
	if err != nil {
		t.Fatal(err)
	}
	want := HouseRules{Rules: []string{"don't touch auth without checking with me"}, AskFirst: []string{}, Never: []string{"infra/"}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("cleaned to %#v, want %#v", got, want)
	}
	if !(HouseRules{}).Empty() || got.Empty() {
		t.Fatal("Empty misread the lists")
	}
	for name, bad := range map[string]HouseRules{
		"long line":  {Rules: []string{strings.Repeat("x", HouseRuleLongest+1)}},
		"line break": {AskFirst: []string{"touch auth/\nand everything else"}},
		"too many":   {Never: make([]string, 0)},
	} {
		if name == "too many" {
			for i := 0; i <= HouseRulesMost; i++ {
				bad.Never = append(bad.Never, "path"+strings.Repeat("x", i))
			}
		}
		if _, err := bad.Clean(); err == nil {
			t.Errorf("%s was accepted", name)
		}
	}
}

// THE FACTORY'S ADAPT WORD TIGHTENS A RUN'S EDIT ON ANY MACHINE: a job stamped
// with fixed refuses the plan's reshaping though its repository allows it, and
// a person's own edit is never bound by it.
func TestTheJobsHouseAdaptBindsThePlanButNotThePerson(t *testing.T) {
	it := Item{Kind: KindIssue, State: StateRunning, Stages: DefaultRecipe().Stages, House: &HouseRules{Adapt: AdaptFixed}}
	edit := RunEdit{By: ByPlan, Skip: []string{"review"}}
	if _, _, err := Edit(it, edit, DefaultRecipe(), EditInRun); err == nil {
		t.Fatal("the plan reshaped a job the factory fixed")
	}
	edit.By = ByYou
	if _, _, err := Edit(it, edit, DefaultRecipe(), EditInRun); err != nil {
		t.Fatalf("the person's own edit was refused: %v", err)
	}
	if _, ok := StricterAdapt(AdaptAsk, AdaptFree); ok {
		t.Fatal("ask was loosened to adapt")
	}
	if _, err := (HouseRules{Adapt: "sometimes"}).Clean(); err == nil {
		t.Fatal("a bad adapt word was kept")
	}
}
