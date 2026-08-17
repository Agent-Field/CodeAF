package session

// THE ADAPTIVE RUN, from this side of the wall.
//
// internal/orchestrate owns the shape: a planner that only amends and a
// scheduler that only launches, one fuel tank, digests between nodes. It owns
// no model, no tools and no worktree, because none of those are shapes. THIS
// FILE IS WHERE THEY ARE SUPPLIED — and the whole of it is three seams and the
// doorways a surface reaches them through:
//
//	the planner    one non-streamed model call per completion, held to an
//	               amendment by the salvage ladder and one repair turn — the
//	               same bargain a harness design is held to (harness_build.go)
//	the executor   one node = one child agent in the session's own loop, with
//	               its write scope enforced by the control plane and its
//	               worktree resolved through [Config.WorktreePath]
//	the lanes      notes, the gauge and the fuel gate go out on a standing
//	               subscription ([Agent.Orchestrations]), never on a turn's hub
//
// THE RUN OUTLIVES ITS TURN, which is the fact every other decision here bends
// around. A turn that asks for one ends immediately — a conversation frozen
// for twenty minutes on work the person can watch is not a conversation — so
// the run holds a context of its own, is registered so [Agent.Close] can end
// it, and reports back through the standing lane and an ambient note. That is
// the arrangement a designed harness already uses one file over, for the same
// reason.
//
// WHAT IS NOT HERE. Nothing in this file decides how much a run may spend, and
// nothing in it decides that a turn wanted one: the cap comes from the person's
// sentence or the default below, and the intent is a cue lookup and never a
// judgement (see [orchestrateCue]). A build with no runner wired
// (Config.OrchestrateRunner) never reaches past one nil check.

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/Agent-Field/aforge-v2/internal/approval"
	"github.com/Agent-Field/aforge-v2/internal/orchestrate"
	"github.com/Agent-Field/aforge-v2/internal/provider"
	"github.com/Agent-Field/aforge-v2/internal/roles"
	"github.com/Agent-Field/aforge-v2/internal/subharness"
	"github.com/Agent-Field/agentfield/sdk/go/ai"
)

const (
	// orchestrateWindow bounds one whole run: every node, every planner call,
	// and the wait at the fuel gate. It is long because the gate's answer is a
	// person's, and bounded at all because a run parked on a question nobody
	// will ever answer is a goroutine parked forever — the same argument
	// harnessDesignWindow makes, at a run's scale rather than a design's.
	orchestrateWindow = 4 * time.Hour

	// orchestrateLanes is how many nodes this session runs at once. Nodes are
	// small by construction, so width is where the speed is; it is not larger
	// because every lane is a whole child agent with its own context.
	orchestrateLanes = 4

	// orchestrateDefaultCap is the tank when nobody named one. Small on
	// purpose: the gate is where more money is asked for, and asking for it
	// once with the frontier on screen is a better question than asking for it
	// up front with nothing on screen.
	orchestrateDefaultCap = 2.00

	// The planner's own budget. It writes an amendment, not a page, and the
	// answer to most completions is `{}` — what the tokens are actually for is
	// a reasoning model's thinking.
	orchestratePlanTokens = 4000
	orchestratePlanTemp   = 0.2

	// A node's digest: what its dependents and the planner see instead of its
	// work. Both bounds are here because BOTH are the point — the planner is
	// the one big-context call in the system and every digest rides in it.
	orchestrateDigestLines = 8
	orchestrateDigestBytes = 1200

	// What stops a node short. A node in an adaptive run is one question and a
	// handful of turns; a node still going after this many steps is not a big
	// node, it is a node the planner cut wrong.
	orchestrateMaxSteps   = 60
	orchestrateNoProgress = 6
)

// ── the doorways a surface holds ────────────────────────────────────────────

