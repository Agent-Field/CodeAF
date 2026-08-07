package head

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/Agent-Field/aforge-v2/internal/manual"
	"github.com/Agent-Field/aforge-v2/internal/resident"
	"github.com/Agent-Field/aforge-v2/internal/store"
	"github.com/Agent-Field/agentfield/sdk/go/ai"
)

// Every deterministic recognizer in this package was written after a live
// failure, and each one answered its failure by learning more words. That road
// has no end: a person can always phrase "kill everything except the finance
// one" in a way no cue list anticipated. So this file stops constraining the
// utterance space and constrains the action space instead. The model is handed
// typed tools over the graph and nothing else — it can read the board, one
// job's whole result, a file that job actually wrote, and aforge's own manual;
// it can ask for one of five verbs against ids it read there; and it can write
// one durable line into the notebook. Every rule that
// makes a change safe lives inside the tools: the store's own legality table,
// the class path's unit rule, the surgery gates, the ordinary journalled
// commands, the same structured confirm question. A model that misreads the
// sentence therefore costs at most one confirmation question, never a silent
// wrong action, and the deterministic recognizers keep the fast path they have.

const (
	beltToolBoard    = "board"
	beltToolControl  = "control"
	beltToolSteer    = "steer"
	beltToolRevise   = "revise"
	beltToolExpedite = "expedite"
	// beltToolManual is the belt's only read that is not about the graph. It
	// rides here rather than in a loop of its own because the two questions
	// arrive in the same sentence often enough — "why did you cancel that?" is
	// about the board and about aforge at once — and a second loop would have
	// to guess which one to open.
	beltToolManual = "manual"
	// beltToolResult is the belt's answer to the same failure the deep slices
	// answer from the other side. Precomputed depth guesses which jobs a message
	// is about; this lets the model decide, after it has read the board and
	// knows which row the user meant. Both exist because the guess is free and
	// the decision is right.
	beltToolResult = "result"
	// beltToolRead finishes what result starts. result returns what a job said
	// about itself and the paths it wrote; when what was asked for is inside one
	// of those documents, result hands back a pointer and the loop used to stop
	// there — offering to fetch a file it had no way to open. artifact.go holds
	// the boundary this reads through.
	beltToolRead = "read"
	// beltToolNote is the belt's only write that never touches the graph. The
	// loop could change work and answer questions and had nowhere at all to put
	// a durable instruction about its own behaviour, so "always answer from the
	// result" was replied to warmly and recorded nowhere, and the next session
	// failed identically. It writes through the same fact machinery the router's
	// remember already uses: one fact_learned event, the same notebook.
	beltToolNote = "note"

	// beltConfirmAction and beltKeepAction ride the existing surgery option
	// codec, so a belt confirmation replays through exactly the durable
	// question path the class confirmations already use.
	beltConfirmAction = "belt"
	beltKeepAction    = "beltkeep"
)

const (
	// BoardRowCap bounds every board read. A board longer than this is a log,
	// not a board: the model reads it to choose a target, and a dozen live jobs
	// is already more than a person holds in their head at once.
	BoardRowCap = 12
	// beltControlIDCap bounds one control call. The unit rule collapses whole
	// subtrees into single ids, so a legitimate set is small; a longer list is
	// a model enumerating leaves it should have named by their root.
	beltControlIDCap = 32
	// beltManualSections is how much of the manual one read returns. The belt
	// allows four calls in total, so a read that hands back a whole chapter
	// spends the message's budget on prose the answer will not use.
	beltManualSections = 4
	// beltResultBytes bounds one result read. It is larger than a deep slice
	// because this read was chosen rather than guessed — the model spent a call
	// on this exact job — and it stays in the manual read's league because both
	// are one message's whole grounding.
	beltResultBytes = 4 << 10
	// beltNoteBytes bounds one notebook line. A durable preference that will not
	// fit in a sentence is not one preference, and the notebook is read into
	// every later prompt under a budget of its own.
	beltNoteBytes = 400
)

// beltTool and beltProp mirror the leaf toolbox's definition idiom. They are
// three lines each and unexported there, so they are restated rather than
// exported across a package boundary that has no other reason to open.
func beltTool(name, description string, properties map[string]any, required ...string) ai.ToolDefinition {
	parameters := map[string]any{"type": "object", "properties": properties}
	if len(required) > 0 {
		parameters["required"] = required
	}
	return ai.ToolDefinition{Type: "function", Function: ai.ToolFunction{
		Name: name, Description: description, Parameters: parameters,
	}}
}

func beltProp(kind, description string) map[string]any {
	return map[string]any{"type": kind, "description": description}
}

