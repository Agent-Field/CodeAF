package chat

import (
	"strconv"
	"strings"

	"github.com/Agent-Field/aforge-v2/internal/store"
	"github.com/Agent-Field/aforge-v2/internal/tui2/blocks"
	"github.com/Agent-Field/aforge-v2/internal/tui2/rail"
	"github.com/Agent-Field/aforge-v2/internal/tui2/tokens"
)

// The work record: what a task room is a transcript OF.
//
// 4.6 says a v1 task room is "a view over the same journal, filtered to one
// node", and until this file existed that filter was one table: the messages
// anchored to the subtree. A resident-run task barely writes any. Measured on a
// live run (2026-08-11, `room-homes/room-repro/graph.db`), a commissioned job
// left EIGHT events under its node — `subtree_spliced`, `node_claimed`,
// `node_started`, two `usage_recorded`, `delivery_gate`, `node_completed`,
// `message_posted` — and exactly ONE of them was a message. Enter that room
// while it runs and the filter answers with nothing; enter it after it settles
// and the filter answers with one collapsed card. The reader who commissioned
// real work, watched a card move on the rail and then opened the room got
// 12.14's teaching line, forever, over work that demonstrably happened.
//
// The record was never missing. It was in the columns beside the ones the room
// read: a node's BRIEF is what it was asked to do, its SUMMARY is what it says
// it did, its ERROR is how it failed, and its STATUS, clock and waits-on edges
// are its lifecycle. The rail has been drawing all of it as a tree since 13.11.
// This file draws the same reading as a transcript, so the room says what the
// rail says — and neither one invents a row (5.20 rule 1, 8.2.20).
//
// WHAT THIS FILE IS NOW. The page's ORDER moved to recordpage.go when the
// record became chronological (user-amended 2026-08-11) and the per-part rows
// became the living tree in recordtree.go. What is left here is the reading —
// which nodes a page is about, what fingerprint decides a rebuild, which models
// billed it — and the one block both variants open on: the task card.
//
// WHAT THIS FILE DOES NOT DO. It writes nothing, and it invents nothing: every
// cell comes from the snapshot the rail was already built from, at the watermark
// the poll already paid for. What it now DOES buy, and only for the page a
// reader is standing in, is that page's own ledger and the models that billed it
// — [scopeSource.roomSpend] and [scopeSource.jobModels], one bounded read each
// per rebuild of an open room, because a receipt is the one fact the rail's
// per-job query cannot always answer for the node a page is about. Where the
// record is genuinely absent it stays absent: a part nothing has billed carries
// no money cell at all, which is a truer picture than `$0.00`.

// workRow is one node's own account of itself: the row the rail drew for it,
// paired with the node the row was drawn from.
//
// Both halves are needed and neither is redundant. The ROW carries the name,
// the lifecycle and the clock the rail already resolved — so the tree and the
// transcript cannot disagree about the same part, which is 12.14's law ("a
// preview that outranks the thing it previews is the affordance lying") asked
// of two renderings of one node. The NODE carries the prose a 28-column rail
// had no room for: the whole brief, the whole summary, the whole error.
type workRow struct {
	row  rail.Row
	node store.Node
}

// workRecord is one task's subtree as the room reads it: the surface row first,
// then every part in splice order.
//
// It is a map lookup and a walk over rows already built. The scope was compiled
// in the same pass that built the card (scope.go's taskRows), so this cannot
// issue a read and cannot see a different subtree than the rail is showing.
func (s *scopeSource) workRecord(root string) []workRow {
	root = strings.TrimSpace(root)
	if s == nil || root == "" {
		return nil
	}
	scope, ok := s.tasks[rowTaskPrefix+root]
	if !ok {
		// No scope means the board has never mentioned this node. A room over
		// it has no record to draw, which is a different thing from a record
		// that is empty, and both are handled by the caller finding nothing
		// here.
		return nil
	}
	out := make([]workRow, 0, len(scope.Rows))
	for _, row := range scope.Rows {
		id := strings.TrimPrefix(row.ID, rowTaskPrefix)
		node, known := s.nodes[id]
		if !known {
			continue
		}
		out = append(out, workRow{row: row, node: node})
	}
	return out
}

