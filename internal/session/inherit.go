package session

// PROMOTION: THIS TURN, CARRIED ON BY SOMEBODY WHO ALREADY HOLDS WHAT IT READ.
//
// ── THE MEASURED FAILURE (owner's laptop, 2026-09-11, conversation 7598fe73) ──
//
// A one-line read-only design question. The chat read home-panel files for ten
// rounds in 48 seconds. The round-ten mark fired: a second model was shown an
// account of the work, drew `A | B | C | D`, and the turn was moved. A worker
// was started on the person's sentence, four drawn items and a list of POINTERS
// to the calls the chat had already run. Its first six calls re-read the chat's
// own results — 116 KB of them — and then it read fourteen more files: 551,000
// input tokens, 24% of them cached, 393 seconds, and not one item ticked. Asked
// why, the chat stopped it and answered out of its own earlier reading.
//
// THREE THINGS WERE WRONG AND THEY ARE THREE DIFFERENT MISTAKES:
//
//   - THE DECISION WAS MADE BY THE WRONG PARTY. A summary reader shown an
//     account of the work decided whether the model holding the work should stop
//     doing it. The one reader in the building that knows whether re-reading
//     everything is worth it is the one that already holds it.
//   - AT THE WRONG SIGNAL. Ten finished rounds is a count of the person's
//     waiting and says nothing whatever about whether the turn is in trouble. A
//     turn that reads ten files in 48 seconds is a turn that is working.
//   - AND THE HAND-OFF THREW THE CONTEXT AWAY. Everything the turn had learned
//     was compiled down to a brief and a list of pointers, and pointers are an
//     instruction to read it all again.
//
// ── SO THE TURN DECIDES, INFORMED; AND WHEN THE HARNESS DOES MOVE IT, IT
//    PROMOTES RATHER THAN RESTARTS ──
//
// This file is those two sentences. It holds:
//
//	THE NOTE ([checkpointChoiceNote]) that the early marks inject instead of
//	drawing: the facts this turn has run up, and the three roads on. It decides
//	nothing and moves nothing.
//
//	THE BOUND ([Agent.inheritFits]) that says whether a worker could hold this
//	turn's context at all, in the window's own arithmetic and not in a number
//	anybody picked.
//
//	THE NET ([Agent.turnHasRunAway]) that is what remains of the ceiling: an
//	ABSOLUTE reading of runaway rather than a count of rounds.
//
//	AND THE PROMOTION ITSELF ([Agent.promotedQuick]), which is the one place a
//	quick node is built to open on its caller's transcript, whether the model
//	asked for it with `inherit` or the net did it on the model's behalf.
//
// THERE IS ONE PROMOTION AND IT HAS ONE DOOR. The model reaching for
// `quick_task inherit:true` (task_quick.go) and the net moving a turn
// (checkpoint_quick.go) build the same spec through the same constructor with
// the same bound in front of it. A second road would be a second set of rules
// about what the worker opens on, which is exactly the divergence that let a
// brief-and-pointers worker exist beside a chat that already had the answer.

import (
	"fmt"
	"strings"

	"github.com/Agent-Field/agentfield/sdk/go/ai"
)

// ── THE NOTE ────────────────────────────────────────────────────────────────

// checkpointChoiceRule is the whole of what the harness says at a mark, and it
// is the law this file registers (lawregistry_test.go, class `event`).
//
// IT RIDES THE EVENT AND NOT THE PAGE, which is the prompt diet's own taxonomy
// applied rather than quoted: a rule that matters at one moment of a few turns
// in a hundred is a rule that costs nothing until that moment arrives, and the
// message announcing the moment is where it is delivered. Nothing of this
// sentence is in prompts/system.md and nothing of it is in a tool description;
// the page's own line names the two roads and this names what choosing between
// them COSTS.
//
// AND IT DECIDES NOTHING. The three roads are stated flat, in the order a model
// weighing them would: the cheapest first. There is no recommendation, no
// "consider", and no threshold quoted at the model — the facts above it are the
// argument, and the model holding the work is the only reader that can weigh
// them against what it still has to find out.
//
// THE COST IS SAID ONCE AND IT IS THE ONE FACT A MODEL CANNOT DERIVE. Everything
// else in the note it could work out for itself; whether a worker it starts
// opens on its transcript or on a paragraph about it is a fact about this
// harness, and it was measured costing 551,000 tokens and six and a half minutes
// to leave unsaid.
const checkpointChoiceRule = "Three roads on, and the choice is yours: answer now from what you have; " +
	"carry the rest on in a room, with `quick_task` and `inherit` set, which opens on this transcript " +
	"as you are holding it; or hand out the parts you have not read yet. A worker that does not inherit " +
	"opens on a brief about your work instead of on your work, and reads it all again. " +
	"Nothing has been decided for you and nothing has been moved."

