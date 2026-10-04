package session

import (
	"context"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Agent-Field/agentfield/sdk/go/ai"
	"github.com/Agent-Field/codeaf/internal/approval"
	procexec "github.com/Agent-Field/codeaf/internal/executor"
	"github.com/Agent-Field/codeaf/internal/preflight"
)

// formSeat remembers, for each tool call that went around it, whether the call
// was made on the setup form of the seat.
type formSeat struct {
	procexec.Stance
	setup bool
	mu    *sync.Mutex
	calls *[]bool
}

func (s formSeat) Around(_ context.Context, _ procexec.Call, run func() ([]byte, bool)) error {
	s.mu.Lock()
	*s.calls = append(*s.calls, s.setup)
	s.mu.Unlock()
	run()
	return nil
}

func (s formSeat) ForSetup() procexec.Seat {
	s.setup = true
	return s
}

// stubMachine is a device with a fixed report; settled says Settle was told.
type stubMachine struct {
	report  preflight.Report
	settled *preflight.Report
}

func (m *stubMachine) Plan() preflight.Report    { return m.report }
func (m *stubMachine) Settle(r preflight.Report) { *m.settled = r }

func item(b preflight.Bucket, name, reason string) preflight.Item {
	return preflight.Item{Bucket: b, Name: name, Reason: reason}
}

func TestSetupTurnRunsItsCallsOnTheSetupSeatAndOrdinaryTurnsDoNot(t *testing.T) {
	var (
		mu    sync.Mutex
		calls []bool
		got   preflight.Report
	)
	seat := formSeat{Stance: procexec.Stance{Class: procexec.Sandboxed}, mu: &mu, calls: &calls}
	machine := &stubMachine{settled: &got, report: preflight.Report{Items: []preflight.Item{
		item(preflight.Installable, "jq", ""),
		item(preflight.Impossible, "cuda", "needs a GPU"),
		item(preflight.Present, "git", ""),
	}}}
	bash := func(context.Context, []ai.Message) (*ai.Response, error) {
		return toolResponse("a", "bash", `{"command":"true"}`), nil
	}
	var brief string
	seeing := func(_ context.Context, m []ai.Message) (*ai.Response, error) {
		brief = userTextIn(m)
		return bash(nil, nil)
	}
	completer := &routedCompleter{parent: []step{seeing, finalText("done"), bash, finalText("done")}}
	agent, _ := newTestAgent(t, completer, func(config *Config) {
		config.Workspace = t.TempDir()
		config.bashBelt = true
		config.InTask = true
		config.taskID = 1
		config.Seat = seat
		config.Machine = machine
	})

	events, err := agent.SubmitSetup(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	collect(t, events)
	if len(got.Items) != 3 {
		t.Fatalf("the machine was not told the setup turn ended: %+v", got)
	}
	if !strings.Contains(brief, "jq") || strings.Contains(brief, "cuda") || strings.Contains(brief, "git") {
		t.Fatalf("the brief must name installable items only:\n%s", brief)
	}
	collect(t, mustSubmit(t, agent, "and now an ordinary one"))

	mu.Lock()
	defer mu.Unlock()
	if len(calls) != 2 || !calls[0] || calls[1] {
		t.Fatalf("calls made on the setup form = %v, want [true false]", calls)
	}
}

func TestSetupRefusesWithNothingToInstallOrNoMachine(t *testing.T) {
	var got preflight.Report
	present := &stubMachine{settled: &got, report: preflight.Report{Items: []preflight.Item{
		item(preflight.Present, "git", ""),
		item(preflight.Impossible, "cuda", "needs a GPU"),
	}}}
	for name, machine := range map[string]Machine{"none": nil, "all present": present} {
		completer := &routedCompleter{}
		agent, _ := newTestAgent(t, completer, func(config *Config) { config.Machine = machine })
		if events, err := agent.SubmitSetup(context.Background()); err == nil || events != nil {
			t.Errorf("%s: SubmitSetup answered %v, %v; want a refusal and no turn", name, events, err)
		}
	}
	if got.Items != nil {
		t.Error("a refused setup must not settle anything")
	}
}

// The network a setup turn opens is asked about, unless the gate is already a
// blanket allow.
func TestSetupCallsAskAboutTheNetworkUnlessApprovalsAreOpen(t *testing.T) {
	bash := ai.ToolCall{ID: "c", Function: ai.ToolCallFunction{Name: "bash", Arguments: `{"command":"pip install --user x"}`}}
	setup := userMessage{setup: true}.turnContext(context.Background())
	for _, tc := range []struct {
		name    string
		ctx     context.Context
		policy  approval.Policy
		allowed bool
	}{
		{"ordinary call, listed allow", context.Background(), approval.Policy{Default: approval.ActionPrompt, Tools: map[string]approval.Action{"bash": approval.ActionAllow}}, true},
		{"setup call, listed allow", setup, approval.Policy{Default: approval.ActionPrompt, Tools: map[string]approval.Action{"bash": approval.ActionAllow}}, false},
		{"setup call, blanket allow", setup, approval.Policy{Default: approval.ActionAllow}, true},
	} {
		policy := tc.policy
		agent, _ := newTestAgent(t, &scriptedCompleter{}, func(config *Config) { config.ApprovalPolicy = &policy })
		if _, allowed := agent.approve(tc.ctx, nil, bash); allowed != tc.allowed {
			t.Errorf("%s: allowed = %v, want %v", tc.name, allowed, tc.allowed)
		}
	}
}

// A setup turn is the harness's own bounded request; whatever it costs, it is
// never handed to a task.
func TestSetupTurnIsNeverHandedToATask(t *testing.T) {
	ask := "install jq and yq into this folder, then check each runs, and report the versions"
	agent := checkpointAgent(t, &scriptedCompleter{})
	agent.personAsk = ask
	ctx := userMessage{setup: true}.turnContext(context.Background())
	over := agent.handOverRunningTurn(ctx, newEventHub(), &Usage{},
		time.Time{}, agent.model, checkpointCeilingNote, checkpointSeamCeiling, 0, nil,
		routeVerdict{Work: true, Goal: ask}, checkpointRead{}, nil)
	if over.moved || over.decision != checkpointCeilingSetup {
		t.Fatalf("the handover decided %q (moved %v), want %q", over.decision, over.moved, checkpointCeilingSetup)
	}
	if agent.graph().node(1) != nil {
		t.Fatal("the handover admitted a node for a setup turn")
	}
}
