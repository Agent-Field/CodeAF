package orchestrate

// WHAT THE PLANNER IS ALLOWED TO SAY, and what happens to a sentence that is
// not one of those things.
//
// A planner is a model, so its answer arrives as text that is meant to be an
// [Amendment] and sometimes is not. Three separate failures live on that path
// and they are answered separately, cheapest first:
//
//	the reply is not JSON      the salvage ladder (internal/subharness) fixes
//	                           the fence, the prose, the smart quotes; the
//	                           session's planner spends ONE repair turn on what
//	                           is left (orchestrate.go's plannerRepair)
//	the JSON is not an         strict decoding refuses it: a key nobody asked
//	amendment                  for is a model answering a different question
//	the amendment is not       validation here refuses it against the frontier
//	legal against THIS run     as it stands, and a planner that can be handed
//	                           its own refusal ([Repairer]) gets one try
//
// What survives all three is applied. What does not is DROPPED WITH A NOTE —
// never guessed at, never half-applied. A run whose planner is talking nonsense
// keeps executing the frontier it already has, which is the only safe reading
// of "the scheduler never thinks".

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"path"
	"strings"

	"github.com/Agent-Field/aforge-v2/internal/subharness"
)

// RepeatLimit is how many times one run may send work at the same file before
// it stops sending it at all.
//
// TWO, because the first failure is news and the second is a pattern. A planner
// that reads a node's digest and cannot see the filesystem has exactly one way
// to react to work that did not land — send it again — and nothing in the loop
// ever tires of that: a real run spent eight nodes and twenty-seven minutes
// re-issuing one brief ("write the final report to research/…md"), every one of
// which came back saying it was about to do the work, and the file was never
// there. The third attempt is where a run has to stop guessing and say so.
const RepeatLimit = 2

// repeatedWork is [Orchestrator.validate]'s refusal of an add aimed at a file
// this run has already failed to write [RepeatLimit] times.
//
// It is a TYPE and not a sentence because it is the one refusal that is not
// repairable. Every other thing validate rejects is a planner that phrased an
// amendment wrongly and can be shown the phrasing; this is a planner whose
// judgement is right and whose work is not landing, and handing it back for a
// re-word would buy one more identical node.
type repeatedWork struct {
	scope string
	tries int
}

// Error is what the run says out loud when it stops repeating itself. It names
// the file, the count and the decision, because "this run stopped" with no
// figure behind it is a sentence somebody has to go and check.
func (r *repeatedWork) Error() string {
	return fmt.Sprintf("%s has been handed out %d times and nothing was written to it; "+
		"this run stops asking for it and reports what is actually there", r.scope, r.tries)
}

// brief is what the write-up is asked for instead of the plan the planner never
// got to write. It exists so that a run which gave up says so IN THE ANSWER,
// rather than settling quietly and leaving a person to notice the missing file.
func (r *repeatedWork) brief() string {
	return fmt.Sprintf("Report what actually happened. %s was handed out %d times and nothing was ever "+
		"written to it, so that part of the goal is incomplete. Say plainly which parts were answered "+
		"and which were not, and do not describe the unwritten part as finished.", r.scope, r.tries)
}

// Repairer is a Planner that can be shown its own refusal. It is optional: a
// planner that does not implement it simply loses the amendment it got wrong.
//
// It exists because the two halves of "is this amendment legal" live in two
// places — the JSON in the model's caller, the frontier in this package — and
// only this side knows that cancelling a running node broke the commitment
// law. Handing that sentence back is one small call; re-planning from scratch
// on the next completion is not.
type Repairer interface {
	Repair(ctx context.Context, v View, why string) (Amendment, error)
}

// Empty reports the NOOP: the planner looked, and there is nothing to change.
// It is the expected answer to most completions and it costs the run nothing.
func (a Amendment) Empty() bool {
	return len(a.Add) == 0 && len(a.Cancel) == 0 && strings.TrimSpace(a.Note) == "" && a.Done == nil
}

