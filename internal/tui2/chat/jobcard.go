package chat

import (
	"strconv"
	"strings"

	"github.com/Agent-Field/aforge-v2/internal/store"
	"github.com/Agent-Field/aforge-v2/internal/tui2/rail"
	"github.com/Agent-Field/aforge-v2/internal/tui2/tokens"
)

// ONE TASK, THREE PRESENCES, AND NOTHING ELSE.
//
// The reader's own words are the law this file implements: "I give a task, I see
// thinking/planning shimmer, then I get back a card saying the task name and the
// actual prompt it is using — that's the row-0 parent task. It stays and I keep
// chatting. Once the WHOLE task is done (not individual nodes) I see a result
// card with the task name, prompt, and the result organized … and I can click it
// to go into the task."
//
// So the main thread carries exactly three things about a task, ever:
//
//	1. the COMMITMENT CARD, born when the task is commissioned and updating in
//	   place for the whole life of the task;
//	2. the DELIVERY CARD, when the WHOLE row-0 task settles, in the commitment's
//	   own position;
//	3. its open QUESTIONS, because amber means a human is actually needed.
//
// WHAT WAS THERE INSTEAD. Measured on the reporter's own journal, one settled
// task (`task-9380`, "20 best stocks ranked") put FOUR top-level blocks in the
// conversation — a running one with a flag note, a settled one carrying
// `ruler: median 19 turns, 5 of 23 overran`, the actual result, and a
// `⚒ forged:` note — each re-printing the title and `$0.27`. Its sibling
// (`task-9400`) added `splitting the remaining work -- 1 pieces queued` rows and
// four `⚑ <child>: …` flags, plus one top-level block per CHILD node
// ("Candidate summaries", "Compile Cooling Stock Data", …). Every one of those
// is §1's "worker lifecycle … progress narration" — the work record's business,
// one click away, never a chat message.
//
// THE DISCRIMINATOR IS THE GRAPH AND NEVER THE PROSE (13.3.1). A row's node id
// resolves to the ROW-0 TASK it belongs to ([scopeSource.jobRoot]), and that one
// read answers all of it at once: a child node's completion, a ruler line, a
// splitting line and a flag are all rows anchored somewhere under a task, so
// they all fold into that task's one card. No renderer here matches on
// `ruler:`, on `splitting`, or on any other producer's wording — a filter made
// of strings is a filter that stops working the day somebody rephrases a line.

// jobRoots is the board's answer to "which task does this node belong to".
//
// It is a SEPARATE optional interface rather than a second method on
// [jobSource], for the reason jobSource is an interface at all: the block
// builder must stay testable without a store, and a surface with no board keeps
// drawing what it drew before instead of failing to compile.
type jobRoots interface {
	jobRoot(nodeID string) string
}

// jobRoot walks a node up to the top-level task the board knows it as.
//
// It stops at the first ancestor that has a task scope — which is exactly the
// set of rows the sidebar draws as cards — so a worker three levels down under
// a spliced plan answers with the job the reader commissioned, and a node the
// board has never heard of answers with itself rather than with nothing. The
// walk is bounded by the map it walks, and a parent chain that loops (which the
// store forbids and this must survive anyway) ends at the cap.
func (s *scopeSource) jobRoot(nodeID string) string {
	nodeID = strings.TrimSpace(nodeID)
	if s == nil || nodeID == "" {
		return nodeID
	}
	id := nodeID
	for hops := 0; hops < maxJobDepth; hops++ {
		if _, ok := s.tasks[rowTaskPrefix+id]; ok {
			return id
		}
		node, known := s.nodes[id]
		if !known {
			return id
		}
		parent := strings.TrimSpace(node.Parent)
		if parent == "" || parent == store.RootID || parent == id {
			return id
		}
		id = parent
	}
	return id
}

// maxJobDepth bounds the walk. A plan is a handful of levels deep; the cap is
// there so a malformed parent chain cannot hang a render.
const maxJobDepth = 16

