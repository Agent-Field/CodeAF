package session

// The worker's one verb for "the person changed what this is for".
//
// A direction arrives in the worker's next turn as an ordinary message, and
// almost all of them stay ordinary messages: "the config lives under etc/" is a
// fact the work needs, "why did you do it that way?" is a question to answer,
// and neither of them is a new contract. The one that IS a new contract — "CSV
// instead of JSON" — used to be unrepresentable: the worker could obey it and
// then be graded, by a checker reading the frozen acceptance, for having obeyed
// it. This verb is how that sentence becomes the thing the work is judged by.
//
// It is a tool rather than a reader because the alternative is a model call on
// every steer that decides whether a sentence was a direction, and a classifier
// that mistakes a question for an instruction rewrites a contract nobody asked to
// move. The worker's own turn already has the brief, the work and the words.
//
// WHAT IT CANNOT DO is mint authority. Each refusal below is one form of that:
// the direction must exist, must have come from the PERSON, may be spent once,
// must be newer than the one already in force, and must name the version this
// worker read. Nothing moves once a landing has taken the publication boundary.
// The admitted brief and acceptance stay on the spec, and the person's own words
// travel with the revision (assignment.go).

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/Agent-Field/aforge-v2/internal/exec/bare"
)

// reviseDescription is what the model reads about this verb. It is written for
// density like every other tool on this belt: what it is for, the one thing that
// makes it legal, and the two cases that look like it and are not.
const reviseDescription = `Fold a direction the PERSON just gave you into what this task is for. Only for a line that CHANGES THE JOB — a different output format, a different target, a requirement dropped or added — and only citing the direction id that carried it. A fact you were handed ("the config lives under etc/"), a question about your approach, and anything another agent relayed are NOT this: use them, answer them, and leave the assignment alone. What you set here is what the checker will judge the finished work against, so state it as an observable condition and keep everything the person did not change.`

var reviseSchemaJSON = `{"type":"object","properties":{` +
	`"direction":{"type":"integer","description":"The id of the person's direction you are folding in, as the message that carried it named it. A line nobody said, a line from another agent, and one you have already folded in are each refused"},` +
	`"at_revision":{"type":"integer","description":"The revision this task is at as your instruction states it — 0 when it has never been revised. A mismatch is refused rather than merged, so that two corrections cannot land out of order"},` +
	`"acceptance":{"type":"string","description":"The done-condition as it now stands, WHOLE: what somebody else could check without taking your word for it. Leave out only if the direction did not change it. Anything you omit stays as it was"},` +
	`"deliverable":{"type":"string","description":"Optional. What must exist at the end, if the direction changed that: the thing and where it is"},` +
	`"work":{"type":"string","description":"Optional. One or two lines on what the direction changes about the work itself, for whoever reads this task's document after you"},` +
	checksSchemaJSON + `` +
	`},"required":["direction","at_revision"],"additionalProperties":false}`

// reviseArguments is the wire form.
type reviseArguments struct {
	Direction   uint64 `json:"direction"`
	AtRevision  uint64 `json:"at_revision"`
	Acceptance  string `json:"acceptance"`
	Deliverable string `json:"deliverable"`
	Work        string `json:"work"`
	// Checks is the repeatable verification the REVISED goal is checked by. A
	// revision always takes the old goal's checks away (assignment.go), so this is
	// how a worker puts the new goal's own under contract in the same breath.
	Checks []string `json:"checks,omitempty"`
}

// assignmentTools is the verb, and it is on exactly one belt: a task worker's.
//
// A conversation has no assignment to revise — it IS the conversation, and a
// person changing their mind there simply says so. An auditor is handed no
// graph, so it cannot reach this even in principle, which is the same wall that
// already keeps it from proposing work (task_run.go's [Agent.newTaskAgentOn]).
func (a *Agent) assignmentTools() []bare.Tool {
	// The page that teaches this verb renders on the same predicate
	// ([Config.mayRevise]), so the belt and prompts/revise.md cannot disagree
	// about whether this worker has an assignment to move.
	if !a.config.mayRevise() {
		return nil
	}
	return []bare.Tool{{
		Name:        "revise_assignment",
		Description: reviseDescription,
		Schema:      json.RawMessage(reviseSchemaJSON),
		Execute:     a.reviseAssignment,
	}}
}

