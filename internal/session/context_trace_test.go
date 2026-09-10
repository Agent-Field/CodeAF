package session

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Agent-Field/aforge-v2/internal/standing"
	"github.com/Agent-Field/aforge-v2/internal/workspace"
)

func traceFixture(t *testing.T, lines ...string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "transcript.jsonl")
	if err := os.WriteFile(path, []byte(strings.Join(lines, "\n")+"\n"), 0600); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestContextTraceLinksOriginalOccurrencesWithoutExposingBodies(t *testing.T) {
	path := traceFixture(t,
		`{"type":"session","version":1,"id":"conversation"}`,
		`{"type":"context_exposure","context_exposure":{"execution_id":"run-a","phase":"selected","parent_cause":"not_recorded"}}`,
		`{"type":"message","role":"assistant","reasoning":"private reasoning","toolCalls":[{"id":"call-1","type":"function","function":{"name":"read","arguments":"private arguments"}}]}`,
		`{"type":"message","role":"tool","toolCallId":"call-1","content":"private result"}`,
		`{"type":"compaction","window":2}`,
		`{"type":"message","role":"assistant","toolCalls":[{"id":"call-1","type":"function","function":{"name":"read","arguments":"copied arguments"}}]}`,
		`{"type":"message","role":"tool","toolCallId":"call-1","content":"copied result"}`,
		`{"type":"message","role":"assistant","toolCalls":[{"id":"call-1","type":"function","function":{"name":"write","arguments":"new arguments"}}]}`,
		`{"type":"message","role":"tool","toolCallId":"call-1","content":"new result"}`,
		`{"type":"principal","principal":{"event":"decided","decision":"done"}}`,
		`{"type":"context_exposure","context_exposure":{"execution_id":"run-a","phase":"loop_returned"}}`,
	)
	page, err := readContextTrace(context.Background(), path, 1)
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Rows) != 7 {
		t.Fatalf("trace includes copied or missing events: %+v", page.Rows)
	}
	if page.Rows[2].CallLine != 3 || page.Rows[4].CallLine != 8 {
		t.Errorf("reused provider ID not linked to original occurrences: %+v", page.Rows)
	}
	if page.Rows[1].ExecutionID != "run-a" || page.Rows[1].Calls[0].Name != "read" {
		t.Errorf("missing window or tool identity: %+v", page.Rows[1])
	}
	body, err := json.Marshal(page)
	if err != nil {
		t.Fatal(err)
	}
	for _, secret := range []string{"private reasoning", "private arguments", "private result", "copied result", "new arguments"} {
		if strings.Contains(string(body), secret) {
			t.Errorf("trace duplicated %q", secret)
		}
	}
}

func TestContextTraceDoesNotGuessOverlappingExecutionsOrDuplicateCalls(t *testing.T) {
	path := traceFixture(t,
		`{"type":"context_exposure","context_exposure":{"execution_id":"a","phase":"selected"}}`,
		`{"type":"context_exposure","context_exposure":{"execution_id":"b","phase":"selected"}}`,
		`{"type":"message","role":"assistant","toolCalls":[{"id":"same","function":{"name":"read"}},{"id":"same","function":{"name":"read"}}]}`,
		`{"type":"message","role":"tool","toolCallId":"same"}`,
		`{"type":"message","role":"tool","toolCallId":"same"}`,
		`{"type":"context_exposure","context_exposure":{"execution_id":"a","phase":"loop_returned"}}`,
		`{"type":"message","role":"assistant","toolCalls":[{"id":"late","function":{"name":"read"}}]}`,
	)
	page, err := readContextTrace(context.Background(), path, 1)
	if err != nil {
		t.Fatal(err)
	}
	for _, row := range page.Rows[2:] {
		if row.Exposure != nil {
			continue
		}
		if row.ExecutionID != "" || row.Association != "overlapping_executions" || row.CallLine != 0 {
			t.Errorf("ambiguous evidence attributed: %+v", row)
		}
	}
}

