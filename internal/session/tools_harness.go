package session

// THE BIG MACHINERY, PUT IN THE MODEL'S HANDS.
//
// Designing a sub-harness was reached by ANCHORED CUE and by nothing else: a
// sentence that began "make a harness for …" started a designer, and everything
// the person said in any other shape went to the model, which had no verb for it
// and answered with prose about harnesses instead of building one.
//
// THE DECIDING IS THE MODEL'S. That is the law this file exists to keep, and it
// is worth stating plainly because the rest of this package leans the other way:
// detection is a table lookup, the build cue was a table lookup, and both are
// written that way because a model call on every turn is a tax and a wrong yes
// is somebody's turn. A TOOL COSTS NEITHER. It is not consulted unless the model
// reaches for it, so the judgement — is this work a saved recipe, or just work —
// is made once, by the thing in this program that can actually make it, with the
// whole conversation in front of it. "Build me something that does this every
// sprint" is a build; "we should make a harness for this one day" is not; no
// regular language can tell those apart and no regex should have to try.
//
// THERE IS ONE ROAD FOR ORDINARY WORK, AND THIS FILE NO LONGER OPENS A SECOND.
// The model used to carry `run_adaptive` here — a hand that started a planned
// graph of nodes beside the conversation — and it is gone rather than gated,
// because a verb the model has is a verb it reasons around: a plain research
// question reached for the planner on width alone, before the road that actually
// wins (one worker that divides itself from the material it opened,
// task_divide.go) had been looked at once. So a chat turn now has propose_task
// and nothing else for work that leaves it. THE PLANNER ENGINE IS UNTOUCHED —
// orchestrate.go still runs it for a conversation, and cmd/harness-design runs
// internal/orchestrate on a driver of its own — but in a conversation it is
// reached only by somebody NAMING a run in so many words, off the anchored cue
// ([orchestrateGoal]), never by a model deciding that this piece of work looks
// wide.
//
// WHAT THE TOOLS DO NOT DECIDE. No hand here commits anything a person did not
// approve: build_harness starts a design that ends in a card somebody says yes
// to (harness_build.go). The model chooses to ASK; the person still chooses to
// keep and to pay.
//
// EACH HAND IS ABSENT WHERE IT CANNOT WORK, which is this codebase's law for a
// belt (tools.go) and matters more here than anywhere: a model told it can build
// a harness plans around that ability for the rest of the conversation, long
// after the first refusal. So the gates are the same ones the cue path checked —
// a store to write into, a runner to run what is written, somebody watching who
// can answer the card — and a build that fails one of them simply does not have
// the verb.

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

	"github.com/Agent-Field/aforge-v2/internal/exec/bare"
)

const buildHarnessDescription = "Design a REUSABLE sub-harness: a named, versioned procedure for a shape of work this project will do again — steps, the tools those steps may use, and its own bounds. Saved, it is offered by the turn itself whenever somebody's words match it, so building one is how a good way of working stops depending on anybody remembering it. The goal is what the harness must DO, written for a designer that cannot see this conversation: when the person pointed at something here (\"build a harness for this\", \"…for what we just did\"), write that context into the goal — the files, the checks, the order — because the sentence you pass is the whole brief. It answers immediately with a TASK NUMBER and the design runs as that task: the person can open it, watch the page being written, talk to it, and stop it, and it moves through designing, then awaiting their look, then saved. Nothing is written to the registry unless they approve the card, and you will be told what became of it. Call list_harnesses first — a harness that already does this is one to run, not to build; asking for a CHANGE to a harness that is already SAVED is a new design, so say what the whole harness must do rather than only what is different. A design that is still on its card is not: the person changes that one by saying so in the design's own room and it is rewritten in place, so never start a second design because they want the first one altered. Use it for a recipe worth repeating; for one-off work use propose_task, wide or not — `wide` starts one worker that splits itself once it has opened the material, so width is never a reason to reach past propose_task."

const buildHarnessSchemaJSON = `{"type":"object","properties":{` +
	`"goal":{"type":"string","description":"What the harness must do, self-contained. The designer never sees this conversation, so fold in whatever the person's words were pointing at: the work, the files, how a good result is checked."}` +
	`},"required":["goal"],"additionalProperties":false}`

const listHarnessesDescription = "List the sub-harnesses saved on this machine: each one's name, version and what it is for, and — for the ones this conversation designed — the task their design thread is in. Call it before build_harness — a harness that already does the work is one to run rather than design again — and whenever the person asks what shapes of work are saved here. A saved one is a subharness like any other: it is on `/subharness` and runs from there, and it is also offered by the turn itself when somebody's words match it closely enough, and the person answers that card. So a name from this list is one to say in the conversation or to hand to `/subharness`."

const listHarnessesSchemaJSON = `{"type":"object","properties":{},"additionalProperties":false}`