// ParseAmendment reads one planner reply.
//
// SILENCE IS A NOOP. A planner that answered with nothing, with whitespace, or
// with the word every model reaches for when it has nothing to say is not an
// error — it is the commonest legal answer there is, and a run that treated it
// as a failure would put a note on the person's screen after every completion.
func ParseAmendment(raw string) (Amendment, error) {
	text := strings.TrimSpace(raw)
	if text == "" || strings.EqualFold(text, "noop") || strings.EqualFold(text, "{}") {
		return Amendment{}, nil
	}
	data, err := subharness.Salvage(text)
	if err != nil {
		return Amendment{}, err
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	var amendment Amendment
	if err := decoder.Decode(&amendment); err != nil {
		return Amendment{}, fmt.Errorf("that is not an amendment: %w. The keys are add, cancel, note, done", err)
	}
	return amendment, nil
}

// absorb applies one planner answer, or says why it did not.
func (o *Orchestrator) absorb(ctx context.Context, landed thought) {
	// A STOPPED RUN TAKES NO AMENDMENT. The planner may well have been mid-call
	// when somebody cancelled, and a node added to a frontier nothing will ever
	// launch from is a chip a person watches sit there forever.
	if o.wasStopped() {
		return
	}
	if landed.err != nil {
		// A planner that could not answer is a NOOP with a line about it. The
		// frontier is untouched and the run carries on: thought is an amendment
		// to execution, never a prerequisite for it.
		o.note("the planner did not answer: " + landed.err.Error())
		return
	}
	amendment := landed.amendment
	if amendment.Empty() {
		return
	}
	if err := o.validate(amendment); err != nil {
		var repeat *repeatedWork
		if errors.As(err, &repeat) {
			// THE ONE REFUSAL THAT ENDS THE RUN. The planner is not confused, it is
			// stuck, and one more turn of the loop is one more child agent spending
			// money on the same brief (see [repeatedWork]).
			o.stopRepeating(repeat)
			return
		}
		repaired, ok := o.repair(ctx, err)
		if !ok {
			o.note("the planner's amendment was dropped: " + err.Error())
			return
		}
		amendment = repaired
	}
	o.apply(amendment)
}

// stopRepeating turns the run towards its write-up instead of towards another
// identical node.
//
// It sets the same two things a DonePlan sets — finishing, and the brief the
// synthesis is written to — so the run ends down the path it already had rather
// than through a new kind of ending nothing else knows about: the loop stops
// launching ([Orchestrator.launch]), stops paying for planner calls
// ([Orchestrator.worthThinking]), lets what is in flight come home, and writes
// up what is actually there. It is NOT a stop: a stop is a person's decision and
// skips the synthesis, and the whole point here is that somebody is owed an
// honest account.
func (o *Orchestrator) stopRepeating(repeat *repeatedWork) {
	o.mu.Lock()
	if o.finishing || o.stopped {
		o.mu.Unlock()
		return
	}
	o.finishing = true
	o.plan = &DonePlan{Brief: repeat.brief()}
	o.mu.Unlock()
	o.publish()
	o.note(repeat.Error())
}

// repair is the one retry: the planner is shown the frontier and the sentence
// that refused it, and whatever it answers is held to exactly the same law.
func (o *Orchestrator) repair(ctx context.Context, why error) (Amendment, bool) {
	fixer, ok := o.planner.(Repairer)
	if !ok {
		return Amendment{}, false
	}
	amendment, err := fixer.Repair(ctx, o.view(), why.Error())
	if err != nil || amendment.Empty() {
		return Amendment{}, false
	}
	if err := o.validate(amendment); err != nil {
		o.note("the planner's repair was refused too: " + err.Error())
		return Amendment{}, false
	}
	return amendment, true
}

// validate holds one amendment against the run as it stands.
//
// THE CANCEL RULE IS THE COMMITMENT LAW and it is the reason this function is
// not a schema check: whether a cancel is legal depends on what that node is
// doing at this instant, which is a fact only the run has.
func (o *Orchestrator) validate(amendment Amendment) error {
	o.mu.Lock()
	defer o.mu.Unlock()

	dropping := map[string]bool{}
	for _, cancel := range amendment.Cancel {
		id := strings.TrimSpace(cancel.ID)
		if id == "" {
			return fmt.Errorf("a cancel with no id")
		}
		node, known := o.index[id]
		if !known {
			return fmt.Errorf("cancel names %q, which is not a node in this run", id)
		}
		switch node.State {
		case Queued, Ready:
		case Running:
			return fmt.Errorf("cancel names %q, which is RUNNING: an amendment adds and cancels pending work, and never retracts a judgement the run is already acting on", id)
		default:
			return fmt.Errorf("cancel names %q, which has already finished: a completed node is a fact", id)
		}
		if dropping[id] {
			return fmt.Errorf("cancel names %q twice", id)
		}
		dropping[id] = true
	}

	adding := map[string]bool{}
	for _, node := range amendment.Add {
		id := strings.TrimSpace(node.ID)
		if id == "" {
			return fmt.Errorf("a node with no id")
		}
		if _, known := o.index[id]; known {
			return fmt.Errorf("node %q already exists in this run: an id is minted once and names one piece of work", id)
		}
		if adding[id] {
			return fmt.Errorf("node %q is added twice in one amendment", id)
		}
		if strings.TrimSpace(node.Goal) == "" {
			return fmt.Errorf("node %q has no goal: a node's brief is its whole world and it must stand on its own", id)
		}
		adding[id] = true
	}
	for _, node := range amendment.Add {
		if repeat := o.repeatedLocked(node); repeat != nil {
			return repeat
		}
	}
	for _, node := range amendment.Add {
		for _, need := range node.Needs {
			need = strings.TrimSpace(need)
			switch {
			case need == node.ID:
				return fmt.Errorf("node %q needs itself", node.ID)
			case dropping[need]:
				return fmt.Errorf("node %q needs %q, which the same amendment cancels", node.ID, need)
			case adding[need]:
			default:
				if _, known := o.index[need]; !known {
					return fmt.Errorf("node %q needs %q, which is not a node in this run", node.ID, need)
				}
			}
		}
	}
	return o.acyclicLocked(amendment.Add)
}

// repeatedLocked is the loop guard: how many times this run has already sent
// work at the file this node wants to write, and come back with nothing.
//
// IT COUNTS FAILURES AND NOT ATTEMPTS. A file two nodes wrote successfully is a
// file the run is making progress on, and a third node touching it is ordinary
// — the collision edges in [Orchestrator.collisionsLocked] are what handle that.
// What this counts is the shape that does not converge: node after node aimed at
// one path, each one ending without the path existing. The session's executor is
// what turns "wrote nothing it was scoped to write" into a failure rather than a
// confident digest (session's orchestrate.go), and this is what stops the run
// once that has happened twice.
//
// A node with no write scope is never counted and never refused: it is read-only
// work, and there is no file for it to fail to produce.
func (o *Orchestrator) repeatedLocked(node Node) *repeatedWork {
	for _, want := range node.WriteScope {
		want = strings.TrimSpace(want)
		if want == "" {
			continue
		}
		tries := 0
		for _, other := range o.nodes {
			if other.State != Failed {
				continue
			}
			if scopesCollide([]string{want}, other.WriteScope) {
				tries++
			}
		}
		if tries >= RepeatLimit {
			return &repeatedWork{scope: want, tries: tries}
		}
	}
	return nil
}

// acyclicLocked refuses a batch of nodes that need each other in a circle.
// Edges into nodes the run already holds are safe by construction — those
// nodes exist and this one does not — so only the new ids can close a loop.
func (o *Orchestrator) acyclicLocked(add []Node) error {
	fresh := make(map[string]Node, len(add))
	for _, node := range add {
		fresh[node.ID] = node
	}
	const (
		walking = 1
		clear   = 2
	)
	mark := make(map[string]int, len(add))
	var walk func(id string) error
	walk = func(id string) error {
		switch mark[id] {
		case clear:
			return nil
		case walking:
			return fmt.Errorf("the added nodes need each other in a circle, at %q", id)
		}
		mark[id] = walking
		for _, need := range fresh[id].Needs {
			if _, isNew := fresh[need]; !isNew {
				continue
			}
			if err := walk(need); err != nil {
				return err
			}
		}
		mark[id] = clear
		return nil
	}
	for _, node := range add {
		if err := walk(node.ID); err != nil {
			return err
		}
	}
	return nil
}

// apply is the only writer of the frontier. It runs AFTER validation and it
// cannot fail: everything that could be refused already was.
func (o *Orchestrator) apply(amendment Amendment) {
	o.mu.Lock()
	for _, cancel := range amendment.Cancel {
		o.dropLocked(strings.TrimSpace(cancel.ID))
	}
	for _, node := range amendment.Add {
		node.ID = strings.TrimSpace(node.ID)
		status := &NodeStatus{Node: node, State: Queued}
		status.Needs = append(status.Needs, o.collisionsLocked(node)...)
		o.nodes = append(o.nodes, status)
		o.index[status.ID] = status
	}
	if amendment.Done != nil {
		plan := *amendment.Done
		o.plan = &plan
		o.finishing = true
	}
	o.mu.Unlock()

	o.publish()
	o.note(amendment.Note)
}

// dropLocked removes one pending node, and every pending node that was only
// there because of it.
//
// THE CASCADE IS NOT A JUDGEMENT, it is bookkeeping: a node whose need has
// been deleted can never become ready, and leaving it on the frontier would be
// a row a surface draws forever under a dependency that does not exist. What
// the planner MEANT by the cancel is in its note; what it costs is here.
func (o *Orchestrator) dropLocked(id string) {
	node, known := o.index[id]
	if !known || (node.State != Queued && node.State != Ready) {
		return
	}
	delete(o.index, id)
	for at, have := range o.nodes {
		if have == node {
			o.nodes = append(o.nodes[:at], o.nodes[at+1:]...)
			break
		}
	}
	for _, other := range append([]*NodeStatus(nil), o.nodes...) {
		if other.State != Queued && other.State != Ready {
			continue
		}
		for _, need := range other.Needs {
			if need == id {
				o.dropLocked(other.ID)
				break
			}
		}
	}
}

// collisionsLocked is law 10 enforced in code rather than asked for in a
// prompt: TWO NODES THAT WRITE THE SAME PATH ARE NOT INDEPENDENT, whatever the
// planner believed when it added them. The later one gains a need on the
// earlier, and the scheduler — which knows nothing about files — serializes
// them for free.
//
// The edge always points BACKWARDS in insertion order, so it can never close a
// cycle. Nodes that are already done are not collided with: their writes have
// landed.
func (o *Orchestrator) collisionsLocked(node Node) []string {
	if len(node.WriteScope) == 0 {
		return nil
	}
	var added []string
	for _, other := range o.nodes {
		if other.State == Done || other.State == Failed {
			continue
		}
		if !scopesCollide(node.WriteScope, other.WriteScope) {
			continue
		}
		if contains(node.Needs, other.ID) || contains(added, other.ID) {
			continue
		}
		added = append(added, other.ID)
	}
	return added
}

// scopesCollide reports whether two write scopes overlap. A path contains
// itself and everything under it, so "internal/session" collides with
// "internal/session/orchestrate.go" — which is the case that matters, because
// it is how a directory-wide node and a file-wide node end up interleaved.
func scopesCollide(left, right []string) bool {
	for _, one := range left {
		for _, two := range right {
			if pathCovers(one, two) || pathCovers(two, one) {
				return true
			}
		}
	}
	return false
}

// Covers reports whether a write scope contains one path. It is exported for
// the enforcement point: the scope is a claim about what a node may write, and
// the code that refuses a write outside it must read the claim exactly as the
// scheduler does when it decides two nodes collide.
func Covers(scope []string, target string) bool {
	for _, allowed := range scope {
		if pathCovers(allowed, target) {
			return true
		}
	}
	return false
}

func pathCovers(parent, child string) bool {
	parent = path.Clean(strings.TrimSpace(parent))
	child = path.Clean(strings.TrimSpace(child))
	if parent == "" || parent == "." || child == "" || child == "." {
		return false
	}
	return parent == child || strings.HasPrefix(child, parent+"/")
}

func contains(list []string, want string) bool {
	for _, have := range list {
		if have == want {
			return true
		}
	}
	return false
}
