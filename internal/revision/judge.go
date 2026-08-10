// Package revision is the engine half of a job's second thoughts: the gate
// that decides whether a deliverable is done, the citation invariant that
// decides whether a named gap may buy more work, the judgements that decide
// who takes a retry and whether an exhausted leaf left anything behind, and
// the sentinel pass that edits a job's remaining plan in light of what just
// landed.
//
// It lived inside cmd/aforge/chat.go, which meant none of it was reachable
// from internal/ — a resident-side orchestrator could run work and could not
// judge it. Nothing here knows about a terminal, a session, or a window: every
// entry point takes the durable graph, the node in question, and a client, and
// returns a judgement. The wiring that decides what to do with one stays with
// whoever is running the work.
//
// The sentinel deliberately does not own the locks it runs under. A job's plan
// document is guarded by the registry that retains it, and the pass takes and
// gives back that lock around the model round-trip; that arrangement is the
// caller's, so the caller passes in a plan.Completer that already knows how to
// let go while it is thinking.
package revision

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"strings"
	"unicode/utf8"

	"github.com/Agent-Field/aforge-v2/internal/config"
	"github.com/Agent-Field/aforge-v2/internal/exec"
	"github.com/Agent-Field/aforge-v2/internal/provider"
	"github.com/Agent-Field/aforge-v2/internal/provider/pool"
	"github.com/Agent-Field/aforge-v2/internal/resident"
	"github.com/Agent-Field/aforge-v2/internal/router"
	"github.com/Agent-Field/aforge-v2/internal/store"
	"github.com/Agent-Field/agentfield/sdk/go/ai"
)

// DeliverablePrompt is a gate, not a critic: its default is pass, and a
// fail must name the specific element of the request that is absent. The
// failure mode being prevented is the gate that always finds something —
// polish loops that spend the user's money on taste.
//
// The working-decisions paragraph ends on verification for the same reason it
// began with method: a promise about evidence and a claim of evidence are the
// same commitment seen from either end. What the deliverable shows about the
// finished thing being used the way it will be used is the first thing it can
// hold — which is exactly what a leaf skips when it proves the parts and infers
// the whole. An honest "not verified here, run this" passes, so the clause
// never pushes anyone towards the lie.
//
// The evidence paragraph is the second thing it can hold, and it is the one
// that stops the claim from being self-certifying. For as long as the gate read
// only the final message, the strongest sentence in the language — "verified" —
// cost a worker nothing to write and the gate nothing to believe. It now
// receives what the leaf left in the workspace and the tail of what the leaf
// actually ran, both of which already existed and neither of which costs a
// call. The records are stated as partial on purpose: they are a tail and one
// directory, so they can convict a claim and can never acquit the absence of
// one, and a gate told otherwise would start failing honest work for the sin of
// having run somewhere it cannot see.
//
// The working-method paragraph closes the hole that made all of this weaker
// than it reads on a planned job. The gate's "compiled goal" for such a job was
// the harness's own two-line stub — "Synthesis / Assemble the finished answer" —
// because the passes that write instructions and methods only ever ran for work
// leaves, and the node that IS the deliverable is not one. The method is where a
// kind of work states what done means and how it is checked, in its own terms
// and per job rather than per domain, which is the only calibration this gate can
// have that is neither a hardcoded rubric nor the worker's own opinion of itself.
//
// The middle paragraph was added after a live failure the gate waved through. A
// worker asked to judge an architecture plan wrote its judgement into a file and
// ended with "the deliverable is written and verified against the actual repo
// source" — true, complete, and containing no verdict. That text became the
// node's summary, and the summary is the single source every later surface
// reads, so the answer existed nowhere the user or the head could reach it. The
// paragraph is stated as a value rather than a list of giveaway phrases,
// because the next way to describe work instead of doing it is always a phrasing
// nobody wrote down: the question is whether the substance is present, not
// whether some sentence pattern is.
const DeliverablePrompt = `You are the final gate before a finished piece of work is handed to the person who asked for it. You receive their verbatim request, the compiled goal, and the deliverable as produced.

Judge exactly one question: would the person who asked accept this as done? Default to PASS. The gate exists for real gaps, not polish — wording, style, and things they never asked for are not gaps.

FAIL only when you can name a specific element of the request that is absent, unanswered, or unsupported by evidence the goal promised. Quote or name the missing element concretely enough that a worker could close the gap from your words alone.

Working decisions declared in the goal are part of what was promised. A commitment about method or evidence — what would be run, checked or reviewed before the work was handed over — is a gap when nothing in the deliverable shows it happened. A claim that the work was checked, proven or verified is itself such a commitment: it is a gap unless the deliverable shows the finished thing exercised the way it will actually be used — what was run, what came back — rather than its parts checked one by one and the whole inferred from them. Naming what could not be verified here, and the check the person can run themselves, is not a gap: it is the honest form of the same claim and it passes.

One absence counts exactly like every other and is the one most easily waved through: the substance itself. What you are handed IS the deliverable — it is the whole of what the person will read, and nothing beside it will be opened for them. So text that reports on the work rather than carrying it — that the work is finished, that a file now holds the answer, that the analysis was checked and is consistent — has described the deliverable in place of being it, and the element of the request that is absent is the answer: the verdict that was asked for, the findings, the numbers, the recommendation. Name that as the gap. A pointer to where the answer lives is not the answer however true the pointer is; naming the file is right beside the substance and never instead of it. The same absence in the future tense is the purest form of it: text saying what would be looked up, what will be compared, what remains to be checked, is a plan for producing the answer handed over in place of the answer, and it is a gap however sound the plan is. This is still one absence and not a second style test: text that gives the answer in its own plain words passes whatever shape it takes.

Below the deliverable, whenever there is anything to show, you are given two records of the run itself: what it left behind, and the tail of what it actually ran. Read the deliverable's claims against them, the way the person would. Something named as produced that nothing produced, or a check the work says it made when nothing of that kind appears in what it ran, is an element unsupported by evidence and is a gap of exactly the kind above — name it in those words. Both records are partial by construction: the tail is the end of a longer run, and what was left behind is one place among many. So they can convict a claim and never acquit one — silence in them is evidence, never proof, and where the deliverable's own account is consistent with what is there, or where these records could never have held the thing in question, pass. One shape in these records is read against the substance rule above: the run wrote a file and the deliverable's own text is thin beside it. Where the request never named a file or document, the substance has been filed where nobody asked and the message points at it — the missing element is that content itself, in the message, and you name it as the gap. Where the request did ask for the file — named it, or asked for work whose product plainly lives in files, like a change to existing material — that split is the CORRECT shape, not a gap: the message carries what was done and the evidence it holds (the answer, the verdict, the numbers, what was run and what came back), never the file's whole contents, and a short message beside an asked-for file convicts nothing by its length.

There is one record that is not partial, and it says so of itself: that the run called no tools and left nothing behind — the whole of it, not a tail. Nothing was looked up, read, computed or checked, so anything the request needed the work to go and find is not in the deliverable and cannot be. Hold the request against that. Where it asked for something only work could produce — figures, sources, the state of something out in the world, a thing built or changed — the gap is that content itself: name what was to be found and never was, in those words, and never as a remark about effort or process. Where the request was answerable from what the worker was already given, an unexercised run is no gap at all and the ordinary reading above decides it.

Where a working method is given, it is the standard this kind of work set for itself before anything was produced, and it is the only standard beside the request itself that you hold the deliverable to. Where it asks for nothing, nothing is missing: a method that names no verification makes an unverified result complete, and a method that names one makes its absence a gap.

A confirmation is the fact of what came back, in the deliverable's own words: what was run, how many passed, what failed, how it ended. When the request asked for a thing to be run and confirmed, that reading satisfies it, and the verbatim transcript of the command is never the gap — demanding the raw output, the exact formatting, or the full terminal text of a check the deliverable already states the result of is a preference of yours, and the honest answer for a preference is pass. Only a request that asked for the output itself — the log, the listing, the exact text — is failed by its absence.

When you name a gap, quote the words of the request it is a failure of — a span of the person's own text, copied exactly as they wrote it, long enough to be unmistakably theirs. Quote the part of what they asked for that is not there. A gap you cannot quote from their request is a preference of yours rather than something they asked for and did not get, and the honest answer for it is pass.

Return exactly one JSON object, nothing else: {"pass": true, "exercised": true or false} or {"pass": false, "gaps": "<the named gaps>", "quote": "<the words of the request this gap fails, copied exactly>"}. "exercised" is a statement about evidence and never about quality: true only when the finished thing was run the way it will actually be used and held — visible in what was run, or reported in the deliverable as what was run and what came back. Everything else is false, including an honest "not verified here" and work that nothing available could have exercised. Both of those still pass; they are simply not evidenced.`

