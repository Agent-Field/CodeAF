package main

import (
	"context"
	"errors"
	"sync"

	"github.com/Agent-Field/aforge-v2/internal/session"
)

// THE OTHER HALF OF THE ADAPTIVE RUN — and it is a shorter half than the
// harness's, because the engine is already in the session.
//
// internal/session ships the whole run: the planner on this conversation's own
// client, the executor that makes each node a child agent, the fuel tank and the
// gate ([session.Agent.RunOrchestrate]). What it does NOT ship is a way for a
// turn to reach it. Config.OrchestrateRunner is the seam, the session checks it
// for nil before it offers anything, and until this file existed nothing filled
// it — so the engine had no caller outside its own tests, and the doors onto it
// were switched off in every build that ships.
//
// AND THERE IS NO DOOR LEFT ABOVE THIS SEAM AT ALL. The model's `run_adaptive`
// hand went first; the anchored cue a turn was read for ("orchestrate the
// migration") followed it, because a conversation may not open a planned graph
// (internal/session's loop.go carries the whole statement). So filling this seam
// arms nothing today: it is kept wired because the session's own run seams — the
// gate, the steering, the roster family and the room that draws them — are built
// against a bound agent, and a surface that could start a run the room cannot
// then find is the defect this arrangement exists to prevent.
//
// WHY A SEAM AT ALL, when the thing it reaches is a method on the very agent
// being built: because the config is what BUILDS the agent, so nothing here has
// an agent to close over at the moment the closure has to exist. That is the
// whole of [v3Runs] below — the config is handed a closure, the agent is built
// from it, and the closure is told which agent it belongs to before anybody can
// call it. It is deliberately not a package-level pointer: /new and /resume each
// build a conversation of their own, and a run started in one must be registered
// in the one the person is looking at, or the room that draws its graph would
// ask the wrong session for a snapshot.

// v3OpenSession builds one v3 conversation with its adaptive runner wired to
// itself.
//
// EVERY DOOR THAT BUILDS A CONVERSATION COMES THROUGH HERE — the launch, the
// relaunch onto a fresh file when the first is locked, /new and /resume — for
// the reason the harness registry is opened once and handed to both halves: a
// session that could start a run the surface cannot then find is worse than a
// session that cannot start one.
func v3OpenSession(cfg session.Config) (*session.Agent, error) {
	cfg, runs := v3Adaptive(cfg)
	agent, err := session.New(cfg)
	if err != nil {
		return nil, err
	}
	runs.bind(agent)
	return agent, nil
}

// v3Adaptive puts the seam on a config and hands back the half that still has
// to be told which agent it belongs to. It is separate from the door above only
// so that both halves of the arrangement can be looked at on their own.
func v3Adaptive(cfg session.Config) (session.Config, *v3Runs) {
	runs := &v3Runs{}
	cfg.OrchestrateRunner = runs.start
	return cfg, runs
}

// v3Runs is the late binding between a config and the agent it built.
//
// The lock is not ceremony: a run is started from a tool call on the turn
// goroutine, and the binding is written on the goroutine that opened the
// session. They are different goroutines even though they can never race in
// practice — nothing can call a tool on an agent that has not been returned yet
// — and a data race that cannot happen is still a data race the detector is
// right about.
type v3Runs struct {
	mu    sync.Mutex
	agent *session.Agent
}

// bind names the agent the seam belongs to. It is called once, immediately
// after the agent is built.
func (r *v3Runs) bind(agent *session.Agent) {
	r.mu.Lock()
	r.agent = agent
	r.mu.Unlock()
}

// start launches one adaptive run on the bound session. An unbound seam is a
// bug in this file rather than a state a person can reach, and it answers with
// a sentence rather than a panic for the reason every other refusal on this
// surface does: a conversation is not worth crashing over.
func (r *v3Runs) start(ctx context.Context, goal, model string, capDollars float64) (string, error) {
	r.mu.Lock()
	agent := r.agent
	r.mu.Unlock()
	if agent == nil {
		return "", errors.New("this conversation is not ready to run one yet")
	}
	return agent.RunOrchestrate(ctx, goal, model, capDollars)
}