// jobOf is the task a message belongs to, or "" when it belongs to none.
//
// TWO DOORS LEAD IN AND BOTH ARE COLUMNS. A row anchored to a NODE belongs to
// that node's task. A COMMISSIONING row is anchored to no node at all — it is
// the head speaking about work it has just handed over — and carries the
// command that spliced the task instead; the node a splice at command N mints
// is `task-N`, and that link is CONFIRMED against the board rather than assumed,
// so a row whose task the board cannot show simply keeps no link and renders
// exactly as it always did.
func jobOf(message store.Message, board jobSource) string {
	if node := strings.TrimSpace(message.NodeID); node != "" {
		if roots, ok := board.(jobRoots); ok {
			return roots.jobRoot(node)
		}
		return node
	}
	if message.CommandSeq == 0 || board == nil {
		return ""
	}
	for _, candidate := range commandJobIDs(message.CommandSeq) {
		if _, known := board.jobFacts(candidate); known {
			return candidate
		}
	}
	return ""
}

// commandTaskID is the node a splice at one command mints. It is the store's own
// naming convention read back, and every caller CHECKS the answer against the
// board before believing it (see [jobOf]) — a guess that is verified is a
// lookup, and a guess that is not is 8.2.20's invented fact.
func commandTaskID(commandSeq int64) string {
	return taskIDPrefix + strconv.FormatInt(commandSeq, 10)
}

// taskIDPrefix is how a spliced top-level task's id begins.
const taskIDPrefix = "task-"

// craftIDPrefix is the OTHER spelling a splice can mint: a request the craft
// shelf answers decisively compiles to `craft-<seq>` (craftmind's own naming),
// and a reader who only ever guessed `task-` declared every craft job's card
// unlinked — and, once the skeleton wave landed, buried a live craft job as
// "didn't start" while its workers ran.
const craftIDPrefix = "craft-"

// commandJobIDs is every node id a splice at one command can have minted, in
// the order they are worth asking about. Both are guesses the way
// [commandTaskID] is a guess, and every caller CHECKS each against the board
// before believing it.
func commandJobIDs(commandSeq int64) [2]string {
	seq := strconv.FormatInt(commandSeq, 10)
	return [2]string{taskIDPrefix + seq, craftIDPrefix + seq}
}

// commandSeqOf runs [commandJobIDs] backwards: the command a job root was
// minted from, or nothing.
//
// It answers only for the ROOT spellings and deliberately so. A part's id
// carries its own suffix — `task-12-3` — and a part is not the node the ask was
// made of ([ownsTheAsk]), so refusing it here is the same rule stated once more
// where the read is, rather than a second opinion about which node owns a job.
func commandSeqOf(nodeID string) (int64, bool) {
	for _, prefix := range [2]string{taskIDPrefix, craftIDPrefix} {
		rest, cut := strings.CutPrefix(strings.TrimSpace(nodeID), prefix)
		if !cut {
			continue
		}
		seq, err := strconv.ParseInt(rest, 10, 64)
		if err != nil || seq <= 0 {
			continue
		}
		return seq, true
	}
	return 0, false
}

// -- the receipt --------------------------------------------------------------

// jobReceipts is the optional read that lets a delivery card carry the whole
// receipt §5 asks a record's header for: "elapsed · $cost · Nk tok plus the
// models that did the work (deduped, dim), each figure absent when unknowable".
//
// It is optional for the same reason every other board read here is: a surface
// with no graph draws the card it can draw rather than no card.
type jobReceipts interface {
	jobTokens(root string) (int, bool)
	jobModels(root string) []string
}