var deliverableSchema = json.RawMessage(`{
  "type": "object",
  "properties": {
    "pass": {"type": "boolean"},
    "gaps": {"type": "string"},
    "quote": {"type": "string"},
    "exercised": {"type": "boolean"}
  },
  "required": ["pass"],
  "additionalProperties": false
}`)

// GateRevisionContract closes every revision, not only the ones whose named gap
// was a missing answer. The revision's own final message replaces the first
// attempt as the node's summary, and a second pass that closes a real gap inside
// a file and then reports that it did so has moved the original failure one
// round along rather than fixing it. The worker was told this once already in
// its own contract; a revision is the moment it demonstrably was not heard.
const GateRevisionContract = "Your final message is the deliverable and the only thing the person will read. " +
	"Put the substance in it — the verdict, the findings, the numbers they asked for — " +
	"and name the files beside that substance, never in place of it. Nothing written in the " +
	"future tense counts: what you would look up or intend to check is a plan, and the person " +
	"is owed the result of carrying it out. " +
	// The one clause that keeps a repaired deliverable from reading as a
	// disputed one. The revision's message REPLACES the first attempt as the
	// node's summary, so anything it says about the review is what the person
	// opens the answer with — and a correct answer introduced by an account of
	// what was wrong with the last one reads as a hedge on itself.
	"Write it as the first and only draft: it replaces the previous attempt entirely. " +
	"Say nothing about the review, the gaps it named, or what you changed — the person is " +
	"reading the work, not its history."

