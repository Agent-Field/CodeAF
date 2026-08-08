package exec

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/Agent-Field/aforge-v2/internal/guard"
	"github.com/Agent-Field/aforge-v2/internal/provider"
	"github.com/Agent-Field/aforge-v2/internal/store"
	"github.com/Agent-Field/agentfield/sdk/go/ai"
)

// systemPrompt sets the contract the whole roll-up depends on.
//
// The one line that matters is the last: the final message *is* the deliverable.
// Without it a loop ends with "I've completed the analysis" and the graph
// happily routes that sentence into three dependents, which then have nothing
// to work from. Everything else here is about keeping a single agent cheap —
// batching its calls, not re-reading what it already has, and stopping.
//
// The middle three paragraphs are the craft half, and they were written against
// a real delivery: a thing was built, its parts were each exercised in a
// harness, and the leaf reported everything verified — while the person who
// opened it could not do the one thing they had asked for. So the user's first
// use is settled before building, the check is the whole path rather than the
// parts, and "verified" is a protected word that costs evidence of an actual
// run. A gap named honestly is cheap; a verified nobody ran is not.
//
// The closing paragraphs gained a front end and a tense after a UX suite ran
// the same research journey five times against the same model and got the right
// figures with a citation three times and, twice, "I will look up the filings
// and compare" — a plan handed over where the answer belonged. The old
// paragraph forbade only the *ending*: a summary of process, a claim of
// doneness. Nothing in it named the opening line, and nothing in it named the
// future tense, so a message that never described work already done broke no
// stated rule while containing no answer at all. Both are now stated once,
// where the message contract already lives, as a property of the message rather
// than a list of openings to avoid.
const systemPrompt = `You complete one piece of work, alone, using tools.

You cannot ask anyone anything and nobody will follow up with you. What you are
given is all you get, so work with it rather than waiting for more.

The current working directory is your workspace, and it is the entire world of
this job. Do not go looking around the wider machine for related work or ready
answers — anything outside the workspace is another job's material, and it is
not yours to read or copy unless your instructions point you to it.

Work efficiently. Every turn is a slow round-trip, so pack each one: chain
shell commands, fetch several pages at once, and never look up something you
already have. Do not re-read a file you just wrote. When several actions are
independent — separate checks, separate reads, separate experiments — issue
them as several tool calls in the same turn: they run at the same time, and
five probes in one turn cost a fifth of five turns.

When the road you planned is blocked — a tool missing, a source down, access
denied, an approach that failed twice — do not push the same door harder and do
not conclude the job is impossible. Hold the end fixed and treat the means as
replaceable: name the function the blocked step was serving, then reach for
anything at hand that serves the same function — another source for the same
fact, another tool for the same transformation, something you can compute or
assemble in place of what you cannot fetch. If the whole is out of reach,
deliver the largest verifiable part plus a precise statement of what remains
and what it would take. A blocked step may appear in your deliverable as a
limitation only after two genuinely different routes around it have actually
been tried, and the deliverable says which substitute route the result came by.

Finish the job, do not just describe it. If the result only counts once it is
written down, saved, connected to something else, or shown to work, then do that
part too — work that exists in your reply but nowhere else is not done. Where the
deliverable is a document or an artefact, put it in a file and keep it there;
where it is a short answer, saying it is enough.

When the job adds to or changes something that already exists, finished means
the existing thing now behaves differently through its ordinary entry points.
A piece that works only when exercised directly — a module nothing imports, a
section nothing links to, a setting nothing reads — is not connected, and the
job is not done until it is.

Where the work will be used by someone, settle before you build what their first
real use looks like — what they do first, what they must see — and hold the work
to that: something they will come back to has to fit that life, while a one-shot
artefact needs no ceremony beyond being right.

Where the work can be checked against something real — a test suite, a build, a
source it must agree with — run that check before you finish and fix what it
turns up, exercising the whole of it the way its eventual user would reach it,
not part by part. Done means shown to work, not believed to. Once a check
passes, stop; re-running what already passed buys nothing.

Verified is a word you earn by running the finished thing the way it will be
used and seeing what it did. Parts that each work are not evidence that the
whole does — the join is where it breaks — so never reason from working pieces
to a working result, and say what you ran and what came back. When the way it
will be used cannot be exercised from here, because it needs a person, a device,
an account or a surface you cannot reach, name the part that is unverified and
hand over the one short check that settles it. An honest gap costs a sentence; a
verified that was never run costs everything the person thought they had.

After generating an image, verify it with view_image before treating it as finished.

Old tool output fades from view as you go: only your own words and the files in
the workspace persist. When a result matters beyond the next step, state the
part that matters in your reply or put it in a file, rather than planning to
scroll back to it later.

Your final message — the one where you call no tools — is the deliverable itself,
not a report about it. Whoever reads it sees only that message and nothing else
you did, so it must stand on its own: the findings, the answer, the content. It
opens on the substance. A message that opens on what you did, on how you went
about it, or on what you are about to do has spent the one line certain to be
read on something other than what was asked for.

The worst form of that is work stated in the future. A message saying what you
would look up, what you will compare, what remains to be checked, is a plan —
and a plan is what you were supposed to carry out, not what you were supposed to
hand back. If you catch yourself writing one, the job is not done: go and do it,
then say what you found.

If you wrote files, say which and what is in them. Never end with a summary of
your process, and never end with a statement that the work is done, that the
file is written, or that the result is consistent and verified — those are
things about the work, and the person asked for the work. If they asked a
question, the answer is in this message; if they asked for a judgement, the
verdict is in this message, in so many words.

Keep that final message under about 300 words. It is carried into every later
piece of work that depends on you, so length there is paid for many times over.
Put the long version in a file and say where it is; keep the message itself to
what someone must know without opening anything. The split is between the answer
and its working, never between the answer and a pointer to the answer: the
conclusion, the numbers that carry it and the verdict stay in the message, and
the file holds the evidence, the detail and the reasoning behind them.`

