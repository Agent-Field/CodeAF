package plan

import (
	"context"
	"strings"
	"sync"
	"testing"

	"github.com/Agent-Field/agentfield/sdk/go/ai"
)

// stagePlanner answers the three calls an expansion can make and counts them, so
// a test can assert both the shape that came out and what it cost.
type stagePlanner struct {
	parts  string // the fan-out's reply
	stages string // the stage question's reply
	sizes  string // every sizing call's reply

	mutex  sync.Mutex
	passes []string
}

func (c *stagePlanner) CompleteWithMessages(_ context.Context, messages []ai.Message, _ ...ai.Option) (*ai.Response, error) {
	var system string
	for _, message := range messages {
		if message.Role == "system" {
			system += textOf(message)
		}
	}
	c.mutex.Lock()
	defer c.mutex.Unlock()
	switch {
	case strings.Contains(system, "You list the parts of one stage"):
		c.passes = append(c.passes, "fanout")
		return response(c.parts), nil
	case strings.Contains(system, "You break one piece of work into the ordered stages"):
		c.passes = append(c.passes, "stages")
		return response(c.stages), nil
	default:
		c.passes = append(c.passes, "size")
		return response(c.sizes), nil
	}
}

func (c *stagePlanner) count(pass string) int {
	c.mutex.Lock()
	defer c.mutex.Unlock()
	seen := 0
	for _, made := range c.passes {
		if made == pass {
			seen++
		}
	}
	return seen
}

// oversizedGraph is the shape #384 was reported from: one node the ruler put
// past one worker's reach, whose inside the fan-out will hand straight back.
func oversizedGraph() (*Graph, int) {
	graph := &Graph{Goal: "carry the whole thing to an end", Stages: []Stage{{Title: "Work"}}, NextID: 1}
	id := graph.Add(Node{Stage: 1, Title: "Work", Summary: "Carry the whole thing to an end",
		Size: SizeOversized, Parts: []string{"one", "two"}})
	return graph, id
}

// THE LAW. An oversized node whose fan-out gives it straight back is divided
// into the ordered stages it is made of, and the chain is spliced with the
// waiting the stages stated.
func TestAnOversizedNodeThatCannotRunAtOnceIsDividedIntoStages(t *testing.T) {
	client := &stagePlanner{
		parts:  `{"parts":[{"title":"Work","summary":"Carry the whole thing to an end"}]}`,
		stages: `{"stages":[{"title":"Read","summary":"Read what is there.","needs":[]},{"title":"Change","summary":"Make the change.","needs":[1]},{"title":"Prove","summary":"Show it holds.","needs":[2]}]}`,
		sizes:  `{"sizes":[{"node":1,"size":"atomic","split_into":[]},{"node":2,"size":"atomic","split_into":[]},{"node":3,"size":"atomic","split_into":[]}]}`,
	}
	graph, parent := oversizedGraph()

	spliced, _, err := ExpandLevel(t.Context(), client, graph, Options{MaxDepth: 2, NodeBudget: 40})
	if err != nil {
		t.Fatalf("ExpandLevel: %v", err)
	}
	if spliced != 1 {
		t.Fatalf("spliced %d nodes, want 1 — the sequence was left whole", spliced)
	}
	links := make([]*Node, 0, 3)
	for index := range graph.Nodes {
		if node := &graph.Nodes[index]; node.Parent == parent {
			links = append(links, node)
		}
	}
	if len(links) != 3 {
		t.Fatalf("the chain has %d links, want 3", len(links))
	}
	if got := []string{links[0].Title, links[1].Title, links[2].Title}; strings.Join(got, ",") != "Read,Change,Prove" {
		t.Fatalf("links = %v, want the stages in the order they were drawn", got)
	}
	// The waiting is the whole of what a chain is: each link but the first waits
	// on the one before it, and the first waits on nothing inside the subtree.
	if len(links[0].Needs) != 0 {
		t.Errorf("the first link waits on %v, want nothing", links[0].Needs)
	}
	for index := 1; index < len(links); index++ {
		if len(links[index].Needs) != 1 || links[index].Needs[0] != links[index-1].ID {
			t.Errorf("link %d waits on %v, want only link %d", index+1, links[index].Needs, index)
		}
	}
	if node := graph.Node(parent); node.Kind != KindSynthesis || node.Undivided != "" {
		t.Errorf("the divided node is %q with undivided %q, want a gathering node with no refusal", node.Kind, node.Undivided)
	}
	// The discipline: the ordinary node's two calls, spent differently.
	if made := client.count("fanout") + client.count("stages") + client.count("size"); made != 3 {
		t.Errorf("the expansion cost %d calls (%v), want the fan-out, the stage question and one sizing pass", made, client.passes)
	}
}

// A stage answer that comes back as one stage is the node in different words,
// and that refusal already has its wording. Nothing is sized: the single link's
// own title has already said what a sizing call would be paid to say.
func TestAStageAnswerOfOneStageIsTheOnePieceRefusal(t *testing.T) {
	client := &stagePlanner{
		parts:  `{"parts":[{"title":"Work","summary":"Carry the whole thing to an end"}]}`,
		stages: `{"stages":[{"title":"Work","summary":"Carry the whole thing to an end.","needs":[]}]}`,
	}
	graph, parent := oversizedGraph()

	spliced, _, err := ExpandLevel(t.Context(), client, graph, Options{MaxDepth: 2, NodeBudget: 40})
	if err != nil {
		t.Fatalf("ExpandLevel: %v", err)
	}
	if spliced != 0 {
		t.Fatalf("spliced %d nodes from a one-stage answer, want 0", spliced)
	}
	if node := graph.Node(parent); node.Undivided != RefusalOnePiece {
		t.Errorf("undivided = %q, want %q", node.Undivided, RefusalOnePiece)
	}
	if made := client.count("size"); made != 0 {
		t.Errorf("a refused chain bought %d sizing calls, want none", made)
	}
}