type Judgment struct {
	Pass bool
	Gaps string
	// Quote is the span of the user's own request the gap is a failure of. It
	// is what buys the gap authority over the job: a gate may re-run one leaf on
	// any named gap, but it may only grow the graph for a gap that quotes the
	// ask. See AdmitGapCitation for why that is the whole convergence argument.
	Quote string
	// Exercised is the gate's separate answer about evidence: it saw the
	// finished thing run the way it will be used, and hold. A pass without it
	// is a pass — it is simply not a verified one, and the difference is the
	// whole reason the field exists rather than being read out of the prose.
	Exercised bool
	Checked   bool
}

const GateNotebookBytes = 1 << 10

// GateVerdict is what a passing gate is entitled to record.
//
// The leaf itself never claims a verified success — the general loop has no
// suite it can assume, so it lands as an unverified one however well it went —
// and for a while a gate PASS overwrote that with the strongest verdict there
// is. Nothing had been checked in the sense the verdict means: one judge read
// one final message and found nothing missing from it. That is a success, and
// it is the same success the leaf already reported; only the evidenced form,
// where the finished thing was actually exercised, is more than that.
//
// The two are not interchangeable in exactly one place, which is where the
// distinction is load-bearing: an unverified success is inert in Graded(), so a
// sentence can no longer move a model's ability rating. Everywhere the product
// counts operational success — competence rates, reflex outcomes, the self
// page — both already count, and they still do.
func GateVerdict(judgment Judgment) provider.Verdict {
	if judgment.Exercised {
		return provider.VerdictVerifiedSuccess
	}
	return provider.VerdictUnverifiedSuccess
}

// Evidence is what the gate can hold a claim against: what the leaf
// left behind and the tail of what it actually ran. Both already existed —
// the artifact list is resolved for three other readers a few lines above the
// gate call, and the run tail is recorded by the executor as it goes — so the
// gate stops being a judge of prose for the price of passing two slices.
type Evidence struct {
	Artifacts []string
	Ran       []string
	// Observed says the run was watched from beginning to end, which is the
	// only thing that turns two empty slices into a fact. Without it the gate
	// could not tell "this leaf did nothing" from "nobody was recording", and
	// it was told in the same breath that silence never acquits — so a run that
	// called no tools and answered with a plan read to the judge as an honest
	// answer whose evidence was simply not available, and passed. It is a field
	// rather than an inference because only the caller holding the outcome
	// knows which of the two it has; every real delivery sets it, and the unit
	// tests that construct a bare Evidence deliberately do not.
	Observed bool
}

// gateEvidenceRan bounds what travels. The executor already keeps a short tail;
// this is the second bound, because the gate's own reply budget is small and a
// judge reading a hundred lines of shell before the deliverable is a judge
// reading the wrong thing first.
const gateEvidenceRan = 24

// UnexercisedRecord is what an observed run with nothing in it says for itself.
// It is the one record in this block that is complete rather than a tail, and it
// is written to say so, because everything else the gate is told about these
// records is that they can never acquit.
const UnexercisedRecord = "Nothing. The work called no tools and left nothing behind: it looked nothing up, " +
	"read nothing, ran nothing, wrote nothing. This is the whole record of the run and not a tail of one."

// block renders the evidence, or nothing at all when there is none to show. It
// sits below the deliverable so a rewritten deliverable is still the first byte
// that moves in a repair pass.
//
// "Nothing to show" and "nothing happened" are different answers and this used
// to give the same one to both. An observed run that did nothing now says so in
// words; an unobserved one still renders empty, so a caller with no outcome in
// hand cannot manufacture the strongest record in the block by omission.
func (e Evidence) block() string {
	if len(e.Artifacts) == 0 && len(e.Ran) == 0 {
		if !e.Observed {
			return ""
		}
		return UnexercisedRecord
	}
	var body strings.Builder
	if len(e.Artifacts) > 0 {
		body.WriteString("What the work left behind:\n")
		for _, path := range e.Artifacts {
			if info, err := os.Stat(path); err == nil {
				fmt.Fprintf(&body, "%s (%d bytes)\n", path, info.Size())
				continue
			}
			// A path the deliverable names and the filesystem does not have is
			// the loudest thing in this block, so it is stated rather than
			// dropped for being unreadable.
			fmt.Fprintf(&body, "%s (not on disk)\n", path)
		}
	}
	ran := e.Ran
	if len(ran) > gateEvidenceRan {
		ran = ran[len(ran)-gateEvidenceRan:]
	}
	if len(ran) > 0 {
		if body.Len() > 0 {
			body.WriteString("\n")
		}
		fmt.Fprintf(&body, "The last %d things the work ran, oldest first:\n", len(ran))
		for _, line := range ran {
			body.WriteString(line + "\n")
		}
	}
	return strings.TrimRight(body.String(), "\n")
}

