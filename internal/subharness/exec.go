package subharness

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"
)

// THE RUNNER: the one interpreter for every registered harness.
//
// It owns the SHAPE of a run — order, branching, rounds, width, gating,
// recursion, and the trace that records all of it — and it owns nothing else.
// What a node actually DOES is the [Env]'s: a worker's turn, a tool call, a
// question put to a person, a check that passes or fails. That split is the
// whole design. The session implements Env over the belt and the consent lane it
// already has, a test implements it over a script, and both get identical
// control flow, identical bounds, and an identical trace — which is what makes a
// harness that behaved one way in a test a harness that behaves that way in a
// conversation.
//
// ── WHAT ENDS A RUN ──
//
// Three things, and they are three different words in the record:
//
//   - an ERROR from a node ends it as failed. There is no catch and no retry
//     except the one a loop.until buys explicitly; a program that wants to
//     handle a failure says so with a branch on `failed`.
//   - a PERSON at a human.gate ends it as declined, or as intervened when they
//     took it over. Neither is a fault (run.go's [StatusDeclined]).
//   - the CONTEXT ends it as cancelled — an interrupt, a closed session.
//
// A verify that reports "no" is NOT one of them. It is a node that ran correctly
// and returned false, which is what the condition language's `failed` is for and
// what a loop.until is usually looping on. Only at the END of a program does an
// unaddressed false become a failure, because a program that finishes on a check
// that did not pass has not done what it said it would.

// stepCeiling bounds how many traces one run may open, whatever the program's
// nesting multiplies out to. Every loop is bounded in the file and every lane is
// counted, so this is not the safety rail — it is the backstop behind the rail,
// and a run that reaches it is a bug in this package rather than a program a
// person wrote.
const stepCeiling = 512

// Env is everything the runner cannot do by itself. Every method may block and
// every method must respect its context.
type Env interface {
	// Loop runs one agent.loop node on input and returns what the worker
	// produced.
	Loop(ctx context.Context, node Node, input string) (string, error)
	// Tool calls one tool with the node's fixed arguments.
	Tool(ctx context.Context, node Node, input string) (string, error)
	// Gate asks a person the node's question and returns their answer. An
	// implementation with nobody to ask must return an answer rather than block
	// forever — see [GateAnswer] for what "nobody is there" should mean.
	Gate(ctx context.Context, node Node, state State) (GateAnswer, error)
	// Check runs one verify node. The bool is whether it passed; the string is
	// what it said, which becomes the state the next condition reads. An ERROR
	// means the check could not be run at all, which is a different fact from a
	// check that ran and failed.
	Check(ctx context.Context, node Node, state State) (bool, string, error)
}

// GateAnswer is what a person said at a human.gate.
//
// THE THIRD ANSWER IS THE POINT. Approve and decline are the two a countdown
// card already has; intervene is the one a harness needs, because a person
// watching a program they wrote go slightly wrong does not want to kill it and
// does not want to wave it through — they want to take it from here. So the run
// stops where it stands, its trace is complete up to that node, and the words
// they typed become the run's output for whoever picks it up.
type GateAnswer struct {
	Approved bool
	// Intervene means the person is taking over: the run ends as intervened,
	// whatever Approved says.
	Intervene bool
	// Note is what they typed — a redirect, a reason, or the instruction they
	// are continuing with.
	Note string
}

// Word is the answer in the one word the trace records.
func (g GateAnswer) Word() string {
	switch {
	case g.Intervene:
		return "intervened"
	case g.Approved:
		return "approved"
	default:
		return "declined"
	}
}

// Loader is how a subharness.call reaches another program. *[Store] satisfies
// it; a test can satisfy it with a map.
type Loader interface {
	LoadVersion(name string, version int) (Harness, error)
}

