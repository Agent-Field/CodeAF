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

	"github.com/Agent-Field/aforge-v2/internal/lane"
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
	// TaskShapeWindow is how long /task will wait for the shaper before the
	// person's words go through untouched.
	//
	// The sizing judge next door gets three seconds because it answers one bit
	// of JSON. This one writes three paragraphs on a careful model that may take
	// a thinking pass first, so three seconds would mean it never once lands.
	// Twenty-five is the far edge of that write and still a wait somebody will
	// sit through for a command they just typed; past it the note on screen has
	// stopped meaning anything, and what they get instead — their own sentence,
	// started immediately — is precisely what they asked for.
	TaskShapeWindow = 25 * time.Second

	// taskShapeTokens is the ceiling. The prompt asks for under 300 words and
	// forbids more than 600, so ~1200 tokens is that bound with room for the
	// JSON around it and for a model that runs slightly long — not room for an
	// essay, which the prompt spends a paragraph refusing.
	taskShapeTokens = 1200

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

// TaskShapeFallbackNote is the one dim line the surface carries on the started
// row when a shaper ran and was cut. VOCABULARY LAW: no machinery words — not
// "cut", not "timeout", not "window". What the person needs to know is that
// what the worker got is what they typed, and nothing more than that.
const TaskShapeFallbackNote = "brief kept as you wrote it"

// taskShapeRepair is the second and last thing said to a shaper that answered
// with something other than the object. It is [Agent.judgeDecomposable]'s move,
// and it restates the schema rather than only complaining, so a model that
// forgot the shape is told the shape.
const taskShapeRepair = `Repair the answer. Return only the exact JSON object required: {"title":"...","brief":"...","acceptance":"...","where":"..."}`