// Stages that wait for nothing are not a sequence. The spine's own levelling
// says so, and what it folds into one stage the acceptance check then refuses in
// the words it already has.
func TestStagesThatWaitForNothingAreNotASequence(t *testing.T) {
	client := &stagePlanner{
		parts:  `{"parts":[{"title":"Work","summary":"Carry the whole thing to an end"}]}`,
		stages: `{"stages":[{"title":"Read","summary":"Read what is there.","needs":[]},{"title":"Write","summary":"Write it up.","needs":[]}]}`,
	}
	graph, parent := oversizedGraph()

	if _, _, err := ExpandLevel(t.Context(), client, graph, Options{MaxDepth: 2, NodeBudget: 40}); err != nil {
		t.Fatalf("ExpandLevel: %v", err)
	}
	if node := graph.Node(parent); node.Undivided != RefusalOnePiece {
		t.Errorf("undivided = %q, want %q", node.Undivided, RefusalOnePiece)
	}
}

// The second move is the second question and never the first: a node whose
// fan-out found real width is sized and spliced exactly as it always was, and
// the ordinary node pays nothing new.
func TestTheStageQuestionIsOnlyAskedWhenTheFanOutGaveBackOnePiece(t *testing.T) {
	client := &stagePlanner{
		parts: `{"parts":[{"title":"Berlin","summary":"Profile Berlin."},{"title":"Paris","summary":"Profile Paris."}]}`,
		sizes: `{"sizes":[{"node":1,"size":"atomic","split_into":[]},{"node":2,"size":"atomic","split_into":[]}]}`,
	}
	graph, _ := oversizedGraph()

	spliced, _, err := ExpandLevel(t.Context(), client, graph, Options{MaxDepth: 2, NodeBudget: 40})
	if err != nil {
		t.Fatalf("ExpandLevel: %v", err)
	}
	if spliced != 1 {
		t.Fatalf("spliced %d nodes, want 1", spliced)
	}
	if made := client.count("stages"); made != 0 {
		t.Errorf("a node with named width bought %d stage questions, want none", made)
	}
}

// A node already within one worker's reach is never cut into stages: being
// stopped part-way is the whole reason to cut a sequence, and it has not been.
func TestANodeWithinReachIsNeverCutIntoStages(t *testing.T) {
	for _, size := range []Size{SizeAtomic, SizeUnknown} {
		if dividesInTime(&Node{Size: size}) {
			t.Errorf("a %q node was offered the stage question", size)
		}
	}
	for _, size := range []Size{SizeOversized, SizeBorderline} {
		if !dividesInTime(&Node{Size: size}) {
			t.Errorf("a %q node was refused the stage question", size)
		}
	}
}

// The prompt is the product here. It states the burden in the words the sizing
// pass states it in, it states its own ceiling, it carries the three shared
// rules rather than a second grammar for them, and it names no domain.
func TestTheStagePromptAsksTheSizePromptsSecondQuestion(t *testing.T) {
	for _, want := range []string{
		"cannot be brought to an end inside what one worker can hold",
		"never more than " + sequenceDepthWord,
		"Do\nnot add a final merge, synthesis or summary stage",
		"Return a single stage when the piece has no order inside it",
		titleRule,
		proportionRule,
		verdictRule,
		agentPremise,
	} {
		if !strings.Contains(sequencePrompt, want) {
			t.Errorf("the stage prompt no longer says %q", want)
		}
	}
	// The burden is quoted from the ruler, not paraphrased beside it.
	if !strings.Contains(sizePromptWith(sizeAnchors), "brought to an end inside what one worker can hold") {
		t.Error("the sizing prompt no longer states the burden the stage prompt discharges")
	}
	// The ceiling the prompt states is the room the reply is given.
	if sequenceDepthWord != "4" || sequenceDepth != 4 {
		t.Errorf("the stated ceiling %q and the derived one %d have drifted apart", sequenceDepthWord, sequenceDepth)
	}
}

// A chain drawn by the second move is not undone by the one-sitting collapse.
// The shape is what guarantees it: a divided node leaves a gathering parent over
// links at depth 1, and the collapse folds only work nodes at depth 0.
func TestASplicedChainIsNotCollapsedBackIntoOneSitting(t *testing.T) {
	client := &stagePlanner{
		parts:  `{"parts":[{"title":"Work","summary":"Carry the whole thing to an end"}]}`,
		stages: `{"stages":[{"title":"Read","summary":"Read what is there.","needs":[]},{"title":"Change","summary":"Make the change.","needs":[1]}]}`,
		sizes:  `{"sizes":[{"node":1,"size":"atomic","split_into":[]},{"node":2,"size":"atomic","split_into":[]}]}`,
	}
	graph, _ := oversizedGraph()
	if _, _, err := ExpandLevel(t.Context(), client, graph, Options{MaxDepth: 2, NodeBudget: 40}); err != nil {
		t.Fatalf("ExpandLevel: %v", err)
	}
	if folded := collapseAtomicChain(graph); folded != 0 {
		t.Fatalf("the one-sitting collapse folded %d nodes of a spliced chain — the division was undone", folded)
	}
	if len(graph.Nodes) != 3 {
		t.Fatalf("the graph has %d nodes, want the gathering node over two links", len(graph.Nodes))
	}
}
