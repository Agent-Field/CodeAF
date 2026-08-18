package session

// THE BIG MACHINERY, PUT IN THE MODEL'S HANDS.
//
// Two of this build's largest capabilities — designing a sub-harness, and
// running an adaptive run — were reached by ANCHORED CUE and by nothing else:
// a sentence that began "make a harness for …" started a designer, and one that
// began "orchestrate …" started a run. Everything the person said in any other
// shape went to the model, which has no verb for either and answered with prose
// about harnesses instead of building one.
//
// THE DECIDING IS THE MODEL'S. That is the law this file exists to keep, and it
// is worth stating plainly because the rest of this package leans the other way:
// detection is a table lookup, the build cue was a table lookup, and both are
// written that way because a model call on every turn is a tax and a wrong yes
// is somebody's turn. A TOOL COSTS NEITHER. It is not consulted unless the model
// reaches for it, so the judgement — is this work a saved recipe, a run, or just
// work — is made once, by the thing in this program that can actually make it,
// with the whole conversation in front of it. "Build me something that does this
// every sprint" is a build; "we should make a harness for this one day" is not;
// no regular language can tell those apart and no regex should have to try.
//
// WHAT THE TOOLS DO NOT DECIDE. Neither hand commits anything a person did not
// approve. build_harness starts a design that ends in a card somebody says yes
// to (harness_build.go), and run_adaptive starts a run against a fuel tank that
// stops and asks when it is empty (orchestrate.go). The model chooses to ASK;
// the person still chooses to keep and to pay.
//
// EACH HAND IS ABSENT WHERE IT CANNOT WORK, which is this codebase's law for a
// belt (tools.go) and matters more here than anywhere: a model told it can build
// a harness plans around that ability for the rest of the conversation, long
// after the first refusal. So the gates are the same ones the cue path checked —
// a store to write into, a runner to run what is written, somebody watching who
// can answer the card and the fuel gate — and a build that fails one of them
// simply does not have the verb.

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

	"github.com/Agent-Field/aforge-v2/internal/exec/bare"
	"github.com/Agent-Field/aforge-v2/internal/orchestrate"
)

const buildHarnessDescription = "Design a REUSABLE sub-harness: a named, versioned procedure for a shape of work this project will do again — steps, the tools those steps may use, and its own bounds. Saved, it is offered by the turn itself whenever somebody's words match it, so building one is how a good way of working stops depending on anybody remembering it. The goal is what the harness must DO, written for a designer that cannot see this conversation: when the person pointed at something here (\"build a harness for this\", \"…for what we just did\"), write that context into the goal — the files, the checks, the order — because the sentence you pass is the whole brief. It answers immediately with a TASK NUMBER and the design runs as that task: the person can open it, watch the page being written, talk to it, and stop it, and it moves through designing, then awaiting their look, then saved. Nothing is written to the registry unless they approve the card, and you will be told what became of it. Call list_harnesses first — a harness that already does this is one to run, not to build; asking for a CHANGE to one is a new design, so say what the whole harness must do rather than only what is different. Use it for a recipe worth repeating; for one-off work with many parts use run_adaptive, and for one self-contained piece use propose_task."

const buildHarnessSchemaJSON = `{"type":"object","properties":{` +
	`"goal":{"type":"string","description":"What the harness must do, self-contained. The designer never sees this conversation, so fold in whatever the person's words were pointing at: the work, the files, how a good result is checked."}` +
	`},"required":["goal"],"additionalProperties":false}`

const listHarnessesDescription = "List the sub-harnesses saved on this machine: each one's name, version and what it is for, and — for the ones this conversation designed — the task their design thread is in. Call it before build_harness — a harness that already does the work is one to run rather than design again — and whenever the person asks what shapes of work are saved here. A saved harness has no command that runs it: it is offered by the turn itself when somebody's words match it closely enough, and the person answers that card. So the useful thing to do with a name from this list is to say it in the conversation."

const listHarnessesSchemaJSON = `{"type":"object","properties":{},"additionalProperties":false}`

// runAdaptiveDescription teaches the one judgement this hand needs: a run is for
// a goal with parts, not for work you can do here.
//
// The default tank is interpolated rather than spelled, for the law a schema
// that said 40 while the executor applied 200 broke: a number written twice is a
// number that will disagree with itself.
var runAdaptiveDescription = "Start an ADAPTIVE RUN: a planner and a fleet of small workers taking one complex, many-part goal apart in parallel, beside this conversation. The planner cuts the goal into small nodes — one question, one artifact each — and re-plans every time one lands, so the shape of the work is discovered as it goes rather than guessed up front; a node whose prerequisites are done starts immediately, and each is a child agent with a context of its own. One fuel tank in dollars caps the WHOLE run, planner calls included: it says so at 80%, and at 100% it finishes what is in flight, starts nothing new, and asks the person to top it up, finish on what is done, or stop. You get the run's id at once — the run outlives this turn, the person can watch its graph and steer it, and its write-up arrives here as a note. Use it when the goal genuinely has independent parts and no known shape: an audit across many packages, a migration whose later steps depend on what the early ones find, a question that needs several investigations before it can be answered. Do NOT use it for work you can do here, for one self-contained piece (that is propose_task), or for a shape worth saving and repeating (that is build_harness)."

