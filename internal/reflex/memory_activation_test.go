package reflex

import (
	"context"
	"strings"
	"testing"
)

func TestNoticeDropsEphemeralAndBoundsUseFeedback(t *testing.T) {
	client := &fake{replies: []string{`{"keep":"run this once","durability":"ephemeral","used":[1,1,0,3,2],"contradicted":[2,2,-1,99]}`}}
	result, err := Notice(context.Background(), client, "run this once", "used rule one", []string{"rule one", "rule two"}, true)
	if err != nil {
		t.Fatal(err)
	}
	if result.Keep != "" || result.Durability != "ephemeral" {
		t.Fatalf("ephemeral candidate survived: %+v", result)
	}
	if len(result.Contradicted) != 1 || result.Contradicted[0] != 2 {
		t.Fatalf("unbounded contradiction feedback: %v", result.Contradicted)
	}
	if len(result.Used) != 2 || result.Used[0] != 1 || result.Used[1] != 2 {
		t.Fatalf("unbounded feedback: %v", result.Used)
	}
}

func TestNoticeFeedbackWorksWithNoticingDisabled(t *testing.T) {
	client := &fake{replies: []string{`{"keep":"must not save","durability":"standing_rule","used":[1]}`}}
	result, err := Notice(context.Background(), client, "apply the rule", "applied", []string{"the rule"}, false)
	if err != nil {
		t.Fatal(err)
	}
	if result.Keep != "" || len(result.Used) != 1 {
		t.Fatalf("disabled noticing lost feedback or saved: %+v", result)
	}
	if !strings.Contains(userMessage(client.calls[0]), "1. the rule") {
		t.Fatal("feedback call did not receive the working set")
	}
}