// RunOrchestrate launches one adaptive run and returns immediately with its
// id. It is the ENGINE this package ships: a surface wires it into
// [Config.OrchestrateRunner] and gets a session that can also answer the run's
// gate and draw its frontier, because the run is registered here.
//
// The model is the TURN'S OWN WORD and it outranks everything: named, it is
// what the planner thinks with and what every node runs on. Named nothing, the
// two halves resolve their own roles instead ([orchestrateRoleModel]). The cap
// is dollars, and zero is a run nobody bounded — legal, and never what a turn
// asks for.
func (a *Agent) RunOrchestrate(ctx context.Context, goal, model string, capDollars float64) (string, error) {
	if goal = strings.TrimSpace(goal); goal == "" {
		return "", errors.New("an adaptive run needs a goal")
	}
	a.mu.Lock()
	if a.closed {
		a.mu.Unlock()
		return "", errAgentClosed
	}
	a.orchestrateSeq++
	seq := a.orchestrateSeq
	named := strings.TrimSpace(model)
	source, session := a.config.RolesSource, a.model
	a.mu.Unlock()

	// The id is the run's number written out. Both spellings name one run: the
	// events carry the number, because [Event.ID] is what every question lane
	// in this package is answered by, and the methods take the string, because
	// a run id travels through surfaces that have no uint64 to put it in.
	id := strconv.FormatUint(seq, 10)
	// THE CONTEXT IS NOT THE TURN'S. The turn that asked for this is over by
	// the time the first node runs; a run cancelled by the request for it would
	// never produce anything.
	runCtx, cancel := context.WithTimeout(context.Background(), orchestrateWindow)

	// THE RUN IS TWO KINDS OF CALL AND THEY ARE NOT THE SAME PURCHASE. The
	// planner is made once per completion and decides what everything else
	// costs; a node is one small question and there are many of them. Two roles,
	// resolved once here, so neither half has to ask again.
	plannerModel := orchestrateRoleModel(source, roles.RolePlanner, named, session)
	planner := &orchestratePlanner{agent: a, model: plannerModel}
	worker := &orchestrateExec{agent: a, model: orchestrateRoleModel(source, roles.RoleWorker, named, session), id: id}
	run := orchestrate.New(goal, planner, worker, orchestrate.Options{
		Cap:   capDollars,
		Lanes: orchestrateLanes,
		// The planner's model rides onto every snapshot so the run's page can
		// name it beside the gauge: it is the judgement the tank is paying for,
		// and with no tiers set it is not the model the person is talking to.
		Planner: plannerModel,
		OnNote: func(text string) {
			a.emitOrchestrate(Event{Kind: EventOrchestrateNote, ID: seq, Text: text})
		},
		OnFuel: func(fuel orchestrate.Fuel) {
			a.emitOrchestrate(Event{
				Kind: EventOrchestrateFuel, ID: seq,
				Text: fuel.Gauge(), Hint: orchestrate.Dollars(fuel.Cap),
			})
		},
		OnPause: func(fuel orchestrate.Fuel) {
			a.emitOrchestrate(Event{
				Kind: EventOrchestratePause, ID: seq,
				Text: fuel.Gauge(), Hint: orchestrate.Dollars(fuel.Cap),
			})
		},
	})
	planner.orch = run

	live := &orchestration{run: run, cancel: cancel}
	a.mu.Lock()
	if a.closed {
		a.mu.Unlock()
		cancel()
		return "", errAgentClosed
	}
	if a.orchestrations == nil {
		a.orchestrations = make(map[string]*orchestration, 1)
	}
	a.orchestrations[id] = live
	a.mu.Unlock()

	go func() {
		defer a.settleOrchestrate(id)
		snap, err := run.Run(runCtx)
		a.landOrchestrate(seq, goal, snap, err)
	}()
	return id, nil
}

// orchestrateRoleModel is a run's model ladder in one line.
//
// THE TURN'S OWN WORD OUTRANKS EVERYTHING. "orchestrate the migration with
// opus" is a person choosing the model for the work they are commissioning, and
// a registry that overrode it would be a setting answering a sentence. With
// nothing named, the ROLE decides — internal/roles' own ladder, pin then tier
// then the conversation's model as the floor — so an install that has
// configured no tiers runs exactly as it did before these roles existed.
//
// A resolution that fails at all falls to the session's model rather than
// refusing: the only ways it can fail are an unregistered role, which is a
// programming error nobody in a running orchestration can fix, and no model
// anywhere, which is the case where there is nothing better to answer with.
func orchestrateRoleModel(source func(key string) (string, bool), role roles.Role, named, session string) string {
	if named != "" {
		return named
	}
	if model, err := roles.Resolve(roles.Source(source), role, session); err == nil {
		return model
	}
	return session
}

// orchestration is one run as the session holds it: the engine, and the one
// way to end it early. Nothing else is kept here — the goal, the frontier and
// the write-up are all on the run's own snapshot, and a second copy of any of
// them would be a second answer to the same question.
type orchestration struct {
	run    *orchestrate.Orchestrator
	cancel context.CancelFunc
}

// ResolveOrchestrate answers one EventOrchestratePause: "topup:<dollars>"
// resumes with a raised cap, "finish" jumps to synthesis over partial
// results, "stop" settles the run with its partial trace.
//
// What comes back is the line to show for it — the gate is a question, and a
// surface that answered one is owed a sentence saying what that answer did.
func (a *Agent) ResolveOrchestrate(id, answer string) (string, error) {
	answer = strings.TrimSpace(strings.ToLower(answer))
	// ONE STOP WORD EVERYWHERE. The gate's "stop" is not a second way to end a
	// run: it is [Agent.Cancel] on this run, so that a run ended at the gate and
	// a run ended by the key on its page leave exactly the same trace and say
	// exactly the same sentence (cancel.go).
	if answer == orchestrate.GateStop {
		return a.Cancel(CancelRun + ":" + strings.TrimSpace(id))
	}
	live, known := a.orchestration(id)
	if !known {
		return "", fmt.Errorf("there is no run %q in this session", id)
	}
	if err := live.run.Resolve(answer); err != nil {
		return "", err
	}
	if answer == orchestrate.GateFinish {
		return "finishing on what is already done", nil
	}
	return "topped up; the run carries on", nil
}

// OrchestrateSnapshot is the room's poll: the run's latest published shape,
// or false when the id names no run this session knows.
func (a *Agent) OrchestrateSnapshot(id string) (orchestrate.Snapshot, bool) {
	live, known := a.orchestration(id)
	if !known {
		return orchestrate.Snapshot{}, false
	}
	return live.run.Snapshot(), true
}