var runAdaptiveSchemaJSON = fmt.Sprintf(`{"type":"object","properties":{`+
	`"goal":{"type":"string","description":"The whole goal, self-contained. The planner cannot see this conversation: say what is to be found out or done, over what, and what a finished answer looks like."},`+
	`"fuel_dollars":{"type":"number","description":"Dollars the whole run may spend, the planner's own calls included. Omit for %s. Name a bigger tank only when the person did: a run that empties its tank pauses and asks for more with its frontier on screen, which is a better question than a large number guessed before anything has run."}`+
	`},"required":["goal"],"additionalProperties":false}`, orchestrate.Dollars(orchestrateDefaultCap))

// harnessTools are the model's hands on the two big machines, and the list that
// says whether one of them already exists.
//
// They come as a group because they are one decision with three answers — run
// what exists, build something that will exist, or plan something one-off — and
// a model that has the first two without the third builds a second harness for
// work this machine already knows how to do.
func (a *Agent) harnessTools() []bare.Tool {
	var tools []bare.Tool
	if a.canDesignHarness() {
		tools = append(tools, a.buildHarnessTool(), a.listHarnessesTool())
	}
	if a.canOrchestrate() {
		tools = append(tools, a.runAdaptiveTool())
	}
	return tools
}

// canDesignHarness is the build gate, and it is the gate the cue path kept: a
// store to write the page into, a runner to run what was written, and somebody
// watching who can answer the card. A design nobody can approve is two model
// calls spent on a page that will be dropped.
func (a *Agent) canDesignHarness() bool {
	return a.config.HarnessStore != nil && a.config.RunHarness != nil && a.config.AskConsent
}

// canOrchestrate is the run gate: a runner to launch one, and somebody watching
// who can answer the fuel gate — a run nobody can top up is a run that stops
// halfway and stays there.
func (a *Agent) canOrchestrate() bool {
	return a.config.OrchestrateRunner != nil && a.config.AskConsent
}

// buildHarnessTool starts one design and comes straight back.
//
// THE DESIGN IS A TASK, which is the whole of what this wrap does with the
// designer under it (harness_task.go). It admits a node, and the node's body is
// the same design that was always there: the same two calls against the same
// guide, the same card, the same registry. What the node adds is everything a
// task already has — a row with a live phase, a room with the design thread in
// it, an id, and a stop — for a piece of work that used to happen entirely out
// of sight.
//
// The announcement stays one line, and it now names the node: the turn is about
// to end with nothing said, so this line is the only thing on screen saying that
// work is happening, and the number on it is where to go and watch.
func (a *Agent) buildHarnessTool() bare.Tool {
	return bare.Tool{
		Name:        "build_harness",
		Description: buildHarnessDescription,
		Schema:      json.RawMessage(buildHarnessSchemaJSON),
		Execute: func(_ context.Context, args json.RawMessage) (string, bool, error) {
			var parsed struct {
				Goal string `json:"goal"`
			}
			if len(args) > 0 {
				if err := json.Unmarshal(args, &parsed); err != nil {
					return "Invalid arguments: " + err.Error(), true, nil
				}
			}
			goal := strings.TrimSpace(parsed.Goal)
			if goal == "" {
				return "Invalid arguments: build_harness needs a goal — what the harness must do, written for a designer that cannot see this conversation.", true, nil
			}
			call := a.designerCall("")
			model := call.model
			// THE LINE GOES OUT BEFORE THE NODE DOES. Admitting a design starts it
			// immediately — it has no dependencies and takes no slot — and a design
			// that finished quickly would put its card on the lane in front of the
			// sentence saying a design had started. So the id is minted first, the
			// announcement carries it, and the node is admitted last
			// (harness_task.go states the law).
			id := a.reserveHarnessDesign()
			a.emitHarness(Event{
				Kind:  EventHarnessDesign,
				ID:    id,
				Text:  goal,
				Hint:  harnessDesigningWord,
				Model: model,
				// Which node to go and watch. It is the only thing a surface can
				// act on from this event, and it is why the line is worth a
				// number at all (session.go's EventHarnessDesign).
				Task: &TaskNotice{ID: id},
			})
			a.admitHarnessDesign(id, goal, call)
			return fmt.Sprintf("task %d is designing a harness for: %s", id, goal) +
				"\nIt takes a minute or two and runs as that task, beside this conversation: the person can open task " +
				strconv.FormatUint(id, 10) +
				" to watch the page being written and to talk to it. What comes back is a page they are shown as a card; nothing is saved unless they approve it, and you will be told what became of it either way. Carry on with the work in front of you rather than waiting.", false, nil
		},
	}
}

