package runs

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/apps/bridge"
	"github.com/Agent-Field/codeaf/internal/apps/connect"
	"github.com/Agent-Field/codeaf/internal/apps/spec"
	"github.com/Agent-Field/codeaf/internal/apps/svc"
	native "github.com/Agent-Field/codeaf/internal/connect"
)

type connectorGrants struct {
	svc.Grants
	reason string
}

func (g *connectorGrants) Resolve(context.Context, svc.Scope, string) (svc.Credential, error) {
	if g.reason != "" {
		return svc.Credential{}, &bridge.Error{Code: bridge.CodeConflict, Message: g.reason, Details: map[string]any{"reason": g.reason}}
	}
	return svc.Credential{Service: "gmail", Level: "personal", Member: svc.Owner}, nil
}

type resumedConnector struct {
	calls    int
	approved bool
	outbound bool
}

func (c *resumedConnector) Call(ctx context.Context, _ svc.Scope, _ string, _ map[string]any) (any, error) {
	c.calls++
	c.approved = connect.Permitted(ctx)
	return "connected reply", nil
}
func (c *resumedConnector) Outbound(string) bool { return c.outbound }

func TestConnectorHookWaitResumeAndSevenDayStop(t *testing.T) {
	for _, reason := range []string{bridge.ReasonExpired, bridge.ReasonNeedsAllow} {
		for _, send := range []bool{false, true} {
			for _, stop := range []bool{false, true} {
				name := reason + "/read"
				if send {
					name = reason + "/send"
				}
				if stop {
					name += "/stop"
				} else {
					name += "/resume"
				}
				t.Run(name, func(t *testing.T) {
					h := newHarness(t)
					grants := &connectorGrants{reason: reason}
					h.all.Connect.Grants = grants
					h.all.Runs = h.runs
					cfg := svc.Config{Root: filepath.Join(h.root, "apps")}
					door := connect.NewWithClock(cfg, h.all, h.clock.get)
					defer door.Close()
					h.all.Connectors = door
					tool := "gmail.read"
					if send {
						tool = "gmail.send"
					}
					h.bundle.Runs["connection"] = &spec.Run{Run: "connection", Steps: []*spec.Step{{ID: "call", Title: "Call Gmail", Tool: tool, Args: map[string]any{"id": "message", "subject": "team"}, OnError: "continue", Retry: &spec.Retry{Max: 3}}}}
					state, err := h.runs.Start(h.ctx, h.scope(), bridge.StartRunParams{Run: "connection"})
					if err != nil {
						t.Fatal(err)
					}
					if send {
						if state.Status != bridge.RunWaiting || len(state.Steps) != 1 || state.Steps[0].Approval == "" {
							t.Fatalf("approval state=%+v", state)
						}
						_, err = h.inbox.Decide(h.ctx, h.scope(), bridge.DecideApprovalParams{ApprovalID: state.Steps[0].Approval, Answer: "yes"})
						if err != nil {
							t.Fatal(err)
						}
						state, _ = h.runs.Get(h.ctx, h.scope(), bridge.GetRunParams{RunID: state.ID})
					}
					if state.Status != bridge.RunNeedsYou || state.Fake || state.Error != "" || len(state.Steps) != 1 || state.Steps[0].Status != bridge.RunNeedsYou {
						t.Fatalf("waiting state=%+v", state)
					}
					waits, err := door.Waits(h.ctx, svc.Caller{Member: svc.Owner})
					if err != nil || len(waits.Waits) != 1 || waits.Waits[0].Step != "call" || waits.Waits[0].RunID != state.ID {
						t.Fatalf("waits=%+v err=%v", waits, err)
					}
					h.reopen(t)
					h.all.Runs = h.runs
					resumed := &resumedConnector{outbound: send}
					h.all.Connectors = resumed
					if stop {
						h.clock.set(h.clock.get().Add(7 * 24 * time.Hour))
						err = door.Sweep(h.ctx)
					} else {
						grants.reason = ""
						err = door.Resume("gmail", svc.Owner)
					}
					if err != nil {
						t.Fatal(err)
					}
					state, err = h.runs.Get(h.ctx, h.scope(), bridge.GetRunParams{RunID: state.ID})
					if err != nil {
						t.Fatal(err)
					}
					if stop {
						if state.Status != bridge.RunStopped || state.Error != "stopped · Gmail expired" || resumed.calls != 0 || state.Steps[0].Status != bridge.RunStopped {
							t.Fatalf("stopped=%+v calls=%d", state, resumed.calls)
						}
					} else if state.Status != bridge.RunDone || resumed.calls != 1 || resumed.approved != send {
						t.Fatalf("resumed=%+v connector=%+v", state, resumed)
					}
					waits, _ = door.Waits(h.ctx, svc.Caller{Member: svc.Owner})
					if len(waits.Waits) != 0 {
						t.Fatal(waits)
					}
				})
			}
		}
	}
}

