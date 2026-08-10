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

// The prompt whose whole job is to say how this kind of work is done well
// believed the worker held four tools. It holds a shell that also runs things in
// the background, file writing and editing, web search and fetch, recall of
// everything folded away, a line to its siblings, and — on ask, where the
// machine is configured — document reading and image, music, video and speech
// generation. A method cannot route through a capability it is not told exists,
// which is how a job that needed a PDF read got a method that worked around it.
func TestTheMethodWriterIsToldTheToolboxTheWorkerActuallyHas(t *testing.T) {
	for name, want := range map[string]string{
		"the shell runs background work": "a shell that also runs work in the background",
		"files are written and edited":   "file writing and\nediting",
		"the web is searched and read":   "web search and page fetching",
		"folded work can be recalled":    "recall of work already folded away",
		"siblings can be told one line":  "one line it can pass to the other agents on this job",
		"documents and media on ask":     "read documents and\ngenerate images, music, video and speech",
	} {
		if !strings.Contains(contractPrompt, want) {
			t.Errorf("the method writer is not told that %s: %q missing", name, want)
		}
	}
	if strings.Contains(contractPrompt, "a shell, file writing, file editing, and web search") {
		t.Error("the four-tool belief is still in the prompt")
	}
}

// Done-means had two authors and only one of them knew it was binding. A task's
// brief asked for a flicker to stop; its contract verified the structure of the
// page, and a sibling task of the same shape got it right — a coin flip. The
// clause costs a line and settles which document states the bar.
func TestTheMethodsVerificationIsBoundToTheBriefsAcceptanceBar(t *testing.T) {
	const want = "Where the\n  agent's instruction already states the bar for done, the check you write\n  exercises that bar itself rather than a stand-in for it."
	if !strings.Contains(contractPrompt, want) {
		t.Errorf("the verify step is no longer bound to the instruction's bar: %q missing", want)
	}
	// And it is bound inside the verify bullet, not stated somewhere the method
	// writer reads as general advice.
	verify := strings.Index(contractPrompt, "- How to verify:")
	mistakes := strings.Index(contractPrompt, "- The two or three mistakes")
	if verify < 0 || mistakes < verify || strings.Index(contractPrompt, want) > mistakes {
		t.Error("the acceptance-bar clause drifted out of the verify step")
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

// The hole this closes: both structuring passes iterated Leaves(), which is
// KindWork only, so the node whose output IS the deliverable — the one every
// finished job is judged against — was the single node in a plan with no
// instruction and no working method. It is written for like any other leaf, and
// it is told which of the two jobs it has.
func TestTheDeliverableOwnerIsWrittenAWorkingMethod(t *testing.T) {
	graph := &Graph{Goal: "compare three cities and write the result", NextID: 1}
	graph.Add(Node{Stage: 1, Title: "Berlin", Summary: "read Berlin", Brief: "Read Berlin."})
	graph.Add(Node{Stage: 1, Title: "Lisbon", Summary: "read Lisbon", Brief: "Read Lisbon."})
	graph.addSynthesis()
	sink := graph.Nodes[len(graph.Nodes)-1].ID
	if got := graph.deliverableSink(); got != sink {
		t.Fatalf("deliverable sink = %d, want the appended synthesis %d", got, sink)
	}

	client := &contractFanoutClient{}
	if _, err := Contracts(context.Background(), client, graph, nil); err != nil {
		t.Fatal(err)
	}
	calls := client.snapshot()
	if len(calls) != 3 {
		t.Fatalf("contract calls = %d, want one per leaf and one for the deliverable owner", len(calls))
	}
	if got := graph.Node(sink); got == nil || strings.TrimSpace(got.Contract) == "" {
		t.Fatal("the deliverable owner still runs on the generic loop")
	}

	// Exactly one of the three is told it is the deliverable, and the shared
	// prefix every contract is billed against does not move to say so.
	owners := 0
	for _, call := range calls {
		if textOf(call[0]) != contractPrompt || textOf(call[0]) != textOf(calls[0][0]) {
			t.Fatal("the deliverable owner reads a different doctrine byte")
		}
		if textOf(call[1]) != textOf(calls[0][1]) {
			t.Fatal("the deliverable owner shifted the shared context")
		}
		if strings.Contains(textOf(call[2]), contractDeliverableLine) {
			owners++
		}
	}
	if owners != 1 {
		t.Fatalf("%d contracts were told they are the deliverable, want exactly 1", owners)
	}
}

// Proportionality, on the same seam: a one-leaf job is already its own answer,
// so there is no second node to write for and no second call to buy.
func TestAOneLeafPlanBuysNoSecondMethod(t *testing.T) {
	graph := &Graph{Goal: "write the note"}
	graph.Add(Node{Kind: KindWork, Summary: "write the note", Stage: 1})
	if got := graph.deliverableSink(); got != 0 {
		t.Fatalf("deliverable sink = %d, want none for a one-leaf plan", got)
	}
	client := &contractFanoutClient{}
	if _, err := Contracts(context.Background(), client, graph, nil); err != nil {
		t.Fatal(err)
	}
	if calls := client.snapshot(); len(calls) != 1 {
		t.Fatalf("contract calls = %d, want exactly 1", len(calls))
	}
}

// The contract call is the one structuring call every leaf makes, and it used
// to fail on 100% of `aforge do` runs against a model the router had no
// structured-output path to: the reply came back fenced or with a sentence in
// front of it, the decode demanded a bare value, and stderr filled with
// `contract "": parse response: invalid character 'B'`. The call was paid for
// either way; the leaf then ran with no working method and nothing said so.
// Both shapes must now produce the method.
func TestContractSurvivesFencedAndProseWrappedReplies(t *testing.T) {
	const method = "Run the focused checks and verify the integrated result."
	for _, shape := range []struct {
		name  string
		reply string
	}{
		{"fenced", "```json\n{\"contract\":\"" + method + "\"}\n```"},
		{"prose prefixed", `Based on the task, here is the working method: {"contract":"` + method + `"}`},
	} {
		t.Run(shape.name, func(t *testing.T) {
			graph := contractFixture()
			usage, err := Contracts(context.Background(), &wrappedReplyClient{reply: shape.reply}, graph, nil)
			if err != nil {
				t.Fatalf("Contracts(): %v", err)
			}
			if usage.Calls != 1 {
				t.Fatalf("calls = %d, want 1", usage.Calls)
			}
			if got := graph.Nodes[0].Contract; got != method {
				t.Fatalf("contract = %q, want %q", got, method)
			}
		})
	}
}

// The prompt has to ask for what the parser now tolerates, so the tolerance is
// the safety net rather than the plan.
func TestContractPromptDemandsABareJSONObject(t *testing.T) {
	for _, phrase := range []string{"bare JSON object", `{"contract"`, "no code fence"} {
		if !strings.Contains(contractPrompt, phrase) {
			t.Fatalf("the contract prompt never says %q", phrase)
		}
	}
}

type wrappedReplyClient struct {
	reply string
}

func (c *wrappedReplyClient) CompleteWithMessages(_ context.Context, _ []ai.Message, _ ...ai.Option) (*ai.Response, error) {
	return response(c.reply), nil
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

// Fifth appearance of the transcript demand, first with the located cause:
// "confirm all tests pass" became "report the full output" when the method
// was written, and the gate then correctly enforced a requirement the person
// never made. The law lives where the laundering happened.
func TestTheMethodWriterMayNotEscalateAConfirmationIntoATranscript(t *testing.T) {
	for _, required := range []string{
		"satisfied by the fact of the\nresult",
		"never escalates that into reporting the raw output",
		"every demand you write\nbecomes a requirement the person never made",
	} {
		if !strings.Contains(contractPrompt, required) {
			t.Errorf("the contract prompt lost the no-escalation law: %q", required)
		}
	}
}
