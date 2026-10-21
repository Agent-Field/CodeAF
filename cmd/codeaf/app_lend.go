package main

import (
	"sync"

	"github.com/Agent-Field/codeaf/internal/appwire"
	"github.com/Agent-Field/codeaf/internal/guard"
	"github.com/Agent-Field/codeaf/internal/remote"
	"github.com/Agent-Field/codeaf/internal/session"
)

// ── A CONVERSATION THIS PROCESS ALREADY HOLDS IS LENT, NEVER REOPENED ───────
//
// The app's own factory worker opens each running stage in this process
// ([stageMaker.Open]): a bare agent with the flock on its journal, served on no
// engine host's socket. The app's crew hold then asked the journal's lock,
// which answers "held" for a lock held by this very process, and read that as
// another window ("this conversation is open in another codeaf window that the
// app cannot reach") — a refusal that could never clear while the stage lived,
// and that left the stage's question and task lanes with nobody able to answer.
//
// SO THE APP DOOR ASKS THIS PROCESS FIRST ([session.LiveAgentFor]) and lends the
// live agent to the app as a view, the way the terminal's item page already
// does ([liveStepView]). Nothing about the stage changes: no second writer
// (there is no journal open, only the same agent), no grant, no posture, no
// re-shaping (the engine has no JoinedWith, so the app's launch is not
// applied), and no change of who closes it — the runner's stageConversation
// does, and a view going away ends nothing.
//
// THE LEND IS FACTORY STAGES ONLY. [session.LiveAgentFor] answers for EVERY
// journal an agent in this process has open: a stage, a shaping turn's agent, a
// crew member a manager started in-process. Only the first is lent, and it is
// known by a registry ([stageLends]) the stage maker writes from [stageMaker.Open]
// to [stageConversation.Close], so the lookup verifies a live stage agent rather
// than assuming every live agent is one. Everything else takes the path it took
// before the lend: its host walk, then the refusal.
//
// A LENT STAGE'S MODEL, POLICY, POSTURE AND BUDGET ARE THE FACTORY'S. The view
// refuses the app's per-agent model, role, approval-posture and spend-rail
// changes with a conflict that names the factory (see [appBackend.lentFence]
// for the app's side, which refuses BEFORE it saves anything, and [lentAgent]
// for the engine side), and the hold never binds the app's saved agent budget
// onto it ([appBackend.bindBudgetConn]). The factory's own job budget is the
// factory's, separately, and a person's Stop and answers pass through.
//
// THE OWNER CLOSING THE AGENT ENDS THE VIEW (see the watch in [appLendLive]),
// so a stage resumed on the same transcript is lent again as a new agent.
//
// THIS COVERS THE SAME PROCESS ONLY. A stage a worker in ANOTHER local process
// holds is still refused, accurately.

// stageLends is the factory stages this process has open: added when
// [stageMaker.Open] has a running stage, removed by [stageConversation.Close].
// A transcript's live agent is lent only if it is in here.
var stageLends = struct {
	mu     sync.Mutex
	agents map[*session.Agent]struct{}
}{agents: map[*session.Agent]struct{}{}}

func rememberStageAgent(agent *session.Agent) {
	stageLends.mu.Lock()
	defer stageLends.mu.Unlock()
	stageLends.agents[agent] = struct{}{}
}

func forgetStageAgent(agent *session.Agent) {
	stageLends.mu.Lock()
	defer stageLends.mu.Unlock()
	delete(stageLends.agents, agent)
}

// liveStageFor is the factory stage's agent open in this process on transcript:
// a live agent that the stage maker registered and that is not closed.
func liveStageFor(transcript string) (*session.Agent, bool) {
	agent, ok := session.LiveAgentFor(transcript)
	if !ok || agent.Closed() {
		return nil, false
	}
	stageLends.mu.Lock()
	defer stageLends.mu.Unlock()
	_, staged := stageLends.agents[agent]
	return agent, staged
}

// errLentStage is the refusal for a change that belongs to the factory.
func errLentStage(what string) error {
	return appwire.Errorf(appwire.CodeConflict, "this is a running factory stage: its %s is set by the factory, not from here", what)
}

// lentFence refuses, BEFORE anything is saved, a per-agent change to the model,
// role, approval posture or spend rail of an agent that is a lent factory stage.
func (b *appBackend) lentFence(a *appAgent, what string) error {
	var live *appLive
	var transcript string
	b.locked(func() { live, transcript = a.live, a.transcript })
	if live != nil && live.conn.Lent {
		return errLentStage(what)
	}
	if live == nil && transcript != "" {
		if _, staged := liveStageFor(transcript); staged {
			return errLentStage(what)
		}
	}
	return nil
}

