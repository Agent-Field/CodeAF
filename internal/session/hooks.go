package session

// The control plane: the four moments in a turn where the harness — rather than
// the model — gets a say, each one named.
//
// Nothing here is new behavior. The stub pass, the approval gate, the guardian
// and the loop detector already existed and already ran at exactly these four
// moments; what they did not have was a NAME for the moment, so each one was
// wired into loop.go as a line of its own. That is fine for four. It is not fine
// for the fifth, and every guardrail, recovery move and context trick this
// harness grows next is a fifth: without a seam they land as more lines in the
// turn, each with its own ordering argument, none of them testable apart from a
// whole turn.
//
// ── THE FOUR, IN HARNESS-R1'S OWN WORDS ──
//
// (https://arxiv.org/abs/2608.02276 §"lifecycle hooks", the edit surface an
// automated harness-engineer emits patches against; the one-liners below are
// theirs, and each interface's doc comment repeats the one it implements.)
//
//	1. episode-init   — set starting context/state
//	2. pre-decision   — augment context with retrieved guidance/constraints
//	                    before the model decides
//	3. pre-action     — canonicalize, rewrite, or VETO the proposed action
//	                    before it hits the environment
//	4. post-feedback  — inspect the observation, trigger recovery when the
//	                    trajectory stalls
//
// The mapping to what this session already does is exact, which is the reason
// to adopt the naming rather than invent one:
//
//	episode-init   the turn's loop window (looped.go) and change ledger (recovery.go)
//	pre-decision   the tool-output stub pass (stub.go)
//	pre-action     the approval gate, and the guardian inside it (consent.go, guardian.go)
//	post-feedback  the loop detector's nudge and its recovery escalation (looped.go, recovery.go)
//
// ── WHY THE REGISTRY IS PER TURN, NOT ON THE AGENT ──
//
// Two of the four citizens carry state that is a fact about ONE turn: the loop
// window, and the ledger of files this turn changed. looped.go's own comment
// says why that state may not outlive its turn — a detector remembering
// yesterday's repetitions nudges a model for a call it is making for the first
// time today — and a ledger remembering yesterday's edits would offer to revert
// work the person has already accepted. So the plane is built per turn, by
// [Agent.newEpisode], and dies with it. The Agent holds nothing: there is no
// registry to reset, no lock to take, and no way for one conversation's turn to
// see another's.
//
// A hook is therefore an object with an Agent in it, not a plugin: it reaches
// the turn's private state, which is the whole point of a seam INSIDE the loop.
// The interfaces are unexported for that reason — a hook that could be written
// outside this package could not touch anything worth hooking.
//
// ── THE LAW OF THE PLANE ──
//
// A hook is an ASIDE, with one exception. episode-init, pre-decision and
// post-feedback may not fail a turn and have no way to say so: they return
// nothing, and a panic in one is a bug in this package, not a turn the person
// loses. pre-action is the exception and the reason the seam is worth having —
// it is the one hook that may STOP something, and it says so in its signature by
// handing back the refusal the model will read.

import (
	"context"

	"github.com/Agent-Field/agentfield/sdk/go/ai"
)

// ── the four hooks ──────────────────────────────────────────────────────────

// episodeInitHook is `episode-init`: "set starting context/state".
//
// It runs once, at the top of a turn, before the first request is assembled. Its
// citizens hang the turn's own state off the episode — a window, a ledger — and
// nothing else: work done here is work done before anybody has asked for
// anything.
type episodeInitHook interface {
	// Name identifies the citizen in the plane's order, and in the failures of
	// the tests that assert it.
	Name() string
	EpisodeInit(ep *episode)
}

// preDecisionHook is `pre-decision`: "augment context with retrieved
// guidance/constraints before the model decides".
//
// It runs at the boundary where the next request's context is settled and the
// model has not yet been asked anything — which for this session is the end of a
// completed turn, where the stub pass rewrites old heavy results into pointers
// before the compaction check weighs what remains (stub.go's own ordering
// argument). Anything that shapes WHAT THE MODEL WILL SEE belongs here: retrieved
// lessons, injected constraints, a memory read.
type preDecisionHook interface {
	Name() string
	PreDecision(ctx context.Context, ep *episode)
}

