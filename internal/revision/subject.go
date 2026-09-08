package revision

import (
	"context"
	"encoding/json"
	"fmt"

	"os"
	"path/filepath"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/Agent-Field/aforge-v2/internal/config"
	"github.com/Agent-Field/aforge-v2/internal/ctxbudget"
	"github.com/Agent-Field/aforge-v2/internal/plan"
	"github.com/Agent-Field/aforge-v2/internal/provider"
	"github.com/Agent-Field/aforge-v2/internal/provider/pool"
	"github.com/Agent-Field/aforge-v2/internal/shaped"
	"github.com/Agent-Field/aforge-v2/internal/store"
	"github.com/Agent-Field/aforge-v2/internal/verify"
	"github.com/Agent-Field/agentfield/sdk/go/ai"
)

// ── WHAT THE GATE IS JUDGING ─────────────────────────────────────────────────
//
// The delivery gate used to have exactly one answer to that question, and it was
// the wrong one wherever a run had changed a repository: the deliverable was the
// worker's final MESSAGE, fenced, and everything the run had actually done sat
// below it as a record the judge was invited to check the message against.
//
// That arrangement is FAILSAFE.md rule 2 read backwards — the evidence sourced
// FROM THE COMPONENT BEING CHECKED — and the run that made it undeniable is
// bench/deepswe textual-richlog-follow-state, nemotron-3.5-lightning, n1. The
// worker answered the delivery fence with a structured object,
// `{"contract": "…"}`, which is a quirk of one model and nothing else. Three
// gates in a row then reasoned about that object:
//
//	"The deliverable is a single JSON contract string, not the required Python
//	 source files. The fenced text between BEGIN DELIVERABLE and END DELIVERABLE
//	 contains only {"contract": "…"} — no _log.py, _rich_log.py …"
//
// while forty-two kilobytes of changed Python sat in the worktree and the
// artifact record named every file of it. The judge convicted the sentence and
// never once looked at the tree; the run ended partial at a cost of $0.46, and
// nothing in the store said that what had been judged was a sentence.
//
// So the subject is a property of the RUN and not of the prompt's layout:
//
//	THE DELIVERABLE OF A REQUEST THAT CHANGED THE TREE IS THE TREE. The worker's
//	final message is its CLAIM about that change, and a claim is read beside the
//	thing it is about, never in place of it.
//
// Which is docs/design/gate/SETTLEMENT.md §6 stated once more at the other door.
// §6 already settled that the delivered text may overturn a finding ONLY where
// the text is the whole of what the run left behind; this is the same rule
// applied one step earlier, to what the judge is handed in the first place.
// Where a run left nothing behind, the message IS the artifact and the fence
// holds it exactly as it always did — that is the question answered in prose,
// and nothing about it moves.

// Subject is what the delivery gate held between its fence markers.
//
// It is a recorded fact rather than an inference for the reason every other
// field on store.DeliveryGate is one: an autopsy asking "what did this gate
// actually read" has nothing else to go on, and the three refusals above are
// indistinguishable, afterwards, from three refusals over a real reading of the
// world.
type Subject string

const (
	// SubjectTree: the run changed files, and those files are the deliverable.
	SubjectTree Subject = "tree"
	// SubjectClaim: the run left nothing behind, so the message is the artifact
	// and the message is what was judged.
	SubjectClaim Subject = "claim"
)

// SubjectFallbackWords is what the record says when the tree contract could not
// be answered and the delivery was judged under the claim contract instead — or
// when no verdict could be read at all and the mechanical gate settled it.
//
// It replaces the subject rather than joining it because the question the field
// answers is "what was this gate holding", and under the fallback the answer is
// neither `tree (n files)` nor `claim`: it is a judge that was shown the tree and
// held to no enum.
const SubjectFallbackWords = "fallback"

// Subject answers which of the two this delivery is, from the artifact record
// settled against the world.
//
// The stat in recordFiles makes the run's record an observation of the tree
// rather than only an account of what leaves reported. The named-file sweep
// would make that observation a lie if it joined the record: a file the request
// named is not a file the run changed, so that answer is kept in Swept instead.
// A record naming files none of which are on disk is a record of nothing, and it
// answers claim: the fail-safe direction here is the one that keeps the worker's
// own words in front of the judge when there is nothing else to show it.
func (e Evidence) Subject() Subject {
	if len(e.recordFiles()) > 0 {
		return SubjectTree
	}
	return SubjectClaim
}

