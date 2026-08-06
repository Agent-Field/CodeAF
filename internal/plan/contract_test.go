package plan

import (
	"context"
	"reflect"
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

func TestContractsAppendsEarnedMethodNotesAfterDoctrine(t *testing.T) {
	graph := contractFixture()
	client := &contractCaptureClient{}
	_, err := Contracts(context.Background(), client, graph, func(node Node) string {
		if node.ID != graph.Nodes[0].ID {
			t.Fatalf("playbook lookup node = %d, want %d", node.ID, graph.Nodes[0].ID)
		}
		return "- [repo:internal/parser] Run make check; direct go test misses generated fixtures."
	})
	if err != nil {
		t.Fatal(err)
	}
	wantSystem := contractPrompt + "\n\nEarned method notes for this territory:\n" +
		"- [repo:internal/parser] Run make check; direct go test misses generated fixtures."
	if got := textOf(client.messages[0]); got != wantSystem {
		t.Fatalf("contract doctrine = %q, want %q", got, wantSystem)
	}
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
