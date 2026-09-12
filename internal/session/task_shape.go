package session

// THE BRIEF A PERSON'S OWN TASK IS ACTUALLY GIVEN.
//
// `/task write a blog post about our launch` used to be handed to an autonomous
// worker exactly as typed, under the one canned done-condition
// [taskPersonAcceptance] — no constraints, no quality bar, no definition of
// done. A sentence is a fine thing to say to somebody who can ask you a
// question; a node cannot ask anybody anything (task_contract.go), so a
// one-sentence brief is a worker guessing at every decision the sentence did
// not make, and the two things that come back are work that is stuck and work
// that is technically responsive and useless.
//
// So one auxiliary call reads what the person typed and writes the brief and the
// acceptance the worker is held to.
//
// ── IT IS A META-PROMPT, AND THAT IS THE WHOLE DESIGN ──
//
// prompts/shape.md contains no domain rules. It does not say what a blog post
// needs, what a migration needs, what a research task needs, because the next
// request is always some kind of work nobody enumerated. What it teaches is HOW
// TO REASON: name the domain, name who receives the output and what good means
// to them, name how this particular kind of work goes wrong, decide what a
// worker with nobody to ask needs settled, and say what done looks like to a
// second party. A rule list would have been a rule list that is wrong for
// whatever somebody types tomorrow.
//
// ── BESIDE THE WORKER, NEVER IN FRONT OF IT ──
//
// WHAT WAS TRUE: the call stood between the command and the graph. `/task`
// awaited it for up to [taskShapeWindow] before the node existed, behind the
// sizing judge's own wait, and on 2026-09-11 both ran their windows out on a
// thinking model and returned nothing — twenty-eight seconds of `shaping the
// brief…` on every `/task`, after which the worker got the person's sentence
// anyway (issue #936).
//
// WHAT IS TRUE NOW: the node is admitted at once on the person's own sentence
// and the canned done-condition, and the shaper is asked BESIDE THE NODE'S FIRST
// WORKER, through the same reading every other answer beside the work comes
// through (task_beside.go). The same model is asked the same question about the
// same words; only when it is asked has moved. When the answer lands
// ([Agent.deliverShaped]) the node's contract is written and the worker is
// handed it on its queue, in the same document it opened on — never a second
// brief format. A shaper that cannot answer changes nothing: the worker is
// already doing what the person typed, which is exactly what a cut shaper always
// meant.
//
// WHAT THIS COSTS, SAID PLAINLY. The worker's first steps are taken on the
// person's sentence rather than on the brief, which is what every `/task` whose
// shaper ran out its window was already doing; the brief reaches it a few steps
// in, and it is the brief the checker judges by from then on. And the shaper no
// longer names the work or places it: the node is named the moment it exists by
// the namer every unnamed door uses (taskname.go), and where it stands is
// decided at admission by the one ladder every door climbs (taskstands.go),
// which reads the paths in the person's own sentence — a folder cannot be moved
// under a worker that is already working in it.
//
// ── THEIR WORDS SURVIVE WHATEVER THE SHAPER DOES ──
//
// The shaped text goes in the spec's brief; the spec's REQUEST is still the raw
// sentence, and [composeBrief] prints it above the work under the heading that
// says whose words they are, with the rule that where the two read differently
// theirs are what was asked for. So a shaper that overreached is overruled by
// the document itself, and not by anybody having to notice. The prompt asks for
// the quote inside the brief as well, because the constraints it writes are
// attached to that quote and read as commentary on it — the doubling is
// deliberate and it is the cheap half of the guarantee, not the load-bearing
// one.

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/Agent-Field/aforge-v2/internal/lane"
	"github.com/Agent-Field/aforge-v2/internal/provider"
	"github.com/Agent-Field/aforge-v2/internal/roles"
	"github.com/Agent-Field/aforge-v2/internal/subharness"
	"github.com/Agent-Field/agentfield/sdk/go/ai"
)

// The shaper is a ROLE, registered from the file that makes the call, as
// internal/roles asks. HIGH and not low: the sizing judge beside it answers one
// bit and a wrong answer costs a chooser row, while this call writes the
// contract a worker is held to, and a vague brief is a whole task's spend on
// work nobody wanted. A person who disagrees pins it (`roles.shaper: <model>`).
func init() { roles.Register(roles.RoleShaper, roles.TierHigh) }