// SubjectWords is the subject as the record keeps it and a person reads it:
// what was judged, and how much of it. "tree (6 files)" and "claim" are the two
// shapes, and the count is there because a tree of one file and a tree of forty
// are different runs and the same word.
func (e Evidence) SubjectWords() string {
	files := e.recordFiles()
	if len(files) == 0 {
		return string(SubjectClaim)
	}
	return fmt.Sprintf("%s (%s)", SubjectTree, plural(len(files), "file"))
}

// plural is this package's one number-and-noun, kept here beside its only
// caller. It exists because "1 files" in a record is the kind of thing that
// makes a reader doubt the number as well as the noun.
func plural(count int, noun string) string {
	if count == 1 {
		return "1 " + noun
	}
	return strconv.Itoa(count) + " " + noun + "s"
}

// recordFiles is the artifact record settled against the disk: every recorded
// path that is a file, now, at judging time.
//
// The stat is the whole of it. A path a leaf reported and the filesystem does
// not have, a directory wearing a file's name, and a file the request named but
// the run never wrote are all things a record can SAY that the world does not
// bear out — and a subject decided from what the record says would be the same
// defect one seam along from the one this file closes.
func (e Evidence) recordFiles() []string {
	held := make([]string, 0, len(e.Artifacts))
	for _, artifact := range e.Artifacts {
		if info, err := os.Stat(artifact); err == nil && !info.IsDir() {
			held = append(held, artifact)
		}
	}
	return held
}

// gateTreeShare is what a known window spends on the tree the gate is judging,
// and gateTreeBytes is the bound when the window is unknown. Both are documented
// in PERF.md.
//
// It is the largest single share in the gate's prompt because it is the only
// block in it that IS the deliverable — everything else there is a record about
// one, and a judge given a generous record and a clipped subject is a judge
// reading the commentary instead of the work. The unknown-window figure is
// deliberately below the patch block's: a build with no catalog has always been
// able to hold a diff, and this is a second reading of the same change.
const (
	gateTreeShare = 25
	gateTreeBytes = 8 << 10
)

// NO FILE IS EVER SHOWN IN PART. A file's contents appear in full or the file
// appears by name only, and the block says which. That rule is the whole of two
// measured refusals:
//
//	"src/circuit-breaker.ts — The deliverable does not include the actual
//	 content of the circuit breaker module … the fenced text shows only a
//	 truncated excerpt ending mid-sentence"    (ofetch v4-flash s12, 4/47)
//	"src/textual/widgets/_rich_log.py — The file is truncated — it cuts off
//	 before the implementation of write(expand=True) …"  (textual v4-flash s12, 15/20)
//
// Both are true of the READING and false of the run — the files were whole on
// disk and the changes were large — and both were reached the same way: a
// reader shown the opening of a file judged the opening. Saying "this is an
// excerpt" in front of it was tried and is not enough; a model reading source
// that stops mid-function concludes the source stops mid-function.
//
// So the percept is removed rather than annotated. A file shown whole cannot
// read as truncated, and a file shown only by name cannot read as truncated
// either — it reads as what it is, a file on disk that there was no room to
// print. The diff still travels underneath (Evidence.Patch), which is where a
// change too large for this block is read.

// treeBlock is the deliverable when the subject is the tree: what the run
// changed, split into the sources and the checks, with what is in them.
//
// The split is verify's own and not a second opinion about what a check is
// (verify.OwnChecks, verify.ChangedSources): a naming convention read in two
// places is a naming convention that will eventually disagree with itself, and
// the reading the gate is weighed against is taken through those same two
// functions.
//
// Nothing here reads a file's meaning. A source that is not text — a compiled
// model, an image, a joblib — is listed with its size and no excerpt, because
// the record's job is to say what the run produced and a byte dump of a PNG
// says it worse than the name does.
//
// EVERY FILE SAYS ITS FULL SIZE ON DISK, AND WHAT IS PRINTED IS PRINTED WHOLE.
// See the block above readableWhole for the two runs that bought that rule.
func (e Evidence) treeBlock(budget ctxbudget.Budget) string {
	files := e.recordFiles()
	if len(files) == 0 {
		return ""
	}
	root := strings.TrimSpace(e.Workspace)
	sources, checks := e.treeSplit(root, files)
	var body strings.Builder
	body.WriteString("This deliverable is the CHANGE THIS RUN MADE TO THE TREE — " +
		plural(len(files), "file") + ", listed here with what is in them. " +
		"It is the whole of what the person is being handed.\n\n" +
		"EVERY FILE BELOW IS ON DISK, WHOLE, AT THE SIZE STATED BESIDE IT. Where a file's " +
		"contents are printed, they are printed ENTIRE — nothing here is an excerpt and " +
		"nothing here stops early. Where a file appears in the list and its contents do not, " +
		"there was no room to print them: that file is whole on disk and no less part of the " +
		"deliverable, and its contents not being in front of you is a fact about this page " +
		"and never about the work.\n")
	writeNames := func(head string, names []string) {
		if len(names) == 0 {
			return
		}
		body.WriteString("\n" + head + "\n")
		for _, name := range names {
			body.WriteString(name + e.sizeWords(root, name) + "\n")
		}
	}
	writeNames("Sources the run wrote or changed:", sources)
	writeNames("Checks the run wrote or changed:", checks)
	if excerpts := e.treeContents(root, append(append([]string{}, sources...), checks...), budget); excerpts != "" {
		body.WriteString("\n" + excerpts)
	}
	return strings.TrimRight(body.String(), "\n")
}