// judgeDeliverable returns a checked pass or named gap. Every failure of the
// gate itself remains fail-open: Checked is false, so it neither blocks delivery
// nor manufactures verified evidence for the profile.
func JudgeDeliverable(ctx context.Context, settings config.Config, client *pool.Client, graph *store.Store, node store.Node, deliverable, method string, evidence Evidence, workerModel string) Judgment {
	ask := node.Provenance.Intent
	// The standing half of the gate comes first and the job in front of it last,
	// which is both the reading order and the billing order. Settled taste is
	// the same text for every job in a session, so leading with it makes it the
	// one block the endpoint can hand back warm; the digest is retrieved per
	// node but identical across a node's repair passes, so it extends that warm
	// stretch through a revision. The request, the goal and the deliverable move
	// with every call and can invalidate nothing but themselves down here.
	//
	// Settled taste still leads the notebook material for the older reason: a
	// rule the user corrected their way to three times is not one lesson among
	// eight — it is the shape of an acceptable answer, and cannot be crowded out
	// by the digest's byte budget.
	var body string
	if taste := resident.TasteBlock(graph); taste != "" {
		body += "Settled taste — hold to these:\n" + taste + "\n\n"
	}
	if digest := resident.NotebookDigest(graph, node.ID, node.Brief, ask, 8); digest != "" {
		body += "Standing preferences and relevant lessons:\n" + clipUTF8Bytes(digest, GateNotebookBytes) + "\n\n"
	}
	body += "Verbatim request:\n" + ask + "\n\nCompiled goal:\n" + node.Brief
	// The working method the worker was actually held to, which is where this
	// kind of work states what done means and how it is checked. It is the only
	// standard the gate is given that was written for the work in front of it,
	// and it is stable across a job's repair passes, so it rides above the
	// deliverable with the rest of the settled half.
	if method = strings.TrimSpace(method); method != "" {
		body += "\n\nThe working method this deliverable was held to:\n" + method
	}
	body += "\n\nDeliverable as produced:\n" + deliverable
	// The records come last, under the deliverable they are used to check: they
	// are the most volatile block in the prompt — a revision rewrites the text
	// and re-runs the work — and the cache pays for volatility by position.
	if records := evidence.block(); records != "" {
		body += "\n\nWhat actually happened, as recorded while it ran:\n" + records
	}
	judgeCtx := settings.Context(router.WithAvoidModel(ctx, workerModel), "gate")
	judgeCtx = provider.WithCall(judgeCtx, provider.ClassPlanAudit)
	// The gate is part of what this deliverable cost, not part of the day's
	// overhead: a job whose bill omits its own review reads as cheaper than it
	// was, and the review is often the second most expensive thing in it.
	judgeCtx = pool.WithSpendNode(judgeCtx, node.ID)
	options := []ai.Option{ai.WithMaxTokens(400)}
	// Structured output is the cascade's free verifier. Keep the no-panel
	// adapter's request options unchanged; there is no second rung to unlock.
	if client.Routed() {
		options = append(options, ai.WithSchema(deliverableSchema))
	}
	response, err := client.CompleteWithMessages(judgeCtx, []ai.Message{
		{Role: "system", Content: []ai.ContentPart{{Type: "text", Text: DeliverablePrompt}}},
		{Role: "user", Content: []ai.ContentPart{{Type: "text", Text: body}}},
	}, options...)
	if err != nil || response == nil {
		provider.Report(judgeCtx, provider.VerdictProviderFailure)
		return Judgment{Pass: true}
	}
	var verdict struct {
		Pass      bool   `json:"pass"`
		Gaps      string `json:"gaps"`
		Quote     string `json:"quote"`
		Exercised bool   `json:"exercised"`
	}
	// One extractor for every structured reply in the system. This used to hold
	// its own — first brace to last brace — which is tolerant in the same
	// direction and wrong in one: a judge that wrote a sentence containing a
	// brace after its object swallowed the sentence into the JSON and failed the
	// parse, and a failed parse here is a silent pass.
	if err := provider.DecodeJSONObject(response.Text(), &verdict); err != nil {
		provider.Report(judgeCtx, provider.VerdictFormatFailure)
		return Judgment{Pass: true}
	}
	if verdict.Pass {
		provider.Report(judgeCtx, provider.VerdictVerifiedSuccess)
		// A judge that omits the field says nothing about evidence, and
		// nothing is the honest reading: the missing answer stays false.
		return Judgment{Pass: true, Exercised: verdict.Exercised, Checked: true}
	}
	gaps := strings.TrimSpace(verdict.Gaps)
	if gaps == "" {
		provider.Report(judgeCtx, provider.VerdictSemanticFailure)
		return Judgment{Pass: true}
	}
	provider.Report(judgeCtx, provider.VerdictVerifiedSuccess)
	// An ungrounded gap is still recorded as a gap: it is said out loud, it
	// rides the delivery, and it is in the ledger. What it does not buy is
	// paid work — neither the revision round nor the extension — and both of
	// those refusals happen at the wiring seam rather than being laundered
	// into a pass here.
	return Judgment{Gaps: gaps, Quote: strings.TrimSpace(verdict.Quote), Checked: true}
}