// taskReceipt is the delivery card's right-aligned telemetry: how long it took,
// what it cost, how much it burned, and who did the work.
//
// EVERY FIGURE IS ABSENT WHEN IT IS UNKNOWABLE, which is §16's EMPTINESS and the
// difference between a receipt and a decoration. Money is the one cell that
// draws its own absence (`$—`), because a job with no cost cell and a job that
// cost nothing are different facts and money is the number a reader looks for.
//
// It is [chargeCells] said for the conversation instead of for the record, and
// deliberately the SAME four cells in the SAME order: the card in the thread and
// the header of the room it opens are two renderings of one job, and §12.14's
// law is that a preview may never disagree with the thing it previews.
func taskReceipt(facts jobFacts, board jobSource, root string) string {
	cells := make([]string, 0, 4)
	if facts.HasElapsed {
		cells = append(cells, tokens.Elapsed(facts.Elapsed))
	}
	if facts.HasCost {
		cells = append(cells, tokens.Money(facts.Cost))
	} else {
		cells = append(cells, tokens.GlyphSpend+tokens.GlyphMissing)
	}
	reads, ok := board.(jobReceipts)
	if !ok || root == "" {
		return strings.Join(append(cells, harnessWords(facts)...), " "+tokens.GlyphSeparator+" ")
	}
	if burned, have := reads.jobTokens(root); have {
		cells = append(cells, tokens.Count(int64(burned))+" tok")
	}
	// The harness, beside the model, in v1's own order and v1's own words
	// (internal/tui/cards.go's cardChoiceReceipt: `swe · kimi-k2`). It sits
	// where it does for the reason the models sit where they do — it is a NAME
	// and not a figure, and the right edge is a column of figures.
	cells = append(cells, harnessWords(facts)...)
	// The models are humane words and deduped, and they are the LAST cell
	// because they are the one a reader glances at rather than compares: §16's
	// right edge is a column of figures, and a name in the middle of it would
	// break the scan down the page.
	cells = append(cells, modelWords(reads.jobModels(root))...)
	return strings.Join(cells, " "+tokens.GlyphSeparator+" ")
}

// harnessWords is the worker cell, or no cell at all.
//
// It answers a list rather than a string so a caller appends it without a
// branch, which is the only way §16's EMPTINESS survives contact with four call
// sites: an absent worker adds nothing and leaves no separator behind.
//
// THE WORD IS THE STORE'S, VERBATIM. This surface never learns one worker's
// name — a surface that branched on `swe` would need editing every time a
// worker is added, and §14 forbids teaching the reader a word the rest of the
// product does not already use. `swe` is what v1's receipt has always said and
// what the record page's own charge line badges (record.go's chargeBlock).
func harnessWords(facts jobFacts) []string {
	if harness := strings.TrimSpace(facts.Harness); harness != "" {
		return []string{harness}
	}
	return nil
}

// modelWords is the deduped humane spelling of the models that billed a job.
//
// The dedupe happens twice — once in the store's own read and once here — and
// that is deliberate rather than redundant: two provider ids can collapse onto
// one humane word ([modelWord] drops the vendor and the date), so a list that
// was already distinct as ids can hold the same WORD twice, and a receipt that
// said `k3 · k3` would be counting one model as two.
func modelWords(models []string) []string {
	if len(models) == 0 {
		return nil
	}
	out := make([]string, 0, len(models))
	seen := make(map[string]bool, len(models))
	for _, model := range models {
		word := modelWord(model)
		if word == "" || seen[word] {
			continue
		}
		seen[word] = true
		out = append(out, word)
	}
	if len(out) > modelWordCap {
		out = out[:modelWordCap]
	}
	return out
}

// modelWordCap is how many model words a receipt names before the rest are left
// to the record. A job that escalated twice is the interesting case and it fits;
// a job that touched nine rungs has a record to say so in.
const modelWordCap = 3

// -- the machinery tier -------------------------------------------------------