// treeSplit names the record the way somebody opening the repository would: as
// paths inside the workspace, with the checks told apart from everything else.
//
// A record with no workspace to be relative to falls back to the recorded paths
// themselves, all of them as sources. That is every caller with no tree to name
// — a unit test, a driver that never had a workspace — and it is the reading
// those callers already got.
func (e Evidence) treeSplit(root string, files []string) (sources, checks []string) {
	if root == "" {
		return files, nil
	}
	sources, checks = verify.ChangedSources(root, files), verify.OwnChecks(root, files)
	// A record whose files all lie outside the workspace splits into nothing,
	// and nothing is the one answer this may not give: the enum below is built
	// from this list, so an empty split would make every refusal unnameable and
	// fault a gate that had a tree in front of it. The recorded paths are the
	// narrower spelling and always the true one.
	if len(sources) == 0 && len(checks) == 0 {
		return files, nil
	}
	return sources, checks
}

// recordNames is the record as the judge is shown it and as the schema admits
// it: one list, sources then checks, in the spelling treeBlock prints. It is
// the same list twice on purpose — a verdict may only name a file the judge was
// shown, and a schema built from a different list than the block would be a
// contract about files nobody put in front of it.
func (e Evidence) recordNames() []string {
	root := strings.TrimSpace(e.Workspace)
	sources, checks := e.treeSplit(root, e.recordFiles())
	return append(append([]string{}, sources...), checks...)
}

// sizeWords is the one fact about a listed file that is never in its name.
func (e Evidence) sizeWords(root, name string) string {
	info, err := os.Stat(e.treePath(root, name))
	if err != nil {
		return ""
	}
	return fmt.Sprintf(" (%d bytes)", info.Size())
}

// treePath resolves a listed name back to the file it names.
func (e Evidence) treePath(root, name string) string {
	if root == "" || filepath.IsAbs(name) {
		return name
	}
	return filepath.Join(root, filepath.FromSlash(name))
}

// treeContents is what is IN the changed files — every one of them that fits,
// entire, in the order the record spells them.
//
// The room is derived from the judge's own window (PERF.md), so on the windows
// this system runs on an ordinary change arrives whole. A file too large for
// what is left of the room is not opened, not sliced and not summarised: it
// stays in the list above with its size, where it reads as a file on disk
// rather than as a file that stops.
//
// Order is the record's own and never a weighting. Which of six changed files
// carries the behaviour a request asked for is precisely the question the judge
// is being paid to answer, and a record that decided it in advance would be
// answering it with an arithmetic nobody could see.
func (e Evidence) treeContents(root string, names []string, budget ctxbudget.Budget) string {
	if len(names) == 0 {
		return ""
	}
	left := budget.Share(gateTreeShare, gateShareTotal, gateTreeBytes)
	var body strings.Builder
	shown := 0
	for _, name := range names {
		text, size, ok := readableWhole(e.treePath(root, name), left)
		if !ok {
			continue
		}
		left -= size
		shown++
		body.WriteString("\n── " + name + " — " + strconv.Itoa(size) +
			" bytes on disk, shown in full ──\n" + text + "\n")
	}
	if shown == 0 {
		return ""
	}
	head := "What is in them, in full:"
	if shown < len(names) {
		head = "What is in " + strconv.Itoa(shown) + " of them, in full. The other " +
			strconv.Itoa(len(names)-shown) + " are named above with their sizes: they are on disk, " +
			"they are whole, they are part of what is being handed over, and there was no room " +
			"to print them here. NOTHING BELOW IS AN EXCERPT AND NOTHING ABOVE IS MISSING:"
	}
	return head + strings.TrimRight(body.String(), "\n") + "\n"
}

