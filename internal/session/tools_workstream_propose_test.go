package session

import (
	"encoding/json"
	"strconv"
	"strings"
	"testing"
)

type proposingDoor struct {
	workingDoor
	can bool
}

func (d *proposingDoor) Proposes() bool { return d.can }

// THE CARDS VERB IS ABSENT WHERE NOTHING DRAWS A CARD. A door that keeps
// proposals carries ws_task_propose; a task's run (Proposes false) and a door
// that never heard of cards do not, so no model holds a verb nobody can see.
func TestOnlyADoorThatProposesCarriesTheCardsVerb(t *testing.T) {
	has := func(door WorkstreamWorkDoor) bool {
		for _, tool := range workstreamWorkBelt(door, nil) {
			if tool.Name == WSTaskProposeTool {
				return true
			}
		}
		return false
	}
	if !has(&proposingDoor{can: true}) {
		t.Error("a door that proposes lacks the verb")
	}
	if has(&proposingDoor{can: false}) || has(&workingDoor{}) {
		t.Error("a door that cannot propose carries the verb")
	}
	for _, tool := range WorkstreamWorkTools() {
		if tool.Name == WSTaskProposeTool {
			t.Error("the shared table a Personal desk pins carries the cards verb")
		}
	}
}

// THE CARDS RULE AND SCHEMA SAY THE PLAN'S NUMBERS, from the one constant.
func TestTheCardsVerbSaysThePlansBounds(t *testing.T) {
	var schema struct {
		Properties struct {
			Tasks struct {
				MaxItems int `json:"maxItems"`
			} `json:"tasks"`
		} `json:"properties"`
	}
	if err := json.Unmarshal(WorkstreamProposeTool().InputSchema, &schema); err != nil {
		t.Fatal(err)
	}
	if schema.Properties.Tasks.MaxItems != WorkstreamPlanMost {
		t.Errorf("a proposal holds %d cards; a plan is at most %d", schema.Properties.Tasks.MaxItems, WorkstreamPlanMost)
	}
	for _, must := range []string{"nothing is made until then", "involve someone else", "carry a due date", strconv.Itoa(WorkstreamPlanFewest) + " or more", "never also make the same tasks", "never say whether the tasks exist yet and never tell them to press Add or Start"} {
		if !strings.Contains(WorkstreamCardsRule, must) {
			t.Errorf("the cards rule does not say %q", must)
		}
	}
}
