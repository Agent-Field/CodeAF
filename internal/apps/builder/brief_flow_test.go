package builder

import (
	"strings"
	"testing"

	"github.com/Agent-Field/codeaf/internal/apps/bridge"
)

// The PLAN reply opens with a design brief that plan.json has no key for: it
// is stripped before plan.json is stored, shown to the owner as a plan step,
// and carried into every WRITE call.
func TestTheBriefReachesEveryWriteButNotPlanJSON(t *testing.T) {
	r := newRig(t)
	r.model.plan = func(int) string {
		return `{"brief":{"users":"a sales lead","daily_view":"Pipeline: which deals need me today?"},` + strings.TrimPrefix(strings.TrimSpace(goldenPlan()), "{")
	}
	r.create(t, true)
	r.waitState(t, bridge.StatePreview)

	_, files := r.store.current()
	if strings.Contains(string(files["plan.json"]), "brief") {
		t.Fatal("plan.json carries the brief; the plan schema has no key for it")
	}
	writes := 0
	r.model.mu.Lock()
	for _, p := range r.model.prompts {
		if callKind(p) != "write" {
			continue
		}
		writes++
		if !strings.Contains(p, "Daily view: Pipeline: which deals need me today?") {
			t.Errorf("a WRITE prompt lacks the brief")
		}
	}
	r.model.mu.Unlock()
	if writes == 0 {
		t.Fatal("no WRITE calls")
	}
	found := false
	for _, ev := range r.hub.events() {
		found = found || strings.HasPrefix(ev.Log, "Design brief · Who uses it: a sales lead")
	}
	if !found {
		t.Error("the owner never sees the brief")
	}
}
