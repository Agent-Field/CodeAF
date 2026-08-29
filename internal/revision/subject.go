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

// Subject answers which of the two this delivery is, from the artifact record
// settled against the world.
//
// It is asked AFTER completeAgainstTheWorld, so the record it reads is an
// observation of the tree rather than an account of what leaves reported. A
// record naming files none of which are on disk is a record of nothing, and it
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
// not have, and a directory wearing a file's name, are both things the record
// SAYS and the world does not — and a subject decided from what the record says
// would be the same defect one seam along from the one this file closes.
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

// gateTreeExcerptFloor is the least a file's excerpt may be and still be worth
// sending. A hundred and sixty bytes is about two lines of source: enough to
// show what a file IS, and the point below which a wider list stops carrying
// information and starts carrying ellipses. A tree too wide to give every file
// that much shows the files it can and SAYS how many it could not, because a
// silent truncation of the deliverable is the one clipping this whole file
// exists to argue against.
const gateTreeExcerptFloor = 160

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
		"It is the whole of what the person is being handed.\n")
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
	if excerpts := e.treeExcerpts(root, append(append([]string{}, sources...), checks...), budget); excerpts != "" {
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

// treeExcerpts is what is IN the changed files, bounded by one share of the
// prompt and divided evenly between them.
//
// Evenly, rather than by size or by any guess about which file matters: which
// of six changed files carries the behaviour a request asked for is precisely
// the question the judge is being paid to answer, and a record that decided it
// in advance would be answering it with an arithmetic nobody could see.
func (e Evidence) treeExcerpts(root string, names []string, budget ctxbudget.Budget) string {
	if len(names) == 0 {
		return ""
	}
	room := budget.Share(gateTreeShare, gateShareTotal, gateTreeBytes)
	shown, each := len(names), room/len(names)
	if each < gateTreeExcerptFloor {
		shown, each = room/gateTreeExcerptFloor, gateTreeExcerptFloor
	}
	if shown <= 0 {
		return ""
	}
	if shown > len(names) {
		shown = len(names)
	}
	var body strings.Builder
	for _, name := range names[:shown] {
		text, ok := readableHead(e.treePath(root, name), each)
		if !ok {
			continue
		}
		body.WriteString("\n── " + name + " ──\n" + text + "\n")
	}
	if body.Len() == 0 {
		return ""
	}
	head := "What is in them:"
	if shown < len(names) {
		head = "What is in the first " + strconv.Itoa(shown) + " of them (" +
			strconv.Itoa(len(names)-shown) + " more are listed above and not excerpted here):"
	}
	return head + strings.TrimRight(body.String(), "\n") + "\n"
}

// readableHead is the first bytes of a file, and only where those bytes are
// text a model can read. It answers false for anything else, which is how a
// binary artifact stays a NAME in the record rather than becoming noise in it.
func readableHead(path string, limit int) (string, bool) {
	if limit <= 0 {
		return "", false
	}
	handle, err := os.Open(path)
	if err != nil {
		return "", false
	}
	defer handle.Close()
	buffer := make([]byte, limit)
	read, err := handle.Read(buffer)
	if read <= 0 || (err != nil && read == 0) {
		return "", false
	}
	head := buffer[:read]
	// A NUL byte is the one thing no source file has and every compiled
	// artifact has early. Valid UTF-8 is the other half of the same question,
	// asked after the head is trimmed back to a rune boundary so a file cut
	// mid-character is not mistaken for a binary one.
	for _, b := range head {
		if b == 0 {
			return "", false
		}
	}
	for len(head) > 0 && !utf8.Valid(head) {
		head = head[:len(head)-1]
	}
	text := strings.TrimRight(string(head), "\n")
	if strings.TrimSpace(text) == "" {
		return "", false
	}
	if read == limit {
		text += "\n…"
	}
	return text, true
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
func treeVerdictSchema(files []string) json.RawMessage {
	file := `{"type": "string"}`
	if len(files) > 0 && len(files) <= treeEnumFiles {
		if names, err := json.Marshal(files); err == nil {
			file = `{"type": "string", "enum": ` + string(names) + `}`
		}
	}
	return json.RawMessage(`{
  "type": "object",
  "properties": {
    "pass": {"type": "boolean"},
    "file": ` + file + `,
    "gaps": {"type": "string"},
    "quote": {"type": "string"},
    "exercised": {"type": "boolean"}
  },
  "required": ["pass"],
  "additionalProperties": false
}`)
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

	// files is the record, set by the caller before the decode. It is lower
	// case because nothing outside this package may hand a verdict a record
	// that is not the one the gate was assembled from.
	files []string
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
		return fmt.Errorf("a fail must name one of the files this run changed, and %q is not one of them",
			strings.TrimSpace(raw.File))
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