// recordStamp is what the room's transcript was last built from.
//
// A task room repaints on the cycles the journal moved, and most of those moves
// are not about this task. The stamp is every number that can change a row —
// each node's own update sequence and status, and the trail's length and tail —
// so an unrelated move costs one comparison instead of a rebuild. It is a
// fingerprint and not a hash: collisions would have to agree on every node's
// update sequence at once, which is the same thing as nothing having changed.
//
// MONEY IS ONE OF THE NUMBERS THAT CAN CHANGE A ROW, and until this line it was
// not in here — which is the second half of the reader's dead receipt ("· 4m ·
// $—" over four minutes of visible execution). A call settling writes a
// `usage_recorded` event and NOTHING else: `nodes.updated_seq` does not move,
// because the node did not move — it is still the same part, still running. So
// the poll saw the journal advance, re-read the ledger for the rail, and then
// the room compared a fingerprint that could not see the one thing that had
// changed and kept the frame it drew on entry. A room's header and every row of
// its tree are made of that ledger, so the ledger belongs in the fingerprint.
//
// It costs ONE rollup of the page's own root, not one per row: a receipt landing
// anywhere under a node changes that node's rollup, so the top of the subtree
// answers for all of it in a single walk of the slice already in hand.
func recordStamp(record []workRow, messages []store.Message, money spend) string {
	var b strings.Builder
	for i := range record {
		b.WriteString(record[i].node.ID)
		b.WriteByte(':')
		b.WriteString(strconv.FormatInt(record[i].node.UpdatedSeq, 10))
		b.WriteByte(':')
		b.WriteString(string(record[i].node.Status))
		b.WriteByte('|')
	}
	b.WriteByte('m')
	b.WriteString(strconv.Itoa(len(messages)))
	if n := len(messages); n > 0 {
		b.WriteByte(':')
		b.WriteString(strconv.FormatInt(messages[n-1].Seq, 10))
	}
	if len(record) > 0 {
		if roll, ok := money.rollup(record[0].node.ID); ok {
			b.WriteByte('$')
			b.WriteString(strconv.Itoa(roll.Runs))
			b.WriteByte(':')
			b.WriteString(strconv.FormatFloat(roll.Cost, 'f', -1, 64))
			b.WriteByte(':')
			b.WriteString(strconv.Itoa(roll.PromptTokens + roll.CompletionTokens))
		}
	}
	return b.String()
}

// -- the blocks --------------------------------------------------------------

// chargeBlockID is the page's TOP RULE — the titled seam that names the job and
// carries its whole receipt (recordpage.go's [chargeRule]). It is not a message
// sequence — nothing in the record is a message — so it carries its own prefix
// and can never collide with [messageID].
//
// It keeps the name it had when the top of the page was a card, and that is
// deliberate: the block at the top of a record page has one identity, which the
// transcript's anchor and cache key on, and renaming it would have moved every
// reader's scroll position on the wave that changed its dress.
const chargeBlockID = "room-charge"

// chargeAskID is the block UNDER the rule: the ask itself, in its zones. It is
// its own block because it is its own thing — the rule says which job this is,
// this says what was wanted — and because the reader's fold answer on the ask
// must survive a repaint that changed a figure in the rule (disclose.go).
const chargeAskID = "room-ask"