// The attribution law. It is provenance — who did the typing — rather than
// advertising, so it lives in exactly two places a reader already looks for
// provenance: the trailer block of a commit, and the last line of a pull
// request or issue body. Everywhere else it is noise on the user's own work,
// which is why the paragraph names the places it must never appear.
//
// The strings are constants because the exact bytes are the feature: a trailer
// with a different address does not attribute, and a footer with a dropped utm
// parameter cannot be counted. Tests pin them so a prompt edit cannot quietly
// reword one.

// AttributionTrailer is the commit trailer, and the only place aforge may sign
// a commit it wrote for the user.
const AttributionTrailer = "Co-Authored-By: aforge <agentfield-bot@users.noreply.github.com>"

// AttributionSeparator is the em-dash line that opens the body footer.
const AttributionSeparator = "—"

// AttributionPullFooter is the one footer line on a pull request aforge opens.
const AttributionPullFooter = "Drafted with [agentfield ai](https://agentfield.ai/github?utm_source=github&utm_medium=pull_request&utm_campaign=drafted_with) · reviewed and owned by the author"

// AttributionIssueFooter is the same line for an issue; only the medium differs.
const AttributionIssueFooter = "Drafted with [agentfield ai](https://agentfield.ai/github?utm_source=github&utm_medium=issue&utm_campaign=drafted_with) · reviewed and owned by the author"

// attributionPrompt is unconditional once the setting is on: the instruction
// carries its own condition, so no task-type detection has to guess whether a
// job will touch git.
const attributionPrompt = `

When you create git commits on the user's behalf, add this trailer at the end of
the commit message, in the trailer block after a blank line, and put nothing
about aforge in the subject or the body:

` + AttributionTrailer + `

When you open pull requests or issues, end the body with an em-dash separator
line and then one sentence — that is the whole footer, nothing else:

` + AttributionSeparator + `
` + AttributionPullFooter + `

On an issue the same line reads ` + "`utm_medium=issue`" + ` instead:

` + AttributionSeparator + `
` + AttributionIssueFooter + `

That is the entire extent of it. It never goes inside a code file, never in a
commit subject line, never in a README or any other document you write for the
user, and never in a deliverable such as a deck, a report, or a note. If the
repository says otherwise — a CONTRIBUTING file or a stated policy that forbids
AI trailers or generated-by lines — the repository wins: leave both out and say
so in your deliverable.`

const reflexSystemPrompt = `

This assignment is a reflex: one obvious, reversible action with a deliberately
small budget. Do the action directly and finish as soon as its result is known.
Do not widen it into research, a sequence of independent changes, or a project.
If inspection reveals that the request is ambiguous, needs several real steps,
or cannot be landed safely in this short run, stop and call promote with the
useful partial you have so the same request can continue as a normal job. A
correct promotion is better than stretching a reflex until its budget cuts it
off.`

// Linear is a single agent working in order: think, call tools, look, repeat.
type Linear struct {
	client    Completer
	workspace *Workspace
	web       *Web
	history   *store.Store
	media     *MediaTools
	maxTurns  int
	maxTokens int
	deadline  time.Duration
	// attribution carries the user's settings row into the standing contract.
	attribution bool
}

// WithStore enables the optional persistent-memory pull tool. It mutates the
// just-constructed loop for fluent wiring; callers that do not opt in retain
// the base-tool completion floor.
func (l *Linear) WithStore(history *store.Store) *Linear {
	l.history = history
	return l
}