// shapedBrief is the wire form of the answer. FellBack is NOT part of that
// wire form: it is set in [Agent.shapeBrief] when a shaper was actually
// invoked and came back cut — an error off the call, the deadline among them —
// and never by anything a model wrote. It is what StartTask reads to tell the
// surface the one honest line about it; every other failure path (no shaper
// configured, an empty request, an answer that arrived whole but did not
// parse) stays the documented silent pass-through.
type shapedBrief struct {
	Title      string `json:"title"`
	Brief      string `json:"brief"`
	Acceptance string `json:"acceptance"`
	Where      string `json:"where"`
	FellBack   bool   `json:"-"`
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
	ctx, cancel := context.WithTimeout(ctx, TaskShapeWindow)
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
	// an answer. THE ONE EXCEPTION IS A CALLER THAT ASKED TO WATCH THIS CALL AND
	// ONLY THIS CALL ([WithBriefWatch]) — and it is not the exception it looks
	// like, because the observer it installs is its OWN and the conversation's is
	// still shut out. Nothing is typed into the room; the words go to whoever is
	// drawing the wait this call is the reason for.
	watch := briefWatchFrom(ctx)
	messages := []ai.Message{textMessage("system", shapePrompt), textMessage("user", request)}
	for attempt := 0; attempt < 2; attempt++ {
		// And a role for the same reason it is made without the stream: shaping a
		// brief is the machine's own housekeeping beside somebody's turn, so it
		// is priced as an errand and it never owns the clock (internal/lane's
		// roles.go).
		//
		// THE WATCH IS REBUILT PER ATTEMPT, so a repair round starts its
		// accumulation from nothing: the second answer replaces the first, and a
		// watcher handed the two concatenated would be reading a document that
		// was never written.
		response, callErr := a.client.CompleteWithMessages(
			provider.WithRole(watchedShapeContext(ctx, watch), lane.RoleAuxiliary), messages,
			ai.WithModel(call.Model), ai.WithMaxTokens(taskShapeTokens))
		if callErr != nil || response == nil {
			// A SHAPER THAT RAN AND WAS CUT IS NOT THE SILENT PASS-THROUGH. The
			// no-shaper paths above are the documented absence of the capability
			// and say nothing; but here a call the person never asked for was
			// made, ran, and did not finish — the deadline among the reasons — so
			// the brief is their own words AND the surface is told one honest
			// line about it. A shaper that answered whole and failed to parse is
			// below, and is not a cut.
			fellBack := callErr != nil
			shaped := unshaped(request)
			shaped.FellBack = fellBack
			return shaped
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
	shaped.Where = strings.TrimSpace(shaped.Where)
	return shaped, true
}

// ── the brief, while it is still being written ──────────────────────────────

// THE GAP THIS CLOSES. `/task` says `⠙ shaping the brief… · 13s` and, until
// this existed, said nothing else for those thirteen seconds — the longest
// silence on the surface, in front of a person who has just typed a command and
// has no way to tell a careful model from a stuck one. The words are being
// written the whole time; they simply had nowhere to go.
//
// So the shaping call may be WATCHED, by the one caller that raised the wait,
// and by nobody else. It is opt-in through the context for [provider.Emit]'s
// reason — a session with no surface in front of it installs nothing and pays a
// context lookup — and it is a separate door from the conversation's observer
// rather than a share of it, because the two are about different things: that
// one is the reply somebody is reading, and this is the machine's own errand
// beside it.
//
// WHAT ARRIVES IS RAW AND PARTIAL AND MUST BE TREATED AS SUCH. The watcher is
// handed the ACCUMULATED answer text so far, which is a prefix of a JSON object
// and is therefore not JSON: nothing may unmarshal it, and nothing may act on
// it. [PartialString] is the tolerant read of one field of such a prefix, and it
// is the same scanner a forming tool call's arguments are previewed through —
// one parser for the two streams, because two would drift.

type briefWatchKey struct{}

// BriefWatch is told what the shaper has produced so far, on every delta.
//
// IT IS HANDED BOTH HALVES BECAUSE ON A THINKING MODEL ONE OF THEM IS EMPTY FOR
// THE WHOLE WAIT. The shaper sits on the careful tier and is deliberately
// allowed to reason (see [Agent.shapeBrief]), and a reasoning model spends the
// visible seconds producing reasoning: a run measured against a real endpoint
// sent 437 stream events across twenty-five seconds and not one of them was an
// answer delta. A watch given only the answer is therefore a watch that hears
// nothing at all for exactly the wait it was built for.
//
// So `answer` is the reply text accumulated so far and `thinking` is the
// reasoning text accumulated so far, and it is the CALLER that decides what to
// do with each. Nothing here presents one as the other.
//
// Both are the whole of what has arrived, every time, on the goroutine making
// the call, in order. A surface must treat this as a wire and not as a hand into
// its own state: take the strings, hand them to whatever owns the screen, and
// return. `thinking` is bounded — the last [PartialStringLimit] bytes of it —
// because reasoning has no ceiling worth trusting and a preview is a tail.
type BriefWatch func(answer, thinking string)

// WithBriefWatch asks the shaper to report what it is producing as it arrives.
//
// A nil watch installs nothing, so a caller may pass one it computed without
// branching around this line.
func WithBriefWatch(ctx context.Context, watch BriefWatch) context.Context {
	if watch == nil {
		return ctx
	}
	return context.WithValue(ctx, briefWatchKey{}, watch)
}

func briefWatchFrom(ctx context.Context) BriefWatch {
	watch, _ := ctx.Value(briefWatchKey{}).(BriefWatch)
	return watch
}

// watchedShapeContext is the context one shaping attempt is made on: the
// conversation's observer taken off it, and — for a caller that asked to watch
// — an observer of this call's own put in its place.
//
// The accumulator lives here, one per attempt, which is what makes a repair
// round start from nothing.
func watchedShapeContext(ctx context.Context, watch BriefWatch) context.Context {
	if watch == nil {
		return provider.WithoutStream(ctx)
	}
	var answer strings.Builder
	// The reasoning goes into the bounded buffer the partial-argument scanner
	// already uses for the same job (toolhint.go): a think has no length anybody
	// can promise, and what a watcher wants from it is the end.
	var thinking tailBuffer
	return provider.WithStreamObserver(ctx, func(event provider.StreamEvent) {
		// THE TWO KINDS THAT CARRY WORDS, KEPT APART. StreamReasoning is the
		// model's working and StreamDelta is what it is actually answering; they
		// are accumulated separately so that nothing downstream can show one and
		// call it the other. Every other kind is a boundary with nothing in it.
		switch event.Kind {
		case provider.StreamDelta:
			if event.Delta == "" {
				return
			}
			answer.WriteString(event.Delta)
		case provider.StreamReasoning:
			if event.Delta == "" {
				return
			}
			thinking.writeString(event.Delta)
		default:
			return
		}
		watch(answer.String(), thinking.text())
	})
}