// The citation invariant: a gate's gap may commission new work only if it
// quotes the ask. This is the whole of why an extending gate cannot spiral, and
// it is worth stating why a string comparison is enough.
//
// The 27-round run was not a failure to terminate — the dollar rail would have
// stopped it eventually. It was a failure to be ABLE to terminate: each round's
// gap was derived from the previous round's own output, so the set of things
// left to fix was unbounded and self-replenishing, and every cap was therefore
// the mechanism rather than the backstop. The fix is to make the set of
// admissible gaps finite and fixed before the first round runs. The user's
// verbatim intent is immutable by construction — the store refuses an empty
// one, never rewrites it, and stamps the same value on every node of every
// splice — so the substrings of that one string are a fixed, finite set. A gap
// must name one of them. Round k+1 must name one no earlier round spent. The
// number of unspent spans falls by at least one per admitted round, so the loop
// terminates on the content of the ask rather than on a counter.
//
// "verification of what the previous round produced" is not a substring of
// anything a person typed, so that round is refused before a planning call is
// made. That is construction rather than policy, and it is the difference
// between a cap that fires and a cap that never has to.
//
// This is a provenance check and not a quality rubric: it says nothing about
// whether the gap is a good one, only that the words it claims to be a failure
// of are the user's own. The residual it does not close is a real span cited
// for an invented requirement — bounded by the round cap, and by the plan's own
// rule that no piece of work may exist to check another's product.
func AdmitGapCitation(intent, quote string, spent []string) string {
	if refusal := admitGapGrounding(quote, intent); refusal != "" {
		return refusal
	}
	quote = citationKey(quote)
	for _, prior := range spent {
		if citationKey(prior) == quote {
			return "the same words were already worked on once"
		}
	}
	return ""
}

// admitGapGrounding is the citation invariant's core, and the one door both
// readers of it go through. A quote is admitted when it is a verbatim span of
// something nobody in this system wrote for itself during the run: the user's
// ask, or the working method this kind of job was held to before anything was
// produced. Everything else — the compiled goal, the working decisions, the
// previous round's own output — is aforge talking to aforge, and a gap that can
// only quote those is a preference rather than a failure.
//
// Whitespace is normalised on both sides and nothing else is: a model that
// re-wraps a quoted line has still quoted it, and a model that invents a
// requirement has still invented it.
func admitGapGrounding(quote string, grounds ...string) string {
	quote = citationKey(quote)
	if quote == "" {
		return "the review could not point at anything in the request that is missing"
	}
	for _, ground := range grounds {
		if ground = citationKey(ground); ground != "" && strings.Contains(ground, quote) {
			return ""
		}
	}
	return "what the review asked for next is not in the request"
}

// AdmitGapRevision applies that same grounding one layer earlier than the
// extension does: to the paid revision round a failed gate buys.
//
// The extension was guarded and the revision was not, and the measured cost of
// that asymmetry is one benchmark cell where the gate held the worker to a
// working decision aforge had invented for itself — "March refers to any
// calendar year present in the data" — bought a five-turn re-run against it,
// and got back a worse deliverable than the one it rejected. A round bought on
// a self-authored standard cannot converge on anything, because the standard
// moves with each round that is written against it.
//
// The working method is admitted as a second ground because it is the one
// standard besides the ask that was fixed before the work started and that the
// worker was actually held to. It is not self-authored in the sense that
// matters: it does not move in response to what the work produced.
//
// A refusal is not a pass. The gap is journaled, it is said in the thread, and
// it rides the delivery — it simply does not redo the work.
func AdmitGapRevision(intent, method, quote string) string {
	return admitGapGrounding(quote, intent, method)
}

// GapNote is what an ungrounded gap gets instead of a round: the reviewer's
// words, said plainly, with the honest reason nothing was redone over them. It
// is the same register as GapHandover and deliberately not the same sentence —
// a reservation says the work fell short, and this says the review did.
func GapNote(gaps, refusal string) string {
	return "a review raised this: " + firstLine(gaps) +
		" — I've delivered as it stands, because " + refusal +
		", and I don't redo work over a standard the request never set. Say the word and I will."
}

func citationKey(text string) string { return strings.Join(strings.Fields(text), " ") }

// Extension is what a gate's judgement was allowed to do about a gap that
// survived the revision pass: the work it commissioned, the words it cited, the
// round it was, and — when nothing was commissioned — why, in the words the user
// would be told.
type Extension struct {
	Spliced int
	Quote   string
	Round   int
	Refused string
}

// GapContinuationNotice is the whole of what a person sees when a judgement
// grows the job: one line, in the same calm register as the governor's, saying
// what is missing and that it is being finished rather than delivered around.
// No new noun is introduced — the user never learns that any of this has a name.
func GapContinuationNotice(gaps string) string {
	return "a review found this still missing: " + firstLine(gaps) + " — finishing that before delivering"
}

// GapHandover is what the delivery carries when nothing more will run. It names
// the gap in the system's own words and says why it stopped, because the next
// thing the person says about it is the correction path's input and a handover
// they cannot see is a handover that never happened.
func GapHandover(gaps string, revised bool, refused string) string {
	handover := "I'm handing this over with a reservation — a review found this still missing: " + firstLine(gaps) + "."
	if !revised {
		handover += " The revision pass came back empty, so this is the first draft."
	}
	if refused != "" {
		handover += " I've taken it as far as repair takes it: " + refused + "."
	}
	return handover
}

