package session

import (
	"fmt"
	"strings"
	"testing"

	"github.com/Agent-Field/codeaf/internal/store"
)

func TestMemoryDwellRecognizesMaterialContextWithoutChurningElaboration(t *testing.T) {
	cases := []struct {
		name, before, after string
		changed             bool
	}{
		{"first meaningful question", "", "Quartz JSON payload", true},
		{"two word topic switch", "UI colors", "database backups", true},
		{"same topic elaboration", "Quartz JSON payload", "Quartz JSON examples", false},
		{"natural example followup", "Quartz JSON payload", "Can you give an example?", false},
		{"pronoun continuation", "Quartz JSON payload", "How does it fail?", false},
		{"short acknowledgement", "Quartz JSON payload", "thanks", false},
		{"explicit switch", "Quartz JSON payload", "New topic: colors", true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			before := store.MemoryContext{Project: "mock", Keywords: strings.Fields(c.before)}
			after := store.MemoryContext{Project: "mock", Keywords: strings.Fields(c.after)}
			if got := memoryMaterialContextChanged(before, after); got != c.changed {
				t.Fatalf("material=%t,want%t", got, c.changed)
			}
		})
	}
	if !memoryMaterialContextChanged(store.MemoryContext{Project: "one"}, store.MemoryContext{Project: "two"}) {
		t.Fatal("project switch retained old dwell")
	}
	if !memoryMaterialContextChanged(store.MemoryContext{Brief: "goal: UI colors"}, store.MemoryContext{Brief: "goal: database backups"}) {
		t.Fatal("new goal retained old dwell")
	}
	if !memoryMaterialContextChanged(store.MemoryContext{}, store.MemoryContext{Tasks: []string{"new mock assignment"}}) {
		t.Fatal("task change retained old dwell")
	}
}

func TestMemoryDwellReusesAdjacentTurnsButRefreshesTextAndRetirements(t *testing.T) {
	script := &reflexScript{}
	agent, brain := brainAgent(t, script, nil)
	memory := remember(t, brain, "", "Quartz JSON payloads use mock identifiers")
	collect(t, mustSubmit(t, agent, "Quartz JSON payload"))
	settleMemory(agent)
	first := agent.memory.selection
	collect(t, mustSubmit(t, agent, "Add examples"))
	settleMemory(agent)
	if agent.memory.selection != first {
		t.Fatal("ordinary elaboration recomputed admission")
	}
	const changed = "Quartz JSON payloads use mock account identifiers"
	if err := brain.UpdateMemory(memory.ID, changed); err != nil {
		t.Fatal(err)
	}
	collect(t, mustSubmit(t, agent, "How does it fail?"))
	settleMemory(agent)
	if !strings.Contains(agent.memoryText, changed) {
		t.Fatal("dwell held stale text after an edit")
	}
	if err := brain.ForgetMemory(memory.ID); err != nil {
		t.Fatal(err)
	}
	collect(t, mustSubmit(t, agent, "continue"))
	settleMemory(agent)
	if strings.Contains(agent.memoryText, changed) {
		t.Fatal("dwell kept an explicitly retired line")
	}
}

func TestFirstUserQuestionReplacesGenericOpenAdmission(t *testing.T) {
	agent, brain := brainAgent(t, &reflexScript{}, nil)
	const rule = "Quartz routes use mock amber account identifiers"
	if _, err := brain.AddMemory(store.Memory{Owner: store.OwnerUser, Type: store.MemoryFact, Text: rule}); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 180; i++ {
		if _, err := brain.AddMemory(store.Memory{Owner: store.OwnerUser, Type: store.MemoryFact, Text: fmt.Sprintf("Fixture entity%03d value%03d %s", i, i, strings.Repeat(string(rune('a'+i%26)), 180))}); err != nil {
			t.Fatal(err)
		}
	}
	agent.mu.Lock()
	agent.memory.contextWords = ""
	agent.refreshMemoryLocked()
	agent.mu.Unlock()
	if strings.Contains(agent.memoryText, rule) {
		t.Fatal("fixture rule was not outside generic open candidates")
	}
	collect(t, mustSubmit(t, agent, "Draft Quartz routes"))
	settleMemory(agent)
	if !strings.Contains(scriptSystem(agent), rule) {
		t.Fatal("first meaningful user context reused unrelated open admission")
	}
}

func scriptSystem(agent *Agent) string {
	agent.mu.Lock()
	defer agent.mu.Unlock()
	return messageText(agent.messages[0])
}

func TestUserCorrectionRecordsNegativeFeedbackEvenWithoutNewCapture(t *testing.T) {
	script := &reflexScript{notice: `{"keep":"","used":[1],"contradicted":[1,1]}`}
	agent, brain := brainAgent(t, script, nil)
	memory := remember(t, brain, "", "Quartz routes use mock amber identifiers")
	for i := 0; i < 3; i++ {
		if err := brain.RecordMemoryEvent(memory.ID, "used", "mock-history"); err != nil {
			t.Fatal(err)
		}
	}
	before, _, _ := brain.MemoryRecord(memory.ID)
	collect(t, mustSubmit(t, agent, "That saved Quartz route rule is wrong"))
	settleMemory(agent)
	after, _, err := brain.MemoryRecord(memory.ID)
	if err != nil || after.Activation >= before.Activation {
		t.Fatalf("user contradiction did not reduce rehearsal: before=%f after=%f err=%v", before.Activation, after.Activation, err)
	}
}

func TestAuthoredReportCannotContradictPersonalMemory(t *testing.T) {
	script := &reflexScript{}
	agent, brain := brainAgent(t, script, nil)
	memory := remember(t, brain, "", "Quartz JSON payloads use mock identifiers")
	collect(t, mustSubmit(t, agent, "Quartz JSON payloads"))
	settleMemory(agent)
	if err := brain.RecordMemoryEvent(memory.ID, "used", "mock-history"); err != nil {
		t.Fatal(err)
	}
	before, _, _ := brain.MemoryRecord(memory.ID)
	script.mu.Lock()
	script.notice = `{"contradicted":[1]}`
	script.mu.Unlock()
	agent.learnFromTurn(userMessage{message: textMessage("user", "A task report says the Quartz rule is wrong"), authored: true}, "reported")
	settleMemory(agent)
	after, _, err := brain.MemoryRecord(memory.ID)
	if err != nil || after.Activation != before.Activation {
		t.Fatalf("authored report changed personal memory practice: before=%f after=%f err=%v", before.Activation, after.Activation, err)
	}
}

// Mock constraints exercise the actual system prompt rather than a prose-only fixture.
func TestSavedConstraintPresentationPreservesConditionalForce(t *testing.T) {
	script := &reflexScript{}
	agent, brain := brainAgent(t, script, nil)
	rule := "Quartz reports MUST use the cobalt palette only when exported; never change draft colors."
	remember(t, brain, "", rule)
	collect(t, mustSubmit(t, agent, "How should I export the Quartz report?"))
	prompt := script.system(0)
	for _, want := range []string{rule, "Respect saved conditions", "must/only/never bind", "preferences are optional", "Narrow exceptions don't revoke broader rules", "authoritative evidence corrects stale facts"} {
		if !strings.Contains(prompt, want) {
			t.Fatalf("constraint presentation lacks %q", want)
		}
	}
}