// SteerOrchestrate appends one steering note; the planner sees it on its next
// call. Steering outranks the plan.
func (a *Agent) SteerOrchestrate(id, text string) error {
	live, known := a.orchestration(id)
	if !known {
		return fmt.Errorf("there is no run %q in this session", id)
	}
	live.run.Steer(text)
	return nil
}

// Orchestrations is the standing subscription to every adaptive run this
// session is driving: the planner's notes, the fuel gauge crossing its
// warning mark, and the gate.
//
// It exists for [Agent.TaskUpdates]'s reason and answers to the same law: a
// run outlives the turn that asked for it, so its most important event — the
// gate — has no hub to arrive on. A surface that draws runs subscribes once at
// startup; a surface that does not never calls this and pays nothing.
func (a *Agent) Orchestrations() <-chan Event {
	stream := newEventStream()
	a.mu.Lock()
	if a.closed {
		a.mu.Unlock()
		stream.close()
		return stream.out
	}
	a.orchestrateWatchers = append(a.orchestrateWatchers, stream)
	a.mu.Unlock()
	return stream.out
}

// emitOrchestrate puts one run event in front of whoever is watching. It is
// the standing lane and not a turn's hub, for [Agent.emitHarness]'s reason:
// every event here is about work that outlives its turn by construction.
func (a *Agent) emitOrchestrate(event Event) {
	a.mu.Lock()
	watchers := make([]*eventStream, len(a.orchestrateWatchers))
	copy(watchers, a.orchestrateWatchers)
	a.mu.Unlock()
	for _, watcher := range watchers {
		watcher.send(event)
	}
}

// orchestration looks one run up.
func (a *Agent) orchestration(id string) (*orchestration, bool) {
	a.mu.Lock()
	defer a.mu.Unlock()
	live, known := a.orchestrations[strings.TrimSpace(id)]
	return live, known
}

// settleOrchestrate releases a finished run's context AND LEAVES THE RUN IN
// THE REGISTRY.
//
// A finished run is not a run to forget. Its snapshot is the frontier that
// crystallized, the digests, the notes and the write-up — which is exactly
// what somebody opens the room to read after it lands, and a registry that
// deleted the entry would leave them holding an id that answers nothing. What
// is released is the goroutine's context and nothing else; the entry is small,
// there is one per run a person asked for, and [Agent.Close] drops them all.
func (a *Agent) settleOrchestrate(id string) {
	a.mu.Lock()
	live := a.orchestrations[id]
	a.mu.Unlock()
	if live != nil {
		live.cancel()
	}
}

// cancelOrchestrationsLocked ends every run in flight. It is called from
// [Agent.Close] with a.mu held, on exactly the terms a design in flight is
// ended: a run holds its own context precisely because its turn is gone, so
// nothing else would ever reach it.
func (a *Agent) cancelOrchestrationsLocked() {
	for _, live := range a.orchestrations {
		live.cancel()
	}
	a.orchestrations = nil
}

// landOrchestrate says what a finished run came to, in the two places it
// belongs: a line for the person, and a note the model will read on the next
// turn (noteHarnessDesign's law, one lane over).
func (a *Agent) landOrchestrate(run uint64, goal string, snap orchestrate.Snapshot, err error) {
	var line string
	switch {
	case snap.Stopped:
		// A PERSON ENDED THIS ONE, and the sentence is theirs rather than the
		// run's: what it spent and how far it got, which are the two things
		// somebody who has just stopped work wants to know and the two that were
		// still moving at the moment they pressed the key (cancel.go).
		line = stoppedRunNote(snap)
	case err != nil:
		line = fmt.Sprintf("the adaptive run for %q ended early: %v", clip(goal, 80), err)
	case snap.Answer != "":
		line = snap.Answer
	default:
		line = fmt.Sprintf("the adaptive run for %q stopped with %d nodes done and no write-up",
			clip(goal, 80), doneNodes(snap))
	}
	a.emitOrchestrate(Event{Kind: EventOrchestrateNote, ID: run, Text: line, Hint: snap.Fuel.Gauge()})
	a.enqueueAmbientNote(line)
}

func doneNodes(snap orchestrate.Snapshot) int {
	var landed int
	for _, node := range snap.Nodes {
		if node.State == orchestrate.Done {
			landed++
		}
	}
	return landed
}

// startOrchestrate launches one adaptive run for goal on a fuel cap, through
// the runner the config was handed (Config.OrchestrateRunner). NIL RUNNER IS
// ORCHESTRATION OFF, the same posture RunHarness keeps one seam over.
func (a *Agent) startOrchestrate(ctx context.Context, goal, model string, capDollars float64) (string, error) {
	run := a.config.OrchestrateRunner
	if run == nil {
		return "", nil
	}
	return run(ctx, goal, model, capDollars)
}

// ── the turn that asks for one ──────────────────────────────────────────────

