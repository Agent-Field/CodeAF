package exec

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/Agent-Field/aforge-v2/internal/guard"
	"github.com/Agent-Field/aforge-v2/internal/orientation"
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
//
// The checking paragraph is the same lesson arriving from the other side. The
// prompt asks, in three separate places, for the work to be checked — a rule
// re-read, a suite run, a "verified" that costs evidence — and never once said
// where the checking goes. A validation battery read the answer: leaves opened
// on "487 words — within reasonable tolerance of 500", on "Files n3, n7, n8 are
// empty", on "I now have comprehensive information. Let me compile the
// deliverable". Every one of those is a leaf doing exactly what it was told and
// then handing over the wrong half of it. So the working is given a destination
// — the record, which already exists — and the three things that legitimately
// cross from the checking into the delivery are named, so the paragraph reads
// as a routing rule rather than as one more thing not to do.
//
// Two things it says in its own words on purpose. The opening states the worker
// premise in the second person, to the worker — plan.workerPremise is the shared
// fact, stated in the third person for the prompts that write *about* this
// agent, and a system prompt that described its own reader from outside would be
// the wrong voice for the one place the reader is present.
//
// The closing paragraphs are this package's rendering of the delivery law, whose
// single statement is plan.DeliverInMessage. The wording differs and must not
// contradict: the law says the split is between the answer and its working,
// never between the answer and a pointer to the answer, and the budget below is
// that same split with a number on the near side of it. The other half of the
// law — plan.DeliverToNamedFile, for an ask that named its own file — is
// rendered by outputClause, which is the one place here that knows the shape of
// the ask.
//
// The produced-thing paragraph is the third clause the two documents share, and
// it is here because without it the budget and the law were flatly
// irreconcilable on a real run: the law demanded the whole finished thing in the
// final message, this prompt capped that message at about three hundred words,
// and a worker holding a 288-line script it had built and run stalled three
// times attempting both, then transcribed the script and never mentioned the
// output it had rendered. What was missing from both texts was that an answer is
// not always made of sentences. It is stated unconditionally, as something the
// worker judges about its own result, rather than switched on when a run has
// artifacts — the system message is the shared warm prefix, and a per-run fact
// in it is a per-run prefix. See plan.DeliverInMessage.
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

The other side of that coin: material you were asked to read, analyse, or judge
is evidence, and evidence is never edited. Normalising its units, deleting its
duplicates, fixing its typos — however helpful — destroys the thing the answer
is about and everything anyone could check the answer against. Work on a copy,
or carry the correction in your arithmetic and your words. Only what the ask
itself asks you to change is yours to change.

Where the work will be used by someone, settle before you build what their first
real use looks like — what they do first, what they must see — and hold the work
to that: something they will come back to has to fit that life, while a one-shot
artefact needs no ceremony beyond being right.

A rule you restated is a rule you apply. When the material itself carries an
instruction that changes the numbers — a unit, a rate, an exclusion, an
adjustment — the work shows it applied, in the arithmetic and not only in the
prose. A total computed in the same breath as quoting an unapplied rule is the
exact shape of wrong, and it is caught by one look: before finishing, read your
own answer against every rule you noted along the way and ask whether each one
visibly happened.

Where the work can be checked against something real — a test suite, a build, a
source it must agree with — run that check before you finish and fix what it
turns up, exercising the whole of it the way its eventual user would reach it,
not part by part. Done means shown to work, not believed to. A check that came
back green is a finished question. Asking it again spends the person's money to
re-learn a fact you already have, and nothing you learn the second time was
worth the first. Once a check passes, move on.

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

Your own checking is working, and working is for the record rather than for the
delivery. The count you measured against a target, the rule you went back and
re-read, the input that came back empty, the tolerance you decided was close
enough — that is how the answer was reached, and it is already kept in the trace
of what you did. Three things survive into the message and nothing else does:
where a check changed the answer, the changed answer is what you hand over;
where it left a real gap, one sentence names the gap; where it is the evidence
for a claim you are making, it goes beside that claim. A verification narrated
before the deliverable is not proof that the deliverable is good — it is the
deliverable arriving second.

If you wrote files, say which and what is in them. Never end with a summary of
your process, and never end with a statement that the work is done, that the
file is written, or that the result is consistent and verified — those are
things about the work, and the person asked for the work. If they asked a
question, the answer is in this message; if they asked for a judgement, the
verdict is in this message, in so many words.

Some answers are not made of sentences. Where you produced a thing whose form is
a file — something that had to be built, rendered, compiled or run to exist, and
which a message could only transcribe rather than contain — that produced thing
is the answer. Deliver it by naming it, saying what it is and what it does, and
giving its substance: what you ran it against, what came back, what it shows,
and what the person should conclude. Do not retype it into the message; that is
not delivery either, and you are never asked to choose between finishing the
work and transcribing it. Naming a file is a pointer only when the answer was
words and you filed them instead of saying them.

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
user, never in a deliverable such as a deck, a report, or a note, and never in
your own reply — the message you write back when the work is done is not an
issue and carries no footer. If the
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
	// contextTokens is how much the working model can hold in one request, and
	// it is the only honest input to the observation window. Zero means nobody
	// could say; see observationWindow, which has a default for exactly that.
	contextTokens int
	// swarm arms the cooperative division tool. Off — the default, and the
	// whole product until somebody sets AFORGE_SWARM — the leaf does not have
	// the verb, which is this codebase's rule for a capability with no path
	// behind it: absent, never present and refused.
	swarm bool
}

// WithStore enables the optional persistent-memory pull tool, and it is also
// where this loop's readings of the project's own checks are journaled. It
// mutates the just-constructed loop for fluent wiring; callers that do not opt
// in retain the base-tool completion floor, and their readings are still taken
// and still weighed, with nowhere to write the row down.
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

// WithContextLength tells the loop how much its model can actually hold, in
// tokens, so the observation window can be sized from it.
//
// It is a separate setter rather than a constructor argument because the answer
// comes from the provider's catalog, which the surface owns and this package
// deliberately does not: exec is handed facts about the model, never a client
// it has to interrogate. An unknown or unavailable model is zero, which is not
// an error — the window has a default for it, and a leaf must never fail to run
// because a metadata endpoint was down.
func (l *Linear) WithContextLength(tokens int) *Linear {
	if tokens > 0 {
		l.contextTokens = tokens
	}
	return l
}

// ContextLength answers what this loop's model holds, for the callers that must
// size something for it before it runs — the scheduler bounding an upstream
// result on its way in, principally. Zero is the honest unknown, exactly as
// WithContextLength leaves it, and every reader has a named fallback for that.
func (l *Linear) ContextLength() int { return l.contextTokens }

// WithAttribution admits the attribution law into the standing contract. Off is
// the absence of the paragraph rather than a paragraph saying not to: a worker
// told nothing about attribution does not attribute.
func (l *Linear) WithAttribution(on bool) *Linear {
	l.attribution = on
	return l
}

// WithSwarm arms the cooperative division tool for this loop.
//
// It is a setter carrying a settings row, exactly as WithAttribution is, and
// for the same reason: the surface owns config and this package is handed
// facts. Off is the absence of request_split from the schema rather than a
// paragraph saying not to divide — a worker that has never been told it can
// hand work back does not hand work back, and the belt is where that is said.
func (l *Linear) WithSwarm(on bool) *Linear {
	l.swarm = on
	return l
}

// Completer is the slice of the provider adapter this package needs.
type Completer interface {
	CompleteWithMessages(ctx context.Context, messages []ai.Message, options ...ai.Option) (*ai.Response, error)
}

// defaultLeafTokens is the budget one leaf may spend, and it is calibrated
// rather than picked.
//
// WHAT IT COUNTS IS WHAT THE JOB PAYS. spent() reads uncached prompt at full
// rate, cache reads at cachedTokenWeightPercent — the provider's own discount,
// not a weight this file invented — and the completion. So the grant is a bill
// expressed in token-equivalents, and a leaf that spends it has cost the job
// what the job agreed to spend on one leaf, whether it did that in twelve
// expensive turns or sixty cheap ones. That is deliberate: the alternative is a
// meter that charges a leaf for re-reading its own transcript, which is a fact
// about how conversations work rather than about what this leaf did.
//
// AND IT IS NOW THE ONLY BOUND THAT MAY LAND A LEAF ON ACCOUNT OF ITS WORK.
// Two cumulative prompt ceilings used to be able to as well; both were Σ over
// turns of the prompt, which is turns × mean-context wearing a token name, and
// on ink s9 of 2026-08-29 one of them cut three leaves at turns 13, 12 and 9
// with a third of this grant unspent. They are wrap-up pressure now. The other
// two things that may still stop a leaf measure something else entirely —
// maxTurnBackstop counts iterations, and the no-progress guard reads whether it
// is working at all. See PERF.md, "A leaf's bounds", and meter.go's
// reuseCeiling for the arithmetic.
//
// Measured: with observations spilled and decayed, a turn costs about 11k input
// tokens, and a well-sized leaf finishes in 8 to 16 turns. That is 90k to 175k,
// so the budget sits just above the top of the honest range. Those figures are
// RAW input; against the discount above, a leaf whose prefix caches well buys
// proportionally more turns for the same grant, which is the discount doing its
// job rather than the grant losing its meaning.
//
// It was originally set at 400k, which turned out to permit around 37 turns —
// and every leaf ran to exactly that, because the loop has no intrinsic reason
// to stop. The lesson is worth writing down: the binding limit is not a safety
// net, it is what makes the loop converge. Set it loosely and the model will
// spend all of it.
const defaultLeafTokens = 150_000

