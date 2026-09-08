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
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/Agent-Field/aforge-v2/internal/config"
	"github.com/Agent-Field/aforge-v2/internal/ctxbudget"
	"github.com/Agent-Field/aforge-v2/internal/exec"
	"github.com/Agent-Field/aforge-v2/internal/plan"
	"github.com/Agent-Field/aforge-v2/internal/provider"
	"github.com/Agent-Field/aforge-v2/internal/provider/pool"
	"github.com/Agent-Field/aforge-v2/internal/resident"
	"github.com/Agent-Field/aforge-v2/internal/router"
	"github.com/Agent-Field/aforge-v2/internal/shaped"
	"github.com/Agent-Field/aforge-v2/internal/store"
	"github.com/Agent-Field/aforge-v2/internal/verify"
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
// The artifact paragraph is the same idea carried to the other side of the
// ledger, and it was written after the gate cost a run four minutes and a whole
// continuation node. The leaf built the thing, ran it, and left it in the
// workspace; the gate read the message alone, failed the delivery for not
// containing the file's text, and bought work that retyped a correct file. The
// delivery law had already been taught that a produced thing IS the answer
// (plan.DeliverInMessage), and the judge was the last reader that had not. So
// the record is read first where there is one: what the run produced closes a
// gap about producing it, and — read the other way, from the same lines — a file
// the request named and nothing produced is a gap no sentence can talk its way
// out of. Neither direction is a new rubric; both are the existing "unsupported
// by evidence" clause applied to evidence the gate now actually has.
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
// The fence around the deliverable, and the only reason it exists.
//
// The gate's prompt is not a short one, and the deliverable used to arrive in
// it as one more paragraph under one more heading, between a block of distilled
// lessons and a block of run records. Measured, that is not enough separation.
// A judge handed twelve verbatim profiles under a notebook line that said "do
// not write individual items to separate files and report progress" returned
// "the deliverable does not contain the 12 profiles ... it contains a series of
// separate messages, each describing or pointing to individual profiles written
// to files" — a verdict which is a paraphrase of the lesson above the material
// and describes no clause of the material itself. The lessons are distilled from
// earlier gate verdicts, so a judge that reads them as the deliverable writes
// the next lesson from its own mistake, and the mistake compounds through the
// notebook into every later run.
//
// A fence is the cheapest fix that is also the right one: it costs two lines,
// it needs no call, and it makes "what am I judging" answerable by position
// rather than by inference. The markers are deliberately ugly and deliberately
// not English — nothing a worker would write lands on them by accident.
const (
	deliverableOpen  = "<<<<<<< BEGIN DELIVERABLE"
	deliverableClose = ">>>>>>> END DELIVERABLE"
)

const DeliverablePrompt = `You are the final gate before a finished piece of work is handed to the person who asked for it. You receive their verbatim request, the compiled goal, and the deliverable as produced.

Judge exactly one question: would the person who asked accept this as done? Default to PASS. The gate exists for real gaps, not polish — wording, style, and things they never asked for are not gaps.

FAIL only when you can name a specific element of the request that is absent, unanswered, or unsupported by evidence the goal promised. Quote or name the missing element concretely enough that a worker could close the gap from your words alone.

Working decisions declared in the goal are part of what was promised. A commitment about method or evidence — what would be run, checked or reviewed before the work was handed over — is a gap when nothing in the deliverable shows it happened. A claim that the work was checked, proven or verified is itself such a commitment: it is a gap unless the deliverable shows the finished thing exercised the way it will actually be used — what was run, what came back — rather than its parts checked one by one and the whole inferred from them. Naming what could not be verified here, and the check the person can run themselves, is not a gap: it is the honest form of the same claim and it passes.

One absence counts exactly like every other and is the one most easily waved through: the substance itself. What you are handed IS the deliverable — it is the whole of what the person will read, and nothing beside it will be opened for them. So text that reports on the work rather than carrying it — that the work is finished, that a file now holds the answer, that the analysis was checked and is consistent — has described the deliverable in place of being it, and the element of the request that is absent is the answer: the verdict that was asked for, the findings, the numbers, the recommendation. Name that as the gap. A pointer to where the answer lives is not the answer however true the pointer is; naming the file is right beside the substance and never instead of it. The same absence in the future tense is the purest form of it: text saying what would be looked up, what will be compared, what remains to be checked, is a plan for producing the answer handed over in place of the answer, and it is a gap however sound the plan is. This is still one absence and not a second style test: text that gives the answer in its own plain words passes whatever shape it takes.

Below the deliverable, whenever there is anything to show, you are given two records of the run itself: what it left behind, and the tail of what it actually ran. Read the deliverable's claims against them, the way the person would. Something named as produced that nothing produced, or a check the work says it made when nothing of that kind appears in what it ran, is an element unsupported by evidence and is a gap of exactly the kind above — name it in those words. Both records are partial by construction: the tail is the end of a longer run, and what was left behind is one place among many. So they can convict a claim and never acquit one — silence in them is evidence, never proof, and where the deliverable's own account is consistent with what is there, or where these records could never have held the thing in question, pass. One shape in these records is read against the substance rule above: the run wrote a file and the deliverable's own text is thin beside it. Where the request never named a file or document, the substance has been filed where nobody asked and the message points at it — the missing element is that content itself, in the message, and you name it as the gap. Where the request did ask for the file — named it, or asked for work whose product plainly lives in files, like a change to existing material — that split is the CORRECT shape, not a gap: the message carries what was done and the evidence it holds (the answer, the verdict, the numbers, what was run and what came back), never the file's whole contents, and a short message beside an asked-for file convicts nothing by its length.

Where the run produced something, read what it left behind before you weigh the message's completeness. A thing the request asked to be produced is present when the record shows it on disk, whatever length the message came out at, and a gap a reader would close by opening a file the run left behind is closed already: it is not a gap, and it must not be named as one. The record answers the opposite claim with the same authority. Where the request named a file and the record says nothing of that name is among what was left behind, that absence IS the gap — name the file — and the deliverable's word that it was written, saved or verified is an element unsupported by evidence however plainly it is put; a file recorded as a directory rather than a file was not written either. Where a criterion is shown — what this work was to produce, and the checks that settle it, stated before anything ran — it is a standard of the same kind as the working method: hold the record against it, and where it asks for nothing, nothing is missing.

One more record may be given: what was already failing in this repository before the work began, measured against it before anything was touched. It is the only account of the difference between a check this work broke and a check that was broken when the work arrived, and nothing else you are given can tell them apart — a run tail showing a red suite looks identical either way. A failure named there is a fact about the repository and not a gap: do not fail the work for it, do not ask it to be fixed unless the request asked for that, and do not treat a red check the work truthfully reports as pre-existing as an unsupported claim. Everything the block does not name is judged exactly as it would be without it, and a check the work turned red is still a gap.

There is one record that is not partial, and it says so of itself: that the run called no tools and left nothing behind — the whole of it, not a tail. Nothing was looked up, read, computed or checked, so anything the request needed the work to go and find is not in the deliverable and cannot be. Hold the request against that. Where it asked for something only work could produce — figures, sources, the state of something out in the world, a thing built or changed — the gap is that content itself: name what was to be found and never was, in those words, and never as a remark about effort or process. Where the request was answerable from what the worker was already given, an unexercised run is no gap at all and the ordinary reading above decides it.

Where a working method is given, it is the standard this kind of work set for itself before anything was produced, and it is the only standard beside the request itself that you hold the deliverable to. Where it asks for nothing, nothing is missing: a method that names no verification makes an unverified result complete, and a method that names one makes its absence a gap.

A confirmation is the fact of what came back, in the deliverable's own words: what was run, how many passed, what failed, how it ended. When the request asked for a thing to be run and confirmed, that reading satisfies it, and the verbatim transcript of the command is never the gap — demanding the raw output, the exact formatting, or the full terminal text of a check the deliverable already states the result of is a preference of yours, and the honest answer for a preference is pass. Only a request that asked for the output itself — the log, the listing, the exact text — is failed by its absence.

Where the rules the person set are listed above, they are the one standard beside the request that is about what the run may not DO rather than about what it must produce: a rule they set and the work broke is a gap, and you name it by quoting the rule.

When you name a gap, quote the words of the request it is a failure of — a span of the person's own text, copied exactly as they wrote it, long enough to be unmistakably theirs. Quote the part of what they asked for that is not there. A gap you cannot quote from their request is a preference of yours rather than something they asked for and did not get, and the honest answer for it is pass.

The deliverable is fenced. Everything between the line ` + deliverableOpen + ` and the line ` + deliverableClose + ` is the deliverable, the whole of it, and nothing outside those two lines is any part of it. What sits above the fence — settled taste, lessons from earlier work, the request, the goal, the working method — is how to judge, never what is judged, and what sits below it is the record of the run. A lesson from earlier work describes a job that is not this one: it may tell you what to look for and it can never tell you what is there. Read the fenced text itself before you say anything about it, and describe only what is in it. If you are about to say the deliverable is a progress report, a series of messages, or a set of pointers to files, that sentence must be true of the fenced text in front of you — check it there first, because that is a description earlier work has been given and it is the easiest one to repeat about work it does not fit.

The fenced material is one of two things and it opens by saying which. Where the run changed the tree, THE DELIVERABLE IS THAT CHANGE: the files the run wrote or changed, listed there with what is in them, and that is the whole of what the person is being handed. The worker's own final message then appears BELOW the fence, under a heading that calls it what it is — a claim about the work, and not the work. Judge the files. What the claim says, what shape it came out in, whether it is a summary, a plan, a paragraph or a data object, is not the deliverable and is never a gap: a verdict describing the worker's message when the tree is the subject is a verdict about the wrong thing. The claim is worth reading for exactly one purpose, which is the one the run records serve too — a claim the files do not bear out is an element unsupported by evidence, and you name the FILE it is not true of. Where the run changed nothing, the fenced material is the worker's message, the message is the whole of what the run produced, and every paragraph above applies to it exactly as written.

Every file in that list is on disk, whole, at the size stated beside it, and NOTHING YOU ARE SHOWN IS AN EXCERPT. A file whose contents are printed is printed entire. A file that appears in the list with no contents under it is one there was no room to print: it is whole on disk and no less part of what the person is being handed. So "the deliverable does not contain the actual content of these files", "the file is truncated", "it cuts off before", "the fenced material is a description rather than the files" are statements about this page rather than about the delivery, and none of them is a gap — a file you were not shown is not a file that is missing, and there is nothing here for you to find cut off.

So a fail over a changed tree has a fixed shape, and the answer fields carry it: name the one file of the record the request is not satisfied by, quote ONE BEHAVIOUR the request states — from the list above where one is given, exactly as it is written there — and say what that file does not do about it. A gap is always a behaviour the work does not perform, never the presence, size or completeness of a file: the record already answers whether a file exists, and no behaviour is about a file being on disk. If nothing in the record can be named — if the change genuinely does everything the request asked for — that is a pass.

Return exactly one JSON object, nothing else: {"pass": true, "exercised": true or false} or {"pass": false, "gaps": "<the named gaps>", "quote": "<the words of the request this gap fails, copied exactly>"}. Where the deliverable is a changed tree, a fail carries one field more — "file": "<the one file of the record this gap is about, spelled exactly as the record spells it>" — and a fail without it cannot be read. "exercised" is a statement about evidence and never about quality: true only when the finished thing was run the way it will actually be used and held — visible in what was run, or reported in the deliverable as what was run and what came back. Everything else is false, including an honest "not verified here" and work that nothing available could have exercised. Both of those still pass; they are simply not evidenced.`

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