// beltDefinitions are what the model sees. Descriptions are terse because they
// are resent every turn, but each one states the thing that goes wrong without
// being said: reads are free, ids are never invented, and the two revision
// verbs differ in whether the plan changes or only the people working it.
func beltDefinitions() []ai.ToolDefinition {
	return []ai.ToolDefinition{
		beltTool(beltToolBoard, "Read the live work. Always safe, always allowed, and the only place ids come from. Call it with no arguments for everything live; narrow with status, or with q when the user named the work in their own words.", map[string]any{
			"status": beltProp("string", `"running", "queued", "failed", or "all"`),
			"q":      beltProp("string", "free text naming the work, matched against titles and briefs"),
			"id":     beltProp("string", "one id from an earlier board read"),
		}),
		beltTool(beltToolControl, "Cancel, pause, resume, restart, or reprioritize the ids you name. Ids come from a board read, never from memory. A set large or expensive enough to need consent comes back as needs_confirmation and nothing changes until the user answers.", map[string]any{
			"verb": beltProp("string", "cancel, pause, resume, restart, or reprioritize"),
			"ids":  map[string]any{"type": "array", "description": "ids from a board read", "items": map[string]any{"type": "string"}},
		}, "verb", "ids"),
		beltTool(beltToolSteer, "Say something to the workers running a job right now, without changing its plan. Use it when the user is adding a constraint or a hint to work already in motion.", map[string]any{
			"job":     beltProp("string", "job id from a board read"),
			"message": beltProp("string", "what the workers should hear, in the user's own terms"),
		}, "job", "message"),
		beltTool(beltToolRevise, "Hand the user's own words to a job so its remaining plan is edited to match them. Use it when what the job is FOR has changed. Pass their words verbatim — do not improve or summarize them.", map[string]any{
			"job":   beltProp("string", "job id from a board read"),
			"words": beltProp("string", "the user's message, verbatim"),
		}, "job", "words"),
		beltTool(beltToolExpedite, "Make a job arrive sooner: it moves up the claim order and its unstarted tail is trimmed to the shortest path to the deliverable. It never adds work. Use it for impatience, never for a change of goal.", map[string]any{
			"job": beltProp("string", "job id from a board read"),
		}, "job"),
		beltTool(beltToolManual, "Read aforge's own manual: what it can do, how one of its mechanisms works, why it behaved the way it did. Always safe. Search with q, or read a whole topic with page. This is the only place answers about aforge itself may come from.", map[string]any{
			"q":    beltProp("string", "the question, in the user's own words"),
			"page": beltProp("string", "one page name to read whole, from a page list you have seen"),
		}),
		beltTool(beltToolResult, "Read what one job actually produced: its findings in full, the files it wrote, what it spent, and how its parts ended. Always safe. Read it whenever the user asks what work found, produced, concluded or decided — the board only says how a job ended, and how it ended is not what it found.", map[string]any{
			"id": beltProp("string", "one id from a board read"),
		}, "id"),
		beltTool(beltToolRead, "Open a file a job wrote and read what is inside it. Always safe. Use it the moment the answer to the question is in a document and what the job recorded only names that document — a result that says where the answer is has not given you the answer, and this is how you go and get it. Never offer to fetch something you can fetch with this call right now. Only files a job actually recorded can be opened.", map[string]any{
			"job":  beltProp("string", "the job that wrote it, id from a board or result read"),
			"file": beltProp("string", "the path or filename, as that job recorded it; omit it when the job wrote only one file"),
		}, "job"),
		beltTool(beltToolNote, "Write one durable thing the user has just told you into the notebook: how they want answers given, a correction to how something was done for them, a lasting fact about them or their setup. The test is whether it will still matter after this conversation is forgotten — task details and one-off instructions fail it. Call it before you tell them it is noted, because this call is the only thing that makes that true.", map[string]any{
			"body":  beltProp("string", "one sharp sentence, in the user's own terms"),
			"scope": beltProp("string", `what it is about: "user" for a personal preference, otherwise tool:<name>, repo:<path>, file:<path>, or domain:<topic>`),
			"kind":  beltProp("string", `"preference" for how they want things done, "fact" for something that is simply true`),
		}, "body"),
	}
}

// beltRun is one control loop's hands and its memory of what they did. The
// receipt is written from this and nothing else, so the reply can only claim
// what a tool actually reported.
type beltRun struct {
	head       *Head
	user       store.Message
	acted      bool
	commandSeq int64
	confirm    *beltConfirm
	did        []string
}

// beltConfirm is a change the gates stopped. Nothing has been journalled; the
// head asks the question once the loop stops talking.
type beltConfirm struct {
	kind   store.CommandKind
	ids    []string
	set    classSet
	impact store.SurgeryImpact
}

