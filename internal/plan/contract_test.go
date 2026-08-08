package plan

import (
	"context"
	"reflect"
	"strings"
	"sync"
	"testing"

	"github.com/Agent-Field/agentfield/sdk/go/ai"
)

type contractCaptureClient struct {
	messages []ai.Message
}

func (c *contractCaptureClient) CompleteWithMessages(_ context.Context, messages []ai.Message, _ ...ai.Option) (*ai.Response, error) {
	c.messages = append([]ai.Message(nil), messages...)
	return response(`{"contract":"Run the focused checks and verify the integrated result."}`), nil
}

func TestContractsNoPlaybookPromptIsByteIdentical(t *testing.T) {
	graph := contractFixture()
	client := &contractCaptureClient{}
	usage, err := Contracts(context.Background(), client, graph, func(Node) string { return " \n" })
	if err != nil {
		t.Fatal(err)
	}
	if usage.Calls != 1 || graph.Nodes[0].Contract == "" {
		t.Fatalf("Contracts() usage=%+v contract=%q", usage, graph.Nodes[0].Contract)
	}

	type renderedMessage struct {
		role string
		text string
	}
	got := make([]renderedMessage, 0, len(client.messages))
	for _, message := range client.messages {
		got = append(got, renderedMessage{role: message.Role, text: textOf(message)})
	}
	want := []renderedMessage{
		{role: "system", text: contractPrompt},
		{role: "user", text: "Goal:\nRepair the parser"},
		{role: "user", text: "The job: Parser checks — Repair parser validation\n" +
			"It is expected to touch: internal/parser/check.go; Makefile\n" +
			"The instruction the agent will receive:\nUpdate internal/parser/check.go and run make check.\n\n" +
			"Write the working method for this kind of job."},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("no-playbook Contracts() prompt changed\ngot:  %#v\nwant: %#v", got, want)
	}
}

// The working method is where a domain's own idea of "checked" gets written
// down, so it is where the inference across the join has to be refused: a
// contract that says which parts to test is what lets a leaf prove every part
// and hand over something nobody can use. Done is stated in the user's terms
// and verification is the whole path, with an honest exit for what this machine
// cannot run.
func TestContractDemandsUserTruthDoneAndEndToEndVerification(t *testing.T) {
	for name, want := range map[string]string{
		"done is what the user does and sees": "said as what whoever ends up using the result does\n  with it and sees",
		"verification is the whole path":      "the whole path exercised the way that user reaches it",
		"before the word may be used":         "run\n  before anything may be called verified",
		"parts never add up":                  "parts checked separately never\n  add up to a working result",
		"the honest gap is named":             "what\n  to declare unverified and the one short check that would settle it",
	} {
		if !strings.Contains(contractPrompt, want) {
			t.Errorf("the contract prompt no longer asks for %s: %q missing", name, want)
		}
	}
}

func TestContractsAppendsEarnedMethodNotesToTargetMessage(t *testing.T) {
	graph := contractFixture()
	client := &contractCaptureClient{}
	const notes = "- [repo:internal/parser] Run make check; direct go test misses generated fixtures."
	_, err := Contracts(context.Background(), client, graph, func(node Node) string {
		if node.ID != graph.Nodes[0].ID {
			t.Fatalf("playbook lookup node = %d, want %d", node.ID, graph.Nodes[0].ID)
		}
		return notes
	})
	if err != nil {
		t.Fatal(err)
	}
	if got := textOf(client.messages[0]); got != contractPrompt {
		t.Fatalf("playbook leaked into the doctrine: %q", got)
	}
	wantTarget := "The job: Parser checks — Repair parser validation\n" +
		"It is expected to touch: internal/parser/check.go; Makefile\n" +
		"The instruction the agent will receive:\nUpdate internal/parser/check.go and run make check.\n\n" +
		"Earned method notes for this territory:\n" + notes + "\n\n" +
		"Write the working method for this kind of job."
	if got := textOf(client.messages[2]); got != wantTarget {
		t.Fatalf("earned notes are not at the tail of the target message:\ngot:  %q\nwant: %q", got, wantTarget)
	}
}

