package remote

import (
	"testing"
)

type spendRailAgent struct {
	*fakeAgent
	rail float64
}

func (a *spendRailAgent) SetSpendRail(usd float64) error {
	a.rail = usd
	return nil
}

func (a *spendRailAgent) SpendRail() float64 { return a.rail }

// The normal engine road sends the live limit through its real protocol before
// the surface can report that the conversation now holds it.
func TestOpenEngineRoadBindsConversationSpendRail(t *testing.T) {
	far := &spendRailAgent{fakeAgent: &fakeAgent{model: "m"}}
	loop, err := Loopback(Hello{Version: Version}, Options{Boot: func(Hello) (*Engine, error) {
		return &Engine{Agent: far, Workspace: "/srv/app", SessionFile: "/srv/app/j.jsonl"}, nil
	}})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = loop.Close() })
	if err := loop.Client.Agent().SetSpendRail(1.5); err != nil {
		t.Fatal(err)
	}
	if far.rail != 1.5 {
		t.Fatalf("engine holds $%.2f, want $1.50", far.rail)
	}
	value, err := loop.Client.Agent().SpendRail()
	if err != nil || value != 1.5 {
		t.Fatalf("live read %v: %v", value, err)
	}
	if err := loop.Client.Agent().SetSpendRail(0); err != nil {
		t.Fatal(err)
	}
	value, err = loop.Client.Agent().SpendRail()
	if err != nil || value != 0 {
		t.Fatalf("unlimited read %v: %v", value, err)
	}
}