func (run *beltRun) execute(name, arguments string) (string, bool) {
	args := map[string]any{}
	if trimmed := strings.TrimSpace(arguments); trimmed != "" && trimmed != "null" {
		if err := json.Unmarshal([]byte(trimmed), &args); err != nil {
			return "those arguments were not valid JSON: " + err.Error(), true
		}
	}
	switch name {
	case beltToolBoard:
		return run.board(args)
	case beltToolControl:
		return run.control(args)
	case beltToolSteer:
		return run.steer(args)
	case beltToolRevise:
		return run.revise(args)
	case beltToolExpedite:
		return run.expedite(args)
	case beltToolManual:
		return run.manual(args)
	case beltToolResult:
		return run.result(args)
	case beltToolRead:
		return run.read(args)
	case beltToolNote:
		return run.note(args)
	}
	return fmt.Sprintf("there is no tool named %q", name), true
}

func (run *beltRun) board(args map[string]any) (string, bool) {
	rows, err := run.head.boardRows(beltString(args, "q"), beltString(args, "status"), beltString(args, "id"))
	if err != nil {
		return err.Error(), true
	}
	if len(rows) == 0 {
		return "no live work matches that.", false
	}
	return renderBoard(rows), false
}

func (run *beltRun) control(args map[string]any) (string, bool) {
	kind, known := beltVerbKind(beltString(args, "verb"))
	if !known {
		return "verb must be one of cancel, pause, resume, restart, reprioritize", true
	}
	ids := beltStrings(args, "ids")
	switch {
	case len(ids) == 0:
		return "ids must name at least one id from a board read", true
	case len(ids) > beltControlIDCap:
		return fmt.Sprintf("that is %d ids; name the jobs rather than their steps", len(ids)), true
	}
	if run.confirm != nil {
		return "the user has already been asked to confirm a change; nothing else may act until they answer", true
	}
	set, err := run.head.beltSet(ids, kind, true)
	if err != nil {
		return err.Error(), true
	}
	if len(set.Units) == 0 {
		return fmt.Sprintf("there is nothing there that %s can touch right now", surgeryVerb(kind)), true
	}
	impact, err := run.head.classImpact(set)
	if err != nil {
		return "the cost of that could not be read: " + err.Error(), true
	}
	if surgeryNeedsConfirmation(kind, impact) {
		run.confirm = &beltConfirm{kind: kind, ids: ids, set: set, impact: impact}
		return fmt.Sprintf("needs_confirmation: %s %s crosses the consent gate. NOTHING has changed. The user is being asked and their answer settles it.",
			surgeryVerb(kind), classSetPhrase(set, classIntent{Class: classAll})), false
	}
	labels, seq := run.head.journalUnits(run.user, kind, run.user.Body, set.Units)
	if len(labels) == 0 {
		return "none of that could be queued", true
	}
	run.record(seq, classReceipt(kind, classIntent{Class: classAll}, set, labels))
	return fmt.Sprintf("%s %d: %s", surgeryProgressive(kind), len(labels), strings.Join(labels, ", ")), false
}

func (run *beltRun) steer(args map[string]any) (string, bool) {
	job, err := run.head.beltJob(beltString(args, "job"))
	if err != nil {
		return err.Error(), true
	}
	message := strings.TrimSpace(beltString(args, "message"))
	if message == "" {
		return "message must say what the workers should hear", true
	}
	// The node-anchored broadcast is the reliable half of a redirection with
	// none of its plan editing: the workers mid-turn hear it, the plan is left
	// exactly as it stands. Nothing here is journalled, so nothing needs a gate.
	informed, err := resident.BroadcastRedirection(run.head.store, job.ID, run.user.SessionID, message)
	if err != nil {
		return "that could not be passed on: " + err.Error(), true
	}
	label := surgeryTargetLabel(job)
	if informed == 0 {
		run.record(0, "Nothing on "+label+" is running at this moment, so there was no one to pass that to.")
		return fmt.Sprintf("nothing on %s is running at this moment, so no one was told", label), false
	}
	run.record(0, fmt.Sprintf("Passed that on to %d %s on %s.",
		informed, pluralWord(informed, "worker", "workers"), label))
	return fmt.Sprintf("told %d running %s on %s", informed, pluralWord(informed, "worker", "workers"), label), false
}

func (run *beltRun) revise(args map[string]any) (string, bool) {
	job, err := run.head.beltJob(beltString(args, "job"))
	if err != nil {
		return err.Error(), true
	}
	words := strings.TrimSpace(beltString(args, "words"))
	if words == "" {
		words = strings.TrimSpace(run.user.Body)
	}
	seq, err := run.head.journalRevision(run.user, store.CommandRedirect, job.ID, words)
	if err != nil {
		return "that could not be queued: " + err.Error(), true
	}
	// The fallback receipt promises only the handoff. What actually changed in
	// the plan and who was told is written a moment later by the reconciler
	// that did it, which is the only place that honestly knows.
	run.record(seq, "Taking that to "+surgeryTargetLabel(job)+" — I'll say what changed once it lands.")
	return "the remaining plan of " + surgeryTargetLabel(job) + " will be edited to match those words", false
}