const (
	// taskShapeWindow is the shaper's own deadline.
	//
	// NOBODY WAITS ON IT. It used to be how long `/task` would hold a command the
	// person had just typed, and twenty-five seconds was chosen as the far edge
	// of a careful model's write that somebody would still sit through. The
	// reading runs beside a worker that is already at work now, so the figure is
	// the call's end rather than anybody's pause, and it is carried for
	// [Agent.guardianAllows]'s reason: the provider's client is built with no
	// timeout, and a reading beside the work must end on its own. How long the
	// shaper should be allowed to think is its own question and is not decided
	// here.
	taskShapeWindow = 25 * time.Second

	// THE SHAPER SENDS NO CEILING. It used to send ~1200 — the prompt's own
	// "under 300 words, never more than 600" turned into tokens, with room for
	// the JSON and for a model that runs slightly long. The prompt still says
	// it, which is where a length limit belongs: it is an instruction the model
	// can follow, rather than a guillotine this file drops on an answer it has
	// already paid for. The bounds below still hold what comes back.

	// The two bounds on what comes back. They are guards against a model that
	// ignored the prompt's own limit, not a second attempt at stating it: the
	// brief is generous because a long request legitimately earns a long brief,
	// and the acceptance is tight because a done-condition somebody else can
	// check is a sentence or two by construction.
	taskShapeBriefLimit      = 6000
	taskShapeAcceptanceLimit = 600
)

// taskPersonAcceptance is the done-condition a task the person typed carries
// until the shaper has written one. It is named rather than typed at each door
// for the one-source-of-truth reason: it is what a person's task is admitted
// with, and it is what the tests recognise "the shaper has not written this" by.
const taskPersonAcceptance = "Complete the brief and report the result and checks run."

// taskShapeRepair is the second and last thing said to a shaper that answered
// with something other than the object. It is [Agent.judgeDecomposable]'s move,
// and it restates the schema rather than only complaining, so a model that
// forgot the shape is told the shape.
const taskShapeRepair = `Repair the answer. Return only the exact JSON object required: {"brief":"...","acceptance":"..."}`

// shapedBrief is the wire form of the answer: the brief the worker is given and
// the done-condition it is judged by.
type shapedBrief struct {
	Brief      string `json:"brief"`
	Acceptance string `json:"acceptance"`
}

// shapeBrief turns what a person typed into what a worker is given, and answers
// false whenever it cannot: no shaper configured, an empty request, a call that
// was cut or never answered, or an answer that did not parse. Every one of those
// leaves the node exactly as it was admitted — on the person's own words and the
// canned acceptance — so the caller has nothing to do about any of them but
// nothing.
func (a *Agent) shapeBrief(ctx context.Context, request string) (shapedBrief, bool) {
	request = strings.TrimSpace(request)
	if request == "" {
		return shapedBrief{}, false
	}

	a.mu.Lock()
	call, err := roles.ResolveCall(roles.Source(a.config.RolesSource), roles.RoleShaper, a.model)
	closed := a.closed
	a.mu.Unlock()
	if closed || err != nil || strings.TrimSpace(call.Model) == "" {
		return shapedBrief{}, false
	}

	ctx, cancel := context.WithTimeout(ctx, taskShapeWindow)
	defer cancel()

	// THE SHAPER IS ALLOWED TO THINK, and that is the deliberate exception to
	// the reflex law (internal/reflex): the calls that are told not to think are
	// the ones that sort and name in a few words, and this one is asked to work
	// out what a domain's characteristic failure is. So it takes whatever level
	// the resolved call carries — a tier value spelled `model:high` reaches the
	// wire as high — and forces nothing of its own.
	if effort, ok := provider.ParseEffort(call.Effort); ok && effort != provider.EffortNone {
		ctx = provider.WithReasoningEffort(ctx, effort)
	}

	// WithoutStream for the title's reason: nobody asked for this call, and left
	// on a stream it would type a document into a room where somebody is reading
	// the worker. And a role for the same reason: shaping a brief is the
	// machine's own housekeeping beside somebody's work, so it is priced as an
	// errand and it never owns the clock (internal/lane's roles.go).
	ctx = provider.WithRole(provider.WithoutStream(ctx), lane.RoleAuxiliary)
	messages := []ai.Message{textMessage("system", shapePrompt), textMessage("user", request)}
	for attempt := 0; attempt < 2; attempt++ {
		response, callErr := a.completeWithModel(ctx, messages, call.Model)
		if callErr != nil || response == nil {
			return shapedBrief{}, false
		}
		// The person pays for it out of the same pocket the title and the
		// guardian come out of, and no turn asked for it.
		a.addAuxiliaryUsage(response, call.Model, 1)
		if shaped, ok := parseShapedBrief(response.Text()); ok {
			return shaped, true
		}
		if strings.TrimSpace(response.Text()) == "" {
			// NOTHING CAME BACK, so there is nothing to repair — the reflex
			// package's finding, which cost half an auxiliary bill before it was
			// made a rule: the repair prompt works by putting the model's own bad
			// answer in front of it, and against an empty answer it is a second
			// full-price call asking the identical question.
			return shapedBrief{}, false
		}
		messages = append(messages, textMessage("assistant", response.Text()),
			textMessage("user", taskShapeRepair))
	}
	return shapedBrief{}, false
}

