package session

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Agent-Field/agentfield/sdk/go/ai"
)

func contextReplayCall(id, name, args string) ai.Message {
	return ai.Message{Role: "assistant", ToolCalls: []ai.ToolCall{{ID: id, Type: "function", Function: ai.ToolCallFunction{Name: name, Arguments: args}}}}
}
func contextReplayResult(id, body string) ai.Message {
	m := textMessage("tool", body)
	m.ToolCallID = id
	return m
}

func TestContextReplayReceiptDoesNotBorrowLaterCommand(t *testing.T) {
	messages := []ai.Message{
		contextReplayCall("call_0", "bash", `{"command":"check-first"}`),
		contextReplayResult("call_0", "FIRST CHECK PASSED"),
		contextReplayCall("call_0", "read", `{"path":"later.txt"}`),
		contextReplayResult("call_0", "LATER FILE CONTENTS"),
	}
	receipts := lastToolReceipts(&Agent{messages: messages}, 10)
	if len(receipts) != 2 {
		t.Fatalf("receipts=%+v", receipts)
	}
	if receipts[0].tool != "bash" || !strings.Contains(receipts[0].args, "check-first") {
		t.Fatalf("first result borrowed later call: %+v", receipts[0])
	}
}

func TestContextReplayPendingCallDoesNotBorrowEarlierResult(t *testing.T) {
	source := admissionSource{messages: []ai.Message{
		contextReplayCall("call_0", "read", `{"path":"first.txt"}`),
		contextReplayResult("call_0", "FIRST FILE"),
		contextReplayCall("call_0", "read", `{"path":"pending.txt"}`),
	}, record: "fixture.jsonl"}
	spent := 0
	handles := admissionEvidence(source, &spent)
	for _, h := range handles {
		if strings.Contains(h.Input, "pending.txt") {
			if h.Outcome != AdmissionUnanswered || h.Source != "" {
				t.Fatalf("pending call borrowed earlier result: %+v", h)
			}
			return
		}
	}
	t.Fatal("pending call disappeared from evidence")
}

func TestContextReplayCompactedResultKeepsItsOwnToolName(t *testing.T) {
	messages := []ai.Message{
		textMessage("system", "fixture"),
		contextReplayCall("call_0", "read", `{"path":"first.txt"}`),
		contextReplayResult("call_0", strings.Repeat("READ CONTENT ", 200)),
		contextReplayCall("call_0", "bash", `{"command":"later-check"}`),
		contextReplayResult("call_0", "LATER CHECK"),
		contextReplayCall("call_1", "ls", `{"path":"."}`),
		contextReplayResult("call_1", "NEWEST BATCH"),
	}
	compacted := compactToolHistory(messages, len(messages), func(ai.Message) string { return "fixture.txt" })
	got := messageContentText(compacted[2])
	if !strings.Contains(got, "[reduced view: read") {
		t.Fatalf("compacted old read named another tool: %s", got)
	}
}

// The real execution path must retain both outcomes when a provider recycles
// its IDs, not merely match a synthetic transcript after the fact.
func TestContextReplayAdmissionUsesEachExecutedOutcome(t *testing.T) {
	completer := &scriptedCompleter{steps: []step{
		func(context.Context, []ai.Message) (*ai.Response, error) {
			return toolResponse("call_0", "read", `{"path":"missing.txt"}`), nil
		},
		func(context.Context, []ai.Message) (*ai.Response, error) {
			return toolResponse("call_0", "read", `{"path":"present.txt"}`), nil
		},
		func(context.Context, []ai.Message) (*ai.Response, error) {
			return textResponse("Read the available input."), nil
		},
	}}
	agent := admissionAgent(t, completer, filepath.Join(t.TempDir(), "session.jsonl"))
	writeFile(t, filepath.Join(agent.config.Workspace, "present.txt"), "CURRENT INPUT")
	collect(t, mustSubmit(t, agent, "Read both inputs."))
	got := agent.admissionContext()
	if len(got.Evidence) != 2 {
		t.Fatalf("distinct calls collapsed: %+v", got.Evidence)
	}
	for _, h := range got.Evidence {
		switch {
		case strings.Contains(h.Input, "missing.txt"):
			if h.Outcome != AdmissionFailed {
				t.Fatalf("failure replaced: %+v", h)
			}
		case strings.Contains(h.Input, "present.txt"):
			if h.Outcome != AdmissionOK {
				t.Fatalf("success lost: %+v", h)
			}
			body, err := os.ReadFile(h.Result)
			if err != nil || string(body) != "CURRENT INPUT" {
				t.Fatalf("wrong result pointer: %+v, %q, %v", h, body, err)
			}
		default:
			t.Fatalf("unexpected call %+v", h)
		}
	}
}