func (run *beltRun) expedite(args map[string]any) (string, bool) {
	job, err := run.head.beltJob(beltString(args, "job"))
	if err != nil {
		return err.Error(), true
	}
	seq, err := run.head.journalRevision(run.user, store.CommandExpedite, job.ID, strings.TrimSpace(run.user.Body))
	if err != nil {
		return "that could not be queued: " + err.Error(), true
	}
	run.record(seq, "Pushing "+surgeryTargetLabel(job)+" to the front and trimming what it has not started.")
	return surgeryTargetLabel(job) + " moves up the claim order and its unstarted tail is trimmed", false
}

// manual is a read like board is a read: it never records, so a message that
// only asked what aforge is journals no command and the reply carries no
// command seq. The answer is grounded or it is not given.
func (run *beltRun) manual(args map[string]any) (string, bool) {
	if name := beltString(args, "page"); name != "" {
		text, found := manual.Page(name)
		if !found {
			return "there is no manual page named " + name + " — the pages are: " +
				strings.Join(manual.Pages(), ", "), true
		}
		return text, false
	}
	query := beltString(args, "q")
	if query == "" {
		query = strings.TrimSpace(run.user.Body)
	}
	sections := manual.Search(query, beltManualSections)
	if len(sections) == 0 {
		return "the manual has nothing on that. Its pages are: " +
			strings.Join(manual.Pages(), ", "), false
	}
	return manual.Render(sections), false
}

// result is a read like board and manual are reads: it records nothing, so a
// message that only asked what a job found journals no command.
func (run *beltRun) result(args map[string]any) (string, bool) {
	node, err := run.head.beltRecordedJob(beltString(args, "id"), "id")
	if err != nil {
		return err.Error(), true
	}
	return run.head.renderResult(node), false
}

// read is the third read, and the only one whose subject is outside the graph.
// It records nothing and journals nothing for the same reason board, manual and
// result do not: asking what a document says changed nothing. What it may open
// is decided entirely in artifact.go, which is where the security boundary and
// its reasons are written down.
func (run *beltRun) read(args map[string]any) (string, bool) {
	node, err := run.head.beltRecordedJob(beltString(args, "job"), "job")
	if err != nil {
		return err.Error(), true
	}
	rendered, err := run.head.readArtifact(node, beltString(args, "file"))
	if err != nil {
		return err.Error(), true
	}
	return rendered, false
}

// note is durable feedback landing where durable feedback goes. It records
// through the head's own fact writer, so the line it writes is indistinguishable
// from one the router's remember wrote and is read back by the same notebook
// render on every later message. Nothing new is stored and no event kind is
// invented; the gap was never the machinery, it was that this loop had no hands
// for it and answered "from now on" with nothing behind the words.
func (run *beltRun) note(args map[string]any) (string, bool) {
	body := truncateBytes(beltString(args, "body"), beltNoteBytes)
	if body == "" {
		return "body must say the durable thing in one sentence", true
	}
	scope := strings.ToLower(beltString(args, "scope"))
	if scope == "" {
		scope = "user"
	}
	kind := store.FactKind(strings.ToLower(beltString(args, "kind")))
	switch kind {
	case store.FactPreference, store.FactQuirk, store.FactLesson, store.FactPlain:
	default:
		kind = store.FactPreference
	}
	fact, err := run.head.store.RecordFactFrom(store.FactWriterHead, store.RootID, scope, kind, body)
	if err != nil {
		return "that could not be written down: " + err.Error(), true
	}
	// The receipt is the record, which is the point: a note that failed to
	// journal produces a tool error, and the loop can then only say so.
	//
	// It records the receipt without claiming the message, which is what
	// separates this from every other write on the belt. "Always answer from the
	// result, and rerun the scans" is one durable preference and one piece of
	// work; a note that marked the run as acted would let the loop answer with
	// the receipt and swallow the rest. The fact is journalled either way, the
	// store deduplicates it, and the sentinel stays free to hand the sentence
	// back to the router.
	run.did = append(run.did, "Noted — "+firstLine(fact.Body))
	return fmt.Sprintf("written into the notebook as #%d under %s; it is in front of you on every later message",
		fact.Seq, fact.Scope), false
}

