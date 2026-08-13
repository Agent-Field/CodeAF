package resident

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/aforge-v2/internal/craft"
	"github.com/Agent-Field/aforge-v2/internal/store"
)

// overlapShelf is a matcher that rewards a request for saying what a workflow
// is FOR, and cannot tell whose sentence said it.
//
// That second half is the whole point of these tests and the whole of the
// defect: BM25 is a function of words, not of authorship, so a request string
// built by pasting a room's transcript under a person's sentence scores as
// whatever the room was about. Scripting a fixed score would prove nothing here
// — the question is not what the shelf does with a request, it is WHICH STRING
// the shelf is handed.
type overlapShelf struct {
	workflow *craft.Workflow
	requests []string
}

func (s *overlapShelf) Match(request string, k int) []craft.Scored {
	s.requests = append(s.requests, request)
	asked := map[string]bool{}
	for _, word := range strings.Fields(strings.ToLower(craftWords(request))) {
		asked[word] = true
	}
	shared := 0.0
	for _, word := range strings.Fields(strings.ToLower(craftWords(s.workflow.Name))) {
		if asked[word] {
			shared++
		}
	}
	if shared == 0 {
		return nil
	}
	return []craft.Scored{{
		Summary: craft.Summary{Name: s.workflow.Name, Description: s.workflow.Description},
		Score:   shared * craft.MatchFloor,
	}}
}

func (s *overlapShelf) Load(name string) (*craft.Workflow, error) { return s.workflow, nil }
func (s *overlapShelf) List() ([]craft.Summary, error)            { return nil, nil }

func (s *overlapShelf) History(string, int) ([]craft.Version, error) { return nil, nil }

func (s *overlapShelf) Save(*craft.Workflow, string) (string, error) { return "cafe1234", nil }

// dagCraft is the learned workflow the incident's room had been teaching all
// morning. Its own words are the words the conversation was full of, which is
// exactly why it won a match it had no business winning.
func dagCraft() *craft.Workflow {
	return &craft.Workflow{
		Name:        "multi-level-dag",
		Commit:      "beef5678",
		Description: "launch a multi level dag task on agentfield using {{model}}",
		Params:      []craft.Param{{Name: "model", Required: true}},
		Steps: []craft.Step{
			{ID: "shape", Brief: "Shape the levels of the dag."},
			{ID: "launch", Brief: "Launch it on agentfield using {{model}}.", Needs: []string{"shape"}},
		},
		Limits: craft.Limits{CostUSD: 1.00, WallClock: 15 * time.Minute},
	}
}

// theRoomWasAboutDAGs is the conversation the incident's ask came out of: ten
// turns of the workflow's own vocabulary, and not one word of what the person
// then asked for.
const theRoomWasAboutDAGs = `them: launch a multi level dag task on agentfield using qwen max
you, earlier: the dag is on agentfield now, running on qwen max
them: add another level to the dag on agentfield
you, earlier: the multi level dag is launched, qwen max on every level
them: launch one more multi level dag task on agentfield using qwen max`

// recordingFiller answers no holes and remembers what it was asked to read
// them out of. The param filler is a model seam, and what a model is shown is
// the only thing that decides what it extracts.
type recordingFiller struct{ saw []string }

func (f *recordingFiller) fill(_ context.Context, instruction string, workflow *craft.Workflow) (map[string]string, error) {
	f.saw = append(f.saw, instruction)
	// Every hole answered, so the compile never fails for a reason these tests
	// are not about. What is under test is the STRING above, not the values.
	filled := make(map[string]string, len(workflow.Params))
	for _, param := range workflow.Params {
		filled[param.Name] = "whatever was asked for"
	}
	return filled, nil
}

// THE INCIDENT (owner's journal, 2026-08-12), reconstructed.
//
// "lets start a research on understanding jepa models help me think through it"
// was typed into a room that had spent the morning on multi-level DAGs. The
// conversation travelled with the task, in the same string, and every part of
// recognition read that string as the person's own words: the matcher scored the
// learned multi-level-dag workflow decisively on the CONTEXT, the subject check
// passed on the same words, and the filler pulled `model=jepa` out of the only
// two words of the ask it could see. The person got a DAG job instead of an
// answer about JEPA.
//
// Recognition is handed the ASK now, so it misses, and a miss is an ordinary
// plan — which is what "help me think through it" always deserved.
func TestAConversationFullOfACraftsWordsDoesNotRecognizeThatCraft(t *testing.T) {
	graph := openStore(t)
	shelf := &overlapShelf{workflow: dagCraft()}
	filler := &recordingFiller{}
	mind := NewCraftMind(shelf, "/home/craft", filler.fill, nil)

	_, planned := craftSpliceCommand(t, graph, mind, store.Command{
		SessionID:   "jepa",
		Kind:        store.CommandSplice,
		Instruction: "lets start a research on understanding jepa models help me think through it",
		Context:     theRoomWasAboutDAGs,
	})

	if !planned {
		t.Fatal("the room's own vocabulary recognized a craft the person never asked for")
	}
	if len(shelf.requests) != 1 {
		t.Fatalf("the shelf was asked %d times, want once", len(shelf.requests))
	}
	request := shelf.requests[0]
	if !strings.Contains(request, "jepa models") {
		t.Fatalf("recognition was not handed the ask: %q", request)
	}
	// The load-bearing assertion. Not "it scored low" — that is a property of
	// this scorer — but that not one byte of the conversation reached the
	// matcher at all.
	for _, leaked := range []string{"agentfield", "qwen", "multi level dag"} {
		if strings.Contains(strings.ToLower(request), leaked) {
			t.Fatalf("the conversation reached the matcher (%q):\n%s", leaked, request)
		}
	}
	if len(filler.saw) != 0 {
		t.Fatalf("the param filler ran on a craft that was never recognized: %q", filler.saw)
	}
}