// orchestrateCue is the whole of adaptive-run detection: a verb that names the
// thing, and the goal it hands over.
//
// IT IS A TABLE LOOKUP AND NEVER A JUDGEMENT, which is [harnessBuildCue]'s
// bargain and is made here for a larger reason: a run costs money. A model
// asked "was that a request for an adaptive run?" would be a call on every
// turn AND a wrong yes would be a wrong yes with a fuel tank attached. So the
// sentence says so in words or nothing happens.
//
// It is anchored for the same reason that one is: "orchestrate the migration"
// at the head of what somebody typed is a request, and the same words inside a
// paragraph are usually somebody describing one.
var orchestrateCue = regexp.MustCompile(
	`(?is)^(?:orchestrate|adaptively\s+(?:run|work\s+on|do)|` +
		`(?:run|start|kick\s+off)\s+(?:an?\s+)?adaptive\s+run\s*(?:on|for|to|that|:)?)\s+(.+)$`)

// orchestrateBudget reads the money out of a sentence: "with a $5 budget",
// "on $2.50", "$10". The FIGURE IS THE PERSON'S DECISION and the only one
// they get to make up front, so it is read wherever in the sentence they put
// it — and the clause comes out of the goal, because "with a $5 budget" is not
// part of the work.
var orchestrateBudget = regexp.MustCompile(`(?i)\s*(?:\b(?:with|on|under|for)\s+)?(?:a\s+)?\$\s*([0-9]+(?:\.[0-9]{1,2})?)\s*(?:dollar[s]?\s*)?(?:budget|cap|tank)?`)

// orchestrateGoal reads one turn's request for a run: what to work on, and how
// much of somebody's money it may spend. false is every other sentence.
func orchestrateGoal(text string) (goal string, cap float64, ok bool) {
	text = strings.TrimSpace(text)
	// The same courtesies a build request is unwrapped from, for the same
	// reason: "please orchestrate X" is "orchestrate X".
	for stripped := true; stripped; {
		stripped = false
		for _, opener := range harnessBuildOpeners {
			if len(text) >= len(opener) && strings.EqualFold(text[:len(opener)], opener) {
				text = strings.TrimSpace(text[len(opener):])
				stripped = true
				break
			}
		}
	}
	found := orchestrateCue.FindStringSubmatch(text)
	if found == nil {
		return "", 0, false
	}
	goal = strings.TrimSpace(found[1])
	cap = orchestrateDefaultCap
	if money := orchestrateBudget.FindStringSubmatchIndex(goal); money != nil {
		if amount, err := strconv.ParseFloat(goal[money[2]:money[3]], 64); err == nil && amount > 0 {
			cap = amount
			goal = strings.TrimSpace(goal[:money[0]] + " " + goal[money[1]:])
		}
	}
	if goal = strings.TrimSpace(goal); goal == "" {
		return "", 0, false
	}
	return goal, cap, true
}

// routeOrchestrate is this file's place in a turn, called from [Agent.runTurn]
// beside the harness routes.
//
// It reports (answered, completed) on routeHarness's own terms: answered=true
// means the turn is OVER because the run has started and there is nothing else
// this turn is going to do, and completed=true because it is over the ordinary
// way.
func (a *Agent) routeOrchestrate(ctx context.Context, hub *eventHub, user userMessage, started time.Time) (bool, bool) {
	// THE GATES ARE THE OFFER'S GATES. A runner to run it, and somebody
	// watching who can answer the fuel gate: a run nobody can top up is a run
	// that stops halfway and stays there.
	if a.config.OrchestrateRunner == nil || !a.config.AskConsent {
		return false, false
	}
	// ONLY WHAT A PERSON TYPED. A woken turn's note is the session talking to
	// itself, and a run commissioned out of one would be the session spending
	// somebody's money on its own suggestion.
	if user.empty() || user.wake || user.authored {
		return false, false
	}
	goal, cap, ok := orchestrateGoal(user.text())
	if !ok {
		return false, false
	}
	// The model is read off the goal by the clause reader the offer already
	// uses (harness.go): "orchestrate the migration with opus" chose a model
	// and asked for work on a migration, and the run is handed the second thing
	// without the first.
	model, _, goal := a.harnessTurnModel(goal)
	if goal = strings.TrimSpace(goal); goal == "" {
		return false, false
	}

	id, err := a.startOrchestrate(ctx, goal, model, cap)
	if err != nil {
		hub.send(Event{Kind: EventError, Err: err, Usage: a.sealTurn(Usage{}, started)})
		return true, false
	}
	if id == "" {
		// The runner declined without saying why. Nothing started, so the turn
		// the person typed runs exactly as it would have.
		return false, false
	}
	// The id comes back from the runner as text and rides the lane as the
	// number every question in this package is answered by; a runner that mints
	// ids of its own shape simply leaves the number at zero.
	seq, _ := strconv.ParseUint(id, 10, 64)
	a.emitOrchestrate(Event{
		Kind: EventOrchestrateNote, ID: seq,
		Text:  fmt.Sprintf("adaptive run started on %s: %s", orchestrate.Dollars(cap), goal),
		Hint:  orchestrate.Dollars(cap),
		Model: model,
	})
	// The turn ends HERE, with no assistant message: the run is the answer and
	// it has not happened yet (routeHarnessBuild's law).
	hub.send(Event{Kind: EventTurnDone, Usage: a.sealTurn(Usage{}, started)})
	return true, true
}