func TestConnectorWaitIdentifiesLoopRound(t *testing.T) {
	h := newHarness(t)
	grants := &connectorGrants{reason: bridge.ReasonExpired}
	h.all.Connect.Grants = grants
	h.all.Runs = h.runs
	door := connect.NewWithClock(svc.Config{Root: filepath.Join(h.root, "apps")}, h.all, h.clock.get)
	defer door.Close()
	h.all.Connectors = door
	h.bundle.Runs["rounds"] = &spec.Run{Run: "rounds", Steps: []*spec.Step{{ID: "loop", Loop: &spec.Loop{Over: "$params.items", As: "item", Max: 2}, Steps: []*spec.Step{{ID: "call", Tool: "gmail.read"}}}}}
	state, err := h.runs.Start(h.ctx, h.scope(), bridge.StartRunParams{Run: "rounds", Params: map[string]any{"items": []any{"first", "second"}}})
	if err != nil {
		t.Fatal(err)
	}
	waits, err := door.Waits(h.ctx, svc.Caller{Member: svc.Owner})
	if err != nil || len(waits.Waits) != 1 || waits.Waits[0].Step != "loop#0/call" {
		t.Fatalf("waits=%+v err=%v", waits, err)
	}
	resumed := &resumedConnector{}
	h.all.Connectors = resumed
	if err := door.Resume("gmail", svc.Owner); err != nil {
		t.Fatal(err)
	}
	state, err = h.runs.Get(h.ctx, h.scope(), bridge.GetRunParams{RunID: state.ID})
	if err != nil || state.Status != bridge.RunDone || resumed.calls != 2 {
		t.Fatalf("state=%+v calls=%d err=%v", state, resumed.calls, err)
	}
}

type emptyConnectorKeys struct{}

func (emptyConnectorKeys) Key(string) (string, bool) { return "", false }
func (emptyConnectorKeys) Set(string, string)        {}

type disconnectedGrants struct {
	*connectorGrants
	manager *native.Manager
}

func (g *disconnectedGrants) ConnectorManager(context.Context, svc.Credential) (*native.Manager, error) {
	return g.manager, nil
}

func TestUnsuccessfulReconnectDoesNotRestartSevenDayClock(t *testing.T) {
	h := newHarness(t)
	cfg := svc.Config{Root: filepath.Join(h.root, "apps")}
	grants := &disconnectedGrants{connectorGrants: &connectorGrants{reason: bridge.ReasonExpired}, manager: native.NewAppManager(h.root, nil, emptyConnectorKeys{})}
	h.all.Connect.Grants = grants
	h.all.Runs = h.runs
	door := connect.NewWithClock(cfg, h.all, h.clock.get)
	defer door.Close()
	h.all.Connectors = door
	h.bundle.Runs["connection"] = &spec.Run{Run: "connection", Steps: []*spec.Step{{ID: "call", Tool: "gmail.read"}}}
	state, err := h.runs.Start(h.ctx, h.scope(), bridge.StartRunParams{Run: "connection"})
	if err != nil {
		t.Fatal(err)
	}
	before, _ := door.Waits(h.ctx, svc.Caller{Member: svc.Owner})
	grants.reason = ""
	h.clock.set(h.clock.get().Add(6 * 24 * time.Hour))
	if err := door.Resume("gmail", svc.Owner); err == nil {
		t.Fatal("missing native credential resumed the run")
	}
	after, _ := door.Waits(h.ctx, svc.Caller{Member: svc.Owner})
	if len(before.Waits) != 1 || len(after.Waits) != 1 || before.Waits[0] != after.Waits[0] {
		t.Fatalf("before=%+v after=%+v", before, after)
	}
	h.clock.set(h.clock.get().Add(24 * time.Hour))
	if err := door.Sweep(h.ctx); err != nil {
		t.Fatal(err)
	}
	state, err = h.runs.Get(h.ctx, h.scope(), bridge.GetRunParams{RunID: state.ID})
	if err != nil || state.Status != bridge.RunStopped || state.Error != "stopped · Gmail expired" {
		t.Fatalf("state=%+v err=%v", state, err)
	}
}