// speaks reports that a row is one of the three sentence classes the chat
// carries (§1) rather than the work record's business.
//
// A LEARNING MOMENT IS A VOICE AND NOT MACHINERY. `· learned`, `⚒ forged` and
// `· reflected` are the machine telling the reader something about ITSELF that
// it will act on next time, which is the one kind of narration §1 does not send
// to the record — and the reporter's own screenshots show them landing correctly
// today, as single dim lines. What they must never do is wear a job's title and
// receipt as a header, which is what happened when they were dressed as work
// cards: the `⚒ forged:` row re-printed "20 best stocks ranked · $0.27" above a
// sentence about a workflow.
//
// It is the ONE place this file reads a body, and what it reads are the
// PRODUCERS' OWN OPENING BYTES rather than a phrase — the same arrangement, and
// for the same reason, as internal/tui2/chat/trace.go's `traceRulePrefix`: these
// are marks matched on the way IN, not glyphs drawn on the way out, so they are
// deliberately not tokens slots and a change to how the room draws can never
// stop the room from classifying. The marks are three bytes with one meaning
// each and no producer rephrases them; the SENTENCE after them is free, which is
// exactly what a prose filter could not have promised.
func speaks(body string) bool {
	body = strings.TrimSpace(body)
	for _, mark := range learningMarks {
		if strings.HasPrefix(body, mark) {
			return true
		}
	}
	return false
}

// learningMarks are the openings of the three learning moments, as the
// producers write them.
var learningMarks = []string{"· learned", "· reflected", "⚒ forged"}

// -- the card's body ----------------------------------------------------------

// splitCardBody cuts a delivery's prose into what STANDS and what folds.
//
// [splitHeadline] cuts at the first line break, which is the right boundary for
// a status line and the wrong one for a finished answer: a result whose first
// line is "Here is the ranked list:" put one useless sentence on the card and
// everything a reader wanted behind a chevron. §4 asks a card for "3–5 sentences
// the assistant absorbed"; this keeps [cardBodyRows] source lines, which is that
// in the unit the record is actually written in.
//
// IT SPLITS AND NEVER SHORTENS. Every word it moves is behind the fold, whole —
// the dressing presents the record and does not rewrite it (13.1 item 3) — and
// a body that fits inside the cap folds nothing at all rather than offering a
// door onto an empty room.
func splitCardBody(body string) (shown, rest string) {
	body = strings.TrimRight(strings.TrimSpace(body), "\n")
	if body == "" {
		return "", ""
	}
	lines := strings.Split(body, "\n")
	if len(lines) <= cardBodyRows {
		return body, ""
	}
	// A blank line is a paragraph boundary, so a cut that lands on one is a cut
	// between thoughts rather than through one. The search only walks BACKWARDS
	// from the cap, so the card never grows past it.
	cut := cardBodyRows
	for i := cardBodyRows; i > cardBodyRows/2; i-- {
		if strings.TrimSpace(lines[i-1]) == "" {
			cut = i - 1
			break
		}
	}
	return strings.TrimRight(strings.Join(lines[:cut], "\n"), "\n"),
		strings.TrimLeft(strings.Join(lines[cut:], "\n"), "\n")
}

// cardBodyRows is how much of a finding stands above the fold. Six source lines
// is a paragraph and a list's opening, which is what a reader needs to decide
// whether to open the rest — and it is bounded for the same reason §5 bounds an
// opened tool result: a card that grew with its content stops being a card.
const cardBodyRows = 6

// -- the head's reading, parsed into the card's zones ------------------------