// chargeBlock is the page's first block: what this task was asked to do.
//
// It is the answer to the half of the report the parts cannot answer. "No plan"
// is a fair complaint about a record over an ATOMIC job, which has no parts for
// a plan to be made of — and the plan was journaled all along, as the node's
// brief, the same sentence the head's commissioning row shows in the room that
// commissioned it (13.3.1's columns, never prose). The owner thread has always
// had it; the page it is about did not.
//
// The header is the card the reader entered from, by construction: 12.14's
// finding 1 is that an entered record must never know less about a task than the
// card above it, so the name, the glyph and the telemetry are the row's own and
// are not derived a second time here.
//
// THE PAGE'S TOP IS A RULE AND THIS BLOCK IS NOT IT (user-amended 2026-08-11;
// see recordpage.go's header for the full amendment). The job's NAME and its
// whole receipt are one titled seam above this block ([chargeRule]); what is
// left here is the ask, which is what this block was always for.
//
// So it wears NO DRESS AND NO HEAD. It wore the commitment ground and a card
// header until this wave, and both were claims it could not make: a settled
// record cannot say "made and still running", and the name and receipt it drew
// were the same two facts the rule above now says once. What it draws is words.
//
// The reading is drawn in its ZONES rather than as a paragraph. `adoptPrompt` is
// the one renderer that knows them — the goal as the lead, the reader's verbatim
// request quoted in their own voice, the `Assumed:` bullets under it — and it is
// asked here rather than copied, so the card in the conversation and the top of
// the record it opens cannot spell one reading two ways (12.14).
//
// THE CONVERSATION IT CAME OUT OF GOES BEHIND THE DOOR AND NEVER ABOVE IT. A
// forked task inherits the recent chat so PLANNING is well-informed, and for one
// release that transcript was pasted into the instruction — so this block opened
// on a hundred and fifty lines of somebody's earlier morning, dressed as the ask
// (user journal, 2026-08-12). The ask is the lead; the context is a quotation
// under it, folded by construction rather than by a budget that happened to run
// out ([messageBlock.foldContext]).
func chargeBlock(item workRow, style *tokens.Styler, context string) *messageBlock {
	block := &messageBlock{id: chargeAskID, style: style, job: item.node.ID}
	// The reading, in its zones. A brief that does not answer the head's grammar
	// is drawn as the plain paragraph it always was — [parseReading.matched] is
	// what keeps 13.3.1's rule true, and `adoptPrompt` reads it for us.
	block.adoptPrompt(chargeReading(item.node))
	// THE RECORD PAGE SHOWS MORE OF THE ASK THAN THE CONVERSATION DOES (user
	// review, 2026-08-11): "the top card can have a lot more lines before view
	// more as tasks are pretty large". A card in the thread is one row of a
	// conversation and keeps its one-line preview; this card IS the top of a
	// page whose whole subject is that ask, so it opens the reading down to a
	// budget and folds only what is past it.
	block.openReading(recordPromptRows)
	// A SEAM SEPARATES TWO HALVES AND THIS CARD HAS ONE. On the conversation's
	// commitment card the seam divides what was asked from what is happening to
	// it; here what is happening to it is the whole rest of the PAGE, below this
	// block, so the seam would be a shade row with nothing under it — and, off a
	// card ground, a second blank line against the one the block already leaves
	// (§20: exactly one blank between blocks).
	if n := len(block.segs); n > 0 && block.segs[n-1].kind == segSeam {
		block.segs = block.segs[:n-1]
	}
	// After the budget, deliberately: the context is not competing with the ask
	// for the rows above the fold, it is behind the fold whatever the ask cost.
	block.foldContext(context)
	block.markLead()
	return block
}

// foldContext puts the conversation an ask came out of behind the block's door.
//
// It is appended AFTER [messageBlock.openReading] rather than handed to it,
// because the two are different judgements. The reading's zones compete for a
// line budget — a small ask stands open, a large one folds the remainder — and
// the context is not in that competition at all: it is evidence about what the
// ask meant, it is the length of a conversation rather than the length of a
// sentence, and a page that let it win the budget would be a page about the room
// instead of the work.
//
// It wears the reader's own voice ([readingSegs]' verbatim zone: dim, indented,
// `│` in the gutter), because that is what it is — people talking, quoted inside
// a card, and not more of the head's sentence.
func (b *messageBlock) foldContext(context string) {
	context = strings.TrimSpace(context)
	if b == nil || context == "" {
		return
	}
	b.segs = append(b.segs, segment{
		kind: segGutter, text: context, indent: bodyIndent + partIndent, folded: true,
	})
	b.collapsible, b.tailFold = true, true
	b.hidden += readingRows(context)
	b.measured = false
}

// chargeReading is what the room's top card LEADS WITH: the ask this work was
// admitted from, with the brief its worker was handed underneath.
//
// THE DEFECT, from the reader's own screenshot (2026-08-11). The card in the
// conversation opened on the task's own ask — "Deliver a single self-contained
// HTML file that is a fast, poli…" — and the room that card opens led with
// "Deliver the result of build a single-file precision arcade game… Every step
// of the craft arrives as one of your inputs… Assemble them into the one answer
// …". That sentence is not a description of the task and was never written for a
// reader: it is internal/resident/craftadapter.go's craftRootBrief, the
// ASSEMBLING WORKER'S instructions, addressed in the second person to the model
// that folds a craft's steps into one answer. The room was reading `nodes.brief`
// — the right column for a PART, and the wrong one for the job, whose brief is
// machinery on every producer that writes one (the craft's assemble leaf, the
// resident's deliver-together fold). 12.14's law is that an entered record may
// never know less about a task than the card above it; leading with the worker's
// errand while the card led with the ask breaks it in the one place a reader
// looks first.
//
// SO THE LEAD IS THE ASK, AND IT IS READ OFF A TYPED COLUMN. `nodes.intent` is
// the sentence the splice was admitted from — store.Splice refuses a subtree
// without one — and it is what every producer records as "what the person asked
// for", in the person's own words (internal/resident's command.Instruction, the
// craft run's intent). No prose is scanned to find it, which is 13.3.1's whole
// rule: the structure was written down by the producer and this reads the field.
//
// NOTHING IS HIDDEN. The brief follows the ask in the same block, behind the
// same disclosure the reading has always used ([openReading]) — the worker's
// errand is still the record of what was handed down, and 13.1 item 3 forbids
// the dressing editing the record. What changed is which sentence is first.
//
// A PART'S PAGE IS NOT ITS JOB'S. Provenance rides the SPLICE, so every node
// under a job carries the job's ask; a drilled-into part that led with it would
// say the job's sentence over the part's own work. A part's ask IS its brief, so
// only the node the ask was made of — one parented on the spine, or on nothing —
// takes the intent, and every other page reads exactly what it read before.
func chargeReading(node store.Node) string {
	brief := strings.TrimSpace(node.Brief)
	ask := strings.TrimSpace(node.Provenance.Intent)
	if ask == "" || !ownsTheAsk(node) {
		return brief
	}
	// A brief that already carries the ask is the ask (a one-line job's brief is
	// its own instruction), and saying it twice on one card is §19 outright.
	if brief == "" || strings.Contains(brief, ask) {
		return brief
	}
	return ask + "\n\n" + brief
}