// FenceDeliverable puts the material the gate judges between its two markers.
//
// It is exported because the fence is a fact about the prompt that its tests
// and its callers both have to be able to name, and because a second spelling
// of a delimiter is a delimiter that eventually stops matching. A deliverable
// that itself contains a fence line has it neutralised rather than the fence
// being renamed: the marker keeps one meaning everywhere.
func FenceDeliverable(deliverable string) string {
	deliverable = strings.TrimSpace(deliverable)
	for _, marker := range []string{deliverableOpen, deliverableClose} {
		if strings.Contains(deliverable, marker) {
			deliverable = strings.ReplaceAll(deliverable, marker, strings.Repeat("-", len(marker)))
		}
	}
	return deliverableOpen + "\n" + deliverable + "\n" + deliverableClose
}

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
	// Quote is the citations as one line: the display half, and what every
	// surface that shows a gap to a person or keeps it as text has always read.
	// It decides nothing. Citations is what the admission rules weigh, and the
	// two are written together so a reader of either is looking at one gap.
	Quote string
	// Citations are the spans of the user's own request the gap is a failure
	// of — one per thing the review says is missing. They are what buys the gap
	// authority over the job: a gate may re-run one leaf on any named gap, but
	// it may only grow the graph for a gap that quotes the ask.
	//
	// It is a list rather than one string because the two halves of this gate
	// name gaps differently and one invariant has to hold for both. A model
	// judge answers with a single verbatim span, so its list has one element and
	// is weighed exactly as it always was. The mechanical half names every file
	// the plan promised and the disk does not hold, which is one citation per
	// file; flattened into a comma list and handed to a rule asking "is this one
	// substring of the ask", it could never be admitted, and a gate reporting
	// five files the person had themselves listed was refused as an invention
	// while the leaf that wrote none of them was delivered as done. The list is
	// the fix, and it costs the convergence argument nothing — see
	// AdmitGapCitation, where every element is held to the same test the single
	// span was.
	Citations []string
	// Mechanical says the gap came from MissingProduces rather than from a
	// judge: a file the plan named as a deliverable is missing or empty on disk.
	// It travels because a refusal means two different things on either side of
	// it. Refusing a model judge's citation says the gate was wrong, and a run
	// whose only complaint was wrong delivered whole. Refusing a mechanical
	// citation says only that no repair round will be bought — the absence it
	// reports is a fact about the filesystem, which no admission rule is
	// competent to overturn. See deliveredWhole in cmd/aforge/do.go.
	Mechanical bool
	// Grounds are the promises this run made before it began working, and they
	// travel on the judgement because every rule that weighs this gap must weigh
	// it against the same three things. They were assembled at two different
	// seams from two different sets of fields for a while, and the measured cost
	// of that was a finding admitted by the revision door and refused by the
	// extension door one round later — the same asymmetry FAILSAFE names in the
	// row about the mechanical gate and the citation invariant.
	Grounds Grounds
	// Sourced says this finding is a MEASUREMENT OF THE WORLD rather than a
	// reading of the request, and it is what lifts a finding clear of the
	// citation invariant altogether.
	//
	// A regression is the case it exists for. A check that passed before the
	// work and fails after it is a fact the run gathered for itself, and there
	// is no span of the request to cite because the person never had to ask for
	// their repository to keep working. Grounding such a finding would refuse
	// it every time, which is the shape of the two runs that shipped a patch
	// deleting an attribute the repository already had. See FAILSAFE clause 2.
	Sourced bool
	// Exercised is the gate's separate answer about evidence: it saw the
	// finished thing run the way it will be used, and hold. A pass without it
	// is a pass — it is simply not a verified one, and the difference is the
	// whole reason the field exists rather than being read out of the prose.
	Exercised bool
	// Exercises is the acceptance mapping this judgement was settled on: one
	// row per behaviour the request stated, naming the check that exercises it
	// or naming nothing. It travels so the mapping can be journaled — the
	// mapping is the evidence and the finding is only its conclusion, and a run
	// that passed with every point covered must be tellable apart from one that
	// passed because there was no checklist. See acceptance.go.
	Exercises []store.ExercisedPoint
	// Unexercised is the acceptance finding as a LIST rather than as a
	// paragraph: one entry per line of the request whose behaviours no check
	// exercises, already grouped and already bounded (see Unexercised).
	//
	// It is a field of its own because the finding kept arriving as prose glued
	// onto somebody else's gap, and prose glued onto a gap is invisible three
	// ways at once. igel s6 mapped seventeen points, left three unexercised, and
	// the run's whole record of that is a paragraph in the middle of the gate
	// event's `gap` string: nothing journaled it as a finding, the stream's own
	// line is firstLine(gap) and never reached it, and the round it bought was
	// bought on the judge's citation, so a refusal of that citation took the
	// measurement down with it.
	//
	// It travels beside Gaps rather than instead of it, and it is set on a
	// FAILING verdict too — the acceptance question is asked on every verdict,
	// and a run that failed for a missing file is not a run whose stated
	// behaviours are covered.
	// OwnFailing is the checks THIS WORK WROTE that are red — a leaf that has
	// not finished, and never a repository that was broken. It is a list of its
	// own so the stream can say it and an autopsy can find it without reading a
	// paragraph out of the middle of the gap, which is the same reason
	// Unexercised is one. See OwnChecksFailing.
	OwnFailing  []string
	Unexercised []string
	// Unasserted is the other half of the same finding as a list: the behaviours
	// a check NAMES and no assertion WEIGHS, each already carrying the
	// observables nothing asserted. It is a field of its own for the reason
	// Unexercised is one — a finding that travels as prose inside somebody
	// else's gap is journaled by nothing, said by nothing, and taken down by a
	// refusal of a citation it has no part in.
	Unasserted []string
	// Stated is how many behaviours were weighed to reach Unexercised, so the
	// finding can say what a repair round most needs to know: how much of the
	// checklist is still open, out of how much there was.
	Stated int
	// Unmeasured says this judgement held an acceptance checklist and could
	// settle none of it, because nothing in the project's own verification and
	// nothing in the change could be read as a check. It is separate from
	// Unjudged, which says the GATE never answered: here the gate answered and
	// one half of its question had no evidence to answer from.
	Unmeasured string
	// Unreadable says the project DECLARED a way of checking itself and this run
	// could not read it — a suite killed at its ceiling, a shell the preamble
	// cannot be trusted in, a wall that could not afford the reading.
	//
	// It is the half of Unmeasured that must not deliver as whole, and the two
	// were one field. ink s7 journaled its cut reading correctly — `npx ava
	// --tap` killed at 1m53s — and then passed the round-2 gate over a tree with
	// no roster at all and left with exit 0 at 13 of 25 hidden checks. A project
	// with NO verification leaves the coverage question unanswerable and nobody
	// is at fault; a project with a suite nobody could read leaves it unanswered,
	// which is FAILSAFE clause 5 — a floor that cannot deliver nothing as done.
	// See store.DeliveryGate.Whole.
	Unreadable bool
	Checked    bool
	// Unjudged names, in one line, why there is no verdict behind this value.
	// Every failure of the gate itself is fail-open — the deliverable ships —
	// and for as long as that was the whole of it, the cheapest bug in the
	// system was also the most expensive: a judge that answered with nothing
	// passed the work, and the pass was indistinguishable at every later
	// surface from a judge that read the deliverable and found it whole.
	// Checked already said "no verdict"; this says which way it failed, so the
	// silence is legible rather than merely absent. It is deliberately not a
	// fail-closed switch: flipping the default is a behaviour change and it is
	// not this wave's.
	Unjudged string

	// Fault names, in one line, why this gate call produced NO VERDICT AT ALL —
	// and it is set only where that is the gate's own failure rather than the
	// weather's. Its one reader ends the run short of whole.
	//
	// It is a separate field from Unjudged because the two mean opposite things
	// to a person. Unjudged is "the work ships and nobody read it", which is a
	// pass. Fault is "the reader was there and could not speak", which is not a
	// pass and not a gap: it is a hole where the run's own check should have
	// been, and a run with a hole in its check has not been shown to be whole.
	// See faulted, and FAILSAFE.md's floor.
	Fault string

	// Subject is what this gate held between its fence markers, in the words
	// the record keeps: "tree (6 files)" or "claim". See subject.go.
	//
	// It is carried on the judgement rather than recomputed by the wiring
	// because the two would answer differently the moment anything about the
	// record moved between them, and the whole value of the field is that an
	// autopsy can trust it: three refusals of one run described a sentence
	// while the tree held 42KB of changed Python, and nothing anywhere said
	// which of the two had been read.
	Subject string

	// File is the one file of the record this finding is about, in the record's
	// own spelling. Empty on a pass, on a claim-subject finding, and on every
	// mechanical judgement that is already about a named file of its own.
	File string
	// HeldPoint is the behaviour of the request this verdict was held to, in
	// the record's own words: the span a refusal was built on, the size of the
	// list a pass was weighed against, or HeldPointEmpty where the request
	// states none and the requirement was off.
	//
	// It exists because the quote alone cannot say which. textual v4-flash s13
	// journaled two gates whose subject and quote were both right, and no
	// reader could tell whether the quote had passed the checklist enum or
	// there had been no checklist at all — which are the mechanism working and
	// the mechanism absent, wearing the same event.
	HeldPoint string

	// Consumers is the finding the changed-definition reading produced: one line
	// per definition this run reshaped that the rest of the project still uses,
	// naming the shape its callers expect and how many of them there are.
	//
	// It is a field of its own for the reason Unexercised and Unasserted are:
	// a finding that travels as prose inside somebody else's gap is journaled by
	// nothing and reachable by nothing. See consumers.go.
	Consumers []string

	// Unbound is the finding the unbound-reference reading produced: one line
	// per name the run's own sources READ that nothing in the tree binds, each
	// carrying the file and line it is read at.
	//
	// It is a field of its own for the reason Consumers is, and the name is the
	// whole point of it: igel s14's gate could say the run's checks were red and
	// could not say that `temp_post_req_data_path` was the name they were red
	// about. See unbound.go.
	Unbound []string

	// Constraint is the finding this gate's newest law produced: one line per
	// rule the person SET that this work broke, their own words followed by the
	// files the run changed in spite of them.
	//
	// It is a field of its own for the reason Unbound and Unexercised are, and
	// for one more that is its alone: it is the only finding here that no round
	// may be bought against. Every other gap can be answered with more work;
	// this one says the work did the thing it was forbidden to do, and more of
	// it is not the answer. See ExtendForGap, which refuses before anything is
	// planned, and store.DeliveryGate.Whole.
	Constraint []string

	// Finding names WHICH MEASUREMENT this gap is, in one stable word, and
	// Cited above holds the things it names. Empty for a model judge's verdict,
	// which is a reading of a request and not a measurement of the world.
	//
	// It exists because a finding's identity has only ever been its SENTENCE,
	// and a sentence is not a structure. A reader asking "is this the same
	// finding the last round raised" — the governor deciding whether a repair
	// round bought anything, an autopsy counting how many times one mechanism
	// fired — has had to compare prose that carries a bounded list of names
	// glued into the middle of it. happy-dom's v4-flash s13 raised the identical
	// removed-checks finding on four consecutive rounds and nothing in the
	// record could say so. The pair is the comparable thing: this kind, and the
	// names.
	Finding string

	// Receipt is the positive sentence this delivery earned, in the words the
	// person reads: that the request was met exactly as it was stated, or that
	// the work's own checks ran and settled while coverage could not be
	// measured. Empty is the ordinary case and says nothing either way.
	//
	// IT IS THE ONE FIELD ON THIS VALUE THAT IS NOT A COMPLAINT. Everything
	// else here names something wanting, and a run that ends because the thing
	// asked for is in hand had no way to say so — so "the plan ran out" and
	// "the request was met" reached the person as the same silence, and the
	// silence was spelled `partial`. It is deliberately NOT read by
	// store.DeliveryGate.Whole: a receipt says why the run stopped, and whether
	// the delivery is whole is still settled by the pass, the repair and the
	// world-doors exactly as it was.
	Receipt string

	// RequestAsked says the one question — is this request, as the person wrote
	// it, satisfied by what is in hand — has already been put for this verdict.
	//
	// It travels because the question has two doors and one price. The gate's
	// caller asks before it buys a repair round; the extension door asks before
	// it buys a remainder; and a gate that asked and was told no must not pay
	// for the same answer twice on the way to the same conclusion. See
	// RequestMet, and satisfied.go for the whole of it.
	RequestAsked bool

	// Request is that question, carried as the ability to ask it.
	//
	// A FUNC ON A VALUE IS UNUSUAL HERE AND IT IS THE CHEAPER OF TWO EVILS. The
	// question needs a model client and a settings object; the two doors are in
	// two packages and only the gate's caller holds either; and the alternative
	// was widening ExtendForGap's signature at every call site — including six
	// in tests — for a door that today has exactly one caller. Nil is the
	// ordinary case and means this verdict cannot ask, which is how every
	// caller that never set it already behaves.
	Request RequestQuestion

	// Fallback says this verdict was reached under the CLAIM contract after the
	// tree contract could not be answered — free text where there were enums —
	// or that the mechanical gate settled it after no verdict could be read at
	// all. It is journaled as the subject, because the subject is what an
	// autopsy reads to learn what the gate was actually holding, and a verdict
	// reached under a narrow contract and one reached under a loose one are two
	// different events wearing one word. See judgeDeliverable's fallback ladder
	// and igel s15.
	Fallback bool
}

// Cited is the gap's citations, and the one reader every admission rule goes
// through. It falls back to the joined Quote when the list is empty so a
// judgement built before the list existed — or by a caller outside this
// package that only knew how to set one span — is still weighed rather than
// silently treated as citing nothing.
func (j Judgment) Cited() []string {
	if cited := trimmedCitations(j.Citations); len(cited) > 0 {
		return cited
	}
	return trimmedCitations([]string{j.Quote})
}

// trimmedCitations is the shape every citation list is kept in: trimmed, with
// the empty ones dropped. A blank element is not a citation, and one silently
// carried through the rules would ground itself against any text at all —
// strings.Contains is true of the empty string everywhere.
func trimmedCitations(citations []string) []string {
	kept := make([]string, 0, len(citations))
	for _, citation := range citations {
		if citation = strings.TrimSpace(citation); citation != "" {
			kept = append(kept, citation)
		}
	}
	return kept
}

// joinCitations renders a citation list as the one line a person reads and a
// text field stores. The separator is the one anybody enumerating in prose
// would use, and it is the only place in this package that turns the list back
// into a string — nothing downstream of it may re-split the result, because a
// citation that legitimately contains a comma would come apart.
func joinCitations(citations []string) string {
	return strings.Join(trimmedCitations(citations), ", ")
}

// GateNotebookBytes is the notebook digest's bound when the window is unknown,
// which is what it always was. Eight distilled lessons of up to 512 bytes each
// were being clipped into a kilobyte, so items six through eight simply were
// not in the prompt — a bound written as an absolute number and outlived by the
// block it bounds. Known windows now spend a share of the budget instead (see
// gateNotebookShare); this literal is what the unknown case falls back to, and
// keeping it means a build with no catalog behaves exactly as it did.
const GateNotebookBytes = 1 << 10

// The gate's prompt is one pot, and three of the things in it are the ones this
// package may shorten: the notebook digest above the deliverable, the tail of
// what the run actually ran below it, and — in the retry judgement — the partial
// the first attempt left behind. The weights are stated out of a hundred and
// deliberately do not sum to it. What is left over is the request, the compiled
// goal, the working method and the deliverable itself, none of which may be
// clipped here at all: a gate that judges an abridged deliverable is judging
// something the person will never read, which is the one failure no budget is
// worth causing.
const (
	gateShareTotal    = 100
	gateNotebookShare = 5
	gateRanShare      = 5
	gatePartialShare  = 25
)

// Option carries a fact a judgement cannot ask its client for. There is exactly
// one today — the context window of the model it is about to call — and it
// arrives as a variadic tail rather than as a parameter so that a caller which
// does not know the window compiles unchanged and behaves exactly as it did
// before this existed. An unknown window spends nothing: every bound below
// names the literal it was written with and falls back to it.
type Option func(*bounds)

// bounds is a judgement's own copy of the window law.
type bounds struct{ contextTokens int }

// WithContextTokens tells a judgement the window of the model it is about to
// call. Zero is unknown, and unknown is never treated as small.
func WithContextTokens(tokens int) Option {
	return func(b *bounds) {
		if tokens > 0 {
			b.contextTokens = tokens
		}
	}
}

func newBounds(options []Option) bounds {
	var settled bounds
	for _, option := range options {
		if option != nil {
			option(&settled)
		}
	}
	return settled
}

// budget turns the window into spendable prompt room, with the judgement's own
// system prompt named as the fixed floor. The floor is measured rather than
// guessed at: the prompt is a constant in this file, so its cost is a fact the
// package already holds.
func (b bounds) budget(prompt string) ctxbudget.Budget {
	return ctxbudget.For(b.contextTokens).WithFloor(len(prompt) / ctxbudget.BytesPerToken)
}