func TestContextReplayResultPairsStayWithinBatch(t *testing.T) {
	messages := []ai.Message{
		contextReplayCall("call_0", "read", `{"path":"old"}`), contextReplayResult("call_0", "old"),
		{Role: "assistant", ToolCalls: []ai.ToolCall{
			{ID: "call_0", Function: ai.ToolCallFunction{Name: "read", Arguments: `{"path":"new"}`}},
			{ID: "call_1", Function: ai.ToolCallFunction{Name: "ls", Arguments: `{}`}},
		}},
		contextReplayResult("call_1", "listing"), contextReplayResult("call_0", "new"),
		contextReplayResult("call_0", "duplicate orphan"),
	}
	pairs := toolResultCalls(messages)
	if len(pairs) != 3 || pairs[1] != &messages[0].ToolCalls[0] || pairs[3] != &messages[2].ToolCalls[1] || pairs[4] != &messages[2].ToolCalls[0] {
		t.Fatalf("crossed, reordered, or duplicate results paired incorrectly: %+v", pairs)
	}
}

func TestContextReplayResumeDropsReusedOrphan(t *testing.T) {
	messages := []ai.Message{
		contextReplayCall("call_0", "read", `{"path":"old"}`), contextReplayResult("call_0", "old"),
		contextReplayCall("call_1", "read", `{"path":"new"}`), contextReplayResult("call_1", "new"),
		contextReplayResult("call_0", "orphan from previous batch"),
	}
	repaired := repairTranscript(messages)
	if len(repaired) != 4 {
		t.Fatalf("old ID admitted an orphan result: %+v", repaired)
	}
}

func TestContextReplayInheritedFailureDoesNotCollideWithLocalID(t *testing.T) {
	parent := AdmissionContext{Evidence: []AdmissionHandle{{Call: "call_0", Tool: "read", Input: "parent.txt", Source: "parent.jsonl", Outcome: AdmissionFailed}}}
	current := AdmissionContext{Evidence: []AdmissionHandle{{Call: "call_0", Tool: "read", Input: "current.txt", Source: "current.jsonl", Outcome: AdmissionOK}}}
	current.inherit(parent, map[string]bool{}, 0)
	if len(current.Evidence) != 2 || current.Evidence[0].Outcome != AdmissionFailed {
		t.Fatalf("inherited failure disappeared: %+v", current.Evidence)
	}
	current.inherit(parent, map[string]bool{}, 0)
	if len(current.Evidence) != 2 {
		t.Fatalf("same inherited evidence duplicated: %+v", current.Evidence)
	}
}

func TestContextReplayInheritedFailureSurvivesEarlierSuccess(t *testing.T) {
	parent := AdmissionContext{Evidence: []AdmissionHandle{
		{Call: "first", Outcome: AdmissionOK},
		{Call: "second", Outcome: AdmissionFailed, Detail: "dependency unavailable"},
	}}
	var child AdmissionContext
	child.inherit(parent, map[string]bool{}, 0)
	if len(child.Evidence) != 1 || child.Evidence[0].Call != "second" {
		t.Fatalf("later failure hidden by successful call: %+v", child)
	}
}

func TestContextReplayOnlySelectedEvidenceFilesResults(t *testing.T) {
	var messages []ai.Message
	for i := 0; i < 20; i++ {
		messages = append(messages, contextReplayCall("call_0", "read", `{"path":"same.txt"}`), contextReplayResult("call_0", "result"))
	}
	files := 0
	source := admissionSource{messages: messages, resultSource: func(ai.Message) string { files++; return strings.Repeat("longpath/", 1000) }}
	spent := admissionRoom() - 500
	handles := admissionEvidence(source, &spent)
	if files != len(handles) || files > admissionHandlesKept || spent > admissionRoom() {
		t.Fatalf("unselected filing or exceeded budget: %d files, %d handles, %d spent", files, len(handles), spent)
	}
}

// The completion reader was removed, but its evidence invariant belongs to
// every remaining history consumer: duplicate and orphan results cannot attach
// to another occurrence of the same provider call ID.
func TestContextReplayBatchPairingIgnoresOrphanResults(t *testing.T) {
	first := contextReplayCall("call_0", "read", `{"path":"first.txt"}`)
	second := contextReplayCall("call_0", "write", `{"path":"second.txt","content":"right"}`)
	messages := []ai.Message{
		first,
		contextReplayResult("call_0", "first content"),
		contextReplayResult("call_0", "DUPLICATE RESULT"),
		textMessage("assistant", "Between batches."),
		contextReplayResult("call_0", "ORPHAN RESULT"),
		second,
		contextReplayResult("call_0", "Successfully wrote 5 bytes"),
	}
	results := toolResults(messages)
	if len(results) != 2 || results[&first.ToolCalls[0]] != "first content" || results[&second.ToolCalls[0]] != "Successfully wrote 5 bytes" {
		t.Fatalf("history paired duplicate or orphan evidence with a call: %#v", results)
	}
}