// rawTokenCeilingMultiple WAS the leaf's second bound and is now pressure on
// the wrap-up warning, for the reason set out at length against reuseCeiling in
// meter.go: Σ over turns of the prompt is turns × mean-context wearing a token
// name, and bounding it lands honest leaves early without reaching the runaway
// any sooner than the guard built to recognise one.
//
// The specific case below is answered instead by maxTurnBackstop and the
// no-progress guard. A 98%-cached loop that buys a million raw tokens with a
// 150k grant has, by construction, spent the grant — the provider charged for
// it, which is what the grant means — and what was actually wrong with the
// audited nodes was that they ran eighty turns without progressing, which is
// the thing noprogress.go measures and this never could.
//
// What follows is the reasoning it was introduced with, kept for the same
// reason meter.go keeps its own.
//
// Cost bounds spend; raw bounded convergence.
//
// spent() weights cache reads at cachedTokenWeightPercent, and that is the
// honest measure of what a leaf COSTS. It is not a measure of how far a leaf has
// got. The two came apart the moment the discount landed: at the 92-98% hit
// rates a warm run actually sees, a 150k discounted ceiling buys roughly a
// million raw prompt tokens, about eight times the allowance the number was
// calibrated for. The audited runs show exactly what a loop does with that —
// single nodes at 1.4M and 1.6M prompt tokens with no per-turn prompt above
// ~17k, i.e. nodes that melted by running very many cheap turns rather than by
// carrying a large context. The discount was right and the number beside it
// quietly stopped meaning anything, which is the failure defaultLeafTokens'
// own comment predicts: set it loosely and the model will spend all of it.
//
// So the raw count comes back as a bound of its own, at three times the grant.
// That still hands a genuinely cache-friendly leaf the extra room the discount
// was meant to buy it, and it lands a 98%-cached runaway at ~450k raw tokens
// instead of ~1.2M. It is a multiple rather than an absolute because the grant
// is per-task, and because recalibrating defaultLeafTokens downward instead
// would have to be redone per endpoint — the effective multiplier is the
// provider's hit rate, which is not ours to set.
const rawTokenCeilingMultiple = 3

// maxTurnBackstop is the ceiling on iterations. It is a ceiling rather than a
// default: whatever a caller asks for, this loop will not run more than this.
//
// It stood at 200, chosen when turns were understood to be the wrong meter and
// the token ceiling was believed to be the thing that bound. That reasoning held
// only while the token ceiling counted raw tokens; once cache reads were
// discounted, a warm runaway could afford far more turns than any real leaf
// needs, and 200 was the only thing left in its path — far too high to be in
// anyone's path. The measured distribution is the calibration: a well-sized leaf
// finishes in 8 to 16 turns (see defaultLeafTokens), the profile's own spread
// reports identical briefs landing at 9, 16 and 25, and the longest honest leaf
// in the audit traces ran 25. Forty sits clear of every one of those.
//
// It stays a backstop. On a cache-discounted runaway the raw bound above is what
// fires first, and it fires into a landing rather than into a stop.
//
// Raised 40 → 400 (2026-08-12, product decision): forty was calibrated on the
// measured distribution of well-sized leaves, but it also stopped honest
// complex work — and stopping a paid-for leaf mid-thought is worse than
// letting the token budget, which is the meter that actually binds, do its
// job. Four hundred is not a target; it is the point past which a loop is
// running on rails no token bound can see, which is the only case a turn
// count was ever for.
const maxTurnBackstop = 400

// ModeFold is what Outcome.Mode says when the loop ran a task as a fold.
const ModeFold = "fold"

// FoldTurns is the whole run of a node whose material is already in its prompt.
//
// Two, and the second is a recovery rather than a continuation. A fold is a
// read-and-write over material it was handed, so one call is the honest shape of
// it: the measured alternative is a join that read three files it had already
// been given and spent thirteen turns and 181,354 tokens doing it, against about
// 7,700 for the single pass — 23×, and 160 of the 366 seconds of that job's
// critical path.
//
// One turn alone would be a trap, though, and the trap is not the model's fault.
// A first call can come back asking for a tool — to write the file it was told
// to produce, most often — and a first call can come back empty or truncated,
// which the loop already refuses to accept as a deliverable. Either is a turn
// spent without an answer, and a cap of one would settle the node on it. So the
// second call exists for exactly those two endings, and after it the loop stops:
// a fold still asking for tools on its third breath is not folding, and what
// happens to it is what happens to any leaf that reaches its cap, which the
// retry and judging paths above this loop already know how to handle.
const FoldTurns = 2

// NewLinear builds the loop. maxTokens is the limit that actually binds; maxTurns
// is the runaway backstop, clamped to maxTurnBackstop however high a caller asks.
//
// Counting turns was the wrong meter. Turns are not what a loop spends — one
// 25-turn leaf cost more than the other nine nodes of a run put together,
// because cost tracks accumulated context rather than iteration count. Bounding
// tokens lets a task take all the small steps it needs while still stopping one
// that is genuinely expensive. What the turn cap is for is the case tokens
// cannot see: a loop whose turns have become cheap enough that no token bound
// arrives in useful time.
func NewLinear(client Completer, workspace *Workspace, web *Web, maxTurns, maxTokens int, deadline time.Duration) *Linear {
	if maxTurns <= 0 || maxTurns > maxTurnBackstop {
		maxTurns = maxTurnBackstop
	}
	if maxTokens <= 0 {
		maxTokens = defaultLeafTokens
	}
	if deadline <= 0 {
		// A caller that said nothing about time gets the generalist's own
		// floor, asked for rather than written out again: this loop IS the
		// generalist, so the shape registered under its name is the answer.
		deadline = linearInfo.Deadline(0)
	}
	return &Linear{client: client, workspace: workspace, web: web,
		maxTurns: maxTurns, maxTokens: maxTokens, deadline: deadline}
}

func (l *Linear) Subharness() string { return LinearSubharness }

// system is the leaf's standing contract: the harness's invariants, then the
// laws the user has switched on, then the narrowing for this assignment. It is
// what a specialised harness would have hand-written for this domain —
// generated instead, which is what keeps the loop generic.
//
// What it deliberately no longer carries is the planner's per-node contract.
//
// The system message is the first thing in every request, ahead of the tool
// block and the entire transcript, so it is the prefix every other leaf's
// prefix has to agree with to be served warm. Appending a contract written for
// one node made it the one part of the standing text that differed per node:
// four leaves of the same job, launched at once against the same model, shared
// nothing at all — each wrote the whole shared prefix cold and paid full price
// for the invariants all four of them were reading verbatim. The contract moved
// to the head of the brief, which is the first place two leaves were always
// going to diverge anyway, and the three inputs left here are the model, the
// operator's attribution setting and whether this is a reflex — a handful of
// shapes across a whole run instead of one per node.
// The last block is assembled rather than written: each tool the leaf is
// actually holding contributes its own standing guidance, and a leaf holding
// two tools reads two lines. See Toolbox.Guidelines for why the guidance
// belongs to the tool. It goes LAST because it is the one part that varies with
// the toolbox, and everything ahead of it stays the byte-identical prefix four
// sibling leaves share.
func (l *Linear) system(task Task, guidelines []string) string {
	system := systemPrompt
	if l.attribution {
		system += attributionPrompt
	}
	if task.Reflex {
		system += reflexSystemPrompt
	}
	if len(guidelines) > 0 {
		system += "\n\nYour tools, in the words of the tools themselves:\n\n" +
			strings.Join(guidelines, "\n\n")
	}
	return system
}