// ── the planner ─────────────────────────────────────────────────────────────

// orchestratePlanner is the thinking half of a run: one non-streamed model
// call per completion, on the session's own client, answering with an
// amendment or with nothing.
//
// It meters ITSELF. The law is that every model call in a run bills against
// one tank, and the planner is the one call the executor cannot see, so it
// charges what it spent the moment it knows (fuel.go's Charge).
type orchestratePlanner struct {
	agent *Agent
	orch  *orchestrate.Orchestrator
	model string
}

// Plan is one call, salvaged, with the ONE repair turn this pipeline allows —
// exactly the ladder a harness design is held to (harness_build.go), for
// exactly its reason: a reply that is a good amendment wearing a code fence
// costs nothing to fix, and re-asking costs the whole view again.
func (p *orchestratePlanner) Plan(ctx context.Context, view orchestrate.View) (orchestrate.Amendment, error) {
	return p.think(ctx, []ai.Message{
		textMessage("system", orchestratePlannerBrief),
		textMessage("user", renderOrchestrateView(view)),
	})
}

// Repair is the second half of that bargain, and the half only the run can
// ask for: an amendment that PARSED and was still refused — a cancel aimed at
// a running node, a need on an id nobody minted — is handed back with the
// sentence that refused it.
func (p *orchestratePlanner) Repair(ctx context.Context, view orchestrate.View, why string) (orchestrate.Amendment, error) {
	return p.think(ctx, []ai.Message{
		textMessage("system", orchestratePlannerBrief),
		textMessage("user", renderOrchestrateView(view)),
		textMessage("user", "Your last amendment was REFUSED: "+why+
			"\n\nAnswer again with one amendment that does not do that. {} is a fine answer."),
	})
}

func (p *orchestratePlanner) think(ctx context.Context, messages []ai.Message) (orchestrate.Amendment, error) {
	raw, err := p.ask(ctx, messages)
	if err != nil {
		return orchestrate.Amendment{}, err
	}
	amendment, parseErr := orchestrate.ParseAmendment(raw)
	if parseErr == nil {
		return amendment, nil
	}
	// THE REPAIR TURN CARRIES NO BRIEF. It is a transcription job, and handing
	// it the law that produced the first reply would invite it to reconsider
	// the plan while it is meant to be fixing a delimiter.
	second, err := p.ask(ctx, []ai.Message{
		textMessage("system", "You repair malformed JSON and do nothing else. You never change content, never add a field, never drop one, and never explain. Your whole reply is one JSON value."),
		textMessage("user", "This was meant to be one JSON object:\n\n"+raw+
			"\n\nIt did not parse. "+parseErr.Error()+
			"\n\nReply with ONLY the corrected JSON — the same content, nothing added, nothing dropped, no prose, no code fence."),
	})
	if err != nil {
		return orchestrate.Amendment{}, err
	}
	return orchestrate.ParseAmendment(second)
}

// ask is one call, billed twice: to the person's session usage, because an
// auxiliary call is not free (title.go, guardian.go), and to the run's tank,
// because a planner that did not meter would be spending money the gauge never
// sees.
func (p *orchestratePlanner) ask(ctx context.Context, messages []ai.Message) (string, error) {
	response, err := p.agent.client.CompleteWithMessages(
		provider.WithoutStream(ctx),
		messages,
		ai.WithModel(p.model),
		ai.WithMaxTokens(orchestratePlanTokens),
		ai.WithTemperature(orchestratePlanTemp))
	if err != nil {
		return "", err
	}
	if response == nil {
		return "", errors.New("the planner answered with nothing")
	}
	p.agent.addAuxiliaryUsage(response)
	p.orch.Charge(orchestrateCost(response, p.model))
	return response.Text(), nil
}

// orchestrateCost is what one call spent: THE PROVIDER'S OWN FIGURE WHEN THERE
// IS ONE, and the price table only when there is not. A reported cost knows
// about caching, discounts and which endpoint served the request; a table
// knows a list price (internal/orchestrate's fuel.go).
func orchestrateCost(response *ai.Response, model string) float64 {
	if response == nil || response.Usage == nil {
		return 0
	}
	usage := response.Usage
	if usage.Cost != nil && *usage.Cost > 0 {
		return *usage.Cost
	}
	return orchestrate.MeterCall(usage.PromptTokens, usage.CompletionTokens, model)
}