// reading is the head's own sentence about a task, cut into the zones the card
// draws it in.
//
// THE GRAMMAR IS THE PRODUCER'S AND IT IS A GRAMMAR, not prose this file is
// guessing at. internal/resident's compileReceipt writes it in one function and
// in one shape — `Here's my reading: <goal>`, then a line per `Assumed: <x>`,
// then `Correct me anytime …` — and internal/head/compiler.go splices
// `Verbatim request:` into the goal ahead of the user's own words. Those four
// marks are the whole vocabulary, they are written by two functions nobody
// rephrases, and the SENTENCES after them are free.
//
// So this is not 13.3.1's banned prose scan. That law forbids re-deriving
// structure the producer already has a typed column for; a commissioning row has
// exactly one column of prose and the structure inside it is a format the
// producer chose. What the law demands is what [reading.matched] provides: a
// text that does not answer the grammar is drawn as the plain wrapped paragraph
// it always was, never mangled into zones it does not have.
type reading struct {
	// goal is the sentence the task was made from, without the mark that
	// introduced it. It is the card's lead.
	goal string
	// verbatim is the reader's own words as the head copied them down, drawn as
	// a quoted sub-block in the user's voice (§3b).
	verbatim string
	// assumed are the things the head decided on its own, one per bullet — the
	// zone the user asked for by name ("separate assumptions with proper bullet
	// list").
	assumed []string
	// hint is the closing offer (`Correct me anytime …`), drawn dim at the
	// card's foot in §20's hints grammar rather than as a sentence of the body.
	hint string
	// rest is everything the grammar did not claim, whole and in order. It is
	// never dropped: the dressing presents the record (13.1 item 3).
	rest string
	// plain is the whole reading with the opening mark taken off and nothing
	// else touched — what an unmatched body opens onto.
	//
	// The mark comes off even there, and that is the one edit this file makes to
	// a record: `Here's my reading:` names a mechanism (§14) and the card's own
	// shape already says the head read something. Dropping it in the preview and
	// keeping it in the opened text would be the same sentence spelled two ways
	// on one card.
	plain string
	// matched says the grammar was actually found. False draws plain text.
	matched bool
}

// The four marks. They are matched on the way IN and are deliberately not
// tokens slots, exactly as [learningMarks] and trace.go's rule prefix are: a
// change to how a card draws can never stop a card from reading.
const (
	readingMark  = "Here's my reading:"
	assumedMark  = "Assumed:"
	verbatimMark = "Verbatim request:"
	amendMark    = "Correct me anytime"
)

// parseReading cuts one commissioning body into [reading].
func parseReading(body string) reading {
	body = strings.TrimSpace(body)
	if body == "" {
		return reading{}
	}
	var out reading
	var goal, verbatim, rest []string
	// zone is where an unmarked line belongs right now: the goal until a mark
	// moves it, the verbatim block while one is open.
	const (
		zoneGoal = iota
		zoneVerbatim
		zoneRest
	)
	zone := zoneGoal
	for _, line := range strings.Split(body, "\n") {
		trimmed := strings.TrimSpace(line)
		switch {
		case strings.HasPrefix(trimmed, readingMark):
			out.matched = true
			zone = zoneGoal
			if tail := strings.TrimSpace(trimmed[len(readingMark):]); tail != "" {
				goal = append(goal, tail)
			}
		case strings.HasPrefix(trimmed, verbatimMark):
			out.matched = true
			zone = zoneVerbatim
			if tail := strings.TrimSpace(trimmed[len(verbatimMark):]); tail != "" {
				verbatim = append(verbatim, tail)
			}
		case strings.HasPrefix(trimmed, assumedMark):
			out.matched = true
			zone = zoneRest
			if tail := strings.TrimSpace(trimmed[len(assumedMark):]); tail != "" {
				out.assumed = append(out.assumed, tail)
			}
		case strings.HasPrefix(trimmed, amendMark):
			out.matched = true
			zone = zoneRest
			out.hint = trimmed
		case trimmed == "":
			// A blank line closes the verbatim block and is otherwise the
			// paragraph boundary it always was.
			if zone == zoneVerbatim {
				zone = zoneRest
				continue
			}
			if zone == zoneGoal && len(goal) > 0 {
				zone = zoneRest
			}
		case zone == zoneVerbatim:
			verbatim = append(verbatim, trimmed)
		case zone == zoneGoal:
			goal = append(goal, trimmed)
		default:
			rest = append(rest, trimmed)
		}
	}
	out.goal = strings.Join(goal, " ")
	out.verbatim = strings.Join(verbatim, "\n")
	out.rest = strings.Join(rest, "\n")
	out.plain = body
	if head, tail, cut := strings.Cut(body, readingMark); cut && strings.TrimSpace(head) == "" {
		out.plain = strings.TrimSpace(tail)
	}
	// A mark with nothing behind it is not a grammar: a body that only ever said
	// `Here's my reading:` and then wandered has no zones to draw, and the plain
	// paragraph is the honest rendering of it.
	if out.verbatim == "" && len(out.assumed) == 0 && out.hint == "" {
		out.matched = false
	}
	return out
}

