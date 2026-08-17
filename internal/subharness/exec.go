package subharness

import (
	"context"
	"errors"
	"fmt"
	"strings"
)

// THE ENVIRONMENT: what a node DOES, as against run.go's walk, which is where a
// node comes in the order.
//
// run.go owns the shape of a run — what is reached, what a branch skipped, how
// many rounds a loop was allowed, what the budget was spent on — and hands each
// node to an [Exec]. This file is the other half: an [Env] that gives every kind
// its meaning, and a [Runner] that turns one into the Exec the walk wants. The
// session implements Env over the belt and the consent lane it already has, a
// test implements it over a script, and both get identical control flow,
// identical bounds and an identical trace — which is what makes a harness that
// behaved one way in a test a harness that behaves that way in a conversation.
//
// ── WHAT ENDS A RUN ──
//
// Three things, and they are three different words in the record:
//
//   - an ERROR from a node ends it as failed. There is no catch and no retry
//     except the one a loop.until buys explicitly; a program that wants to
//     handle a failure says so with a branch on `failed`.
//   - a PERSON at a human.gate ends it as declined, or as intervened when they
//     took it over. Neither is a fault, and neither comes back as an error.
//   - the CONTEXT ends it as cancelled — an interrupt, a closed session.
//
// A verify that reports "no" is NOT one of them. It is a node that ran correctly
// and returned false, which is what the condition language's `failed` is for and
// what a loop.until is usually looping on. Only at the END of a program does an
// unaddressed false become a failure, because a program that finishes on a check
// that did not pass has not done what it said it would.

// MaxCallDepth bounds subharness.call nesting. It is a runtime bound rather than
// one of the file's caps (registry.go): a page cannot declare it, because the
// depth is a property of who called whom, and the recursion it stops is a fan of
// three harnesses that each thought they were the top one.
const MaxCallDepth = 3

// Status is how a run ended. It is written into the trace so a saved run says
// what happened without the reader re-deriving it from an error string.
type Status string

const (
	// StatusOK means the program reached its end.
	StatusOK Status = "ok"
	// StatusFailed means a node failed and nothing caught it.
	StatusFailed Status = "failed"
	// StatusDeclined means a person said no at a human.gate. It is NOT a
	// failure: the gate did exactly what it is for, and a history that filed
	// every refusal as a fault would be a history that punishes the feature.
	StatusDeclined Status = "declined"
	// StatusIntervened means a person took the run over at a gate — the
	// escalation ([GateAnswer.Intervene]). The program stopped where it stood
	// and a human continued from there.
	StatusIntervened Status = "intervened"
	// StatusCancelled means the context died: an interrupt, a closed session, a
	// deadline.
	StatusCancelled Status = "cancelled"
)

// status is the trace's own word for how it ended, derived for a trace written
// by a plain [Run] that never carried one.
func (t Trace) status() Status {
	switch {
	case t.Status != "":
		return t.Status
	case t.Err != "":
		return StatusFailed
	}
	return StatusOK
}

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
	// Cond judges a branch's `when` or a loop.until's `until` when the condition
	// language cannot (predicate.go). It is the seam where "the suite is green"
	// stops being a string and becomes an answer, and it is asked ONLY for
	// conditions [ValidCondition] refuses — everything the small language can
	// decide is decided here, in this package, the same way every time.
	Cond(ctx context.Context, node Node, condition string, state State) (bool, error)
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
	Load(name string, version int) (Harness, error)
}

// Saver is where a called harness's own run is recorded. A call's child run
// belongs in the CHILD's history — that is where somebody looking at "how has
// triage behaved" would go — so the parent's trace records the path and the
// child's file holds the detail. *[Store] satisfies this too.
type Saver interface {
	SaveRun(Trace) (string, error)
}

// Runner gives a harness's nodes their meaning and hands the shape to [Run].
// It is single-use per Run call and holds no state between them.
type Runner struct {
	// Env is what makes the nodes do anything. Required.
	Env Env
	// Loader resolves subharness.call. Nil means this runner cannot make calls,
	// and a program that tries gets an error rather than a silent skip.
	Loader Loader
	// Saver, when set, is where a called harness's own trace is written.
	Saver Saver
	// Depth is how many calls deep this runner already is. The top level is 0.
	Depth int
}

// stop is a control-flow signal that is not a fault: a person ended the run at a
// gate. It travels as an error because that is the one channel an [Exec] has to
// end a walk with, and it is unwrapped at the top ([Runner.Run]) into a status
// rather than into a failure.
type stop struct {
	status Status
	note   string
}

