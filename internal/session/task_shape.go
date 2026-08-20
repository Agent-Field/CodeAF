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
// So one auxiliary call stands between the command and the graph. It reads what
// the person typed and writes the brief and the acceptance the worker gets — and
// the NAME the rail calls the work, because working out a good name is the same
// reading, and a second call to a second prompt would be a second bill and a
// second thing to keep in step.
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
// ── FAILURE IS SILENT PASS-THROUGH ──
//
// No model, no answer, a stall, prose where JSON was asked for: the task starts
// with the person's own words and the canned acceptance, exactly as it did
// before this file existed. A PERSON'S TASK IS NEVER BLOCKED OR LOST BY THE
// SHAPER — the capability is absent for that one start, not broken, and nothing
// on screen reports a fault about a call nobody asked for.
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
	"strings"
	"time"

	"github.com/Agent-Field/aforge-v2/internal/provider"
	"github.com/Agent-Field/aforge-v2/internal/roles"
	"github.com/Agent-Field/aforge-v2/internal/subharness"
	"github.com/Agent-Field/agentfield/sdk/go/ai"
)

// The shaper is a ROLE, registered from the file that makes the call, as
// internal/roles asks. HIGH and not low: the sizing judge beside it answers one
// bit and a wrong answer costs a chooser row, while this call writes the only
// document a worker will ever read, and a vague brief is a whole task's spend on
// work nobody wanted. A person who disagrees pins it (`roles.shaper: <model>`).
func init() { roles.Register(roles.RoleShaper, roles.TierHigh) }

const (
	// taskShapeWindow is how long /task will wait for the shaper before the
	// person's words go through untouched.
	//
	// The sizing judge next door gets three seconds because it answers one bit
	// of JSON. This one writes three paragraphs on a careful model that may take
	// a thinking pass first, so three seconds would mean it never once lands.
	// Twenty-five is the far edge of that write and still a wait somebody will
	// sit through for a command they just typed; past it the note on screen has
	// stopped meaning anything, and what they get instead — their own sentence,
	// started immediately — is precisely what they asked for.
	taskShapeWindow = 25 * time.Second

	// taskShapeTokens is the ceiling. The prompt asks for under 300 words and
	// forbids more than 600, so ~1200 tokens is that bound with room for the
	// JSON around it and for a model that runs slightly long — not room for an
	// essay, which the prompt spends a paragraph refusing.
	taskShapeTokens = 1200

	// taskShapeTemp is zero because A BRIEF IS A CONTRACT. The same request
	// typed twice should shape the same way twice, and a person who re-ran a
	// command to get a different brief would be gambling rather than working.
	taskShapeTemp = 0

	// The two bounds on what comes back. They are guards against a model that
	// ignored the prompt's own limit, not a second attempt at stating it: the
	// brief is generous because a long request legitimately earns a long brief,
	// and the acceptance is tight because a done-condition somebody else can
	// check is a sentence or two by construction.
	taskShapeBriefLimit      = 6000
	taskShapeAcceptanceLimit = 600
)

// taskPersonAcceptance is the done-condition a task the person typed carries
// when nothing shaped it. It is named rather than typed at each door for the
// one-source-of-truth reason: it is what BOTH person-task paths fall back to,
// and it is what the tests recognise "the shaper did not run" by.
const taskPersonAcceptance = "Complete the brief and report the result and checks run."

// taskShapeRepair is the second and last thing said to a shaper that answered
// with something other than the object. It is [Agent.judgeDecomposable]'s move,
// and it restates the schema rather than only complaining, so a model that
// forgot the shape is told the shape.
const taskShapeRepair = `Repair the answer. Return only the exact JSON object required: {"title":"...","brief":"...","acceptance":"..."}`

// shapedBrief is the wire form of the answer.
type shapedBrief struct {
	Title      string `json:"title"`
	Brief      string `json:"brief"`
	Acceptance string `json:"acceptance"`
}

// unshaped is what a caller is handed when no shaper ran: the person's own
// sentence as the brief, the canned done-condition, and NO NAME — an empty title
// is the signal that the mechanical one ([taskPersonTitle]) is what this task
// gets, and it is spelled once here so all six failure paths agree on it.
func unshaped(request string) shapedBrief {
	return shapedBrief{Brief: request, Acceptance: taskPersonAcceptance}
}

