package session

import (
	"context"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Agent-Field/aforge-v2/internal/roles"
	"github.com/Agent-Field/agentfield/sdk/go/ai"
)

// Auxiliary cancellation is recorded even when the caller's context is over.
func TestAnErrandCutByItsCallersDeadlineIsStillWrittenDown(t *testing.T) {
	path := filepath.Join(t.TempDir(), "session.jsonl")
	agent := auxiliaryRoleAgent(t, &deadCompleter{}, func(config *Config) { config.SessionFile = path })

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, _, err := agent.callRole(ctx, roles.RoleAuditor, "", []ai.Message{
		textMessage("user", "check the result"),
	}); err == nil {
		t.Fatal("an errand made on a dead context reported success")
	}

	rows := journaledErrors(t, path)
	if len(rows) == 0 {
		t.Fatal("an errand cut by its caller's deadline wrote nothing at all")
	}
	if rows[0].Role != string(roles.RoleAuditor) {
		t.Errorf("the row names %q, want the errand that made the call", rows[0].Role)
	}
	if strings.TrimSpace(rows[0].Message) == "" {
		t.Error("the row says a call failed and not one word about why")
	}
}

// AND A WEDGED FIRST RUNG LEAVES THE FALL-THROUGH TIME TO ANSWER.
//
// The caller's deadline used to bound only the context while every rung was
// still given the tier's whole patience, so one silent endpoint on rung one ate
// the caller's entire budget and the ladder's floor — the session's own model,
// alive by construction — was never asked. That is how a division review died
// on 2026-08-28: three minutes of provider silence, and the one model
// answering every other request in the session never heard the question. The
// budget is now shared across the rungs that remain, so the errand survives
// exactly one wedged endpoint, which is the failure the ladder exists for.
func TestAWedgedFirstRungLeavesTheFallThroughTimeToAnswer(t *testing.T) {
	agent := auxiliaryRoleAgent(t, &wedgedFirstRung{})

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	response, model, err := agent.callRole(ctx, roles.RoleAuditor, "the-session-model",
		[]ai.Message{textMessage("user", "check the result")})

	if err != nil {
		t.Fatalf("the errand died on one wedged rung: %v", err)
	}
	if model != "the-session-model" {
		t.Fatalf("the answer came from %q, want the fall-through rung", model)
	}
	if response == nil || response.Text() != "answered" {
		t.Fatalf("the fall-through's answer did not come back: %+v", response)
	}
}

// wedgedFirstRung sits silent for the whole of its context on the first model
// it is ever asked, and answers instantly on any other — one wedged endpoint
// and one live one, which is the shape of the measured failure.
type wedgedFirstRung struct {
	mu    sync.Mutex
	first string
}

func (w *wedgedFirstRung) CompleteWithMessages(ctx context.Context, _ []ai.Message, options ...ai.Option) (*ai.Response, error) {
	var request ai.Request
	for _, option := range options {
		_ = option(&request)
	}
	w.mu.Lock()
	if w.first == "" {
		w.first = request.Model
	}
	first := w.first
	w.mu.Unlock()
	if request.Model == first {
		<-ctx.Done()
		return nil, ctx.Err()
	}
	return textResponse("answered"), nil
}

// deadCompleter answers every request with the context's own error, which is what
// an adapter does with a request made on a context that is already over.
type deadCompleter struct{}

func (deadCompleter) CompleteWithMessages(ctx context.Context, _ []ai.Message, _ ...ai.Option) (*ai.Response, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return nil, context.Canceled
}

// modelAsked answers every request with the brief and remembers which model it
// was asked for, which is the whole of what a rung's resolution can be observed
// by from outside.
type modelAsked struct {
	mu   sync.Mutex
	seen string
}

func (m *modelAsked) CompleteWithMessages(_ context.Context, _ []ai.Message, options ...ai.Option) (*ai.Response, error) {
	var request ai.Request
	for _, option := range options {
		_ = option(&request)
	}
	m.mu.Lock()
	m.seen = request.Model
	m.mu.Unlock()
	return textResponse("the brief"), nil
}

func (m *modelAsked) model() string {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.seen
}

// AND THE FLAG REACHES A CHILD, because a session is not only the turns typed
// into it.
//
// The promise is about every text call the SESSION makes, and the nodes a turn
// hands out and the hands they lift are the session at one remove. A child that
// copied the ladder but not the flag would leave exactly the crew-only rungs
// behind — they are the only ones that need telling — so the first thing a task
// did on its own ceiling would fail the way the conversation's used to (#443).
func TestTheOneModelPromiseTravelsToATaskNodeAndItsCrewOnlyErrands(t *testing.T) {
	asked := &modelAsked{}
	agent, _ := newTestAgent(t, asked, func(config *Config) {
		config.Model = "the-one/model"
		config.RolesSource = nil
		config.OneModel = true
	})
	graph := agent.graph()
	graph.run = func(*TaskNode) {}
	id := graph.reserve()
	graph.admit(id, taskSpec{title: "t", brief: "b", acceptance: "a"})

	child, err := agent.newTaskAgent(context.Background(), t.TempDir(), graph.node(id), "")
	if err != nil {
		t.Fatalf("newTaskAgent: %v", err)
	}
	defer child.Close()
	if !child.config.OneModel {
		t.Fatal("the node did not inherit the flag, so its crew-only rungs have no model")
	}

	// AND THE INHERITED BIT IS LOAD-BEARING AND NOT DECORATION: the same
	// crew-only errand that had nowhere to call now resolves on the child.
	_, model, err := child.callRole(context.Background(), roles.RoleAuditor, "",
		[]ai.Message{textMessage("user", "check the result")})
	if err != nil {
		t.Fatalf("a crew-only errand on the node had no model to call: %v", err)
	}
	if model != child.model {
		t.Errorf("the node's errand ran on %q, want its own model %q", model, child.model)
	}
}

// auxiliaryRoleAgent supplies the live checking role with one configured model.
func auxiliaryRoleAgent(t *testing.T, completer Completer, mutate ...func(*Config)) *Agent {
	t.Helper()
	a, _ := newTestAgent(t, completer, func(c *Config) {
		c.RolesSource = tierSettings(map[string]string{roles.PinKey(roles.RoleAuditor): "auxiliary-model"})
		for _, edit := range mutate {
			edit(c)
		}
	})
	return a
}