// Runner executes one harness. It is single-use per Run call and holds no state
// between them.
type Runner struct {
	// Env is what makes the nodes do anything.
	Env Env
	// Loader resolves subharness.call. Nil means this runner cannot make calls,
	// and a program that tries gets an error rather than a silent skip.
	Loader Loader
	// Saver, when set, is where a called harness's own run is recorded. A call's
	// child run belongs in the CHILD's history — that is where somebody looking
	// at "how has triage behaved" would go — so the parent's trace records the
	// path and the child's file holds the detail.
	Saver interface {
		SaveRun(Run) (string, error)
	}
	// Depth is how many calls deep this runner already is. The top level is 0.
	Depth int
}

// runState is one execution's mutable bookkeeping, threaded through the walk.
type runState struct {
	trace *recorder
	// steps is how many traces have been opened, against [stepCeiling].
	steps int
	mu    sync.Mutex
}

func (s *runState) spend() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.steps++
	if s.steps > stepCeiling {
		return fmt.Errorf("the run opened more than %d steps", stepCeiling)
	}
	return nil
}

// stop is a control-flow signal that is not a fault: a person ended the run at a
// gate. It travels as an error because it has to unwind every nested sequence,
// and it is unwrapped at the top ([Runner.Run]) into a status rather than into a
// failure.
type stop struct {
	status Status
	note   string
}

func (s *stop) Error() string { return string(s.status) }

// Run executes a harness and returns the trace, saved by the caller. The error
// is non-nil only for a run that FAILED; a declined or intervened run returns a
// complete trace and no error, because nothing went wrong.
func (r *Runner) Run(ctx context.Context, h Harness, input string) (Run, error) {
	Clamp(&h)
	if err := Validate(&h); err != nil {
		return Run{}, err
	}
	if r.Env == nil {
		return Run{}, errors.New("subharness: a runner needs an Env")
	}
	run := Run{
		Harness: h.Name,
		Version: h.Version,
		Input:   input,
		Trigger: "person",
		Started: time.Now().UTC(),
		Status:  StatusOK,
	}
	if len(h.Program) > 0 && h.Program[0].Kind == KindTrigger {
		run.Trigger = string(h.Program[0].On)
	}

	state := &runState{trace: newRecorder()}
	_, final, err := r.steps(ctx, &h, state, h.Program, State{Last: input, OK: true}, nil, 0, "")
	run.Nodes = state.trace.take()
	run.Finished = time.Now().UTC()
	run.Output = capText(final.Last)

	var ended *stop
	switch {
	case errors.As(err, &ended):
		run.Status = ended.status
		if ended.note != "" {
			run.Output = capText(ended.note)
		}
		return run, nil
	case err != nil && ctx.Err() != nil:
		run.Status, run.Error = StatusCancelled, err.Error()
		return run, err
	case err != nil:
		run.Status, run.Error = StatusFailed, err.Error()
		return run, err
	case !final.OK:
		// The program ran to its end on a check that did not pass and nothing
		// in it reacted. Reporting that as a success would make the verify
		// ladder decorative.
		run.Status = StatusFailed
		run.Error = "the last check did not pass"
		return run, errors.New(run.Error)
	}
	return run, nil
}

// steps runs one sequence and returns the id of the last trace it opened and the
// state it left behind. needs is the edge into the first node of this sequence.
func (r *Runner) steps(ctx context.Context, h *Harness, run *runState, steps []Node, state State, needs []string, round int, lane string) (string, State, error) {
	previous := needs
	lastID := ""
	if len(needs) == 1 {
		lastID = needs[0]
	}
	for _, node := range steps {
		if err := ctx.Err(); err != nil {
			return lastID, state, err
		}
		id, next, err := r.step(ctx, h, run, node, state, previous, round, lane)
		if id != "" {
			lastID, previous = id, []string{id}
		}
		state = next
		if err != nil {
			return lastID, state, err
		}
	}
	return lastID, state, nil
}