// ownsTheAsk says this node is the one the ask was made OF, rather than one of
// the parts the ask was compiled into. The spine is where a job hangs from
// (13.8 finding 4), so a node parented on it — or on nothing, which is what an
// un-parented root reads as — is a job, and everything below it is a part.
func ownsTheAsk(node store.Node) bool {
	parent := strings.TrimSpace(node.Parent)
	return parent == "" || parent == store.RootID
}

// chargeCells is the entered record's FULL RECEIPT, in the order §16 reads a
// receipt: how long it took, what it cost, how much it burned, and who did it.
//
//	─ wisp-parity-2 · swe · 28m · $8.65 · 41k tok · claude-k3 ───────────────
//
// It hands back CELLS rather than one joined string, because the row it lands on
// is [blocks.Ruled] and the separator between telemetry cells is that renderer's
// to draw — a caller that pre-joined them would be spelling the separator a
// second time, and a rule that had to shed a cell under width pressure could
// only shed the whole receipt.
//
// It is the ROLLUP and not the row's own figure, because a record is about a
// whole job: store.SubtreeLedger.Rollup answers for any member of the subtree
// from ONE ledger — the one the page took ([scopeSource.roomSpend]) — so the
// header and every row of the tree beneath it are quoting a single read taken at
// a single moment and cannot disagree.
//
// EVERY FIGURE IS ABSENT WHEN IT IS UNKNOWABLE (§16, 8.2.20). Money is the one
// cell that renders its absence, as `$—`, because a job with no cost cell and a
// job that cost nothing are different facts and money is the number a reader
// looks for. A token count that has not been billed yet is simply not there —
// a running leaf usually carries no usage row at all, so a live figure would be
// a floor presented as a total.
//
// THE MODEL CELL IS THE SUBTREE'S WHEN THE SUBTREE CAN BE ASKED. §5 asks this
// header for "the models that did the work, deduped, dim", and the deduped list
// of everything that actually billed a run lives in `usage.model` — reachable
// now through [Models], which is the seam this lane opened. What is drawn is
// what RAN.
//
// The rail's single model word is the fallback and not a second opinion: it is
// the binding on the job's own row, which on an escalating job names the rung
// the work STARTED on rather than the one that finished it. A backend with no
// usage read keeps it, exactly as before, because a true-and-incomplete word
// beats no word — and a backend that HAS the read must not draw both, or the
// header would name the same fact twice and disagree with itself on the job
// that escalated.
//
// THE MONEY IS THE SUBTREE'S OWN LEDGER FIRST (user-reported, 2026-08-11: "· 4m
// · $—" over four minutes of visible execution, with no burn beside it). The
// row's figure is the BOARD's reading of this job, and the board only carries a
// per-job number for the roots its one usage query answers for — a job hung
// under a territory, or one whose ledger the rail had not read yet, arrives with
// nothing on it. The room has a stronger read in hand: [scopeSource.roomSpend]
// is this page's own subtree ledger, re-taken with the journal, so the header
// rolls up whatever has billed anywhere under this node as the calls settle. The
// row stays as the fallback, because where both answer they are the same
// arithmetic over the same table and the row costs nothing.
//
// BILLED IS THE GATE ON BOTH FIGURES, exactly as it is on the rail's own row
// (scope.go's taskCard). A rollup with no runs in it is a subtree nothing has
// measured yet, and `$0.00` for work in flight is a settled-looking number over
// an unsettled fact (8.2.20). Money then renders its ABSENCE — `$—`, the one
// cell that does, for the reason this comment gives above — and the burn renders
// as nothing at all.
func chargeCells(item workRow, money spend, models []string) []string {
	cells := make([]string, 0, 6)
	// THE SPECIALIST WORD, and only for a specialist. Work executes on atomic
	// harnesses and the store settles which one on the node itself, so this is
	// read rather than attributed. It is drawn EXACTLY as v1 draws it
	// (internal/tui/cards.go's settledWorker): whatever string is there, dim,
	// verbatim, with no branch on any worker's name — a surface that learned one
	// name would need editing every time a worker is added. A default job leaves
	// the field empty and therefore says nothing, which is the whole reason the
	// word means something on the job that has one.
	//
	// It was a bracketed BADGE while the top of this page was a card. A rule has
	// no badges and wants none: a badge is a chip on a title row, and this row is
	// a boundary — so the word joins the receipt it was always sitting beside.
	if worker := settledWorker(item.node); worker != "" {
		cells = append(cells, worker)
	}
	if item.row.Meta.HasElapsed {
		cells = append(cells, tokens.Elapsed(item.row.Meta.Elapsed))
	}
	rollup, billed := money.rollup(item.node.ID)
	billed = billed && rollup.Billed()
	switch {
	case billed:
		cells = append(cells, tokens.Money(rollup.Cost))
	case item.row.Meta.HasCost:
		cells = append(cells, tokens.Money(item.row.Meta.Cost))
	default:
		cells = append(cells, tokens.GlyphSpend+tokens.GlyphMissing)
	}
	if billed {
		if burned := rollup.PromptTokens + rollup.CompletionTokens; burned > 0 {
			cells = append(cells, tokens.GlyphEstimate+tokens.Count(int64(burned))+" tok")
		}
	}
	// HUMANE WORDS AND NEVER SLUGS (user-directed 2026-08-11). `anthropic/
	// claude-k3-20260114` is a provider id, and a provider id on a receipt is
	// 5.14's never-shown tier reaching a cell. [modelWords] is the board's own
	// shortening, and the dedupe happens AFTER it because two ids routinely
	// collapse onto one word — a header that said `k3 · k3` would be counting one
	// model as two.
	switch words := modelWords(models); {
	case len(words) > 0:
		cells = append(cells, words...)
	case item.row.Meta.Model != "":
		if word := modelWord(item.row.Meta.Model); word != "" {
			cells = append(cells, word)
		}
	}
	return cells
}