// harnessTools are the model's hand on the one big machine it still starts, and
// the list that says whether the thing it is about to design already exists.
//
// They come as a pair because they are one decision with two answers — run what
// exists, or build something that will exist — and a model that has the second
// without the first builds a second harness for work this machine already knows
// how to do.
//
// THE THIRD ANSWER USED TO BE `run_adaptive`, and its absence is the point: the
// belt this function returns is byte-identical to a world where the chat never
// had a planner hand, which is what "absent, not refusing" means when the thing
// being taken away is a verb (this file's header).
func (a *Agent) harnessTools() []bare.Tool {
	tools := a.designThreadTools()
	if a.canDesignHarness() {
		tools = append(tools, a.buildHarnessTool(), a.listHarnessesTool())
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

// THE RUN GATE THAT USED TO SIT HERE IS GONE WITH THE HAND IT GUARDED. It read
// `OrchestrateRunner != nil && AskConsent`, and it was here because `run_adaptive`
// was on the belt above. The one path left that may open a planned run spells the
// same two conditions where it stands ([Agent.routeOrchestrate], orchestrate.go),
// and a gate kept alive in this file for a caller in another one is the second
// source of truth this codebase does not keep.

// ── THE HAND A DESIGN'S OWN THREAD HAS ──────────────────────────────────────
//
// build_harness above is the conversation's hand: it commissions a page. This is
// the hand INSIDE one of those designs, and there is exactly one of it.
//
// The design's room is a place a person stands in front of a page they have just
// read. What they say there is one of two things — a question about the page, or
// a change to it — and until this tool existed the thread could only ever do the
// first. Asked for a change it said, correctly and uselessly, that a revision was
// a new design and they should go and ask for one: a person looking at a draft,
// in the draft's own room, told to leave and start over. So the second thing they
// say is a verb now, and the deciding of which of the two they said is the
// model's, made once, with the page and the whole conversation in front of it —
// the same law the two hands above are written to (this file's header).
//
// IT COMMITS NOTHING, which is the bargain every hand in this file keeps. A
// rewrite ends in another card, and the registry is untouched until the person
// approves one.

const reviseDesignDescription = "The person wants the PAGE CHANGED. Call this with the change stated completely, and the harness is written again with their change in it: the card in front of them comes down, the designer rewrites the whole page, and a new card is raised for them to approve. Call it the moment they ask for something to be different — \"add a step that runs the linter\", \"drop the second check\", \"this should work over the whole repo, not one package\" — including when they say it sideways (\"this wouldn't catch a flake in a subpackage\", \"I don't think two steps is enough\"). Do NOT call it to answer a QUESTION: \"why two steps?\" and \"would this fit the nightly build?\" are answered here, from the page, and calling this on one of them throws away a draft the person was happy with. State the change WHOLE, in their own words plus whatever they were pointing at — the designer cannot see this room and rewrites the entire page from your one sentence. Nothing is saved by calling this, and nothing ever is until the person approves a card; it works only while their card is up, and says so if it is not."

const reviseDesignSchemaJSON = `{"type":"object","properties":{` +
	`"change":{"type":"string","description":"What the harness must do differently, stated completely and in the person's own terms. The designer sees only this sentence and the page it is rewriting, so carry what they said and what they were pointing at when they said it."}` +
	`},"required":["change"],"additionalProperties":false}`

// designThreadTools is that one hand, and nothing at all in every agent that is
// not a design's thread.
//
// The gate is the door itself being nil rather than a question asked about the
// config, and that is the absent-not-broken law at its sharpest: a thread told it
// can rewrite a page it has no design behind it would offer the person a rewrite
// every time, and be refused every time.
func (a *Agent) designThreadTools() []bare.Tool {
	revise := a.config.reviseDesign
	if revise == nil {
		return nil
	}
	return []bare.Tool{{
		Name:        "revise_design",
		Description: reviseDesignDescription,
		Schema:      json.RawMessage(reviseDesignSchemaJSON),
		Execute: func(_ context.Context, args json.RawMessage) (string, bool, error) {
			var parsed struct {
				Change string `json:"change"`
			}
			if len(args) > 0 {
				if err := json.Unmarshal(args, &parsed); err != nil {
					return "Invalid arguments: " + err.Error(), true, nil
				}
			}
			change := strings.TrimSpace(parsed.Change)
			if change == "" {
				return "Invalid arguments: revise_design needs the change — what the harness must do differently, stated completely, because the designer rewrites the whole page from it.", true, nil
			}
			if err := revise(change); err != nil {
				// The door's own sentence, kept. "This design is not waiting on an
				// answer right now" and "a change to this page is already being
				// made" are different facts about what to do next, and a flattened
				// "could not revise" would throw away the half that says which.
				return "That change did not reach the designer: " + err.Error(), true, nil
			}
			return "The change went to the designer and the card has come down. The page is being written again now — the person watches that happen in this room, a new card is raised when it is ready, and nothing reaches the registry unless they approve that one. Say in a line what you asked for, and wait.", false, nil
		},
	}}
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
			out.WriteString("\nEach of these is on `/subharness` and runs from there, and each is offered by the turn itself when the person's words match it, and they answer that card.")
			return out.String(), false, nil
		},
	}
}
