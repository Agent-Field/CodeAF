package session

import (
	"context"
	"encoding/json"
	"fmt"
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
			if strings.Contains(page, `write {"value"`) {
				t.Fatal("ledger showed a clipped report instead of its target")
			}
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
		{Role: "tool", ToolCallID: "small", Content: []ai.ContentPart{{Type: "text", Text: "write succeeded"}}},
	}
	page := checkpointCompletionPage("Write the files", messages)
	if !strings.Contains(page, small) {
		t.Fatalf("small write absent:\n%s", page)
	}
	if strings.Contains(page, "NOT-CONFIRMED") || strings.Contains(page, strings.Repeat("large payload ", 100)) {
		t.Fatalf("unconfirmed or oversized payload reached the reader:\n%s", page)
	}
	page = checkpointCompletionPage("Write the files", append(messages,
		toolCallMessage("large", "write", string(large)),
		ai.Message{Role: "tool", ToolCallID: "large", Content: []ai.ContentPart{{Type: "text", Text: "write failed: quota exceeded"}}}))
	if strings.Contains(page, strings.Repeat("large payload ", 100)) {
		t.Fatal("oversized input reached reader")
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
	edited := `{"path":"report.json","edits":[{"oldText":"1","newText":"2"}]}`
	batch := toolCallMessage("append", "write", appended)
	batch.ToolCalls = append(batch.ToolCalls, toolCallMessage("edit", "edit", edited).ToolCalls...)
	page := checkpointCompletionPage("Update report", []ai.Message{
		batch,
		{Role: "tool", ToolCallID: "edit", Content: []ai.ContentPart{{Type: "text", Text: "edit failed"}}},
		{Role: "tool", ToolCallID: "append", Content: []ai.ContentPart{{Type: "text", Text: "append succeeded"}}},
	})
	if !strings.Contains(page, "edit failed\nsubmitted arguments: "+edited) || !strings.Contains(page, "append succeeded") || strings.Contains(page, appended) {
		t.Fatalf("write modes or outcomes crossed calls:\n%s", page)
	}
}

// Writing several small files must not evict the test failure that preceded
// them. Only the newest completed write gets the extra argument allowance.
func TestWriteEvidenceKeepsEarlierFailureWhenManyFilesWereWritten(t *testing.T) {
	messages := []ai.Message{
		toolCallMessage("test", "bash", `{"command":"go test ./..."}`),
		{Role: "tool", ToolCallID: "test", Content: []ai.ContentPart{{Type: "text", Text: "FAIL: migration broke existing records"}}},
	}
	var newest string
	for i := 0; i < 20; i++ {
		id := fmt.Sprintf("write-%d", i)
		raw, _ := json.Marshal(map[string]string{"path": id + ".md", "content": strings.Repeat("bounded data ", 100) + id})
		newest = string(raw)
		messages = append(messages, toolCallMessage(id, "write", newest), ai.Message{Role: "tool", ToolCallID: id, Content: []ai.ContentPart{{Type: "text", Text: "write succeeded"}}})
	}
	page := checkpointCompletionPage("Fix migration and update the docs", messages)
	if !strings.Contains(page, "FAIL: migration broke existing records") || !strings.Contains(page, newest) {
		t.Fatalf("latest write input crowded out the failure it must be weighed against:\n%s", page)
	}
	if strings.Count(page, "submitted arguments:") != 1 {
		t.Fatal("more than the newest completed write carries its input")
	}
}
