package main

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/aforge-v2/internal/exec"
	"github.com/Agent-Field/aforge-v2/internal/store"
	"github.com/Agent-Field/agentfield/sdk/go/ai"
)

// lessonClient stands in for the worker model and
// keeps every message list it was shown, so a test can ask what the leaf read.
type lessonClient struct {
	reply string
	seen  [][]ai.Message
}

func (c *lessonClient) CompleteWithMessages(_ context.Context, messages []ai.Message, _ ...ai.Option) (*ai.Response, error) {
	copied := make([]ai.Message, len(messages))
	copy(copied, messages)
	c.seen = append(c.seen, copied)
	text := c.reply
	if text == "" {
		text = "done"
	}
	return &ai.Response{Choices: []ai.Choice{{
		Message:      ai.Message{Role: "assistant", Content: []ai.ContentPart{{Type: "text", Text: text}}},
		FinishReason: "stop",
	}}, Usage: &ai.Usage{PromptTokens: 10, CompletionTokens: 5}}, nil
}

func (c *lessonClient) Model() string { return "test/model" }

func (c *lessonClient) prompt(t *testing.T) string {
	t.Helper()
	if len(c.seen) == 0 {
		t.Fatal("the worker was never called")
	}
	var text strings.Builder
	for _, message := range c.seen[0] {
		for _, part := range message.Content {
			text.WriteString(part.Text)
			text.WriteByte('\n')
		}
	}
	return text.String()
}

// Journey 23, end to end, in the shape the live harness runs it: a lesson is
// taught in the thread, the next job is commissioned, and the lesson has to be
// in front of the worker doing it.
//
// It was captured correctly and then went nowhere. The chain has four links —
// the head writes the fact, retrieval finds it, injection puts it in the leaf's
// inputs, rendering gives it weight — and three of them were broken at once.
// Retrieval ran only its scope-cue arm, and a brief with a file path in it spent
// every slot on repo-scoped lessons before reaching the `user` shelf where the
// new one sat. If it did survive that, it arrived as one undistinguished bullet
// among eight, in the same voice as a distilled guess.
func TestAStatedLessonReachesTheNextJobsWorker(t *testing.T) {
	graph, err := store.Open(filepath.Join(t.TempDir(), "graph.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer graph.Close()

	// Month three: the shelves a real brief cues into are already full.
	for index := range 12 {
		if _, err := graph.RecordFactFrom(store.FactWriterDistiller, "", "repo:/src", store.FactLesson,
			"the /src build wants a clean module cache, note "+string(rune('a'+index))); err != nil {
			t.Fatal(err)
		}
	}
	for index := range 12 {
		if _, err := graph.RecordFactFrom(store.FactWriterDistiller, "", "user", store.FactPreference,
			"they read deliverables on a narrow screen, note "+string(rune('a'+index))); err != nil {
			t.Fatal(err)
		}
	}

	// 1. The lesson is taught in the thread. The front desk's own tests cover
	//    the routing that produces this write; what it produces is a fact in the
	//    head's voice, which is exactly what lands here.
	const lesson = "always run what you build once before telling me it's done"
	if _, err := graph.PostMessage(store.Message{
		SessionID: "s1", Role: store.RoleUser, Body: lesson,
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := graph.RecordFactFrom(store.FactWriterHead, store.RootID, "user",
		store.FactPreference, lesson); err != nil {
		t.Fatal(err)
	}
	if !factRecorded(t, graph, lesson) {
		t.Fatal("the lesson was never written into the notebook")
	}

	// 2. The very next job is commissioned, about something else entirely.
	if err := graph.Splice(store.RootID, store.Subtree{Nodes: []store.NodeSpec{{
		ID:    "job",
		Brief: "add a --version subcommand to the binary in /src and make sure it works",
		Title: "Add --version",
		Stage: 1,
	}}}, store.Provenance{Origin: store.OriginUser, SessionID: "s1",
		Intent: "add a --version subcommand to the binary in /src"}); err != nil {
		t.Fatal(err)
	}
	node, found, err := graph.Node("job")
	if err != nil || !found {
		t.Fatalf("node: found=%t err=%v", found, err)
	}

	// 3. The leaf is assembled and run exactly as the runner assembles it.
	worker := &lessonClient{}
	space, err := exec.NewWorkspace(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	linear := exec.NewLinear(worker, space, nil, 2, 100_000, time.Minute)
	if _, err := linear.Run(context.Background(), exec.Task{
		NodeID: 1, StoreNodeID: node.ID, Title: node.Title,
		Brief: node.Brief, Goal: node.Provenance.Intent,
		Inputs: leafNotebookInputs(graph, node),
	}); err != nil {
		t.Fatal(err)
	}

	prompt := worker.prompt(t)
	if !strings.Contains(prompt, lesson) {
		t.Fatalf("the lesson never reached the worker doing the next job:\n%s", prompt)
	}
	// Weight, not merely presence. A line the person said in their own voice is
	// marked as theirs and leads the block; the failure mode this whole journey
	// measures is a lesson that arrived and was read past.
	if !strings.Contains(prompt, "(they told you this) "+lesson) {
		t.Fatalf("the lesson arrived unmarked, in the same voice as a distilled guess:\n%s", prompt)
	}
	digest := notebookBlock(prompt)
	if index := strings.Index(digest, lesson); index < 0 || index > len(digest)/2 {
		t.Fatalf("the lesson was buried in the back half of the notebook block:\n%s", digest)
	}
	// 4. The injection is journalled against the node, so the fact's own
	// outcome record can later say this belief rode into real work.
	outcomes, err := graph.FactOutcomes()
	if err != nil {
		t.Fatal(err)
	}
	seq := factSeq(t, graph, lesson)
	if outcomes[seq].Rides == 0 {
		t.Fatalf("the injection was never recorded against the job: %+v", outcomes[seq])
	}
}

// notebookBlock slices the notebook block out of a rendered worker prompt.
func notebookBlock(prompt string) string {
	start := strings.Index(prompt, "notebook (lessons from earlier work")
	if start < 0 {
		return prompt
	}
	rest := prompt[start:]
	if end := strings.Index(rest, "=== from"); end > 0 {
		return rest[:end]
	}
	return rest
}

func factRecorded(t *testing.T, graph *store.Store, body string) bool {
	t.Helper()
	facts, err := graph.ActiveFacts("user", 200)
	if err != nil {
		t.Fatal(err)
	}
	for _, fact := range facts {
		if fact.Body == body {
			return true
		}
	}
	return false
}

func factSeq(t *testing.T, graph *store.Store, body string) int64 {
	t.Helper()
	facts, err := graph.ActiveFacts("user", 200)
	if err != nil {
		t.Fatal(err)
	}
	for _, fact := range facts {
		if fact.Body == body {
			return fact.Seq
		}
	}
	t.Fatalf("no active fact with body %q", body)
	return 0
}