// shapeBrief turns what a person typed into what a worker is given — and, in the
// same breath, into the two or three words the rail will call it. It answers with
// their own words and the canned acceptance whenever it cannot.
//
// THE NAME RIDES THE CALL THAT WAS ALREADY BEING MADE. Naming a task well means
// reading what the task IS, which is the exact reading this call already does and
// pays for; a second small call to a second small prompt would be a second bill,
// a second thing to keep in step with the first, and a second way for the two to
// disagree about the same work. So the shaper answers with three fields instead
// of two, and `/task` costs precisely what it cost before.
//
// Every return path is a whole answer, never an error: there is nothing a caller
// could usefully do with a failure here except start the task anyway, which is
// what [unshaped] already says.
func (a *Agent) shapeBrief(ctx context.Context, request string) shapedBrief {
	request = strings.TrimSpace(request)
	if request == "" {
		return unshaped(request)
	}

	a.mu.Lock()
	call, err := roles.ResolveCall(roles.Source(a.config.RolesSource), roles.RoleShaper, a.model)
	closed := a.closed
	a.mu.Unlock()
	if closed || err != nil || strings.TrimSpace(call.Model) == "" {
		return unshaped(request)
	}

	// IT CARRIES ITS OWN DEADLINE, for [Agent.guardianAllows]'s reason: the
	// provider's client is built with no timeout, so a stalled shaper would hold
	// a command the person just typed until somebody interrupted the session.
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
	// an answer.
	messages := []ai.Message{textMessage("system", shapePrompt), textMessage("user", request)}
	for attempt := 0; attempt < 2; attempt++ {
		response, callErr := a.client.CompleteWithMessages(provider.WithoutStream(ctx), messages,
			ai.WithModel(call.Model), ai.WithTemperature(taskShapeTemp), ai.WithMaxTokens(taskShapeTokens))
		if callErr != nil || response == nil {
			return unshaped(request)
		}
		// The person pays for it out of the same pocket the title and the
		// guardian come out of, and no turn asked for it.
		a.addAuxiliaryUsage(response, call.Model, 1)
		if shaped, ok := parseShapedBrief(response.Text()); ok {
			return shaped
		}
		if strings.TrimSpace(response.Text()) == "" {
			// NOTHING CAME BACK, so there is nothing to repair — the reflex
			// package's finding, which cost half an auxiliary bill before it was
			// made a rule: the repair prompt works by putting the model's own bad
			// answer in front of it, and against an empty answer it is a second
			// full-price call asking the identical question.
			return unshaped(request)
		}
		messages = append(messages, textMessage("assistant", response.Text()),
			textMessage("user", taskShapeRepair))
	}
	return unshaped(request)
}

// parseShapedBrief reads the answer back, through the same salvage ladder every
// JSON-answering auxiliary call on this base uses, so a fenced or smart-quoted
// object is not a wasted call.
//
// AN EMPTY BRIEF IS NOT AN ANSWER. A shaper that returned the object with
// nothing in it has not shaped anything, and admitting a blank brief would lose
// the person's task outright — which is the one thing this whole file is
// written not to do. An empty acceptance is survivable and falls back to the
// canned line, because the brief is the part no default can stand in for.
//
// AND AN EMPTY TITLE IS SURVIVABLE TOO, for the same reason and with a different
// stand-in: [taskPersonTitle] cuts a serviceable name out of the person's own
// first eight words, so a shaper that answered with two fields where three were
// asked for costs a good name and nothing else. It is left empty here rather
// than filled in, because this function does not have the request to cut.
//
// THE NAME IS CLEANED BY THE SAME HAND THAT CLEANS THE SESSION'S ([cleanTitle],
// title.go). A model asked for a short lowercase name answers "Title: ..." or
// quotes it or welds it into a slug at exactly the same rates whichever prompt
// asked, and one repair belongs in one place.
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
	shaped.Title = cleanTitle(shaped.Title)
	return shaped, true
}
