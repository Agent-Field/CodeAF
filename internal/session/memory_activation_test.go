package session

import (
	"context"
	"strings"
	"testing"

	"github.com/Agent-Field/codeaf/internal/store"
)

func TestEphemeralNoticeNeverReplacesPinnedMemory(t *testing.T) {
	script := &reflexScript{notice: `{"keep":"temporary override","scope":"user","durability":"ephemeral","replaces":1}`}
	agent, brain := brainAgent(t, script, nil)
	if _, err := agent.RememberAlways("Use mock customer names in examples", true); err != nil {
		t.Fatal(err)
	}
	collect(t, mustSubmit(t, agent, "just this once use a temporary override"))
	settleMemory(agent)
	got := activeTexts(t, brain, store.OwnerUser)
	if len(got) != 1 || got[0] != "Use mock customer names in examples" {
		t.Fatalf("ephemeral replaced pin: %v", got)
	}
}

func TestMemorySearchRespectsProjectOwners(t *testing.T) {
	agent, brain := brainAgent(t, &reflexScript{}, func(c *Config) { c.MemoryProjectKey = "mock-alpha" })
	for _, m := range []store.Memory{
		{Owner: store.OwnerUser, Text: "Quartz examples use mock customers", Type: store.MemoryFact},
		{Owner: store.OwnerProject("mock-alpha"), Text: "Quartz endpoint accepts JSON", Type: store.MemoryFact},
		{Owner: store.OwnerProject("mock-private"), Text: "Quartz private endpoint", Type: store.MemoryFact},
	} {
		if _, err := brain.AddMemory(m); err != nil {
			t.Fatal(err)
		}
	}
	text, failed, err := agent.memorySearchTool().Execute(context.Background(), []byte(`{"query":"Quartz"}`))
	if err != nil || failed {
		t.Fatalf("search: %s %v", text, err)
	}
	if !strings.Contains(text, "mock customers") || !strings.Contains(text, "accepts JSON") || strings.Contains(text, "private endpoint") {
		t.Fatalf("scope leak or missing result: %s", text)
	}
}

func TestMemorySelectionRendererPreservesPinsPastBudget(t *testing.T) {
	pins := []store.Memory{{Owner: store.OwnerUser, Origin: store.OriginPerson, Text: strings.Repeat("a", store.MemoryBlockRunes)}, {Owner: store.OwnerUser, Origin: store.OriginPerson, Text: "final explicit pin"}}
	text := renderMemorySelection(pins, 7, "")
	if !strings.Contains(text, "final explicit pin") || !strings.Contains(text, "· 7 more in memory") {
		t.Fatal("renderer silently truncated admitted pins or omitted count")
	}
	if text := renderMemorySelection(nil, 3, ""); !strings.Contains(text, "3 more in memory") {
		t.Fatal("all archived working set became invisible")
	}
}

func TestNoticedUseReinforcesOnlyAttributedWorkingSetLine(t *testing.T) {
	script := &reflexScript{notice: `{"keep":"","used":[1]}`}
	agent, brain := brainAgent(t, script, nil)
	one := remember(t, brain, "", "Quartz service accepts JSON payloads")
	two := remember(t, brain, "", "Amber project uses mock customer names")
	collect(t, mustSubmit(t, agent, "Quartz"))
	settleMemory(agent)
	selection, err := agent.MemorySelection()
	if err != nil || len(selection.Memories) != 2 {
		t.Fatalf("selection: %+v %v", selection, err)
	}
	engaged := selection.Memories[0].ID
	for _, memory := range []store.Memory{one, two} {
		got, _, err := brain.MemoryRecord(memory.ID)
		if err != nil {
			t.Fatal(err)
		}
		expected := 0
		if memory.ID == engaged {
			expected = 1
		}
		if got.Rehearsals != expected {
			t.Fatalf("%s rehearsals=%d, want %d", memory.Text, got.Rehearsals, expected)
		}
	}
}

func TestNoticedProjectStateRetainsItsCaptureType(t *testing.T) {
	script := &reflexScript{notice: `{"keep":"Mock migration is awaiting a database upgrade","scope":"project","durability":"project_state"}`}
	agent, brain := brainAgent(t, script, func(c *Config) { c.MemoryProjectKey = "mock-alpha" })
	collect(t, mustSubmit(t, agent, "The mock migration must wait for the database upgrade"))
	settleMemory(agent)
	got, err := brain.MemoriesFor(MemoryOwners("mock-alpha"))
	if err != nil || len(got) != 1 || got[0].Type != store.MemoryProjectState {
		t.Fatalf("capture type lost: %+v %v", got, err)
	}
}

func TestMemorySelectionInspectionDoesNotChangeActualAdmission(t *testing.T) {
	agent, brain := brainAgent(t, &reflexScript{}, nil)
	memory := remember(t, brain, "", "Quartz examples use mock names")
	collect(t, mustSubmit(t, agent, "Quartz"))
	settleMemory(agent)
	first, err := agent.MemorySelection()
	if err != nil {
		t.Fatal(err)
	}
	before, _, err := brain.MemoryRecord(memory.ID)
	if err != nil {
		t.Fatal(err)
	}
	delete(first.Decisions, memory.ID)
	first.Memories[0].Text = "mutated inspection result"
	second, err := agent.MemorySelection()
	if err != nil || second.Memories[0].Text != memory.Text || !second.Decisions[memory.ID].Riding {
		t.Fatalf("inspection mutated admission: %+v %v", second, err)
	}
	after, _, err := brain.MemoryRecord(memory.ID)
	if err != nil || before.Rehearsals != after.Rehearsals || before.Activation != after.Activation {
		t.Fatalf("inspection rehearsed memory: before=%+v after=%+v err=%v", before, after, err)
	}
}

func TestOrdinaryConversationAutomaticallyInformsFreshChat(t *testing.T) {
	script := &reflexScript{notice: `{"keep":"Quartz examples use mock customer names","scope":"project","durability":"standing_rule"}`}
	first, brain := brainAgent(t, script, func(c *Config) { c.MemoryProjectKey = "mock-quartz" })
	collect(t, mustSubmit(t, first, "Our Quartz examples always use mock customer names"))
	settleMemory(first)
	secondScript := &reflexScript{}
	second, _ := newTestAgent(t, secondScript, func(c *Config) {
		c.Memory = brain
		c.MemoryProjectKey = "mock-quartz"
		c.ProfileDir = t.TempDir()
	})
	collect(t, mustSubmit(t, second, "Draft a Quartz example"))
	if !strings.Contains(secondScript.system(0), "Quartz examples use mock customer names") {
		t.Fatal("ordinary automatic capture did not anticipate fresh chat context")
	}
}

func TestAutomaticCorrectionPreservesExplicitPin(t *testing.T) {
	script := &reflexScript{notice: `{"keep":"Quartz deploys on Tuesdays","scope":"user","durability":"standing_rule","replaces":1}`}
	agent, brain := brainAgent(t, script, nil)
	if _, err := agent.RememberAlways("Quartz deploys on Fridays", true); err != nil {
		t.Fatal(err)
	}
	collect(t, mustSubmit(t, agent, "Quartz deploys on Tuesdays now, not Fridays"))
	settleMemory(agent)
	kept, err := brain.MemoriesFor(MemoryOwners(""))
	if err != nil || len(kept) != 1 || kept[0].Text != "Quartz deploys on Tuesdays" || !kept[0].Rule || kept[0].Origin != store.OriginNoticed {
		t.Fatalf("correction lost pin or provenance: %+v %v", kept, err)
	}
}