// renderOrchestrateView is the whole of what the planner sees. It is TEXT and
// not the JSON of a View, because half of it is a person's own sentences and a
// planner reading its steering out of a serialized struct is a planner one
// escape away from ignoring it.
//
// The order is the argument: the goal, then what the person has said since
// (which outranks the plan), then facts, then the frontier, then the money.
func renderOrchestrateView(view orchestrate.View) string {
	var out strings.Builder
	fmt.Fprintf(&out, "THE GOAL:\n%s\n", view.Goal)
	if len(view.Steer) > 0 {
		out.WriteString("\nWHAT THE PERSON HAS SAID SINCE (this outranks your plan):\n")
		for _, line := range view.Steer {
			fmt.Fprintf(&out, "- %s\n", line)
		}
	}
	out.WriteString("\nWHAT IS DONE:\n")
	if len(view.Results) == 0 {
		out.WriteString("- nothing yet; this is the opening call\n")
	}
	for _, node := range view.Results {
		switch node.State {
		case orchestrate.Failed:
			fmt.Fprintf(&out, "- %s FAILED: %s\n", node.ID, firstLine(node.Err))
		default:
			fmt.Fprintf(&out, "- %s: %s\n", node.ID, node.Digest)
		}
	}
	out.WriteString("\nTHE FRONTIER:\n")
	if len(view.Frontier) == 0 {
		out.WriteString("- empty\n")
	}
	for _, node := range view.Frontier {
		fmt.Fprintf(&out, "- %s [%s] %s%s\n", node.ID, orchestrateStateWord(node.State),
			firstLine(node.Goal), orchestrateNeedsWord(node.Needs))
	}
	fmt.Fprintf(&out, "\nFUEL: %s\n", view.Fuel.Gauge())
	if view.Fuel.Low() {
		out.WriteString("The tank is low. Add only what the goal cannot be answered without.\n")
	}
	out.WriteString("\nAnswer with one amendment, as JSON, and nothing else. {} means no change.\n")
	return out.String()
}

func orchestrateStateWord(state orchestrate.State) string {
	switch state {
	case orchestrate.Queued:
		return "queued"
	case orchestrate.Ready:
		return "ready"
	case orchestrate.Running:
		return "running"
	case orchestrate.Done:
		return "done"
	case orchestrate.Failed:
		return "failed"
	}
	return "?"
}

func orchestrateNeedsWord(needs []string) string {
	if len(needs) == 0 {
		return ""
	}
	return " (needs " + strings.Join(needs, ", ") + ")"
}

// orchestratePlannerBrief is what the planner is told.
//
// It is a constant here rather than a document because it is the LAW OF THE
// RUN restated for a model — the amendment vocabulary, the commitment rule,
// the size of a node — and every clause of it is a rule this package enforces
// in code a few hundred lines up. A guide that drifted from those checks would
// be a planner refused by its own instructions.
const orchestratePlannerBrief = `You plan an adaptive run. You never do the work.

You are called once at the start and once every time a node finishes. Each
time you see the goal, what has finished (as short digests), the frontier, the
fuel, and anything the person has typed at the run. You answer with ONE JSON
object and nothing else — no prose, no code fence.

THE WHOLE VOCABULARY:

  {"add": [{"id": "n3", "goal": "...", "needs": ["n1"], "write_scope": ["path"], "worktree": false, "verify": ""}],
   "cancel": [{"id": "n4", "reason": "..."}],
   "note": "one line about what you are doing",
   "done": {"brief": "what the write-up should say"}}

Every key is optional. {} is a complete answer and it is the RIGHT answer most
of the time: a node finished, nothing about the plan changed, say nothing.

THE RULES:

- A NODE IS SMALL. One question, one artifact, a handful of turns. Parallelism
  comes from having many nodes, never from a big one. A node whose goal has an
  "and" in it is two nodes.
- A NODE'S GOAL IS SELF-CONTAINED. It is read by a worker who cannot see this
  conversation, the other nodes, or you. Never write "see above" or "as
  discussed".
- NEEDS ARE ONLY FOR REAL DEPENDENCIES: this node cannot start until that one's
  finding exists. Two nodes with no edge between them run at the same time, so
  every edge you add that was not necessary is time somebody waits for nothing.
- WRITE_SCOPE IS THE PATHS A NODE MAY WRITE. Nodes that only read leave it out.
  Two nodes that write the same path are serialized for you — you do not need
  an edge for that.
- YOU MAY NOT CANCEL A RUNNING NODE. Cancel is for work that has not started.
  A finished node is a fact.
- IDS ARE MINTED ONCE. Never reuse one, never rename one.
- SAY DONE WHEN THE GOAL IS ANSWERED, not when the frontier is empty: a run
  that has what it needs should stop, and the brief you write is what the
  write-up is asked for.
- THE FUEL IS ONE TANK for the whole run, your own calls included. When it is
  low, add only what the goal cannot be answered without.`

// ── the executor ────────────────────────────────────────────────────────────

// orchestrateExec is the working half: one node, one child agent, the
// session's own loop and hands.
type orchestrateExec struct {
	agent *Agent
	model string
	id    string
}

// Exec runs one node and hands back its digest.
//
// A NODE IS A CHILD AGENT, which is the same answer task_run.go gives and for
// the same reason: a node is the same worker doing the same job somewhere
// quieter. What this adds is the two bounds the contract puts on it — the
// write scope, enforced by the control plane rather than asked for in the
// brief, and the worktree, resolved through the session's own seam.
//
// THE ERROR IS THE PLANNER'S NEWS. Nothing here retries, escalates or repairs:
// a node that failed is a fact on the next view, and what to do about it is
// the one judgement this whole design reserves for the planner.
func (e *orchestrateExec) Exec(ctx context.Context, node orchestrate.Node, deps []orchestrate.NodeStatus) (string, float64, error) {
	dir, shared := e.workspace(node)
	child, err := e.newChild(dir, node)
	if err != nil {
		return "", 0, err
	}
	defer child.Close()

	changed, stopped, runErr := runTaskChild(ctx, child, orchestrateBrief(node, deps, shared),
		dir, taskLimits{maxSteps: orchestrateMaxSteps, noProgress: orchestrateNoProgress}, nil, io.Discard)

	cost := e.spend(child)
	digest := orchestrateDigest(child, changed)
	switch {
	case runErr != nil:
		return digest, cost, runErr
	case stopped != "":
		return digest, cost, errors.New(stopped)
	case ctx.Err() != nil:
		return digest, cost, ctx.Err()
	}
	return digest, cost, nil
}