// beltRecordedJob resolves an id for the two reads that are allowed to name work
// that has already finished. It deliberately does not go through beltJob: that
// resolver refuses settled work because the verbs cannot touch it, and settled
// work is precisely what has findings and files.
func (h *Head) beltRecordedJob(id, field string) (store.Node, error) {
	id = strings.TrimSpace(id)
	if id == "" {
		return store.Node{}, fmt.Errorf("%s must name one job from a board read", field)
	}
	node, found, err := h.store.Node(id)
	if err != nil {
		return store.Node{}, fmt.Errorf("that job could not be read: %w", err)
	}
	if !found || node.ID == store.RootID || !beltAddressable(node) {
		return store.Node{}, fmt.Errorf("there is no work of the user's with id %q — read the board again", id)
	}
	return node, nil
}

// renderResult is one job's whole account of itself. The finding comes first
// because it is the answer; status and spend trail it because they are context
// for the answer, and the children are there so "what did each part conclude"
// is one read rather than five.
func (h *Head) renderResult(node store.Node) string {
	now := time.Now()
	var rendered strings.Builder
	fmt.Fprintf(&rendered, "%s | %s | %s", node.ID, node.Status, surgeryTargetLabel(node))
	if impact, err := h.store.Impact(node.ID, now); err == nil && impact.Cost > 0 {
		fmt.Fprintf(&rendered, " | $%.2f", impact.Cost)
	}
	if age := store.AgeLabel(node.FinishedAt, now); age != "" {
		rendered.WriteString(" | finished " + age)
	}
	rendered.WriteString("\n")
	body := truncateBytes(nodeResult(node), beltResultBytes)
	if body != "" {
		rendered.WriteString("result:\n" + body + "\n")
	} else {
		rendered.WriteString("result: nothing recorded yet — this job has not settled.\n")
	}
	if files := unnamedFiles(node, body); len(files) > 0 {
		rendered.WriteString("files: " + strings.Join(files, ", ") + "\n")
	}
	if children := h.resultChildren(node.ID); len(children) > 0 {
		rendered.WriteString("its parts:\n" + strings.Join(children, "\n") + "\n")
	}
	return strings.TrimSpace(rendered.String())
}

// resultChildren gives each direct child the board's one line. BoardRowCap
// bounds it for the board's own reason: past a dozen rows this is a log rather
// than a list of parts, and the parent's own result already summarises it.
func (h *Head) resultChildren(id string) []string {
	nodes, err := h.store.ActiveNodes()
	if err != nil {
		return nil
	}
	lines := make([]string, 0, BoardRowCap)
	for _, node := range nodes {
		if node.Parent != id || node.Folded {
			continue
		}
		line := fmt.Sprintf("- %s | %s | %s", node.ID, node.Status, surgeryTargetLabel(node))
		if summary := firstLine(nodeResult(node)); summary != "" {
			line += " | " + summary
		}
		lines = append(lines, line)
		if len(lines) == BoardRowCap {
			break
		}
	}
	return lines
}

// record is the only way the run learns it acted. The command seq is the last
// one journalled, which is what ties the reply to durable work the way every
// other receipt in this package does.
func (run *beltRun) record(seq int64, receipt string) {
	run.acted = true
	if seq != 0 {
		run.commandSeq = seq
	}
	if receipt != "" {
		run.did = append(run.did, receipt)
	}
}

// summary is the fallback receipt for a loop that acted and then said nothing
// useful. It is assembled from what the tools reported, never from intent.
func (run *beltRun) summary() string {
	if len(run.did) == 0 {
		return "Done."
	}
	return strings.Join(run.did, " ")
}

// beltJob resolves one job id the model named. Only the user's own live work is
// addressable: the resident's practice and its own internals are not on the
// board, so they can never be named, and a hallucinated id fails here.
func (h *Head) beltJob(id string) (store.Node, error) {
	id = strings.TrimSpace(id)
	if id == "" {
		return store.Node{}, fmt.Errorf("job must name an id from a board read")
	}
	node, found, err := h.store.Node(id)
	if err != nil {
		return store.Node{}, fmt.Errorf("that job could not be read: %w", err)
	}
	if !found || node.Folded || node.ID == store.RootID {
		return store.Node{}, fmt.Errorf("there is no live work with id %q — read the board again", id)
	}
	if !beltAddressable(node) {
		return store.Node{}, fmt.Errorf("%q is not the user's work and is not yours to change", id)
	}
	if !classOpen(node.Status) {
		return store.Node{}, fmt.Errorf("%q has already finished", id)
	}
	return node, nil
}

// beltAddressable is the ownership membrane the whole belt sits behind. The
// class path draws the same line with its sweeping flag; here it is absolute,
// because a model composing tools has no user word to weigh against it.
func beltAddressable(node store.Node) bool {
	switch node.Group {
	case store.TerritoryGroup, charterNodeGroup, store.PracticeGroup:
		return false
	}
	return node.Provenance.Origin != store.OriginSelf
}