// parseShapedBrief reads the answer back, through the same salvage ladder every
// JSON-answering auxiliary call on this base uses, so a fenced or smart-quoted
// object is not a wasted call.
//
// AN EMPTY BRIEF IS NOT AN ANSWER. A shaper that returned the object with
// nothing in it has not shaped anything, and writing a blank brief over the
// person's sentence would lose their task outright — which is the one thing
// this whole file is written not to do. An empty acceptance is survivable and
// falls back to the canned line, because the brief is the part no default can
// stand in for.
func parseShapedBrief(text string) (shapedBrief, bool) {
	raw, err := subharness.Salvage(text)
	if err != nil {
		return shapedBrief{}, false
	}
	var shaped shapedBrief
	if err := json.Unmarshal(raw, &shaped); err != nil {
		return shapedBrief{}, false
	}
	shaped.Brief = clip(strings.TrimSpace(shaped.Brief), taskShapeBriefLimit)
	if shaped.Brief == "" {
		return shapedBrief{}, false
	}
	shaped.Acceptance = clip(strings.TrimSpace(shaped.Acceptance), taskShapeAcceptanceLimit)
	if shaped.Acceptance == "" {
		shaped.Acceptance = taskPersonAcceptance
	}
	return shaped, true
}

// ── the brief, written beside the worker ────────────────────────────────────

// shapeBeside starts the shaper beside this node's first worker, and answers nil
// where the node's brief is not waiting to be written — every node that did not
// come through a person's own `/task`, and that one too once its brief exists.
//
// THE RECEIVER IS THE AGENT THAT OWNS THE NODE, and the worker is only who the
// answer is for. The shaper is the conversation's errand, billed to the pocket it
// always came out of and resolved against the conversation's roles.
//
// IT STARTS WITH THE WORKER, NOT BEFORE IT, which is what makes the answer's road
// one road. A shaper that could land before the worker had opened would need a
// second delivery — write the spec and say nothing — and a person's `/task` would
// open on a different document depending on which of two calls was quicker. The
// worker opens on the person's sentence every time, and the brief always reaches
// it the same way.
func (a *Agent) shapeBeside(ctx context.Context, node *TaskNode, room *taskRoom, tree taskTree, log io.Writer) *shapingBeside {
	request, waiting := node.briefToWrite()
	if !waiting {
		return nil
	}
	shaping := &shapingBeside{node: node, log: log}
	shaping.writing = beside(ctx, func(ctx context.Context) {
		shaped, ok := a.shapeBrief(ctx, request)
		if !ok || ctx.Err() != nil {
			return
		}
		shaping.handed = node.handShaped(room, tree, shaped)
		if !shaping.handed {
			fmt.Fprintln(log, "the brief was written after the worker had finished, so the work stands on the person's own words")
		}
	})
	return shaping
}

// shapingBeside is the brief being written beside a node's first worker. It is
// the second owner of a reading beside the work in this family, beside
// [sizingBeside], and it is joined at the same two moments: when the first
// worker's reading ends, and on every road out of the node's run.
type shapingBeside struct {
	writing *besideWork
	node    *TaskNode
	log     io.Writer
	// handed says a worker's queue took the brief. It is written by the reading
	// and read only after [shapingBeside.end] has joined it.
	handed bool
}

// end joins the writing, and says so where the brief reached the worker's queue
// and was never read: the worker's last step came first, so the work stands on
// the person's own words and nothing was written into its contract. It is safe
// on a nil value and more than once, because the node's run calls it from every
// road out.
func (s *shapingBeside) end() {
	if s == nil {
		return
	}
	s.writing.end()
	if s.handed && s.node.briefWaiting() {
		s.handed = false
		fmt.Fprintln(s.log, "the brief reached the worker after its last step, so the work stands on the person's own words")
	}
}