// Models is the optional read that names WHO DID THE WORK: every model that
// billed a run anywhere under one node, deduped.
//
// It is [Receipts]' third twin and it is asked in the same breath: the ledger
// read says what a subtree COST and this says what SPENT it, two columns of one
// table, and *store.Store and *command.Commander each answer both. A backend
// that answers neither leaves [chargeCells] on the rail's own model word, which
// is what every record drew before this existed.
//
// Why it is not on [Graph]: for the reason [Subtrees] and [Trace] are not
// either — a method on Graph is a method a Backend must have, and a host that
// lacks this one would lose the whole rail over a single dim word.
type Models interface {
	NodeModels(nodeID string) ([]string, error)
}

// Commands is the optional read that recovers THE OTHER HALF OF THE ASK: the
// conversation a forked task came out of.
//
// The ask and its context are two typed fields on the command (store's ask.go),
// and only the ask rides onto the node as provenance. That asymmetry is
// deliberate — provenance is copied onto every node of a subtree, and a room's
// worth of transcript copied thirty times is a cost nobody chose — so a page
// that wants to SHOW the context has to ask the journal for it, once, here.
//
// Optional for the reason every other read on this surface is: a backend that
// cannot answer draws the ask alone, which is exactly the page every record drew
// before the two halves were told apart.
type Commands interface {
	CommandBySeq(seq int64) (store.Command, bool, error)
}

// jobContext is the conversation an entered job's ask came out of, or none.
//
// ONE READ PER REBUILD OF AN OPEN ROOM, on [scopeSource.jobModels]' bargain and
// no looser: paintRoom is stamped on the journal, so this runs when the record
// itself is being rebuilt and at no other time.
//
// It answers for JOB ROOTS only ([commandSeqOf]), which is [ownsTheAsk] stated
// where the read is: a drilled-into part's page is about the part, and leading
// it with the room the whole job was commissioned in would be the same mistake
// in the other direction.
func (s *scopeSource) jobContext(nodeID string) string {
	if s == nil || s.commands == nil {
		return ""
	}
	seq, ok := commandSeqOf(nodeID)
	if !ok {
		return ""
	}
	command, found, err := s.commands.CommandBySeq(seq)
	if err != nil || !found {
		return ""
	}
	return strings.TrimSpace(command.Context)
}

