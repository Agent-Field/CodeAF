package tui3

import (
	"strings"
	"testing"

	"github.com/Agent-Field/codeaf/internal/session"
)

func TestTypedAutomationEditOffersOnlySaveAndDontSave(t *testing.T) {
	a, _ := automationLab(t, labWork("weekly update", "0 9 * * 1"))
	runCmd(a.openAutomations())
	drive(t, a, key("right"), key("e"))
	a.input.setText(strings.Replace(a.input.String(), "0 9 * * 1", "0 10 * * 1", 1))
	drive(t, a, key("enter"))
	harnessSettled(t, a)
	if a.auto == nil || a.auto.head != typedEditHead {
		t.Fatal("editing the moment did not raise save this change?")
	}
	q := a.automationShown(a.auto)
	if len(q.question.Options) != 2 || a.questionTakesOther(q) {
		t.Fatal("typed edit offers an answer beyond Save and Don't save")
	}
	if q.question.Options[0].Key != session.AutomationSaveKey || q.question.Options[1].Key != session.AutomationNoKey {
		t.Fatal("typed edit changed its answer keys")
	}
	if text := plain(frame(a)); strings.Contains(text, "something else") {
		t.Fatal("typed edit draws something else: " + text)
	}
}