// checkpointChoiceLead is the tag every harness note to a running turn wears,
// in the family looped.go's `[stuck]` and `[silent]` belong to: a bracketed
// word saying what kind of interruption this is, so the model can tell the
// harness's voice from the person's at a glance.
const checkpointChoiceLead = "[taking stock]"

// turnFacts is what a turn has actually run up, in the three quantities a model
// weighing "is it worth carrying all this somewhere else" has to have in front
// of it.
//
// EVERY ONE OF THEM IS ALREADY HELD AND NONE OF THEM IS A READING. The rounds
// are the meter's own count; the files are the paths this turn has named to a
// tool, counted once each; the bytes are what the tool results in the transcript
// actually weigh. No model is asked anything to produce this, which is the whole
// point — the reading is what cost eight seconds and decided nothing.
type turnFacts struct {
	rounds int
	files  int
	bytes  int
}

// turnOpenedAt is where THIS TURN begins in the transcript: the index of the
// message the person opened it with.
//
// THE THREE FIGURES HAVE TO BE ABOUT ONE ANSWER OR THEY ARE ABOUT NOTHING. The
// rounds come from the turn's own meter, and a walk of the whole session would
// have counted every file yesterday's conversation opened beside them — a note
// telling a model it is holding 400 KB after two rounds, which is exactly the
// kind of figure that makes a model do the wrong thing confidently.
//
// A STEER IS PART OF THE TURN IT LANDS IN, so the walk goes back over the tool
// traffic to the newest person's message and then keeps going while the
// messages before it are the person's too: a steer sits directly on the ask it
// steered, and stopping at the steer would start the count halfway through the
// work it was about.
func turnOpenedAt(messages []ai.Message) int {
	index := len(messages) - 1
	for index >= 0 && messages[index].Role != "user" {
		index--
	}
	for index > 0 && messages[index-1].Role == "user" {
		index--
	}
	if index < 0 {
		return 0
	}
	return index
}

// readTurnFacts walks THIS TURN once and answers all three, from the message it
// opened on ([turnOpenedAt]).
//
// A PATH IS WHAT THE CALL SAID IT WAS ABOUT, read through the one parser this
// package already has for a model's arguments ([checkpointArgumentNamed]). It
// is deliberately every tool that names a `path` and not a list of readers: a
// list would be a rule about which verbs count, kept by hand, and wrong the day
// somebody adds one.
func readTurnFacts(messages []ai.Message, from, rounds int) turnFacts {
	facts := turnFacts{rounds: rounds}
	if from < 0 || from > len(messages) {
		from = 0
	}
	seen := make(map[string]bool)
	for _, message := range messages[from:] {
		for index := range message.ToolCalls {
			path := checkpointArgumentNamed(message.ToolCalls[index].Function.Arguments, "path")
			if path == "" || seen[path] {
				continue
			}
			seen[path] = true
		}
		if message.Role != "tool" {
			continue
		}
		facts.bytes += messageBytes(message)
	}
	facts.files = len(seen)
	return facts
}

// line is the facts as a person's model reads them: one clause each, and the
// emptiness law over the two that can be zero — a turn that has read no file and
// holds no result says so by not mentioning either, rather than by claiming
// `0 files`.
func (f turnFacts) line() string {
	parts := []string{fmt.Sprintf("%d round%s so far", f.rounds, plural(f.rounds))}
	if f.files > 0 {
		parts = append(parts, fmt.Sprintf("%d file%s opened", f.files, plural(f.files)))
	}
	if f.bytes > 0 {
		parts = append(parts, fmt.Sprintf("%s of results in front of you", humanBytes(f.bytes)))
	}
	return strings.Join(parts, ", ")
}

// checkpointChoiceNote is the note itself: the tag, this turn's facts, and the
// rule. It is composed here so that the law is one const and the figures are
// one function, and neither can be edited without the other being read.
func checkpointChoiceNote(facts turnFacts) string {
	return checkpointChoiceLead + " " + facts.line() + ". " + checkpointChoiceRule
}

// humanBytes is KB above a kilobyte and bytes below it, because a note saying
// `118,784 bytes` is a note a reader has to do arithmetic on.
func humanBytes(bytes int) string {
	if bytes < 1024 {
		return fmt.Sprintf("%d bytes", bytes)
	}
	return fmt.Sprintf("%d KB", bytes/1024)
}