// jobModels is the deduped list of models that ran an entered job, or none.
//
// ONE READ PER REBUILD OF AN OPEN ROOM, which is [scopeSource.readReceipts]'
// bargain and not a looser one: paintRoom is stamped on the journal and returns
// without rebuilding when nothing this room cares about moved, so this runs when
// the record itself is being rebuilt and at no other time. A room nobody is
// standing in costs nothing, and the home rail never asks.
//
// The dedupe is here as well as in the store, and that is deliberate rather than
// redundant: [Models] is an interface, so what answers it is whatever the host
// plugged in, and a host whose usage table records one row per RUN would hand
// back the same model name once per call. Order is FIRST APPEARANCE, so the
// store's own ordering — most expensive first — survives the pass untouched.
//
// A read that fails is drawn as absence and never as an error row. The models
// are a dim cell on a header; a record that refused to draw because it could not
// name them would be spending the whole room on the least of its facts.
func (s *scopeSource) jobModels(root string) []string {
	root = strings.TrimSpace(root)
	if s == nil || root == "" {
		return nil
	}
	if s.models == nil {
		return nil
	}
	models, err := s.models.NodeModels(root)
	if err != nil || len(models) == 0 {
		return nil
	}
	out := make([]string, 0, len(models))
	seen := make(map[string]bool, len(models))
	for _, model := range models {
		model = strings.TrimSpace(model)
		if model == "" || seen[model] {
			continue
		}
		seen[model] = true
		out = append(out, model)
	}
	return out
}

// settledWorker is the node's own answer to "who runs this", in the order every
// dispatch path already reads it: the worker admission settled on the row, the
// subtree's choice otherwise.
//
// It is internal/tui/cards.go's function of the same name, read across rather
// than re-derived, because "which harness ran this" has one answer and two
// surfaces asking would be two answers waiting to disagree.
func settledWorker(node store.Node) string {
	if settled := strings.TrimSpace(node.Subharness); settled != "" {
		return settled
	}
	return strings.TrimSpace(node.Provenance.Subharness)
}

// THE PER-PART WORK ROW IS GONE (user-amended 2026-08-11). It drew one node's
// glyph, name, clock and folded words as a transcript block, stacked in splice
// order — which at four parts was four paragraphs about a job whose subject was
// the whole. The living tree says the same rows in one tight unit, on the grid,
// with a receipt each (recordtree.go), and a part's own WORDS are its own page
// one click down. What survives here is [workBody], because both of those still
// ask a node what it has to say for itself.

// workBody is what a part actually said about itself.
//
// A FAILURE SPEAKS ITS ERROR AND NOT ITS SUMMARY. A node that failed may carry
// both — the summary is how far it got, the error is why it stopped — and the
// second one is the fact the reader opened the room for. They are joined rather
// than chosen between, error first, because 13.1 item 3's rule is that the
// dressing presents the record whole and never edits it.
func workBody(node store.Node) string {
	summary := strings.TrimSpace(node.Summary)
	reason := strings.TrimSpace(node.Error)
	switch {
	case reason == "":
		return summary
	case summary == "":
		return reason
	default:
		return reason + "\n\n" + summary
	}
}

// waitsWord is how a blocked row says what it is behind, ONE spelling
// product-wide: internal/tui2/rail/card.go writes `waits: <deps>` and this row
// is the same fact one column over. Two spellings of one thing is two things to
// the eye (12.14: a preview and the room it previews may not disagree).
const waitsWord = "waits: "

// gistCap is how much of its own words a collapsed row shows before the rest
// folds — about two rows at an ordinary transcript width.
//
// A cap is needed and a newline is not enough. [splitHeadline] folds at the
// first line break, which is the right boundary for a worker that wrote a
// headline and then its detail, and NO boundary at all for one that wrote a
// single four-hundred-character paragraph. Measured live at 120 columns: one
// part's unbroken result took six rows and pushed the job's own charge, and the
// three parts under it, off the screen — which is 13.10's finding ("a job that
// wrote a long answer pushed the conversation off the screen") arriving one
// surface later, and 5.9's progressive disclosure is the law it breaks.
const gistCap = 160

// splitGist is [splitHeadline] with a length of its own.
//
// It cuts at the first line break, and then, only if what it kept is still too
// long to be a status line, at the last sentence end before the cap — falling
// back to the last word boundary when the paragraph has no sentence in it. The
// FOLD KEEPS EVERY WORD IT MOVED: this splits the record, it never shortens it,
// so an expanded row still reads exactly what the worker wrote (13.1 item 3).
func splitGist(body string, limit int) (gist, rest string) {
	head, tail := splitHeadline(body)
	join := func(a, b string) string {
		a, b = strings.TrimSpace(a), strings.TrimSpace(b)
		switch {
		case a == "":
			return b
		case b == "":
			return a
		default:
			return a + "\n" + b
		}
	}
	if len(head) <= limit {
		return head, tail
	}
	cut := sentenceCut(head, limit)
	if cut <= 0 {
		cut = strings.LastIndexByte(head[:limit], ' ')
	}
	if cut <= 0 {
		return head, tail
	}
	return strings.TrimSpace(head[:cut]), join(head[cut:], tail)
}

