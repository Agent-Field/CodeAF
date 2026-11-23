package factory_test

import (
	"reflect"
	"testing"

	"github.com/Agent-Field/codeaf/internal/factory"
)

// A PERSON PICKS THE MODEL ONE STEP OF ONE JOB RUNS ON, before it runs. The
// pick is kept on the step and said as a line; "" gives the step back to the
// job's model. A step that has run, a check or an approval (which run no
// model), a fixed step, and a pick by anyone but the person are refused, and a
// refusal changes nothing.
func TestAPersonPicksTheModelOfAStepThatHasNotRun(t *testing.T) {
	it := adaptItem()
	it.Stream.Phases = []factory.Phase{{Name: "plan", State: factory.PhaseDone}, {Name: "write", State: factory.PhaseRunning}}
	got, lines, err := factory.Edit(it, factory.RunEdit{Model: map[string]string{"review": " anthropic/claude-opus-5.5 "}, By: factory.ByYou}, factory.DefaultRecipe(), factory.EditInRun)
	if err != nil || !reflect.DeepEqual(lines, []string{"you set review model anthropic/claude-opus-5.5"}) || got.Stages[3].Model != "anthropic/claude-opus-5.5" || got.Stages[3].By != factory.ByYou {
		t.Fatalf("pick = %q %v %+v", lines, err, got.Stages[3])
	}
	back, lines, err := factory.Edit(got, factory.RunEdit{Model: map[string]string{"review": ""}, By: factory.ByYou}, factory.DefaultRecipe(), factory.EditInRun)
	if err != nil || !reflect.DeepEqual(lines, []string{"you set review model usual"}) || back.Stages[3].Model != "" {
		t.Fatalf("back to the job's = %q %v %+v", lines, err, back.Stages[3])
	}
	if _, lines, err := factory.Edit(got, factory.RunEdit{Model: map[string]string{"review": "anthropic/claude-opus-5.5"}, By: factory.ByYou}, factory.DefaultRecipe(), factory.EditInRun); err != nil || len(lines) != 0 {
		t.Fatalf("the same pick again = %q %v", lines, err)
	}
	for _, c := range []struct {
		e    factory.RunEdit
		want string
	}{
		{factory.RunEdit{Model: map[string]string{"plan": "m/x"}, By: factory.ByYou}, "you may not change plan, which has already run"},
		{factory.RunEdit{Model: map[string]string{"test": "m/x"}, By: factory.ByYou}, "test is a check or an approval and runs no model"},
		{factory.RunEdit{Model: map[string]string{"sign": "m/x"}, By: factory.ByYou}, "sign is a check or an approval and runs no model"},
		{factory.RunEdit{Model: map[string]string{"review": "m/x"}, By: factory.ByManager}, "only the person picks the model a step runs on"},
		{factory.RunEdit{Model: map[string]string{"review": "two words"}, By: factory.ByYou}, "a model is a model id of at most 200 characters, with no spaces"},
	} {
		out, lines, err := factory.Edit(it, c.e, factory.DefaultRecipe(), factory.EditInRun)
		if err == nil || err.Error() != c.want || lines != nil || !reflect.DeepEqual(out, it) {
			t.Fatalf("%+v = %v, want %q", c.e, err, c.want)
		}
	}
}