// beltSet resolves an explicit id set exactly the way the class path resolves a
// status set: expand each named id over its open subtree, keep only what the
// verb may legally touch, and reduce the result to the outermost wholly-legal
// units. Naming a job whose leaf is running under a verb that cannot touch
// running work therefore reaches its queued leaves individually and leaves the
// running one alone — the same safety property, arrived at from ids instead of
// from a status word.
//
// strict is the difference between the tool and the answered question. The tool
// is strict so a wrong id comes back as something the model can read and fix;
// the confirmed replay is lenient, so a unit that settled between the question
// and the answer simply drops out rather than failing the whole set.
func (h *Head) beltSet(ids []string, kind store.CommandKind, strict bool) (classSet, error) {
	nodes, err := h.store.ActiveNodes()
	if err != nil {
		return classSet{}, err
	}
	byID := make(map[string]store.Node, len(nodes))
	for _, node := range nodes {
		byID[node.ID] = node
	}
	children := make(map[string][]string, len(nodes))
	for _, node := range nodes {
		if _, ok := byID[node.Parent]; ok {
			children[node.Parent] = append(children[node.Parent], node.ID)
		}
	}
	matched := make(map[string]bool, len(nodes))
	for _, id := range ids {
		id = strings.TrimSpace(id)
		node, present := byID[id]
		if !present || node.Folded || node.ID == store.RootID {
			if strict {
				return classSet{}, fmt.Errorf("there is no live work with id %q — read the board again", id)
			}
			continue
		}
		if !beltAddressable(node) {
			if strict {
				return classSet{}, fmt.Errorf("%q is not the user's work and is not yours to change", id)
			}
			continue
		}
		if !beltLegal(node, kind) {
			if strict {
				return classSet{}, fmt.Errorf("%q is %s, and %s only applies to %s",
					id, beltStatusWord(node), surgeryVerb(kind), beltAllowedWords(kind))
			}
			continue
		}
		for _, member := range beltSubtree(byID, children, id) {
			if beltLegal(byID[member], kind) {
				matched[member] = true
			}
		}
	}
	return classUnits(nodes, matched), nil
}

// beltSubtree walks one named id and everything open beneath it. Settled work
// is skipped rather than walked through: a finished branch has no bearing on
// whether the branch above it is wholly in the set.
func beltSubtree(byID map[string]store.Node, children map[string][]string, root string) []string {
	seen := make(map[string]bool, len(byID))
	members := make([]string, 0, 8)
	var walk func(string)
	walk = func(id string) {
		if seen[id] {
			return
		}
		seen[id] = true
		members = append(members, id)
		for _, child := range children[id] {
			if classOpen(byID[child].Status) || byID[child].Status == store.Failed {
				walk(child)
			}
		}
	}
	walk(root)
	return members
}

// beltLegal is the store's own legality table read ahead of time, so a bad
// combination becomes a sentence the model can act on rather than a rejected
// command the user never hears about.
func beltLegal(node store.Node, kind store.CommandKind) bool {
	if node.Folded || node.ID == store.RootID {
		return false
	}
	for _, status := range surgeryAllowedStatuses(kind) {
		if node.Status == status {
			return surgeryEligible(node, kind)
		}
	}
	return false
}

func beltStatusWord(node store.Node) string {
	if node.Held {
		return "paused"
	}
	return string(node.Status)
}

func beltAllowedWords(kind store.CommandKind) string {
	switch kind {
	case store.CommandRestart:
		return "failed or cancelled work"
	case store.CommandResume:
		return "paused work"
	case store.CommandReprioritize:
		return "work that has not started"
	default:
		return "work that is queued or running"
	}
}

func beltVerbKind(verb string) (store.CommandKind, bool) {
	switch strings.ToLower(strings.TrimSpace(verb)) {
	case "cancel":
		return store.CommandCancel, true
	case "pause":
		return store.CommandPause, true
	case "resume":
		return store.CommandResume, true
	case "restart":
		return store.CommandRestart, true
	case "reprioritize":
		return store.CommandReprioritize, true
	}
	return "", false
}

// boardRow is one line of live work: what it is, how it is going, and what it
// has cost. Nothing else fits in a row that is resent on every turn.
type boardRow struct {
	node    store.Node
	age     string
	running int
	queued  int
	failed  int
	cost    float64
}

