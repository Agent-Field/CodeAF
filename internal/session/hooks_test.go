package session

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"testing"

	"github.com/Agent-Field/aforge-v2/internal/approval"
	"github.com/Agent-Field/agentfield/sdk/go/ai"
)

// ── the harness ─────────────────────────────────────────────────────────────

// planeNames is one list of citizens, by name, in registration order.
func planeNames[T interface{ Name() string }](hooks []T) []string {
	names := make([]string, 0, len(hooks))
	for _, hook := range hooks {
		names = append(names, hook.Name())
	}
	return names
}

func sameNames(got, want []string) bool {
	if len(got) != len(want) {
		return false
	}
	for index := range got {
		if got[index] != want[index] {
			return false
		}
	}
	return true
}

// recorder is a test citizen of all four hooks: it writes down that it ran and
// changes nothing.
type recorder struct {
	name string

	mu   sync.Mutex
	ran  []string
	veto bool
}

func (r *recorder) Name() string { return r.name }

func (r *recorder) mark(where string) {
	r.mu.Lock()
	r.ran = append(r.ran, where)
	r.mu.Unlock()
}

func (r *recorder) saw(where string) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, seen := range r.ran {
		if seen == where {
			return true
		}
	}
	return false
}

func (r *recorder) EpisodeInit(*episode)                  { r.mark("init") }
func (r *recorder) PreDecision(context.Context, *episode) { r.mark("decision") }
func (r *recorder) PostFeedback(context.Context, *episode, *eventHub, []ai.ToolCall, []toolResult, bool) {
	r.mark("feedback")
}

func (r *recorder) PreAction(_ context.Context, _ *episode, _ *eventHub, call ai.ToolCall) (ai.ToolCall, toolResult, bool) {
	r.mark("action")
	if r.veto {
		return call, refusal("vetoed by " + r.name), false
	}
	return call, toolResult{}, true
}

// ── the registry ────────────────────────────────────────────────────────────

// The four mechanisms that existed before the plane are its four citizens, and
// the two orders that matter are the two the plane's own doc claims: the gate
// runs before the ledger at pre-action, the ledger runs before the detector at
// post-feedback.
func TestTheControlPlaneRegistersTheFourMechanismsInOrder(t *testing.T) {
	agent, _ := newTestAgent(t, &scriptedCompleter{}, nil)
	plane := agent.controlPlaneFor()

	for _, expected := range []struct {
		hook string
		got  []string
		want []string
	}{
		// The error→fix lane is the third piece of turn state (fixrecall.go), and
		// the process rules the loop enforces are the fourth (processrule.go).
		// episode-init is the one hook whose order carries no argument: every
		// citizen there writes its own field on a struct nobody has read yet.
		// The write seam sits beside the change ledger for both of its hooks: it
		// asks the same question of the same calls and keeps a different answer
		// (writeseam.go).
		{"episode-init", planeNames(plane.episodeInit), []string{"changes", "writes", "loop", "fixes", "process-rules"}},
		{"pre-decision", planeNames(plane.preDecision), []string{"stub", "turn-fold"}},
		// The write scope and the tree claim are the two citizens that are inert
		// for an ordinary agent: both are registered on every plane and refuse
		// nothing until an agent is built with a scope (orchestrate.go), or some
		// node is running in a tree this agent is writing in (treehold.go). The
		// claim comes after the scope because the scope is about the writer and
		// the claim is about everybody else.
		//
		// AND THE GROUND COMES BEFORE THE GIT GUARD, which is the one order in
		// this list that a person would notice being wrong: the git guard's
		// sentences are about a task's OWN copy, so a command aimed at another
		// directory has to meet the path law first or be refused with a
		// paragraph that is false about every path in it (taskoutside.go).
		{"pre-action", planeNames(plane.preAction), []string{"approval", "changes", "write-scope", "tree-claim", "task-ground", "task-git"}},
		{"post-feedback", planeNames(plane.postFeedback), []string{"changes", "writes", "loop"}},
	} {
		if !sameNames(expected.got, expected.want) {
			t.Errorf("%s citizens = %v, want %v", expected.hook, expected.got, expected.want)
		}
	}
}