// ── THE BOUND ───────────────────────────────────────────────────────────────

// inheritFits reports whether a worker on `model` could hold this turn's context
// and still have room to work, and says why not when it could not.
//
// IT IS A PROPERTY AND NOT A NUMBER, and every term of it is a fact this build
// already holds:
//
//   - THE WINDOW is the model's own ([Agent.childWindow], off the catalog). A
//     window nobody can name is a refusal rather than a guess: handing a
//     transcript to a worker whose capacity is unknown is the one direction
//     where being wrong costs the whole node.
//   - WHAT OF IT IS USABLE is [ctxbudget.For] — the fill share, less the
//     completion reserve and the fixed floor — which is the same arithmetic
//     every other reader of a window in this build uses.
//   - AND ROOM TO WORK IS ONE TOOL RESULT AT THAT WINDOW ([bare.CapsFor]). It is
//     the smallest unit of work this harness has: a worker that cannot take one
//     more result is a worker that cannot take one more step, and a node started
//     in that state would spend its whole budget failing to read anything.
//
// SO THE TEST IS: the transcript, plus one result, inside the budget. Nothing
// here is a ratio and nothing here is tuned — change the person's fill percent
// or their completion reserve and this moves with them, because it is their
// arithmetic and not a second opinion about it.
func (a *Agent) inheritFits(model string) (refusal string, ok bool) {
	carried, room, known := a.contextRoomFor(model)
	if !known {
		return inheritNoWindow, false
	}
	if carried > room {
		return fmt.Sprintf(inheritTooBig, carried, room), false
	}
	return "", true
}

// contextRoomFor is the arithmetic both readers of a window share: what this
// conversation weighs, how far a conversation may grow in that model's window
// before this build has to start throwing part of it away, and whether anything
// knows the window at all.
//
// TWO READERS AND ONE SUM, because "could a worker hold this" and "can this turn
// still work here" are the same question asked about two models. They differ
// only in what an UNKNOWN window means, which is the one thing each caller
// decides for itself.
//
// THE LINE IS THE ONE THIS BUILD ALREADY DRAWS and not a second opinion about
// it. [compactThresholdOf] is how far a conversation may grow before compaction
// must fold it — the window less the larger of fifteen percent and sixteen
// thousand tokens, or the fill a person pinned held under the completion reserve
// (loop.go states the whole derivation, and internal/ctxbudget holds the figures
// it is built from). A conversation past that line cannot take another step
// without something it has already read being folded away, which is precisely
// what "cannot keep working in this window" means; below it, the reserve the
// line keeps back IS the room to work, so there is no second allowance to
// subtract here and no number of this file's own.
//
// WHAT THE CONVERSATION WEIGHS IS [Agent.ContextTokens] and not a walk of the
// messages: it is the provider's own count of the request it last served where
// there is one — the page, the tool schemas, every result, every argument — and
// the content estimate where the transcript has grown past it. A second reading
// taken here would be a second answer to a number a person can already see.
func (a *Agent) contextRoomFor(model string) (carried, room int, known bool) {
	window := a.childWindow(model)
	if window <= 0 {
		return 0, 0, false
	}
	room = compactThresholdOf(window)
	if room <= 0 {
		return 0, 0, false
	}
	return a.ContextTokens(), room, true
}

// The two refusals, spelled as the model reads them. Each says the fact and then
// the road that is still open, because a refusal that names no alternative is a
// refusal a model answers by trying the same call again.
const (
	inheritNoWindow = "`inherit` is refused: nothing here knows how big this model's context is, " +
		"so there is no way to tell whether it could hold yours. Start it without `inherit` and " +
		"write what it needs into `line` and `items`."
	inheritTooBig = "`inherit` is refused: this conversation is about %d tokens and a worker on this model " +
		"can hold about %d and still have room to work. Start it without `inherit` and write what it needs " +
		"into `line` and `items`, naming the files it should open."
)

// ── THE NET ─────────────────────────────────────────────────────────────────