// ExtendForGap is the authority the delivery gate never had.
//
// The judgement at the job root was already the right one and its maximum power
// was to re-run the same leaf once and then ship regardless; meanwhile the only
// mechanism that can grow a live job fires on running out of money and never on
// being wrong. Quality failure and resource failure were handled by two disjoint
// mechanisms and only the resource one could add work. This is the wire between
// them, and it is short because ReplanOverrun already handles everything hard:
// the round counter is read off id arithmetic, the daily rail defers and resumes,
// the job-size ceiling and the round cap post their own notices, and a repair on
// a top-level job continues as a top-level job that will be announced like any
// other deliverable.
//
// What arrives here is a named gap, so the replan is aimed at a remainder a
// reviewer found rather than at whatever sounds like more work — and the goal it
// is planned from forbids inventing verification, as the plan's own proportion
// rule forbids a node whose purpose is to check another's product. Assurance may
// add work that closes a gap; it may never add work that checks one.
func ExtendForGap(ctx context.Context, graph *store.Store, node store.Node, partial string,
	unmet Judgment, artifacts []string, dailyBudgetUSD float64,
	planRemainder resident.OverrunPlanFunc) Extension {
	base, round := resident.OverrunLineage(node.ID)
	extension := Extension{Quote: strings.TrimSpace(unmet.Quote), Round: round + 1}
	if graph == nil || planRemainder == nil {
		extension.Refused = "there is nothing here that could plan the rest"
		return extension
	}
	// Admissibility is decided before any planning call: an ungrounded gap must
	// cost nothing at all, or the refusal is only a refusal to splice what has
	// already been bought.
	if refusal := AdmitGapCitation(node.Provenance.Intent, extension.Quote, SpentCitations(graph, base)); refusal != "" {
		extension.Refused = refusal
		return extension
	}
	spliced, _, err := resident.ReplanOverrun(ctx, graph, node, partial, unmet.Gaps, artifacts, dailyBudgetUSD, planRemainder)
	if err != nil {
		log.Printf("note: could not plan the rest of %s: %v", node.ID, err)
		extension.Refused = "the work that would close it could not be planned"
		return extension
	}
	if spliced == 0 {
		// A governor has already said so in the thread in its own words, or the
		// rail has journaled the repair and is waiting on consent. Either way
		// nothing new is running and the delivery has to say so.
		extension.Refused = "no more work could be started on it"
		return extension
	}
	extension.Spliced = spliced
	return extension
}

// SpentCitations is the ledger: the spans of the ask that earlier rounds of this
// job already commissioned work against. A read failure returns nothing, which
// is the fail-safe direction for a bound on new work only in company with the
// round cap — which is exactly what that cap is for.
func SpentCitations(graph *store.Store, baseID string) []string {
	if graph == nil {
		return nil
	}
	gates, err := graph.DeliveryGateLineage(baseID)
	if err != nil {
		log.Printf("note: could not read the gap ledger for %s: %v", baseID, err)
		return nil
	}
	var spent []string
	for _, gate := range gates {
		if gate.Extended && strings.TrimSpace(gate.Quote) != "" {
			spent = append(spent, gate.Quote)
		}
	}
	return spent
}

// ── who takes the next attempt ───────────────────────────────────────────────
//
// Two judgements in this file already read a leaf that did not get there: the
// one that decides whether an exhausted leaf left work behind, and — from this
// wave — the one that decides who retries a failed one. Both used to answer with
// a stronger model or a continuation and nothing else, because a stronger model
// was the only other place a leaf could go.
//
// The menu is injected into both rather than a third mechanism being built,
// because there is no third question. "This failed; what now" already has a
// judge; what changes is that the answer may name a different kind of worker.
// And the menu is the registry's, so a build with only the generalist renders
// nothing, no prompt gains a byte, and no call is made that was not made before.
//
// The headless scheduler's escalation (internal/exec/schedule.go's Escalations)
// is deliberately left mechanical. Nothing judges there: a verdict that says
// "a stronger model might fix this" puts the node back to pending and the
// ordinary launch path picks it up, and there is no model in that loop to hand a
// menu to. Adding one would be a second dispatch policy in the surface that has
// no conversation to explain itself in — the two-surface covenant says every
// worker is REACHABLE from both surfaces, which it is, not that every judgement
// is made on both. Retries are judged where retries are judged: on the surface
// with a head. A headless run reaches a specialist the way it always has, by the
// planner choosing one at sizing time.

// WorkerChoiceBrief renders the menu into a judgement that may name a worker,
// or nothing at all when there is nothing to choose between.
func WorkerChoiceBrief(menu string) string {
	if strings.TrimSpace(menu) == "" {
		return ""
	}
	return "\n\n" + menu + "\nReturn the choice as \"worker\":\"<name>\". " +
		"Omit it, or leave it empty, for the default worker."
}

// retryWorkerPrompt asks whether the second attempt should go somewhere
// different in kind, not merely somewhere stronger.
//
// The default answer is stated as the default and the specialist as the
// exception, in the compiler's own words, because this is the same choice the
// compiler makes and a leaf that reaches here has already been judged once. The
// difference is the evidence: a failure is in front of this judge and was not in
// front of that one. What must not follow from that evidence is "it failed, so
// try something else" — most failures are answered by a stronger model doing the
// same thing, and a judge that reads failure as a reason to change worker would
// route every hard prose leaf into a coding pipeline.
const retryWorkerPrompt = `A worker was given one assignment, worked on it, and did not finish it. The same assignment is about to be attempted once more.

By default that second attempt goes to the same kind of worker, on a stronger model. You decide one thing only: whether the essence of this assignment is the thing a specialist below exists for, in which case the attempt goes to that specialist instead.

The failure itself is not a reason to change the kind of worker. Most work that fails once is finished by the same kind of worker trying again with more capacity behind it. Change the kind only when the assignment's essence — what the work fundamentally is — matches a specialist's purpose, in which case the first attempt was on the wrong sort of worker from the beginning.

Return exactly one JSON object, nothing else.`