// readableWhole is a file's entire text, and only where the file is text a
// model can read and small enough to arrive whole.
//
// Three answers are one answer here — too big, unreadable, not text — because
// the block above does the same thing with all three: leaves the file in the
// list, with its size, saying nothing about its contents. That is how a
// compiled model, an image and a nine-thousand-line module all stay NAMES in
// the record rather than becoming either noise in it or a truncation somebody
// judges.
func readableWhole(path string, room int) (text string, size int, ok bool) {
	info, err := os.Stat(path)
	if err != nil || info.IsDir() || info.Size() == 0 || info.Size() > int64(room) {
		return "", 0, false
	}
	body, err := os.ReadFile(path)
	if err != nil || len(body) != int(info.Size()) {
		return "", 0, false
	}
	// A NUL byte is the one thing no source file has and every compiled
	// artifact has early; valid UTF-8 is the other half of the same question.
	// Neither is a judgement about the file — only about whether printing it
	// tells a reader anything.
	for _, b := range body {
		if b == 0 {
			return "", 0, false
		}
	}
	if !utf8.Valid(body) {
		return "", 0, false
	}
	trimmed := strings.TrimRight(string(body), "\n")
	if strings.TrimSpace(trimmed) == "" {
		return "", 0, false
	}
	return trimmed, len(body), true
}

// ── THE VERDICT THE TREE SUBJECT ADMITS ──────────────────────────────────────
//
// A finding about the FENCE has to be structurally unsayable, not discouraged.
// The prompt could be told a dozen ways that the fenced material is the change
// and not a message, and the run this closes proves what that is worth: the
// prompt already said, in its own words, "Read the fenced text itself before you
// say anything about it", and three verdicts described a JSON object anyway.
//
// So the shape of a refusal changes with the subject. Where the run changed the
// tree, a fail must name ONE FILE OF THE RECORD and quote the behaviour of the
// request that file fails. The file is a field with the record as its enum, so a
// routed endpoint refuses anything else on the wire; and it is checked here as
// well, against the same record, so a build with no router gets the same
// contract. A verdict that names neither is not a refusal this gate can read —
// it goes back through internal/shaped's one re-ask with the schema and its own
// words quoted at it, and if it still will not answer in shape the gate FAULTS.
//
// Which is the only honest ending for it. A judge that cannot say what is wrong
// with a file the run changed has not judged the run, and FAILSAFE.md's floor
// says a check that did not happen may not be the reason a run reports itself
// whole.

// treeEnumFiles bounds the record that travels as an enum. Past it the field
// keeps its meaning and loses its wire-level guard: the Go-side check below is
// the one that decides, and a schema carrying two hundred paths would spend more
// of the prompt on the list than on the files.
const treeEnumFiles = 64

