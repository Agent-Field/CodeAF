package factory_test

import (
	"reflect"
	"testing"

	"github.com/Agent-Field/codeaf/internal/factory"
)

// A PERSON SETS HOW MANY TRIES ONE CONVERSATION STEP OF ONE JOB GETS, 1 TO 3,
// before it runs; the line says it. More than three, a check or an approval
// (a check says its rounds when it is added, and an approval does not loop), a
// step that has run, a fixed step and anyone but the person are refused, and a
// refusal changes nothing.
func TestAPersonSetsAStepsTriesFromOneToThree(t *testing.T) {
	it := adaptItem()
	it.Stream.Phases = []factory.Phase{{Name: "plan", State: factory.PhaseDone}, {Name: "write", State: factory.PhaseRunning}}
	got, lines, err := factory.Edit(it, factory.RunEdit{Max: map[string]int{"review": 3}, By: factory.ByYou}, factory.DefaultRecipe(), factory.EditInRun)
	if err != nil || !reflect.DeepEqual(lines, []string{"you set review tries 3"}) || got.Stages[3].Max != 3 || got.Stages[3].By != factory.ByYou {
		t.Fatalf("set = %q %v %+v", lines, err, got.Stages[3])
	}
	if _, lines, err := factory.Edit(got, factory.RunEdit{Max: map[string]int{"review": 3}, By: factory.ByYou}, factory.DefaultRecipe(), factory.EditInRun); err != nil || len(lines) != 0 {
		t.Fatalf("the same tries again = %q %v", lines, err)
	}
	for _, c := range []struct {
		e    factory.RunEdit
		want string
	}{
		{factory.RunEdit{Max: map[string]int{"review": 4}, By: factory.ByYou}, "a step gets 1 to 3 tries"},
		{factory.RunEdit{Max: map[string]int{"review": 0}, By: factory.ByYou}, "a step gets 1 to 3 tries"},
		{factory.RunEdit{Max: map[string]int{"plan": 2}, By: factory.ByYou}, "you may not change plan, which has already run"},
		{factory.RunEdit{Max: map[string]int{"test": 2}, By: factory.ByYou}, "test is a check or an approval; only a conversation step's tries change here"},
		{factory.RunEdit{Max: map[string]int{"sign": 2}, By: factory.ByYou}, "sign is a check or an approval; only a conversation step's tries change here"},
		{factory.RunEdit{Max: map[string]int{"review": 2}, By: factory.ByPlan}, "only the person sets how many tries a step gets"},
	} {
		out, lines, err := factory.Edit(it, c.e, factory.DefaultRecipe(), factory.EditInRun)
		if err == nil || err.Error() != c.want || lines != nil || !reflect.DeepEqual(out, it) {
			t.Fatalf("%+v = %v, want %q", c.e, err, c.want)
		}
	}
}