// WithMedia installs graph-level image, music, video, speech, and image-inspection tools.
// It is executor configuration, so reflex micro-leaves inherit it unchanged.
func (l *Linear) WithMedia(media *MediaTools) *Linear {
	l.media = media
	return l
}

// WithAttribution admits the attribution law into the standing contract. Off is
// the absence of the paragraph rather than a paragraph saying not to: a worker
// told nothing about attribution does not attribute.
func (l *Linear) WithAttribution(on bool) *Linear {
	l.attribution = on
	return l
}

// Completer is the slice of the provider adapter this package needs.
type Completer interface {
	CompleteWithMessages(ctx context.Context, messages []ai.Message, options ...ai.Option) (*ai.Response, error)
}

// defaultLeafTokens is the budget one leaf may spend, and it is calibrated
// rather than picked.
//
// Measured: with observations spilled and decayed, a turn costs about 11k input
// tokens, and a well-sized leaf finishes in 8 to 16 turns. That is 90k to 175k,
// so the budget sits just above the top of the honest range.
//
// It was originally set at 400k, which turned out to permit around 37 turns —
// and every leaf ran to exactly that, because the loop has no intrinsic reason
// to stop. The lesson is worth writing down: the binding limit is not a safety
// net, it is what makes the loop converge. Set it loosely and the model will
// spend all of it.
const defaultLeafTokens = 150_000

// NewLinear builds the loop. maxTurns is a runaway backstop set far above real
// work; maxTokens is the limit that actually binds.
//
// Counting turns was the wrong meter. Turns are not what a loop spends — one
// 25-turn leaf cost more than the other nine nodes of a run put together,
// because cost tracks accumulated context rather than iteration count. Bounding
// tokens lets a task take all the small steps it needs while still stopping one
// that is genuinely expensive.
func NewLinear(client Completer, workspace *Workspace, web *Web, maxTurns, maxTokens int, deadline time.Duration) *Linear {
	if maxTurns <= 0 {
		maxTurns = 200
	}
	if maxTokens <= 0 {
		maxTokens = defaultLeafTokens
	}
	if deadline <= 0 {
		deadline = 15 * time.Minute
	}
	return &Linear{client: client, workspace: workspace, web: web,
		maxTurns: maxTurns, maxTokens: maxTokens, deadline: deadline}
}

func (l *Linear) Skill() string { return "linear" }

// system is the leaf's standing contract: the harness's invariants, then the
// laws the user has switched on, then the narrowing for this assignment, then
// the contract the planner wrote for this particular kind of job. Together they
// are what a specialised harness would have hand-written for this domain —
// generated instead, which is what keeps the loop generic.
func (l *Linear) system(task Task) string {
	system := systemPrompt
	if l.attribution {
		system += attributionPrompt
	}
	if task.Reflex {
		system += reflexSystemPrompt
	}
	if contract := strings.TrimSpace(task.Contract); contract != "" {
		system += "\n\nHow this particular kind of job is done well:\n" + contract
	}
	return system
}