// A citizen is registered once and appears in every list it satisfies.
func TestRegisteringOneCitizenFillsEveryHookItImplements(t *testing.T) {
	plane := &controlPlane{}
	plane.register(&recorder{name: "all-four"})

	if len(plane.episodeInit) != 1 || len(plane.preDecision) != 1 ||
		len(plane.preAction) != 1 || len(plane.postFeedback) != 1 {
		t.Fatalf("a four-hook citizen landed in %d/%d/%d/%d lists, want one each",
			len(plane.episodeInit), len(plane.preDecision), len(plane.preAction), len(plane.postFeedback))
	}
	// And an object that implements nothing is a silent no-op, not a panic.
	plane.register(struct{}{})
}

// episode-init is what opens the turn's state. Nothing else may assume it.
func TestEpisodeInitOpensTheTurnsState(t *testing.T) {
	agent, _ := newTestAgent(t, &scriptedCompleter{}, nil)
	episode := agent.newEpisode()

	if episode.watch == nil {
		t.Error("episode-init left the loop window unopened")
	}
	if episode.changes == nil {
		t.Error("episode-init left the change ledger unopened")
	}
	if changes := episode.changes.list(); len(changes) != 0 {
		t.Errorf("a fresh episode already knows about %d changes", len(changes))
	}
	if episode.fixes == nil {
		t.Error("episode-init left the error→fix lane unopened")
	}
	if _, waiting := episode.fixes.peek("bash"); waiting {
		t.Error("a fresh episode already has a failure waiting to be answered")
	}
}

// The first veto ends the chain: a call somebody has already refused is not a
// call the next citizen has an opinion about.
func TestThePreActionChainStopsAtTheFirstVeto(t *testing.T) {
	agent, _ := newTestAgent(t, &scriptedCompleter{}, nil)
	gate, after := &recorder{name: "gate", veto: true}, &recorder{name: "after"}
	plane := &controlPlane{}
	plane.register(gate)
	plane.register(after)
	episode := &episode{agent: agent, plane: plane}

	call := ai.ToolCall{ID: "c1", Function: ai.ToolCallFunction{Name: "read", Arguments: "{}"}}
	_, refused, allowed := episode.preAction(context.Background(), nil, call)
	if allowed {
		t.Fatal("a vetoing citizen did not stop the call")
	}
	if !strings.Contains(refused.text, "vetoed by gate") || !refused.isError {
		t.Fatalf("the refusal the model reads is %+v, want the vetoing citizen's words", refused)
	}
	if after.saw("action") {
		t.Fatal("the chain kept running after a veto")
	}
}

// ── each legacy mechanism, through its hook ─────────────────────────────────

// THE STUB PASS, through pre-decision: the same pass, at the same moment, with
// the same result — an old heavy result becomes a pointer, a recent one does
// not.
func TestTheStubPassRunsThroughPreDecision(t *testing.T) {
	agent, _ := newTestAgent(t, &scriptedCompleter{}, nil)
	heavy := heavyOutput("HOOKED")

	agent.mu.Lock()
	for turn := 1; turn <= 6; turn++ {
		agent.messages = append(agent.messages,
			textMessage("user", fmt.Sprintf("turn %d", turn)),
			ai.Message{Role: "assistant", ToolCalls: []ai.ToolCall{{
				ID: fmt.Sprintf("c%d", turn), Function: ai.ToolCallFunction{Name: "fat", Arguments: "{}"},
			}}},
			ai.Message{Role: "tool", ToolCallID: fmt.Sprintf("c%d", turn),
				Content: []ai.ContentPart{{Type: "text", Text: heavy}}},
		)
	}
	agent.mu.Unlock()

	agent.newEpisode().preDecision(context.Background())

	texts := toolTexts(agent)
	if len(texts) != 6 {
		t.Fatalf("tool messages: got %d, want 6", len(texts))
	}
	if !strings.HasPrefix(texts[0], stubMarker) || !strings.HasPrefix(texts[1], stubMarker) {
		t.Fatalf("the pre-decision hook did not stub the two old results: %.60q / %.60q", texts[0], texts[1])
	}
	if texts[5] != heavy {
		t.Fatalf("the newest result was stubbed through the hook: %.60q", texts[5])
	}
}