// workspace is the hybrid collision policy in one function: the shared tree by
// default, and the run's own worktree when the planner asked for one AND the
// seam has somewhere to put it.
//
// IT DEGRADES, IT NEVER ERRORS. A path that is not there yet is not a worktree
// — filling [Config.WorktreeRoot] with real ones belongs to the wave that owns
// session ids — and a node run in an empty directory it expected to be a
// checkout is worse than a node run in the tree everybody else is in. So the
// path is used only when it already exists, and the brief says which it got.
func (e *orchestrateExec) workspace(node orchestrate.Node) (dir string, shared bool) {
	dir = e.agent.config.Workspace
	if !node.Worktree {
		return dir, true
	}
	isolated := e.agent.config.WorktreePath(e.id + "-" + node.ID)
	if isolated == "" {
		return dir, true
	}
	if info, err := os.Stat(isolated); err != nil || !info.IsDir() {
		return dir, true
	}
	return isolated, false
}

// newChild builds the agent that IS the node. It is [Agent.newTaskAgent]'s
// configuration minus the graph: the same client, the same accounts, the same
// permissive-but-floored gate, a journal of its own — and the write scope,
// which is the one thing a task node has no equivalent of.
func (e *orchestrateExec) newChild(dir string, node orchestrate.Node) (*Agent, error) {
	a := e.agent
	a.mu.Lock()
	parent := a.config
	model := e.model
	if strings.TrimSpace(model) == "" {
		model = a.model
	}
	window := parent.ContextWindow
	if !strings.EqualFold(strings.TrimSpace(model), strings.TrimSpace(a.model)) {
		// A window measured for another model is not a fact about this one
		// (newTaskAgent states the whole argument).
		window = 0
	}
	client := unwrapCompleter(a.client)
	journal := orchestrateJournalPath(a.sessionID(), e.id, node.ID)
	a.mu.Unlock()

	return newAgent(Config{
		Workspace:      dir,
		Model:          model,
		APIKey:         parent.APIKey,
		BaseURL:        parent.BaseURL,
		ContextWindow:  window,
		CompactEnabled: parent.CompactEnabled,
		SessionFile:    journal,
		ApprovalPolicy: &approval.Policy{Default: approval.ActionAllow},
		AskConsent:     false,
		InTask:         true,
		writeScope:     node.WriteScope,
		SupportsImages: parent.SupportsImages,
		RolesSource:    parent.RolesSource,
		SearchProvider: parent.SearchProvider,
		SearchFetcher:  parent.SearchFetcher,
		Connect:        parent.Connect,
		connectHub:     parent.connectHub,
		ImageGenModel:  parent.ImageGenModel,
		ImageGenClient: parent.ImageGenClient,
		DocumentEngine: parent.DocumentEngine,
	}, client)
}

// spend is what one node's agent cost, and it folds that spend into the
// session's own pocket on the way past — a node's calls are the person's
// calls, exactly as a task node's are ([Agent.foldTaskUsage]).
func (e *orchestrateExec) spend(child *Agent) float64 {
	used := child.Usage()
	e.agent.mu.Lock()
	e.agent.usage.Input += used.Input
	e.agent.usage.Output += used.Output
	e.agent.usage.CacheRead += used.CacheRead
	e.agent.usage.CacheWrite += used.CacheWrite
	e.agent.usage.CostUSD += used.CostUSD
	e.agent.mu.Unlock()
	if used.CostUSD > 0 {
		return used.CostUSD
	}
	return orchestrate.MeterCall(used.Input, used.Output, e.model)
}

