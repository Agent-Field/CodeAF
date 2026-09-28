package tui3

import (
	"errors"
	"strings"
	"testing"
)

type budgetLiveAgent struct {
	*fakeAgent
	rail float64
	err  error
}

func (a *budgetLiveAgent) SetSpendRail(usd float64) error {
	if a.err != nil {
		return a.err
	}
	a.rail = usd
	return nil
}

// The command that reports a conversation limit must bind the conversation
// already behind this window, including the value used by its next run.
func TestBudgetConversationBindsTheOpenChatBeforeItsReceipt(t *testing.T) {
	a, _ := sheetApp(t)
	engine := &budgetLiveAgent{fakeAgent: &fakeAgent{model: "openai/gpt-4.1-mini"}}
	a.agent = engine
	before := len(a.entries)
	cmd := a.budget("conversation 1.5")
	if cmd == nil {
		t.Fatal("the open conversation did not receive a limit command")
	}
	if engine.rail != 0 {
		t.Fatal("the engine call blocked the update loop")
	}
	if a.spendRail != 0 || !a.railRead {
		t.Fatalf("the active status reading changed before the bind: %v, read=%v", a.spendRail, a.railRead)
	}
	if len(a.entries) != before {
		t.Fatal("/budget confirmed the changed limit before the engine answered")
	}
	msg, ok := cmd().(doorMsg)
	if !ok {
		t.Fatal("the bind did not return through the door")
	}
	a.doorSaid(msg)
	if engine.rail != 1.5 {
		t.Fatalf("/budget showed a new limit but the open chat still has %v", engine.rail)
	}
	if a.spendRail != 1.5 {
		t.Fatalf("the active status reading did not follow the bound limit: %v", a.spendRail)
	}
}

// A failed engine bind leaves the active status reading alone and says when
// the saved profile limit will apply.
func TestBudgetConversationFailedBindSaysNextConversation(t *testing.T) {
	a, _ := sheetApp(t)
	a.agent = &budgetLiveAgent{
		fakeAgent: &fakeAgent{model: "openai/gpt-4.1-mini"},
		err:       errors.New("engine unavailable"),
	}
	cmd := a.budget("conversation 1.5")
	if cmd == nil {
		t.Fatal("the changed limit had no engine bind")
	}
	a.doorSaid(cmd().(doorMsg))
	if a.spendRail != 0 {
		t.Fatalf("a refused bind changed the active status reading to %v", a.spendRail)
	}
	if len(a.entries) == 0 || !strings.Contains(a.entries[len(a.entries)-1].text, "saved for the next conversation") {
		t.Fatalf("the refusal did not say when it applies: %+v", a.entries)
	}
}