// GateLessonsHeading labels the notebook block for what it is: an account of
// OTHER work. The fence below it bounds where the deliverable IS; this bounds
// what these lines may be used for.
//
// They were distilled from earlier jobs, and several of them from earlier
// verdicts of this same gate — which is how one false negative became a lesson
// ("do not write individual items to separate files and report progress … the
// gate requires one contiguous output") that was then injected above the next
// job's deliverable and read back out as that job's gap. A judge that reads a
// lesson as a description of the material in front of it is reading a previous
// mistake as present evidence, and about to write the next one.
//
// It is exported because it is the seam's own name and the prompt-shape tests
// hold the gate to a reading order; two spellings of one heading is a heading
// that eventually stops matching.
const GateLessonsHeading = "Lessons from EARLIER, UNRELATED work — " +
	"what to look for, never a description of the deliverable below:\n"

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
	// Swept is what the workspace walk answered a NAME with: a file carrying a
	// name the request or plan spelled that the run's own record of what it left
	// behind does not hold. It is kept apart because one list was answering two
	// questions, and answered "what did this run change" wrongly for the run of
	// 2026-09-03 that made 156 shell calls, wrote nothing, and was refused over
	// `CLAUDE.md` — a file its contract had told it to READ.
	//
	// Nil on every delivery whose sweep found nothing, which is nearly all of
	// them.
	Swept []string
	Ran   []string
	// Named is what the request itself named as a file, in the person's own
	// spelling. It is the half of the record the gate could never check: a
	// judge holding only prose was asked whether the finished thing exists,
	// could see no further than the sentence claiming it does, and answered
	// from the sentence — in both directions. It failed a delivery for not
	// retyping a file that was on disk, and it passed one that claimed a file
	// was "written and verified" when nothing of that name had been written at
	// all. Rendered against Artifacts and Swept, each name settles itself without
	// turning the workspace's answer into a change the run made.
	Named []string
	// Done is the criterion the plan stated before the work started: what this
	// leaf was to produce and the checks that settle it. It travels verbatim
	// through retries by construction (plan.Spec), so it is the one standard
	// here that the run cannot have moved, and it belongs beside the record it
	// is settled against rather than in the prose above it.
	Done plan.Done
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
	// Baseline is what was already broken before this work began, in the words
	// of the only thing that measured it. A coding worker photographs the
	// repository's own checks before it starts, so when a check comes back red
	// it can say whether the change caused it; nothing else in the tree can,
	// and the judge least of all — it holds prose and a file list, and cannot
	// run anything.
	//
	// Without it the gate read a suite's absolute state as a verdict on the
	// change: a repository carrying one pre-existing red test failed every
	// correct patch that passed through it, four times out of four on the
	// measured battery (audit-notes §14.4.1). It is stated as fact rather than
	// as an excuse, and it acquits only what it names.
	Baseline []string
	// Regressed names the project's own checks that PASSED BEFORE this work and
	// FAIL AFTER it, in the words the runner printed them in.
	//
	// It is Baseline's opposite number and the half that convicts. Baseline
	// acquits what was already red; this names what this work turned red, and it
	// is the one finding on this whole record that no citation could ever be
	// weighed for — a person does not have to ask for their repository to keep
	// working. See Regressions, which raises it as a finding of its own, and
	// Judgment.Sourced, which is what lifts it clear of the citation invariant.
	//
	// Nil on every worker that cannot take two readings of the project's own
	// command, which reads as no claim.
	Regressed []string
	// Removed names the PUBLIC names this work deleted: a name the tree spelled
	// before the job's first change and does not spell now, in the files the
	// run's own record says it changed.
	//
	// It is Regressed's other half. A check that goes red is the suite noticing;
	// this is what the suite structurally cannot notice, because a project only
	// has checks for what somebody wrote checks for. See RemovedPublicNames and
	// verify.Surface.
	//
	// Nil on every worker that cannot take two readings of the tree, which reads
	// as no claim.
	Removed []string
	// OwnFailing names the red checks that first appeared AFTER the baseline —
	// the ones this run wrote itself and did not get passing. See
	// OwnChecksFailing, and verify.Reading.OwnFailing for why it is not a
	// regression.
	OwnFailing []string

	// Unbound is what the LEAF measured of the same question the gate re-takes
	// for itself: names the run's own sources read that nothing in the tree
	// binds, already worded (verify.UnboundWords). The gate reads it only
	// where it has no workspace of its own to re-read — which is the same place
	// removedSinceTheJobBegan leaves the leaf's answer standing, and for the
	// same reason: a measurement nobody could re-take is still a measurement.
	Unbound []string
	// Account is the worker's own structured account of the work: the files it
	// changed, with the kind and size of each change, and the checks it ran
	// with what each one found.
	//
	// It is the other half of the same correction Baseline made. A worker that
	// drives a whole pipeline behind a process boundary used to hand the gate
	// one sentence, and when the pipeline ended without a verdict the sentence
	// said only that it had ended — so the gate judged a void and answered
	// differently each time it was asked. The account is that void filled with
	// what the worker actually observed, and it arrives as rows, beside the run
	// tail, in the same grammar: facts the judge could not gather for itself,
	// and no instruction about what to make of them.
	//
	// Nil on every leaf whose worker cannot photograph its own change set,
	// which is nearly all of them, and nil reads as no claim.
	Account *exec.Account
	// Patch is a path to the whole text of the change the work made, when the
	// worker could derive one from the repository.
	//
	// It is the difference between a record of NAMES and a record of CONTENT,
	// and the gap between the two was measured as a false statement shipped to a
	// person. A leaf was asked to explain a root cause; its method — written
	// before the work ran, by a pass handed nothing but file paths — offered an
	// illustrative example of what a root cause might be; the leaf shipped that
	// example verbatim as the real one; and this gate passed it, because
	// everything it held said which files changed and nothing said what the
	// change was. A judge that can read the diff can convict that claim, and can
	// equally absolve a correct one it would otherwise have had to guess at.
	//
	// Empty on every worker that produces no diff, which reads as no claim.
	Patch string
	// Accept is the acceptance checklist: the behaviours the person's REQUEST
	// states, read from the request before any work existed and carried on the
	// plan's spec. It is what the gate holds the delivery to beyond "does this
	// read like an answer", and it is the one thing on this record that was
	// neither produced by the work nor written about it.
	//
	// Empty on every job whose request states nothing checkable, which is most
	// of them, and empty reads as NO CHECKLIST rather than as nothing asked for.
	Accept []plan.Point
	// Constraints are the rules the person's REQUEST states about what the run
	// may or may not DO, in their own words, carried on the plan's spec.
	//
	// They are Accept's other half and they answer to a different question.
	// The checklist is what the finished thing must DO; a constraint is what the
	// run may not do on the way there, and no reading of a deliverable can
	// settle it — only the list of what the run changed can. The mechanical
	// kinds are held here before a judge is bought (HoldConstraints); the rest
	// are shown to the judge as the standard beside the request.
	//
	// Empty on every job whose request stated no rule, which is most of them,
	// and empty reads as NO RULE rather than as a rule nobody could check.
	Constraints []plan.Constraint
	// Verification is the photograph of the project's own checks the run took —
	// the roster before the work and the roster after it, on the budget PERF.md
	// states. The gate reads it instead of reading the deliverable's sentence
	// about its own tests: a claim that "all 56 tests pass" is a claim about
	// tests the same worker wrote, and weighing it is how two graded runs ended
	// at exit 0 over wrong answers (docs/design/gate/ACCEPTANCE.md).
	//
	// A zero value is a photograph nobody took, which stops the acceptance
	// settlement rather than convicting anything: NOBODY LOOKED IS NOT NOTHING
	// WRONG, and it is not a finding either.
	Verification verify.Reading
	// Workspace is where the work happened, and it is here for one purpose: the
	// gate may need to take the after reading itself. A worker that took no
	// second photograph — a repair that only rewrote the account, a leaf whose
	// wall could not afford one — leaves the final tree unmeasured, and the
	// reading of the tree that is about to be handed over is the gate's to hold.
	//
	// Empty means the gate takes no reading of its own, which is what every
	// caller that has no workspace to name already gets.
	Workspace string
}

// gateEvidenceRan bounds what travels when the window is unknown. The executor
// already keeps a short tail; this is the second bound, because a judge reading
// a hundred lines of shell before the deliverable is a judge reading the wrong
// thing first. It is a floor now rather than a ceiling: a known window buys more
// of the tail, and a build that knows nothing about its model sends the same
// twenty-four lines it always did.
const gateEvidenceRan = 24

// gateEvidenceLine is what one recorded tool call is assumed to cost. The block
// is bounded in lines because that is what it is made of and what the prompt
// counts out loud, and the budget is spent in bytes, so one of the two has to be
// stated. A tool call with a short argument object is the shape being sized.
const gateEvidenceLine = 256