// -- the birth phases (§3's committed → planning → running) -------------------

// jobPhase names what is happening to a task before its work is running, in
// plain words derived from the GRAPH and never from a posted status line.
//
// §3 gives the first phase its exact spelling — "committed/planning: `◇ <title>`
// + `planning…` with a slow dim→bright breathe" — and the reader asked for the
// second by name: "when you are creating the initial task, also have like a
// 'creating workers for this' … we need to ensure we are giving all info". The
// gap between them is what a task looks like while its plan is landing: the
// children exist, none of them has started, and a card that said `planning…`
// through all of it would be describing a phase that had already ended.
//
// COUNTS ONLY WHEN TRUE AND NEVER A FRACTION. §3 forbids a denominator outright
// ("a dynamic graph cannot promise a denominator"), so the second phase says how
// many parts exist SO FAR and says nothing about how many there will be.
type jobPhase struct {
	// word is the status line, or "" once real work is running and the phase
	// line has nothing left to say.
	word string
	// breathing marks a phase that carries §18.2's breathe: the task is alive
	// and has produced nothing yet.
	breathing bool
}

// phaseWords are the two phases, in plain words a reader was never taught.
const (
	planningWord = "planning…"
	settingUp    = "setting up"
	// creatingWord is the phase BEFORE the first two: the head has commissioned
	// the work and the graph has not caught up yet.
	//
	// THE GAP IS REAL AND THE READER TIMED IT: "once it decides we are creating
	// task … there is like a few seconds where nothing happens". The
	// commissioning row is journaled the moment the head hands the work over;
	// the task NODE it names lands one snapshot later, and until it does the
	// board cannot answer with a name, a lifecycle or a receipt — so the card
	// that is about to exist could not be drawn and the thread sat silent
	// through the one interval a reader is watching hardest.
	//
	// It is not a fake and cannot become one: this phase is only ever reached
	// from a commissioning row that IS in the journal, so what is on screen is a
	// commission that has actually happened, drawn before the graph has caught
	// up with it (8.2.20 forbids inventing the fact, not drawing a fact early).
	creatingWord = "creating task…"
	// unstartedWord is the skeleton's ending when its command settles without
	// a task: the request was refused before any work existed, and the refusal
	// itself speaks in the thread.
	unstartedWord = "didn't start"
)

// jobTwigs is the optional read a live card's subtree comes from. Optional for
// the reason every other board read here is: a surface with no graph draws the
// card it can draw rather than no card.
type jobTwigs interface {
	jobParts(root string) []jobPart
}

// jobPart is one member of a task's subtree as the card draws it: the state
// glyph's lifecycle and the part's own name, and nothing else. §14's vocabulary
// law is why it is not called a node or a worker on any surface.
type jobPart struct {
	// Node is the graph id, NEVER RENDERED (§14, 5.14). It is here so the card
	// can be told what this part's recorder last said ([App.refreshCardPreviews])
	// without the renderer learning what a node is.
	Node string
	Name string
	Life rail.Lifecycle
	// Preview is the latest human line from this part's flight recorder, or "".
	// It is only ever filled for a RUNNING part — a settled one's words are its
	// result and a queued one has said nothing — and it is the same reading the
	// record page's tree draws (recordtree.go), a second consumer of one read.
	//
	// The changing text is not a fourth motion (§11 permits exactly three): what
	// moves is the WORK, and the spinner already on the part's own glyph is what
	// carries the movement.
	Preview string
}