// sentenceCut is the byte after the last sentence that ends inside limit, or
// zero when there is none. A terminator only counts when a space follows it, so
// a version number or an ellipsis is not mistaken for the end of a thought.
func sentenceCut(text string, limit int) int {
	if limit > len(text) {
		limit = len(text)
	}
	for i := limit - 1; i > 0; i-- {
		switch text[i] {
		case '.', '!', '?':
			if i+1 < len(text) && text[i+1] == ' ' {
				return i + 1
			}
		}
	}
	return 0
}

// -- assembling the room -----------------------------------------------------

// hasRecord is the emptiness question, and it is the one 12.14's teaching line
// is the answer to.
//
// A ROOM IS EMPTY WHEN THE JOURNAL HAS NOTHING TO SAY ABOUT IT — not when its
// message trail is empty, which is the reading this wave replaces. A task's name
// and its status are not a record: they are the CARD, and the card is what the
// empty room draws beside the line explaining why there is nothing under it. So
// the question is asked of the four things that would be worth reading — a part,
// a message, the job's brief, or the job's own account of how it went — and the
// teaching line survives exactly as long as all four are absent.
// nodeTraceBlocks is one part's execution rows, or none when its worker has not
// written a recorder — which is the ordinary case for a node that plans rather
// than runs, and is an ABSENT row and never an empty one.
func nodeTraceBlocks(item workRow, traces map[string]nodeTrace,
	style *tokens.Styler, open func(id string) bool, clock *blocks.Clock) []blocks.Block {

	held, ok := traces[item.node.ID]
	if !ok {
		return nil
	}
	// The recorder's own mtime is where a running row's clock starts: a call
	// line is written the moment the call is issued, so on a tail whose last
	// line is an unreturned call the file's last write IS that call's start
	// (trace.go's [traceLive]).
	return traceBlocks(item.node.ID, parseTrace(held.text), style, open,
		traceLive{clock: clock, since: held.mod})
}

// hasTrace says a room has execution rows to draw even if the journal has
// nothing to say about it.
//
// It exists because of the exact shape of the bug this lane fixes: a worker can
// be fifteen seconds into a run, have made four tool calls, and have journaled
// NOTHING but `node_started` — H13 measured precisely that. Without this clause
// such a room would draw 12.14's teaching line ("nothing journaled here yet")
// over a trace sitting on disk beside it, which is the same class of lie one
// layer further in.
func hasTrace(record []workRow, traces map[string]nodeTrace) bool {
	for i := range record {
		if held, ok := traces[record[i].node.ID]; ok && strings.TrimSpace(held.text) != "" {
			return true
		}
	}
	return false
}

func hasRecord(record []workRow, messages []store.Message) bool {
	if len(messages) > 0 || len(record) > 1 {
		return true
	}
	if len(record) == 0 {
		return false
	}
	return strings.TrimSpace(record[0].node.Brief) != "" || workBody(record[0].node) != ""
}

// jobSpend is the entered job's own ledger, or the absent one.
//
// It reads the map [scopeSource.readReceipts] already filled — one query per
// room the reader has opened, re-read with the journal — so an entered record
// costs no store read of its own for its receipt. It lives HERE rather than
// beside the map because the map is the rail's read and this is the record's
// question; the type it hands back is the rail's own, so the two renderings of
// one job's money cannot diverge.
func (s *scopeSource) jobSpend(root string) spend {
	if s == nil {
		return spend{}
	}
	ledger, ok := s.ledgers[strings.TrimSpace(root)]
	if !ok {
		return spend{}
	}
	return spend{ledger: ledger, have: true}
}