// gateEvidenceLines is how much of the run tail this window can afford.
func gateEvidenceLines(budget ctxbudget.Budget) int {
	if lines := budget.Share(gateRanShare, gateShareTotal, 0) / gateEvidenceLine; lines > gateEvidenceRan {
		return lines
	}
	return gateEvidenceRan
}

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
func (e Evidence) block(budget ctxbudget.Budget) string {
	if len(e.Artifacts) == 0 && len(e.Ran) == 0 && len(e.Named) == 0 &&
		len(e.Baseline) == 0 && len(e.Regressed) == 0 && !e.Verification.Taken &&
		e.Done.Empty() && e.Account.Empty() && e.Patch == "" {
		if !e.Observed {
			return ""
		}
		return UnexercisedRecord
	}
	var body strings.Builder
	// A TREE THAT IS THE DELIVERABLE IS NOT ALSO A RECORD ABOUT ONE. Where the
	// fence holds the changed files (see subject.go), listing them again here
	// would put one list in the prompt twice under two headings that mean
	// different things — and the second heading, "what the work left behind",
	// is the one that invites a judge to weigh the tree as supporting evidence
	// for a message it is no longer being shown as the deliverable.
	// A swept file was not left behind by this run, so it does not belong in
	// this list; namedBlock accounts for it against the name the request used.
	//
	// WHAT THE RECORD CLAIMS AND THE WORLD DOES NOT HOLD SURVIVES EITHER WAY,
	// because the fence can only carry files that exist and those two lines are
	// the ones that convict: a path the deliverable names and the filesystem
	// does not have, and a directory wearing a file's name. They are the whole
	// of the block under a tree subject and they lead it under a claim.
	if len(e.Artifacts) > 0 {
		tree := e.Subject() == SubjectTree
		head := "What the work left behind:\n"
		if tree {
			head = "What the record NAMES and the tree does not hold:\n"
		}
		for _, artifact := range e.Artifacts {
			info, err := os.Stat(artifact)
			switch {
			case err != nil:
				// A path the deliverable names and the filesystem does not
				// have is the loudest thing in this block, so it is stated
				// rather than dropped for being unreadable.
				body.WriteString(headOnce(&head) + fmt.Sprintf("%s (not on disk)\n", artifact))
			case info.IsDir():
				// A directory where a file was expected is the shape that made
				// a leaf claim a written file that was never written: reported
				// as a size it reads as the deliverable.
				body.WriteString(headOnce(&head) + fmt.Sprintf("%s (a directory, not a file)\n", artifact))
			case !tree:
				body.WriteString(headOnce(&head) + fmt.Sprintf("%s (%d bytes)\n", artifact, info.Size()))
			}
		}
	}
	if named := e.namedBlock(); named != "" {
		if body.Len() > 0 {
			body.WriteString("\n")
		}
		body.WriteString(named)
	}
	if criterion := doneBlock(e.Done); criterion != "" {
		if body.Len() > 0 {
			body.WriteString("\n")
		}
		body.WriteString("What this work was to produce, stated before it ran:\n")
		body.WriteString(criterion + "\n")
	}
	// The worker's own account of the change, in rows. It sits above the run
	// tail for the reason the artifact list does: the tail is what the work
	// typed, and this is what came of it.
	if rows := e.Account.Lines(); len(rows) > 0 {
		if body.Len() > 0 {
			body.WriteString("\n")
		}
		for _, row := range rows {
			body.WriteString(row + "\n")
		}
	}
	// The change itself, under the rows that name it. This is the only block in
	// the record that can settle a claim about WHY something was changed, so it
	// sits with the account it belongs to rather than with the run tail.
	if patch := e.patchBlock(budget); patch != "" {
		if body.Len() > 0 {
			body.WriteString("\n")
		}
		body.WriteString(patch)
	}
	if len(e.Baseline) > 0 {
		if body.Len() > 0 {
			body.WriteString("\n")
		}
		body.WriteString("What was ALREADY failing in this repository before the work began, " +
			"measured against it before anything was touched. These are not this work's " +
			"doing and are not gaps:\n")
		for _, note := range e.Baseline {
			body.WriteString(note + "\n")
		}
	}
	// And its opposite number, stated in the same breath so the two are read
	// together: a judge told only what was already red is a judge that can
	// forgive a fresh failure by mistaking it for an old one.
	if len(e.Regressed) > 0 {
		if body.Len() > 0 {
			body.WriteString("\n")
		}
		body.WriteString("What this work TURNED RED. These checks passed before it and fail after it, " +
			"measured twice with the project's own command:\n")
		for _, name := range e.Regressed {
			body.WriteString(name + "\n")
		}
	}
	// What the project's own verification actually said, in place of what the
	// deliverable says it said. A judge holding a sentence like "All 56 tests
	// pass" and nothing beside it weighs the sentence, because a sentence is
	// what it has; a judge holding the command, its exit status and its roster
	// weighs the world. The two runs this block exists for both passed on the
	// sentence (docs/design/gate/ACCEPTANCE.md).
	if reading := e.readingBlock(); reading != "" {
		if body.Len() > 0 {
			body.WriteString("\n")
		}
		body.WriteString(reading)
	}
	ran, tail := e.Ran, gateEvidenceLines(budget)
	if len(ran) > tail {
		ran = ran[len(ran)-tail:]
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

// headOnce writes a section's heading the first time the section has anything
// under it, and nothing after that. It exists because a heading printed above an
// empty list is the record asserting a fact it does not hold — "what the tree
// does not have:" followed by nothing reads as a claim that something is missing.
func headOnce(head *string) string {
	written := *head
	*head = ""
	return written
}

// gatePatchBytes is how much of the change's text travels when the window is
// unknown, and gatePatchShare is what a known window spends on it. It is the
// largest share of the three clippable blocks because it is the only one that
// can settle a claim rather than merely support one — and because a diff is the
// most compressible thing in the prompt: hunk headers and context lines are
// cheap tokens, and even a truncated one names the functions it touched.
const (
	gatePatchBytes = 6 << 10
	gatePatchShare = 12
)

// patchBlock puts the change's own text in front of the judge, clipped, and
// says out loud that it is clipped.
//
// It reads the file here rather than being handed the bytes for the same reason
// the artifact block stats its paths here: what the record must say is what is
// TRUE at judging time, and a patch that has been swept away between the run and
// the review is a fact about the record, not a detail to paper over. A patch
// that cannot be read renders nothing at all — an unreadable diff must never be
// able to read as an empty one, which is the direction that acquits.
func (e Evidence) patchBlock(budget ctxbudget.Budget) string {
	if strings.TrimSpace(e.Patch) == "" {
		return ""
	}
	body, err := os.ReadFile(e.Patch)
	if err != nil || strings.TrimSpace(string(body)) == "" {
		return ""
	}
	head := "The change itself, as the repository records it"
	if span := e.Account.ChangeRange(); span != "" {
		head += " (" + span + ")"
	}
	head += ". Every claim the deliverable makes about WHAT was wrong and WHAT was " +
		"changed is settled here and nowhere else:\n"
	clipped := clipUTF8Bytes(string(body), budget.Share(gatePatchShare, gateShareTotal, gatePatchBytes))
	if len(clipped) < len(body) {
		head += "(the first " + strconv.Itoa(len(clipped)) + " bytes of " +
			strconv.Itoa(len(body)) + "; the whole of it is at " + e.Patch + ")\n"
	}
	return head + clipped + "\n"
}

// patchSource is the change's whole text, read from where the worker left it.
//
// It is separate from patchBlock because the two readers want different things
// from one file. The block is for a model and is clipped to a share of a prompt
// budget; this is for the check-declaration reader, which scans and keeps
// nothing, and which would report a truncated diff's later hunks as checks the
// work never wrote. An unreadable patch is the empty string, which reads as no
// claim everywhere it is used.
func (e Evidence) patchSource() string {
	if strings.TrimSpace(e.Patch) == "" {
		return ""
	}
	body, err := os.ReadFile(e.Patch)
	if err != nil {
		return ""
	}
	return string(body)
}

// namedBlock settles every file the request named against the files the run
// actually left behind and the names the workspace answered. Each line is a
// fact rather than a judgement — the gate still decides what a missing file
// means for this ask — and the three directions are stated in the same words so
// none can be read as the louder one.
func (e Evidence) namedBlock() string {
	if len(e.Named) == 0 {
		return ""
	}
	var body strings.Builder
	body.WriteString("What the request named by name, and whether the run produced it:\n")
	for _, name := range e.Named {
		if produced, ok := ProducedFile(name, e.Artifacts); ok {
			fmt.Fprintf(&body, "%s — produced, at %s\n", name, produced)
			continue
		}
		if found, ok := ProducedFile(name, e.Swept); ok {
			fmt.Fprintf(&body, "%s — a file of that name is there, at %s; this run did not write it\n",
				name, found)
			continue
		}
		fmt.Fprintf(&body, "%s — nothing of that name is among what was left behind\n", name)
	}
	return body.String()
}

// doneBlock renders the criterion the way the plan stated it. It is deliberately
// the same shape the worker was handed (plan.Spec.Render) rather than a second
// wording of it: a standard restated is a standard that drifts.
func doneBlock(done plan.Done) string {
	if done.Empty() {
		return ""
	}
	var body strings.Builder
	if len(done.Produces) > 0 {
		body.WriteString("It produces: " + strings.Join(done.Produces, "; "))
	}
	for _, condition := range done.Conditions {
		check := strings.TrimSpace(condition.Check)
		if check == "" {
			continue
		}
		if body.Len() > 0 {
			body.WriteString("\n")
		}
		kind := strings.TrimSpace(condition.Kind)
		if kind != plan.CheckRun && kind != plan.CheckRead {
			kind = plan.CheckRead
		}
		body.WriteString("- (" + kind + ") " + check)
		if expect := strings.TrimSpace(condition.Expect); expect != "" {
			body.WriteString(" — " + expect)
		}
	}
	return strings.TrimRight(body.String(), "\n")
}

// NamedFiles lists, in order and without repeats, the files a piece of text
// names. It is exported because the gate's caller holds the request and the
// gate holds the record, and the answer they need is the same list.
//
// The shape rule itself is verify.NamedPaths and is deliberately not repeated
// here. Which text names a file is one question asked at three ends — which
// files a request is about, which decides where a reading is taken; which files
// a request named, which the delivery record settles against the disk; and which
// file a produces entry IS — and a regular expression written down twice is a
// regular expression that will differ.
func NamedFiles(text string) []string { return verify.NamedPaths(text) }

// ProducedFile answers whether one named file is among the files a run left
// behind, and returns the path it landed at.
//
// Which recorded path answers to a name is namedAs, which states that law once
// for this package because the grounding rule asks the same question of a
// citation and a request.
//
// The file must be on disk and must be a file: a recorded path with nothing at
// it, or a directory wearing the name, is not a produced deliverable.
func ProducedFile(named string, artifacts []string) (string, bool) {
	if fileKey(named) == "" {
		return "", false
	}
	for _, artifact := range artifacts {
		if !namedAs(named, artifact) {
			continue
		}
		if info, err := os.Stat(artifact); err != nil || info.IsDir() {
			continue
		}
		return artifact, true
	}
	return "", false
}

// AdmitGapArtifact refuses the one gap the world has already closed: the review
// quoted a span of the request that names a file, and the workspace holds every
// file that span names.
//
// It is the artifact half of the same invariant AdmitGapRevision applies to
// prose. A gap is what the person asked for and did not get; a file they asked
// for by name, sitting on disk at the name they used, is something they got. The
// measured cost of not having this was a delivery failed for "not containing the
// script text" while the script sat in the workspace, a repair round, and a
// whole continuation node spent retyping a correct file into a message.
//
// It refuses nothing else. A gap about what is INSIDE a produced file quotes the
// substance rather than the filename, and the substance is not a name this can
// match — so the ordinary path judges it, as it should.
func AdmitGapArtifact(citations []string, evidence Evidence) string {
	// Every file every citation names, because the gap is one gap: a list of
	// five files of which four are on disk is still a gap about the fifth.
	var names []string
	for _, citation := range trimmedCitations(citations) {
		names = append(names, NamedFiles(citation)...)
	}
	if len(names) == 0 {
		return ""
	}
	for _, name := range names {
		if _, ok := ProducedFile(name, evidence.producedArtifacts()); !ok {
			return ""
		}
	}
	return "what it asked for is already on disk under the name the request used"
}

// enumerationItem matches one item of a list the person spelled out: a quoted
// phrase, or a run of words that begins with a capital or a digit. It is the
// shape of an enumeration and nothing looser — a lowercase clause is prose, and
// prose is what the ordinary path judges.
var enumerationItem = regexp.MustCompile(`"[^"]{2,60}"|'[^']{2,60}'|\p{Lu}[\p{L}\p{N}]*(?:[ \-][\p{Lu}\p{N}][\p{L}\p{N}]*)*|\p{N}[\p{L}\p{N}]*`)

// enumerationFloor is how many named items make a span an enumeration. Two is
// not a list — "Go CLI", "New York" — and a rule that fired on two would refuse
// gaps about ordinary prose that happens to name a product. Three is the
// smallest span a person writes as a list.
const enumerationFloor = 3

// AdmitGapPresent refuses the gap the deliverable has already closed in words,
// as AdmitGapArtifact refuses the one it closed on disk. The two are one
// invariant asked at either end of the same ledger: a gap is what the person
// asked for and did not get, and a thing they asked for by name that is sitting
// in the text they are about to read is something they got.
//
// It is narrow on purpose and it refuses nothing else. The only span it can
// settle is an ENUMERATION — three or more items the person named themselves,
// inside the words the review quoted — and it settles it only when every one of
// them appears in the deliverable. That is the case the gate demonstrably gets
// wrong: twelve databases named in the ask, twelve profiles in the message,
// and a verdict saying the twelve are not there. A gap about prose names no
// enumeration and reaches the ordinary path; a gap about what is INSIDE one of
// the items names the substance rather than the item, and reaches it too; a
// deliverable missing even one of the named items is judged as it always was.
//
// AND IT MAY ONLY SPEAK WHERE THE DELIVERED TEXT IS THE WHOLE OF WHAT THE RUN
// LEFT BEHIND. This acquits a finding — it sets store.DeliveryGate.Overturned,
// which is the field the exit code turns on — and an acquittal has to be
// weighed against the world. The deliverable is not the world; it is the
// component being checked, and a rule that reads it is FAILSAFE clause 2 broken
// in the strict sense the clause states it. A worker that restates the request
// back in the request's own words satisfies the containment test below by
// writing prose about work it did not do, and one did: textual s5 was acquitted
// on "everything it names is already in the delivered text" over a true finding
// that an example file had never been written, and shipped 1 of 20 hidden
// checks under exit 0 (2026-08-29, bench/deepswe; docs/design/gate/SETTLEMENT.md
// §6).
//
// Where the run produced NOTHING BUT THE MESSAGE, that is not a softening of the
// rule but the same rule: the message is the only artifact the run made, so a
// citation settled against it is settled against everything there is. That is
// the twelve-profiles case exactly, and it survives untouched. The moment a file
// is in the record, the record is the world and the text is a claim about it —
// which AdmitGapArtifact is the door for, and this one closes.
//
// Presence is checked case-insensitively and nowhere else is anything relaxed:
// this is a containment test, so it can close a gap and can never open one.
func AdmitGapPresent(citations []string, deliverable string, evidence Evidence) string {
	// The record first, because it decides whether this door exists at all for
	// this delivery. It is asked of the whole record rather than of the
	// citation's own file names: a run that left three files behind has a world
	// to be checked against, and prose does not get to overrule it about any of
	// them.
	if len(evidence.Artifacts) > 0 {
		return ""
	}
	// The enumeration is read off the citations as one line. Joining is safe
	// here and only here: an item of an enumeration cannot span the separator,
	// because the separator is not a character an item is made of.
	quote, deliverable := joinCitations(citations), strings.ToLower(deliverable)
	if quote == "" || deliverable == "" {
		return ""
	}
	seen := map[string]bool{}
	items := make([]string, 0, 8)
	for _, match := range enumerationItem.FindAllString(quote, -1) {
		item := strings.ToLower(strings.Trim(strings.TrimSpace(match), `"'`))
		if len(item) < 2 || seen[item] {
			continue
		}
		seen[item] = true
		items = append(items, item)
	}
	if len(items) < enumerationFloor {
		return ""
	}
	for _, item := range items {
		if !strings.Contains(deliverable, item) {
			return ""
		}
	}
	return "everything it names is already in the delivered text, in the words the request used"
}

// ── how much room a verdict gets, and what happens when it uses it all ───────
//
// Every judgement in this file answers with one small JSON object, and for a
// long time the cap on the call said exactly that: two hundred tokens for a
// worker's name, four hundred for a verdict. That is the right size for the
// visible answer and the wrong size for the call. A reasoning model spends its
// completion budget thinking before it writes, so a cap sized for the object
// alone is spent entirely on deliberation and the object never arrives.
//
// What made it worse here than anywhere else is what an empty reply MEANS to a
// gate. An unparseable verdict used to be a pass, so a cap too small for a
// reasoning model was not a call that failed: it was a gate that stopped
// existing, quietly, on the models most worth pointing it at.
//
// BOTH HALVES OF THAT ARE NOW THE SHARED SEAM'S (internal/shaped). The room is
// derived from the ask rather than named here — the share of the completion
// reserve this file measured is stated there, once, for every structured call in
// the harness — and a reply that arrives cut off or unreadable is continued or
// asked again before this file ever sees it. What is left here is the one
// question that was always the gate's: what an answer that still did not come
// back MEANS. See the fault path in judgeDeliverable.

// askVerdict sends one judgement through the shared structured-answer seam and
// decodes it into the caller's destination.
//
// The seam owns the ceiling, the continuation of a cut answer, the single re-ask
// for a reply that was not an object, and the typed fault when neither worked.
// This wrapper exists only to say the two things that are true of every
// judgement in this file and of nothing else: the lane is "gate", and the schema
// travels on the wire only where there is a router to carry it.
func askVerdict(ctx context.Context, client *pool.Client, messages []ai.Message,
	schema json.RawMessage, into any) (*ai.Response, error) {
	return shaped.Answer(ctx, client, shaped.Ask{
		Lane:     "gate",
		Messages: messages,
		Schema:   schema,
		Routed:   client != nil && client.Routed(),
	}, into)
}

// judgeDeliverable returns a checked pass or named gap. Every failure of the
// gate itself remains fail-open: Checked is false, so it neither blocks delivery
// nor manufactures verified evidence for the profile. It is also said out loud
// now — see Judgment.Unjudged and unjudged below — because fail-open and silent
// are two different designs and only one of them was ever chosen.
func JudgeDeliverable(ctx context.Context, settings config.Config, client *pool.Client, graph *store.Store, node store.Node, deliverable, method string, evidence Evidence, workerModel string, options ...Option) Judgment {
	// THE READING OF THE TREE THAT IS ABOUT TO BE HANDED OVER IS THE GATE'S TO
	// HOLD. A worker photographs the project's own checks before it starts and
	// again at the end, and that second photograph is normally the reading of
	// the final tree — but a repair round that only rewrote the account, and a
	// worker that could not afford the second reading, both leave the tree
	// unmeasured at the moment it is judged. The gate takes the reading itself
	// there, on the same budget the worker was held to, so that the world's
	// answer exists wherever a verdict is being reached rather than only where a
	// worker happened to be able to take one.
	evidence.measureFinalTree(ctx, verify.JobKey(node.Provenance.Intent))
	// AND EVERY NAME THIS DELIVERY IS ABOUT IS SETTLED AGAINST THE WORLD BEFORE
	// ANYTHING IS ASKED OF IT. The mechanical gate and the door that refuses a
	// gap the disk has already closed read the union; every question about what
	// changed keeps reading the run's own record alone.
	evidence.completeAgainstTheWorld()
	// AND WHAT THE JOB HAS DELETED FROM ITS OWN PUBLIC SURFACE IS RE-SETTLED
	// HERE, AGAINST THE TREE AS IT NOW STANDS. Until this, the finding was the
	// judged node's own outcome — so a job whose work was done by grown leaves
	// lost it entirely, and igel s14 shipped an ImportError into all twenty-four
	// hidden tests while three of its leaves had each journaled the loss.
	// Re-taking it rather than carrying the leaf's list is what lets a repair
	// round CLEAR the finding: a name put back is a name the new reading finds.
	// See Evidence.removedSinceTheJobBegan.
	if lost, settled := evidence.removedSinceTheJobBegan(
		verify.JobKey(node.Provenance.Intent)); settled {
		evidence.Removed = lost
	}
	// AND WHAT THIS GATE IS ABOUT IS STAMPED ON WHATEVER COMES BACK, HERE, ONCE.
	//
	// It used to be written onto each judgement at the place that built it, and
	// a judgement is built at eleven places in this file and two more in
	// acceptance.go. ofetch s12 is what that costs: a gate refused, the event
	// journaled, and `subject` empty in the store — so the one question an
	// autopsy of this mechanism has to answer, did the review read the world or
	// a sentence, had no answer on the very run that needed it. A field set by
	// every constructor is a field the next constructor forgets. This is the
	// one exit, so there is nothing left to forget.
	judgment := judgeDeliverable(ctx, settings, client, graph, node, deliverable, method,
		evidence, workerModel, options...)
	judgment.Subject = evidence.SubjectWords()
	// AND A VERDICT THE NARROW CONTRACT COULD NOT HOLD SAYS SO. It is stamped
	// here with everything else that is derived once, so no constructor can
	// forget it.
	if judgment.Fallback {
		judgment.Subject = SubjectFallbackWords
	}
	// And the other half of the same question, derived the same way and for the
	// same reason: WHICH behaviour this verdict was held to, or that there was
	// no list to hold it to. See heldPointWords.
	grounds := Grounds{Intent: node.Provenance.Intent, Method: method, Done: evidence.Done}
	judgment.HeldPoint = heldPointWords(evidence, grounds,
		newBounds(options).budget(DeliverablePrompt), judgment)
	// AND A REFUSAL THAT QUOTES A RULE THE PERSON SET IS A BROKEN RULE, WHOEVER
	// REACHED IT. The mechanical door settles the two readings arithmetic can
	// settle; the rest are put to the judge, and a judge that convicts on one was
	// right about the finding and had no way to say what KIND of finding it is.
	// It is stamped here, at the one exit, above every reader that decides what a
	// round may be bought for. See ConstraintQuoted.
	judgment = ConstraintQuoted(judgment, evidence.Constraints)
	// AND THE ONE QUESTION THIS VERDICT CAN STILL BE ASKED IS BOUND HERE, ONCE,
	// FOR THE SAME REASON EVERYTHING ELSE ABOVE IS.
	//
	// The question needs a model client, and only this function's caller has
	// one — so a field left for some later caller to fill in is a field nothing
	// fills in, and `ExtendForGap`'s door was dead wiring on every caller that
	// is not the chat surface. Bound at the one exit every verdict in this
	// program leaves through, it is live for all of them and there is nothing
	// left for a constructor to forget. Nil where there is no client, which is
	// how a caller that cannot ask already behaves. See satisfied.go.
	judgment.Request = requestQuestion(settings, client, node, grounds, deliverable, evidence)
	return judgment
}

func judgeDeliverable(ctx context.Context, settings config.Config, client *pool.Client, graph *store.Store, node store.Node, deliverable, method string, evidence Evidence, workerModel string, options ...Option) Judgment {
	// The one fact a judge should never be paid to discover is settled before a
	// model round is bought: a file the plan named as a deliverable that is
	// missing or empty on disk. It is checked from the structured criterion
	// alone — never inferred from prose — so it fires only when the plan
	// itself named files, and a missing one is a gate failure naming the absent
	// files verbatim. The judgment is the same shape a judged gap takes, so the
	// repair flow the gate already owns runs on it unchanged; the model judge
	// is skipped for that round, because its cost buys nothing when the absence
	// is a fact about the filesystem. When the criterion named no files, or
	// every named file is present and non-empty, this returns ok=false and the
	// path is byte-identical to before it existed.
	// The promises this run made before it began working, assembled once and
	// carried on every judgement below. Both admission doors read them off the
	// judgement rather than rebuilding them from whatever fields their own
	// caller happened to hold, which is how they came to disagree.
	grounds := Grounds{Intent: node.Provenance.Intent, Method: method, Done: evidence.Done}
	// The job this delivery belongs to, spelled the one way every reader in this
	// system spells it: a digest of the person's own request, which is the one
	// thing every leaf of a job holds identically and no two jobs share. It is
	// what the readings below are looked up against, because a reading is a
	// property of the JOB and the tree and never of the node being judged.
	job := verify.JobKey(node.Provenance.Intent)
	// THE ONE DECISION THIS WHOLE GATE TURNS ON: what the fence holds. Where the
	// run changed the tree, the change is the deliverable and the worker's
	// message is a claim about it; where it changed nothing, the message is the
	// whole of what the run produced and it is the deliverable, exactly as it
	// always was. The readings this reads are taken by the caller above, once.
	// See subject.go.
	subject := evidence.Subject()
	// THE READINGS OF THE WORLD ARE TAKEN BEFORE ANY DOOR CAN RETURN, and this
	// one used to be taken after four of them. igel s14's two gates were both
	// MECHANICAL — a file the plan promised was not on disk — so judgeDeliverable
	// returned above the block that reads this, and the store holds no consumers
	// event of any kind for that run: not an empty one, which would have said
	// the reading happened and found nothing, and not a full one. A measurement
	// that only happens on the path where a model is bought is a measurement
	// that is absent exactly when the run is already going wrong.
	//
	// Nil everywhere the reading found nothing, which is most runs.
	var consumers []verify.ChangedDefinition
	var consumerFiles, consumerLines []string
	unbound := evidence.Unbound
	if subject == SubjectTree {
		consumers = evidence.changedDefinitions(job)
		consumerFiles, consumerLines = consumerGrounds(consumers)
		// Journaled either way, INCLUDING the reading that found nothing: an
		// autopsy asking whether this door was open on a run has nothing else to
		// read (FAILSAFE.md clause 4).
		journalConsumers(graph, node.ID, consumers)
		// And the reading one question further back, taken here for every reason
		// that one is: it is about the tree the gate is judging rather than
		// about the leaf that happened to write it, and a measurement that only
		// happens on the path where a model is bought is absent exactly when a
		// run is already going wrong. See unbound.go.
		if found, settled := evidence.unboundReferences(); settled {
			unbound = verify.UnboundWords(found)
			journalUnbound(graph, node.ID, found)
		}
	}
	// A REGRESSION IS THE FIRST THING THIS GATE ANSWERS, AND IT IS NOT AN
	// OPINION. A check that passed before the work and fails after it is a
	// measurement the run made of the world, and it outranks every other reading
	// of the deliverable: whatever else was produced, the repository is worse
	// than it was found. It is settled before a model round is bought for the
	// same reason a missing promised file is — the answer is already known and a
	// judge's cost would buy nothing.
	// A RULE THE PERSON SET OUTRANKS EVERY READING OF THE WORK, INCLUDING THIS
	// ONE. The findings below are measurements of a repository; this is the
	// person's own sentence held against the files the run changed, and a run
	// that broke it has failed at the one thing it was told without ambiguity.
	// It is settled first and without a model for the reason the regression is
	// settled without one: the answer is already known, and a judge's cost would
	// buy nothing. See constraint.go and issue #427.
	if held, broken := ConstraintsHeld(evidence); broken {
		held.Grounds = grounds
		return held
	}
	if regression, broke := Regressions(evidence.Regressed); broke {
		regression.Grounds = grounds
		return regression
	}
	// And the half of the same measurement no suite can make: A PUBLIC NAME THAT
	// EXISTED BEFORE THIS WORK AND DOES NOT NOW. It sits here, beside the
	// regression and above everything a model is paid for, because it is
	// evidence of exactly the same kind — two readings of the world, taken by
	// the run, with nothing in between them to argue with. igel s11's check-level
	// reading called the tree IMPROVED on the change that broke all twenty-four
	// of its hidden tests at setup.
	if lost, removed := RemovedPublicNames(evidence.Removed); removed {
		lost.Grounds = grounds
		return lost
	}
	// And the third measurement off the same pair of readings, which is a
	// different fact about the run and gets different words: THE CHECKS THIS
	// WORK WROTE AND DID NOT GET PASSING. It sits below the two findings about
	// damage because it is not damage — nothing that was working stopped — and
	// above every model round for the reason they are: the answer is already
	// measured, and a judge's cost would buy nothing.
	if unfinished, red := OwnChecksFailing(evidence.OwnFailing); red {
		unfinished.Grounds = grounds
		return unfinished
	}
	// And the fact one question further back than any of them: A NAME THIS WORK
	// READS THAT NOTHING IN THE TREE BINDS. It sits here, beside the checks the
	// run wrote and left red, because it is what those reds usually ARE — igel
	// s14's twenty-four hidden failures were one `ImportError` on one name, and
	// the only thing the gate could say was that a suite was red. It is settled
	// above every model round for the reason the three above it are: the answer
	// is already measured, and a judge's cost would buy nothing.
	if imagined, reads := UnboundNames(unbound); reads {
		imagined.Grounds = grounds
		return imagined
	}
	// And its near neighbour, which the photograph could not see until it kept
	// rosters rather than only failures: A CHECK THAT STOPPED EXISTING. Deleting
	// the test that was failing is the cheapest way there is to make a suite
	// green, so a coverage rule that closed every other door and left this one
	// open would be teaching exactly that move. Same evidence standard as a
	// regression — the worker's own diff and two readings of the world — and so
	// the same place in the order.
	if weakened, removed := WeakenedChecks(evidence.removedChecks(), evidence.Verification.Vanished()); removed {
		weakened.Grounds = grounds
		return weakened
	}
	if mechanical, missing := MissingProduces(evidence.Done, evidence.producedArtifacts()); missing {
		mechanical.Grounds = grounds
		return mechanical
	}
	ask := node.Provenance.Intent
	budget := newBounds(options).budget(DeliverablePrompt)
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
	// The lessons are labelled for what they are — an account of OTHER work —
	// because the fence alone bounds where the deliverable is and this bounds
	// what these lines may be used for. They were distilled from earlier jobs,
	// several of them from earlier verdicts of this same gate, so a judge that
	// reads one as a description of the material in front of it is reading a
	// previous mistake as present evidence and about to write the next one.
	if digest := resident.NotebookDigest(graph, node.ID, node.Brief, ask, 8); digest != "" {
		body += GateLessonsHeading +
			clipUTF8Bytes(digest, budget.Share(gateNotebookShare, gateShareTotal, GateNotebookBytes)) + "\n\n"
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
	judged := deliverable
	if subject == SubjectTree {
		judged = evidence.treeBlock(budget)
	}
	// The behaviours a refusal may be built on, above the fence with everything
	// else that is how to judge rather than what is judged. They are the same
	// list the acceptance settlement counts (HeldPoints), so the gate cannot
	// refuse a verdict for citing a behaviour the settlement is about to weigh.
	var behaviours []string
	if subject == SubjectTree {
		if spans := behaviourSpans(HeldPoints(evidence, grounds), budget); len(spans) > 0 {
			behaviours = spans
			body += "\n\n" + behavioursBlock(spans)
		}
	}
	// And the rules the person set that no arithmetic could settle, beside the
	// behaviours and for the same reason: they are a standard the delivery is
	// held to, written by the person, before any work existed. The mechanical
	// ones never reach here — they were held above, without a model — so what
	// is shown is exactly the set a reader has to weigh. See ConstraintsBlock.
	if rules := ConstraintsBlock(evidence.Constraints); rules != "" {
		body += "\n\n" + rules
	}
	// And the definitions this run reshaped that the rest of the project still
	// uses, beside the behaviours because it belongs to the same half of the
	// prompt: how to judge, read from the world, rather than what is judged. It
	// is the one fact that catches a name kept and a shape moved, which is the
	// hole igel s12 went through with all twenty-four hidden checks red. The
	// reading itself was taken above, before any door could return. See
	// consumers.go.
	if block := consumersBlock(consumers, budget); block != "" {
		body += "\n\n" + block
	}
	body += "\n\nDeliverable as produced:\n" + FenceDeliverable(judged)
	// The worker's own account, below the fence, named for what it is. It is
	// first among the records because it is the most useful of them for the one
	// purpose it has left — a claim the files do not bear out — and because a
	// reader that meets it anywhere above the fence is a reader that has been
	// invited to judge it.
	if subject == SubjectTree {
		if claim := strings.TrimSpace(deliverable); claim != "" {
			body += "\n\nWhat the worker said about its own work. THIS IS A CLAIM ABOUT " +
				"THE DELIVERABLE AND IS NOT THE DELIVERABLE:\n" + boundedDelivery(claim, budget)
		}
	}
	// The records come last, under the deliverable they are used to check: they
	// are the most volatile block in the prompt — a revision rewrites the text
	// and re-runs the work — and the cache pays for volatility by position.
	if records := evidence.block(budget); records != "" {
		body += "\n\nWhat actually happened, as recorded while it ran:\n" + records
	}
	judgeCtx := settings.Context(router.WithAvoidModel(ctx, workerModel), "gate")
	judgeCtx = provider.WithCall(judgeCtx, provider.ClassPlanAudit)
	// "gate", not the routing class's own "audit": the class pools this call
	// with the planner's audits because they rate alike, and the model-call log
	// is read by a person who wants to know which of them refused a deliverable.
	judgeCtx = provider.WithCallTag(judgeCtx, "gate")
	// The gate is part of what this deliverable cost, not part of the day's
	// overhead: a job whose bill omits its own review reads as cheaper than it
	// was, and the review is often the second most expensive thing in it.
	judgeCtx = pool.WithSpendNode(judgeCtx, node.ID)
	// And the same node on the model-call log, which is a separate carrier from
	// the spend one above: the bill is keyed by the job that pays, the log by
	// the work a row belongs to, and a gate row with no node cannot be read
	// beside the leaf rows for the deliverable it just refused.
	judgeCtx = provider.WithCallNode(judgeCtx, node.ID)
	// The answer's shape follows the subject. Over a changed tree a refusal must
	// name one file of the record and quote the behaviour it fails, which is
	// what makes a finding about the worker's sentence unsayable rather than
	// merely discouraged; over a claim the shape is the one it always was.
	record := evidence.recordNames()
	promised := evidence.PromisedFiles()
	verdict := treeVerdict{files: record}
	schema := deliverableSchema
	if subject == SubjectTree {
		verdict.behaviours = behaviours
		verdict.consumerFiles, verdict.consumerLines = consumerFiles, consumerLines
		verdict.promised = promised
		schema = treeVerdictSchema(record, behaviours, consumerFiles, consumerLines, promised)
	}
	// One seam for every structured reply in the system. This used to send, then
	// scan for braces, then decide — and a failed decode here was a silent pass.
	// Both mistakes are gone: the seam continues a verdict that ran out of room,
	// asks once more for one that came back as prose, and when neither works it
	// says so in a type this file cannot mistake for silence. A verdict this
	// gate cannot READ — a fail over a changed tree that names no file of the
	// record — travels that same path for the same reason: the contract it
	// broke is the schema's, so the schema is what it is asked again with.
	messages := []ai.Message{
		{Role: "system", Content: []ai.ContentPart{{Type: "text", Text: DeliverablePrompt}}},
		{Role: "user", Content: []ai.ContentPart{{Type: "text", Text: body}}},
	}
	_, err := askVerdict(judgeCtx, client, messages, schema, &verdict)
	// ONE MORE ASK, INSIDE THE WALL, BEFORE A DELIVERY GOES OUT WITH NOTHING
	// HAVING READ IT.
	//
	// A call that never landed says nothing about the work, and the cheapest
	// thing that can be done about it is to make it again. reef-145 delivered
	// unjudged twice on a route that answered 404 in 58 ms — a failure fast
	// enough that the run had four minutes of wall left and spent none of it
	// asking a second time. The answer to which happened rides the note below,
	// because "asked twice and still nothing" and "there was no time to ask" are
	// different facts about a run and only one of them is anybody's to fix.
	//
	// It is one more ask and never a loop: the wall is the run's, the gate is
	// spending it, and a judge that cannot be reached twice inside a floor of
	// time is not going to be reached on the third.
	asked := ""
	if err != nil && !shaped.Unreadable(err) {
		if asked = worthAskingAgain(judgeCtx, err); asked == gateAskedTwice {
			_, err = askVerdict(judgeCtx, client, messages, schema, &verdict)
		}
	}
	// A CONTRACT MAY NOT BE THE REASON A RUN ENDS WITH NOTHING STARTED.
	//
	// The tree contract is narrow on purpose, and igel s15 shows both halves of
	// what that costs when the enum is wrong. The request says "After fit, write
	// feature_schema.joblib in the results directory"; the file was absent; the
	// judge said exactly that, naming the file; the enum did not hold the name
	// because the name is in no record of what the run LEFT BEHIND. Re-asked,
	// refused again, and the run ended at eight minutes with `the review could
	// not be read, so this delivery was never checked` and nothing further
	// started. The first half is fixed above, where the enum now holds what the
	// request and the plan promised. The second half is below.
	//
	// WHAT IS NOT DONE HERE: re-asking under the CLAIM contract, with no enums,
	// and taking whatever comes back. It was built and measured and it re-admits
	// the three findings the tree contract exists to refuse — the verdict about
	// the fenced text (`contains only {"contract": …}`), the complaint that a
	// file was shown in part, and the preference wearing a citation. A floor
	// that lets a run be settled by the opinion the contract just refused is not
	// a floor; it is the contract deleted on the path where it matters most. See
	// FAILSAFE.md's seventeenth chapter and its twenty-first.
	if err != nil {
		// AN UNREADABLE VERDICT IS A FAULT ON THE GATE, NEVER AN ABSTENTION.
		// This is the s4 sweep's second fatal shape: the gate answered with
		// something no reader could parse, the caller took that for "no opinion",
		// and the run delivered unjudged work as done with exit 0. The seam has
		// already asked again by the time this is reached, so what arrives here
		// is a gate that was given every chance and produced no verdict — which
		// is a fact about the run, and FAILSAFE.md's floor says a fact about the
		// run reaches the exit code. See Judgment.Fault and its one reader in
		// cmd/aforge/chat.go.
		if shaped.Unreadable(err) {
			provider.Report(judgeCtx, provider.VerdictFormatFailure)
			// AND THE MECHANICAL GATE STILL RUNS. A gate that produced no
			// verdict has not settled anything, and a promised file that is not
			// on disk is settled by the filesystem rather than by anybody's
			// opinion — so the run still gets the finding it plainly has, and
			// still buys the round that closes it. Ending eight minutes of work
			// with "nothing further was started" over an absent deliverable is
			// FAILSAFE.md's floor broken: a check that did not happen may not be
			// the reason a run stops.
			if absent := evidence.MissingPromised(); len(absent) > 0 {
				gap := joinCitations(absent)
				return Judgment{Gaps: gap, Quote: gap, Citations: absent,
					Mechanical: true, Checked: true, Grounds: grounds,
					Finding: FindingMissingProduces, Fallback: true}
			}
			return faulted(node, gateUnreadableWhy, err)
		}
		// A transport failure is a different thing and stays fail-open: the
		// model was never reached, so nothing about this deliverable was
		// examined and holding it hostage to the weather buys nobody anything.
		// What it is NOT any more is silent — the note says the gate was never
		// reached and what was done about it, the delivery path turns that into
		// a gate row of its own kind, and the run leaves unchecked rather than
		// ok. See GateUnreached and cmd/aforge/chat.go's seam.
		provider.Report(judgeCtx, provider.VerdictProviderFailure)
		return unjudged(node, gateNote(GateUnreached, asked), err)
	}
	if verdict.Pass {
		provider.Report(judgeCtx, provider.VerdictVerifiedSuccess)
		// A judge that omits the field says nothing about evidence, and
		// nothing is the honest reading: the missing answer stays false.
		pass := Judgment{Pass: true, Exercised: verdict.Exercised, Checked: true,
			Grounds: grounds}
		// And the last question: does anything CHECK what the person asked
		// for? On a pass it is the whole verdict — a delivery about to be
		// called whole with a stated behaviour nothing exercises is the run
		// this mechanism was built for.
		return settleAcceptance(ctx, settings, client, graph, node, evidence, grounds, workerModel, pass)
	}
	gaps := strings.TrimSpace(verdict.Gaps)
	if gaps == "" {
		provider.Report(judgeCtx, provider.VerdictSemanticFailure)
		// A fail that names nothing is not a fail and is not a pass either: the
		// gate held an opinion it could not state. It ships, and it says so.
		return unjudged(node, "the gate failed the work and named no gap", nil)
	}
	provider.Report(judgeCtx, provider.VerdictVerifiedSuccess)
	// An ungrounded gap is still recorded as a gap: it is said out loud, it
	// rides the delivery, and it is in the ledger. What it does not buy is
	// paid work — neither the revision round nor the extension — and both of
	// those refusals happen at the wiring seam rather than being laundered
	// into a pass here.
	//
	// The model judge is asked for one verbatim span and answers with one, so
	// its gap carries a single-element list and is weighed exactly as it always
	// was. Both fields are written because they are one fact seen from two
	// sides: what a person reads, and what the admission rules weigh.
	quote := strings.TrimSpace(verdict.Quote)
	// THE FILE FIRST. Over a changed tree the finding is about one file of the
	// record, so the line a person reads opens with its path rather than with
	// whatever clause the model chose to lead on.
	gaps = treeGapWords(verdict.File, gaps)
	failed := Judgment{Gaps: gaps, Quote: quote, Citations: trimmedCitations([]string{quote}),
		Checked: true, Grounds: grounds, File: verdict.File}
	// A REFUSAL THAT NAMES A PROMISED FILE THE DISK DOES NOT HOLD IS THE
	// MECHANICAL GAP, FOUND BY A JUDGE. It is the same fact the gate settles for
	// itself when the plan states a produces list, so it is marked the same way
	// and buys the same round — a refusal of its citation says only that no round
	// will be bought, never that the absence is not real.
	if verdict.Promised && !producedNonEmpty(verdict.File, evidence.producedArtifacts()) {
		failed.Mechanical = true
		failed.Finding = FindingMissingProduces
	}
	// A CONSUMER-GROUNDED REFUSAL IS ITS OWN FINDING, AND IT IS SOURCED. There
	// is no span of the request to weigh, because nobody writes down that the
	// thing behind a name must keep answering to how it is used — the same
	// reason a regression carries no citation (FAILSAFE clause 2). So it says
	// what moved and who is still using it, in front of the judge's own words,
	// and it buys the repair round every other measured finding buys.
	if verdict.Consumer {
		if finding, ok := ConsumerFinding(quote, consumers); ok {
			failed.Consumers = []string{finding}
			failed.Sourced = true
			failed.Gaps = treeGapWords(verdict.File, finding+". "+gaps)
		}
	}
	// And the coverage question on this side too. A repair round is aimed at
	// the gap the gate NAMED, so a round bought for a missing branch name
	// closes the branch name and leaves every behaviour nothing checks exactly
	// where it was; ten gates across the s5 sweep failed and the question was
	// asked at none of them. The settlement adds its findings to this gap
	// rather than replacing it — see settleAcceptance.
	return settleAcceptance(ctx, settings, client, graph, node, evidence, grounds, workerModel, failed)
}

// faulted is the gate call that produced no verdict, reported as the fault it is.
//
// It is unjudged's opposite number and the whole of what this wave changed about
// the gate. The two failures it separates used to be one: a judge that could not
// be REACHED (weather, a 429, a dead endpoint) and a judge that answered with
// something no reader could parse. The first says nothing about the work and
// must not hold it hostage. The second is the gate not existing — and a gate
// that does not exist may not be the reason a run reports itself whole.
//
// It carries neither a pass nor a gap: there is no verdict to carry. Checked
// stays false, so nothing downstream manufactures verified evidence out of it,
// and Fault is what the delivery path reads to end the run PARTIAL with the
// reason in the stream rather than 0 with nothing.
func faulted(node store.Node, why string, err error) Judgment {
	note := why
	if err != nil {
		note += ": " + firstLine(err.Error())
	}
	log.Printf("note: the delivery gate faulted on %s — %s; the run is partial, not whole", node.ID, note)
	return Judgment{Fault: note}
}

const (
	gateUnreadableWhy     = "the gate answered with nothing this could read"
	gateFaultSentence     = "the review could not be read, so this delivery was never checked"
	gateFaultHandoverLead = "I'm handing this over unchecked: the review of it could not be read"
	gateFaultHandoverTail = ", so nothing has confirmed this is what you asked for."
)

// gateFaultReason keeps the part of a fault that distinguishes this run from
// another one. The shared lead already says the review was unreadable, so
// repeating the gate's equivalent lead would obscure the evidence that follows
// it. That evidence is already bounded by internal/shaped's replyDetailBytes
// and noteBytes; this reading does not invent another limit.
func gateFaultReason(fault string) string {
	reason := strings.TrimSpace(fault)
	if strings.HasPrefix(reason, gateUnreadableWhy) {
		reason = strings.TrimPrefix(reason, gateUnreadableWhy)
		reason = strings.TrimPrefix(reason, ": ")
	}
	return strings.TrimSpace(reason)
}

// GateFaultWords is the shortfall as the delivery gate's own ledger keeps it,
// and as the headless stream prints it. It says what did not happen — the check
// — and why it did not happen, never alleging anything about the work when the
// gate found nothing. An empty reason stays empty so an unknown renders as
// nothing rather than as a dangling separator.
func GateFaultWords(fault string) string {
	words := gateFaultSentence
	if reason := gateFaultReason(fault); reason != "" {
		words += " — " + reason
	}
	return words
}

// GateFaultHandover is the reservation that rides the delivery when the gate
// faulted.
//
// It exists for the same reason GapHandover does: a run that hands over work its
// own check never looked at must not hand it over in silence. What it must NOT
// say is that anything is wrong with the work — nobody knows, and that is the
// whole point — so it names the missing check and why it did not happen, never
// alleging anything about the work. An empty reason stays empty so an unknown
// renders as nothing rather than as an empty parenthetical.
func GateFaultHandover(fault string) string {
	words := gateFaultHandoverLead
	if reason := gateFaultReason(fault); reason != "" {
		words += " (" + reason + ")"
	}
	return words + gateFaultHandoverTail
}

// unjudged is the fail-open pass, said out loud.
//
// The behaviour is unchanged and deliberately so: a gate that cannot answer must
// not hold a finished deliverable hostage, so the work ships. What changes is
// that the pass is no longer indistinguishable from a verdict. It carries the
// reason on the judgement, so any surface holding one can tell "read and found
// whole" from "never read", and it is logged in the same register as everything
// else in this file that could not do its job — because a gate that has quietly
// stopped existing on the models most worth pointing it at is exactly the kind
// of failure nobody goes looking for until it has been true for a month.
func unjudged(node store.Node, why string, err error) Judgment {
	note := why
	if err != nil {
		note = gateNote(why, firstLine(err.Error()))
	}
	log.Printf("note: the delivery gate did not judge %s — %s; delivering unjudged", node.ID, note)
	return Judgment{Pass: true, Unjudged: note}
}

// GateUnreached is why a delivery went out with nothing having read it, in the
// words the door prints, the journal keeps and a rig reads back.
//
// IT IS THE FIRST CLAUSE OF THE NOTE AND NEVER THE WHOLE OF IT. What follows it
// — how the gate was asked, and the provider's own sentence — is detail a person
// may or may not need; this is the fact they are owed, and cmd/aforge/do.go
// prints it verbatim after "delivered without a check: ". Spelled once here
// because a sentence in two places is two sentences.
const GateUnreached = "the gate could not be reached"

// GateName is what the thing that judges a delivery is called, in the words a
// person reads and a machine reads back.
//
// Spelled once here for the reason GateUnreached above it is: a name in two
// places is two names, and the second one drifts. cmd/aforge's `--json`
// envelope publishes it as `judged_by`, docs/HEADLESS.md's contract table
// quotes it, and the chat manual answers "what checked my unattended run" with
// it — three readers, one string.
const GateName = "delivery gate"

// The three answers to "and what was done about it", which is the question a
// person reading an unchecked delivery asks second. One of them is always on the
// note: the gate was asked again, or there was no wall left to ask inside, or
// asking again would have bought the same refusal.
const (
	gateAskedTwice  = "asked twice"
	gateNoTimeToAsk = "the wall left no time for a second call"
	gateAskRefused  = "the request itself was refused, so asking again would say the same"
)

// gateSecondAskFloor is the least wall one more gate call is worth starting in.
//
// It is the smallest honest figure rather than a share of anything: a structured
// verdict over a whole deliverable is not a call that finishes in seconds, and
// starting one with less than this left buys a certain timeout in place of the
// run's remaining time. STATED ONCE — worthAskingAgain is its only reader.
const gateSecondAskFloor = 30 * time.Second

// worthAskingAgain answers whether a gate call that never landed is worth making
// once more, and NAMES ITS ANSWER in the words the note carries — so the reading
// and the sentence a person gets cannot drift apart.
//
// Two things say no. A refusal that is about the REQUEST will be refused the
// same way by every endpoint, and provider.APIError.OurRequest is the shape that
// says so — already the reading two other retry seams take (internal/exec and
// internal/session), so this is that question asked again rather than a second
// answer to it. And a wall with no room left for a call is a wall this gate may
// not spend on one: the run's remaining time belongs to the work.
//
// A context with no deadline at all has all the time there is, and is asked.
func worthAskingAgain(ctx context.Context, err error) string {
	if refusal, ok := provider.RefusalFrom(err); ok && refusal.OurRequest() {
		return gateAskRefused
	}
	if deadline, timed := ctx.Deadline(); timed && time.Until(deadline) < gateSecondAskFloor {
		return gateNoTimeToAsk
	}
	return gateAskedTwice
}

// gateNote joins an unjudged delivery's clauses into the one line the record
// keeps and the door prints.
//
// The separator is " · " rather than ": " because every clause after the first
// is an aside — how it was asked, what the provider said — and a colon would
// promise that what follows explains what precedes it. Empty clauses are dropped
// on the emptiness law: a run that has nothing to add says nothing.
func gateNote(clauses ...string) string {
	kept := make([]string, 0, len(clauses))
	for _, clause := range clauses {
		if clause = strings.TrimSpace(clause); clause != "" {
			kept = append(kept, clause)
		}
	}
	return strings.Join(kept, " · ")
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
// splice — so the substrings of that one string, and the files it names, are a
// fixed, finite set. Every citation a gap carries must be one of them. Round
// k+1 must carry at least one no earlier round spent, and spends every citation
// it carries. The number of unspent citations falls by at least one per
// admitted round, so the loop terminates on the content of the ask rather than
// on a counter.
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
func AdmitGapCitation(grounds Grounds, citations, spent []string) string {
	if refusal := admitGapCitations(citations, grounds); refusal != "" {
		return refusal
	}
	// The ledger keys per citation and not on the joined line, which is the one
	// thing the list changes about the argument above. Keyed on the line, a gap
	// naming four files already worked on and one fresh one would read as words
	// nobody had spent, and a gap naming one file already worked on beside a
	// fresh one would be refused wholesale — the first buys unbounded rounds,
	// the second abandons real work. Per citation, a round is admitted only if
	// it names something no earlier round did, and every citation it names is
	// spent by it.
	//
	// WHAT COUNTS AS SPENT IS DECIDED BY EVIDENCE, NOT BY A COUNT. SpentCitations
	// hands back only the words whose round left nothing new in the world; a
	// round that moved the tree and still did not close what it was aimed at
	// leaves its words unspent, and the growth journal's standstill and fixed
	// point are what stop the lineage after that. The two bounds divide the work
	// cleanly: this one bounds SCOPE against a finite ask, the journal bounds
	// REPETITION against measured change. A count of one standing in for the
	// second is how a run with eighty-six minutes and ninety-nine per cent of its
	// money left stopped holding a finding it agreed with.
	for _, citation := range trimmedCitations(citations) {
		if !citationSpent(citation, spent) {
			return ""
		}
	}
	return "the same words were already worked on once"
}

// citationSpent reports that an earlier round of this job already commissioned
// work against this citation. It asks the two questions the grounding rule
// asks, in the same order: the same words, or — when the citation is a file
// name — the same file under either spelling. The second is what keeps a round
// from being bought twice for one file by resolving its path in between.
func citationSpent(citation string, spent []string) bool {
	key := citationKey(citation)
	cited, isFile := citedFile(citation)
	for _, prior := range spent {
		if citationKey(prior) == key {
			return true
		}
		if !isFile {
			continue
		}
		if priorFile, ok := citedFile(prior); ok && namesSameFile(cited, priorFile) {
			return true
		}
	}
	return false
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
// It weighs the finding against the same Grounds the extension does, which for
// a while it did not: this door admitted the working method and the extension's
// did not, so a run could pay for a repair against a standard and then be told
// the same standard was an invention. ink s1 spent both of its gates that way
// and settled at four minutes of ninety.
//
// A refusal is not a pass. The gap is journaled, it is said in the thread, and
// it rides the delivery — it simply does not redo the work.
func AdmitGapRevision(grounds Grounds, citations []string) string {
	return admitGapCitations(citations, grounds)
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

// GapClosedNote is what a gap the run has already closed on disk gets instead of
// a round. It is GapNote's sibling and stops one sentence earlier on purpose:
// the standard was the person's own and it was met, so there is nothing to offer
// to redo — the file is there, and the honest thing is to say why the review's
// words are being delivered under rather than acted on.
func GapClosedNote(gaps, closed string) string {
	return "a review raised this: " + firstLine(gaps) +
		" — I've delivered as it stands, because " + closed + "."
}

func citationKey(text string) string { return strings.Join(strings.Fields(text), " ") }

// Extension is what a gate's judgement was allowed to do about a gap that
// survived the revision pass: the work it commissioned, the words it cited, the
// round it was, and — when nothing was commissioned — why, in the words the user
// would be told.
type Extension struct {
	Spliced int
	// Met says the one question was put at this door and came back yes: the
	// request, as the person wrote it, is satisfied by what is in hand, so no
	// remainder was bought and none was owed. Refused then carries the receipt
	// rather than a refusal, and Unclosed is false — the gap did not survive,
	// it was answered.
	//
	// The gate's own caller asks the same question one door earlier and passes
	// the delivery there, so it never sees this. It is here for the callers
	// that reach the extension without going through that door.
	Met bool
	// Quote is the citations as one line, and Citations is the list the
	// admission rule actually weighed. They travel together for the same reason
	// they do on a Judgment: the ledger that bounds the next round reads the
	// list, and everything that shows a person what was cited reads the line.
	Quote      string
	Citations  []string
	Round      int
	Refused    string
	Mechanical bool
	// Cause is the growth governor's machine-readable word for WHICH governor
	// refused, verbatim from resident.GrowVerdict, and empty when nothing
	// refused. Refused above is what a person reads; this is what a decision is
	// made from, and the two are separate fields because a decision read out of
	// a sentence is a decision that breaks when the sentence is reworded. See
	// resident.GrowthStopped, which is the only thing allowed to turn one of
	// these words into an ending.
	Cause string
	// Unclosed says the gap is still open: the repair was refused for want of
	// money, rounds or a planner, rather than because the gap itself was found
	// inadmissible. See store.DeliveryGate.Unclosed for why the difference is
	// the one the exit code reads.
	Unclosed bool
	// AN EXTENSION CANNOT ACQUIT. There is no Overturned here and there must
	// not be one: everything this function can learn is whether a round was
	// bought, and a round nobody bought says nothing whatever about whether the
	// finding was right. Only the two world-doors — AdmitGapArtifact and
	// AdmitGapPresent, which read the disk and the delivered text — and a
	// person may set store.DeliveryGate.Overturned. See coverageRefused for the
	// run that shipped a dead behaviour on the reasoning this comment replaces.
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
// records, when there are any, are files the finished work left behind that the
// remainder must READ rather than reuse — the text of the change it made, above
// all. They travel apart from the artifact list because a remainder handed only
// paths cannot learn what the work it is continuing actually did, and a leaf
// asked to state such a fact with no way to obtain it states something else.
func ExtendForGap(ctx context.Context, graph *store.Store, node store.Node, partial string,
	unmet Judgment, artifacts []string, dailyBudgetUSD float64,
	planRemainder resident.OverrunPlanFunc, records ...string) Extension {
	base, round := resident.OverrunLineage(node.ID)
	// The grounds ride on the judgement, which is where the gate assembled them.
	// A judgement built by a caller that predates them — or by one holding only a
	// gap and a citation — still gets the ask, because the node carries it and
	// the store stamps the same verbatim intent on every node of every splice.
	// One value, filled from one place, so the two doors cannot drift apart
	// again.
	grounds := unmet.Grounds
	if strings.TrimSpace(grounds.Intent) == "" {
		grounds.Intent = node.Provenance.Intent
	}
	cited := unmet.Cited()
	extension := Extension{Quote: joinCitations(cited), Citations: cited, Round: round + 1,
		Mechanical: unmet.Mechanical}
	// A BROKEN CONSTRAINT NEVER BUYS A ROUND, and the refusal comes before
	// anything is planned so that it costs nothing at all.
	//
	// Every other finding here is something more work could close: a file that
	// is not on disk, a behaviour nothing exercises, a check the run turned red.
	// This one says the run did what the person forbade, and a remainder planned
	// to close it is one more worker inside the same workspace with the same
	// permission the last one abused. #427 is that mechanism measured: the
	// remainder spliced for a coverage finding wrote the file the person had
	// said not to write.
	if len(unmet.Constraint) > 0 {
		extension.Refused = "the work broke a rule the person set, and no round is bought to close that"
		extension.Unclosed = true
		return extension
	}
	if graph == nil || planRemainder == nil {
		extension.Refused, extension.Unclosed = "there is nothing here that could plan the rest", true
		return extension
	}
	// Admissibility is decided before any planning call: an ungrounded gap must
	// cost nothing at all, or the refusal is only a refusal to splice what has
	// already been bought.
	//
	// A finding the run MEASURED skips the question entirely. The invariant asks
	// whose words a finding is a failure of, and a check that passed before the
	// work and fails after it is nobody's words — it is the world, reported. See
	// Judgment.Sourced.
	if !unmet.Sourced {
		if refusal := AdmitGapCitation(grounds, extension.Citations, SpentCitations(graph, base)); refusal != "" {
			// A MEASURED FINDING RIDING ON A REFUSED ONE IS STILL A MEASURED
			// FINDING. The citations weighed here are the judge's, and refusing
			// them says the judge's words were not the person's. It says nothing
			// whatever about a coverage gap the run MEASURED, which has no
			// citation to weigh and is admitted with none — so a round is still
			// bought, aimed at the half that survives. Without this, igel s6's
			// three unexercised behaviours died with a refusal of a sentence
			// about something else entirely.
			if len(unmet.Unexercised) == 0 && len(unmet.Unasserted) == 0 {
				extension.Refused = refusal
				return extension
			}
			unmet = unmet.measuredHalf()
			extension.Quote, extension.Citations = unmet.Quote, unmet.Cited()
			extension.Mechanical = false
		}
	}
	// ROOM, NOT ROUNDS. A repair the wall will kill mid-flight spends money to
	// deliver nothing, and there is no honest way to call the result whole. The
	// floor is derived rather than typed: a repair is about the size of the
	// attempt that produced the finding, so the run must still hold at least
	// that much wall. A run whose deadline nobody set, or whose attempt was
	// never timed, is not refused on a clock it cannot read — the fail-safe
	// direction here is to try, because the alternative is the defect this whole
	// change is about: eight runs that stopped at a tenth of their wall by
	// choice. See PERF.md and docs/design/gate/SETTLEMENT.md §3.
	// A REQUEST ALREADY SATISFIED BUYS NO ROUND. It is asked here, before the
	// wall is weighed and before anything is planned, because a run that has
	// done what was asked should end saying so rather than end saying it ran out
	// of clock. See satisfied.go: it is one call, at the one moment a round
	// would otherwise be bought, and never where the gate's own caller has
	// already put it.
	if settled, met := metExtension(ctx, extension, unmet); met {
		return settled
	}
	if refusal := outOfWall(ctx, node); refusal != "" {
		extension.Refused, extension.Unclosed = refusal, true
		return extension
	}
	// The reason travels rather than being inherited: this is quality failure
	// growing a job, not resource failure, and the journal that bounds growth
	// could not tell the two apart while one borrowed the other's whole path.
	spliced, _, cause, err := resident.ReplanOverrunAs(ctx, graph, node, partial, unmet.Gaps, artifacts,
		dailyBudgetUSD, resident.Growth{Reason: resident.GrowGap, Records: records,
			// A finding that names a file of the record is a reading of the
			// world, so the first round it buys is not the coverage question's
			// to refuse. See resident.GrowRequest.Grounded.
			Grounded: strings.TrimSpace(unmet.File) != ""}, planRemainder)
	if err != nil {
		log.Printf("note: could not plan the rest of %s: %v", node.ID, err)
		extension.Refused, extension.Unclosed = "the work that would close it could not be planned", true
		return extension
	}
	extension.Cause = cause
	if spliced == 0 {
		// A governor has already said so in the thread in its own words, or the
		// rail has journaled the repair and is waiting on consent. Either way
		// nothing new is running and the delivery has to say so.
		//
		// AND THE COVERAGE GOVERNOR SAYS SO IN ITS OWN WORDS, STILL UNCLOSED.
		// The cause is read from the journal rather than threaded back through
		// four signatures, because the journal is where it is already written
		// down and a fact carried twice is a fact that will differ.
		if coverageRefused(graph, base) {
			extension.Refused, extension.Unclosed = "the job's own reading of what it is "+
				"judged on found nothing left to add, so nothing further was started", true
			return extension
		}
		// AND THE TWO GOVERNORS THAT READ THE WORLD SAY IT IN THEIR OWN WORDS.
		// "no more work could be started on it" is true of a cap, a wall and a
		// planner that came back empty, and it is the wrong account of a job
		// that has concluded nothing is changing — which is a finding about the
		// work rather than about what is left to spend on it.
		if words, stopped := resident.GrowthStopped(cause); stopped {
			extension.Refused, extension.Unclosed = words, true
			return extension
		}
		extension.Refused, extension.Unclosed = "no more work could be started on it", true
		return extension
	}
	extension.Spliced = spliced
	return extension
}

// coverageRefused reads the growth journal for the refusal that has just
// happened and answers whether it was the coverage question, so the delivery
// can say which governor declined in that governor's own terms.
//
// A GOVERNOR MAY REFUSE A ROUND AND NEVER A FINDING. It used to set Overturned
// here, on the reasoning that coverage is a broader measurement than one
// review's finding and the broader one settles it. That reasoning was wrong at
// the root: coverage is a MODEL'S READING OF THE JOB'S OWN ACCOUNT — the plan's
// Done and the workers' own summaries — and the finding is a reading of the
// world. A run answered "the job's own reading of what it is judged on found
// nothing left uncovered, so the review's finding is what was wrong" over a
// behaviour that was genuinely dead in the delivered tree, and shipped it
// (2026-09-01, deepseek-v4-flash, a real issue as the brief). So a refusal here
// leaves the gap exactly where the rounds cap, the wall and the rail leave it:
// Unclosed, which is the field the exit code turns on. Overturned is reserved
// for AdmitGapArtifact, AdmitGapPresent and a person — the three doors that
// weigh a finding against the world.
//
// A journal that cannot be read answers false, which costs only the more
// particular sentence: the delivery is partial either way.
func coverageRefused(graph *store.Store, lineage string) bool {
	if graph == nil {
		return false
	}
	growths, err := graph.JobGrowths(jobRootOf(lineage))
	if err != nil || len(growths) == 0 {
		return false
	}
	last := growths[len(growths)-1]
	return !last.Allowed && last.Cause == resident.CauseCovered
}

// jobRootOf is the job the growth journal is kept under, from a lineage id. A
// lineage is the job root with a round suffix, and OverrunLineage is the law
// that strips it — asked here of the base rather than re-derived, so one rule
// answers it everywhere.
func jobRootOf(lineage string) string {
	root, _ := resident.OverrunLineage(lineage)
	return root
}

// SpentCitations is the ledger: the spans of the ask that earlier rounds of this
// job already commissioned work against AND GOT NOTHING FOR. A read failure
// returns nothing, which is the fail-safe direction for a bound on new work only
// in company with the round cap — which is exactly what that cap is for.
//
// The second half of that sentence is the change, and it is what turns a count
// into a measurement. Spending words on a round that moved nothing is what the
// bound exists to stop happening twice; spending them on a round that rewrote
// half the repository and still left the thing genuinely undone is the system
// working, and refusing the next round over it is a count of one wearing an
// invariant's clothes. The growth journal already records, per round, how many
// files the work actually left behind (store.JobGrowth.Produced, with Measured
// saying somebody looked) — so the evidence exists and was simply not being
// read here.
//
// EVERYTHING UNKNOWN IS SPENT. A round with no journal row, a row nobody
// measured, or a row that measured zero all leave their citations on the ledger.
// That keeps the bound's direction unchanged wherever the evidence is missing,
// and it means this can only ever release words the journal positively says were
// productive.
func SpentCitations(graph *store.Store, baseID string) []string {
	if graph == nil {
		return nil
	}
	gates, err := graph.DeliveryGateLineage(baseID)
	if err != nil {
		log.Printf("note: could not read the gap ledger for %s: %v", baseID, err)
		return nil
	}
	productive := productiveRounds(graph, baseID)
	// Flattened across gates, because the ledger is a set of citations and not
	// a set of rounds: what bounds the next round is which of the ask's words
	// and files have already been worked on, whichever round spent them.
	var spent []string
	for _, gate := range gates {
		if gate.Extended && !productive[gate.Round] {
			spent = append(spent, gate.Cited()...)
		}
	}
	return spent
}

// productiveRounds names the rounds of this lineage that the growth journal says
// left something new in the world.
//
// It reads the journal the growth governor writes, keyed by the job root, and
// keeps only the rows for this lineage: a sibling lineage's productivity is not
// evidence about this one, and a bound that borrowed it would let one branch of
// a job buy the other's rounds. A read that fails, or a job with no journal at
// all, answers "nothing was productive", which leaves every citation spent.
func productiveRounds(graph *store.Store, baseID string) map[int]bool {
	growths, err := graph.JobGrowths(baseID)
	if err != nil {
		log.Printf("note: could not read the growth journal for %s: %v", baseID, err)
		return nil
	}
	productive := make(map[int]bool, len(growths))
	for _, growth := range growths {
		if lineage := strings.TrimSpace(growth.Lineage); lineage != "" && lineage != baseID {
			continue
		}
		if growth.Allowed && growth.Measured && growth.Produced > 0 {
			productive[growth.Round] = true
		}
	}
	return productive
}

// outOfWall answers whether this run still holds enough time to be worth buying
// a repair in, and names the shortfall in the words the person is told.
//
// The measure is the attempt that produced the finding: a repair aimed at one
// gap is about the size of the work that left the gap, so a run that cannot
// afford that much again cannot afford the repair. Both halves are read off
// things that already exist — the errand's own deadline, which the headless run
// sets from --timeout, and the node's start, which the store stamps — so nothing
// here is a new knob and nothing is a typed number. An unknown answers empty,
// which buys the round: this is a floor under settlement, never a new reason to
// settle early.
func outOfWall(ctx context.Context, node store.Node) string {
	deadline, ok := ctx.Deadline()
	if !ok || node.StartedAt.IsZero() {
		return ""
	}
	attempt := time.Since(node.StartedAt)
	if attempt <= 0 || time.Until(deadline) >= attempt {
		return ""
	}
	return "there is not enough time left on the run to finish it"
}

// remainderPrompt SIZES the remainder. It does not settle the node, and the
// prompt no longer talks as though it might.
//
// It used to be shown the brief and the worker's last paragraph and nothing
// else, and it was told in as many words that "exhaustion is not evidence of
// incompleteness" — so a leaf cut off mid-edit with a red build was judged done
// on its own closing sentence, "All 722 tests pass. Let me verify the dry-run
// tests specifically:". It is now shown what it is judging: that the worker was
// cut off, how far it got, the turns it took, the change it actually made, and
// what the project's own checks said. Exhaustion is still not proof of an
// unfinished result — a leaf often lands inside its reserve — but it is a fact
// about the run, and a judge that is not told it is guessing.
//
// The judgment is against the leaf's own brief, not the job's intent: a
// mid-graph leaf that inventoried a folder is done when the inventory is done,
// even though the job it serves is not.
const remainderPrompt = `A worker was stopped mid-assignment because it ran out of the room it was given. You size what is left, so the next worker can be aimed at it.

You receive the assignment, what the worker had produced when it stopped, how it was stopped, the change it actually made, and what the project's own checks said. Judge one question: what, if anything, of the ASSIGNMENT is not yet in the produced result?

The worker's own account of itself is not evidence. "All tests pass" is a sentence, written by the worker that was cut off, about checks it wrote itself; the reading of the project's checks and the change itself are the evidence. Where the two disagree, the reading wins.

Return exactly one JSON object, nothing else:
{"done": true} when every element of the assignment is present in the result and the evidence agrees.
{"done": false, "remaining": "<the unfinished work>"} when you can name a specific element of the assignment that is absent or unfinished — concretely enough that a worker could finish from your words alone. Do not invent work the assignment never asked for.`

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
}

// RemainderVerify is what a continuation is aimed at when the judge found
// nothing left to write.
//
// A CUT LEAF STILL GETS THE TURN ITS EXHAUSTION BOUGHT AWAY. The turn a leaf is
// stopped on is the one where it would have run what it wrote and read the
// result, and that is exactly where a run's fatal defect surfaces: the leaf that
// prompted this was cut off one turn before running the binary, over a blocker
// that was discarded at construction. So a "done" on a cut leaf buys one cheap
// leaf that checks, rather than a tick. Where the judge was right it costs a
// verification; where it was wrong it is the missing turn.
const RemainderVerify = "The previous worker was stopped before it could check its own work. " +
	"Run what it wrote, read the result, and fix only what that reveals. Do not start anything new."

// judgeRemainder decides whether an exhausted leaf actually left work behind.
// Failures fail toward "not done" with Checked false: the continuation still
// runs, now bounded by the overrun governors, rather than a judge outage
// silently shipping genuinely cut-off work as finished.
// It takes no Option: nothing in this prompt is clipped here, so there is no
// window-derived bound for one to move. What it does share with the other two is
// the completion cap and the empty-reply retry, both of which are facts about
// the reply rather than about the window.
func JudgeRemainder(ctx context.Context, settings config.Config, client *pool.Client, graph *store.Store,
	node store.Node, produced string, evidence Evidence, workerModel string,
) Remainder {
	body := "The assignment:\n" + node.Brief + "\n\nProduced before stopping:\n" + produced +
		remainderSubject(graph, node, evidence)
	judgeCtx := settings.Context(router.WithAvoidModel(ctx, workerModel), "remainder")
	judgeCtx = provider.WithCall(judgeCtx, provider.ClassPlanAudit)
	// "gate", not the routing class's own "audit": the class pools this call
	// with the planner's audits because they rate alike, and the model-call log
	// is read by a person who wants to know which of them refused a deliverable.
	judgeCtx = provider.WithCallTag(judgeCtx, "gate")
	// Like the delivery gate, the judgment is part of what this leaf cost.
	judgeCtx = pool.WithSpendNode(judgeCtx, node.ID)
	// And on the log, for the reason the delivery gate names its own node.
	judgeCtx = provider.WithCallNode(judgeCtx, node.ID)
	var verdict struct {
		Done      bool   `json:"done"`
		Remaining string `json:"remaining"`
	}
	// This used to read its own reply first brace to last brace, which is
	// tolerant in the same direction as the shared extractor and wrong in one: a
	// judge that wrote a sentence containing a brace after its object swallowed
	// the sentence into the JSON and failed the parse. It goes through the one
	// seam now, which also means a cut answer is continued rather than lost.
	if _, err := askVerdict(judgeCtx, client, []ai.Message{
		{Role: "system", Content: []ai.ContentPart{{Type: "text", Text: remainderPrompt}}},
		{Role: "user", Content: []ai.ContentPart{{Type: "text", Text: body}}},
	}, remainderSchema, &verdict); err != nil {
		// FAILURES FAIL TOWARD "NOT DONE" WITH Checked FALSE, unchanged: the
		// continuation still runs, bounded by the overrun governors, rather than
		// a judge outage silently shipping genuinely cut-off work as finished.
		// That is the safe direction here and it is why this one does not fault
		// the way the delivery gate does — nothing is being called whole.
		if shaped.Unreadable(err) {
			provider.Report(judgeCtx, provider.VerdictFormatFailure)
		} else {
			provider.Report(judgeCtx, provider.VerdictProviderFailure)
		}
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
	return Remainder{Done: verdict.Done, Remaining: remaining, Checked: true}
}

// remainderSubject is what the remainder judgement is SHOWN besides the brief
// and the words the worker left behind.
//
// Four facts, and none of them is the worker's account of itself: that it was
// cut off and by what, the turns it actually took, the change it made, and what
// the project's own checks said. Every one of them was already in the record and
// none of them reached this call — which is how a leaf stopped mid-edit with
// `undefined: logf` in its build was judged done on the sentence "All 722 tests
// pass. Let me verify the dry-run tests specifically:".
//
// A fact that cannot be read renders NOTHING rather than an empty heading: a
// judge told "the change:" followed by nothing reads it as a run that changed
// nothing, which is the direction that acquits.
func remainderSubject(graph *store.Store, node store.Node, evidence Evidence) string {
	var body strings.Builder
	if graph != nil {
		if record, ok, err := graph.LeafExhaustedFor(node.ID); err == nil && ok {
			body.WriteString("\nHOW IT STOPPED. It did not choose to stop: " +
				strings.TrimSpace(record.Reason) + ".\n")
			if record.Turns > 0 {
				fmt.Fprintf(&body, "It had taken %d turns when it was stopped.\n", record.Turns)
			}
		}
		// The run's own turns, oldest first, which is the only account of what
		// the attempt did that the attempt did not write about itself.
		if banked, turns := resident.BankedRun(graph, node.ID); turns > 0 && strings.TrimSpace(banked) != "" {
			body.WriteString("\n" + banked + "\n")
		}
	}
	if patch := evidence.patchBlock(ctxbudget.Budget{}); patch != "" {
		body.WriteString("\n" + patch)
	}
	if reading := evidence.readingBlock(); reading != "" {
		body.WriteString("\n" + reading)
	}
	return body.String()
}

// deliveryPartialBytes is what the partial handed to a judgement is bounded to
// when the window is unknown. It is the same bound the delivery path uses —
// what a message can carry, less the room a sentence is posted with — which is
// a fact about the transport and was standing in for a fact about the prompt.
// This partial is never posted anywhere; it is read by a model, so a known
// window bounds it by what that model can hold and this literal is only the
// answer for a build that does not know.
//
// It is a fallback and not a floor, which is the difference between it and the
// run tail above. On a window whose reserve leaves a small pot, fifteen
// kilobytes of one block is most of the prompt — the partial would crowd out
// the assignment it is being read against — so a known window that says less is
// believed. The run tail is floored instead because it is counted in lines and
// a handful of them convicts nothing: six recorded tool calls are worse to
// judge against than the twenty-four this always sent.
const deliveryPartialBytes = store.MaxMessageBytes - 1<<10

func boundedDelivery(text string, budget ctxbudget.Budget) string {
	return clipUTF8Bytes(strings.TrimSpace(text),
		budget.Share(gatePartialShare, gateShareTotal, deliveryPartialBytes))
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

// RetargetSpec carries a failed node's spec onto the node that stands in for it.
//
// This is the §6 defect closed structurally. The retry path used to author a
// brand-new node out of failure context — the sentinel names a title and a
// summary, and the store's brief falls back to those two lines — so the module
// name, the filename, the type names and the acceptance check the original spec
// carried were all simply gone by the second attempt. Nothing was truncating
// them: nothing was carrying them.
//
// The rule the object makes enforceable is that a re-target may re-aim and may
// not re-author. Done and Sources travel verbatim, because they are what the
// work is judged against and what it must touch, and neither changed when the
// attempt failed. Method travels too, unless the replacement brought its own.
// Only Instruction is written to, and only by addition: the original words,
// then what happened, then what is now being asked for on top of them.
func RetargetSpec(original plan.Spec, aim, failure string) plan.Spec {
	if original.Empty() {
		// Nothing to carry. The caller falls back to whatever it did before
		// specs existed, which is the byte-identical path.
		return plan.Spec{}
	}
	retargeted := original
	var instruction strings.Builder
	instruction.WriteString(strings.TrimSpace(original.Instruction))
	instruction.WriteString("\n\nA previous agent was given exactly this work and did not finish it.")
	if failure = strings.TrimSpace(failure); failure != "" {
		instruction.WriteString(" It stopped like this: ")
		instruction.WriteString(firstLine(failure))
	}
	if aim = strings.TrimSpace(aim); aim != "" {
		instruction.WriteString("\n\nWhat this attempt is being asked to do differently: ")
		instruction.WriteString(aim)
	}
	instruction.WriteString("\n\n")
	instruction.WriteString(resident.SpecUnchangedNotice)
	retargeted.Instruction = strings.TrimSpace(instruction.String())
	return retargeted
}

// RetargetAdds re-aims a failed node's spec onto every node the sentinel added
// after it failed, before those nodes are mirrored into the store.
//
// It runs on the plan document, which is where a node's spec lives and where
// the sentinel has just written its additions, and it writes three fields on
// each: the spec itself, the brief that is still the executor's read, and the
// working method, which a replacement inherits for the same reason it inherits
// the criterion — how this kind of work is done well is a fact about the work,
// not about the attempt.
//
// It is deliberately narrow. Only a node the sentinel added while reacting to a
// FAILED node is a replacement; a node added because a landed result taught the
// job something new is new work, and giving it someone else's criterion would
// be inventing a requirement rather than preserving one.
func RetargetAdds(planGraph *plan.Graph, failed *plan.Node, operations []plan.Operation) int {
	if planGraph == nil || failed == nil || failed.Spec.Empty() {
		return 0
	}
	carried := 0
	for _, operation := range operations {
		if operation.Op != "add" || !operation.Applied {
			continue
		}
		node := planGraph.Node(operation.Node)
		if node == nil || node.ID == failed.ID {
			continue
		}
		aim := strings.TrimSpace(node.Summary)
		if aim == "" {
			aim = strings.TrimSpace(operation.Reason)
		}
		node.Spec = RetargetSpec(failed.Spec, aim, failed.Failure)
		// Brief is still the read this release, so the carried instruction has
		// to land there too or the object would be durable and unread — the
		// replacement would go on running from the sentinel's two lines.
		node.Brief = node.Spec.Instruction
		if strings.TrimSpace(node.Contract) == "" {
			node.Contract = node.Spec.Method
		}
		carried++
	}
	return carried
}

// PlanNodeFor finds the plan node one store node was minted from.
//
// The mapping is the id scheme and nothing else: a job's nodes are minted as
// "<prefix>-n<planID>", except the sink, which takes the bare prefix. A sink
// cannot be resolved back this way — the bare prefix names no plan id — so it
// returns nothing rather than guessing, and a caller that finds nothing does
// what it did before specs existed.
func PlanNodeFor(planGraph *plan.Graph, prefix, nodeID string) *plan.Node {
	if planGraph == nil {
		return nil
	}
	for index := range planGraph.Nodes {
		candidate := &planGraph.Nodes[index]
		if fmt.Sprintf("%s-n%d", prefix, candidate.ID) == nodeID {
			return candidate
		}
	}
	return nil
}

// measureFinalTree fills in the after half of the verification photograph when
// nobody else did, and does nothing at all otherwise.
//
// The three conditions are each a refusal to invent a measurement. Without a
// before reading there is nothing to subtract from, and a second reading alone
// would report the repository's own pre-existing reds as this work's doing —
// the failure that threw away a correct fix to spf13/cobra once. Without a
// workspace there is nowhere to run. And with an after reading already taken,
// re-running the suite would spend an eighth of a wall to learn what is already
// known.
//
// It is a measurement and never a gate: a command that will not run, an
// entrypoint that vanished, or a ceiling that fires all leave the evidence
// exactly as it arrived.
//
// job is the request this tree is being changed for, and it is here for the one
// question that has to be asked before the suite is: HAS ANYTHING HAPPENED SINCE
// SOMEBODY LOOKED. Where the job has produced or changed no file since its
// reading was taken, the tree in front of this gate is the tree in that reading,
// and running the suite again spends an eighth of a wall to reproduce a roster
// the evidence is already carrying. See verify.TreeState.
func (e *Evidence) measureFinalTree(ctx context.Context, job string) {
	reading := e.Verification
	if !reading.Taken || reading.AfterTaken || strings.TrimSpace(e.Workspace) == "" {
		return
	}
	if verify.TreeUnchangedSince(e.Workspace, job, verify.TreeState(e.Workspace, e.Artifacts)) {
		if settled, unchanged := reading.OnAnUnchangedTree(); unchanged {
			e.Verification = settled
		}
		return
	}
	// The SAME strategy the first reading was taken with, pinned rather than
	// re-derived. Two readings taken with two different commands subtract to
	// noise, and re-deriving here would let a worker that edited its own test
	// script choose what the gate's reading measures.
	//
	// It is widened by exactly one thing, and only outward: the check files the
	// run itself left behind, taken from the record of the tree rather than from
	// anybody's account of what was tested. A scope is chosen before the work
	// exists and so can never hold a test the work wrote — igel's s8 read the two
	// checks its one touched file already had and never the forty the run put in
	// a new file — and a checklist point exercised only by such a check is
	// unexercised for as long as it is out of scope. Widening cannot forge a
	// regression: Reading.Regressed counts only checks the before roster
	// reported green, and these did not exist then.
	strategy := reading.Before.Strategy
	switch widened, added := strategy.WithChangedWork(e.Workspace, e.Artifacts); {
	case added:
		strategy = widened
	case strategy.Scope == verify.ScopeWhole && reading.Partial:
		// A whole reading that was killed at its ceiling will be killed again.
		// The diff is the reading that fits, and the roster is what this gate
		// came for. See verify.ChangedWorkStrategy.
		if narrowed, ok := verify.ChangedWorkStrategy(e.Workspace, reading.Plan, e.Artifacts); ok {
			strategy = narrowed
		}
	}
	after, ok := verify.RunReading(ctx, e.Workspace, strategy, reading.Budget)
	if !ok || after.TimedOut {
		return
	}
	reading.After, reading.AfterTaken = after, true
	e.Verification = reading
	if len(e.Regressed) == 0 {
		e.Regressed = reading.Regressed()
	}
}

// readingBlock is what the project's own verification said, stated as the
// measurement it is.
//
// It names the command, its exit status and the size of its roster, and it says
// out loud when nothing was measured — because "the suite is green" and "nobody
// ran the suite" are the two readings a deliverable's own sentence about its
// tests is equally happy to produce, and a judge that cannot tell them apart
// weighs a claim in place of a fact.
func (e Evidence) readingBlock() string {
	reading := e.Verification
	if !reading.Taken {
		return ""
	}
	result, when := reading.Before, "before the work"
	if reading.AfterTaken {
		result, when = reading.After, "on the finished tree"
	}
	var body strings.Builder
	body.WriteString("What the project's OWN verification said when it was run " + when +
		". This is the measurement; anything the deliverable says about its tests is a claim:\n")
	fmt.Fprintf(&body, "`%s` exited %d, and named %d checks",
		result.Entrypoint.Command, result.Exit, len(result.Reported))
	if failing := len(result.Failing); failing > 0 {
		fmt.Fprintf(&body, ", %d of them failing", failing)
	}
	body.WriteString(".\n")
	if !reading.AfterTaken {
		body.WriteString("The finished tree was NOT measured — this reading is of the " +
			"repository as the work found it.\n")
	}
	return body.String()
}

// removedChecks names the check declarations the work's own diff takes away. It
// is the cheap half of the disappearance evidence — it needs no run at all — and
// it is empty for every worker that derives no diff, which reads as no claim.
func (e Evidence) removedChecks() []string {
	patch := e.patchSource()
	if patch == "" {
		return nil
	}
	_, removed := verify.PatchChecks(patch)
	return removed
}