// THE APPROVAL GATE, through pre-action: a denied call is vetoed, in the
// policy's own words, and the model reads the refusal it always read.
func TestTheApprovalGateRunsThroughPreAction(t *testing.T) {
	agent, _ := newTestAgent(t, &scriptedCompleter{}, func(config *Config) {
		config.ApprovalPolicy = &approval.Policy{
			Default: approval.ActionAllow,
			Tools:   map[string]approval.Action{"write": approval.ActionDeny},
		}
	})
	episode := agent.newEpisode()
	call := ai.ToolCall{ID: "c1", Function: ai.ToolCallFunction{Name: "write", Arguments: `{"path":"a.md"}`}}

	_, refused, allowed := episode.preAction(context.Background(), nil, call)
	if allowed {
		t.Fatal("a denied call passed pre-action")
	}
	if !strings.HasPrefix(refused.text, "denied by approval rule:") {
		t.Fatalf("refusal = %q, want the gate's own wording", refused.text)
	}

	// An allowed call passes with nothing to say, and the call that runs is the
	// call that was asked for: today's citizens canonicalize nothing.
	read := ai.ToolCall{ID: "c2", Function: ai.ToolCallFunction{Name: "read", Arguments: `{"path":"a.md"}`}}
	running, _, allowed := episode.preAction(context.Background(), nil, read)
	if !allowed {
		t.Fatal("an allowed call was vetoed at pre-action")
	}
	if running.Function.Arguments != read.Function.Arguments {
		t.Fatalf("pre-action rewrote a call nobody asked it to: %q", running.Function.Arguments)
	}
}

// THE GUARDIAN, through pre-action: still inside the gate, still able to do
// exactly one thing — turn a prompt into an allow.
func TestTheGuardianStillAnswersInsidePreAction(t *testing.T) {
	completer := &guardianCompleter{inner: &scriptedCompleter{}, verdict: "ALLOW"}
	agent, _ := newTestAgent(t, completer, func(config *Config) {
		config.ApprovalPolicy = promptAll()
		config.AskConsent = true
		config.Guardian = true
	})
	episode := agent.newEpisode()
	call := ai.ToolCall{ID: "c1", Function: ai.ToolCallFunction{Name: "write", Arguments: `{"path":"a.md","content":"x"}`}}

	hub := newEventHub()
	events := hub.subscribe()
	_, _, allowed := episode.preAction(context.Background(), hub, call)
	hub.close()
	if !allowed {
		t.Fatal("the guardian's ALLOW did not open the gate through pre-action")
	}
	if len(completer.questions()) != 1 {
		t.Fatalf("the guardian was asked %d times, want 1", len(completer.questions()))
	}
	var vouched bool
	for event := range events {
		if event.Kind == EventGuardianAllowed {
			vouched = true
		}
	}
	if !vouched {
		t.Fatal("the guardian allowed a call and said nothing about it")
	}
}

// THE LOOP DETECTOR, through post-feedback: the third identical call raises the
// event and drops the note, from the hook rather than from a line in the turn.
func TestTheLoopDetectorRunsThroughPostFeedback(t *testing.T) {
	agent, _ := newTestAgent(t, &scriptedCompleter{}, nil)
	episode := agent.newEpisode()
	call := ai.ToolCall{ID: "c1", Function: ai.ToolCallFunction{Name: "touch", Arguments: `{"path":"a.md"}`}}
	results := []toolResult{{text: "fine"}}

	hub := newEventHub()
	events := hub.subscribe()
	for attempt := 1; attempt <= 3; attempt++ {
		episode.postFeedback(context.Background(), hub, []ai.ToolCall{call}, results, true)
	}
	hub.close()

	nudges := 0
	for event := range events {
		if event.Kind == EventNudge {
			nudges++
		}
	}
	if nudges != 1 {
		t.Fatalf("nudges through the hook: got %d, want 1", nudges)
	}
	agent.mu.Lock()
	queued := len(agent.steering)
	agent.mu.Unlock()
	if queued != 1 {
		t.Fatalf("steering notes queued: got %d, want 1", queued)
	}
}