// listHarnessesTool is the registry in three columns.
//
// It reads [Agent.harnessRegistry] and not the store, so a harness this
// conversation designed and saved a minute ago is in the list — the same law
// that makes it reachable from the very next sentence.
func (a *Agent) listHarnessesTool() bare.Tool {
	return bare.Tool{
		Name:        "list_harnesses",
		Description: listHarnessesDescription,
		Schema:      json.RawMessage(listHarnessesSchemaJSON),
		Execute: func(context.Context, json.RawMessage) (string, bool, error) {
			registry := a.harnessRegistry()
			if len(registry) == 0 {
				return "No harnesses are saved here yet. build_harness designs one when the work is a shape worth repeating.", false, nil
			}
			var out strings.Builder
			for _, entry := range registry {
				out.WriteString(entry.Name)
				// The version is dropped when the entry has none rather than
				// written v0: an unknown is nothing, never a zero.
				if entry.Revision > 0 {
					fmt.Fprintf(&out, " · v%d", entry.Revision)
				}
				if desc := strings.TrimSpace(entry.Description); desc != "" {
					out.WriteString(" · " + desc)
				}
				// THE THREAD, WHERE THIS SESSION KNOWS ONE. A harness designed in
				// this conversation has a room with its whole design story in it,
				// and the number is how anybody gets there; one designed last week
				// gets nothing rather than a guess (harness_task.go).
				if thread := a.harnessThread(entry.Name); thread > 0 {
					fmt.Fprintf(&out, " · designed in task %d", thread)
				}
				out.WriteString("\n")
			}
			out.WriteString("\nThere is no command that runs one: a harness is offered by the turn itself when the person's words match it, and they answer that card.")
			return out.String(), false, nil
		},
	}
}

// runAdaptiveTool launches one adaptive run and hands back its id.
//
// THE RUN OUTLIVES THIS CALL, which is the whole arrangement (orchestrate.go):
// the tool returns as soon as the run is registered, the conversation carries
// on, and the write-up arrives later as an ambient note. A tool that waited
// would freeze the conversation for twenty minutes on work the person can watch.
func (a *Agent) runAdaptiveTool() bare.Tool {
	return bare.Tool{
		Name:        "run_adaptive",
		Description: runAdaptiveDescription,
		Schema:      json.RawMessage(runAdaptiveSchemaJSON),
		Execute: func(ctx context.Context, args json.RawMessage) (string, bool, error) {
			var parsed struct {
				Goal string  `json:"goal"`
				Fuel float64 `json:"fuel_dollars"`
			}
			if len(args) > 0 {
				if err := json.Unmarshal(args, &parsed); err != nil {
					return "Invalid arguments: " + err.Error(), true, nil
				}
			}
			goal := strings.TrimSpace(parsed.Goal)
			if goal == "" {
				return "Invalid arguments: run_adaptive needs a goal — the whole thing to be worked on, written for a planner that cannot see this conversation.", true, nil
			}
			cap := parsed.Fuel
			if cap <= 0 {
				// A tank nobody named is the default, and a negative one is a
				// model spelling "I did not choose" the wrong way. Neither is
				// worth a refusal that costs the person the run.
				cap = orchestrateDefaultCap
			}
			// The model is the conversation's own: the run's planner and its
			// nodes think with whatever this session thinks with, and a run that
			// picked its own model would be spending the person's money on a
			// choice they never made.
			id, err := a.startOrchestrate(ctx, goal, "", cap)
			if err != nil {
				return "The run could not be started: " + err.Error(), true, nil
			}
			if id == "" {
				// The runner declined without saying why. Nothing started, and
				// saying so is better than an id that names nothing.
				return "Adaptive runs are not available in this build, so nothing started. Do the work here, or hand one self-contained piece to propose_task.", true, nil
			}
			a.announceOrchestrate(id, goal, "", cap)
			return fmt.Sprintf("adaptive run %s started on a %s tank: %s", id, orchestrate.Dollars(cap), goal) +
				"\nThe planner is cutting it into nodes now. It runs beside this conversation — the person can watch its graph, steer it, and answer it when the tank runs dry — and its write-up arrives here when it lands. Carry on rather than waiting.", false, nil
		},
	}
}
