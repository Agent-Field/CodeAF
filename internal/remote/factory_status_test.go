package remote

import (
	"context"
	"testing"
)

type statusAgent struct {
	*fakeAgent
	reads int
}

func (a *statusAgent) ReadFactoryStatus(context.Context, string) (string, bool, error) {
	a.reads++
	return "Plan is waiting for the saved choice.", true, nil
}
func TestFactoryStatusUsesOwningEngineGetterWithoutATurn(t *testing.T) {
	if classify(methodFactoryStatus) != classGetter || classify(methodFactoryStatus).road() != onItsOwn {
		t.Fatal("status may queue behind work")
	}
	far := &statusAgent{fakeAgent: &fakeAgent{model: "selected-model"}}
	loop := askLoop(t, far)
	text, handled, err := loop.Client.Agent().ReadFactoryStatus(t.Context(), "status")
	if err != nil || !handled || text != "Plan is waiting for the saved choice." || far.reads != 1 || far.Model() != "selected-model" {
		t.Fatalf("status getter failed: %q %t %v", text, handled, err)
	}
	_, handled, err = loop.Client.Agent().ReadFactoryStatus(t.Context(), "fix it")
	if err != nil || handled || far.reads != 1 {
		t.Fatal("ordinary steering was intercepted")
	}
}