// step runs one node: open a trace, do the thing, close the trace. Every kind
// goes through here, which is what makes "every node that ran is in the DAG" a
// property of the runner rather than of nine call sites.
func (r *Runner) step(ctx context.Context, h *Harness, run *runState, node Node, state State, needs []string, round int, lane string) (string, State, error) {
	if err := run.spend(); err != nil {
		return "", state, err
	}
	id := run.trace.open(node, needs, round, lane)

	switch node.Kind {
	case KindTrigger:
		// Declarative. It is recorded because the trace should say what started
		// the run, and it does nothing because nothing is what it means here:
		// the thing that fires a trigger is outside the program.
		run.trace.close(id, true, "", string(node.On)+" "+node.Spec, "", nil)
		return id, state, nil

	case KindAgentLoop:
		output, err := r.Env.Loop(ctx, node, state.Last)
		run.trace.close(id, err == nil, output, "", "", err)
		if err != nil {
			return id, State{Last: output, OK: false}, fmt.Errorf("%s: %w", node.ID, err)
		}
		return id, State{Last: output, OK: true}, nil

	case KindToolCall:
		output, err := r.Env.Tool(ctx, node, state.Last)
		run.trace.close(id, err == nil, output, node.Tool, "", err)
		if err != nil {
			return id, State{Last: output, OK: false}, fmt.Errorf("%s: %w", node.ID, err)
		}
		return id, State{Last: output, OK: true}, nil

	case KindVerify:
		passed, said, err := r.Env.Check(ctx, node, state)
		rung := string(node.Rung.Or(RungAccept))
		run.trace.close(id, passed, said, rung, "", err)
		if err != nil {
			return id, State{Last: said, OK: false}, fmt.Errorf("%s: %w", node.ID, err)
		}
		// A check's own words become the state, so the next condition can read
		// them — and when it said nothing, the material it checked stands, so a
		// silent pass does not blank the run.
		next := State{Last: state.Last, OK: passed}
		if strings.TrimSpace(said) != "" {
			next.Last = said
		}
		return id, next, nil

	case KindHumanGate:
		answer, err := r.Env.Gate(ctx, node, state)
		if err != nil {
			run.trace.close(id, false, "", "", "", err)
			return id, State{Last: state.Last, OK: false}, fmt.Errorf("%s: %w", node.ID, err)
		}
		run.trace.close(id, answer.Approved || answer.Intervene, answer.Note, "", answer.Word(), nil)
		switch {
		case answer.Intervene:
			return id, state, &stop{status: StatusIntervened, note: answer.Note}
		case !answer.Approved:
			return id, state, &stop{status: StatusDeclined, note: answer.Note}
		}
		// An approval with words attached is a redirect: the person's sentence
		// becomes what the next node reads, exactly as a redirected task
		// proposal appends theirs to the brief (session's task.go).
		if note := strings.TrimSpace(answer.Note); note != "" {
			return id, State{Last: state.Last + "\n\nThe person approving this says: " + note, OK: true}, nil
		}
		return id, state, nil

	case KindBranch:
		return r.branch(ctx, h, run, node, state, id, round, lane)

	case KindLoopUntil:
		return r.loop(ctx, h, run, node, state, id, lane)

	case KindParallelSplit:
		return r.split(ctx, h, run, node, state, id, round)

	case KindSubharnessCall:
		return r.call(ctx, run, node, state, id)
	}
	err := fmt.Errorf("%s: %q is not a node kind this build runs", node.ID, node.Kind)
	run.trace.close(id, false, "", "", "", err)
	return id, state, err
}

// branch picks the first arm whose condition holds, and runs it.
//
// NO ARM MATCHING IS NOT AN ERROR. A branch with no else is a program saying
// "and otherwise carry on", which is a thing people write on purpose; the trace
// records that nothing matched so the shape of the run is still legible.
func (r *Runner) branch(ctx context.Context, h *Harness, run *runState, node Node, state State, id string, round int, lane string) (string, State, error) {
	for at, one := range node.Cases {
		hit, err := Match(one.When, state)
		if err != nil {
			run.trace.close(id, false, "", "", "", err)
			return id, state, fmt.Errorf("%s: %w", node.ID, err)
		}
		if !hit {
			continue
		}
		run.trace.close(id, true, "", fmt.Sprintf("%s → %s", letter(at), one.When), "", nil)
		return r.steps(ctx, h, run, one.Steps, state, []string{id}, round, lane)
	}
	if len(node.Else) > 0 {
		run.trace.close(id, true, "", "else", "", nil)
		return r.steps(ctx, h, run, node.Else, state, []string{id}, round, lane)
	}
	run.trace.close(id, true, "", "no case matched", "", nil)
	return id, state, nil
}