var retryWorkerSchema = json.RawMessage(`{
  "type": "object",
  "properties": {
    "worker": {"type": "string"}
  },
  "required": ["worker"],
  "additionalProperties": false
}`)

// judgeRetryWorker names the worker for one retry, or nothing for the default.
//
// Everything about it fails toward the behavior that existed before it did: no
// menu means no call at all, an unparseable answer means the default worker, and
// a name that reaches no registered worker means the default worker. The retry
// happens either way — this decides who gets it, never whether there is one.
func JudgeRetryWorker(ctx context.Context, settings config.Config, client *pool.Client,
	node store.Node, task exec.Task, outcome *exec.Outcome, failure error,
	menu, workerModel string) string {
	if strings.TrimSpace(menu) == "" || client == nil {
		return ""
	}
	var body strings.Builder
	body.WriteString("The assignment:\n" + task.Brief)
	if produced := strings.TrimSpace(outcome.Text); produced != "" {
		body.WriteString("\n\nWhat the first attempt had produced when it stopped:\n" + boundedDelivery(produced))
	}
	if failure != nil {
		body.WriteString("\n\nHow it ended: " + firstLine(failure.Error()))
	} else {
		body.WriteString("\n\nHow it ended: " + string(outcome.Verdict))
	}
	judgeCtx := settings.Context(router.WithAvoidModel(ctx, workerModel), "retry-worker")
	judgeCtx = provider.WithCall(judgeCtx, provider.ClassPlanAudit)
	judgeCtx = pool.WithSpendNode(judgeCtx, node.ID)
	options := []ai.Option{ai.WithMaxTokens(200)}
	if client.Routed() {
		options = append(options, ai.WithSchema(retryWorkerSchema))
	}
	// The menu rides the end of the user message, not the system one. It reads
	// as law — here are the workers, here is how to choose between them — but it
	// carries each specialist's measured line, and those are run counts and a
	// four-decimal average cost that move every time a leaf of that worker
	// finishes. Sent as part of the system message it rewrote, mid-session, the
	// one string in this call that could have been identical from job to job.
	// Position by volatility: what churns sinks (12.4.1, and the same fix
	// internal/head/compiler.go took for the same block).
	response, err := client.CompleteWithMessages(judgeCtx, []ai.Message{
		{Role: "system", Content: []ai.ContentPart{{Type: "text", Text: retryWorkerPrompt}}},
		{Role: "user", Content: []ai.ContentPart{{Type: "text",
			Text: body.String() + WorkerChoiceBrief(menu)}}},
	}, options...)
	if err != nil || response == nil {
		provider.Report(judgeCtx, provider.VerdictProviderFailure)
		return ""
	}
	chosen := DecodeWorkerChoice(response.Text())
	if chosen == "" {
		// Not a failure: "the default worker" is the answer this judge gives
		// most of the time and the one it is told to give when in doubt.
		provider.Report(judgeCtx, provider.VerdictVerifiedSuccess)
		return ""
	}
	provider.Report(judgeCtx, provider.VerdictVerifiedSuccess)
	return chosen
}

// DecodeWorkerChoice reads a worker out of a judgement's reply, keeping only a
// name that reaches a worker this build can actually construct. It is the same
// degradation head.Compiler.normalizeSubharness makes on the compile path, for
// the same reason: a hallucinated worker costs a retry its specialist and
// nothing else.
func DecodeWorkerChoice(text string) string {
	text = strings.TrimSpace(text)
	start, end := strings.Index(text, "{"), strings.LastIndex(text, "}")
	if start < 0 || end <= start {
		return ""
	}
	var reply struct {
		Worker string `json:"worker"`
	}
	if json.Unmarshal([]byte(text[start:end+1]), &reply) != nil {
		return ""
	}
	chosen := strings.TrimSpace(reply.Worker)
	if !exec.KnownSubharness(chosen) {
		return ""
	}
	return chosen
}

// remainderPrompt asks the one question the overrun path used to assume
// an answer to. Running out of budget while landing a finished result is
// common — the executor grants a landing reserve for exactly that — so
// exhaustion is treated as a fact about resources, never as evidence of
// unfinished work. The judgment is against the leaf's own brief, not the
// job's intent: a mid-graph leaf that inventoried a folder is done when the
// inventory is done, even though the job it serves is not.
const remainderPrompt = `A worker ran out of resources while working on one assignment and stopped. You decide whether anything is actually left to do.

You receive the assignment and what the worker had produced when it stopped. Judge exactly one question: does the produced result already fulfill the assignment? Running out of budget while landing a finished result is common — exhaustion is not evidence of incompleteness. Judge only the substance against the assignment.

Return exactly one JSON object, nothing else:
{"done": true} when the assignment is fulfilled and a consumer could use this result as-is.
{"done": false, "remaining": "<the unfinished work>"} only when you can name a specific element of the assignment that is absent or unfinished — concretely enough that a worker could finish from your words alone. Work the assignment never asked for is never remaining work: do not prescribe verification, re-verification, or review of what already exists.`