// treeVerdictSchema is the delivery verdict's shape when the subject is the
// tree. It is the claim schema plus the one field that makes a finding about
// anything other than a changed file impossible to state.
func treeVerdictSchema(files, behaviours, consumerFiles, consumerLines, promised []string) json.RawMessage {
	file := `{"type": "string"}`
	// AND A FILE THAT USES WHAT THE RUN CHANGED IS A FILE A REFUSAL MAY NAME.
	// The record is what the run WROTE; a consumer is somewhere else in the same
	// repository that the run broke without touching, which is the one finding
	// this door exists to make sayable. It joins the same enum rather than
	// getting one of its own, because a verdict names one file and the question
	// is only whether that file is one the judge was shown.
	// AND A FILE THE REQUEST OR THE PLAN PROMISED IS A FILE A REFUSAL MAY NAME,
	// WHETHER OR NOT IT EXISTS. An absent deliverable is the oldest finding this
	// gate has, and the first thing a judge reaches for is its name — which is
	// in no record, because the record is what the run LEFT BEHIND and the whole
	// complaint is that this is not in it. igel s15 is what leaving it out
	// costs: the judge answered `"file": "feature_schema.joblib"` about a file
	// the request names and the disk does not hold, was refused, was re-asked,
	// refused again, and the run ended after eight minutes with
	// `the review could not be read, so this delivery was never checked`.
	named := joinSpans(joinSpans(files, consumerFiles), promised)
	if len(named) > 0 && len(named) <= treeEnumFiles {
		if names, err := json.Marshal(named); err == nil {
			file = `{"type": "string", "enum": ` + string(names) + `}`
		}
	}
	// AND THE QUOTE IS A BEHAVIOUR OF THE REQUEST, FROM THE LIST THE JUDGE WAS
	// SHOWN. Where the request states checkable behaviours, they are the only
	// spans a refusal over a changed tree may be built on — which is what makes
	// "the deliverable does not include the actual content of this file"
	// unsayable rather than merely wrong. Where it states none, the field is
	// what it always was, because a contract nobody was shown is a contract
	// nobody can satisfy.
	quote := `{"type": "string"}`
	if len(behaviours) > 0 {
		// The consumer lines join the same enum, and ONLY where there was
		// already an enum to join. A request that states no checkable behaviour
		// leaves the quote field free, and turning it into a list of consumer
		// lines would narrow a refusal that was never narrowed before — the
		// opposite of what this door is for.
		if spans, err := json.Marshal(joinSpans(behaviours, consumerLines)); err == nil {
			quote = `{"type": "string", "enum": ` + string(spans) + `}`
		}
	}
	return json.RawMessage(`{
  "type": "object",
  "properties": {
    "pass": {"type": "boolean"},
    "file": ` + file + `,
    "gaps": {"type": "string"},
    "quote": ` + quote + `,
    "exercised": {"type": "boolean"}
  },
  "required": ["pass"],
  "additionalProperties": false
}`)
}

// joinSpans is two lists as one, in order, with the repeats dropped. Two enums
// that overlap would offer a model the same string twice and say nothing by it.
func joinSpans(first, second []string) []string {
	joined := make([]string, 0, len(first)+len(second))
	seen := make(map[string]bool, len(first)+len(second))
	for _, span := range append(append([]string{}, first...), second...) {
		if span = strings.TrimSpace(span); span != "" && !seen[span] {
			seen[span] = true
			joined = append(joined, span)
		}
	}
	return joined
}

// gateAcceptShare is what a known window spends on the behaviours a refusal may
// be built on, and gateAcceptBytes is the bound when the window is unknown.
// PERF.md carries both.
//
// A checklist too long for the room sends none, and sending none turns the
// requirement off: this is the fail-safe direction and the only honest one. A
// judge held to a list it was never shown would fault every refusal it made,
// which is a gate that has stopped existing.
const (
	gateAcceptShare = 6
	gateAcceptBytes = 4 << 10
)

// behaviourSpans is the checklist as the schema admits it: each point's own
// verbatim span of the request, deduplicated, in the order the request states
// them — or nothing at all when they will not fit the room.
func behaviourSpans(points []plan.Point, budget ctxbudget.Budget) []string {
	room := budget.Share(gateAcceptShare, gateShareTotal, gateAcceptBytes)
	seen := make(map[string]bool, len(points))
	spans := make([]string, 0, len(points))
	spent := 0
	for _, point := range points {
		span := strings.TrimSpace(point.Quote)
		if span == "" {
			span = strings.TrimSpace(point.Behaviour)
		}
		if span == "" || seen[span] {
			continue
		}
		seen[span] = true
		if spent += len(span) + 4; spent > room {
			// PART OF A CHECKLIST IS NOT A CHECKLIST. A list clipped to fit
			// would refuse every refusal built on the behaviours that fell off
			// it, which is the same defect as a list nobody was shown.
			return nil
		}
		spans = append(spans, span)
	}
	return spans
}

// behavioursBlock puts those spans in front of the judge, above the fence with
// the rest of what a delivery is held to. The schema can admit a span and the
// model still has to be able to read one.
func behavioursBlock(spans []string) string {
	if len(spans) == 0 {
		return ""
	}
	var body strings.Builder
	body.WriteString("The behaviours this request states, read from it before any work existed. " +
		"A gap over a changed tree is a failure of ONE of these, quoted exactly:\n")
	for _, span := range spans {
		body.WriteString("- " + span + "\n")
	}
	return strings.TrimRight(body.String(), "\n")
}