func TestContextTracePaginationPreservesOriginalLineLinks(t *testing.T) {
	lines := []string{`{"type":"context_exposure","context_exposure":{"execution_id":"a","phase":"selected"}}`}
	for i := 0; i < 45; i++ {
		lines = append(lines, fmt.Sprintf(`{"type":"message","role":"assistant","toolCalls":[{"id":"c-%d","function":{"name":"read"}}]}`, i), fmt.Sprintf(`{"type":"message","role":"tool","toolCallId":"c-%d"}`, i))
	}
	path := traceFixture(t, lines...)
	first, err := readContextTrace(context.Background(), path, 1)
	if err != nil {
		t.Fatal(err)
	}
	if len(first.Rows) != contextTracePageSize || first.NextLine == 0 {
		t.Fatalf("page not bounded: %+v", first)
	}
	second, err := readContextTrace(context.Background(), path, first.NextLine)
	if err != nil {
		t.Fatal(err)
	}
	if second.Rows[0].Line != first.NextLine || second.Rows[0].ExecutionID != "a" || second.Rows[0].CallLine != first.NextLine-1 {
		t.Errorf("page cut lost earlier call association: %+v", second.Rows[0])
	}
}

func TestContextTraceRetainsSelectedSnapshotAndReportsUnreadableTail(t *testing.T) {
	path := traceFixture(t,
		`{"type":"context_exposure","context_exposure":{"execution_id":"a","phase":"selected","standing":[{"id":"hold","revision":3,"prompt":"Keep original instruction","origin":{"sessionId":"source"}}],"shared":[{"id":"fact","revision":2}]}}`,
		`{"type":"message"`,
		`{"type":"principal","principal":{"decision":"done"}}`,
	)
	page, err := readContextTrace(context.Background(), path, 1)
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Rows) != 1 || page.Unreadable == "" {
		t.Fatalf("truncated reading presented as complete: %+v", page)
	}
	if page.Rows[0].Exposure.Standing[0].Prompt != "Keep original instruction" || page.Rows[0].Exposure.Shared[0].Revision != 2 {
		t.Errorf("snapshot lost: %+v", page.Rows[0])
	}
}

func TestContextTraceRecordsScopeAndAnswerFromSelectedSnapshot(t *testing.T) {
	path := filepath.Join(t.TempDir(), "transcript.jsonl")
	file, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	agent := &Agent{file: &sessionFile{file: file}}
	item := standing.Item{ID: "rule", Revision: 7, Words: "Keep the API compatible", Scope: &standing.Scope{CollectionIDs: []string{"product"}, Descendants: true}, Adoption: &standing.Adoption{Actor: "person", ProposalID: 12}}
	owner := workspace.Ref{Kind: workspace.ConversationKind, ID: "conversation"}
	execution := agent.recordContextExposureLocked(owner, nil, []standing.Item{item}, "", map[string]int{"product": 1, "implementation": 0})
	if execution == "" {
		t.Fatal("selected receipt did not reach disk")
	}
	agent.finishContextExposure(execution)
	page, err := readContextTrace(context.Background(), path, 1)
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Rows) != 2 {
		t.Fatalf("missing receipt boundary: %+v", page)
	}
	got := page.Rows[0].Exposure
	if got.Owner != owner || got.GoverningCollections["product"] != 1 || got.Standing[0].Scope == nil || got.Standing[0].Adoption == nil || got.Standing[0].Adoption.ProposalID != 12 || got.Standing[0].Altitude != "" {
		t.Fatalf("scope or actual answer provenance was lost or replaced by legacy altitude: %+v", got)
	}
	if got.Standing[0].Prompt != item.Words || got.Standing[0].Revision != 7 {
		t.Errorf("selected instruction not retained: %+v", got.Standing[0])
	}
	if page.Rows[1].Exposure.ExecutionID != execution || page.Rows[1].Exposure.Phase != "loop_returned" {
		t.Errorf("end misidentified: %+v", page.Rows[1])
	}
}
