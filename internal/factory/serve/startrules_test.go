package serve_test

import (
	"reflect"
	"testing"

	"github.com/Agent-Field/codeaf/internal/factory"
)

// A PERSON'S START STAMPS THE RULES THE JOB STARTS UNDER: the repository's
// ask-first and never lines (here the defaults, which name none) go on the job
// so its plan can be checked against them wherever it runs, and the factory's
// own rules ride along when it is queued.
func TestAStartStampsTheRulesTheJobStartsUnder(t *testing.T) {
	f := newV2Jobs(t)
	if _, err := f.st.ChangeHouseRules(func(h *factory.HouseRules) error {
		h.Rules = []string{"no new dependencies without asking"}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	it := f.create(t, factory.Item{State: factory.StateNew, Assignee: "sam", RuleHold: "left over"})
	f.post(t, it, "run", "", 200)
	cur, err := f.st.Get(it.ID)
	if err != nil {
		t.Fatal(err)
	}
	if cur.StartRules == nil || cur.StartRules.AskFirst == nil || cur.StartRules.Never == nil {
		t.Fatalf("start rules %#v", cur.StartRules)
	}
	if cur.RuleHold != "" {
		t.Fatalf("an earlier hold survived the start: %q", cur.RuleHold)
	}
	if cur.House == nil || !reflect.DeepEqual(cur.House.Rules, []string{"no new dependencies without asking"}) {
		t.Fatalf("house rules %#v", cur.House)
	}
}