// Run executes one task.
func (l *Linear) Run(ctx context.Context, task Task) (returned *Outcome, runErr error) {
	started := time.Now()
	ctx, cancel := context.WithTimeout(ctx, l.deadline)
	defer cancel()
	deadline, _ := ctx.Deadline()
	landingReserve := deadlineLandingReserve(time.Until(deadline))

	tools := newToolboxWithMedia(l.workspace, task.NodeID, l.web, l.history, l.media)
	task.control.attach(tools)
	defer func() {
		if returned != nil && runErr == nil {
			requests := tools.ServiceRequests(task.StoreNodeID)
			if task.StoreNodeID == "" {
				for index := range requests {
					requests[index].Stop()
				}
				if len(requests) > 0 {
					returned.Text = strings.TrimSpace(returned.Text) + "\n\nservice promotion is available only in resident chat; requested jobs stopped at leaf end"
				}
			} else {
				returned.ServiceRequests = requests
			}
		} else {
			tools.ForceClose()
		}
		terminated := tools.Close()
		if returned != nil && terminated > 0 {
			note := fmt.Sprintf("%d background jobs terminated at leaf end", terminated)
			if strings.TrimSpace(returned.Text) == "" {
				returned.Text = note
			} else {
				returned.Text = strings.TrimSpace(returned.Text) + "\n\n" + note
			}
			returned.Artifacts = l.workspace.Artifacts(task.NodeID)
		}
		task.control.detach(tools, terminated)
	}()
	definitions := tools.Definitions()
	if task.Reflex {
		definitions = append(definitions, reflexPromotionDefinition())
	}
	trace := newTracer(l.workspace, task.NodeID)
	defer trace.close()
	system := l.system(task)
	if contract := strings.TrimSpace(task.Contract); contract != "" {
		trace.note("contract:\n" + contract)
	}
	userContent := text(l.brief(task))
	workingModel := ""
	if l.media != nil {
		workingModel = l.media.WorkingModel
	}
	if len(task.ImagePaths) > 0 {
		sees := l.media != nil && l.media.Catalog != nil &&
			l.media.Catalog.Supports(workingModel, "input", "image")
		if sees {
			userContent = append(userContent, imageParts(task.ImagePaths)...)
		} else if note := attachedImageNote(l.workspaceNames(task.ImagePaths), l.visionProxy()); note != "" {
			// An attached image used to vanish here when the working model had
			// no eyes: no content part, no fallback, and nobody told. The image
			// is in the workspace now, so the leaf is told what it has and who
			// can look — and when nothing can, that it must say so.
			userContent = append(userContent, text(note)...)
		}
	}
	messages := []ai.Message{
		{Role: "system", Content: text(system)},
		{Role: "user", Content: userContent},
	}

	outcome := &Outcome{Stop: StopDone}
	// Identical repeated calls are the most common way a loop burns turns
	// without learning anything. Answering from the previous result turns that
	// spend into a nudge.
	seen := map[string]string{}
	// What each tool result was, so a faded one can still be recognised.
	labels := map[string]string{}
	// fade shortens old observations losslessly: before a result is stubbed
	// its bytes go to a spill file the agent can re-read with sh. The decayer
	// carries the once-per-result bookkeeping across turns.
	fade := newDecayer(labels, tools.decaySpill)
	warned := false
	// landing counts the reserved turns left after the node has been told to
	// finish; zero means no landing has begun yet.
	landing := 0
	landingStop := StopReason("")

	// The observation window scales with the task's budget rather than sitting
	// at a constant. The constant was tuned for the default budget, and a task
	// granted ten times the tokens was still forgetting at the small-task rate —
	// which for file-heavy work meant re-reading the same sources for the whole
	// run. One sixth of the budget in bytes reproduces the original tuning at
	// the default and grows with what the operator actually granted.
	obsBudget := l.maxTokens / 6
	if obsBudget < observationBudget {
		obsBudget = observationBudget
	}
	if obsBudget > maxObservationBudget {
		obsBudget = maxObservationBudget
	}

	for turn := 0; turn < l.maxTurns; turn++ {
		if task.Control != nil {
			switch task.Control() {
			case ControlCancel:
				outcome.Stop = StopCancelled
				trace.note("cancel requested — stopping at turn boundary")
				return l.land(ctx, task, outcome, started), nil
			case ControlPause:
				outcome.Stop = StopPaused
				trace.note("pause requested — holding at turn boundary")
				return l.land(ctx, task, outcome, started), nil
			}
		}
		if landing == 0 && time.Until(deadline) <= landingReserve {
			landing = landingTurns
			landingStop = StopDeadline
			// Recorded the moment the landing is ordered rather than when it
			// fails. The landing usually succeeds — that is what it is for — and
			// on that path Stop stays StopDone, so this is the only record that
			// the leaf was still working when the clock took it.
			outcome.Exhausted = StopDeadline
			trace.note("deadline close — landing reserve started")
			messages = append(messages, ai.Message{Role: "user", Content: text(
				"The wall-clock deadline for this task is close. Use the remaining time only to " +
					"land the work safely. In order: make whatever you were changing consistent " +
					"again; run the single quickest check that would catch breakage; fix only what " +
					"it reveals. Do not start anything new. Then give your final answer.")})
		}
		outcome.Steered += readSteering(task, &messages, trace)
		// Called every turn, but mutating on few of them: decay only fires once
		// the window crosses the budget, and then clears to a low-water mark so
		// the turns that follow can resend a byte-identical prefix and be billed
		// at the cached rate.
		outcome.Decayed += fade.decay(messages, obsBudget)
		// What the leaf had left before this turn, so the circuit breaker below
		// can weigh what the turn cost against what remained rather than against
		// the budget it started with.
		remaining := l.maxTokens - spent(outcome)
		response, err := l.complete(ctx, messages, definitions)
		if err != nil {
			outcome.Stop = StopError
			outcome.Text = strings.TrimSpace(lastAssistantText(messages))
			if ctx.Err() != nil {
				outcome.Stop = StopDeadline
			}
			return l.land(ctx, task, outcome, started), fmt.Errorf("node %d: %w", task.NodeID, err)
		}
		outcome.Turns++
		addUsage(&outcome.Usage, response)

		calls := response.ToolCalls()
		if task.Reflex {
			if partial, promote := reflexPromotion(calls); promote {
				outcome.Stop = StopPromote
				outcome.Promote = true
				outcome.Text = partial
				if outcome.Text == "" {
					outcome.Text = strings.TrimSpace(response.Text())
				}
				if outcome.Text == "" {
					outcome.Text = "The quick pass found that this needs a full job."
				}
				trace.turn(outcome.Turns, response, calls, nil, "promoted")
				return l.land(ctx, task, outcome, started), nil
			}
		}
		if len(calls) == 0 {
			outcome.Text = strings.TrimSpace(response.Text())
			// A turn that returns no visible text has either been cut off
			// mid-think or spent its whole pass on private deliberation.
			// Continuing is the right answer to the first and a trap for the
			// second: the probe lab watched a model burn an entire 16k budget on
			// reasoning and emit zero characters, four tasks running, which is
			// the most expensive way there is to fail — full price, nothing
			// delivered, and 15% of all failures. So a turn that eats most of
			// what the leaf has left and says nothing is not a hiccup, it is the
			// mode, and nudging the same model only buys it again. The leaf is
			// abandoned here rather than retried in place, so that whatever
			// routed it can send the work somewhere else.
			if outcome.Text == "" && remaining > 0 && completionOf(response) > remaining/2 {
				outcome.Stop = StopEmpty
				trace.turn(outcome.Turns, response, nil, nil, fmt.Sprintf(
					"empty reply burned %d of %d remaining tokens — abandoned for escalation",
					completionOf(response), remaining))
				return l.land(ctx, task, outcome, started), nil
			}
			// An empty message with no tool calls is not a deliverable — it is
			// what a reasoning model produces when the output ceiling cut it
			// off mid-think, or when a turn's whole budget went to private
			// deliberation. Accepting it ends the task with "produced no
			// result" after real work; the honest move is to say so and let
			// the loop continue.
			if outcome.Text == "" {
				messages = append(messages,
					ai.Message{Role: "assistant", Content: text("")},
					ai.Message{Role: "user", Content: text(
						"Your last reply was empty — it either hit the output limit or contained " +
							"only private reasoning. Continue the work with tool calls, or if the work " +
							"is finished, state the deliverable itself in the body of your reply.")})
				trace.turn(outcome.Turns, response, nil, nil, "empty reply — nudged to continue")
				continue
			}
			// A reply that ended at the output limit is not a deliverable
			// either — it is a runaway monologue cut mid-sentence. Left
			// unguarded, a model that starts drafting the whole work inside
			// its reply gets truncated, the truncation is accepted as final,
			// and the run reports done with nothing on disk.
			if finishOf(response) == "length" {
				messages = append(messages,
					ai.Message{Role: "assistant", Content: text(response.Text())},
					ai.Message{Role: "user", Content: text(
						"That reply hit the output limit and was cut off, so it cannot be the " +
							"result. Your reply is not the place to produce the work: do it with tool " +
							"calls — write files with the write tool, in several pieces if they are " +
							"large — and keep the final message short.")})
				trace.turn(outcome.Turns, response, nil, nil, "truncated reply — nudged to use tools")
				continue
			}
			// The mailbox, one last time, before the door closes.
			//
			// Steering is polled at the top of a turn, which quietly meant a
			// leaf could only hear the user during work it had not finished.
			// Words arriving while the final turn was in flight reached a
			// worker that had already written its answer and was one statement
			// away from handing it over — and the delivered thing was then the
			// thing the user had just said they did not want. That is the whole
			// of "commission a poem about mountains, say make it about the sea,
			// receive a poem about mountains": the redirect was journaled, the
			// mailbox got it, and the only reader had stopped reading.
			//
			// A leaf that has not landed has not delivered, so it looks once
			// more. Guidance found here reopens the loop rather than ending it:
			// the answer just written goes into the transcript as the draft it
			// now is, the user's words follow it, and the next turn produces the
			// deliverable they asked for. A leaf under a landing reserve is
			// exempt — it is out of clock or out of budget, and reopening work
			// there buys a truncated answer instead of a redirected one.
			if landing == 0 && turn+1 < l.maxTurns {
				draft := ai.Message{Role: "assistant", Content: text(response.Text())}
				messages = append(messages, draft)
				if steered := readSteering(task, &messages, trace); steered > 0 {
					outcome.Steered += steered
					trace.turn(outcome.Turns, response, nil, nil,
						"steered at the finish — the answer was reopened rather than delivered")
					continue
				}
				messages = messages[:len(messages)-1]
			}
			trace.turn(outcome.Turns, response, nil, nil, "final")
			return l.land(ctx, task, outcome, started), nil
		}

		messages = append(messages, ai.Message{
			Role:      "assistant",
			Content:   text(response.Text()),
			ToolCalls: calls,
		})

		// A turn may carry several calls. They are independent by definition —
		// the model asked for them together — so running them concurrently is a
		// free wall-clock win, and the results go back in the order requested.
		results := make([]Result, len(calls))
		keys := make([]string, len(calls))
		var group sync.WaitGroup
		for index, call := range calls {
			outcome.ToolCalls++
			key := fingerprint(call)
			if previous, repeated := seen[key]; repeated && call.Function.Name != "view_image" {
				results[index] = tools.finishResult(Result{Content: previous + "\n\n(identical call already made; this is the same result. If you were re-checking, nothing has changed — move on to the next step)"})
				continue
			}
			keys[index] = key
			labels[call.ID] = callLabel(call)
			group.Add(1)
			go func(index int, call ai.ToolCall) {
				defer group.Done()
				// Execute answers a fault with an error result of its own; this is
				// the belt for anything that could fault outside it.
				defer func() {
					if recovered := recover(); recovered != nil {
						_ = guard.Note("exec/linear tool "+call.Function.Name, recovered)
						results[index] = errorf("internal fault in this tool call — recorded to the log. Try a different approach.")
					}
				}()
				results[index] = tools.Execute(ctx, call.Function.Name, call.Function.Arguments)
			}(index, call)
		}
		group.Wait()
		for index := range results {
			outcome.Usage.merge(results[index].Usage)
		}
		// The run record is written here rather than in the workers, for the
		// same reason the cache below is: it is one slice and several
		// goroutines just finished. It records every call the model asked for,
		// including one answered from the cache — the model asked, and a
		// reader checking whether a check was ever run needs the ask.
		for index, call := range calls {
			outcome.record(call, results[index].IsError)
		}

		// The cache is filled here, on this goroutine, and never inside the
		// workers. Writing a shared map from several tool goroutines at once is
		// a hard panic in Go, and it would only ever fire on the turns where the
		// model asked for several tools — the exact case this loop exists for.
		for index, key := range keys {
			if key != "" && !results[index].IsError && !results[index].reportedJobs {
				seen[key] = results[index].Content
			}
		}
		// Any mutation invalidates the whole cache. A memoised read served after
		// an edit is the pre-edit file presented as current — an answer that is
		// confidently wrong, which is worse than paying to run the call again.
		// sh is included because a shell command can change anything.
		for index, call := range calls {
			name := call.Function.Name
			if !results[index].IsError && (name == "write" || name == "edit" || name == "sh" || name == "generate_image" || name == "generate_music" || name == "generate_video" || name == "speak") {
				clear(seen)
				break
			}
		}

		trace.turn(outcome.Turns, response, calls, results, "")

		for index, call := range calls {
			body := results[index].Content
			if results[index].IsError {
				body = "ERROR: " + body
			}
			messages = append(messages, ai.Message{
				Role:       "tool",
				ToolCallID: call.ID,
				Content:    text(body),
			})
		}
		for _, result := range results {
			if len(result.Followup) == 0 || result.IsError {
				continue
			}
			messages = append(messages, ai.Message{Role: "user", Content: result.Followup})
		}

		// The budget ends work in two stages, and the staging is what protects
		// the workspace. A hard stop at the limit truncated runs mid-edit —
		// twice it left files syntactically broken with the model's final,
		// already-emitted repairs discarded unexecuted. So the emitted calls of
		// every turn always execute (they are paid for), and exhaustion buys a
		// short landing instead of a guillotine: a few reserved turns whose only
		// job is to leave the workspace consistent, checked, and answered.
		if spent(outcome) >= l.maxTokens && landing == 0 {
			landing = landingTurns
			landingStop = StopBudget
			// Same reason as the deadline reserve above: the budget is spent
			// here, whether or not the landing later has to be cut short.
			outcome.Exhausted = StopBudget
			trace.note("budget exhausted — landing reserve granted")
			messages = append(messages, ai.Message{Role: "user", Content: text(
				"The budget for this task is spent. You have a few final tool calls to land the " +
					"work safely, and nothing more. In order: make whatever you were changing " +
					"consistent again; run the single quickest check that would catch breakage; fix " +
					"only what it reveals. Do not start anything new. Then give your final answer.")})
			continue
		}
		if landing > 0 {
			if landing == 1 {
				outcome.Stop = landingStop
				outcome.Text = strings.TrimSpace(lastAssistantText(messages))
				return l.land(ctx, task, outcome, started), nil
			}
			landing--
			continue
		}

		// Tell it what it has left.
		//
		// Every leaf spent its entire budget, whatever the budget was: at 400k
		// they ran 38 turns, at 150k they ran 20. Halving the limit halved the
		// work and changed nothing else, which says the loop has no intrinsic
		// stopping point — an agent that cannot see a budget cannot plan to
		// finish inside one, so it explores until something cuts it off.
		//
		// Naming the remaining budget once, late, turns a hard stop into a
		// deadline the model can actually work towards. It is announced a single
		// time rather than every turn: repeating it would cost tokens on exactly
		// the turns that have none to spare.
		if !warned && float64(spent(outcome))/float64(l.maxTokens) > wrapUpAt {
			warned = true
			messages = append(messages, ai.Message{Role: "user", Content: text(
				"You have used most of the budget for this task. If the deliverable is not yet " +
					"produced and connected into place, start that now and leave room for one " +
					"verification pass at the end — work you have not done yet will not happen. " +
					"Stop exploring; nothing you have already confirmed needs another look.")})
		}
	}

	// The cap is a backstop, not a budget. A leaf sized for one agent should
	// finish in well under it, so reaching it is evidence the sizing anchors put
	// too much into one node — which is worth reporting rather than hiding.
	outcome.Stop = StopTurnCap
	outcome.Text = strings.TrimSpace(lastAssistantText(messages))
	return l.land(ctx, task, outcome, started), nil
}