// behaviourNamed answers whether a verdict's quote is one of the behaviours:
// that span, or a piece of it.
//
// THE CONTAINMENT ONLY RUNS ONE WAY, AND THAT IS THE WHOLE RULE. A quote that
// is part of a listed behaviour is that behaviour, quoted shorter, and a model
// asked for a verbatim span may reasonably give less than the whole of one. A
// quote that CONTAINS a listed behaviour is a longer span of the request that
// happens to have a behaviour inside it — which is precisely the s12 verdict:
// "Create a circuit breaker state machine module (src/circuit-breaker.ts) that
// implements … and shared state keyed by origin" swallows a real behaviour
// whole while being a sentence about producing a file. Admitting that direction
// would leave the door it is meant to close standing open.
//
// Whitespace and case are folded because a span re-wrapped by a model is the
// same words, and nothing else is forgiven.
func behaviourNamed(quote string, spans []string) (string, bool) {
	want := foldedSpan(quote)
	if want == "" {
		return "", false
	}
	for _, span := range spans {
		if have := foldedSpan(span); have != "" && strings.Contains(have, want) {
			return span, true
		}
	}
	return "", false
}

// HeldPointEmpty is what the record says where the request states no checkable
// behaviour, or states more of them than the judge's room holds: the quote
// requirement was OFF for this gate.
//
// It is a sentence rather than a silence because an empty field already means
// something else — a gate journaled before any of this existed — and the two
// readings an autopsy has to tell apart are exactly "the quote passed the list"
// and "there was no list". textual v4-flash s13 journaled two gates whose
// subject and quote were both right and whose held point was nothing at all, so
// neither could be told from the other without rebuilding the prompt.
const HeldPointEmpty = "checklist: empty"

// heldPointWords is what the record says about the behaviour a tree verdict was
// held to: the span a refusal was built on, the size of the list a pass was
// weighed against, or HeldPointEmpty where there was no list.
//
// It is DERIVED at the gate's one exit rather than written by whichever
// judgement happened to be built, for the reason Subject is: a field set by
// every constructor is a field the next constructor forgets, and this one is a
// pure function of things that exit already holds.
func heldPointWords(evidence Evidence, grounds Grounds, budget ctxbudget.Budget, judgment Judgment) string {
	if evidence.Subject() != SubjectTree {
		return ""
	}
	// A REFUSAL GROUNDED ON A CONSUMER WAS HELD TO A CONSUMER, and saying it was
	// held to a checklist of nine behaviours would be the exact confusion this
	// field exists to end. It carries the finding's own words, which name the
	// definition and its sites.
	if len(judgment.Consumers) > 0 {
		return "consumer: " + judgment.Consumers[0]
	}
	spans := behaviourSpans(HeldPoints(evidence, grounds), budget)
	if len(spans) == 0 {
		return HeldPointEmpty
	}
	if matched, ok := behaviourNamed(judgment.Quote, spans); ok {
		return matched
	}
	return "checklist: " + plural(len(spans), "behaviour")
}

// foldedSpan is the form those comparisons are made in: lower case, with every
// run of whitespace collapsed, because a quote re-wrapped by a model is the
// same words.
func foldedSpan(text string) string {
	return strings.ToLower(strings.Join(strings.Fields(text), " "))
}

// treeVerdict is the answer, with the record it has to be true of carried
// alongside so that decoding and admitting are one act.
//
// The validation lives in UnmarshalJSON deliberately. internal/shaped owns
// every repair this system makes to a shaped answer, and it decides what needs
// repairing by whether the caller's destination accepted the bytes — so a
// verdict that is unreadable BY CONTRACT and one that is unreadable by syntax
// travel the same path, get the same one re-ask carrying the same schema, and
// end in the same typed fault. A second repair ladder written here would be a
// second contract, which is the shape of defect this codebase keeps closing.
type treeVerdict struct {
	Pass      bool
	Gaps      string
	Quote     string
	File      string
	Exercised bool

	// Consumer says this refusal was grounded on a CONSUMER SITE rather than on
	// a behaviour of the request: the judge quoted a line of the project that
	// uses something this run reshaped. It is a different finding with a
	// different sentence and a different journal field, and it is decided here
	// because here is where the quote is settled against what the judge was
	// shown.
	Consumer bool
	// Promised says the file this refusal names is one the plan or the person
	// said would exist and is in no record of what the run left behind. Whether
	// it is actually absent is settled by the caller against the disk; all this
	// says is which list the name came from.
	Promised bool

	// files is the record, set by the caller before the decode. It is lower
	// case because nothing outside this package may hand a verdict a record
	// that is not the one the gate was assembled from.
	files []string
	// behaviours are the spans a refusal may be built on, and they are set only
	// where the judge was actually shown them. Empty means the request states
	// no checkable behaviour, or states more of them than the room holds, and
	// either way the requirement is off.
	behaviours []string
	// consumerFiles and consumerLines are the other ground a refusal over a
	// changed tree may stand on: the files that use a definition this run
	// reshaped, and the exact lines of them the block above printed. Both are
	// set only where the judge was shown them, for the reason behaviours is.
	consumerFiles []string
	consumerLines []string
	// promised are the files the plan or the person said would exist, in their
	// own spelling. They are nameable whether or not they are on disk, because
	// the finding they carry is precisely that one of them is not.
	promised []string
}

