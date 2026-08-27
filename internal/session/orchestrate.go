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
// nothing in it decides that a turn wanted one. The cap comes from the person's
// sentence, from the model's call, or from the default below; and a run is
// commissioned from exactly two places — an anchored cue in what a person typed
// ([orchestrateCue], which is a lookup and never a judgement) and the model's
// own hand on the belt (tools_harness.go's run_adaptive, which is the judgement
// and is where the sentences no cue can catch are read). A build with no runner
// wired (Config.OrchestrateRunner) never reaches past one nil check.

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
	"sync"
	"time"

	"github.com/Agent-Field/aforge-v2/internal/approval"
	"github.com/Agent-Field/aforge-v2/internal/effort"
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

	// orchestrateDefaultCap is the tank when nobody named one. It was $2, and
	// real runs hit that gate mid-work often enough that the question became a
	// nag rather than a decision — the owner raised it to ten. The gate is
	// still where more money is asked for; it just fires when a run is
	// genuinely large rather than merely ordinary.
	orchestrateDefaultCap = 10.00

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
	// AND THE TANK IS HELD TO THIS SESSION'S OWN CAP. A session given a spend
	// rail may not hand out a run larger than the rail (rail.go's [Agent.railCap]);
	// it is read here, under the same lock every other config field on this path
	// is, and the gate the tank fires is where the figure is honoured.
	capDollars = a.railCap(capDollars)
	a.mu.Unlock()
	// THE PERSON'S OWN MESSAGE, TAKEN HERE AND NOT ASKED FOR. Whatever door
	// started this run — the tool on the belt, the anchored cue, /task — the
	// sentence that caused it is the newest thing the person typed, and this is
	// the last moment anybody has it: from here the run is a goal, a planner and
	// a fleet of workers that have never met them. It reaches the planner above
	// the goal and every node above its own (task_brief.go).
	request := a.taskRequest()

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
	plannerCall := orchestrateRoleCall(source, roles.RolePlanner, named, session)
	plannerModel := plannerCall.model
	planner := &orchestratePlanner{agent: a, call: plannerCall, request: request}
	worker := &orchestrateExec{
		agent:   a,
		call:    orchestrateRoleCall(source, roles.RoleWorker, named, session),
		id:      id,
		request: request,
		goal:    goal,
	}
	// THE RUN TAKES A ROW ON THE ROSTER BEFORE ITS FIRST NODE DOES, so that the
	// tree has a root to hang the family off from the moment the run exists
	// (the family section at the foot of this file).
	family := a.newOrchestrateFamily(goal, plannerModel, id)
	// AND THE RUN IS NAMED, if its goal is a sentence rather than a name
	// (taskname.go). It is asked for HERE, at the door, and not inside the
	// constructor: the row is published under the goal first, so the roster shows
	// the run from the moment somebody asked for it, and the name replaces the
	// sentence when it lands a few seconds later.
	family.nameRun(goal)
	// AND THE NODES' MODEL GOES WITH IT, settled here for the run's whole life
	// the way a task's is settled at admission: every row this family publishes
	// says which model is doing the work, and the answer must not be able to move
	// under a `/model` switch half way through the run.
	family.worker = worker.model()
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
		OnNodes: family.upsert,
		// AND A NODE THE PLANNER DID NOT NAME IS NAMED BY THE SAME SMALL MODEL
		// EVERYTHING ELSE IS. The law asks the planner for a title on the call it
		// adds the node, which is the name arriving on a call somebody is already
		// paying for; this is what happens when it does not, and it is deliberately
		// not a second namer (taskname.go's [orchestrateFamily.nameWorker]).
		Name: family.nameWorker,
	})
	planner.orch = run

	live := &orchestration{run: run, cancel: cancel, born: time.Now()}
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
		// The family settles before the write-up is announced: the roster is where
		// somebody looks when the note lands, and a root still saying "running"
		// beside a report of what the run came to is the roster disagreeing with
		// the conversation.
		family.settle(snap, err)
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
	return orchestrateRoleCall(source, role, named, session).model
}

// orchestrateRoleCall keeps the effort half of the role resolution until the
// request is made. A named model and the session floor carry no effort because
// [roles.ResolveCall] deliberately gives neither one a tier's setting.
func orchestrateRoleCall(source func(key string) (string, bool), role roles.Role, named, session string) roleRequest {
	if named != "" {
		return roleRequest{model: named}
	}
	if call, err := roles.ResolveCall(roles.Source(source), role, session); err == nil {
		return newRoleRequest(call)
	}
	return roleRequest{model: session}
}