// orchestrateBrief is a node's whole world: its goal, what its prerequisites
// found, and the two bounds it is running under.
//
// UPSTREAM ARRIVES AS DIGESTS AND NOTHING ELSE. A node that could read its
// prerequisite's artifact would be a node whose context grows with the run,
// which is the shape this design exists to refuse.
func orchestrateBrief(node orchestrate.Node, deps []orchestrate.NodeStatus, shared bool) string {
	var out strings.Builder
	out.WriteString(node.Goal)
	if len(deps) > 0 {
		out.WriteString("\n\nWHAT THE WORK BEFORE YOU FOUND:\n")
		for _, dep := range deps {
			fmt.Fprintf(&out, "\n[%s] %s\n", dep.ID, dep.Digest)
		}
	}
	if len(node.WriteScope) > 0 {
		fmt.Fprintf(&out, "\n\nYOU MAY WRITE ONLY UNDER: %s\nAn edit or a write anywhere else is refused.",
			strings.Join(node.WriteScope, ", "))
	} else {
		out.WriteString("\n\nTHIS IS READ-ONLY WORK: find out, do not change anything.")
	}
	if !shared {
		out.WriteString("\nYou are in an isolated worktree; nobody else is working in it.")
	}
	// The kind and the rung are the planner's words about the SHAPE of the
	// work. The only executor here is the session loop, so they ride in the
	// brief rather than switching machinery: a run that wants a harness's
	// shapes reaches them through a harness (Config.RunHarness), which is a
	// page somebody wrote and this is not.
	if kind, known := subharness.Lookup(strings.TrimSpace(node.Kind)); known && kind.Name != subharness.KindAgentLoop {
		fmt.Fprintf(&out, "\nThe shape asked for is %s: %s.", kind.Name, kind.Desc)
	}
	if rung := strings.TrimSpace(node.Verify); rung != "" && subharness.VerifyRung(rung) >= 0 {
		fmt.Fprintf(&out, "\nYour work is checked at the %q rung: do not claim it is done until it passes that bar.", rung)
	}
	out.WriteString("\n\nEnd with a SHORT report of what you found or did — it is the only thing " +
		"anybody downstream will see of this work.")
	return out.String()
}

// orchestrateDigest is what the node hands back: its last word, cut to the
// size the planner's view can afford, with the files it wrote named after it
// because "what changed" is the half a report most often leaves out.
func orchestrateDigest(child *Agent, changed []string) string {
	digest := clip(firstLines(lastSaid(child), orchestrateDigestLines), orchestrateDigestBytes)
	if len(changed) == 0 {
		return digest
	}
	written := "wrote: " + strings.Join(changed, ", ")
	if digest == "" {
		return written
	}
	return digest + "\n" + clip(written, 200)
}

// orchestrateJournalPath is where one node's transcript lives, under the run
// that asked for it: ~/.aforge/v3/runs/<session>/<run>/<node>.jsonl.
//
// It is a REAL SESSION FILE for taskJournalPath's reason — the node is an
// agent, and everything it did should be readable with the same tools — and a
// machine with no home directory gets an in-memory node rather than a failed
// one.
func orchestrateJournalPath(session, run, node string) string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	safe := strings.Map(func(r rune) rune {
		if r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '-' || r == '_' {
			return r
		}
		return '-'
	}, node)
	return filepath.Join(home, ".aforge", "v3", "runs", session, run, safe+".jsonl")
}

// ── the write scope ─────────────────────────────────────────────────────────

// writeGuard is the control plane's citizen for [Config.writeScope]: a node
// that was given a slice of the tree may write in that slice and nowhere else.
//
// IT IS A HOOK AND NOT A SENTENCE IN THE BRIEF, and that is the whole point. A
// scope written into a prompt is a request; a scope on the pre-action seam is
// the one moment every execution passes through (hooks.go), so a node that
// wandered is refused by the harness rather than trusted not to wander. The
// refusal is a result the model READS — it can pick a different file and carry
// on — because a veto that ended the turn would cost the node its work.
//
// IT BINDS THE HANDS WHOSE TARGET IS A KNOWN PATH, which is edit and write
// (recovery.go's mutatingTools). bash is deliberately out of reach: a shell
// command's effects are whatever it did, and a guard that pattern-matched
// commands would be claiming a guarantee it cannot keep. What bounds a node's
// shell is the same thing that bounds every other agent's — the approval floor.
type writeGuard struct{ agent *Agent }

func (writeGuard) Name() string { return "write-scope" }

func (g writeGuard) PreAction(_ context.Context, _ *episode, _ *eventHub, call ai.ToolCall) (ai.ToolCall, toolResult, bool) {
	scope := g.agent.config.writeScope
	if len(scope) == 0 {
		return call, toolResult{}, true
	}
	path, shown, ok := g.agent.mutatingPath(call)
	if !ok {
		return call, toolResult{}, true
	}
	if orchestrateInScope(g.agent.config.Workspace, scope, path) {
		return call, toolResult{}, true
	}
	return call, toolResult{
		text: fmt.Sprintf("%s is outside this node's write scope (%s), so nothing was written. "+
			"Work inside the scope, or report what needs changing elsewhere and let the run decide.",
			shown, strings.Join(scope, ", ")),
		isError: true,
	}, false
}

// orchestrateInScope reads one absolute path against a node's scope. A path
// outside the workspace entirely is outside every scope: the scope is a slice
// of the work tree, and something above it is not a corner of it.
func orchestrateInScope(workspace string, scope []string, path string) bool {
	relative, err := filepath.Rel(workspace, path)
	if err != nil || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
		return false
	}
	return orchestrate.Covers(scope, filepath.ToSlash(relative))
}

// WorktreePath resolves where job id's isolated worktree would live. Empty
// root means empty path, and an empty path means the node shares the
// workspace — the planner's worktree flag degrades, it never errors.
func (c Config) WorktreePath(jobID string) string {
	if c.WorktreeRoot == "" {
		return ""
	}
	clean := strings.Map(func(r rune) rune {
		if r >= 'a' && r <= 'z' || r >= '0' && r <= '9' || r == '-' {
			return r
		}
		return '-'
	}, strings.ToLower(jobID))
	return filepath.Join(c.WorktreeRoot, clean)
}