// preActionHook is `pre-action`: "canonicalize, rewrite, or veto the proposed
// action before it hits the environment".
//
// It runs inside [Agent.executeTool] — the one chokepoint every execution passes
// through, the batch's and the early start's alike (loop.go) — after the belt has
// been found to carry the tool and before the tool runs.
//
// The contract:
//
//   - The returned call is what runs. A citizen that canonicalizes hands back a
//     rewritten call; every citizen today hands back the call it was given. The
//     tool NAME is fixed by the dispatch that already happened, so a hook that
//     wants a different tool must veto and let the model ask again — a rewrite
//     that changed the name would run one tool's arguments through another's.
//   - false is a VETO, and the toolResult beside it is what the model reads
//     instead of the tool's output. It must say why, in words the model can act
//     on: a refusal it can work around is worth more than one that ends the turn.
//   - The first veto wins and the rest of the chain does not run. A call somebody
//     has already refused is not a call the next citizen has an opinion about.
type preActionHook interface {
	Name() string
	PreAction(ctx context.Context, ep *episode, hub *eventHub, call ai.ToolCall) (ai.ToolCall, toolResult, bool)
}

// postFeedbackHook is `post-feedback`: "inspect the observation, trigger recovery
// when the trajectory stalls".
//
// It runs at the step boundary, after a batch's results are in the transcript and
// before the next request is assembled — the one moment a note can ride into the
// next request the way a person's steering does (looped.go).
//
// It sees the calls and their results in call order, paired by index. It sees
// nothing else on purpose: a detector that read the whole transcript would be a
// second model's worth of judgement about a turn, and the deterministic stuck
// signals are the cheap half of recovery that works without one
// (PMCoder, https://arxiv.org/abs/2608.06811).
type postFeedbackHook interface {
	Name() string
	PostFeedback(ctx context.Context, ep *episode, hub *eventHub, calls []ai.ToolCall, results []toolResult)
}

// ── the registry ────────────────────────────────────────────────────────────

// controlPlane is the four ordered lists. It is built once per turn and never
// written after, so it needs no lock: pre-action runs from every goroutine in a
// tool batch at once, and a registry that could be appended to mid-turn would be
// a slice being read by a dozen calls while somebody grew it.
type controlPlane struct {
	episodeInit  []episodeInitHook
	preDecision  []preDecisionHook
	preAction    []preActionHook
	postFeedback []postFeedbackHook
}

// register adds one citizen to every list it satisfies. A hook that implements
// two of the four — the ledger implements three — is registered once and appears
// in each, in the order it was registered.
//
// An object that implements none is a silent no-op rather than an error: this is
// called with literals from one function in this file, so a hook that satisfies
// nothing is a compile-time typo, and there is nobody at runtime to tell.
func (p *controlPlane) register(hook any) {
	if citizen, ok := hook.(episodeInitHook); ok {
		p.episodeInit = append(p.episodeInit, citizen)
	}
	if citizen, ok := hook.(preDecisionHook); ok {
		p.preDecision = append(p.preDecision, citizen)
	}
	if citizen, ok := hook.(preActionHook); ok {
		p.preAction = append(p.preAction, citizen)
	}
	if citizen, ok := hook.(postFeedbackHook); ok {
		p.postFeedback = append(p.postFeedback, citizen)
	}
}

// controlPlaneFor builds the session's plane: the four mechanisms that existed
// before this file, each now the first citizen of its own hook.
//
// THE REGISTRATION ORDER IS THE LAW, and one order satisfies both lists that
// care about it:
//
//   - pre-action runs THE GATE FIRST. A call nobody has approved is not a call
//     the ledger needs to have taken a note about, and the note it takes is a
//     stat of the file the call is about to change.
//   - post-feedback runs THE LEDGER FIRST. The detector's escalation offers to
//     revert what this turn changed, and "this turn" has to include the batch
//     that just tipped the detector over — an offer computed before the ledger
//     read the batch would be an offer missing the very edit the model is stuck
//     repeating.
//
// The guardian is NOT a separate citizen. It lives where it has always lived,
// inside the gate's prompt branch (guardian.go), because its whole safety
// argument is positional: it sees only calls the policy already decided to ask
// about, and it can only turn that prompt into an allow. Registered beside the
// gate it would be a hook that could veto — which is exactly the power it must
// not have.
func (a *Agent) controlPlaneFor() *controlPlane {
	plane := &controlPlane{}
	plane.register(approvalGate{agent: a})
	plane.register(&changeLedger{agent: a})
	plane.register(loopDetector{agent: a})
	plane.register(stubPass{agent: a})
	// The error→fix sidecar hangs one more piece of turn state (fixrecall.go).
	// It is registered LAST and its position carries no argument, because
	// episode-init is the one hook whose order cannot matter: every citizen there
	// writes its own field on a struct nobody else has read yet.
	plane.register(fixMemory{agent: a})
	// The write scope runs LAST of the pre-action citizens, and only ever
	// refuses: an agent with no scope (every agent but a node of an adaptive
	// run) is one slice length away from being where it was before this
	// citizen existed (orchestrate.go).
	plane.register(writeGuard{agent: a})
	// AND WHAT A WORKER'S GIT MAY DO, which is the same shape as the write scope
	// and about a different kind of reach: not which files this agent may touch,
	// but whose work it may pull into its own copy (taskgit.go). It is registered
	// after the scope because it is the narrower question — a call the scope
	// already refused is a call there is nothing left to say about — and it is a
	// no-op on every agent that is not inside a task, which is every conversation.
	plane.register(taskGitGuard{agent: a})
	// AND A HAND'S ROUND BUDGET, which is a citizen only on a hand (fork.go). It
	// is registered conditionally rather than made a no-op on every agent because
	// post-feedback is on the step boundary of every turn this program runs, and
	// a citizen that did nothing there would still be a lock taken and a slice
	// walked on each of them.
	if a.config.handLeash != nil {
		plane.register(a.config.handLeash)
	}
	return plane
}