// handShaped hands the written brief to the worker in this node's room, and
// answers whether a reader took it.
//
// IT GOES THROUGH THE ONE DOOR every message into a conversation goes through
// ([deliverTo], mailbox.go): the room's seat resolves the reader and appends
// under the room's own lock, so a worker the runner has already withdrawn
// refuses it rather than taking a note nobody will drain. It is the runtime's
// own account of something that happened, never a person speaking.
//
// AND THE CONTRACT IS WRITTEN WHEN THE WORKER READS IT, not when its queue takes
// it. The note carries what writes it ([durableDelivery]): the moment the
// worker's own record holds the note — the drain that puts it in front of the
// model — is the moment the brief becomes what the work is judged by
// ([TaskNode.writeBrief]). So there is no instant in which the checker could hold
// the work to a done-condition the worker never read: a worker whose last step
// came before the note never reads it, and nothing is written.
//
// AND THE WORKER IS TOLD IN THE DOCUMENT IT OPENED ON, recomposed with the
// written contract ([TaskNode.instructionWith]) under one line of the harness's
// own ([briefWrittenBeside]). Writing a second format for the same contract is
// how the two would come to mean different things.
func (n *TaskNode) handShaped(room *taskRoom, tree taskTree, shaped shapedBrief) bool {
	note := briefNote(withReport(briefWrittenBeside, n.instructionWith(tree, shaped)))
	note.delivered = []durableDelivery{{settled: func() {
		if n.writeBrief(shaped) {
			n.graph.republish(n)
		}
	}}}
	return deliverTo(delivery{origin: fromRuntime, kind: msgNotice, note: note},
		roomSeat{at: conversationOf(n), room: room}).accepted()
}

// briefWrittenBeside is the one sentence of the harness's own that stands over
// the written brief, and it says the two things the worker cannot work out for
// itself: this is the same task, now written out, and it is what the work is
// judged by from here — while the person's words above it still win.
const briefWrittenBeside = "YOUR BRIEF IS WRITTEN OUT NOW. This is the same task you are already doing, set down in full: the person's request is unchanged and still decides, and the work and the done-condition below are what this work is judged by from here. Carry on from where you are; do not start again."

// briefToWrite answers the person's sentence where this node's brief is still
// waiting for the shaper, and false for every other node.
func (n *TaskNode) briefToWrite() (string, bool) {
	n.graph.mu.Lock()
	defer n.graph.mu.Unlock()
	if !n.spec.unshaped {
		return "", false
	}
	return n.spec.request, true
}

// briefWaiting reports whether the node's brief is still the person's sentence
// standing in.
func (n *TaskNode) briefWaiting() bool {
	_, waiting := n.briefToWrite()
	return waiting
}

// instructionWith is the worker's document as it reads once this brief replaces
// the stand-in. It changes nothing; [TaskNode.writeBrief] is the one write.
func (n *TaskNode) instructionWith(tree taskTree, shaped shapedBrief) string {
	n.graph.mu.Lock()
	defer n.graph.mu.Unlock()
	return n.instructionLocked(tree, shaped.writtenLocked(n))
}

// writtenLocked is the node's contract with this brief in place of the stand-in,
// read under the graph's lock.
//
// THE ASSEMBLED BRIEF OPENS ON THE SPEC'S, because [TaskGraph.briefLocked] builds
// it that way — the admitted brief, then what the work ahead of it reported, then
// the standing orders — so the written brief replaces the one it stood in for and
// everything assembled around it is kept.
func (b shapedBrief) writtenLocked(n *TaskNode) taskContract {
	assembled := n.brief
	if around, ok := strings.CutPrefix(n.brief, n.spec.brief); ok {
		assembled = b.Brief + around
	}
	return taskContract{assembled: assembled, acceptance: b.Acceptance}
}

// writeBrief puts the shaper's contract into the node, once, and answers whether
// it did.
//
// IT IS NOT A REVISION, and assignment.go's law — the admitted spec never
// changes, and only the person's direction moves the goal — is untouched by it.
// A person's `/task` is admitted with its contract UNWRITTEN: [taskSpec.unshaped]
// says the brief is their sentence standing in, and this is the one writer that
// may replace it, the one time. What it writes is the contract the work was
// always going to be held to, arriving late; the assignment's overlay, and every
// revision a person makes, still stand on top of it.
func (n *TaskNode) writeBrief(shaped shapedBrief) bool {
	n.graph.mu.Lock()
	defer n.graph.mu.Unlock()
	if !n.spec.unshaped {
		return false
	}
	contract := shaped.writtenLocked(n)
	n.brief = contract.assembled
	n.spec.brief, n.spec.acceptance = shaped.Brief, shaped.Acceptance
	n.spec.unshaped = false
	return true
}