// lengthStopBatchFailure is what every call in a length-stopped batch is
// answered with. It is stated as a fact plus the one move that fixes it: the
// arguments may be truncated, nothing ran, ask again — and, because the reply
// hit a ceiling, ask for less at a time. It goes back verbatim for every call in
// the batch rather than being tailored per tool, so a model reading four of them
// sees one thing that happened rather than four different problems.
//
// It is not routed through the observation memo — these bytes are identical
// across the batch by design, and collapsing them into "same as the result
// above" would turn the one sentence that has to be read into a pointer.
const lengthStopBatchFailure = "This tool call was NOT executed. The reply carrying it hit the output token " +
	"limit, so its arguments may be truncated mid-write and running them could do something " +
	"other than what you intended. Nothing was changed. Re-issue the call with complete " +
	"arguments — and issue fewer or smaller calls this turn, since the last reply did not fit."

// Run executes one task.
func (l *Linear) Run(ctx context.Context, task Task) (returned *Outcome, runErr error) {
	started := time.Now()
	// One leaf is one routing lineage. The run key inherited from above already
	// says "keep this run together"; this narrows it to "keep THIS leaf's turns
	// together", which is the grain an automatic prefix cache can actually hit
	// at — see provider.WithLeafCacheKey for what the narrowing gives up and why
	// it is not close. It is applied before the deadline so that every call the
	// leaf makes, including the tool-side model calls, rides the same key.
	ctx = provider.WithLeafCacheKey(ctx, task.leafKey())
	// The project's own account of whether it still works, read while the tree
	// is still pristine. THIS BELT IS THE DEFAULT EVERY UNROUTED NODE GETS, and
	// until it took a reading a whole graded run could finish with no
	// verification event in its store at all — an absence that spells four
	// different facts at once and diagnoses none of them (FAILSAFE.md, the sixth
	// failure). The reading is an account of the run and never part of it: it
	// cannot fail this leaf, and every way it can go wrong is written down
	// rather than returned.
	//
	// It is taken BEFORE the tree is photographed on purpose. A test runner
	// leaves its own droppings — a .pytest_cache, a target/, a coverage file —
	// and a reading taken after WatchTree would file every one of them as
	// something this leaf produced. Taken first, they belong to the world the
	// leaf arrived in, which is what they are.
	opening := PhotographBefore(ctx, l.workspace, l.history, l.deadline, task)
	// The world's own account of what this leaf leaves behind starts here: the
	// tree as it stands before a single turn has run. Everything the leaf writes
	// with a shell command, a script or a build is invisible to the write tools
	// and visible to this. See Workspace.WatchTree.
	l.workspace.WatchTree(task.leafKey())

	// WHICH NODE THESE CALLS BELONG TO, on the same context and for the same
	// reason: every model call this leaf makes, turn after turn, is one node's
	// work, and a log of a fanned-out run is unreadable without saying whose.
	// The tag itself is derived from the routing class the scheduler already
	// stamped (provider.WithCallTag), so "leaf" is spelled once, not twice.
	//
	// It is leafKey rather than NodeKey because NodeKey is empty on every
	// headless plan leaf — that path carries its plan node's number in NodeID
	// and sets no key at all — and naming the node from the field that is only
	// sometimes filled left exactly those rows anonymous. leafKey is already
	// the one place "who is this leaf, for naming purposes?" is answered, so
	// the log now agrees with the artifact bucket and the flight recorder.
	ctx = provider.WithCallTag(ctx, "leaf")
	ctx = provider.WithCallNode(ctx, task.leafKey())
	// Keep the context this leaf was HANDED before its own lease is put around
	// it. An ordered landing must survive that lease expiring, while the caller's
	// cancel and the errand's own wall must still reach it; deriving the landing
	// from this context preserves exactly those two properties.
	granted := ctx
	ctx, cancel := context.WithTimeout(ctx, l.deadline)
	defer cancel()
	deadline, _ := ctx.Deadline()
	wallStarted := time.Now()
	wall := deadline.Sub(wallStarted)
	landingReserve := deadlineLandingReserve(wall)
	wallPaceAfter := time.Duration(float64(wall) * wallPaceAt)
	turnCtx := ctx
	var stopDeadlineLanding context.CancelFunc
	defer func() {
		if stopDeadlineLanding != nil {
			stopDeadlineLanding()
		}
	}()

	tools := newToolbox(l.workspace, task.leafKey(), l.web, l.history, l.media, l.contextTokens)
	tools.share = task.Share
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
			// A background job that was still running at landing may have
			// written after the account was taken, so the tree is read once more
			// before the list is rebuilt.
			l.workspace.RecordChanges(task.leafKey())
			returned.Artifacts = l.workspace.Artifacts(task.leafKey())
		}
		task.control.detach(tools, terminated)
	}()
	// What the assignment structurally already contains buys back its own
	// schema before turn 1: a leaf handed an image must be able to look at it,
	// and a leaf handed a PDF must be able to read it, without spending a turn
	// asking. This is code judging structure — the presence of a file — and
	// never code judging what the work is about.
	if len(task.ImagePaths) > 0 {
		tools.Arm("view_image")
	}
	if len(task.DocumentPaths) > 0 {
		tools.Arm("read_document")
	}
	// Recomputed every turn rather than once, because a worker that asks for a
	// capability has to be holding it on the turn after it asked. The cost is
	// one slice build per turn against a model call.
	definitions := func() []ai.ToolDefinition {
		current := tools.Definitions()
		if task.Reflex {
			current = append(current, reflexPromotionDefinition())
		}
		// A reflex is one obvious micro-action and already has the verb for
		// "this is bigger than it looked": promote. Offering it a second way to
		// say so would be two answers to one question, and the first thing to
		// go wrong with a second answer is that it disagrees.
		if l.swarm && !task.Reflex {
			current = append(current, requestSplitDefinition())
		}
		return current
	}
	// THE GENERALIST NOW LEAVES A RECORD. Every turn of this loop and every
	// note the harness writes about itself already passes through this one
	// object; wiring the store's transcript into it is what makes the default
	// worker's work readable afterwards and resumable by whatever continues it.
	// See tracer.sink.
	trace := newRecordingTracer(ctx, l.workspace, task.leafKey())
	defer trace.close()
	system := l.system(task, tools.Guidelines())
	if contract := strings.TrimSpace(task.Contract); contract != "" {
		trace.note("contract:\n" + contract)
	}
	brief := l.brief(task)
	if duration := wallDurationText(wall); duration != "" {
		brief += "\n\nYou have " + duration + " of wall-clock time for this task."
	}
	userContent := text(brief)
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
	// What each tool result was, so a faded one can still be recognised.
	labels := map[string]string{}
	// fade shortens old observations losslessly: before a result is stubbed
	// its bytes go to a spill file the agent can re-read with sh. The decayer
	// carries the once-per-result bookkeeping across turns.
	fade := newDecayer(labels, tools.decaySpill)
	// Material the leaf has already been shown is carried once and pointed at
	// afterwards. This is keyed on the bytes rather than on the call, so every
	// route to the same content collapses and nothing is ever answered from a
	// stale copy; see observations.
	carried := newObservations(fade)
	warned := false
	wallPaced := false
	// landing counts the reserved turns left after the node has been told to
	// finish; zero means no landing has begun yet.
	landing := 0
	landingStop := StopReason("")
	// A repeated timeout is counted separately from no progress because a timed
	// out command is an error and errors are deliberately progress there. This
	// limit reads the runner's timeout fact instead. See tooltimeout.go.
	toolTimeouts := newToolTimeoutGuard()
	// The no-progress guard catches a leaf that is spending turns without
	// advancing: repeating the same tool call, going many turns without writing
	// anything or learning anything new, or simply running past any honest
	// leaf's measured need. The recon signal asks for the result and does nothing
	// else; the other three signals conclude through the same landing shape as
	// the budget and deadline reserves, so the workspace is left consistent and
	// the partial goes out whole. See noprogress.go for the signals and thresholds.
	progress := newProgressGuard()
	// The leaf's own closing. It is armed here, beside the other once-only
	// questions above, because it is one of them: a finding this leaf's own
	// after-photograph raises against this leaf's own work is put to it once
	// per kind, and the gate is the floor under whatever is still red the
	// second time. See selfclose.go.
	closer := NewSelfCloser(l.history, task)
	// startDeadlineLanding grants the protected reserve exactly once, whether it
	// was ordered between turns or after the lease arrived inside a model call.
	// Switching the turn context on the same line as the landing state is what
	// makes the reserve executable: every later model and tool call runs on a
	// clock the ordinary work could not spend.
	startDeadlineLanding := func() {
		landing = landingTurns
		landingStop = StopDeadline
		turnCtx, stopDeadlineLanding = landingClock(granted, landingReserve)
		// Recorded the moment the landing is ordered rather than when it
		// fails. The landing usually succeeds — that is what it is for — and
		// on that path Stop stays StopDone, so this is the only record that
		// the leaf was still working when the clock took it.
		outcome.Exhausted = StopDeadline
		outcome.Meter = Meter{Name: MeterDeadline, Unit: "seconds",
			Reached: int(time.Since(started).Seconds()), Allowed: int(l.deadline.Seconds())}
		trace.note("deadline close — landing reserve started")
		messages = append(messages, ai.Message{Role: "user", Content: text(
			"The wall-clock deadline for this task is close. Use the remaining time only to " +
				"land the work safely. In order: make whatever you were changing consistent " +
				"again; run the single quickest check that would catch breakage; fix only what " +
				"it reveals. Do not start anything new. Then give your final answer.")})
	}

	// The observation window is sized from what the model can hold in one
	// request, and from nothing else.
	//
	// It used to be one sixth of maxTokens, which is a ceiling on what the leaf
	// may spend across every turn of the whole run — a quantity with no
	// relationship at all to how much material fits in a single call. The
	// arithmetic looked like tuning and was a category error: at the default
	// budget it produced a 25KB window, so a 26KB file did not fit in the
	// memory meant to hold it, and the leaf spent the run re-reading what it had
	// already been shown. maxTokens stays what it is — the spend ceiling the
	// landing reserve and the wrap-up warning are measured against — and stops
	// sizing memory.
	obsBudget := observationWindow(l.contextTokens)
	obsSafetyBudget := observationSafetyWindow(l.contextTokens)

	// The turn ceiling this run is actually bound by. It is l.maxTurns for every
	// leaf that has anything to go and find, and FoldTurns for the one whose
	// material is already in the prompt above. The clamp is one-directional: a
	// fold never buys turns a caller did not grant, it only declines to use
	// them.
	turnCap := l.maxTurns
	if task.Fold {
		outcome.Mode = ModeFold
		if FoldTurns < turnCap {
			turnCap = FoldTurns
		}
		trace.note(fmt.Sprintf(
			"mode: fold — %d results were pushed into this prompt whole, so this leaf assembles "+
				"rather than gathers; %d calls, the second only for a tool call or an undeliverable reply",
			len(task.Inputs), turnCap))
	}

	for turn := 0; turn < turnCap; turn++ {
		if task.Control != nil {
			switch task.Control() {
			case ControlCancel:
				outcome.Stop = StopCancelled
				trace.note("cancel requested — stopping at turn boundary")
				return l.land(ctx, task, outcome, started, opening), nil
			case ControlPause:
				outcome.Stop = StopPaused
				trace.note("pause requested — holding at turn boundary")
				return l.land(ctx, task, outcome, started, opening), nil
			}
		}
		if landing == 0 && time.Until(deadline) <= landingReserve {
			startDeadlineLanding()
		}
		// A leaf gets one live reading of its own wall while there is still room
		// to act on it. This follows the deadline landing check so a leaf that has
		// just entered its reserve hears only the landing reason, and the once-only
		// flag keeps later turns from paying for the same reading again.
		if !wallPaced && landing == 0 {
			left := time.Until(deadline)
			gone := time.Since(wallStarted)
			if gone > wallPaceAfter {
				wallPaced = true
				goneText := wallDurationText(gone)
				leftText := wallDurationText(left)
				trace.note("wall pace — " + goneText + " gone, " + leftText + " left")
				messages = append(messages, ai.Message{Role: "user", Content: text(
					"The clock for this task now reads " + goneText + " gone and " +
						leftText + " left. Use the time that remains to produce the result " +
						"and leave room to check it.")})
			}
		}
		outcome.Steered += readSteering(task, &messages, trace)
		// Called every turn, but mutating on few of them: decay only fires once
		// the window crosses the budget, and then clears to a low-water mark so
		// the turns that follow can resend a byte-identical prefix and be billed
		// at the cached rate.
		retired := fade.decayWithin(messages, obsBudget, obsSafetyBudget)
		outcome.Decayed += retired
		// And, only when retiring spent raw material was not enough to bring the
		// whole live body back inside the working set, the leaf's own aged
		// reasoning folds the same way. It does nothing on the ordinary leaf; see
		// decayer.fold for the order and what protects the live edge.
		folded := fade.foldWithin(messages, obsBudget, obsSafetyBudget)
		outcome.Folded += folded
		// A rewrite anywhere in the transcript invalidates the prefix from that
		// point on, so THIS is the turn whose hit= will read near zero however
		// well the discipline is working. Written down beside the turn it belongs
		// to, the dip has a cause; without it, a benchmark reading the ratio can
		// only see a cache that intermittently fails. Both passes are batched
		// precisely so this line is rare — a run where it appears every turn is
		// the run whose hysteresis is not doing its job.
		if retired > 0 || folded > 0 {
			trace.note(fmt.Sprintf(
				"prefix rewritten — %d observation(s) retired, %d turn(s) folded; this turn re-reads cold",
				retired, folded))
		}
		// What the leaf had left before this turn, so the circuit breaker below
		// can weigh what the turn cost against what remained rather than against
		// the budget it started with.
		remaining := l.maxTokens - spent(outcome)
		response, err := l.complete(turnCtx, messages, definitions())
		if err != nil {
			// A lease that runs out inside one turn is the reserve arriving late,
			// not the leaf ending. The landing clock still owes this leaf its
			// reserve, so the cut turn is discarded and the landing is ordered on
			// the clock the work could not spend. The landing guard makes this arm
			// a one-time handoff rather than a retry loop.
			if landing == 0 && ctx.Err() != nil {
				startDeadlineLanding()
				continue
			}
			outcome.Stop = StopError
			outcome.Text = strings.TrimSpace(lastAssistantText(messages))
			if turnCtx.Err() != nil || ctx.Err() != nil {
				outcome.Stop = StopDeadline
				outcome.Exhausted = StopDeadline
				if !outcome.Meter.Named() {
					outcome.Meter = Meter{Name: MeterDeadline, Unit: "seconds",
						Reached: int(time.Since(started).Seconds()), Allowed: int(l.deadline.Seconds())}
				}
				// A lease spent during the landing did not finish. The error remains
				// non-nil because calling a guillotined landing clean would be a worse
				// lie than the missing meter this arm used to leave behind.
			}
			return l.land(ctx, task, outcome, started, opening), fmt.Errorf("node %s: %w", task.leafKey(), err)
		}
		outcome.Turns++
		// The node total, and the same numbers kept per turn. See meter.go: a
		// summed row cannot reproduce turns x context, and turns x context is
		// what every governor below is really about.
		outcome.meterTurn(response)

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
				return l.land(ctx, task, outcome, started, opening), nil
			}
		}
		// The cooperative ending, and it is terminal by contract: a leaf that
		// asked to divide does not continue, because the parts it just described
		// are planned against the partial it is holding, and a leaf that carried
		// on would be producing work its own children were already commissioned
		// to produce. The tool description says so in the same words.
		//
		// Every other call in the same turn is dropped with it. A model that
		// asks to stop and to run a command in one breath has said two things;
		// the stop is the one that was checked, and executing the rest would be
		// spending a budget the leaf has already handed back.
		if l.swarm && !task.Reflex {
			if request, asked := requestedSplit(calls); asked {
				outcome.Stop = StopSplit
				outcome.SplitRequest = request
				// The partial is what the parts consume, so it is taken from
				// the same two places the promotion path takes it from — the
				// leaf's own words this turn, and failing that the last thing
				// it said. A split with nothing behind it still divides; its
				// parts simply start from the assignment, as they would have if
				// the build had cut it this way in the first place.
				outcome.Text = strings.TrimSpace(response.Text())
				if outcome.Text == "" {
					outcome.Text = strings.TrimSpace(lastAssistantText(messages))
				}
				trace.turn(outcome.Turns, response, calls, nil, fmt.Sprintf(
					"asked to divide into %d parts", len(request.Parts)))
				return l.land(ctx, task, outcome, started, opening), nil
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
				return l.land(ctx, task, outcome, started, opening), nil
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
			if store.ClassifyEnd(finishOf(response), false) == store.EndLength {
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
			if landing == 0 && turn+1 < turnCap {
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
			landed := l.land(ctx, task, outcome, started, opening)
			// AND THE LEAF READS ITS OWN LANDING BEFORE ANYBODY ELSE DOES. The
			// photograph the line above just took is a measurement of THIS
			// leaf's work, and until now everything it found — a public name
			// deleted, a name read that nothing binds, a check turned red — went
			// past this worker to a gate, and came back as a repair round: a
			// cold leaf with a fresh brief and none of the context that made the
			// mistake. The worker that can fix it cheapest is the one still
			// standing here holding the transcript. So it is asked, once per
			// kind, inside what is left of its own meter — and lands with the
			// finding when there is nothing left, exactly as it did before.
			//
			// It reopens the loop the same way the mailbox above does, and for
			// the same reason: a leaf that has not landed has not delivered. The
			// answer just written goes in as the draft it now is, the finding
			// follows it, and the next turns settle it. See selfclose.go.
			if note, closing := closer.Close(landed, RoomLeft(landed, turnCap, l.maxTokens,
				time.Until(deadline), landingReserve)); note != "" {
				messages = append(messages,
					ai.Message{Role: "assistant", Content: text(response.Text())},
					ai.Message{Role: "user", Content: text(note)})
				// The verdict belongs to a landing that is no longer happening.
				// verdictFor keeps whatever it is handed, so a verdict written
				// for this reading would outlive it and grade the leaf on an
				// ending it did not have.
				landed.Verdict = ""
				trace.turn(outcome.Turns, response, nil, nil,
					"closing its own finding — "+strings.Join(SelfCloseKinds(closing), ", "))
				continue
			}
			return landed, nil
		}

		messages = append(messages, ai.Message{
			Role:      "assistant",
			Content:   text(response.Text()),
			ToolCalls: calls,
		})

		// A reply that ended at the output limit while holding tool calls is a
		// reply whose LAST call is very likely cut in half, and NONE of the
		// batch runs.
		//
		// The temptation is to run the ones that parsed. The reason not to is
		// that "parsed" is not "complete": arguments are JSON, a JSON object
		// truncated at a string boundary can still close and still validate,
		// and what arrives is a call the model never finished writing. A write
		// with half its text is a file silently truncated. An edit with half its
		// old is either no match — the cheap outcome — or, when the half happens
		// to be unique, a replacement that deletes the rest of the block. An sh
		// with half a command is a shell line whose meaning is unrelated to the
		// one intended: `rm -rf build/tmp` cut at the wrong byte is a different
		// command that runs fine.
		//
		// So the whole batch fails, unexecuted, and each call is answered with
		// the same sentence: the reply hit the limit, re-issue it. That is
		// cheap — one wasted turn — and it is the only failure here that leaves
		// the workspace exactly as the model believes it to be. Failing the
		// batch rather than the truncated call alone is deliberate too: the
		// earlier calls in a cut-off batch were written by a model that was
		// planning all of them together, and re-issuing them as a set keeps that
		// plan intact instead of half-applying it.
		if store.ClassifyEnd(finishOf(response), false) == store.EndLength {
			for _, call := range calls {
				outcome.ToolCalls++
				labels[call.ID] = callLabel(call)
				outcome.record(call, true)
				messages = append(messages, ai.Message{
					Role: "tool", ToolCallID: call.ID, Content: text(lengthStopBatchFailure),
				})
			}
			trace.turn(outcome.Turns, response, calls, nil, fmt.Sprintf(
				"reply hit the output limit holding %d tool calls — none executed, all returned for re-issue", len(calls)))
			continue
		}

		// A turn may carry several calls. They are independent by definition —
		// the model asked for them together — so running them concurrently is a
		// free wall-clock win, and the results go back in the order requested.
		// The no-progress guard and context decayer need to know whether the
		// turn wrote anything to disk. This is a revision rather than the
		// number of distinct paths: a second edit to the same file is still a
		// new action and spends the observations it used.
		mutationsBefore := l.workspace.MutationCount(task.leafKey())
		// And the same signal answers the decay pass's question. Everything the
		// transcript holds at this point was in front of the model when it
		// asked for this turn's calls, so if the turn changes the workspace,
		// the model acted on what it had read. See decayer.retire.
		historyBefore := len(messages)

		results := make([]Result, len(calls))
		var group sync.WaitGroup
		for index, call := range calls {
			outcome.ToolCalls++
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
				// The span every running command is opened inside: a tool
				// that takes minutes writes nothing to the journal while it
				// runs, and the claim reaper has nothing else to read. See
				// Working.
				defer Working(turnCtx)()
				results[index] = tools.Execute(turnCtx, call.Function.Name, call.Function.Arguments)
			}(index, call)
		}
		group.Wait()
		for index := range results {
			outcome.meterTool(results[index].Usage)
		}
		// The run record is written here rather than in the workers, because it
		// is one slice and several goroutines just finished. It records every
		// call the model asked for, including one whose bytes turn out to be a
		// repeat — the model asked, and a reader checking whether a check was
		// ever run needs the ask.
		for index, call := range calls {
			outcome.record(call, results[index].IsError)
			outcome.noteCommand(call)
		}

		trace.turn(outcome.Turns, response, calls, results, "")

		// What the transcript actually carries, which is what is billed on every
		// remaining turn — the body the first time these bytes appear, a pointer
		// to the earlier copy after that. The same total is what the decay pass
		// is told about: hysteresis is sized from the bytes the leaf really adds
		// per turn, not from the bytes its tools happened to return.
		admitted := 0
		for index, call := range calls {
			body := carried.admit(outcome.Turns, call, results[index])
			admitted += len(body)
			messages = append(messages, ai.Message{
				Role:       "tool",
				ToolCallID: call.ID,
				Content:    text(body),
			})
		}
		fade.observe(admitted)

		// Read once and used twice: the decay pass learns what this turn acted
		// past, and the no-progress guard below weighs the same pair. Nothing
		// between here and there can add an artifact, because only a tool call
		// can, and this turn's have all finished.
		mutationsAfter := l.workspace.MutationCount(task.leafKey())
		if mutationsAfter > mutationsBefore {
			fade.actedPast(historyBefore)
		}

		for _, result := range results {
			if len(result.Followup) == 0 || result.IsError {
				continue
			}
			messages = append(messages, ai.Message{Role: "user", Content: result.Followup})
		}

		// A fold about to take its last call is told so. This is not a rule
		// about how to behave, it is the mechanism stated: the loop stops after
		// the next reply, and a model that does not know that will spend the
		// reply asking for another tool and be cut off holding it. The same
		// courtesy the deadline and budget reserves already extend, for the same
		// reason and in the same place.
		if task.Fold && turn+2 >= turnCap {
			messages = append(messages, ai.Message{Role: "user", Content: text(
				"That was the one round of tool calls this step has. Your next reply is the " +
					"result: state it in full, in the body of the message.")})
		}
		// The budget ends work in two stages, and the staging is what protects
		// the workspace. A hard stop at the limit truncated runs mid-edit —
		// twice it left files syntactically broken with the model's final,
		// already-emitted repairs discarded unexecuted. So the emitted calls of
		// every turn always execute (they are paid for), and exhaustion buys a
		// short landing instead of a guillotine: a few reserved turns whose only
		// job is to leave the workspace consistent, checked, and answered.
		//
		// Either bound ends work the same way. The cost ceiling says the leaf
		// has spent its money; the raw ceiling says it has stopped converging on
		// a warm prefix, which the cost ceiling cannot see. Both buy the same
		// landing, and both record the same StopBudget, because from the leaf's
		// side and from the reconciler's they are one fact: this node was still
		// working when its allowance ran out.
		// WHAT LANDS A LEAF IS WHAT ITS WORK COSTS, AND NOTHING ELSE COUNTS
		// TEXT. The two cumulative prompt bounds that used to land it here are
		// kept as pressure on the wrap-up warning above (see budgetUsed) and
		// journaled as evidence, but they no longer stop anything. See
		// contextPressure and reuseCeiling in meter.go for the measurement that
		// retired them.
		if exhausted(outcome, l.maxTokens) && landing == 0 {
			landing = landingTurns
			landingStop = StopBudget
			// Same reason as the deadline reserve above: the budget is spent
			// here, whether or not the landing later has to be cut short.
			outcome.Exhausted = StopBudget
			// The bound names itself, with its own two numbers, so the journal
			// and the line a person reads are composed from one fact rather
			// than from two guesses. See Outcome.Meter.
			outcome.Meter = Meter{
				Name: MeterCost, Reached: spent(outcome), Allowed: l.maxTokens,
				Unit: "tokens of billed work",
			}
			reached := "budget exhausted — landing reserve granted"
			trace.note(reached)
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
				return l.land(ctx, task, outcome, started, opening), nil
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
		if !warned && budgetUsed(outcome, l.maxTokens, l.contextTokens) > wrapUpAt {
			warned = true
			messages = append(messages, ai.Message{Role: "user", Content: text(
				"You have used most of the budget for this task. If the deliverable is not yet " +
					"produced and connected into place, start that now and leave room for one " +
					"verification pass at the end — work you have not done yet will not happen. " +
					"Stop exploring; nothing you have already confirmed needs another look.")})
		}

		// A leaf already landing keeps the reason that granted its reserve. The
		// repeated timeout is checked only while no legitimate ending has spoken,
		// so it cannot rewrite a budget or deadline as a different finding.
		if landing == 0 {
			if reason, count, stop := toolTimeouts.observe(calls, results); stop {
				outcome.Stop = StopToolTimeouts
				outcome.Exhausted = StopToolTimeouts
				// This meter is the durable structured account of the bound. This
				// ending is not out of room, so no person-facing meter renders it.
				outcome.Meter = Meter{
					Name: MeterToolTimeouts, Reached: count, Allowed: toolTimeoutRepeatCap,
					Unit: "timeouts of one command",
				}
				outcome.Text = strings.TrimSpace(lastAssistantText(messages))
				trace.note(reason)
				journalToolTimeout(l.history, task, outcome, reason)
				return l.land(ctx, task, outcome, started, opening), nil
			}
		}

		// The no-progress guard, checked AFTER the legitimate bounds. A leaf
		// that exhausted its budget, hit the pressure ceiling, was handed back
		// by the straggler, or is already landing must not be stopped for "no
		// progress" — those are the reasons the leaf stopped, and misattributing
		// them would teach the ruler nothing. The guard fires only when no
		// legitimate bound has spoken: the leaf had money and turns left and was
		// not advancing.
		if landing == 0 {
			switch progress.observe(calls, results, mutationsBefore, mutationsAfter) {
			case progressPace:
				trace.note(progress.reconNote())
				messages = append(messages, ai.Message{Role: "user", Content: text(progress.reconNotice())})
			case progressConclude:
				progress.markConcluded()
				trace.note(progress.noProgressReason() + " — conclude directive injected")
				messages = append(messages, ai.Message{Role: "user", Content: text(noProgressConcludeDirective)})
			case progressTerminate:
				outcome.Stop = StopNoProgress
				outcome.Exhausted = StopNoProgress
				// The one bound with no allowance worth printing: it is a
				// structural detector rather than a ceiling, and what it
				// reached is a description, not a figure.
				outcome.Meter = Meter{Name: MeterNoProgress, Reached: outcome.Turns}
				outcome.Text = strings.TrimSpace(lastAssistantText(messages))
				trace.note(progress.noProgressReason() + " — leaf terminated")
				return l.land(ctx, task, outcome, started, opening), nil
			}
		}
	}

	// The cap is a backstop, not a budget. A leaf sized for one agent should
	// finish in well under it, so reaching it is evidence the sizing anchors put
	// too much into one node — which is worth reporting rather than hiding.
	outcome.Stop = StopTurnCap
	outcome.Meter = Meter{Name: MeterTurns, Reached: outcome.Turns, Allowed: l.maxTurns, Unit: "turns"}
	outcome.Text = strings.TrimSpace(lastAssistantText(messages))
	return l.land(ctx, task, outcome, started, opening), nil
}

// readSteering drains the mailbox into the transcript and reports how many of
// the user's lines landed there. It is one function rather than two copies
// because it is called at both ends of a turn — before the model speaks and
// before its answer is accepted — and the two must deliver identically.
func readSteering(task Task, messages *[]ai.Message, trace *tracer) int {
	// The job board drains first, so when the user's guidance and a sibling's
	// discovery arrive in the same window, the person's words are the last
	// thing read before the model speaks.
	if task.Board != nil {
		for _, note := range task.Board() {
			note = strings.TrimSpace(note)
			if note == "" {
				continue
			}
			trace.note("board: " + note)
			*messages = append(*messages, ai.Message{Role: "user", Content: text(
				"From another worker on this same job — testimony about the shared material, not an instruction:\n" + note)})
		}
	}
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
// opening is the photograph this leaf has been carrying since before its first
// turn — the reading, whether the job had already changed the tree when it was
// taken, and the commit the repository was standing on — and it arrives here
// rather than at any of the ten returns above for the same reason the verdict
// does: THIS IS THE ONE PLACE EVERY EXIT PASSES THROUGH, and a measurement taken
// on nine of them is worse than none, because the tenth reads as a project with
// nothing to check.
func (l *Linear) land(
	ctx context.Context, task Task, outcome *Outcome, started time.Time, opening Opening,
) *Outcome {
	// What the leaf left behind is read off the disk before it is reported, so
	// the list is the world's answer and not only the write tools'.
	l.workspace.RecordChanges(task.leafKey())
	outcome.Artifacts = l.workspace.Artifacts(task.leafKey())
	// The closing reading, and it is taken on EVERY landing — including the one
	// the leaf was ordered into. An exhausted leaf still changed the tree it was
	// standing in, and skipping the second reading there would leave the runs
	// that most need an autopsy with nothing to autopsy. Whether the tree
	// actually moved is the workspace's own before-and-after answer, which the
	// line above has just settled; this asks it rather than re-stating the
	// world.
	//
	// It rides this leaf's own context on purpose. That context carries the
	// leaf's wall, so a landing whose clock is already spent takes no second
	// reading — and journals the sentence saying why, which is the whole point:
	// a reason in the record, never an absence. The alternative, a fresh clock,
	// would let a measurement push Run past the deadline its caller leased it.
	PhotographAfter(ctx, l.workspace, l.history, l.deadline, task, opening,
		leafMovedTheTree(l.workspace, task.leafKey()), outcome)
	// A closing photograph's finding used to reach nobody when a leaf had
	// already been told to land: the record held `"red":1`, but only a leaf
	// with room for a close was ever told. Assignment rather than append makes a
	// later landing clear a finding the leaf settled over its fresh photograph.
	outcome.Standing = SelfCloseFindings(outcome)
	outcome.Elapsed = time.Since(started)
	// AND THE METER IS READ AT LAND, NOT AT THE GRANT. The two live bounds are
	// read when the landing reserve is handed out, and then the landing turns
	// run — more model calls, more seconds — so the figure the journal keeps and
	// the figure the person reads were both the leaf's spend one turn before it
	// stopped. Recomputed from the run's own usage rows, a measured run reported
	// 152,090 tokens against 172,791 actually spent and 178,086 against 199,131:
	// every exhaustion line under-reported by 12-14%, and an autopsy comparing a
	// grant against a reading of the grant learns nothing. This is the one place
	// every exit passes through, so it is where the reading is taken.
	switch outcome.Meter.Name {
	case MeterCost:
		outcome.Meter.Reached = spent(outcome)
	case MeterDeadline:
		outcome.Meter.Reached = int(time.Since(started).Seconds())
	}
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
	case StopSplit:
		// A leaf that handed its budget back because it had found several jobs
		// inside one did not fail to converge — it declined to converge on the
		// wrong thing. Grading it as a budget stop would teach the ruler that
		// this worker could not do the work, from the one run where it read the
		// work correctly.
		return provider.VerdictUnverifiedSuccess
	case StopPaused, StopCancelled:
		// User-directed stops say nothing about model capability.
		return provider.VerdictUnverifiedSuccess
	case StopEmpty:
		return provider.VerdictEmptyResponse
	case StopOverrun:
		// Graded exactly as a budget stop, because it is the same finding
		// arriving earlier: this worker did not converge inside what work of
		// this kind takes here. Reading it any other way would leave the one
		// leaf that most needed to teach the ruler something teaching it
		// nothing — and the ruler is rewritten from precisely this evidence.
	case StopNoProgress:
		// The leaf had money and turns left and was not advancing. Graded
		// as a budget stop — the same finding the ruler recalibrates from —
		// because the mechanism is a tail-risk bound on a runaway, not a
		// judgment that the work was wrong.
		return provider.VerdictBudgetStop
	case StopToolTimeouts:
		// The leaf had money, turns and clock left and spent them re-running a
		// command it had already learned does not return. Graded as a budget stop
		// because that failure to converge is exactly what a rating measures.
		return provider.VerdictBudgetStop
	case StopError, StopDeadline:
		return provider.VerdictProviderFailure
	}
	// A landing the leaf was ordered into is not the ending it chose. Stop says
	// it finished cleanly, which is true — it complied with the order — but the
	// work was not finished when the order came, and that is precisely what a
	// rating measures. Only the budget grades: a deadline is a fact about the
	// clock rather than about ability, exactly as the StopDeadline arm above.
	if outcome.Exhausted == StopBudget || outcome.Exhausted == StopOverrun || outcome.Exhausted == StopNoProgress {
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
		// A refusal of the request ITSELF is not retried: the same body sent
		// into the same wall three times is three bills for one answer, and
		// the answer is the provider's own words, which are already here.
		// Anything else — a dropped connection, a busy upstream — earns the
		// attempts below.
		if refusal, ok := provider.RefusalFrom(err); ok && refusal.OurRequest() {
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

// brief assembles what the agent sees. The order matters: the working method
// leads because it is the standing law for this kind of job and everything
// below is read through it, the goal orients, the inputs are the only upstream
// work it is allowed to know about, and its own instruction comes last so it is
// the freshest thing in the prompt.
//
// The method used to be appended to the system message, where it broke the one
// property that message is worth having: being the same bytes for every leaf in
// a run. Here it costs nothing — the brief is per-node by definition, so the
// prefix was already going to end at the top of this message — and the law
// still reaches the model ahead of the assignment it governs. See system.
func (l *Linear) brief(task Task) string {
	var block strings.Builder
	if contract := strings.TrimSpace(task.Contract); contract != "" {
		fmt.Fprintf(&block, "How this particular kind of job is done well:\n%s\n\n", contract)
	}
	if task.Goal != "" {
		fmt.Fprintf(&block, "This work is part of a larger goal:\n%s\n\n", task.Goal)
	}
	if len(task.Inputs) > 0 {
		block.WriteString("Results from earlier work, which you already have and must not gather again. " +
			"Where two of them speak to the same quantity, the LATER one stands: a corrected figure " +
			"replaces its predecessor, and reaching back past a correction to the number it corrected " +
			"is the one way to be wrong with everything you need in hand:\n")
		for _, input := range task.Inputs {
			fmt.Fprintf(&block, "\n=== from %q ===\n%s\n", input.Title, input.Result)
			if len(input.Artifacts) == 0 {
				continue
			}
			// The header above claims the leaf already holds this work, and
			// until the edge carried the product that claim was false for every
			// producer whose deliverable was a file: the leaf was handed a
			// sentence naming a path and an invitation to go and read it, and it
			// read it — measured, nineteen turns of archaeology across four
			// nodes of one job. The invitation is now made only where it is
			// still true. Where the text above IS the file, the paths are given
			// for citation and for the one case the block cannot serve, and the
			// line says which it is.
			if input.Whole {
				fmt.Fprintf(&block, "(the text above is the whole of what those files contain: %s — "+
					"you are holding their contents, so opening them again buys nothing)\n",
					strings.Join(input.Artifacts, ", "))
				continue
			}
			fmt.Fprintf(&block, "(files: %s — read them if you need the full detail)\n", strings.Join(input.Artifacts, ", "))
		}
		block.WriteString("\n")
	}
	if l.briefIsWhole(task) {
		// The claim that was never made. The prompt fences the leaf inside its
		// working directory and tells it what it holds; nothing in it ever said
		// the holding was COMPLETE, and an agent with no such assurance does the
		// only responsible thing — it goes and looks. Measured: five of eleven
		// turns spent listing machinery and reading files back. The one place this
		// assurance already existed is the Whole fan-in line four lines up, and
		// that is precisely where the looking stopped.
		//
		// It is one sentence and it is stated only where it is a fact — see
		// [Linear.briefIsWhole], which reads the task's own data and then reads
		// the directory rather than assuming anything about it.
		block.WriteString("This is the whole of what exists for this job: everything above is in your hands, " +
			"and there is nothing in the working directory to discover before you produce. " +
			"Begin on the work itself.\n\n")
	}
	// The orientation digest carries the repo's directory tree and code
	// declarations so the leaf's first turn is spent on the work, not on
	// listing files and reading headers. It is assembled once (cached per
	// workspace) and omitted when the brief already says the directory holds
	// nothing to discover — see briefIsWhole — so the two never contradict.
	if l.workspace != nil && !l.briefIsWhole(task) {
		if digest := strings.TrimSpace(orientation.BuildDigest(l.workspace.Root(), nil)); digest != "" {
			block.WriteString(digest)
			block.WriteString("\n\n")
		}
	}
	block.WriteString("Your work:\n")
	block.WriteString(task.Brief)
	if task.Share != nil {
		// The measured miss this line exists for: a sibling found the
		// duplicated row while the revenue worker was still summing, told
		// nobody, and the total shipped wrong. The tool's own description says
		// what share is; this says when — the moment of discovery, because
		// the others are acting on the material right now.
		block.WriteString("\n\nOther workers are on this job with you right now. The moment you discover " +
			"something about the shared material — a unit, a quirk, a duplicate, a broken assumption, a dead end — " +
			"share it (the share tool) before you continue. They are acting on that material as you read this, " +
			"and what you just learned may be the difference between their answer being right or wrong.")
	}
	block.WriteString(outputClause(task))
	return block.String()
}

// briefIsWhole reports that the brief this leaf is about to read is the whole of
// what exists for its job.
//
// Three conditions, and every one of them is a fact rather than a judgment:
//
//   - Every input arrived with its material rather than a pointer at it. Whole
//     is set by whoever assembled the task, from what it actually inlined; an
//     input on a handle, or one clipped to fit, is an input the leaf has to open.
//   - Nothing was attached that still has to be read. A staged document or an
//     image is material the leaf holds only after it has gone and got it, which
//     is the exact act this sentence would be telling it not to do.
//   - The working directory holds no file the leaf could discover other than the
//     ones its inputs already named. This is the half that cannot be reasoned
//     out — a person's repository and a fresh job directory are the same shape
//     to a Task — so it is measured, once, by a bounded walk that returns on the
//     first unaccounted file. See [Workspace.HoldsNothingBut].
//
// A leaf with no inputs at all can satisfy all three, and that is correct: a
// first leaf in an empty directory genuinely has nothing to find. The same leaf
// pointed at somebody's project fails the third and is told nothing, which is
// also correct — the project is the material.
func (l *Linear) briefIsWhole(task Task) bool {
	if len(task.DocumentPaths) > 0 || len(task.ImagePaths) > 0 {
		return false
	}
	var named []string
	for _, input := range task.Inputs {
		if !input.Whole {
			return false
		}
		named = append(named, input.Artifacts...)
	}
	return l.workspace.HoldsNothingBut(named)
}

// outputClause says where a file goes, and — far more often — that there is no
// file to write.
//
// It used to say only the first half, to every leaf, as a standing offer: "a
// standalone document goes to NN-title.md". Workers took the offer, because an
// address handed to you reads as an expectation, and a run left
// 07-pr-482-code-review.md beside 70-read-diff.md and 144-synthesis.md in the
// person's own directory — process artifacts from nodes whose whole output was
// consumed downstream, littered next to the one file anybody might have wanted.
//
// So the offer is conditioned rather than phrased away. No list of giveaway
// words: the model judges whether the ask named a file or the content is
// genuinely unusable as a message, which is the only honest test and the only
// one that survives contact with work nobody anticipated.
//
// That judgment is the delivery law's carve-out arriving at the leaf, and it
// must agree with plan.DeliverInMessage and plan.DeliverToNamedFile, which is
// where the law is stated once for every prompt that commissions a deliverable.
// The wording here is the leaf's own — second person, and specific about the
// address it is being offered — but the two facts are the law's: a message that
// says where the answer lives instead of carrying it has delivered nothing, and
// an ask that named the file makes the file the deliverable, with the message
// carrying the answer beside it rather than in place of it.
func outputClause(task Task) string {
	if task.Intermediate {
		if task.OutputHint == "" {
			return "\n\nNothing you produce here is handed to anyone: your result is read by the work " +
				"that comes after you, and your final message is how it travels. There is no document to write."
		}
		return fmt.Sprintf("\n\nNothing you produce here is handed to anyone: your result is read by the "+
			"work that comes after you, and your final message is how it travels. Do not write a document "+
			"for it. If the work itself needs a file — something too large to carry in a message, or notes "+
			"you will read back — put it at %s and name that path in your final message. Work that belongs "+
			"inside existing material still goes there.", task.OutputHint)
	}
	if task.OutputHint == "" {
		// No address was offered, and that is not a loophole: the two cells this
		// product lost on a blind reading were lost to a worker who invented its
		// own file name and left the message pointing at it.
		return "\n\nYour final message is the deliverable — it is the whole of what the person will read, " +
			"and the substance belongs in it. Write a file only when they asked for one or your " +
			"instructions name a place; a message that says where the answer lives instead of " +
			"carrying it has delivered nothing. Work that belongs inside existing material goes there."
	}
	return fmt.Sprintf("\n\nIf your instructions already say where the deliverable goes, that wins. "+
		"Otherwise your final message is the deliverable — it is the whole of what the person will read, "+
		"and the substance belongs in it. Write a separate document as well only when they asked for a "+
		"file or when what you produced cannot be read as a message; it goes to %s, named in your final "+
		"message beside the substance and never in place of it. A message that says where the answer "+
		"lives instead of carrying it has delivered nothing. Whether or not you write that file, the "+
		"message carries the whole answer on its own: a file is a second copy for whoever wants the "+
		"detail, and nobody reading after you can be required to open one. If you do write it, it is "+
		"that one address and no other — a name you invented is a file nobody will look for. Work that "+
		"belongs inside existing material goes there — never into a separate file.", task.OutputHint)
}

// wrapUpAt is how much of the budget may be spent before the model is told to
// land the work.
const wrapUpAt = 0.7

// wallPaceAt is the fraction of a leaf's wall that may pass before it gets one
// live clock reading. Half is earlier than the budget's wrap-up fraction because
// elapsed time cannot be bought back, and it remains far ahead of the bounded
// deadline landing reserve where the leaf must stop starting work.
const wallPaceAt = 0.5

// landingTurns is the reserve granted after the budget runs out: enough calls
// to restore consistency, run one check, and repair one breakage — never
// enough to keep working. The reserve is what stands between "budget reached"
// and "workspace left broken mid-edit".
const landingTurns = 4

// landingClock is the time an ordered landing actually gets, and THE WORK
// CANNOT SPEND IT.
//
// A leaf was granted a ninety-second landing reserve, the turn in flight left
// sixty-one seconds of it, and the landing's own model call was then cancelled
// by the very lease that ordered the landing. The leaf ended mid-edit holding a
// tree that did not build. An ordered landing therefore gets its whole reserve,
// measured from the moment it was ordered, rather than the remainder of a clock
// ordinary work was allowed to consume.
//
// The landing can end at most one reserve past the leaf's lease, and
// deadlineLandingReserve is capped at watchdogPad. The node watchdog already
// sits that pad above the lease because it is the room the landing needs. This
// is one bound in two parts, and neither half moves alone. A deadline on granted
// is the errand's own wall and still cuts the landing there, because the
// person's number outranks the leaf's pad.
func landingClock(granted context.Context, reserve time.Duration) (context.Context, context.CancelFunc) {
	return context.WithTimeout(granted, reserve)
}

// deadlineLandingReserve is what a deadline-stopped leaf is granted to land.
// Its cap is watchdogPad because the pad is where the node watchdog sits so the
// landing fits underneath it. The two are one bound in two parts, and neither
// half moves alone.
func deadlineLandingReserve(deadline time.Duration) time.Duration {
	reserve := deadline / 10
	if reserve > watchdogPad {
		return watchdogPad
	}
	return reserve
}

// wallDurationText spells a measured wall the same way --timeout accepts it.
// Whole trailing units carry no information, so they are removed after the
// reading is rounded to the second; an unknown or zero reading stays absent.
func wallDurationText(duration time.Duration) string {
	if duration <= 0 {
		return ""
	}
	duration = duration.Round(time.Second)
	if duration <= 0 {
		return ""
	}
	spelled := duration.String()
	if strings.HasSuffix(spelled, "m0s") {
		spelled = strings.TrimSuffix(spelled, "0s")
	}
	if strings.HasSuffix(spelled, "h0m") {
		spelled = strings.TrimSuffix(spelled, "0m")
	}
	return spelled
}

// cachedTokenWeightPercent is what one re-sent cached prompt token costs
// against the leaf's ceiling, as a percentage of a fresh one.
//
// The ceiling exists to bound spend. It was counting raw tokens, which is a
// different quantity the moment a prefix cache is in play: a provider bills the
// cached rate for the longest prefix that is byte-identical to the previous
// call, and everything this loop does to keep that prefix stable — the frozen
// tool block, the batched decay, the run-wide affinity key — is work done to
// make most of every turn's prompt cheap. Charging those tokens at full weight
// makes the ceiling bind on the transcript's SIZE rather than on its COST, so a
// leaf whose memory grew ten-fold could no longer finish inside one budget even
// though the money it spent barely moved.
//
// Ten percent is deliberately conservative against the market. Cache reads are
// billed at 10% of the input rate by the Anthropic-family endpoints and at
// roughly 10-25% elsewhere, so this never flatters a run: a token discounted
// here is a token that really was cheaper, and by at least this much. It is
// applied only to what the provider itself reported as a cache read — see
// addUsage — so a provider that reports nothing is billed exactly as before and
// the ceiling degrades to the raw count it always was.
const cachedTokenWeightPercent = 10

// spent is what this leaf has cost so far, in the units the ceiling is written
// in: fresh prompt tokens at full weight, cache reads at a fraction, completion
// tokens at full weight because nothing about them is ever cached.
func spent(outcome *Outcome) int {
	usage := outcome.Usage
	cached := usage.CachedTokens
	// A provider that reports more cache reads than prompt tokens is reporting
	// something this arithmetic cannot use; clamping keeps the discount a
	// discount rather than a credit.
	if cached > usage.PromptTokens {
		cached = usage.PromptTokens
	}
	if cached < 0 {
		cached = 0
	}
	fresh := usage.PromptTokens - cached
	return fresh + cached*cachedTokenWeightPercent/100 + usage.CompletionTokens
}

// rawSpent is the same leaf with no discount at all: every token the provider
// was sent and every token it sent back, warm or cold. It is the quantity the
// convergence bound is written in, because it is the one that tracks how much
// work has gone past rather than how much of it was billed.
func rawSpent(outcome *Outcome) int {
	return outcome.Usage.PromptTokens + outcome.Usage.CompletionTokens
}

// exhausted reports whether the leaf has spent the grant it was given.
//
// ONE BOUND ON WORK, AND IT IS THE MONEY. It used to be two — this, and the
// undiscounted raw ceiling below — and the second is now pressure on the
// wrap-up warning rather than a landing, because Σ over turns of the prompt is
// not a second opinion about spend. It is turns × mean-context wearing a token
// name: any transcript that only grows re-sends its whole prefix every turn, so
// the sum climbs at the same rate for a leaf doing hard work as for one
// circling, and the measured separation between the two is nil (see
// reuseCeiling in meter.go for the figures out of the ink run of 2026-08-29).
//
// What is left to catch the warm runaway the raw ceiling was added for — very
// many cheap turns, no per-turn prompt above 17k — is the pair built to tell
// hard work from stuck: maxTurnBackstop and the no-progress guard, whose own
// file opens by saying that a magnitude bound cannot make that distinction and
// that this is what was missing. It fires at noProgressTurnFloor, which is a
// fifth of where the raw ceiling would have.
func exhausted(outcome *Outcome, maxTokens int) bool {
	return spent(outcome) >= maxTokens
}

// rawCeiling is the grant expressed in undiscounted tokens.
func rawCeiling(maxTokens int) int { return maxTokens * rawTokenCeilingMultiple }

// The leaf's third bound — the cumulative one — is reuseCeiling in meter.go,
// beside the ledger it is measured off.

// budgetUsed is how far into its allowance the leaf is, read on whichever of
// its three readings is furthest along. The wrap-up warning is measured against
// this rather than against cost alone, so a heavily cached leaf hears that it
// should be landing while it still has the turns to land in.
//
// THIS IS WHERE THE TWO CUMULATIVE BOUNDS LIVE NOW. They used to land the leaf
// and they were wrong to, but they are not nothing: a leaf whose transcript has
// gone round many times over IS more likely to be near the end of its useful
// run than one that has not, and telling it so costs a sentence and lands
// nobody. A warning that fires early on an honest leaf makes it wrap up
// promptly; a stop that fires early on an honest leaf throws its work away.
func budgetUsed(outcome *Outcome, maxTokens, contextTokens int) float64 {
	if maxTokens <= 0 {
		return 0
	}
	used := float64(spent(outcome)) / float64(maxTokens)
	if raw := float64(rawSpent(outcome)) / float64(rawCeiling(maxTokens)); raw > used {
		used = raw
	}
	if ceiling := reuseCeiling(contextTokens); ceiling > 0 {
		if reuse := float64(outcome.contextPressure()) / float64(ceiling); reuse > used {
			used = reuse
		}
	}
	return used
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
	// Cache reads arrive under two different names. OpenAI-shaped endpoints nest
	// them in prompt_tokens_details.cached_tokens; Anthropic-family endpoints —
	// including OpenRouter when it passes the upstream body through rather than
	// normalising it — spell them cache_read_input_tokens at the top level. This
	// read used to see only the first, so on the second shape every leaf recorded
	// zero cache reads however warm the prefix actually was, and the ceiling
	// discount above would never have engaged. The SDK's accessor knows both.
	usage.CachedTokens += response.Usage.CacheReadTokens()
	if response.Usage.Cost != nil {
		usage.Cost += *response.Usage.Cost
	}
}