// readSteering drains the mailbox into the transcript and reports how many of
// the user's lines landed there. It is one function rather than two copies
// because it is called at both ends of a turn — before the model speaks and
// before its answer is accepted — and the two must deliver identically.
func readSteering(task Task, messages *[]ai.Message, trace *tracer) int {
	if task.Steer == nil {
		return 0
	}
	delivered := 0
	for _, guidance := range task.Steer() {
		guidance = strings.TrimSpace(guidance)
		if guidance == "" {
			continue
		}
		trace.note("steered: " + guidance)
		*messages = append(*messages, ai.Message{Role: "user", Content: text(
			"Guidance from the user, mid-task — adjust course without discarding sound work already done:\n" + guidance)})
		delivered++
	}
	return delivered
}

// land finishes a leaf. It collects what the leaf left behind, decides the
// verdict, and tells whatever routed the leaf how it went — in one place,
// because there are five ways out of the loop above and a verdict that is set on
// four of them is worse than none at all.
func (l *Linear) land(ctx context.Context, task Task, outcome *Outcome, started time.Time) *Outcome {
	outcome.Artifacts = l.workspace.Artifacts(task.NodeID)
	outcome.Elapsed = time.Since(started)
	outcome.Verdict = verdictFor(outcome)
	provider.Report(ctx, outcome.Verdict)
	return outcome
}