// loop runs the body until the condition holds or the rounds run out.
//
// THE CONDITION IS TESTED AFTER EACH ROUND, never before the first. A loop.until
// is "do this until it is right", and a version that tested first would be a
// while-loop wearing an until-loop's name — it would skip the work entirely
// whenever the condition already happened to hold on the state it inherited.
//
// RUNNING OUT OF ROUNDS IS A FAILURE. The file said how many rounds this is
// worth; reaching that number without the condition holding is the program's own
// statement that it has not achieved what it set out to.
func (r *Runner) loop(ctx context.Context, h *Harness, run *runState, node Node, state State, id string, lane string) (string, State, error) {
	rounds := node.Max
	if rounds <= 0 {
		rounds = DefaultRounds
	}
	last := id
	for round := 1; round <= rounds; round++ {
		var err error
		last, state, err = r.steps(ctx, h, run, node.Steps, state, []string{last}, round, lane)
		if err != nil {
			run.trace.close(id, false, "", fmt.Sprintf("round %d of %d", round, rounds), "", err)
			return last, state, err
		}
		hit, err := Match(node.Until, state)
		if err != nil {
			run.trace.close(id, false, "", "", "", err)
			return last, state, fmt.Errorf("%s: %w", node.ID, err)
		}
		if hit {
			run.trace.close(id, true, "", fmt.Sprintf("%s after %d of %d", node.Until, round, rounds), "", nil)
			return last, state, nil
		}
	}
	err := fmt.Errorf("%s: ran %d rounds and %q never held", node.ID, rounds, node.Until)
	run.trace.close(id, false, "", fmt.Sprintf("%d rounds, unmet", rounds), "", err)
	return last, State{Last: state.Last, OK: false}, err
}

// split runs the lanes at once and joins them.
//
// The JOIN IS A NODE, minted here and never written in a file (kinds.go). It is
// what gives the trace its diamond: every lane's last trace is an edge into it,
// so a reader of the DAG can see where the width closed even though the program
// only ever said where it opened.
func (r *Runner) split(ctx context.Context, h *Harness, run *runState, node Node, state State, id string, round int) (string, State, error) {
	mode := node.Join.Or()
	laneCtx, cancel := context.WithCancel(ctx)
	defer cancel()

	type result struct {
		lane   string
		id     string
		state  State
		err    error
		at     int
		landed time.Time
	}
	results := make([]result, len(node.Lanes))
	var wait sync.WaitGroup
	for at, lane := range node.Lanes {
		wait.Add(1)
		go func(at int, lane Lane) {
			defer wait.Done()
			lastID, out, err := r.steps(laneCtx, h, run, lane.Steps, state, []string{id}, round, lane.Name)
			results[at] = result{lane: lane.Name, id: lastID, state: out, err: err, at: at, landed: time.Now()}
			if mode == JoinFirst && err == nil {
				// The first lane home ends the others. They are cancelled, not
				// killed: a lane's own nodes see a dead context and unwind
				// through the ordinary path, so the trace records them as
				// cancelled rather than simply stopping mid-DAG.
				cancel()
			}
		}(at, lane)
	}
	wait.Wait()

	needs := make([]string, 0, len(results))
	for _, one := range results {
		if one.id != "" {
			needs = append(needs, one.id)
		}
	}
	if err := run.spend(); err != nil {
		run.trace.close(id, false, "", "", "", err)
		return id, state, err
	}
	joinID := run.trace.open(Node{ID: node.ID + ".join", Kind: KindParallelJoin}, needs, round, "")
	run.trace.close(id, true, "", fmt.Sprintf("%d lanes · join %s", len(node.Lanes), mode), "", nil)

	switch mode {
	case JoinFirst:
		// The winner is the first lane that finished without an error. A
		// cancelled sibling reports its context error and is not one.
		var won *result
		for at := range results {
			one := results[at]
			if one.err != nil || one.id == "" {
				continue
			}
			if won == nil || one.landed.Before(won.landed) {
				won = &results[at]
			}
		}
		if won == nil {
			err := fmt.Errorf("%s: every lane failed", node.ID)
			run.trace.close(joinID, false, "", "first", "", err)
			return joinID, State{Last: state.Last, OK: false}, err
		}
		run.trace.close(joinID, true, won.state.Last, "first · "+won.lane, "", nil)
		return joinID, won.state, nil

	default:
		var joined strings.Builder
		ok := true
		var failure error
		for _, one := range results {
			if one.err != nil {
				ok = false
				if failure == nil {
					failure = fmt.Errorf("%s: lane %s: %w", node.ID, one.lane, one.err)
				}
				continue
			}
			if joined.Len() > 0 {
				joined.WriteString("\n\n")
			}
			fmt.Fprintf(&joined, "── %s ──\n%s", one.lane, one.state.Last)
			ok = ok && one.state.OK
		}
		run.trace.close(joinID, failure == nil, joined.String(), "all", "", failure)
		if failure != nil {
			return joinID, State{Last: joined.String(), OK: false}, failure
		}
		return joinID, State{Last: joined.String(), OK: ok}, nil
	}
}