func (s *stop) Error() string { return string(s.status) }

// Run executes a harness and returns its trace. The error is non-nil only for a
// run that FAILED or was cancelled; a declined or intervened run comes back with
// a complete trace and no error, because nothing went wrong.
//
// The walk is [Run]'s, unchanged — this method only decides what each node means
// and what the run's last word is.
func (r *Runner) Run(ctx context.Context, h Harness, input string) (Trace, error) {
	if r.Env == nil {
		return Trace{}, errors.New("subharness: a runner needs an Env")
	}
	state := State{Last: input, OK: true}
	trace, err := Run(ctx, h, func(ctx context.Context, node Node) (Result, error) {
		return r.step(ctx, h, &state, node)
	})

	var ended *stop
	switch {
	case errors.As(err, &ended):
		// The gate answered. The trail already holds the node it answered at,
		// and the run's own error is not one.
		trace.Status, trace.Err = ended.status, ""
		if note := strings.TrimSpace(ended.note); note != "" {
			trace.Out = note
		}
		return trace, nil
	case err != nil && ctx.Err() != nil:
		trace.Status = StatusCancelled
		return trace, err
	case err != nil:
		trace.Status = StatusFailed
		return trace, err
	case !state.OK:
		// The program ran to its end on a check that did not pass and nothing in
		// it reacted. Reporting that as a success would make the verify ladder
		// decorative.
		trace.Status = StatusFailed
		trace.Err = "the last check did not pass"
		return trace, errors.New(trace.Err)
	}
	trace.Status, trace.Out = StatusOK, state.Last
	return trace, nil
}

// step is one node's meaning: what it does, and what it leaves behind for the
// condition that reads it next.
func (r *Runner) step(ctx context.Context, h Harness, state *State, node Node) (Result, error) {
	switch node.Kind {
	case KindTrigger:
		// Declarative. It is recorded because the trace should say what started
		// the run, and it does nothing because nothing is what it means here:
		// the thing that fires a trigger is outside the program.
		return Result{Out: join([]string{node.Fields.Get("source"), node.Fields.Get("spec"), node.Fields.Get("command")})}, nil

	case KindAgentLoop:
		out, err := r.Env.Loop(ctx, node, state.Last)
		*state = State{Last: out, OK: err == nil}
		if err != nil {
			return Result{}, err
		}
		return Result{Out: out}, nil

	case KindToolCall:
		out, err := r.Env.Tool(ctx, node, state.Last)
		*state = State{Last: out, OK: err == nil}
		if err != nil {
			return Result{}, err
		}
		return Result{Out: out}, nil

	case KindVerify:
		passed, said, err := r.Env.Check(ctx, node, *state)
		if err != nil {
			*state = State{Last: said, OK: false}
			return Result{}, err
		}
		// A check's own words become the state, so the next condition can read
		// them — and when it said nothing, the material it checked stands, so a
		// silent pass does not blank the run.
		next := State{Last: state.Last, OK: passed}
		if strings.TrimSpace(said) != "" {
			next.Last = said
		}
		*state = next
		return Result{Out: verifyWord(node, passed, said)}, nil

	case KindHumanGate:
		answer, err := r.Env.Gate(ctx, node, *state)
		if err != nil {
			state.OK = false
			return Result{}, err
		}
		switch {
		case answer.Intervene:
			return Result{Out: answer.Word()}, &stop{status: StatusIntervened, note: answer.Note}
		case !answer.Approved:
			return Result{Out: answer.Word()}, &stop{status: StatusDeclined, note: answer.Note}
		}
		// An approval with words attached is a redirect: the person's sentence
		// becomes what the next node reads.
		if note := strings.TrimSpace(answer.Note); note != "" {
			*state = State{Last: state.Last + "\n\nThe person approving this says: " + note, OK: true}
		}
		return Result{Out: answer.Word()}, nil

	case KindBranch:
		return r.branch(ctx, h, *state, node)

	case KindLoopUntil:
		condition := node.Fields.Get("until")
		done, err := r.cond(ctx, node, condition, *state)
		if err != nil {
			return Result{}, err
		}
		return Result{Out: loopWord(condition, done), Done: done}, nil

	case KindParallelSplit:
		// The walk is sequential and says so (run.go): a split is where the width
		// OPENS, and every lane it leads to is reached in program order. An
		// executor that really runs lanes at once is the one place that changes,
		// and the join's mode is carried through the trace for it.
		return Result{Out: fmt.Sprintf("width %d", node.Fields.Int("width", 1))}, nil

	case KindParallelJoin:
		return Result{Out: "join " + joinMode(node)}, nil

	case KindSubharnessCall:
		return r.call(ctx, node, state)
	}
	return Result{}, fmt.Errorf("%q is not a node kind this build runs", node.Kind)
}