// turnHasRunAway reports that this turn has stopped being an answer that is
// taking a while and become one that cannot finish where it is.
//
// ── WHY IT IS NOT A COUNT OF ROUNDS ──
//
// It was, and the count is what the measured failure was made of: ten finished
// rounds moved a turn that had read ten files in 48 seconds. A round is a good
// unit for THE PERSON'S WAITING — which is what the note above is priced in —
// and a useless one for TROUBLE, because a round says nothing about whether the
// turn is getting anywhere or whether it still can.
//
// ── SO WHAT IS LEFT IS ONE ABSOLUTE FACT, AND THE OTHER TWO ARE ALREADY WRITTEN ──
//
// THE CONTEXT IS THIS FUNCTION'S OWN and it is the honest reading of runaway: a
// turn whose transcript no longer leaves room for another tool result at its own
// window is a turn that cannot take another step, whatever it is doing. It is
// [Agent.inheritFits] read against THIS agent's model rather than a worker's —
// one function, two readers — so the bound that says a worker could not hold
// this context is the same bound that says the turn can no longer work in it.
// A turn that reads ten distinct files in 48 seconds is nowhere near it.
//
// THE CLOCK IS turnwall.go AND IS NOT REPEATED HERE. An unattended session's
// wall already bounds the stretch one turn may spend inline, at a share of the
// wall, and it is asked at every boundary AHEAD of this ladder. A second clock
// in this file would be a second mechanism for one shape.
//
// THE LOOP IS looped.go AND IS NOT REPEATED HERE EITHER. Past
// [loopNudgeCeiling] the watch marks the episode and the turn is handed over
// through [Agent.handOverLoopingTurn], which reaches the very same ceiling this
// net does. "Added nothing, twice" is that file's sentence and it stays there.
//
// AND THE CLAIM RUNG IS THE METER'S ([checkpointMeter.askAgainAt]): a turn that
// said it was finished and then did [checkpointPrice] more rounds of real work
// has disproved itself, which is an absolute fact about a claim rather than a
// count of anybody's patience.
func (a *Agent) turnHasRunAway() bool {
	carried, room, known := a.contextRoomFor(a.model)
	// AND A WINDOW NOBODY CAN NAME IS NOT A RUNAWAY. It is the opposite fail-open
	// direction from [Agent.inheritFits], and both are the conservative one for
	// their own question: a worker that MIGHT not hold a transcript is not handed
	// it, and a turn that MIGHT be near its window is not taken off the person who
	// is watching it. A doubt stops a promotion; it does not stop an answer.
	return known && carried > room
}

// ── THE PROMOTION ───────────────────────────────────────────────────────────

// promotedQuick is the ONE place a quick node is built to open on its caller's
// transcript. Both parties that can promote a turn come through it: the model
// asking for `inherit` (task_quick.go) and the runaway net moving the turn on
// its behalf (checkpoint_quick.go).
//
// IT IS THE ORDINARY CONSTRUCTOR WITH THE SEED HUNG ON IT and never a second
// one. Everything else about the node is untouched — kind quick, in place,
// items, claims, landing, the room, the rail row — which is the whole reason a
// promotion is cheap to reason about: it is the node this build already has,
// started on a transcript instead of on a page about one.
//
// THE SEED IS READ ONCE, HERE. [Agent.forkSeed] takes the caller's messages at
// this instant, and the system text that stands in front of them travels with
// it so the worker's first request is byte-identical to the caller's up to the
// brief — which is the whole economy of the thing (a provider prefix cache hit
// rather than 116 KB read again).
func (a *Agent) promotedQuick(line string, items, files []string) *quickTaskSpec {
	quick := newQuickTaskSpec(line, items, files)
	quick.seed, quick.system = a.forkSeed()
	return quick
}

// adoptSeed puts a promoted node's inherited transcript in front of its worker,
// and does nothing at all for the ordinary quick node that has none.
//
// IT IS THE HAND'S OWN INSTALL, and the copy is the copy-on-write: what is
// SHARED is what the messages point at — their text, their images — which
// nothing rewrites and where all the weight is; what is not shared is the slice,
// because this worker appends its own work to the tail and rewrites element zero
// at the start of every turn ([Agent.refreshSystemLocked]). Two agents writing
// one backing array is a race in the one place nobody would ever look.
//
// AND ELEMENT ZERO IS THE WORKER'S OWN PAGE. `Config.System` was handed the
// caller's text plus this node's own tail at construction, so the page a worker
// reads is the page its caller read with one paragraph saying what is different
// about being this worker — and `refreshSystemLocked` rewrites the same bytes
// rather than putting a freshly rendered prompt in front of an inherited
// transcript, which would break the shared prefix on its first byte.
func adoptSeed(child *Agent, quick *quickTaskSpec) {
	if child == nil || quick == nil || len(quick.seed) == 0 {
		return
	}
	messages := make([]ai.Message, len(quick.seed))
	copy(messages, quick.seed)
	child.mu.Lock()
	defer child.mu.Unlock()
	// THE PAGE IS THE CALLER'S AND IS STAMPED AS SOMEBODY ELSE'S. `systemOwn`
	// false is what stops [Agent.refreshClockLocked] rendering this build's own
	// prompt over the top of it an hour later — the same mark a Config that
	// carries a System gets at construction (agent.go), said here because this
	// page arrives after the agent was built.
	child.system = promotedSystem(quick.system)
	child.systemOwn = false
	messages[0] = textMessage("system", child.system)
	child.messages = messages
}