// verdictFor reads the leaf's own accounting.
//
// A leaf that stopped under its own power is an *unverified* success, never a
// verified one: this is the general loop, and the general loop has no test
// suite it can assume. That is the honest reading and it is also the one the
// probe lab argues for — where you cannot check an outcome, do not claim to
// have. A specialised executor that does own a verifier can say more, by
// setting the verdict itself before landing.
//
// A timeout is a provider fact rather than an ability one, so a deadline stop
// grades nothing. Everything else is a way of not finishing inside what the
// leaf was given, and that is precisely what a rating measures.
func verdictFor(outcome *Outcome) provider.Verdict {
	if outcome.Verdict != "" {
		return outcome.Verdict
	}
	switch outcome.Stop {
	case StopBudget:
		return provider.VerdictBudgetStop
	case StopTurnCap:
		return provider.VerdictTurnCap
	case StopPromote:
		return provider.VerdictUnverifiedSuccess
	case StopPaused, StopCancelled:
		// User-directed stops say nothing about model capability.
		return provider.VerdictUnverifiedSuccess
	case StopEmpty:
		return provider.VerdictEmptyResponse
	case StopError, StopDeadline:
		return provider.VerdictProviderFailure
	}
	// A landing the leaf was ordered into is not the ending it chose. Stop says
	// it finished cleanly, which is true — it complied with the order — but the
	// work was not finished when the order came, and that is precisely what a
	// rating measures. Only the budget grades: a deadline is a fact about the
	// clock rather than about ability, exactly as the StopDeadline arm above.
	if outcome.Exhausted == StopBudget {
		return provider.VerdictBudgetStop
	}
	if strings.TrimSpace(outcome.Text) == "" {
		return provider.VerdictEmptyResponse
	}
	return provider.VerdictUnverifiedSuccess
}