// bindBudgetConn is [appBackend.bindBudget] for a held connection: a lent stage
// keeps the factory's budget, so the app's saved agent figure is not bound onto it.
func (b *appBackend) bindBudgetConn(id string, conn *appConn) error {
	if conn.Lent {
		return nil
	}
	return b.bindBudget(id, conn.Conv)
}

// lentAgent is a live agent as the app's session wrapper holds it. The
// wrapper's endings (the connection saying goodbye, the session retiring) stop
// the turn in flight before they close the agent, which is right for a
// conversation the wrapper owns and wrong for one it only borrows: so those
// doors are dropped here. A person's own Stop, and the machinery stops that are
// not an ending of the view, still reach the agent, as they do in the terminal.
type lentAgent struct {
	liveStepView
}

// endsAView is the stop doors that mean the VIEW is going and not the work.
func endsAView(door session.StopDoor) bool {
	switch door {
	case session.StopByLeaving, session.StopByClosing, session.StopByRetired, session.StopByEngineStopped, session.StopByAbandoned:
		return true
	}
	return false
}

// SetModel is dropped: the stage's model is the factory's. The app refuses a
// model change before it gets here ([appBackend.lentFence]); this is the engine
// side of the same refusal for a client that asks directly, and the protocol's
// model change has no error to return.
func (l lentAgent) SetModel(string) {}

// SetSpendRail refuses: the stage's rail is the factory's.
func (l lentAgent) SetSpendRail(float64) error { return errLentStage("spend limit") }

// SetApprovalPosture refuses: a stage's posture is set by the factory.
func (l lentAgent) SetApprovalPosture(string) error { return errLentStage("approval posture") }

// InterruptFor drops the view's endings; any other stop goes through.
func (l lentAgent) InterruptFor(door session.StopDoor) {
	if endsAView(door) {
		return
	}
	l.Agent.InterruptFor(door)
}

// InterruptNamed is [lentAgent.InterruptFor] with the window the door named.
func (l lentAgent) InterruptNamed(door session.StopDoor, name string) {
	if endsAView(door) {
		return
	}
	l.Agent.InterruptNamed(door, name)
}

// closedAgent exposes the owner's close signal without a polling clock.
type closedAgent interface{ ClosedSignal() <-chan struct{} }

// watchAgentClosed releases the view when its owner closes. release must be
// idempotent and must not join this goroutine.
func watchAgentClosed(agent closedAgent, stop <-chan struct{}, release func()) {
	closed := agent.ClosedSignal()
	guard.Go("app/agent-close-watch", func() {
		select {
		case <-stop:
		case <-closed:
			release()
		}
	})
}

// appLendLive serves the live agent to the app over an in-process attach. The
// session is persistent, so a torn pipe keeps the agent, and releasing it
// ends the view and never the stage.
func appLendLive(agent *session.Agent, ask appOpenAsk, hello remote.Hello) (*appConn, error) {
	engine := &remote.Engine{
		Agent:       lentAgent{liveStepView{agent}},
		Workspace:   ask.Workspace,
		SessionFile: ask.Transcript,
		Resumed:     true,
	}
	sess := remote.NewSession(engine, true)
	loop, err := remote.LoopbackAttach(hello, remote.AttachOptions{
		Open: func(remote.Hello) (*remote.Session, error) { return sess, nil },
	})
	if err != nil {
		_ = sess.Close()
		return nil, err
	}
	// RELEASE IS IDEMPOTENT AND NEVER JOINS THE WATCH. It runs from the app
	// (a view letting go), from the connection dying (b.died) and from the watch
	// below (the owner closing the agent), in any order and possibly at once.
	var once sync.Once
	stop := make(chan struct{})
	release := func() {
		once.Do(func() {
			close(stop)
			_ = loop.Close()
			// The session's rails are the view's; the agent is not (lentAgent).
			_ = sess.Close()
		})
	}
	// THE OWNER CLOSING THE AGENT ENDS THE VIEW. The runner closes a stage with
	// agent.Close, which tells a remote.Session nothing, so without this the
	// question lane never ends, the app keeps a hold on a dead agent, and a
	// RESUMED stage on the same transcript is never lent again: its card reaches
	// nobody.
	watchAgentClosed(agent, stop, release)
	conn := appConnOf(loop.Client, release, false)
	if conn.Conv == nil {
		release()
		return nil, appwire.Errorf(appwire.CodeConflict, "this conversation could not be lent to the app")
	}
	conn.Lent = true
	return conn, nil
}