// orchestration is one run as the session holds it: the engine, and the one
// way to end it early. Nothing else is kept here — the goal, the frontier and
// the write-up are all on the run's own snapshot, and a second copy of any of
// them would be a second answer to the same question.
type orchestration struct {
	run    *orchestrate.Orchestrator
	cancel context.CancelFunc
	// born is when this run was registered, and it is the ONE thing here that is
	// not a second copy of something on the snapshot: a run's shape, fuel and
	// write-up are all the orchestrator's own, and its age is not on any of them.
	// [Agent.WorkingNow] draws it, and a run scripted by a test that never set it
	// answers zero, which renders as nothing.
	born time.Time
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

// OrchestrateNodeJournal is the transcript door behind one published run node.
// The registry supplies the session identity and the snapshot supplies the
// node: neither a guessed path nor a file left by another run is evidence that
// this session knows the work. A machine with no home keeps the node in memory,
// on the same terms [orchestrateJournalPath] uses when it creates the worker.
func (a *Agent) OrchestrateNodeJournal(runID, nodeID string) (string, bool) {
	live, known := a.orchestration(strings.TrimSpace(runID))
	if !known {
		return "", false
	}
	nodeID = strings.TrimSpace(nodeID)
	found := false
	for _, node := range live.run.Snapshot().Nodes {
		if node.ID == nodeID {
			found = true
			break
		}
	}
	if !found {
		return "", false
	}
	path := orchestrateJournalPath(a.sessionID(), strings.TrimSpace(runID), nodeID)
	if path == "" {
		return "", false
	}
	if _, err := os.Stat(path); err != nil {
		return "", false
	}
	return path, true
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
	lane, _ := a.WatchOrchestrations()
	return lane
}

// WatchOrchestrations is [Agent.Orchestrations] with a way to stop, for
// [Agent.WatchTaskUpdates]' reason and on its terms: same subscription, stop
// takes the watcher off the list and ends its pump, never nil, and calling it
// twice is calling it once.
func (a *Agent) WatchOrchestrations() (<-chan Event, func()) {
	stream := newEventStream()
	a.mu.Lock()
	if a.closed {
		a.mu.Unlock()
		stream.close()
		return stream.out, func() {}
	}
	a.orchestrateWatchers = append(a.orchestrateWatchers, stream)
	a.mu.Unlock()
	var once sync.Once
	return stream.out, func() {
		once.Do(func() {
			a.mu.Lock()
			a.orchestrateWatchers = dropWatcher(a.orchestrateWatchers, stream)
			a.mu.Unlock()
			stream.leave()
		})
	}
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
// IT IS A TABLE LOOKUP AND NEVER A JUDGEMENT, and the bargain is worth stating
// because a run costs money: a wrong yes here is a wrong yes with a fuel tank
// attached. So the sentence says so in words or this path does nothing. The
// judgement it refuses to make is not lost — it is the model's, on the belt
// (tools_harness.go), where it is made once with the conversation in view
// instead of on every turn against a regular expression.
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

// orchestrateOpeners are the courtesies a request is wrapped in, stripped
// before the cue is read so that "please orchestrate X" is the same request as
// "orchestrate X". They are openers only — each is removed from the FRONT and
// the rest is re-read — so none of them can match anything in the middle of a
// sentence.
//
// They lived in harness_build.go until commissioning a harness became a tool the
// model calls (tools_harness.go), which left this the only cue in the package
// and these the only courtesies anything strips.
var orchestrateOpeners = []string{
	"please ", "can you ", "could you ", "would you ", "let's ", "lets ",
	"i want you to ", "i'd like you to ", "i would like you to ",
}

// orchestrateGoal reads one turn's request for a run: what to work on, and how
// much of somebody's money it may spend. false is every other sentence.
func orchestrateGoal(text string) (goal string, cap float64, ok bool) {
	text = strings.TrimSpace(text)
	// The openers come off one at a time, so "please can you orchestrate X" is
	// read too. The loop terminates because every pass strips a prefix.
	for stripped := true; stripped; {
		stripped = false
		for _, opener := range orchestrateOpeners {
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
		hub.send(Event{Kind: EventError, Err: err, Usage: a.sealTurn(Usage{}, started, a.Model())})
		return true, false
	}
	if id == "" {
		// The runner declined without saying why. Nothing started, so the turn
		// the person typed runs exactly as it would have.
		return false, false
	}
	a.announceOrchestrate(id, goal, model, cap)
	// The turn ends HERE, with no assistant message: the run is the answer and
	// it has not happened yet.
	hub.send(Event{Kind: EventTurnDone, Usage: a.sealTurn(Usage{}, started, a.Model())})
	return true, true
}

// announceOrchestrate says one run has started, on the standing lane where
// every other thing that run will say arrives. It is the FIRST NEWS OF A RUN and
// the only line a surface has to go on: a run is not a node, so it is on no
// roster and has no row, and this event is what tells a surface one exists at
// all (internal/tui3's roomorch.go).
//
// THE ID IS THE RUN'S NUMBER, AND A RUNNER MAY NOT HAVE ONE. Every question in
// this package is answered by [Event.ID], which is a uint64, and the engine here
// mints its ids as decimal numbers written out ([Agent.RunOrchestrate]) — so the
// number goes back on the event. A surface that was handed a runner minting ids
// of some other shape gets ID zero, which every reader already treats as "this
// names no run": the note still lands in the conversation, and what it costs is
// the page, which could not have been opened on an id the session cannot resolve
// anyway. The error is read rather than dropped so that this is a decision on
// the page and not an accident of the underscore.
func (a *Agent) announceOrchestrate(id, goal, model string, capDollars float64) {
	seq, err := strconv.ParseUint(strings.TrimSpace(id), 10, 64)
	if err != nil {
		seq = 0
	}
	a.emitOrchestrate(Event{
		Kind: EventOrchestrateNote, ID: seq,
		Text:  fmt.Sprintf("adaptive run started on %s: %s", orchestrate.Dollars(capDollars), goal),
		Hint:  orchestrate.Dollars(capDollars),
		Model: model,
	})
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
	call  roleRequest
	// request is the person's own message, verbatim, carried from the moment the
	// run was asked for. The planner reads it above the goal so that a paraphrase
	// cannot quietly become the requirement (see [renderOrchestrateView]).
	request string
}

// Plan is one call, salvaged, with the ONE repair turn this pipeline allows —
// exactly the ladder a harness design is held to (harness_build.go), for
// exactly its reason: a reply that is a good amendment wearing a code fence
// costs nothing to fix, and re-asking costs the whole view again.
func (p *orchestratePlanner) Plan(ctx context.Context, view orchestrate.View) (orchestrate.Amendment, error) {
	return p.think(ctx, []ai.Message{
		textMessage("system", orchestratePlannerBrief),
		textMessage("user", renderOrchestrateView(view, p.request)),
	})
}

// Repair is the second half of that bargain, and the half only the run can
// ask for: an amendment that PARSED and was still refused — a cancel aimed at
// a running node, a need on an id nobody minted — is handed back with the
// sentence that refused it.
func (p *orchestratePlanner) Repair(ctx context.Context, view orchestrate.View, why string) (orchestrate.Amendment, error) {
	return p.think(ctx, []ai.Message{
		textMessage("system", orchestratePlannerBrief),
		textMessage("user", renderOrchestrateView(view, p.request)),
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
	ctx = p.call.context(ctx)
	response, err := p.agent.client.CompleteWithMessages(
		provider.WithoutStream(ctx),
		messages,
		ai.WithModel(p.call.model),
		ai.WithMaxTokens(orchestratePlanTokens),
		ai.WithTemperature(orchestratePlanTemp))
	if err != nil {
		return "", err
	}
	if response == nil {
		return "", errors.New("the planner answered with nothing")
	}
	p.agent.addAuxiliaryUsage(response, p.call.model, 1)
	p.orch.Charge(orchestrateCost(response, p.call.model))
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
// The order is the argument: the person's own request, then the goal groomed
// out of it, then what they have said since (which outranks the plan), then
// facts, then the frontier, then the money.
//
// THE REQUEST IS FIRST AND IT IS VERBATIM. The goal is one model's account of
// what somebody asked for, and the planner's whole job is to cut that account
// into nodes — so a requirement the account dropped is a requirement no node
// will ever carry. Their sentence is captured by the code that started the run
// ([Agent.RunOrchestrate]) and put where the paraphrase can be checked against
// it. It is empty for a run nobody typed — a resumed one, a scripted one — and
// then this section is simply absent (task_brief.go states the emptiness law
// these briefs are written to).
func renderOrchestrateView(view orchestrate.View, request string) string {
	var out strings.Builder
	if request = strings.TrimSpace(request); request != "" {
		fmt.Fprintf(&out, "%s\n%s\n\n%s\n\n", briefAskHeading, briefAskRule, clip(request, briefAskLimit))
	}
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
//
// The last two rules carry [orchestrate.RepeatLimit] rather than a number typed
// twice, because a planner told it may retry three times by a guide that
// refuses the third is a planner reasoning from the wrong figure.
var orchestratePlannerBrief = fmt.Sprintf(orchestratePlannerLaw, orchestrate.RepeatLimit)

const orchestratePlannerLaw = `You plan an adaptive run. You never do the work.

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
  low, add only what the goal cannot be answered without.
- A NODE THAT WROTE NOTHING FAILED, whatever its digest reads like. A node given
  a write_scope that ends without writing anything comes back failed, and its
  digest begins "INCOMPLETE: nothing was written". A worker announcing what it is
  about to do is not a worker that did it — read those digests as unfinished
  work, never as a deliverable.
- THE SAME FILE IS NOT SENT OUT FOREVER. Once %d nodes aimed at one path have
  come back without writing it, the run stops taking new nodes for that path and
  goes straight to the write-up. Sending the identical brief a third time is not
  a plan. Change what is being asked for — a smaller piece, a different worker,
  or a different path — or say done and let the write-up say honestly which part
  of the goal is incomplete.`

// ── the executor ────────────────────────────────────────────────────────────

// orchestrateExec is the working half: one node, one child agent, the
// session's own loop and hands.
type orchestrateExec struct {
	agent *Agent
	call  roleRequest
	id    string
	// request is the person's own message and goal is what the run was asked to
	// do. Both ride onto EVERY node's brief ([orchestrateBrief]), because a node
	// is handed one small piece of a job it can otherwise see nothing of: the
	// planner cut that piece out of its own reading of the goal, and a node that
	// cannot see what the whole thing was for cannot notice when the cut lost
	// something. They are bounded on the way in — see [orchestrateRootBrief].
	request string
	goal    string
}

// model is the id this run's nodes actually run on: the worker tier's, and the
// conversation's where nothing resolved one.
//
// IT IS ONE FUNCTION BECAUSE TWO PLACES ASK IT — the child agent that is built
// on it ([orchestrateExec.newChild]) and the roster row that SAYS it is running
// on it ([orchestrateFamily.publish]) — and a row naming a model its node is not
// on is exactly the defect this exists to prevent. The run's nodes are the one
// class of work on this surface whose model genuinely differs from the
// conversation's with the shipped crew configured, and they were the one class
// that published nothing.
func (e *orchestrateExec) model() string {
	if model := strings.TrimSpace(e.call.model); model != "" {
		return model
	}
	e.agent.mu.Lock()
	defer e.agent.mu.Unlock()
	return e.agent.model
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

	// A RUN'S NODE IS NOT A TASK GRAPH NODE. It has no row in the tasker to hand
	// a lane back to and no sub-tasks to wait for — this executor is the
	// scheduler for its own graph — so the holder is nil and runTaskChild's
	// waiting half never runs (task_run.go).
	changed, stopped, runErr := runTaskChild(ctx, child, nil, orchestrateBrief(e.root(), node, deps, shared),
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
	// AND THE LAST QUESTION IS WHETHER THE WORK ACTUALLY HAPPENED. A node that
	// was given somewhere to write and wrote nothing did not do its job, however
	// confidently its last sentence reads — see [orchestrateUnwritten].
	if unwritten := orchestrateUnwritten(node, changed); unwritten != "" {
		return orchestrateLead(unwritten, digest), cost, errors.New(unwritten)
	}
	return digest, cost, nil
}

// orchestrateUnwritten is the run's refusal to call a node done on its own last
// words, and it is the empty string for every node that is not that case.
//
// A NODE'S FINAL REPLY IS NARRATION UNTIL SOMETHING IS ON DISK. The digest is
// the last thing the node said ([orchestrateDigest]), and a model that ends its
// turn on "Now I have both files. Let me write the synthesized report." has said
// something that reads exactly like success and is not: no write happened, the
// file the planner asked for is not there, and the planner — which sees digests
// and never a filesystem — has no way to tell the two apart. So the one fact
// this side does have, whether the node changed anything, is what decides the
// state: a node whose brief carried a WRITE SCOPE and whose run changed no file
// lands FAILED with this sentence as its error, and the planner reads it as the
// unfinished work it is.
//
// A read-only node — no write scope, which the brief spells out as "THIS IS
// READ-ONLY WORK" — is untouched: writing nothing is the whole of what it was
// asked for.
func orchestrateUnwritten(node orchestrate.Node, changed []string) string {
	if len(node.WriteScope) == 0 || len(changed) > 0 {
		return ""
	}
	return "INCOMPLETE: nothing was written — this was scoped to write " +
		strings.Join(node.WriteScope, ", ") + " and it ended without writing anything."
}

// orchestrateLead puts a warning in front of a digest without losing the digest.
func orchestrateLead(warning, digest string) string {
	if digest == "" {
		return warning
	}
	return warning + "\n" + digest
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
	// Asked before the lock is taken, because [orchestrateExec.model] takes the
	// same one: this package has a single lock order and nesting it here would be
	// the first exception to it.
	model := e.model()
	a.mu.Lock()
	parent := a.config
	window := parent.ContextWindow
	if !strings.EqualFold(strings.TrimSpace(model), strings.TrimSpace(a.model)) {
		// A window measured for another model is not a fact about this one
		// (newTaskAgent states the whole argument).
		window = 0
	}
	client := unwrapCompleter(a.client)
	journal := orchestrateJournalPath(a.sessionID(), e.id, node.ID)
	// The rung this session's own next turn would ask for, carried into the node
	// as its floor exactly as a task node inherits it (task_run.go's
	// newTaskAgent). A node of an adaptive run is the person's work at one
	// remove too, and it ran at whatever a fresh agent's zero value was.
	inherited := a.effortLocked(a.model)
	a.mu.Unlock()

	child, err := newAgent(Config{
		// An adaptive run's worker shares the project's error→fix file for a task
		// node's reason (task_run.go's newTaskAgent, fixstore.go).
		fixesDir:       a.config.fixesBucket(),
		Workspace:      dir,
		Model:          model,
		APIKey:         parent.APIKey,
		BaseURL:        parent.BaseURL,
		ContextWindow:  window,
		CompactEnabled: parent.CompactEnabled,
		SessionFile:    journal,
		EffortRole:     effort.RoleWorker,
		DefaultEffort:  inherited,
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
		Media:          parent.Media,
		MediaModel:     parent.MediaModel,
		MediaPick:      parent.MediaPick,
		DocumentEngine: parent.DocumentEngine,
	}, client)
	if err != nil {
		return nil, err
	}
	if e.call.effort != provider.EffortNone {
		child.SetReasoning(string(e.call.effort))
	}
	return child, nil
}

// spend is what one node's agent cost, and it folds that spend into the
// session's own pocket on the way past — a node's calls are the person's
// calls, exactly as a task node's are ([Agent.foldTaskUsage]).
//
// The fold goes through the auxiliary door rather than reaching into the totals
// itself, which is the same accounting through ONE seam: the figures land where
// they always did, and they are written down on the way past, so a resumed run's
// spend is still in the conversation's books tomorrow.
//
// The returned figure is the TANK's and is unchanged: the provider's own cost
// when there is one, and the price table only when there is not.
func (e *orchestrateExec) spend(child *Agent) float64 {
	used := child.Usage()
	cost := used.CostUSD
	e.agent.addAuxiliaryUsage(&ai.Response{Usage: &ai.Usage{
		PromptTokens:             used.Input,
		CompletionTokens:         used.Output,
		CacheReadInputTokens:     used.CacheRead,
		CacheCreationInputTokens: used.CacheWrite,
		Cost:                     &cost,
	}}, e.call.model, used.Calls)
	if used.CostUSD > 0 {
		return used.CostUSD
	}
	return orchestrate.MeterCall(used.Input, used.Output, e.call.model)
}

// orchestrateRootBriefLimit bounds the goal's half of what every node is told.
// The person's ask is bounded separately and more generously (briefAskLimit in
// task_brief.go) because it is the part nothing else in the run carries; a goal
// longer than this has already been cut into nodes, and each node's own goal is
// the part of it that node has to act on.
const orchestrateRootBriefLimit = 2000

// root is what every node of this run reads before its own goal: the person's
// request in their own words, then the goal the run was started with. It is
// composed once per node rather than kept, because it is two short strings and
// a run's nodes are minutes apart.
func (e *orchestrateExec) root() string {
	return orchestrateRootBrief(e.request, e.goal)
}

// orchestrateRootBrief lays the run's own contract out in the same voice a
// task's opening message uses (task_brief.go), so that a person who reads one
// node's thread and one task's thread is reading the same document twice.
//
// A RUN HAS NO SEPARATE DELIVERABLE OR DONE-CONDITION and this is where that
// shows: a run's goal is also its title on the roster and in the room header
// (roomorch.go draws it as one line), so the contract lives INSIDE the goal the
// model writes rather than in fields beside it, and run_adaptive's schema is
// what asks for it there. Everything else about the layout is decided in
// [composeBrief] and not here.
func orchestrateRootBrief(request, goal string) string {
	return composeBrief(request, clip(strings.TrimSpace(goal), orchestrateRootBriefLimit), "", "")
}

// orchestrateBrief is a node's whole world: what the run as a whole was asked
// for, then its own goal, what its prerequisites found, and the two bounds it is
// running under.
//
// UPSTREAM ARRIVES AS DIGESTS AND NOTHING ELSE. A node that could read its
// prerequisite's artifact would be a node whose context grows with the run,
// which is the shape this design exists to refuse.
//
// THE ROOT ARRIVES BOUNDED, for that same reason and it is the reason the root
// is not simply pasted in: it rides in front of every node of the run, so a
// generous copy would be paid for once per node ([orchestrateRootBrief] does the
// bounding). What it buys is the one thing a node could not otherwise have — the
// person's actual words. A planner writes each node's goal out of its own
// reading of the run, and a node holding only that reading has no way to notice
// a requirement the reading dropped.
func orchestrateBrief(root string, node orchestrate.Node, deps []orchestrate.NodeStatus, shared bool) string {
	var out strings.Builder
	if root = strings.TrimSpace(root); root != "" {
		out.WriteString(root + "\n\nYOUR PART OF IT, and the whole of what you are answerable for:\n")
	}
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
	if child.turnTruncated() {
		stopped := fmt.Sprintf("INCOMPLETE: the node's final reply was cut off at the output limit after %d continuation attempts.", truncationContinuations)
		if digest == "" {
			digest = stopped
		} else {
			digest = stopped + "\n" + digest
		}
	}
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
// agent, and everything it did should be readable with the same tools.
//
// ONE HOME, ONE SEAM. The state root comes from internal/home, exactly as
// taskJournalPath's legacy branch does, so AFORGE_HOME moves a run's
// transcripts with every other v3 file. Reading os.UserHomeDir here instead was
// the bug that left node journals in the real home while everything else in the
// process had been pointed somewhere disposable.
func orchestrateJournalPath(session, run, node string) string {
	safe := strings.Map(func(r rune) rune {
		if r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '-' || r == '_' {
			return r
		}
		return '-'
	}, node)
	return filepath.Join(RunsRoot(), session, run, safe+".jsonl")
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

// ── the run as a task FAMILY ────────────────────────────────────────────────
//
// A run is work a person handed over, exactly as a task is, and until this
// section existed it was the one kind of work with no row anywhere: the roster
// draws tasks, the run drew a note in the conversation and a page somebody had
// to know to open. So a run registers itself with the tasker — one row for the
// RUN, one per node under it — and the roster's tree has a family to draw
// (internal/tui3's taskstrip.go, whose parent seam this fills).
//
// IT REGISTERS AND IT DOES NOT ADMIT. Nothing here calls [TaskGraph.admit]:
// admission is what STARTS work, and a node's work is already running in this
// package's own executor. What the family reuses is the tasker's id sequence
// (so no run's row can ever collide with a task's) and [Agent.emitTaskUpdate],
// which is the one lane every surface reads a task's life from. What it
// deliberately does not reach is the rest of a node's afterlife — the project
// index and the note the model reads when work lands — because a run already
// says what it came to, once, in its own write-up, and a model told twelve
// times that a node finished would report each of them.
//
// WHAT THAT COSTS, said plainly: the ids these rows carry name nothing in the
// task graph, so a surface that offers to open a node's room or stop it by id
// will find no node there. The run's own page is where a node is read and
// steered ([Agent.OrchestrateSnapshot], [Agent.SteerOrchestrate]), and the row
// is a row.
//
// WHAT THE GRAPH DOES HOLD IS THE ROWS THEMSELVES, and that is not admission.
// Every notice this family publishes goes through one door
// ([orchestrateFamily.publish]) which hands the family's rows to
// [TaskGraph.keepRunRows] — not as nodes, which would make them schedulable, but
// as the last thing said about each row. That buys the two things a row that
// exists only at the instant it moves cannot have: it is REPLAYED to a lane that
// opens late ([Agent.replayTaskRoster]), so a surface that detached and came back
// gets the family whole; and it is WRITTEN DOWN with the graph (task_store.go's
// [runRecord]), so a conversation reopened tomorrow redraws yesterday's runs
// settled instead of showing an empty column beside a transcript full of them.
// The run itself still does not survive the process — nothing here resumes one.

// orchestrateFamily is one run's rows: the root, the id minted for each node,
// and the last state each was published in — which is the whole of what makes
// this ONE UPSERT PER STATE CHANGE rather than one per publish. A run publishes
// on every launch, landing, note and steer, and a roster redrawing twelve rows
// for a note is a roster nobody can read.
type orchestrateFamily struct {
	agent *Agent
	run   string
	root  uint64
	title string
	// goal is the run's whole sentence, kept because the project index wants the
	// uncut title and [orchestrateFamily.title] is the clipped one a chip draws.
	// It also has to be the uncut one because the settle row is a SECOND row for
	// the same id (see [orchestrateFamily.recordRoot]), and a closing row whose
	// slug and label were built from the clipped title would resolve as a
	// different piece of work from the row it closes.
	goal string
	// started is when the root's row was minted, and it is here so that the
	// closing row can carry how long the run took — the one figure a row about
	// finished work cannot be given after the fact.
	started time.Time
	// model is the planner's, drawn on the run's own row: it is the judgement the
	// tank is paying for, and with tiers configured it is not the model the
	// person is talking to ([Snapshot.Planner] says the same thing to the room).
	model string
	// worker is what the run's NODES run on, drawn on each of their rows, and it
	// is a second field rather than the same one because the two are different
	// models on purpose: one careful call decides what happens, many cheap ones
	// do it (roles.TierMastermind against roles.TierLow). It is settled once, at
	// the run's start, from the same [orchestrateExec.model] the child agents are
	// built with. Empty on a family nobody handed one to, which publishes nothing
	// — the emptiness law, and what every test constructing a bare family gets.
	worker string

	mu   sync.Mutex
	ids  map[string]uint64
	said map[string]TaskState
	// settled says the run's own row has been published in its final state
	// ([orchestrateFamily.settle]). It is read by exactly one thing — the namer
	// that arrives late (taskname.go) — and it is the whole of what stops a run
	// that finished from being republished as running because three words landed
	// a moment after it ended.
	settled bool
	// names is the last goal each node was published with, keyed the way ids is.
	//
	// IT EXISTS SO THAT NO ROW OF THIS RUN IS EVER PUBLISHED NAMELESS. A surface
	// draws what it is told and falls back to "task 19" when it is told nothing
	// (internal/tui3's taskTitleOf), and this file had two ways to tell it
	// nothing: [orchestrateFamily.retire], which settles a node the planner
	// dropped and never had a goal in its hand, and a snapshot node whose Goal
	// came back empty. Both now say the name this map remembers.
	names map[string]string
	// drew is the title each row was last PUBLISHED under, and it is a second map
	// rather than a reading of names because the two answer different questions:
	// names is what this node is called, drew is what a surface has been told it
	// is called. A run's namer answers a second or two after a node is admitted
	// (internal/orchestrate's [orchestrate.Options.Name]), and the difference
	// between those two strings is the whole of how [orchestrateFamily.claim]
	// knows a row needs redrawing when nothing about the work itself moved.
	drew map[string]string
	// born counts the node rows this family has ever minted, and formed says the
	// first amendment has landed and been published.
	//
	// THEY EXIST FOR THE MINUTE BEFORE THERE IS ANYTHING TO DRAW. A run's own row
	// is minted the instant somebody asks for it, and the workers under it cannot
	// exist until the opening planner call comes back — the one call in a run that
	// has no work to overlap it with (internal/orchestrate's [Orchestrator.Run]).
	// For that whole minute the row sat there saying nothing while the machine was
	// doing the most consequential thinking of the run, and a person watching it
	// had no way to tell the difference between that and nothing happening at all.
	// [orchestrateFamily.formingLocked] is the line that fills the gap, and these
	// two are the only state behind it: no clock, no animation, nothing that
	// advances except a worker actually arriving.
	born   int
	formed bool

	// rows is the LAST NOTICE published for every row of this family, and shown
	// is the order those rows were first published in — the run's own row, then
	// each worker as it was minted.
	//
	// THEY EXIST BECAUSE THE NOTICE IS THE ONLY RECORD THIS FAMILY EVER HAD. A
	// run's rows are not nodes in the task graph (the section header at the top
	// of this seam says why they are not), so nothing walks them the way
	// [Agent.replayTaskRoster] walks the graph: they existed only at the instant
	// they moved, and a surface that detached and came back — home switched away
	// and switched back, a conversation reopened tomorrow — held a column with a
	// live run's whole family missing from it and no way to ask for it again.
	//
	// So the family REMEMBERS WHAT IT SAID, and that memory is what both answers
	// are built from: the replay is these notices sent again, and the checkpoint
	// is these notices written down (task_store.go's [runRecord]). Remembering
	// the notice rather than rebuilding one is the whole of what makes a replayed
	// row and a live row impossible to tell apart — there is no second builder to
	// drift.
	rows  map[uint64]TaskNotice
	shown []uint64

	// say serializes PUBLISHING, which f.mu deliberately does not.
	//
	// f.mu guards this family's bookkeeping and is released before anything slow.
	// This one is held across the whole of [orchestrateFamily.publish], so the
	// order rows reach a watcher is the order they reach the disk, and a snapshot
	// built a moment earlier can never overtake a fresher one on the way out. A
	// run publishes from several goroutines at once — the frontier's own, and the
	// namer that answers a second or two late (taskname.go) — which is exactly
	// that race.
	say sync.Mutex
}

// publish is this family's ONE DOOR onto the roster: it remembers the notice,
// sends it, and hands the family's rows to the graph that writes them down.
//
// THE ORDER OF THOSE THREE IS NOT ARBITRARY. The notice is remembered first so
// that a lane opening in the same breath replays a row no older than the one
// live watchers just saw; the send is next because a person watching is owed the
// news before a file is; the keep is last because it touches the disk.
//
// THE LOCKS GO ONE WAY: say, then f.mu, then the agent's, then the graph's —
// with the store's between the last two on a write ([taskStore] spells that
// half). Nothing holds f.mu across the send or the keep, and NOTHING IN THE
// GRAPH OR THE STORE EVER TAKES EITHER OF THIS FAMILY'S LOCKS: the family pushes
// its rows outward and the graph never reaches in for them, which is what leaves
// [TaskGraph.document] and [Agent.replayTaskRoster] free to walk those rows under
// the graph's own lock and closes no cycle.
func (f *orchestrateFamily) publish(notice TaskNotice) {
	if f == nil || f.agent == nil {
		return
	}
	f.say.Lock()
	defer f.say.Unlock()
	f.mu.Lock()
	if f.rows == nil {
		f.rows = make(map[uint64]TaskNotice, 8)
	}
	if _, drawn := f.rows[notice.ID]; !drawn {
		f.shown = append(f.shown, notice.ID)
	}
	f.rows[notice.ID] = notice
	// The slice handed out is BUILT FRESH AND NEVER TOUCHED AGAIN, which is what
	// lets the graph hold it without a copy: every publish makes a new one, so
	// what the graph is holding is a snapshot of one instant rather than a window
	// onto a map that keeps moving.
	rows := make([]TaskNotice, 0, len(f.shown))
	for _, id := range f.shown {
		rows = append(rows, f.rows[id])
	}
	root := f.root
	f.mu.Unlock()

	f.agent.emitTaskUpdate(notice)
	f.agent.graph().keepRunRows(root, rows)
}

// newOrchestrateFamily takes the run's own row. It is minted before the first
// planner call so the roster shows the run from the moment somebody asked for
// it — a run that appeared only once a node landed would be a minute of a
// person watching nothing happen.
func (a *Agent) newOrchestrateFamily(goal, planner string, runID ...string) *orchestrateFamily {
	var run string
	if len(runID) > 0 {
		run = runID[0]
	}
	family := &orchestrateFamily{
		agent:   a,
		run:     strings.TrimSpace(run),
		root:    a.graph().reserve(),
		title:   clip(firstLine(goal), hintLimit),
		goal:    strings.TrimSpace(goal),
		started: time.Now(),
		model:   strings.TrimSpace(planner),
		ids:     make(map[string]uint64, 8),
		said:    make(map[string]TaskState, 8),
		names:   make(map[string]string, 8),
		drew:    make(map[string]string, 8),
	}
	family.publish(TaskNotice{
		ID: family.root, Run: family.run, Title: family.title, State: TaskRunning, Model: family.model,
		// AND IT SAYS SO FROM ITS FIRST BREATH. The row is minted here, before the
		// opening planner call, so this is the moment the forming line has to start
		// — a row published bare and only filled in later would leave the gap it
		// exists to close ([orchestrateFamily.formingLocked]).
		Doing: orchestrateForming,
	})
	a.mu.Lock()
	session := a.sessionID()
	a.mu.Unlock()
	// THE ROW IS WRITTEN LIVE, AND IT IS THE ONE ROW IN THIS FILE THAT IS. Every
	// other row the project index holds is written when work LANDED; this one
	// says "running" so the "@" list and the `tasks` tool can see a run that is
	// still going, and it is a promise that a second row will close it —
	// [orchestrateFamily.settle] keeps that promise when the run ends inside this
	// process, and [Agent.closeInflightTaskIndexRows] keeps it when the process
	// went away instead. A row left saying "running" is a project's record of a
	// present that ended hours ago.
	a.recordTaskIndexEntry(TaskIndexEntry{
		ID:    strconv.FormatUint(family.root, 10),
		Name:  TaskSlug(family.goal),
		Label: taskLabel(family.goal),
		Title: family.goal,
		// A RUN THAT PLANS ITSELF SAYS SO ON ITS ROW, from the first breath and
		// on the row that closes it: this is the one seam that knows, because an
		// adaptive run has no TaskNode to carry a kind for it
		// (session's TaskKindAdaptive).
		Kind:          TaskKindAdaptive,
		Status:        string(TaskRunning),
		EndedAt:       family.started,
		SessionID:     session,
		TranscriptURI: orchestrateFamilyURI(session, family.run),
	})
	return family
}

// reserveRunNames pushes this session's run counter past every run a restored
// checkpoint remembers, so no run of this life wears the name of one already on
// the column.
//
// A RUN'S NAME IS NOT A ROW ID. Rows come from the graph's `seq`, which the
// checkpoint carries across lives; a run's name is [Agent.orchestrateSeq], which
// starts at one in every process because a run has never before outlived one.
// Now that yesterday's rows come back, run 1 of today would sit beside run 1 of
// yesterday under two different roots — and, worse, would write its nodes'
// journals into that run's folder ([orchestrateJournalPath] keys on the
// conversation and the name). One line at recovery keeps the names unique for as
// long as the conversation is.
func (a *Agent) reserveRunNames(records []runRecord) {
	var highest uint64
	for _, record := range records {
		// Anything that is not a number is a run a test named by hand; there is
		// nothing to reserve past it and nothing to be confused by.
		if named, err := strconv.ParseUint(strings.TrimSpace(record.Run), 10, 64); err == nil && named > highest {
			highest = named
		}
	}
	if highest == 0 {
		return
	}
	a.mu.Lock()
	if a.orchestrateSeq < highest {
		a.orchestrateSeq = highest
	}
	a.mu.Unlock()
}

// orchestrateFamilyURI is the TRANSCRIPT a run's own row points at.
//
// IT NAMES THE SYNTHESIS NODE AND NOT THE RUN'S FOLDER. This used to hand back
// the directory the run's nodes are written into, and nothing downstream ever
// distinguishes a folder from a journal: the card drew `transcript ·
// …/runs/<session>/<run>` for something that could not be opened, and then said
// the file was gone or could not be read — three sentences about a path that was
// never a transcript in the first place. The run's closing call IS its
// transcript — it is the node that reads what every worker produced and writes
// the run's own report ([orchestrate.SynthesisID]) — so the row points there,
// and the card can peek it like any other row's.
//
// A run still in flight has not written that file yet, and the card's own
// `Kept` sentence is the honest answer for the seconds that is true of.
func orchestrateFamilyURI(session, run string) string {
	path := orchestrateJournalPath(session, run, orchestrate.SynthesisID)
	if path == "" {
		return ""
	}
	return taskURI(path)
}

// upsert is [orchestrate.Options.OnNodes]: the crystallized graph, every time
// any of it moves.
func (f *orchestrateFamily) upsert(nodes []orchestrate.NodeStatus) {
	if f == nil {
		return
	}
	live := make(map[string]bool, len(nodes))
	for _, node := range nodes {
		live[node.ID] = true
		state, stopped := orchestrateTaskState(node.State)
		// THE NAME IS READ OFF THE NODE AND NOWHERE ELSE. The frontier settles it
		// once, at the one place it is written (internal/orchestrate's apply), and
		// a name the run's namer lands afterwards lands on that same field — so a
		// row rebuilt from the id here would be a second answer that never learns
		// what the first one did, which is precisely how `r1` stayed on a rail
		// while the node behind it was called something a person could read.
		title := f.name(node.ID, node.Title)
		id, moved, renamed := f.claim(node.ID, state, title)
		if !moved && !renamed {
			continue
		}
		f.publish(TaskNotice{
			ID:     id,
			Run:    f.run,
			Node:   node.ID,
			Parent: f.root,
			// THE NAME IS NEVER PUBLISHED EMPTY ONCE THERE IS ONE. A snapshot whose
			// node came back nameless — an amendment mid-flight, a namer still out —
			// keeps the name the row was last drawn under, and a surface draws its own
			// answer for a row nobody has named yet ([orchestrateFamily.names] says
			// the rest).
			Title:   title,
			State:   state,
			Stopped: stopped,
			// WHAT THIS NODE IS RUNNING ON. It was missing, and it was missing on
			// exactly the rows it mattered most for: a run's nodes are the one class
			// of work whose model genuinely differs from the conversation's under the
			// shipped crew, so a person who opened one of these rooms to check that
			// their crew was doing anything found the model line blank. The root row
			// beside them has always carried the planner's ([Agent.RunOrchestrate]).
			Model:   f.worker,
			Report:  orchestrateNodeReport(node),
			CostUSD: node.Cost,
		})
		// AND THE PROJECT'S RECORD IS WRITTEN ON THE MOVE AND NEVER ON THE RENAME.
		// A settled state reaches the append-only index exactly once; a name that
		// arrived after the work landed is a row already filed, and filing it again
		// would be two rows for one node in a person's history.
		if moved && state.settled() {
			f.recordNode(id, node, state)
		}
	}
	f.retire(live)
	// AND THE FORMING IS OVER THE MOMENT THE FIRST WORKER EXISTS. Nothing
	// lingers afterwards: from here the rows themselves are what a person reads,
	// and the fold they already sit under is the collapsed view of them
	// (internal/tui3's railShut).
	f.formingDone()
}

// sayForming publishes the run's own row wearing the one line it shows while
// its workers are still being formed. It is the ordinary running row with
// TaskNotice.Doing filled: a surface draws that instead of a state word, so this
// needs no field, no event kind and no rendering of its own.
func (f *orchestrateFamily) sayForming() {
	if f == nil {
		return
	}
	f.mu.Lock()
	if f.settled {
		f.mu.Unlock()
		return
	}
	line, title := f.formingLocked(), f.title
	f.mu.Unlock()
	f.publish(TaskNotice{
		ID: f.root, Run: f.run, Title: title, State: TaskRunning, Model: f.model, Doing: line,
	})
}

// formingDone ends the forming line once there is at least one worker to look
// at, and says so exactly once.
func (f *orchestrateFamily) formingDone() {
	if f == nil {
		return
	}
	f.mu.Lock()
	over := !f.formed && f.born > 0
	f.formed = f.formed || over
	f.mu.Unlock()
	if over {
		f.sayForming()
	}
}

// orchestrateForming is the line the run's row wears while its workers are being
// formed. IT IS PLAIN ENGLISH IN THE PRESENT TENSE and not the name of any
// machinery: what is happening is that the shape of the work is being decided,
// and "forming the work" is what a person would call that.
const orchestrateForming = "forming the work"

// formingLocked is the line itself, and there are exactly two answers because
// there are exactly two states: this run has no workers yet, or it has some.
//
//	forming the work        the opening call is out and there is nothing to draw
//	(nothing)               the workers are the picture from here
//
// NOTHING IN IT ADVANCES ON A CLOCK. A spinner that counted seconds would be
// this surface asserting progress it cannot see; the line changes when a worker
// actually exists and at no other moment.
func (f *orchestrateFamily) formingLocked() string {
	if f.formed || f.born > 0 {
		return ""
	}
	return orchestrateForming
}

// recordNode is the adaptive scheduler's landing seam. OnNodes can publish a
// node many times, but claim admits one transition only, so a settled state
// reaches the append-only index exactly once.
func (f *orchestrateFamily) recordNode(id uint64, node orchestrate.NodeStatus, state TaskState) {
	f.agent.mu.Lock()
	session := f.agent.sessionID()
	f.agent.mu.Unlock()
	// THE ROW IS FILED UNDER THE NODE'S NAME, exactly as a task's row is filed
	// under its title (task_index.go). Home's cards draw the Label and fall back
	// to the Title, so a row built from the goal put a paragraph of second-person
	// brief where a card wanted two words.
	title := f.name(node.ID, node.Title)
	if title == "" {
		// AND THE ID IS THE LAST RESORT HERE AND ONLY HERE. A live row that has no
		// name yet is drawn as nothing and renamed a second later, because a person
		// is looking at it and the id would be a lie dressed as an answer; this row
		// is the project's permanent record, written once, at the moment the work
		// landed — and a node that failed in the second before its namer answered
		// would otherwise be dropped from the index entirely (an entry with no title
		// is not written, task_index.go). The machine's filing beats no record.
		title = orchestrate.NodeTitle(node.Node)
	}
	f.agent.recordTaskIndexEntry(TaskIndexEntry{
		ID:     strconv.FormatUint(id, 10),
		Parent: strconv.FormatUint(f.root, 10),
		Name:   TaskSlug(title),
		Label:  taskLabel(title),
		Title:  title,
		// A worker of an adaptive run is part of one, and its row says so for
		// the same reason its root's does (session's TaskKindAdaptive).
		Kind:          TaskKindAdaptive,
		Status:        string(state),
		Outcome:       taskOutcome(orchestrateNodeReport(node)),
		Cost:          node.Cost,
		EndedAt:       time.Now(),
		SessionID:     session,
		TranscriptURI: taskURI(orchestrateJournalPath(session, f.run, node.ID)),
	})
}

// name is what one node of this run is CALLED: the node's own title — the one
// field the frontier settles ([orchestrate.Node.Title]) — or the last name it
// was published under when this snapshot has none.
//
// THE NAME IS NOT CUT OUT OF THE GOAL, and that is the whole of what this
// function used to get wrong. A planned node's goal is its whole world, written
// to a worker who can see nothing else, so law 4 has the planner open it by
// telling that worker what it is — and a title cut from its first line named
// every worker of a nine-way run "You are a". The planner is asked for a name
// now, a node that arrives without one is named by the same small model every
// other piece of work in this package is named by (taskname.go), and a node
// whose namer has not answered yet is published with NO name rather than with
// the id it was filed under.
//
// It remembers as it answers, which is why it is one function and not two: every
// publish goes through here, so the name a row was last given is always the name
// the next nameless publish will use.
func (f *orchestrateFamily) name(node, title string) string {
	title = clip(firstLine(strings.TrimSpace(title)), hintLimit)
	f.mu.Lock()
	defer f.mu.Unlock()
	if title == "" {
		return f.names[node]
	}
	if f.names == nil {
		f.names = make(map[string]string, 8)
	}
	f.names[node] = title
	return title
}

// claim is the id for one node and whether there is anything to say about it:
// whether it MOVED, and whether it was RENAMED. The mint and the de-dup are one
// critical section because a publish can arrive from any of the run's
// goroutines, and two of them racing here would be two rows for one node.
//
// THE TWO ANSWERS ARE SEPARATE BECAUSE THEY BUY DIFFERENT THINGS. A move is news
// a surface draws AND a landing the project's index records; a rename is news a
// surface draws and nothing else — the run's namer answers a second or two after
// the node was admitted, and a node sitting on the frontier behind its needs
// would otherwise wait for its next transition to learn its own name, which for
// the last node of a wide run is the whole run.
//
// IT IS ALSO WHERE A WORKER'S EXISTENCE IS COUNTED, because this is the one
// place that ever learns it, and it learns it once — under the lock that makes
// it once ([orchestrateFamily.formingLocked] is what reads the count).
func (f *orchestrateFamily) claim(node string, state TaskState, title string) (id uint64, moved, renamed bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	id, known := f.ids[node]
	if !known {
		id = f.agent.graph().reserve()
		f.ids[node] = id
		f.born++
	}
	if f.drew == nil {
		f.drew = make(map[string]string, 8)
	}
	drawn, everDrawn := f.drew[node]
	renamed = everDrawn && drawn != title
	f.drew[node] = title
	if said, seen := f.said[node]; seen && said == state {
		return id, false, renamed
	}
	f.said[node] = state
	return id, true, renamed
}

// retire settles the rows of nodes that have LEFT THE GRAPH. An amendment that
// cancels pending work deletes it outright (internal/orchestrate's amend.go) —
// the planner changing its mind about work nobody has started — and a row left
// saying "queued" for it would be the roster holding a queue that no longer
// exists.
//
// It settles as STOPPED rather than failed, on TaskNotice.Stopped's own terms:
// nothing went wrong with the work and nobody made a finding about it, it was
// called off. A node that already settled is left alone.
func (f *orchestrateFamily) retire(live map[string]bool) {
	f.mu.Lock()
	var gone []struct {
		node string
		id   uint64
	}
	for node, id := range f.ids {
		if live[node] {
			continue
		}
		switch f.said[node] {
		case TaskQueued, TaskRunning:
			f.said[node] = TaskFailed
			gone = append(gone, struct {
				node string
				id   uint64
			}{node: node, id: id})
		}
	}
	f.mu.Unlock()
	for _, row := range gone {
		// IT KEEPS ITS NAME ON THE WAY OUT. This is the one notice in the file that
		// has no snapshot behind it — the node is gone from the graph, which is why
		// it is being settled — so the title comes from what it was last published
		// as ([orchestrateFamily.names]). Without it the last thing a surface heard
		// about this row was nameless, and a row that lost its name at the moment it
		// stopped is the row a person is most likely to be asking about.
		f.publish(TaskNotice{
			ID: row.id, Run: f.run, Node: row.node, Parent: f.root,
			Title: f.name(row.node, ""), State: TaskFailed, Stopped: true,
		})
	}
}

// settle closes the family: every node in its final state, then the run's own
// row. The three endings are the three the write-up already distinguishes
// ([Agent.landOrchestrate]) — somebody stopped it, it broke, or it landed — and
// the report is the run's answer where there is one, because that is what the
// row's card is asked to show.
func (f *orchestrateFamily) settle(snap orchestrate.Snapshot, err error) {
	if f == nil {
		return
	}
	f.upsert(snap.Nodes)
	// THE ROW IS CLOSED, AND IT IS THE LAST THING SAID ABOUT IT. The flag is
	// raised under the same lock the title is read under, so a namer that comes
	// back after this point writes its answer and publishes nothing
	// (taskname.go).
	f.mu.Lock()
	f.settled = true
	title := f.title
	f.mu.Unlock()
	notice := TaskNotice{
		ID: f.root, Run: f.run, Title: title, State: TaskDone, Model: f.model,
		Report:  strings.TrimSpace(snap.Answer),
		CostUSD: snap.Fuel.Spent,
	}
	switch {
	case snap.Stopped:
		// A stopped run settles like a stopped node: failed, because nothing was
		// finished, and stopped beside it, because "failed" would send somebody
		// looking for a fault that is their own decision.
		notice.State, notice.Stopped = TaskFailed, true
	case err != nil:
		notice.State = TaskFailed
		if notice.Report == "" {
			notice.Report = err.Error()
		}
	}
	f.publish(notice)
	// AND THE PROJECT'S RECORD IS CLOSED IN THE SAME BREATH. The roster reads the
	// notice above and the roster dies with the window; the index outlives it, and
	// until this row is appended the file still says this run is running — which
	// is what a person saw for hours after the work had actually finished.
	f.recordRoot(notice, snap)
}

// recordRoot writes the run's SECOND index row: what the whole run came to.
//
// The launch minted a row saying "running" with no price on it, because at that
// moment there was neither an ending nor a bill. This is the row that closes it
// — the ending, the run's answer, the planner's model, how long it took and the
// whole tank — and it is APPENDED rather than edited because that is this file's
// own idiom: a resolution landing after the work did writes a second row, and
// every reader takes the newest row per (sessionId, id) (task_index.go).
//
// THE ROOT'S FIGURE IS THE TANK, NOT THE SUM OF ITS CHILDREN. Every planner
// call and the closing synthesis bill against the run and belong to no node
// (internal/orchestrate's fuel.go), so the root's cost minus its children's is
// exactly what the orchestration itself cost — a share that reached no file at
// all before this row existed.
func (f *orchestrateFamily) recordRoot(notice TaskNotice, snap orchestrate.Snapshot) {
	f.agent.mu.Lock()
	session := f.agent.sessionID()
	f.agent.mu.Unlock()
	f.agent.recordTaskIndexEntry(TaskIndexEntry{
		ID:            strconv.FormatUint(f.root, 10),
		Name:          TaskSlug(f.goal),
		Label:         taskLabel(f.goal),
		Title:         f.goal,
		Kind:          TaskKindAdaptive,
		Status:        string(notice.State),
		Outcome:       taskOutcome(notice.Report),
		Cost:          snap.Fuel.Spent,
		Model:         f.model,
		DurationMS:    time.Since(f.started).Milliseconds(),
		EndedAt:       time.Now(),
		SessionID:     session,
		TranscriptURI: orchestrateFamilyURI(session, f.run),
	})
}

// orchestrateTaskState maps one node's state onto the tasker's, and says
// whether the row is a STOPPED one beside it.
//
// The two vocabularies are not the same size, and the join is at [Cancelled]:
// the tasker has no stopped STATE — a stopped node settles as failed and says
// so with a flag (task_contract.go's TaskNotice.Stopped) — so a cancelled node
// arrives as exactly that pair. Ready collapses into queued because the
// difference between "waiting on a prerequisite" and "waiting on a lane" is a
// distinction the run's own page draws and the roster does not.
func orchestrateTaskState(state orchestrate.State) (TaskState, bool) {
	switch state {
	case orchestrate.Running:
		return TaskRunning, false
	case orchestrate.Done:
		return TaskDone, false
	case orchestrate.Failed:
		return TaskFailed, false
	case orchestrate.Cancelled:
		return TaskFailed, true
	}
	return TaskQueued, false
}

// orchestrateNodeReport is what a settled node's row says: its digest when it
// worked, its error when it did not, and nothing at all while it is still
// going — an unknown is nothing, never a placeholder.
func orchestrateNodeReport(node orchestrate.NodeStatus) string {
	switch node.State {
	case orchestrate.Done:
		return node.Digest
	case orchestrate.Failed:
		return firstLine(node.Err)
	}
	return ""
}