const (
	nodeCallAttempts = 3
	nodeCallBackoff  = 500 * time.Millisecond
)

// complete absorbs failures that escape the provider's transport retries. It
// stops immediately when the node context is done because no later attempt can
// outlive that decision.
func (l *Linear) complete(ctx context.Context, messages []ai.Message, definitions []ai.ToolDefinition) (*ai.Response, error) {
	var lastErr error
	for attempt := 0; attempt < nodeCallAttempts; attempt++ {
		response, err := l.client.CompleteWithMessages(ctx, messages, ai.WithTools(definitions))
		if err == nil {
			return response, nil
		}
		lastErr = err
		if ctx.Err() != nil {
			return nil, err
		}
		if attempt == nodeCallAttempts-1 {
			break
		}
		if err := backoffWait(ctx, nodeCallBackoff*time.Duration(1<<attempt)); err != nil {
			return nil, err
		}
	}
	return nil, fmt.Errorf("after %d node call attempts: %w", nodeCallAttempts, lastErr)
}

// backoffWait is the retry pause, with its timer stopped on the way out. A
// time.After inside a select leaves the timer armed for the whole delay when
// the other case wins, and the other case here is cancellation — which is
// exactly when the run is trying to let go of things.
func backoffWait(ctx context.Context, delay time.Duration) error {
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

// brief assembles what the agent sees. The order matters: the goal orients it,
// the inputs are the only upstream work it is allowed to know about, and its own
// instruction comes last so it is the freshest thing in the prompt.
func (l *Linear) brief(task Task) string {
	var block strings.Builder
	if task.Goal != "" {
		fmt.Fprintf(&block, "This work is part of a larger goal:\n%s\n\n", task.Goal)
	}
	if len(task.Inputs) > 0 {
		block.WriteString("Results from earlier work, which you already have and must not gather again:\n")
		for _, input := range task.Inputs {
			fmt.Fprintf(&block, "\n=== from %q ===\n%s\n", input.Title, input.Result)
			if len(input.Artifacts) > 0 {
				fmt.Fprintf(&block, "(files: %s — read them if you need the full detail)\n", strings.Join(input.Artifacts, ", "))
			}
		}
		block.WriteString("\n")
	}
	block.WriteString("Your work:\n")
	block.WriteString(task.Brief)
	if task.OutputHint != "" {
		fmt.Fprintf(&block, "\n\nIf your instructions already say where the deliverable goes, that wins. "+
			"Otherwise: a standalone document goes to %s, and work that belongs inside existing "+
			"material goes there — never into a separate file.", task.OutputHint)
	}
	return block.String()
}

// wrapUpAt is how much of the budget may be spent before the model is told to
// land the work.
const wrapUpAt = 0.7

// landingTurns is the reserve granted after the budget runs out: enough calls
// to restore consistency, run one check, and repair one breakage — never
// enough to keep working. The reserve is what stands between "budget reached"
// and "workspace left broken mid-edit".
const landingTurns = 4

// deadlineLandingReserve leaves enough of a node's own deadline for a bounded
// landing without taking more than two minutes away from long-running work.
func deadlineLandingReserve(deadline time.Duration) time.Duration {
	reserve := deadline / 10
	if reserve > 2*time.Minute {
		return 2 * time.Minute
	}
	return reserve
}

func spent(outcome *Outcome) int {
	return outcome.Usage.PromptTokens + outcome.Usage.CompletionTokens
}

func fingerprint(call ai.ToolCall) string {
	sum := sha256.Sum256([]byte(call.Function.Name + "\x00" + call.Function.Arguments))
	return hex.EncodeToString(sum[:12])
}

func completionOf(response *ai.Response) int {
	if response == nil || response.Usage == nil {
		return 0
	}
	return response.Usage.CompletionTokens
}

func finishOf(response *ai.Response) string {
	if response == nil || len(response.Choices) == 0 {
		return ""
	}
	return response.Choices[0].FinishReason
}

func lastAssistantText(messages []ai.Message) string {
	for index := len(messages) - 1; index >= 0; index-- {
		if messages[index].Role == "assistant" {
			var parts []string
			for _, part := range messages[index].Content {
				if part.Text != "" {
					parts = append(parts, part.Text)
				}
			}
			if joined := strings.TrimSpace(strings.Join(parts, "\n")); joined != "" {
				return joined
			}
		}
	}
	return ""
}

func text(body string) []ai.ContentPart {
	return []ai.ContentPart{{Type: "text", Text: body}}
}

func addUsage(usage *Usage, response *ai.Response) {
	usage.Calls++
	if response == nil || response.Usage == nil {
		return
	}
	usage.PromptTokens += response.Usage.PromptTokens
	usage.CompletionTokens += response.Usage.CompletionTokens
	if details := response.Usage.PromptTokensDetails; details != nil {
		usage.CachedTokens += details.CachedTokens
	}
	if response.Usage.Cost != nil {
		usage.Cost += *response.Usage.Cost
	}
}