func (v *treeVerdict) UnmarshalJSON(data []byte) error {
	var raw struct {
		Pass      bool   `json:"pass"`
		Gaps      string `json:"gaps"`
		Quote     string `json:"quote"`
		File      string `json:"file"`
		Exercised bool   `json:"exercised"`
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	v.Pass, v.Exercised = raw.Pass, raw.Exercised
	v.Gaps, v.Quote = strings.TrimSpace(raw.Gaps), strings.TrimSpace(raw.Quote)
	v.File = ""
	if raw.Pass {
		return nil
	}
	// THE RECORD IS THE SUBJECT. An empty one is a run that left nothing
	// behind, whose deliverable is its message and whose refusal is the shape
	// it always was — the mechanical gap covers the promised-file case there,
	// and demanding a file of a record that holds none would fault every
	// question ever answered in prose. It is read off the record itself rather
	// than off a second flag for the reason the subject is: two spellings of
	// one fact drift, and this one decides whether a verdict is readable.
	if len(v.files) == 0 {
		return nil
	}
	// A FAIL OVER A CHANGED TREE IS A FILE AND A BEHAVIOUR, OR IT IS NOT A
	// FINDING. Each half answers a different failure the record has actually
	// seen: without the file, the verdict is free to be about the worker's
	// sentence, which is the run this file was written for; without the quote,
	// it is free to be a preference, which is what the citation invariant has
	// always existed to refuse.
	if v.Gaps == "" {
		return fmt.Errorf("a fail must say what is missing")
	}
	if v.Quote == "" {
		return fmt.Errorf("a fail must quote the words of the request it is a failure of")
	}
	named := recordEntry(raw.File, v.files)
	if named == "" {
		// A run can break a file it never opened, and that file is not in the
		// record. Where the reading found consumers, their files are nameable
		// too — and only theirs, so a verdict still cannot be about a file
		// nobody put in front of it.
		named = recordEntry(raw.File, v.consumerFiles)
	}
	if named == "" {
		// AND A FILE THAT WAS PROMISED AND IS NOT THERE. It is in no record by
		// definition, and refusing the verdict that names it is refusing the
		// gate's own oldest finding. See treeVerdictSchema.
		if named = recordEntry(raw.File, v.promised); named != "" {
			v.Promised = true
		}
	}
	if named == "" {
		return fmt.Errorf("a fail must name one of the files this run changed, one of the files "+
			"that use what it changed, or one the request asked for, and %q is not one of them",
			strings.TrimSpace(raw.File))
	}
	// AND THE QUOTE IS A BEHAVIOUR THE FILE FAILS, NOT A SENTENCE ABOUT THE
	// FILE'S EXISTENCE. ofetch s12 satisfied every rule above it: the file was
	// a record path, the quote was a verbatim span of the request, the gap was
	// prose. What it said was that the module's content was absent — a claim
	// about how much of the file this reading showed, wearing a citation about
	// producing it. A refusal built on a behaviour the request states cannot be
	// that claim, because the record already answers whether a file exists and
	// no behaviour is about its being on disk.
	_, held := behaviourNamed(v.Quote, v.behaviours)
	// OR A LINE OF THE PROJECT THAT USES WHAT THIS RUN RESHAPED. It is the same
	// containment test over a different list, because it is the same question:
	// is this quote one of the things the judge was actually shown. What it
	// admits is the finding no behaviour of any request could ever carry —
	// nobody writes "and the config object must still support item assignment",
	// so a door that only took behaviours could never hear it.
	if !held {
		if _, cited := behaviourNamed(v.Quote, v.consumerLines); cited {
			held, v.Consumer = true, true
		}
	}
	if len(v.behaviours) > 0 && !held {
		return fmt.Errorf("a fail must quote one of the behaviours this request states, or one of "+
			"the lines that use what this run changed, and %q is not one of them",
			clipUTF8Bytes(v.Quote, 120))
	}
	v.File = named
	return nil
}

// recordEntry answers which entry of the record a verdict's file names, by the
// same rule ProducedFile settles a request's named file against it (namedAs).
// A bare name answers wherever the file landed; a name carrying a directory
// answers only at that place. The record's own spelling is what comes back, so
// the gap a person reads names the file the way the repository does.
func recordEntry(named string, record []string) string {
	if fileKey(named) == "" {
		return ""
	}
	for _, entry := range record {
		// Direction-free, which is what namesSameFile exists for: the record
		// spells a path inside the workspace and a judge may answer with either
		// that or the absolute one it read in the run tail, and both are the
		// same file rather than one of them being an invention.
		if namesSameFile(named, entry) {
			return entry
		}
	}
	return ""
}

// treeGapWords is the finding as the record keeps it and the headless stream
// prints it: THE FILE FIRST, then what is wrong with it.
//
// The order is the whole point. firstLine(gap) is what a person watching a run
// reads, and for three refusals in a row that line was a description of a
// sentence. A line that opens with a path in the repository is a line that can
// be acted on without opening anything.
func treeGapWords(file, gaps string) string {
	file, gaps = strings.TrimSpace(file), strings.TrimSpace(gaps)
	if file == "" || strings.HasPrefix(gaps, file) {
		return gaps
	}
	return file + " — " + gaps
}

// ── THE FENCE'S OWN SHAPE ────────────────────────────────────────────────────
//
// The subject above decides what the gate JUDGES. This decides what the person
// READS, and it is the other half of the same failure.
//
// A deliverable that arrives as a data object is not a bad answer; it is an
// answer in the wrong shape, which is a thing this system already knows how to
// repair everywhere except here. internal/shaped has owned that repair for every
// call that asks a model for an object and gets prose; the delivery is the one
// ask in the system pointing the other way, and it had no repair at all — so a
// model quirk became three silent re-drives, a partial run, and a store with no
// structured_repair event in it to say what had happened.
//
// ONE ASK, AND THE ORIGINAL IF IT FAILS. A delivery whose shape cannot be fixed
// is still the delivery; losing it would cost the run its only account of
// itself to fix a presentation problem.

// ReshapeDelivery repairs a deliverable that came back as a data object, once,
// before anything is judged.
//
// It is here rather than at the wiring seam because the delivery law is this
// package's — the gate is what holds a worker to the shape it was asked for —
// and because both doors that judge a deliverable reach it through one call.
// The journal is the caller's, on the context, exactly as every other repair's
// is: a run that reshaped its answer says so against the node it belongs to.
func ReshapeDelivery(ctx context.Context, settings config.Config, client *pool.Client,
	node store.Node, deliverable string) (string, bool) {
	if client == nil || !shaped.ObjectShaped(deliverable) {
		return deliverable, false
	}
	askCtx := settings.Context(ctx, "gate")
	askCtx = provider.WithCall(askCtx, provider.ClassPlanAudit)
	askCtx = provider.WithCallTag(askCtx, "delivery")
	// The reshape is part of what this deliverable cost, for the reason the
	// gate's own call is: a repair charged to the day's overhead is a repair
	// nobody sees on the job that needed it.
	askCtx = pool.WithSpendNode(askCtx, node.ID)
	// And on the log, so a reshape is attributable to the deliverable it
	// reshaped rather than floating free of every node in the run.
	askCtx = provider.WithCallNode(askCtx, node.ID)
	return shaped.Prose(askCtx, client, shaped.Ask{Lane: "delivery", Messages: []ai.Message{
		{Role: "system", Content: []ai.ContentPart{{Type: "text", Text: ReshapePrompt}}},
		{Role: "user", Content: []ai.ContentPart{{Type: "text", Text: "What was asked for:\n" +
			node.Provenance.Intent + "\n\nThe goal you were working to:\n" + node.Brief}}},
	}}, deliverable)
}

// ReshapePrompt is the standing half of that ask. It says who is speaking and
// what a deliverable is, and nothing about what the answer should contain: the
// substance is the worker's and this is only about its shape.
const ReshapePrompt = `You are the worker that has just finished a piece of work, writing the final handover for the person who asked for it.

The handover is the deliverable itself: what was done, what it was checked against, and what came back, in plain words that person can read. It is never a data structure, never a set of field names, and never a restatement of the instructions you were given.`