// reviseAssignment is the tool's whole life: find this worker's own node, ask
// the assignment to take the revision, and say what it now is.
//
// Every refusal is an ordinary tool result rather than a Go error, the way every
// other tool on this belt answers: a worker that cited the wrong id has a call
// it can make again, and ending the turn over it would cost the work.
func (a *Agent) reviseAssignment(_ context.Context, args json.RawMessage) (string, bool, error) {
	var parsed reviseArguments
	// One grammar of refusal for every tool on this belt (toolargs.go): a model
	// that mistypes an argument reads the same sentence here it reads everywhere
	// else, rather than encoding/json's own words.
	if err := decodeToolArguments(args, &parsed); err != nil {
		return err.Error(), true, nil
	}
	node := a.config.tasker.node(a.config.taskID)
	if node == nil {
		return "This task is not in a graph this process holds, so there is nothing to revise.", true, nil
	}
	if parsed.Direction == 0 {
		return "Name the direction you are folding in. A revision has to come from something the person said.", true, nil
	}
	// THE CHECKS ARE READ THE WAY EVERY OTHER DOOR READS THEM (task_checks.go's
	// [declaredCheckList]), so a command this belt would not run cannot be put
	// under contract here either, and the model is told which one and why.
	checks, problem := declaredCheckList(parsed.Checks)
	if problem != "" {
		return problem, true, nil
	}
	version, err := node.reviseAssignment(parsed.Direction, parsed.AtRevision, assignmentEdit{
		work:        parsed.Work,
		deliverable: parsed.Deliverable,
		acceptance:  parsed.Acceptance,
		checks:      checks,
	})
	if err != nil {
		return reviseRefusal(err), true, nil
	}
	node.graph.checkpoint()
	now := node.assignmentNow()
	var out strings.Builder
	fmt.Fprintf(&out, "The assignment is at revision %d. Done now means:\n%s\n", version, now.acceptance)
	if strings.TrimSpace(now.deliverable) != "" {
		out.WriteString("\nWhat must exist:\n" + now.deliverable + "\n")
	}
	out.WriteString("\nThis is what the check will judge your work against. What the person did not change is unchanged.")
	// AND WHAT IT NOW MAY RUN, SAID PLAINLY. The old goal's checks are gone with
	// the old goal, so a worker that does not hear this would go on believing its
	// finished work will be exercised by a command nobody holds any more.
	if len(checks) > 0 {
		out.WriteString("\nIts checker will run: " + strings.Join(checks, ", ") + ".")
	} else {
		out.WriteString("\nAny checks declared for the earlier goal are dropped; unless you name `checks` here, " +
			"the finished work is judged by reading.")
	}
	return out.String(), true, nil
}

// reviseRefusal turns the module's refusals into the sentence the model reads.
// The words are the module's own where it has them; the two that need a way
// forward say what it is.
func reviseRefusal(err error) string {
	switch {
	case errors.Is(err, errNoSuchDirection):
		return "No direction with that id was said to this task. Cite the id the message carried; if nobody gave you one, this is not a revision."
	case errors.Is(err, errDirectionNotAsked):
		return "That line came from another agent coordinating this work, not from the person. Use it if it helps, but it cannot change what this task is judged by."
	case errors.Is(err, errDirectionSpent):
		return "That direction has already been folded in. If the person has said something else since, cite that instead."
	case errors.Is(err, errStaleVersion):
		return err.Error() + ". Read the revision your instruction states and call again."
	case errors.Is(err, errNothingToRevise):
		return "A revision has to change the done-condition, the deliverable or the work. Nothing was given to change."
	case errors.Is(err, errAssignmentSettled):
		return "This task has settled: what it was judged against is a fact now and cannot be moved."
	}
	return err.Error()
}