// The whole point of moving the playbook: every leaf in one fan-out shares the
// system message and the shared-context message byte for byte, whatever its own
// earned notes say, so N-1 of the concurrent calls land on a warm prefix.
func TestContractsSharePrefixAcrossTargets(t *testing.T) {
	graph := contractFixture()
	graph.Add(Node{
		Title:   "Fixture regeneration",
		Summary: "Regenerate the parser fixtures",
		Sources: []string{"internal/parser/testdata"},
		Brief:   "Regenerate the golden fixtures.",
	})
	client := &contractFanoutClient{}
	if _, err := Contracts(context.Background(), client, graph, func(node Node) string {
		return "- notes for node " + strings.Repeat("x", node.ID)
	}); err != nil {
		t.Fatal(err)
	}
	calls := client.snapshot()
	if len(calls) != 2 {
		t.Fatalf("contract calls = %d, want 2", len(calls))
	}
	for _, call := range calls[1:] {
		if textOf(call[0]) != textOf(calls[0][0]) {
			t.Fatalf("system message differs across targets:\n%q\n%q", textOf(call[0]), textOf(calls[0][0]))
		}
		if textOf(call[1]) != textOf(calls[0][1]) {
			t.Fatalf("shared context differs across targets:\n%q\n%q", textOf(call[1]), textOf(calls[0][1]))
		}
		if textOf(call[2]) == textOf(calls[0][2]) {
			t.Fatal("two different leaves produced the same target message")
		}
	}
}

type contractFanoutClient struct {
	mutex sync.Mutex
	calls [][]ai.Message
}

func (c *contractFanoutClient) CompleteWithMessages(_ context.Context, messages []ai.Message, _ ...ai.Option) (*ai.Response, error) {
	c.mutex.Lock()
	defer c.mutex.Unlock()
	c.calls = append(c.calls, append([]ai.Message(nil), messages...))
	return response(`{"contract":"Run the focused checks and verify the integrated result."}`), nil
}

func (c *contractFanoutClient) snapshot() [][]ai.Message {
	c.mutex.Lock()
	defer c.mutex.Unlock()
	return append([][]ai.Message(nil), c.calls...)
}

func contractFixture() *Graph {
	graph := &Graph{Goal: "Repair the parser", NextID: 1}
	graph.Add(Node{
		Title:   "Parser checks",
		Summary: "Repair parser validation",
		Sources: []string{"internal/parser/check.go", "Makefile"},
		Brief:   "Update internal/parser/check.go and run make check.",
	})
	return graph
}

// The one-leaf job has no title because there was nothing to distinguish it
// from — it is the whole ask. Naming it as an empty handle, or naming the same
// words twice, spends the model's attention on nothing.
func TestUntitledLeafStatesTheJobOnce(t *testing.T) {
	graph := &Graph{Goal: "write the note that announces the change"}
	graph.Add(Node{Kind: KindWork, Summary: "write the note that announces the change", Stage: 1})
	client := &contractCaptureClient{}
	usage, err := Contracts(context.Background(), client, graph, nil)
	if err != nil {
		t.Fatal(err)
	}
	if usage.Calls != 1 {
		t.Fatalf("calls = %d, want exactly 1 for a one-leaf job", usage.Calls)
	}
	want := "The job: write the note that announces the change\n\n" +
		"Write the working method for this kind of job."
	if got := textOf(client.messages[2]); got != want {
		t.Fatalf("untitled target message:\ngot:  %q\nwant: %q", got, want)
	}
	if got := textOf(client.messages[0]); got != contractPrompt {
		t.Fatalf("the one-leaf job reads a different doctrine byte: %q", got)
	}
}