// call runs another registered harness here.
//
// The child gets its OWN run record, in its own history, because that is where
// anybody asking "how does triage behave" will look — and the parent's trace
// keeps the pointer, the version it resolved to, and the status, which is
// everything needed to walk from one to the other.
func (r *Runner) call(ctx context.Context, run *runState, node Node, state State, id string) (string, State, error) {
	if r.Loader == nil {
		err := fmt.Errorf("%s: this surface cannot reach other harnesses", node.ID)
		run.trace.close(id, false, "", "", "", err)
		return id, state, err
	}
	if r.Depth+1 > MaxCallDepth {
		err := fmt.Errorf("%s: calls are nested more than %d deep", node.ID, MaxCallDepth)
		run.trace.close(id, false, "", "", "", err)
		return id, state, err
	}
	child, err := r.Loader.LoadVersion(node.Call, node.CallVersion)
	if err != nil {
		run.trace.close(id, false, "", "", "", err)
		return id, state, fmt.Errorf("%s: %w", node.ID, err)
	}
	inner := &Runner{Env: r.Env, Loader: r.Loader, Saver: r.Saver, Depth: r.Depth + 1}
	childRun, runErr := inner.Run(ctx, child, state.Last)
	note := fmt.Sprintf("%s v%d · %s", child.Name, child.Version, childRun.Status)
	if r.Saver != nil {
		if path, err := r.Saver.SaveRun(childRun); err == nil {
			note += " · " + path
		}
	}
	if runErr != nil {
		run.trace.close(id, false, childRun.Output, note, "", runErr)
		return id, State{Last: childRun.Output, OK: false}, fmt.Errorf("%s: %w", node.ID, runErr)
	}
	// A CHILD THAT WAS DECLINED STOPS THE PARENT. The person said no to work
	// this program asked for; carrying on as if they had not is the one reading
	// of that answer nobody meant.
	if childRun.Status == StatusDeclined || childRun.Status == StatusIntervened {
		run.trace.close(id, true, childRun.Output, note, string(childRun.Status), nil)
		return id, State{Last: childRun.Output, OK: true}, &stop{status: childRun.Status, note: childRun.Output}
	}
	run.trace.close(id, true, childRun.Output, note, "", nil)
	return id, State{Last: childRun.Output, OK: true}, nil
}