// boardRows is the belt's whole read side. It is the ordinary surgery search
// over job roots, annotated with the counts and spend a person would want
// before deciding anything, and narrowed to the user's own work.
func (h *Head) boardRows(query, status, id string) ([]boardRow, error) {
	class := classAll
	if word := strings.ToLower(strings.TrimSpace(status)); word != "" {
		named, ok := classVocabulary[word]
		if !ok {
			return nil, fmt.Errorf("status must be running, queued, failed, or all")
		}
		class = named
	}
	nodes, err := h.store.ActiveNodes()
	if err != nil {
		return nil, fmt.Errorf("the board could not be read: %w", err)
	}
	byID := make(map[string]store.Node, len(nodes))
	for _, node := range nodes {
		byID[node.ID] = node
	}
	children := make(map[string][]string, len(nodes))
	for _, node := range nodes {
		if _, ok := byID[node.Parent]; ok {
			children[node.Parent] = append(children[node.Parent], node.ID)
		}
	}

	var candidates []store.SurgeryTarget
	switch {
	case strings.TrimSpace(id) != "":
		node, found, err := h.store.Node(strings.TrimSpace(id))
		if err != nil {
			return nil, fmt.Errorf("the board could not be read: %w", err)
		}
		if !found || node.Folded || !beltAddressable(node) || node.ID == store.RootID {
			return nil, nil
		}
		candidates = []store.SurgeryTarget{{Node: node}}
	case strings.TrimSpace(query) != "":
		// No allowed statuses: a read is never narrowed by what some verb could
		// legally touch, only by what the caller asked to see.
		candidates, err = h.store.SearchSurgeryTargets(strings.TrimSpace(query), false)
		if err != nil {
			return nil, fmt.Errorf("the board could not be read: %w", err)
		}
	default:
		// The unqueried board is an enumeration, not a search. Ranking words
		// against nothing scores every job the same and then truncates the tie
		// arbitrarily, and a board that silently omits a job is exactly the
		// reality the model must not be handed.
		candidates = boardEnumeration(nodes, byID)
	}

	now := time.Now()
	rows := make([]boardRow, 0, len(candidates))
	for _, candidate := range candidates {
		if candidate.Node.Provenance.Origin != store.OriginUser || !beltAddressable(candidate.Node) {
			continue
		}
		row := boardRow{node: candidate.Node, age: candidate.Age}
		for _, member := range beltSubtree(byID, children, candidate.Node.ID) {
			switch byID[member].Status {
			case store.Running, store.Claimed:
				row.running++
			case store.Pending:
				row.queued++
			case store.Failed:
				row.failed++
			}
		}
		if impact, err := h.store.Impact(candidate.Node.ID, now); err == nil {
			row.cost = impact.Cost
		}
		if !boardRowMatches(row, class) {
			continue
		}
		rows = append(rows, row)
		if len(rows) == BoardRowCap {
			break
		}
	}
	return rows, nil
}

// boardEnumeration lists every job root, live first and newest first within
// each band — the same ordering rule the router's snapshot follows, and for the
// same reason: what is happening now must never be the thing the cap drops.
func boardEnumeration(nodes []store.Node, byID map[string]store.Node) []store.SurgeryTarget {
	roots := make([]store.Node, 0, len(nodes))
	for _, node := range nodes {
		if node.ID == store.RootID || node.Folded || !beltAddressable(node) {
			continue
		}
		if node.Parent == store.RootID || byID[node.Parent].Group == store.TerritoryGroup {
			roots = append(roots, node)
		}
	}
	rank := func(node store.Node) int {
		switch node.Status {
		case store.Running, store.Claimed:
			return 0
		case store.Pending:
			return 1
		case store.Failed:
			return 2
		default:
			return 3
		}
	}
	sort.SliceStable(roots, func(i, j int) bool {
		if ri, rj := rank(roots[i]), rank(roots[j]); ri != rj {
			return ri < rj
		}
		return roots[i].CreatedSeq > roots[j].CreatedSeq
	})
	targets := make([]store.SurgeryTarget, 0, len(roots))
	for _, node := range roots {
		targets = append(targets, store.SurgeryTarget{Node: node})
	}
	return targets
}

// boardRowMatches reads the class off the job as a whole rather than off its
// root node, because "the running ones" means jobs with somebody working on
// them, not jobs whose root happens to carry a running status.
func boardRowMatches(row boardRow, class string) bool {
	switch class {
	case classRunning:
		return row.running > 0
	case classQueued:
		return row.queued > 0
	case classFailed:
		return row.failed > 0 || row.node.Status == store.Failed
	default:
		return row.running > 0 || row.queued > 0 || row.failed > 0 ||
			classOpen(row.node.Status) || row.node.Status == store.Failed
	}
}

func renderBoard(rows []boardRow) string {
	lines := make([]string, 0, len(rows))
	for _, row := range rows {
		line := fmt.Sprintf("- %s | %s | %s | %d running, %d queued",
			row.node.ID, surgeryTargetLabel(row.node), boardRowStatus(row), row.running, row.queued)
		if row.failed > 0 {
			line += fmt.Sprintf(", %d failed", row.failed)
		}
		line += fmt.Sprintf(" | $%.2f", row.cost)
		if age := strings.TrimSpace(row.age); age != "" {
			line += " | " + age
		}
		lines = append(lines, line)
	}
	return strings.Join(lines, "\n")
}