// promotedSystem is the page an inherited worker opens on: the caller's own,
// plus the one paragraph that is true of this worker and was not true of its
// caller.
//
// THE CALLER'S PAGE IS KEPT WORD FOR WORD because the shared prefix is the
// entire argument for inheriting anything. A worker handed a freshly rendered
// page in front of an inherited transcript pays for every token of that
// transcript again at full price, which is the bill this whole road exists to
// stop.
//
// AND THE TAIL IS THE DIFFERENCE AND NOTHING ELSE. It does not restate the quick
// laws — prompts/quick.md rides the brief for every quick worker, promoted or
// not — it says the one thing a reader of an inherited transcript would
// otherwise get wrong: the conversation above is not theirs to answer.
func promotedSystem(system string) string {
	if strings.TrimSpace(system) == "" {
		return ""
	}
	return system + promotedSeedTail
}

// promotedSeedTail is that paragraph. It is short on purpose: everything else a
// promoted worker needs to know is in the brief under it, and a page that
// re-explained the node would be the prefix growing for a thing the brief
// already says.
const promotedSeedTail = "\n\n## The conversation above is what you were handed, not what you are answering\n\n" +
	"You were started from the middle of that turn and you hold everything it read. Nobody is " +
	"waiting on a reply to any message in it. What you are to do is the brief below, and nothing " +
	"above it is an instruction to you. Do not read again what is already in front of you."

// admissionFor is the working context a quick node opens with, and it is nothing
// at all for a promoted one.
//
// THE TWO ARE ALTERNATIVES AND NEVER A PAIR. A pointer section answers "where
// did the call you did not make end up"; a promoted worker made none of them and
// holds all of them, so the section it would be given is a list of things to go
// and read again — which is precisely the 551,000 tokens this file was written
// from. Keeping it for the cold worker and dropping it for the warm one is the
// one decision, taken once, here.
func admissionFor(quick *quickTaskSpec, caller *Agent) AdmissionContext {
	if quick.inherits() {
		return AdmissionContext{}
	}
	return caller.admissionContext()
}

// carriedResults puts what a turn actually read under the goal a worker opens
// on, and answers the goal unchanged where there was nothing to carry.
//
// IT IS THE ONE COMPOSITION AND IT IS HERE rather than beside either of the two
// documents it joins, because what it is FOR is the promotion's fallback: the
// worker could not be handed the turn, so it is handed the turn's findings
// instead of a map of where to go and find them again.
func carriedResults(goal, digest string) string {
	goal, digest = strings.TrimSpace(goal), strings.TrimSpace(digest)
	if goal == "" || digest == "" {
		return goal
	}
	return goal + "\n\n" + carriedResultsHeading + "\n" + carriedResultsRule + "\n\n" + digest
}

// carriedResultsHeading and carriedResultsRule are how the worker reads it. The
// rule says the two things that are true of this section and of nothing else in
// the brief: it is what has ALREADY happened, and it is CUT — so a worker that
// needs the whole of one of these goes and gets that one, rather than starting
// the reading over.
const (
	carriedResultsHeading = "WHAT THIS WORK ALREADY FOUND OUT"
	carriedResultsRule    = "The account of the turn you are carrying on from: what was called, and the end of what came back. It is cut, newest first, and it says how many older ones it left out. This has already happened. Do not run these again to see what they said; where you need the whole of one, run that one."
)

// workerModelFor settles which model a quick node asked for with this word will
// actually run on, through the resolver the door itself uses (taskmodel.go).
//
// IT EXISTS SO THAT A ROAD CAN ASK THE BOUND BEFORE IT COMMITS. The door
// resolves the model and answers a refusal a MODEL reads; the carry-on road has
// no model to read one and needs the answer in order to choose between
// promoting the turn and briefing a worker instead — so it asks the same
// question of the same model, rather than of this conversation's own, which is
// routinely not the one the work leaves on ([Agent.defaultTaskModel]).
func (a *Agent) workerModelFor(word string) string {
	choice := a.resolveTaskModel(strings.TrimSpace(word))
	if len(choice.options) > 0 {
		return settleTaskModel(choice.options, "")
	}
	return choice.model
}
