package session

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/Agent-Field/agentfield/sdk/go/ai"
)

// A receipt naming a file cannot establish the values written inside it. The
// live #672 trace rejected fields absent only from the reader's summary.
func TestCompletionReaderSeesExactSmallWriteAndItsOutcome(t *testing.T) {
	const report = `{"value":"receipt_id","status":"current","record_id":"059f308d57830a036ed0b080c97491ae","revision":1,"source_id":"805ee9f1df5f4ef0","draft":"Use receipt_id.","note":""}`
	for _, outcome := range []string{"Successfully wrote 174 bytes", "Error: permission denied"} {
		t.Run(outcome, func(t *testing.T) {
			args, _ := json.Marshal(map[string]any{"path": strings.Repeat("long-directory/", 10) + "initial.json", "content": report})
			completer := &scriptedCompleter{steps: []step{finalText("NOTHING LEFT TO DO")}}
			a := checkpointAgent(t, completer)
			workedTurn(a, "Write the current contract as JSON with a numeric revision and exact source IDs.", 0)
			a.record(toolCallMessage("write-report", "write", string(args)))
			a.record(ai.Message{Role: "tool", ToolCallID: "write-report", Content: []ai.ContentPart{{Type: "text", Text: outcome}}})
			a.record(textMessage("assistant", "The report is written."))
			a.readRemains(context.Background())
			if completer.requests() != 1 {
				t.Fatal("completion reader was not called")
			}
			page := messageText(completer.request(0)[0])
			if !strings.Contains(page, string(args)) || !strings.Contains(page, outcome) {
				t.Fatalf("reader cannot distinguish the exact submitted report from its outcome:\n%s", page)
			}
		})
	}
}

func TestWriteEvidenceRequiresAMatchingResultAndRemainsBounded(t *testing.T) {
	small := `{"path":"small.json","content":"EXACT-WRITTEN-BYTES"}`
	large, _ := json.Marshal(map[string]string{"path": "large.json", "content": strings.Repeat("large payload ", 2000)})
	messages := []ai.Message{
		toolCallMessage("pending", "write", `{"path":"pending.json","content":"NOT-CONFIRMED"}`),
		toolCallMessage("small", "write", small),
		toolCallMessage("large", "write", string(large)),
		{Role: "tool", ToolCallID: "small", Content: []ai.ContentPart{{Type: "text", Text: "write succeeded"}}},
		{Role: "tool", ToolCallID: "large", Content: []ai.ContentPart{{Type: "text", Text: "write failed: quota exceeded"}}},
	}
	page := checkpointCompletionPage("Write the files", messages)
	if !strings.Contains(page, small) {
		t.Fatalf("small write absent:\n%s", page)
	}
	if strings.Contains(page, "NOT-CONFIRMED") || strings.Contains(page, strings.Repeat("large payload ", 100)) {
		t.Fatalf("unconfirmed or oversized payload reached the reader:\n%s", page)
	}
	if !strings.Contains(page, "arguments omitted") || !strings.Contains(page, "write failed: quota exceeded") {
		t.Fatalf("omission or failure was concealed:\n%s", page)
	}
	if len(page) > checkpointDigestBytes {
		t.Fatalf("digest grew beyond its budget: %d", len(page))
	}
}

// A later edit must travel with its own result rather than borrowing an older
// write's success. The raw arguments retain append mode and edit structure.
func TestWriteEvidencePairsEditAndAppendInputsByCallID(t *testing.T) {
	appended := `{"path":"report.json","content":"append-value","append":true}`
	edited := `{"path":"report.json","old_text":"1","new_text":"2"}`
	page := checkpointCompletionPage("Update report", []ai.Message{
		toolCallMessage("append", "write", appended),
		toolCallMessage("edit", "edit", edited),
		{Role: "tool", ToolCallID: "edit", Content: []ai.ContentPart{{Type: "text", Text: "edit failed"}}},
		{Role: "tool", ToolCallID: "append", Content: []ai.ContentPart{{Type: "text", Text: "append succeeded"}}},
	})
	if !strings.Contains(page, "edit failed\nsubmitted arguments: "+edited) || !strings.Contains(page, "append succeeded\nsubmitted arguments: "+appended) {
		t.Fatalf("write modes or outcomes crossed calls:\n%s", page)
	}
}