var remainderSchema = json.RawMessage(`{
  "type": "object",
  "properties": {
    "done": {"type": "boolean"},
    "remaining": {"type": "string"}
  },
  "required": ["done"],
  "additionalProperties": false
}`)

type Remainder struct {
	Done      bool
	Remaining string
	Checked   bool
	// Worker is the specialist the same judge named for the work that is left,
	// when it named one. Empty is the default worker and is the answer in every
	// build without a specialist, because the question is never asked there.
	Worker string
}

// remainderWorkerSchema is the remainder schema with the worker field.
// Two constants rather than one built at runtime: a prompt's schema is part of
// the prompt, and the baseline one has to be readable as the thing that has not
// changed.
var remainderWorkerSchema = json.RawMessage(`{
  "type": "object",
  "properties": {
    "done": {"type": "boolean"},
    "remaining": {"type": "string"},
    "worker": {"type": "string"}
  },
  "required": ["done"],
  "additionalProperties": false
}`)

// judgeRemainder decides whether an exhausted leaf actually left work behind.
// Failures fail toward "not done" with Checked false: the continuation still
// runs, now bounded by the overrun governors, rather than a judge outage
// silently shipping genuinely cut-off work as finished.
// The menu is the caller's: it is the registry's text with the leaf's own
// worker excluded — a continuation of work this worker ran out of resources on
// belongs with it by default and needs no naming, and the interesting answer is
// the other one. Who was promised this leaf is read by the dispatch path, which
// is the one owner of that question, so it arrives here already answered.
func JudgeRemainder(ctx context.Context, settings config.Config, client *pool.Client, graph *store.Store, node store.Node, produced, menu, workerModel string) Remainder {
	body := "The assignment:\n" + node.Brief + "\n\nProduced before stopping:\n" + produced
	judgeCtx := settings.Context(router.WithAvoidModel(ctx, workerModel), "remainder")
	judgeCtx = provider.WithCall(judgeCtx, provider.ClassPlanAudit)
	// Like the delivery gate, the judgment is part of what this leaf cost.
	judgeCtx = pool.WithSpendNode(judgeCtx, node.ID)
	options := []ai.Option{ai.WithMaxTokens(400)}
	if client.Routed() {
		schema := remainderSchema
		if menu != "" {
			schema = remainderWorkerSchema
		}
		options = append(options, ai.WithSchema(schema))
	}
	// The menu sits at the end of the user message for the reason the retry
	// judgement's does: it is measured, it moves within a session, and the
	// system prompt above it is a constant this build never rewrites.
	response, err := client.CompleteWithMessages(judgeCtx, []ai.Message{
		{Role: "system", Content: []ai.ContentPart{{Type: "text", Text: remainderPrompt}}},
		{Role: "user", Content: []ai.ContentPart{{Type: "text", Text: body + WorkerChoiceBrief(menu)}}},
	}, options...)
	if err != nil || response == nil {
		provider.Report(judgeCtx, provider.VerdictProviderFailure)
		return Remainder{}
	}
	text := strings.TrimSpace(response.Text())
	start, end := strings.Index(text, "{"), strings.LastIndex(text, "}")
	if start < 0 || end <= start {
		provider.Report(judgeCtx, provider.VerdictFormatFailure)
		return Remainder{}
	}
	var verdict struct {
		Done      bool   `json:"done"`
		Remaining string `json:"remaining"`
	}
	if err := json.Unmarshal([]byte(text[start:end+1]), &verdict); err != nil {
		provider.Report(judgeCtx, provider.VerdictFormatFailure)
		return Remainder{}
	}
	remaining := strings.TrimSpace(verdict.Remaining)
	if !verdict.Done && remaining == "" {
		// A "not done" that cannot name the gap is the exact failure the old
		// path had: a remainder assumed rather than found. Unchecked, so the
		// replan proceeds on the partial alone.
		provider.Report(judgeCtx, provider.VerdictSemanticFailure)
		return Remainder{}
	}
	provider.Report(judgeCtx, provider.VerdictVerifiedSuccess)
	return Remainder{Done: verdict.Done, Remaining: remaining, Checked: true,
		Worker: DecodeWorkerChoice(text)}
}

// deliveryPartialBytes bounds the partial handed to a judgement. It is the
// same bound the delivery path uses: what a message can carry, less the room a
// sentence is posted with.
const deliveryPartialBytes = store.MaxMessageBytes - 1<<10

func boundedDelivery(text string) string {
	return clipUTF8Bytes(strings.TrimSpace(text), deliveryPartialBytes)
}

func nodeDisplay(node store.Node) string {
	if title := strings.TrimSpace(node.Title); title != "" {
		return title
	}
	return firstLine(node.Brief)
}

func firstLine(text string) string {
	if index := strings.IndexByte(text, '\n'); index >= 0 {
		return text[:index]
	}
	return text
}

func clipUTF8Bytes(value string, limit int) string {
	if limit <= 3 || len(value) <= limit {
		return value
	}
	cut := limit - 3
	for cut > 0 && !utf8.ValidString(value[:cut]) {
		cut--
	}
	return strings.TrimSpace(value[:cut]) + "..."
}