// jobParts is the task's children, in the order the board holds them.
//
// It reads the scope cache and never the store, like every other read in this
// file: the walk happened once, at the last journal move. Only the FIRST level
// is drawn — the reader commissioned one thing and the shape of its parts is
// what says "workers are being made"; a whole tree in a chat card would be the
// room's job, one click away.
func (s *scopeSource) jobParts(root string) []jobPart {
	root = strings.TrimSpace(root)
	if s == nil || !s.ready || root == "" {
		return nil
	}
	scope, ok := s.tasks[rowTaskPrefix+root]
	if !ok {
		return nil
	}
	parts := make([]jobPart, 0, len(scope.Rows))
	for _, row := range scope.Rows {
		node := strings.TrimPrefix(row.ID, rowTaskPrefix)
		if node == root || node == "" {
			continue
		}
		parent := strings.TrimSpace(s.nodes[node].Parent)
		if parent != root {
			continue
		}
		name := strings.TrimSpace(row.Name)
		if name == "" {
			name = nodeLabel(node)
		}
		parts = append(parts, jobPart{Node: node, Name: name, Life: row.Life})
		if len(parts) == cardPartCap {
			break
		}
	}
	return parts
}

// cardPartCap is how many parts a card draws before the rest are the room's
// business. Six is a plan's opening; a card that grew with its graph stops
// being a card ([cardBodyRows] makes the same bargain for prose).
const cardPartCap = 6

// phaseOf reads the birth phase off the parts a task has.
//
// THE DISCRIMINATOR IS THE GRAPH AND NEVER THE PROSE (13.3.1), which here is
// also the only thing that works: the progress lines that used to narrate this
// are the work record's business now (§1) and never reach the conversation, so
// the phase has to be derived from what the snapshot says exists.
func phaseOf(facts jobFacts, parts []jobPart) jobPhase {
	if facts.Life != rail.LifeWorking && facts.Life != rail.LifeQueued {
		return jobPhase{}
	}
	if len(parts) == 0 {
		// AN ATOM IS NOT A TASK WAITING FOR A PLAN. A job that came out as one
		// leaf has no parts BY CONSTRUCTION — its root is its own single hand —
		// so "no parts" alone said `planning…` for the whole life of the
		// commonest job there is, breathing at a reader whose work was already
		// running. The graph settles it in one column: once the row has been
		// claimed the planning is over, whatever the shape turned out to be.
		//
		// What replaces it is nothing, and deliberately so. There is no tree
		// below to narrate and the card's own glyph already carries the life;
		// a phase line here would be a label doing structure's job (§15), which
		// is the same sentence that silences the phase the moment a part starts
		// below. Who is doing the work is a fact and not a phase, and it rides
		// the receipt where the other facts are ([taskReceipt], [commissionCells]).
		if facts.Started {
			return jobPhase{}
		}
		return jobPhase{word: planningWord, breathing: true}
	}
	for _, part := range parts {
		if part.Life == rail.LifeWorking {
			// Work is running and the tree below says so, row by row. A phase
			// line here would be a label doing structure's job (§15).
			return jobPhase{}
		}
	}
	word := settingUp
	if len(parts) == 1 {
		word += " " + tokens.GlyphSeparator + " 1 part so far"
	} else {
		word += " " + tokens.GlyphSeparator + " " +
			strconv.Itoa(len(parts)) + " parts so far"
	}
	return jobPhase{word: word, breathing: true}
}

// jobTokens is how much of the window one task burned, or nothing.
//
// It reads the ledger [scopeSource.readReceipts] filled and never the store, so
// a card costs no query of its own. THE LEDGER IS ONLY READ FOR JOBS SOMEBODY
// HAS OPENED, which is why a task nobody has entered draws no token cell — an
// absence, per §16, and a wiring gap rather than a rendering one: the read
// exists (store.SubtreeReceipts) and the only question is which roots it is
// asked for.
func (s *scopeSource) jobTokens(root string) (int, bool) {
	if s == nil || root == "" {
		return 0, false
	}
	rollup, ok := s.jobSpend(root).rollup(root)
	if !ok || rollup.Runs == 0 {
		return 0, false
	}
	burned := rollup.PromptTokens + rollup.CompletionTokens
	if burned <= 0 {
		return 0, false
	}
	return burned, true
}