// branch picks the successor the condition names.
//
// The FIRST successor is the arm the `when` holds for and the SECOND is the
// else; anything past two is unreachable and Validate has no opinion about it,
// so it is written down here instead: a branch is a question with two answers,
// and a program that wants three asks twice.
//
// A BRANCH WITH ONE SUCCESSOR TAKES IT EITHER WAY, because the walk has no way
// to end a lane — every reached node runs. That is not a silent default: it is
// why a branch that means "and otherwise carry on" has to draw the otherwise.
func (r *Runner) branch(ctx context.Context, h Harness, state State, node Node) (Result, error) {
	condition := node.Fields.Get("when")
	hit, err := r.cond(ctx, node, condition, state)
	if err != nil {
		return Result{}, err
	}
	successors := h.Program.Successors(node.Id)
	if len(successors) == 0 {
		return Result{Out: branchWord(condition, hit, "")}, nil
	}
	taken := successors[0]
	if !hit && len(successors) > 1 {
		taken = successors[1]
	}
	return Result{Out: branchWord(condition, hit, taken), Next: taken}, nil
}

// call runs another registered harness here.
//
// The child gets its OWN trace, in its own history, because that is where
// anybody asking "how does triage behave" will look — and the parent's trail
// keeps the pointer, the version it resolved to and the status, which is
// everything needed to walk from one to the other.
func (r *Runner) call(ctx context.Context, node Node, state *State) (Result, error) {
	if r.Loader == nil {
		return Result{}, errors.New("this surface cannot reach other harnesses")
	}
	if r.Depth+1 > MaxCallDepth {
		return Result{}, fmt.Errorf("calls are nested more than %d deep", MaxCallDepth)
	}
	child, err := r.Loader.Load(node.Fields.Get("name"), node.Fields.Int("version", 0))
	if err != nil {
		return Result{}, err
	}
	inner := &Runner{Env: r.Env, Loader: r.Loader, Saver: r.Saver, Depth: r.Depth + 1}
	childTrace, childErr := inner.Run(ctx, child, state.Last)
	note := fmt.Sprintf("%s v%d · %s", child.Id.Name, child.Id.Version, childTrace.status())
	if r.Saver != nil {
		if path, err := r.Saver.SaveRun(childTrace); err == nil {
			note += " · " + path
		}
	}
	if childErr != nil {
		state.OK = false
		return Result{}, fmt.Errorf("%s: %w", note, childErr)
	}
	*state = State{Last: childTrace.Out, OK: true}
	// A CHILD THAT WAS DECLINED STOPS THE PARENT. The person said no to work
	// this program asked for; carrying on as if they had not is the one reading
	// of that answer nobody meant.
	if s := childTrace.status(); s == StatusDeclined || s == StatusIntervened {
		return Result{Out: note}, &stop{status: s, note: childTrace.Out}
	}
	return Result{Out: note}, nil
}

// cond is the one place a condition is answered, and the whole of the bargain
// predicate.go describes: the small language decides what it can, and everything
// else is the environment's judgement.
func (r *Runner) cond(ctx context.Context, node Node, condition string, state State) (bool, error) {
	if ValidCondition(condition) == nil {
		return MatchCondition(condition, state)
	}
	return r.Env.Cond(ctx, node, condition, state)
}

func branchWord(condition string, hit bool, taken string) string {
	word := "when " + condition
	if hit {
		word += " · yes"
	} else {
		word += " · no"
	}
	if taken != "" {
		word += " → " + taken
	}
	return word
}

func loopWord(condition string, done bool) string {
	if done {
		return "until " + condition + " · held"
	}
	return "until " + condition + " · another round"
}

func verifyWord(node Node, passed bool, said string) string {
	word := node.Fields.Get("ladder")
	if word == "" {
		word = "verify"
	}
	if passed {
		word += " · passed"
	} else {
		word += " · did not pass"
	}
	if said = strings.TrimSpace(said); said != "" {
		word += " · " + said
	}
	return word
}