// roomSpend is the ledger the OPEN PAGE draws its receipt from: this node's own
// subtree, whatever the board happens to have read for the rail.
//
// THE DEFECT IT FIXES, measured on the reader's own run: a task room four
// minutes into a live job drew `· 4m · $—` with no burn beside it and no money
// on any tree row, while the ledger read that answers all three sat one call
// away. [scopeSource.readReceipts] fills its map for the roots the RAIL knows —
// the rooms the subtree read has come back for, and the first few cards on the
// home — and a page can be standing over a node that is in neither: a job whose
// subtree read has not landed yet, a job hung somewhere the home rail's task
// rows do not name, or a PART the reader drilled into, which is never a ledger
// root because a ledger is read per job.
//
// So the page asks in three widening steps, and only the last one costs a query:
//
//  1. the ledger keyed by this node, which is the entered job's own;
//  2. any ledger that CONTAINS this node, which is what a drilled-into part is
//     — its job's ledger already holds its receipt, and re-reading would be a
//     second opinion about one subtree taken at two moments (record.go's header);
//  3. the read itself, [Receipts.SubtreeReceipts], for a page the rail has not
//     paid for.
//
// ONE READ PER REBUILD OF AN OPEN ROOM, which is [scopeSource.jobModels]'
// bargain and not a looser one: paintRoom is stamped on the journal and returns
// without rebuilding when nothing this room cares about moved. The answer is
// kept in the same map the rail's own reads land in, so the second rebuild of
// one frame is free and the next poll re-takes it — money moves with the
// journal, and readReceipts replaces the map on every refresh.
//
// A read that fails is drawn as absence and never as an error row: the page's
// figures go quiet and its rows stay, which is [Receipts]' own rule.
func (s *scopeSource) roomSpend(root string) spend {
	root = strings.TrimSpace(root)
	if s == nil || root == "" {
		return spend{}
	}
	if money := s.jobSpend(root); money.have {
		if _, member := money.rollup(root); member {
			return money
		}
	}
	for _, ledger := range s.ledgers {
		if _, member := ledger.Rollup(root); member {
			return spend{ledger: ledger, have: true}
		}
	}
	if s.receipts == nil {
		return spend{}
	}
	ledger, err := s.receipts.SubtreeReceipts(root)
	if err != nil {
		return spend{}
	}
	if s.ledgers == nil {
		s.ledgers = make(map[string]store.SubtreeLedger, 4)
	}
	s.ledgers[root] = ledger
	return spend{ledger: ledger, have: true}
}

// recordPromptRows is how much of the reading stands above the record page's
// fold: about a screenful's half, which is where a large task's goal, the
// reader's verbatim words and its assumptions all fit without the page becoming
// the ask instead of the work.
const recordPromptRows = 15

// openReading unfolds the reading's zones down to a line budget.
//
// It counts SOURCE lines and not rendered rows, deliberately: the rendered
// height is a fact about the frame's width, and a budget that changed with the
// terminal would make the same record two different documents. A source line is
// what the producer wrote, which is what the budget is a judgement about.
//
// THE ONE-LINE PREVIEW GOES WHEN THE ZONES ARRIVE. `adoptPrompt` puts the goal
// on a `segPrompt` row and the whole reading behind the fold; with the reading
// standing open the two are the same sentence twice on one screen, which §19
// names outright. So the peek row is dropped rather than kept above the zones
// it duplicates.
//
// A reading that fits inside the budget folds NOTHING and offers no door — 5.20
// rule 3 forbids naming an affordance that opens onto an empty room.
func (b *messageBlock) openReading(budget int) {
	if b == nil || budget < 1 {
		return
	}
	// A card with nothing folded has nothing to open, and dropping its peek row
	// would leave the top of the page with no words at all. A reading that fits
	// on one line is that case exactly.
	opened := false
	for _, seg := range b.segs {
		if seg.folded {
			opened = true
			break
		}
	}
	if !opened {
		return
	}
	used, folded := 0, 0
	segs := make([]segment, 0, len(b.segs))
	for _, seg := range b.segs {
		if seg.kind == segPrompt {
			continue
		}
		if !seg.folded {
			segs = append(segs, seg)
			continue
		}
		lines := readingRows(seg.text)
		if used+lines <= budget {
			seg.folded = false
			used += lines
		} else {
			folded += lines
		}
		segs = append(segs, seg)
	}
	b.segs = segs
	b.hidden = folded
	if folded == 0 {
		b.collapsible, b.tailFold, b.expanded = false, false, false
	}
}

// readingRows is how many ROWS one zone will cost, near enough to spend a budget
// on.
//
// It is an estimate and says so: the true height is a fact about the frame's
// width, and a budget that changed with the terminal would make one record two
// different documents. What it must not do is count a four-hundred-character
// paragraph as one line — that is the shape [splitGist] exists for, and it is
// the shape a source-line count gets wrong by an order of magnitude — so each
// source line is charged at the prose measure the surface actually reads at.
func readingRows(text string) int {
	rows := 0
	for _, line := range strings.Split(strings.TrimSpace(text), "\n") {
		rows += 1 + len(line)/tokens.ProseMeasure
	}
	if rows < 1 {
		return 1
	}
	return rows
}