// ── the episode ─────────────────────────────────────────────────────────────

// episode is one turn as the control plane sees it: the plane itself, the agent
// it belongs to, and the state its citizens hang here at episode-init.
//
// It is threaded through the turn by hand rather than carried on the Agent or in
// a context value. By hand because the compiler then knows which paths have one:
// [Agent.executeTool] cannot be called without an episode, so the gate cannot be
// bypassed by a new call site that forgot it — which is the property the
// chokepoint comment in loop.go claims and could not previously enforce.
type episode struct {
	agent *Agent
	plane *controlPlane

	// watch is the loop detector's window over this turn's calls (looped.go).
	watch *loopWatch
	// changes is what this turn's successful edits and writes touched
	// (recovery.go), and what a revert would restore.
	changes *fileLedger
	// fixes is the error→fix lane: what has failed on each hand this turn, so
	// that the next call on that hand can be read as the fix or as the same
	// failure again (fixrecall.go).
	fixes *fixLane
}

// newEpisode builds one turn's control plane and runs `episode-init`.
func (a *Agent) newEpisode() *episode {
	ep := &episode{agent: a, plane: a.controlPlaneFor()}
	for _, hook := range ep.plane.episodeInit {
		hook.EpisodeInit(ep)
	}
	return ep
}

// preDecision runs the pre-decision chain.
func (ep *episode) preDecision(ctx context.Context) {
	if ep == nil {
		return
	}
	for _, hook := range ep.plane.preDecision {
		hook.PreDecision(ctx, ep)
	}
}

// preAction runs the pre-action chain and reports the call to run, or the
// refusal to hand the model. The first veto ends the chain.
func (ep *episode) preAction(ctx context.Context, hub *eventHub, call ai.ToolCall) (ai.ToolCall, toolResult, bool) {
	if ep == nil {
		return call, toolResult{}, true
	}
	for _, hook := range ep.plane.preAction {
		rewritten, refused, allowed := hook.PreAction(ctx, ep, hub, call)
		if !allowed {
			return call, refused, false
		}
		call = rewritten
	}
	return call, toolResult{}, true
}

// postFeedback runs the post-feedback chain.
func (ep *episode) postFeedback(ctx context.Context, hub *eventHub, calls []ai.ToolCall, results []toolResult) {
	if ep == nil {
		return
	}
	for _, hook := range ep.plane.postFeedback {
		hook.PostFeedback(ctx, ep, hub, calls, results)
	}
}

// ── the four legacy citizens ────────────────────────────────────────────────

// stubPass is the tool-output stub (stub.go) as a pre-decision citizen. It is a
// pure adapter: the pass itself, its guards and its silence are unchanged.
type stubPass struct{ agent *Agent }

func (stubPass) Name() string { return "stub" }

func (s stubPass) PreDecision(context.Context, *episode) { s.agent.stubOldOutputs() }

// approvalGate is the consent gate (consent.go), with the guardian inside it
// (guardian.go), as the pre-action citizen. It rewrites nothing — the gate's
// answer is yes or no, and a gate that edited the call it was asked about would
// be answering a different question than the one the person was shown.
type approvalGate struct{ agent *Agent }

func (approvalGate) Name() string { return "approval" }

func (g approvalGate) PreAction(ctx context.Context, _ *episode, hub *eventHub, call ai.ToolCall) (ai.ToolCall, toolResult, bool) {
	refused, allowed := g.agent.approve(ctx, hub, call)
	return call, refused, allowed
}

// loopDetector is the stuck watch (looped.go): it opens the turn's window at
// episode-init and reads every batch at post-feedback, nudging or escalating.
type loopDetector struct{ agent *Agent }

func (loopDetector) Name() string { return "loop" }

func (loopDetector) EpisodeInit(ep *episode) { ep.watch = newLoopWatch() }

func (d loopDetector) PostFeedback(ctx context.Context, ep *episode, hub *eventHub, calls []ai.ToolCall, results []toolResult) {
	d.agent.nudgeIfLooping(ctx, hub, ep, calls, results)
}