// The mirror, and the reason the fix is a separation rather than a smaller
// window: the SAME words, said by the person as the ask, are the request the
// shelf exists to answer. Recognition is not being made harder — it is being
// pointed at the sentence whose author asked for the work.
func TestTheSameWordsInTheAskDoRecognizeTheCraft(t *testing.T) {
	graph := openStore(t)
	shelf := &overlapShelf{workflow: dagCraft()}
	filler := &recordingFiller{}
	mind := NewCraftMind(shelf, "/home/craft", filler.fill, nil)

	_, planned := craftSpliceCommand(t, graph, mind, store.Command{
		SessionID:   "dag",
		Kind:        store.CommandSplice,
		Instruction: "launch a multi level dag task on agentfield using qwen max",
		Context:     "them: how did the jepa reading go\nyou, earlier: still thinking it through",
	})

	if planned {
		t.Fatal("the ask said what the craft is for and the planner was reached anyway")
	}
	if len(shelf.requests) != 1 || strings.Contains(shelf.requests[0], "jepa") {
		t.Fatalf("recognition was handed something other than the ask: %q", shelf.requests)
	}
}

// The param filler is a model seam, so the only guard on what it extracts is
// what it is shown. Shown the conversation, it answers about the conversation:
// the incident's `model=jepa` came from a filler reading a DAG workflow's holes
// out of a sentence about JEPA, glued under five turns about Qwen Max.
func TestParamFillingReadsTheAskAndNotTheConversation(t *testing.T) {
	graph := openStore(t)
	shelf := &overlapShelf{workflow: dagCraft()}
	filler := &recordingFiller{}
	mind := NewCraftMind(shelf, "/home/craft", filler.fill, nil)

	craftSpliceCommand(t, graph, mind, store.Command{
		SessionID:   "dag",
		Kind:        store.CommandSplice,
		Instruction: "launch a multi level dag task on agentfield using qwen max",
		Context:     theRoomWasAboutDAGs + "\nthem: and read up on jepa models while you're at it",
	})

	if len(filler.saw) != 1 {
		t.Fatalf("the filler ran %d times, want once", len(filler.saw))
	}
	if seen := filler.saw[0]; strings.Contains(seen, "jepa") || strings.Contains(seen, "them:") {
		t.Fatalf("the filler was shown the conversation: %q", seen)
	}
}

// The compiler is the one reader entitled to both halves — the conversation
// travels precisely so planning is well-informed, and a planner that never saw
// it would be a worker asking again for everything the person already settled.
func TestTheCompilerReadsTheAskAndTheConversationBoth(t *testing.T) {
	graph := openStore(t)
	command, err := graph.RequestCommand(store.Command{
		SessionID:   "design",
		Kind:        store.CommandSplice,
		Instruction: "go and build the exporter",
		Context:     "them: the export keeps the column order from the source",
	})
	if err != nil {
		t.Fatalf("request command: %v", err)
	}

	var briefed string
	compile := func(_ context.Context, instruction, _ string) (Compiled, error) {
		briefed = instruction
		return Compiled{Goal: "Build the exporter.", Scale: "task"}, nil
	}
	plan := func(ctx context.Context, _ Compiled) (store.Subtree, error) {
		anchor, _ := PlanAnchorFromContext(ctx)
		return store.Subtree{Nodes: []store.NodeSpec{{ID: anchor.NodeID, Brief: "build it", Stage: 1}}}, nil
	}
	if err := New(graph, compile, plan).Tick(context.Background()); err != nil {
		t.Fatalf("tick: %v", err)
	}

	if !strings.HasPrefix(briefed, "go and build the exporter") {
		t.Fatalf("the ask does not lead the brief: %q", briefed)
	}
	if !strings.Contains(briefed, store.ForkedContextPrefix) {
		t.Fatalf("the conversation reached the compiler unfenced: %q", briefed)
	}
	if !strings.Contains(briefed, "keeps the column order from the source") {
		t.Fatalf("the conversation did not reach the compiler at all: %q", briefed)
	}

	// And what the work is named after is the ask alone: the node the person
	// finds on the board says what they asked for, not what the room was about.
	settled := commandBySeq(t, graph, command.Seq)
	if settled.Instruction != "go and build the exporter" {
		t.Fatalf("the settled ask = %q", settled.Instruction)
	}
	node, ok, err := graph.Node(commandPlanAnchor(settled))
	if err != nil || !ok {
		t.Fatalf("the job node: ok=%v err=%v", ok, err)
	}
	if node.Provenance.Intent != "go and build the exporter" {
		t.Fatalf("the job's intent carried the conversation: %q", node.Provenance.Intent)
	}
}
