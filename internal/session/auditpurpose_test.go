package session

// ── A CHECKER'S CALLS SAY THEY ARE THE CHECKER'S, AND WHICH NODE THEY CHECK ──
//
// An auditor's provider call used to be indistinguishable from the
// conversation's own turn in every record this build keeps: the model-call log
// tagged both `turn`, and the usage ledger wrote no role and no task on either.
// A bill nobody could read — a check and a session turn on the same model were
// the same row.
//
// THE TAG AND THE NODE ARE READ OFF A CALLS.JSONL ROW, because that is the one
// place the word is legible: the tag rides the context ([provider.WithCallTag])
// and only the provider's own log writes it down. So these tests run the real
// provider boundary over a scripted HTTP client — the rig agent_test.go already
// uses ([fakeHTTP], [eventStreamOf]) — with the model-call log pointed at a file
// of the test's own.

import (
	"bufio"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/Agent-Field/codeaf/internal/calllog"
	"github.com/Agent-Field/codeaf/internal/provider"
)

// auditWireClient is a real provider adapter over a scripted event stream, so
// the tag the turn loop stamps reaches the model-call log exactly as it does in
// the product. It answers every request with a one-line finish.
func auditWireClient(t *testing.T) *provider.Client {
	t.Helper()
	client, err := provider.NewClient(provider.Config{
		BaseURL: "http://provider.test", Model: "test/model", APIKey: "test-key",
		HTTPClient: fakeHTTP(eventStreamOf(
			`{"choices":[{"index":0,"delta":{"role":"assistant","content":"done"},"finish_reason":"stop"}]}`,
		)),
	})
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	return client
}

// callLogFinishes is the finished half of the model-call log: the start rows are
// the same call's partners and say nothing an assertion here wants.
func callLogFinishes(t *testing.T, path string) []calllog.Record {
	t.Helper()
	file, err := os.Open(path)
	if err != nil {
		t.Fatalf("open the model-call log: %v", err)
	}
	defer file.Close()
	var rows []calllog.Record
	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 0, 64<<10), 16<<20)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		var row calllog.Record
		if err := json.Unmarshal([]byte(line), &row); err != nil {
			t.Fatalf("a line of the model-call log is not a record: %v", err)
		}
		if row.Phase == calllog.PhaseStart {
			continue
		}
		rows = append(rows, row)
	}
	return rows
}

// aCallTagged is the last finished row carrying a tag, and whether there was
// one. A turn beside its own errands writes several rows, so the tag — not the
// position — is what a reader here is looking for.
func aCallTagged(rows []calllog.Record, tag string) (calllog.Record, bool) {
	for _, row := range rows {
		if row.Tag == tag {
			return row, true
		}
	}
	return calllog.Record{}, false
}

// The auditor answers for a role and checks a node that is NOT the one it is,
// and both facts have to reach the model-call log: the word `auditor` and the
// id of the node it read. A conversation's turn and a task node's own turn keep
// the words they always had, so the checker's new row is a distinction rather
// than a relabelling.
func TestTheAuditorsCallSaysAuditorAndTheNodeItChecked(t *testing.T) {
	path := filepath.Join(t.TempDir(), "calls.jsonl")
	t.Setenv(calllog.EnvVar, path)
	calllog.Open("")
	t.Cleanup(func() {
		calllog.Close()
		os.Setenv(calllog.EnvVar, calllog.OffValue)
		calllog.Open("")
	})

	parent, dir := newTestAgent(t, auditWireClient(t), nil)

	// A conversation's own turn is still the turn, and carries no node.
	drainTurn(t, parent, "hello")

	// A task node's turn is its own word, with the node it IS beside it.
	graph := parent.graph()
	graph.run = func(*TaskNode) {}
	id := graph.reserve()
	graph.admit(id, taskSpec{title: "work", brief: "answer", acceptance: "done"})
	child, err := parent.newTaskAgent(context.Background(), dir, graph.node(id), "")
	if err != nil {
		t.Fatalf("newTaskAgent: %v", err)
	}
	t.Cleanup(func() { _ = child.Close() })
	drainTurn(t, child, "work")

	// The checker is built for node 7 and is not that node itself.
	auditGraph := &TaskGraph{nodes: map[uint64]*TaskNode{}}
	node := &TaskNode{graph: auditGraph, id: 7, spec: taskSpec{title: "the greeting"}}
	auditGraph.nodes[7] = node
	auditor, err := parent.newAuditAgent(dir, node, plainDoor(auditReadCommands), "")
	if err != nil {
		t.Fatalf("newAuditAgent: %v", err)
	}
	t.Cleanup(func() { _ = auditor.Close() })
	drainTurn(t, auditor, "is this finished?")

	rows := callLogFinishes(t, path)

	audit, ok := aCallTagged(rows, "auditor")
	if !ok {
		t.Fatalf("the checker's call reached the log under no `auditor` tag: %+v", rows)
	}
	if audit.Node != "7" {
		t.Errorf("the checker's call names node %q, want the node it checked, 7", audit.Node)
	}

	turn, ok := aCallTagged(rows, "turn")
	if !ok {
		t.Fatalf("a conversation's turn did not reach the log under `turn`: %+v", rows)
	}
	if turn.Node != "" {
		t.Errorf("a conversation's turn names node %q, want none", turn.Node)
	}

	task, ok := aCallTagged(rows, "task")
	if !ok {
		t.Fatalf("a task node's turn did not reach the log under `task`: %+v", rows)
	}
	if want := strconv.FormatUint(id, 10); task.Node != want {
		t.Errorf("a task node's turn names node %q, want its own id %s", task.Node, want)
	}
}