func boardRowStatus(row boardRow) string {
	switch {
	case row.node.Held:
		return "paused"
	case row.running > 0:
		return "running"
	case row.queued > 0:
		return "queued"
	default:
		return string(row.node.Status)
	}
}

// askBeltConfirm is the one question the belt is allowed, and it is the class
// path's question with an id list where the class word was. Both name the count
// before anything moves, because the difference between one node and fourteen
// is the whole reason the user would want to be asked.
func (h *Head) askBeltConfirm(user store.Message, confirm *beltConfirm) error {
	class := classIntent{Class: classAll}
	verb := surgeryVerb(confirm.kind)
	prompt := fmt.Sprintf("%s %s?", upperFirst(verb), classSetPhrase(confirm.set, class))
	if confirm.impact.Cost > 0 || confirm.impact.RunningFor > 0 {
		// The set's own count is already in the prompt, so the loss clause must
		// not repeat it.
		lossOnly := confirm.impact
		lossOnly.OpenNodes, lossOnly.Nodes = 0, 0
		prompt += " " + surgeryLoss(confirm.kind, lossOnly)
	}
	encoded := encodeBeltIDs(confirm.ids)
	return h.askSurgerySetConfirm(user, prompt, confirm.set,
		store.QuestionOption{
			Label: fmt.Sprintf("yes, %s all %d", verb, confirm.set.Affected),
			Value: encodeSurgeryOption(beltConfirmAction, confirm.kind, encoded, user.Body)},
		store.QuestionOption{
			Label: classKeepLabel(confirm.kind, class),
			Value: encodeSurgeryOption(beltKeepAction, confirm.kind, encoded, user.Body)})
}

// applyBeltOption settles a belt confirmation. Like the class answers, it
// re-resolves from the ids as the graph stands now rather than from a frozen
// list of units: the set the user agreed to is the set as it stands when they
// agree.
func (h *Head) applyBeltOption(user store.Message, action string, kind store.CommandKind,
	target, instruction string) (bool, error) {
	switch action {
	case beltKeepAction:
		return true, h.postAgent(user.SessionID, "Keeping them as they are.", 0)
	case beltConfirmAction:
	default:
		return false, nil
	}
	ids, ok := decodeBeltIDs(target)
	if !ok {
		return true, h.postAgent(user.SessionID, commandErrorReply, 0)
	}
	set, err := h.beltSet(ids, kind, false)
	if err != nil {
		return true, err
	}
	if len(set.Units) == 0 {
		return true, h.postAgent(user.SessionID, classEmptyReply(kind, classIntent{Class: classAll}), 0)
	}
	labels, seq := h.journalUnits(user, kind, instruction, set.Units)
	if len(labels) == 0 {
		return true, h.postAgent(user.SessionID, commandErrorReply, 0)
	}
	if len(labels) < len(set.Units) {
		set.Affected = len(labels)
		set.Jobs = 0
	}
	return true, h.postAgent(user.SessionID,
		classReceipt(kind, classIntent{Class: classAll}, set, labels), seq)
}

// encodeBeltIDs hides the separator inside the option's single target field.
// Node ids are opaque strings and the codec splits on colons, so the list is
// encoded rather than joined.
func encodeBeltIDs(ids []string) string {
	return base64.RawURLEncoding.EncodeToString([]byte(strings.Join(ids, "\n")))
}

func decodeBeltIDs(value string) ([]string, bool) {
	decoded, err := base64.RawURLEncoding.DecodeString(strings.TrimSpace(value))
	if err != nil {
		return nil, false
	}
	ids := make([]string, 0, 4)
	for _, id := range strings.Split(string(decoded), "\n") {
		if id = strings.TrimSpace(id); id != "" {
			ids = append(ids, id)
		}
	}
	return ids, len(ids) > 0
}

func beltString(args map[string]any, key string) string {
	if value, ok := args[key].(string); ok {
		return strings.TrimSpace(value)
	}
	return ""
}

// beltStrings accepts both shapes providers actually emit for a list argument:
// a JSON array, and a single comma-separated string.
func beltStrings(args map[string]any, key string) []string {
	switch value := args[key].(type) {
	case []any:
		ids := make([]string, 0, len(value))
		for _, item := range value {
			if id, ok := item.(string); ok && strings.TrimSpace(id) != "" {
				ids = append(ids, strings.TrimSpace(id))
			}
		}
		return ids
	case string:
		ids := make([]string, 0, 4)
		for _, id := range strings.Split(value, ",") {
			if id = strings.TrimSpace(id); id != "" {
				ids = append(ids, id)
			}
		}
		return ids
	}
	return nil
}
