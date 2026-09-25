package session

// THE NODE ROAD DOES NOT LEARN THE PLAN'S VOCABULARY.
//
// The note channel is the bash belt's alone: the worker that carries a note
// between its steps is internal/run's, which nothing constructs off that belt,
// and the store the note lands in does not exist on the node road at all. So
// the `note` field is offered only where a plan store exists to hold it, and
// the sense of "offered" here is the literal one — a conversation on the node
// road carries the schema it carried before this change, byte for byte.
//
// It is asserted rather than argued because it is cheap to break by accident:
// the schema is one string, and a field appended to it unconditionally would
// change the bytes of every request every node-road conversation makes, and
// would put a verb on a belt that can only refuse it.

import (
	"strings"
	"testing"
)

func TestTheNoteFieldIsOnlyOfferedWhereAPlanStoreHoldsIt(t *testing.T) {
	// The plan road: the belt asked for and an engine wired. Both are needed —
	// [Config.oneTaskRoad] is the one reading of that question, and the schema
	// is built from it so the page and the belt cannot disagree.
	t.Setenv("CODEAF_TASK_BELT", "bash")
	agent, _ := newTestAgent(t, &scriptedCompleter{}, nil)
	registerBeltRunEngine(t, &beltRunDouble{})
	if schema := string(agent.tasksTool().Schema); !strings.Contains(schema, `"note"`) {
		t.Fatalf("the plan road's tasks schema carries no note field:\n%s", schema)
	}

	// And the node road, where a note has nowhere to land. The schema must be
	// the one every conversation carried before this change.
	t.Setenv("CODEAF_TASK_BELT", "node")
	node, _ := newTestAgent(t, &scriptedCompleter{}, nil)
	schema := string(node.tasksTool().Schema)
	if strings.Contains(schema, `"note"`) {
		t.Fatalf("the node road's tasks schema carries a note field it cannot answer:\n%s", schema)
	}
	if schema != tasksSchemaJSON {
		t.Fatalf("the node road's tasks schema is not the schema it was:\ngot:  %s\nwant: %s", schema, tasksSchemaJSON)
	}
}

// AND A `note` SENT WHERE IT IS NOT OFFERED IS ANSWERED IN A SENTENCE. A model
// that has read a schema without the field will not send it; one that carries a
// memory of another road might, and a parse error is a worse answer than a
// refusal that says what happened.
func TestANoteWithNoRunToPutItOnIsRefusedInWords(t *testing.T) {
	t.Setenv("CODEAF_TASK_BELT", "node")
	agent, _ := newTestAgent(t, &scriptedCompleter{}, nil)
	answer, failed, err := agent.tasksTool().Execute(t.Context(), []byte(`{"note":"a fact it lacks"}`))
	if err != nil {
		t.Fatalf("the call errored rather than answering: %v", err)
	}
	if !failed || !strings.Contains(answer, "note needs an id") {
		t.Fatalf("a note with no id answered %q (refused=%v), want the sentence naming what it needs", answer, failed)
	}
}
