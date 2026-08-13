package chat

import (
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/Agent-Field/aforge-v2/internal/store"
	"github.com/Agent-Field/aforge-v2/internal/thread"
	"github.com/Agent-Field/aforge-v2/internal/tui2/blocks"
	"github.com/Agent-Field/aforge-v2/internal/tui2/composer"
	"github.com/Agent-Field/aforge-v2/internal/tui2/rail"
)

// The two feeds, and the one transcript they agree on.
//
// A chat surface has exactly two sources of new rows and they race by
// construction: the durable journal, read on a timer, and the provider's tokens,
// arriving over a channel while the message they will become does not exist
// yet. The old surface reconciled them with four booleans, a queue and a
// typewriter; this one reconciles them with a rule:
//
//	the transcript is the journal, plus at most one live turn at its tail.
//
// Every settled block came out of the store. The live turn is detached before
// new journal rows are appended and re-attached after, so it can never end up
// above a row that is older than it, and it is dropped outright the moment the
// durable reply it was previewing lands. There is no third state, no queue and
// nothing to leak: a turn is live or it is journaled.

type pollTickMsg struct{}

type animTickMsg struct{}

// pollResultMsg is one store read. quiet says the journal watermark had not
// moved, so no other field was read and none carries meaning — the whole point
// of the poll being cheap.
//
// more says the page came back full, which is the store's way of saying it had
// to stop rather than that it had finished. It exists because the two watermarks
// in this struct measure different things: journal counts every event the engine
// ever wrote, and the read watermark counts only this room's messages, so one
// page of messages can cover an arbitrary span of journal. A read that stopped
// mid-room has consumed no journal watermark at all, and saying otherwise is
// what stranded the tail of long rooms — see applyPoll.
type pollResultMsg struct {
	journal  int64
	quiet    bool
	more     bool
	messages []store.Message
	err      error

	// room and turn are the two money windows (10.5.23), read on the same trip
	// as the messages because they answer the same watermark: a journal that
	// did not move cannot have billed anything, so a quiet poll carries neither
	// and the numbers on screen stay exactly as they were.
	room, turn  store.RoomSpend
	haveRoom    bool
	haveTurn    bool
	spendFailed bool

	// day is the whole machine's spend today, for the footer's right zone
	// (§13: "Day total lives in the bottom bar"). Absent renders nothing.
	day     float64
	haveDay bool

	// commands is this session's commissioned-but-not-yet-consumed work, read
	// on the same trip as the messages. It is what the pre-card skeleton is
	// drawn from: a splice sits pending for as long as compile and plan take,
	// and that whole interval used to render as silence. haveCommands is
	// PRESENCE: a backend that cannot answer makes no claim, and a skeleton
	// may only be buried on a claim.
	commands     []store.Command
	haveCommands bool
}

type postResultMsg struct {
	message store.Message
	err     error
}

type streamBatchMsg struct{ events []StreamEvent }

type streamClosedMsg struct{}

// -- the poll chain ----------------------------------------------------------

// startPoll requests a read. A read already in flight turns the request into a
// poke the result honours, so there is never more than one chain.
func (a *App) startPoll() tea.Cmd {
	if a.backend == nil {
		return nil
	}
	if a.polling {
		a.poke = true
		return nil
	}
	a.polling = true
	return a.pollCmd()
}

// pollCmd is the read itself, off the render goroutine. It asks the cheapest
// question first: an unchanged event watermark is proof that the thread, the
// graph and everything else projected beside them are unchanged too.
func (a *App) pollCmd() tea.Cmd {
	backend, session, journal, watermark := a.backend, a.session, a.journal, a.watermark
	return func() tea.Msg {
		seq, err := backend.LatestEventSeq()
		if err != nil {
			return pollResultMsg{err: err}
		}
		if seq == journal {
			return pollResultMsg{journal: seq, quiet: true}
		}
		messages, err := backend.Messages(session, watermark, messagePage)
		if err != nil {
			return pollResultMsg{journal: seq, err: err}
		}
		result := pollResultMsg{journal: seq, messages: messages,
			more: len(messages) >= messagePage}
		result.readSpend(backend, session)
		if reader, ok := backend.(commandReader); ok {
			if pending, err := reader.PendingCommands(pendingCommandRead); err == nil {
				result.haveCommands = true
				for _, command := range pending {
					if command.Kind == store.CommandSplice && command.SessionID == session {
						result.commands = append(result.commands, command)
					}
				}
			}
		}
		return result
	}
}

// commandReader is the optional read behind the pre-card skeleton. It is an
// optional interface on the backend rather than a method on [Backend] for the
// reason the notebook's is: *store.Store answers today, and a host that cannot
// simply has no skeletons — the card still arrives with the receipt.
type commandReader interface {
	PendingCommands(limit int) ([]store.Command, error)
}

// pendingCommandRead bounds the pending read, the same figure the head's own
// twin guard uses. A session with more splices in flight than this has a
// problem no skeleton can narrate.
const pendingCommandRead = 32

// readSpend asks the two money windows on the same trip as the messages.
//
// A failed read is recorded and NOT raised: money is one cell on two strips, and
// a chat that stopped rendering its transcript because the usage table would not
// answer would have let an ornament take the surface down. What a failure does
// is keep the missing glyph, which is the same thing an unanswered question
// always renders as here (8.2.20).
func (r *pollResultMsg) readSpend(backend Backend, session string) {
	room, ok, err := backend.SessionSpend(session)
	if err != nil {
		r.spendFailed = true
	} else if ok {
		r.room, r.haveRoom = room, true
	}
	turn, ok, err := backend.TurnSpend(session)
	if err != nil {
		r.spendFailed = true
	} else if ok {
		r.turn, r.haveTurn = turn, true
	}
	// The day total is an optional read for the same reason the two windows
	// are cheap ones: it rides the trip the messages already paid for.
	if today, ok := backend.(interface{ SpendToday() (float64, error) }); ok {
		if v, err := today.SpendToday(); err == nil {
			r.day, r.haveDay = v, true
		}
	}
}

// afterPoll re-arms the chain: at once if something asked while the read was
// out or the last page did not reach the end of the room, on the cadence
// otherwise.
//
// The immediate re-arm cannot spin. A full page is only reported when the store
// handed over messagePage rows, every one of which raises the read watermark, so
// each extra read strictly advances and the run ends at the first short page —
// the same drain-to-empty loop the head runs over the same journal
// (internal/head/head.go, poll).
func (a *App) afterPoll() tea.Cmd {
	a.polling = false
	if a.poke || a.behind() {
		a.poke = false
		return a.startPoll()
	}
	return tea.Tick(a.pollEvery, func(time.Time) tea.Msg { return pollTickMsg{} })
}

// applySpend folds the two money windows into the two strips that carry them.
//
// 10.5.23's split is the hard line and it is enforced here rather than trusted:
// the ROOM's bill goes to the status line, THIS TURN's to the composer's meta
// strip, and neither number is written to the other. Every "not answered" case
// keeps the missing glyph — a room with no window, a window with no runs, a
// model whose catalog entry has no size — because 8.2.20 forbids an estimate
// standing in for a fact, and a zero is the loudest estimate there is.
func (a *App) applySpend(result pollResultMsg) {
	if result.quiet {
		return
	}
	// The room's own bill goes to the ROOM ROW in the rail, not to the status
	// line: 10.5.23 gives that row system health and the question count and says
	// in as many words that nothing on it mentions money. The rail card is where
	// 5.9 puts a room's money, and it is the one place it is not a duplicate.
	if a.source != nil {
		a.source.SetRoomSpend(a.session, result.room.Cost(),
			result.haveRoom && result.room.Recorded())
	}
	if result.haveDay {
		a.status.spend, a.status.haveSpend = result.day, true
	}
	// THIS TURN's cost and the context gauge used to land on the composer's
	// meta strip, one row up. §7 dissolved that strip into the bar row: the cost
	// is a middle-zone chip that exists only while the turn does, the gauge is a
	// standing fact on the right. The READ is unchanged — the same journal, the
	// same watermark — only the address it is written to.
	if !result.haveTurn || !result.turn.Recorded() {
		a.status.haveCost, a.status.haveUsage = false, false
		return
	}
	a.status.cost, a.status.haveCost = result.turn.Cost(), true

	// The gauge needs both halves and takes neither on faith. The numerator is
	// zero when the window was SHARED — half a context is not a context, and the
	// journal says so rather than guessing — and it is an upper bound when a
	// summed spine row landed inside the window, which is the safe direction for
	// a health signal: a gauge that errs toward amber sends a person to look.
	a.status.haveUsage = false
	if a.commander == nil || result.turn.SpinePromptHighWater <= 0 {
		return
	}
	window, known := a.commander.ContextWindow("talk")
	if !known || window <= 0 {
		return
	}
	a.status.used = int64(result.turn.SpinePromptHighWater)
	a.status.window = int64(window)
	a.status.haveUsage = true
}

// followNode keeps an open task room current.
//
// A task room is a lens over the same journal (4.6), so its TRAIL moves when the
// journal does and never on a timer of its own — one more watermarked read on
// the cycle the conversation already paid for, and nothing at all on a quiet
// one.
//
// ITS TRACE IS THE EXCEPTION, and the exception is the whole reason the trace
// exists as a separate source. A worker appending to its recorder journals
// nothing, so `quiet` — the cheap proof that the THREAD has not moved — is no
// proof at all that the WORK has not. A room that re-read the recorder only on
// journal moves would freeze mid-run and look exactly like a worker that had
// stopped, which is the picture this whole lane was reported as. v1's poll makes
// the same exception for the same reason, in the same words
// (internal/tui/model.go: "the executor appends outside the journal").
//
// The stamp is what makes the exception affordable: a recorder that has not
// grown costs one open and one stat per node, and only a file that actually
// moved is read at all.
func (a *App) followNode(result pollResultMsg) tea.Cmd {
	if result.err != nil {
		return nil
	}
	// THE POLL IS WHERE WORK BECOMES LIVE, so the poll is where the animation
	// clock has to be armed.
	//
	// THIS WAS THE DEFECT, and the reader described its signature exactly:
	// "animation seems to happen only when I hover on something". A tick chain
	// sustains itself once it has started (app.go's animTickMsg handler re-arms
	// on every tick), and it was STARTED from three places — the window opening,
	// a post landing, a stream batch arriving — every one of which is an INPUT.
	// A job card becomes live on none of them: the reader sends a message (at
	// which instant nothing is live yet and [App.startTick] correctly declines),
	// the head commissions the work, and the card arrives on a POLL — a path
	// that armed nothing at all. From then on the only frames were the ones some
	// other event happened to repaint, which is a pointer moving.
	//
	// It is idempotent and costs nothing when nothing is live: [App.startTick]
	// returns nil unless something is actually moving, and refuses outright
	// while a chain is already armed.
	tick := a.startTick()
	if a.view == nil || a.view.kind != viewNode {
		// THE CONVERSATION'S OWN CARDS make the same exception the room's trace
		// does, for the same reason: a running part's one-line preview comes off
		// a recorder that journals nothing when it grows (trace.go's
		// readCardTracesCmd), so a quiet cycle is no proof that the work has not
		// moved. It costs nothing while no card is live.
		return tea.Batch(tick, a.readCardTracesCmd())
	}
	trace := a.readTraceCmd(a.view.node)
	if result.quiet {
		return tea.Batch(tick, trace)
	}
	return tea.Batch(tick, a.readNodeCmd(a.view.node, a.view.watermark), trace)
}

// behind reports that this window has read messages the journal numbered below
// its own watermark and has not caught up. It is the one condition under which
// an unmoved journal is NOT proof that the screen is current.
func (a *App) behind() bool { return a.journal < a.watermark }

// applyPoll folds one read into the transcript.
func (a *App) applyPoll(result pollResultMsg) {
	if result.err != nil {
		a.status.err = result.err.Error()
		a.shell.Invalidate()
		return
	}
	if a.status.err != "" {
		a.status.err = ""
		a.shell.Invalidate()
	}
	// The journal watermark is recorded only by a read that reached the end of
	// the room, because that number is a claim — "this window is current as of
	// here" — and the whole cheap path downstream believes it. A full page is a
	// read that stopped early, so it makes no claim and leaves the old one
	// standing; afterPoll comes straight back for the rest.
	//
	// Recording it unconditionally is what ate replies in every room with a
	// history: one page of messages can span thousands of journal rows, so a
	// window that had read the first five hundred messages declared itself
	// current as of the newest event and then answered every later poll with
	// "quiet" — while the reply the reader was waiting for sat in the store, a
	// page or ten below the read watermark, with the awaiting line spinning
	// above it forever (13.2's P0).
	if !result.more {
		a.journal = result.journal
	}
	a.applySpend(result)
	a.refreshScope(result.journal)
	if result.quiet {
		traceJournal(a, result, 0)
		return
	}
	appended := 0
	if len(result.messages) > 0 {
		appended = a.absorb(result.messages)
	}
	// After the messages, so a receipt in this same batch has already claimed
	// its job and no skeleton is minted under it. A journal move with no
	// session message at all is exactly the moment a commission lands — the
	// command row is the only artifact the store has yet.
	skeletons := a.reconcilePending(result.commands, result.haveCommands)
	if appended == 0 && !skeletons {
		traceJournal(a, result, 0)
		return
	}
	traceJournal(a, result, appended)
	a.refresh()
}

// reconcilePending keeps the conversation honest about work the head has
// handed over that the graph has not caught up with. Each pending splice of
// this session gets the commitment card's SKELETON ([messageBlock.dressCommitting],
// "creating task…", breathing) the moment its command row exists — which is
// seconds to minutes before compile and plan finish and the receipt arrives.
// The skeleton then leaves by one of its two honest doors: the receipt
// coalesces into it in place ([App.coalesceJob]), or the board names its task
// and [messageBlock.refreshCard] promotes it — and a command that settled
// without ever minting a task re-dresses as its own ending rather than
// breathing forever over work that will not come.
func (a *App) reconcilePending(pending []store.Command, havePending bool) bool {
	if a.transcript == nil {
		return false
	}
	live := make(map[int64]bool, len(pending))
	for _, command := range pending {
		live[command.Seq] = true
	}
	changed := false
	for i := 0; havePending && i < a.transcript.Len(); i++ {
		block, ok := a.transcript.Block(i).(*messageBlock)
		if !ok || !block.provisional || block.job == "" || block.source == nil {
			continue
		}
		seq := block.source.CommandSeq
		if seq == 0 || live[seq] {
			continue
		}
		// The command settled. A splice can have minted EITHER spelling —
		// task-<seq> or craft-<seq> — and the skeleton was born guessing the
		// first, so the settled command's real job is looked for under both
		// before anything is declared dead. A skeleton buried over a live
		// craft job said "didn't start" while the work ran (user-reported,
		// 2026-08-11).
		named := ""
		if a.source != nil {
			for _, candidate := range commandJobIDs(seq) {
				if _, known := a.source.jobFacts(candidate); known {
					named = candidate
					break
				}
			}
		}
		if named != "" {
			if block.job != named {
				// Applied under the other spelling: adopt it, so refreshCard
				// names this block and the receipt coalesces into it instead
				// of standing beside it.
				block.job = named
				a.transcript.Replace(i, block)
				changed = true
			}
			continue
		}
		block.dressUnstarted()
		a.transcript.Replace(i, block)
		changed = true
	}
	appended := false
	for _, command := range pending {
		exists := false
		for _, candidate := range commandJobIDs(command.Seq) {
			if _, _, held := a.jobCard(candidate); held {
				exists = true
				break
			}
		}
		if exists {
			continue
		}
		if !appended {
			a.detachLive()
			appended = true
		}
		message := store.Message{
			Seq:        command.Seq,
			SessionID:  command.SessionID,
			Role:       store.RoleSystem,
			CommandSeq: command.Seq,
			Body:       command.Instruction,
		}
		block := newMessageBlock(message, a.style, a.source)
		a.applyFold(block)
		a.foldable = a.foldable || block.collapsible
		block.animate(a.transcript.Clock(), a.now())
		a.transcript.Append(block)
		changed = true
	}
	if appended {
		a.attachLive()
	}
	return changed
}

// refreshScope rebuilds the scope map when the journal moved, and then lets the
// rail re-merge it.
//
// The order matters and the split is the production bar this wave is held to:
// the SOURCE is rebuilt at most once per journal move (one snapshot read, one
// usage read, one question read), and the RAIL's own Refresh — which keeps the
// visible order and the cursor's row by id (7.2) — runs only when the source
// actually produced something new. A quiet poll costs one integer comparison.
func (a *App) refreshScope(journal int64) {
	if a.source == nil || a.railModel == nil {
		return
	}
	// The rail ages its live rows' clocks between snapshots, and this is where
	// it is told which clock and from when: a snapshot is about to be loaded,
	// so the drift restarts here (rail.Model.SetClock / Refresh). It is
	// idempotent and costs a pointer write on the cycles the journal moved.
	if a.railModel != nil {
		a.railModel.SetClock(a.transcript.Clock())
	}
	if a.hudModel != nil {
		a.hudModel.SetClock(a.transcript.Clock())
	}
	if !a.source.refresh(journal, false) {
		return
	}
	// Delivery and failure are read off the rebuilt board, here, because this is
	// the one moment the lifecycles moved.
	a.settleJobBlocks()
	a.noticeWork()
	a.refreshHomes()
	// An open task room is a lens on the board, and the board is what just
	// moved. Its transcript therefore follows the SNAPSHOT and not only the
	// message trail: a part that claimed, started, finished or failed changed a
	// row that is on screen, and none of those is a message (record.go). A room
	// that repainted only on new messages would show a job's parts frozen as
	// they were the moment it was opened, which is the same picture a dead
	// surface draws.
	a.paintRoom()
	a.railModel.Refresh()
	if a.hudModel != nil {
		a.hudModel.Refresh()
	}
	a.status.breadcrumb = a.breadcrumb()
	a.shell.Invalidate()
}

// absorb appends the journal rows this window has not seen.
//
// The live turn is detached first and re-attached last. That ordering is the
// whole reconciliation: a preview may never sit above a row the store numbered
// after it, and a preview whose durable reply has landed may not sit anywhere
// at all.
// It reports how many rows it put on screen, which is the number the trace
// compares against the page the store handed over: a page absorbed short is the
// signature of a row this window read and did not draw.
func (a *App) absorb(messages []store.Message) int {
	a.detachLive()
	settled, appended := false, 0
	// The block the live turn's activity collapses under: the durable reply that
	// ended the turn, and nothing else (activity.go). Nil for every poll that did
	// not end one, which is almost all of them.
	var landedInto *messageBlock
	for i := range messages {
		message := messages[i]
		if message.Seq > a.watermark {
			a.watermark = message.Seq
		}
		if _, exists := a.transcript.IndexOf(messageID(message.Seq)); exists {
			continue
		}
		sanitizeMessage(&message)
		block := newMessageBlock(message, a.style, a.source)
		// A row is born on the side of the fold this room is on. Before this
		// the conversation's own blocks were born collapsed regardless, so a
		// reader who had pressed ctrl+r watched every new turn arrive shut.
		a.applyFold(block)
		a.foldable = a.foldable || block.collapsible
		block.animate(a.transcript.Clock(), a.now())
		if a.coalesceJob(block) {
			// The job's own block evolved in place; nothing was appended, and
			// nothing that follows in this loop cares — a status line is not a
			// turn, does not end one, and never carries a question.
			continue
		}
		a.transcript.Append(block)
		appended++
		if message.Role == store.RoleUser {
			a.status.turns++
		}
		// The needs-input event, raised where the ask ARRIVES rather than where
		// the count is painted: a question is an interruption once, when it is
		// asked, and a notification driven off the standing count would fire
		// again on every poll for as long as nobody answered.
		if block.questions > 0 && !block.user {
			a.noticeQuestion(message.Body)
		}
		if a.turn.active && endsTurn(message, a.turn.since) {
			settled, landedInto = true, block
		}
	}
	if settled {
		// Before endTurn, which throws the live turn away: the activity has to be
		// handed to the row that ends it while there is still a live turn holding
		// it.
		a.collapseActivity(landedInto)
		a.endTurn()
		return appended
	}
	a.attachLive()
	return appended
}

// coalesceJob folds one machinery row into the job block already standing for
// that job, and reports that it did.
//
// §3 IS ONE BLOCK PER JOB, EVOLVING IN PLACE — "the commitment block updates in
// place (title · receipt · current status line), it does not append a sibling
// per status change" — and without this it appended a sibling per status
// change. Measured on the reporter's own journal: `task-9196` posted fifteen
// node-anchored status rows ("preparing the repository", "reading the issue",
// "running the repository's own checks", …), and the thread drew fifteen
// blocks, each re-stating the job's name and receipt above one line of status.
// The screenshot that reported this shows two of them stacked; the room had
// thirteen more.
//
// THE RULE IS ADJACENCY AND NOT IDENTITY, which is §15's own grammar read back:
// a job block belongs where the conversation was when it started, so a row that
// arrives after the reader has said something else opens a NEW block at the new
// position rather than reaching back up the transcript. Consecutive is
// therefore exactly the right test, and it is also the cheap one — the last
// block, and nothing else, is ever examined.
//
// THE SUPERSEDED STATUS IS DROPPED FROM THE LIVE VIEW AND NOT FROM THE RECORD.
// The journal keeps every row it ever kept; the task room renders all of them
// in order (record.go's roomBlocks, deliberately untouched). What collapses is
// the CONVERSATION's rendering of them, which is §1's line about progress
// narration being the work record's business and never a chat message.
//
// A DELIVERY TAKES THE BLOCK'S PLACE RATHER THAN SITTING UNDER IT. §3's last
// clause is "delivered: the block becomes the delivery card in place", so the
// card replaces the machinery it is the ending of, at the same index, wearing
// its own identity.
func (a *App) coalesceJob(block *messageBlock) bool {
	if block == nil || block.job == "" || block.user || block.source == nil {
		return false
	}
	last, index, ok := a.jobCard(block.job)
	if !ok {
		return false
	}
	// A QUESTION IS NEVER SUPERSEDED AND NEVER SUPERSEDES. Amber means a human
	// is actually needed (§18.3), so an ask that scrolled past behind a status
	// line would be the one row this collapse is not allowed to lose — and an
	// ask arriving over a status line is a new thing to answer, not the same
	// thing said again.
	if block.questions > 0 || last.questions > 0 {
		return false
	}
	if block.machinery {
		// The same block, one status later: it keeps its identity so the
		// reader's fold, the copy chip's hover and the transcript's own anchor
		// all survive a status change (13.16's fold law — an id that moved
		// would throw the reader's answer away several times a minute).
		block.id, block.version = last.id, last.version+1
		// THE READING SURVIVES EVERY STATUS. A status message does not carry the
		// head's reading — only the commissioning does — so a card that took its
		// segments from the new row alone would show the prompt once and never
		// again, which is the reader's "a card saying the task name and the
		// actual prompt it is using" losing half of itself on the first update.
		block.adoptPrompt(last.prompt)
		a.transcript.Replace(index, block)
		return true
	}
	// ONLY THE TASK'S OWN ENDING TAKES THE CARD'S PLACE. A learning moment is
	// delivery-SHAPED by columns — a system row, anchored to a node, belonging
	// to no command — and it is not an ending: it is the machine saying what it
	// will do differently next time ([speaks]). Letting it replace the card
	// would delete the result the reader came back for.
	if !isDelivery(*block.source) || speaks(block.source.Body) {
		return false
	}
	// The delivery card is the same card, finished: it keeps the reading the
	// commitment was carrying, so the finished row still says what was asked as
	// well as what came back (§4's anatomy, and the reader's own list).
	block.adoptPrompt(last.prompt)
	a.transcript.Replace(index, block)
	return true
}

// jobCard finds the block already standing for one task, anywhere in the
// conversation.
//
// IT IS A LOOKUP BY TASK AND NOT BY ADJACENCY, and that is the whole difference
// between "the last two rows happened to be the same job" and §3's "ONE block
// per job, at the position of the ask, evolving IN PLACE". Two tasks running at
// once interleave their progress — the reporter's own journal alternates
// `task-9400` and `task-9380` rows for pages — so an adjacency rule would mint a
// fresh block every time the other task said something, which is the defect
// wearing a different hat.
//
// It walks BACKWARDS and stops at the first match, so the ordinary case (the
// task that just spoke is the task that spoke last) costs one comparison. The
// live region is detached while [App.absorb] runs, so every block it sees is a
// journaled one and never a preview.
func (a *App) jobCard(job string) (*messageBlock, int, bool) {
	if job == "" || a.transcript == nil {
		return nil, 0, false
	}
	for i := a.transcript.Len() - 1; i >= 0; i-- {
		block, ok := a.transcript.Block(i).(*messageBlock)
		if !ok || block.job != job || !block.machinery {
			continue
		}
		return block, i, true
	}
	return nil, 0, false
}

// endsTurn reports that this journaled message is the reply the live turn was
// previewing. A head reply is an agent line with no node behind it: a node's
// message is a job reporting, which is a different row entirely and must not
// retire a conversation the head is still having.
func endsTurn(message store.Message, since int64) bool {
	return message.Role == store.RoleAgent && message.NodeID == "" && message.Seq > since
}

// sanitizeMessage runs one journaled message through the v2 chokepoint — body
// and every text part — so nothing downstream sanitizes again and nothing
// downstream forgets to.
func sanitizeMessage(message *store.Message) {
	message.Body = sanitizeText(message.Body)
	for i := range message.Parts {
		if message.Parts[i].Kind == store.PartText {
			message.Parts[i].Text = sanitizeText(message.Parts[i].Text)
		}
	}
}

// -- the composer's turn -----------------------------------------------------

// postCmd puts the draft in the journal through the single door every other
// writer uses (internal/thread). The head is watching that door; posting IS
// triggering the turn, and a surface that also poked the head would be a second
// way to start work.
// Attachments ride the same door (attach.go): they are part of the message, not
// a second write, so there is exactly one place a user turn enters the journal.
// The content-addressed copy is made INSIDE the returned command — hashing a
// file is IO, and the render goroutine does none.
func (a *App) postCmd(text string, attachments ...composer.Attachment) tea.Cmd {
	text = strings.TrimSpace(text)
	if text == "" {
		// A send with files and no words still says something; the body for it
		// is this side's to write, because the composer does not know what a
		// picture is. Still empty means there was nothing at all.
		text = attachmentBody(attachments)
	}
	if text == "" || a.backend == nil {
		return nil
	}
	backend := a.backend
	message := store.Message{SessionID: a.session, Role: store.RoleUser, Body: text}
	keeper, _ := a.commander.(AttachmentKeeper)
	files := append([]composer.Attachment(nil), attachments...)
	return func() tea.Msg {
		message.Attachments = keepAttachments(keeper, files)
		posted, err := thread.Post(backend, message)
		if err != nil {
			// The OUTGOING message rides back on the failure, because the
			// store's answer to a refused write is a zero row and the one thing
			// the surface needs from a failed send is the words it was carrying
			// ([App.failSend] puts them back in the draft).
			return postResultMsg{message: message, err: err}
		}
		return postResultMsg{message: posted, err: err}
	}
}

// applyPost lands the accepted turn at once rather than at the next poll (5.20
// rule 4: no dead-air sends). The row is the durable one, carrying the sequence
// the store gave it, so the poll behind it recognizes the turn instead of
// doubling it — and the watermark deliberately does not move, so a message that
// arrived between the last read and this post is not skipped.
func (a *App) applyPost(result postResultMsg) {
	if result.err != nil {
		// The store is not dead — one write was refused — so this is the
		// composer's failed state and not the footer's whole coloured sentence
		// about an unreachable journal. The words go back into the draft on the
		// same call, because a failure that destroyed the sentence would be
		// worse than one that said nothing (§7, [App.failSend]).
		a.failSend(result.message.Body, result.err)
		a.refresh()
		return
	}
	a.status.err = ""
	a.detachLive()
	if _, exists := a.transcript.IndexOf(messageID(result.message.Seq)); !exists {
		message := result.message
		sanitizeMessage(&message)
		a.transcript.Append(newMessageBlock(message, a.style, a.source))
		a.status.turns++
		// One mark per COMMITTED user message: the row is in the store, with the
		// sequence the store gave it, so the terminal's prompt navigation is
		// anchored to a turn that really happened.
		a.noticePrompt()
	}
	a.beginTurn(result.message.Seq)
	a.transcript.GotoBottom()
	a.refresh()
}

// -- the live turn -----------------------------------------------------------

// beginTurn opens the live region: an awaiting line, and nothing else until
// there are words to put above it.
func (a *App) beginTurn(since int64) {
	if a.turn.active {
		a.turn.since = since
		// The live blocks may be detached right now — applyPost takes them down
		// before it lands the row it is about to open a turn for — and this
		// branch is the only one that would have left them there. A second send
		// while the first turn is still being answered used to blank the
		// awaiting line and the words already streamed until the next journal
		// row happened to arrive, which is a chat that stops saying it heard you.
		a.attachLive()
		traceTurn(a, "rearmed")
		return
	}
	// The progress channel opens here and closes in endTurn — the two states
	// 10.5.27 allows, around the live turn and nothing else.
	a.noticeBusy(true)
	a.turn = liveTurn{
		active: true,
		since:  since,
		await: &awaitingBlock{
			clock:         a.transcript.Clock(),
			style:         a.style,
			phase:         "thinking",
			interruptible: a.commander != nil,
			// The breathe, unless the window is linear — where nothing moves and
			// the line says the same thing standing still (10.1.5). The ONE
			// SPINNER is still the composer's prompt (§8); this is §11's second
			// motion, which is a different mark saying a different thing.
			motion: !a.linear,
		},
	}
	a.attachLive()
	traceTurn(a, "began")
}

// endTurn closes the live region. What replaces it is already in the
// transcript: the durable reply, with whatever mark the engine journaled about
// how it ended.
func (a *App) endTurn() {
	traceTurn(a, "ended")
	a.noticeBusy(false)
	a.detachLive()
	a.turn = liveTurn{}
	a.refresh()
}

// attachLive puts the live blocks back at the tail, reply first.
//
// It is idempotent: a live region that is already attached is left exactly
// where it is. Callers pair it with detachLive, and one that did not — because
// a branch returned early, because two seams both thought they owned the
// pairing — used to leave the reader's turn either doubled on screen or missing
// from it. Neither is worth a rule nobody can check, so the function checks.
func (a *App) attachLive() {
	if !a.turn.active || a.turn.await == nil {
		return
	}
	if _, attached := a.transcript.IndexOf(a.turn.await.ID()); attached {
		return
	}
	if a.turn.reply != nil {
		a.transcript.Append(a.turn.reply)
	}
	// THE ACTIVITY PINS BELOW THE TEXT AND ABOVE THE WAIT (activity.go). The
	// order is the reading order of the turn: what it has said, what it is doing,
	// and that it is not finished. It is skipped entirely while it holds no
	// steps, so an ordinary turn's live region is exactly the two blocks it has
	// always been.
	if a.turn.activity.live() {
		a.transcript.Append(a.turn.activity)
	}
	a.transcript.Append(a.turn.await)
}

// detachLive removes them. The live seam is the index of the first block that
// is not finalized, and every journaled block is finalized from birth — so this
// drops exactly the live tail and can never eat a settled row.
func (a *App) detachLive() {
	a.transcript.Truncate(a.transcript.LiveSeam())
}

// applyStream folds one boundary of the provider feed into the live region and
// reports whether anything on screen moved.
func (a *App) applyStream(event StreamEvent) bool {
	// The session filter (12.1's keyed events). An event that names a room this
	// window is not showing is dropped rather than drawn; an event that names
	// no room at all is this one's, which is what every existing emitter means
	// by an empty key.
	if event.Session != "" && event.Session != a.session {
		traceStream(a, event, true)
		return false
	}
	traceStream(a, event, false)

	switch event.Kind {
	case StreamStarted:
		if !a.turn.active {
			a.beginTurn(a.watermark)
		}
		a.turn.raw.Reset()
		a.turn.shown = ""
		a.turn.await.phase = "thinking"
		a.turn.await.interruptible = a.commander != nil
		return true

	case StreamThinking:
		if !a.turn.active {
			return false
		}
		a.turn.await.phase = "thinking"
		return true

	case StreamDelta:
		if !a.turn.active {
			return false
		}
		a.turn.raw.WriteString(event.Delta)
		reply, found := partialReply(a.turn.raw.String())
		if !found {
			// The head answers with a structured object, and its opening bytes
			// are not prose. Nothing is drawn until the reply key arrives —
			// which is the honest rendering of "it has not said anything yet".
			return false
		}
		a.writeReply(sanitizeText(reply))
		a.turn.await.phase = "replying"
		return true

	case StreamFinished:
		if !a.turn.active {
			return false
		}
		// The provider is done; the durable line is on its way. The awaiting
		// line stops offering an interrupt it can no longer perform.
		a.turn.await.interruptible = false
		if !a.turn.stopped {
			a.turn.await.phase = "settling"
		}
		return true

	case StreamFailed:
		if !a.turn.active {
			return false
		}
		a.turn.await.interruptible = false
		if !a.turn.stopped {
			a.turn.await.phase = "stream lost"
		}
		return true

	// THE TOOL ACTIVITY BOUNDARIES (activity.go). One arm rather than three
	// bodies, because what they share — the live region, the session filter
	// above, the attach — is all of it, and what differs is one line inside
	// [App.applyToolStream].
	case StreamToolBegin, StreamToolEnd, StreamToolFailed:
		return a.applyToolStream(event)
	}
	return false
}

// writeReply advances the streamed block.
//
// The ordinary case is an append: the decoded reply grows by its tail, so the
// block's settled head stays settled and the frame costs the live region rather
// than the reply. The other case is a stream that rewrote what it had already
// said — a malformed escape resolving differently, a provider retrying — and
// there the block is replaced outright, because a settled head that turned out
// to be wrong is not settled.
func (a *App) writeReply(text string) {
	if text == "" {
		return
	}
	if a.turn.reply == nil {
		a.detachLive()
		a.turn.reply = a.newReplyBlock()
		a.attachLive()
	}
	body := a.turn.reply.Body()
	switch {
	case text == body:
		return
	case strings.HasPrefix(text, body):
		a.turn.reply.Write(text[len(body):])
	default:
		fresh := a.newReplyBlock()
		fresh.Write(text)
		if index, ok := a.transcript.IndexOf(fresh.ID()); ok {
			a.transcript.Replace(index, fresh)
		}
		a.turn.reply = fresh
	}
	a.turn.shown = text
}

// newReplyBlock is the streamed reply: a headerless body that grows, at exactly
// the depth its journaled twin will have.
//
// IT IS ONE BLOCK AGAIN, because the answer is the unmarked voice (§3b). The
// preview used to be two — a header-only block saying "aforge" flush left, and
// the prose indented under it — so that the live rows and the journaled rows
// would line up at the moment a turn settled. Now that neither party of the
// conversation is named, there is nothing to line up but the words, and the
// pair collapses back into the single block it wanted to be: same id, same
// indent, and the same promise that the instant the preview becomes a journaled
// reply nothing on screen moves sideways (8.1.6, on the row a reader is watching
// most closely).
func (a *App) newReplyBlock() *blocks.TextBlock {
	block := blocks.NewText("live-reply", blocks.Header{})
	block.Styler = a.style
	block.BodyState = blocks.StateSettled
	block.Indent = bodyIndent
	return block
}

// -- the token feed ----------------------------------------------------------

// waitStream blocks on the feed and hands back everything a single wake found
// queued. Adjacent deltas from one room merge, so a fast reply costs one
// decode per wake rather than one per token.
func (a *App) waitStream() tea.Cmd {
	events := a.events
	if events == nil {
		return nil
	}
	return func() tea.Msg {
		event, open := <-events
		if !open {
			return streamClosedMsg{}
		}
		batch := []StreamEvent{event}
		for draining := true; draining; {
			select {
			case next, stillOpen := <-events:
				if !stillOpen {
					draining = false
					break
				}
				last := &batch[len(batch)-1]
				if next.Kind == StreamDelta && last.Kind == StreamDelta &&
					next.Session == last.Session {
					last.Delta += next.Delta
					continue
				}
				batch = append(batch, next)
			default:
				draining = false
			}
		}
		return streamBatchMsg{events: batch}
	}
}

// -- the animation clock -----------------------------------------------------

// startTick arms the shared animation clock, and only while something is
// actually moving. A settled window ticks for nothing, which is what lets it
// cost nothing.
//
// TWO THINGS MOVE. A live turn in this thread is the first and always was. The
// second is an OPEN TASK ROOM WATCHING WORK HAPPEN: a worker's recorder ends on
// a call whose result has not come back, so the record draws a running row
// (trace.go's [runningBlock]) and that row is the live region of its own
// transcript. Without this clause the spinner would advance only when the poll
// happened to find the journal moved — which for a worker mid-tool-call is
// exactly never, since a recorder append journals nothing at all. The room
// would freeze on one braille frame and read as a window that had stopped,
// which is the precise failure the running row exists to fix.
//
// CALM STOPS THE GLYPHS AND NOT THE CLOCK. The linear profile used to return
// here, which stopped the WAKE-UPS — so every elapsed cell in the window froze
// too, and a job that had been running for four minutes said `12s` for the rest
// of its life. §18's rule is the other one: the glyph freezes on its first
// frame ([blocks.Clock.Calm], which every driven motion in this tree already
// honours) and the number keeps counting, because a still glyph beside a moving
// number is still a row visibly alive. A calm window therefore wakes on the
// SECOND rather than on the animation step — that is the only cadence anything
// left moving needs, and it is a tenth of the wake-ups.
func (a *App) startTick() tea.Cmd {
	if a.ticking || !(a.turn.active || a.roomIsLive()) {
		return nil
	}
	a.ticking = true
	// The profile is pushed to the clock here rather than at construction
	// because this is the one function both halves of the chain go through, and
	// a clock whose calmness disagreed with the ticks driving it would draw a
	// frozen glyph on a window that was still paying to wake up.
	//
	// It only ever turns calmness ON. A window in the linear profile is calm by
	// definition; a window that is not says nothing either way, because the
	// clock's own Calm is also how a host, a test or a future reduced-motion
	// setting asks for the same thing, and a profile check that wrote `false`
	// would be this seam quietly overruling all three.
	if a.linear {
		a.transcript.Clock().Calm = true
		return tea.Tick(calmInterval, func(time.Time) tea.Msg { return animTickMsg{} })
	}
	// THE WAKE-UP IS ASKED OF THE CLOCK, not measured from now.
	//
	// A fixed `Tick(DefaultInterval)` sleeps one interval from whenever this
	// call happened to run, which is a phase the poll, a keystroke and a delta
	// all shift independently. The frame then lands mid-step: the glyph index is
	// the same one the last frame drew, every row is byte-identical, and the
	// repaint produces zero dirty rows — the exact wasted paint 8.1.3's
	// floor(now/interval) design exists to make impossible. Worse, the drift
	// accumulates, so the visible cadence of the breathe wanders while the
	// underlying clock does not.
	//
	// [blocks.Clock.NextTick] answers the only question worth asking — when does
	// the next glyph actually change — off the same latched instant every live
	// glyph in the frame derives from. Waking there means every repaint has
	// something to repaint. A clock that has never been latched, or a calm one,
	// answers the zero time; a live turn on an unlatched clock still has to wake
	// up, so that case falls back to one interval.
	next := a.transcript.Clock().NextTick()
	if next.IsZero() {
		return tea.Tick(blocks.DefaultInterval, func(time.Time) tea.Msg { return animTickMsg{} })
	}
	if wait := next.Sub(a.now()); wait > 0 {
		return tea.Tick(wait, func(time.Time) tea.Msg { return animTickMsg{} })
	}
	return tea.Tick(blocks.DefaultInterval, func(time.Time) tea.Msg { return animTickMsg{} })
}

// roomIsLive says the LENS ON SCREEN holds something that is still moving.
//
// It is the one question both halves of the animation chain ask — [startTick]
// asks it to decide whether to arm, and the tick's own handler asks it to decide
// whether a repaint has anything to repaint — so the two cannot disagree about
// what "live" means, which is why the second lens was added here rather than
// beside each of them.
//
// AN OPEN TASK ROOM is the first answer and the original one: it asks the
// TRANSCRIPT rather than the record, because "is anything live" is already the
// transcript's own question ([blocks.Transcript.LiveSeam]) and a second answer
// derived from the traces would be a second opinion that could disagree with the
// one deciding which blocks get rebuilt.
//
// THE WORK PAGE is the second. A board with a job in `working` draws a spinner
// and an ageing clock, and neither advances on a journal move — a worker inside
// a tool call journals nothing at all — so without this clause the page freezes
// on one frame and reads as a window that has stopped. That was the reported
// defect ("no animation of running etc."), and it is the same defect the room
// clause fixed one lens over.
//
// THE CONVERSATION ITSELF is the third, and it is the one the reader meets
// first. A job block in the thread carries a clock while its job runs (§3's
// receipt), and a clock nobody wakes for is a number that stops — which is what
// the reporter saw: a job block stating an elapsed measured whenever the
// journal last happened to move.
func (a *App) roomIsLive() bool {
	if a.boardIsLive() || a.threadIsLive() || a.railIsLive() {
		return true
	}
	if a.view == nil || a.view.kind != viewNode || a.view.transcript == nil {
		return false
	}
	return a.view.transcript.LiveSeam() < a.view.transcript.Len()
}

// threadIsLive says the conversation holds a job block whose clock is running.
//
// It walks the TAIL and not the whole transcript, because a job block evolves
// in place at the position of its ask and a thread with a thousand settled rows
// must not pay for them on every frame. The window is generous enough to hold
// every job a reader could have commissioned without speaking in between, and
// the answer is only ever used to decide whether to wake up.
func (a *App) threadIsLive() bool {
	if a.transcript == nil {
		return false
	}
	lo := a.transcript.Len() - liveTailWindow
	if lo < 0 {
		lo = 0
	}
	for i := a.transcript.Len() - 1; i >= lo; i-- {
		block, ok := a.transcript.Block(i).(*messageBlock)
		if !ok || !block.working {
			continue
		}
		// THREE THINGS ON A CARD MOVE, and asking only about the clock missed
		// two of them — which is half of why the window animated only when
		// something else happened to repaint it.
		//
		//   - the ELAPSED cell, whenever the board has measured one;
		//   - the BREATHE on the phase line, which is the whole of what a card
		//     shows while it is being created or planned and has no measured
		//     clock at all ([creatingWord], [planningWord], [settingUp]);
		//   - the SPINNER on a running part's own row (§18.2's live subtree
		//     twigs), which a card can carry before it has been billed a second.
		//
		// A skeleton card is the case that proves it: it is drawn the frame its
		// commissioning row is read, it has no lifecycle, no receipt and no
		// parts, and the one thing on it is a breathing dot. A liveness question
		// that asked for a measured clock answered "nothing is moving" over a
		// card whose only content was a motion.
		if block.hasElapsed || block.breathing || block.partsAlive() {
			return true
		}
	}
	return false
}

// settleJobBlocks stops the clock on job blocks whose jobs have finished.
//
// A block is dressed once, from the board as it stood when the row arrived, so
// a job that finishes WITHOUT posting a last word — a sub-job, a cancel, a
// failure the reconciler settled quietly — would leave its block counting
// forever. That is the battery law's own failure case ("no animation may run
// when nothing is live") and it is also a lie on screen: a stopped job with a
// number still climbing.
//
// The ordinary ending needs none of this: a delivery replaces the block outright
// (coalesceJob). This is the door for every ending that is not announced, and it
// runs on the one cycle where a lifecycle can have moved at all.
func (a *App) settleJobBlocks() {
	if a.transcript == nil || a.source == nil {
		return
	}
	lo := a.transcript.Len() - liveTailWindow
	if lo < 0 {
		lo = 0
	}
	for i := lo; i < a.transcript.Len(); i++ {
		block, ok := a.transcript.Block(i).(*messageBlock)
		// A settled card is walked too, and that is not a contradiction of this
		// function's name: its money and its burn are still arriving. A job's
		// last run is billed after the row that announced it, so a delivery card
		// that stopped reading at the moment it was dressed shows the bill it
		// had rather than the bill it has.
		if !ok || (!block.working && block.card == dressNone) {
			continue
		}
		facts, known := a.source.jobFacts(block.job)
		if !known {
			continue
		}
		switch facts.Life {
		case rail.LifeWorking, rail.LifeQueued:
			// Still going, and its own figure is worth taking: a job whose
			// elapsed the board has re-measured re-latches here, so the row
			// never drifts away from the card beside it (12.14).
			//
			// THE RE-LATCH IS MONOTONE, and that is what makes the card's clock
			// SMOOTH (user review, 2026-08-11: "the animations inside card seems
			// to be not smooth or weird"). The board measures an elapsed at the
			// instant its SNAPSHOT was taken; this runs at the instant the poll
			// folded that snapshot in, which is strictly later. Re-latching
			// unconditionally therefore restarted the count from a figure
			// measured in the past — so every poll the number jumped BACKWARDS
			// by the read's own latency and then climbed again, four times a
			// second, which is exactly the jitter the reader saw. A measurement
			// that is behind what the frame is already showing is a stale
			// measurement, not a correction; it is dropped, and the clock beside
			// it keeps counting from the last reading that was ahead.
			shown, showing := block.liveElapsed()
			if !showing || block.at.IsZero() || facts.Elapsed >= shown {
				block.elapsed, block.hasElapsed = facts.Elapsed, facts.HasElapsed
				block.at = a.now()
			}
		default:
			block.working = false
			// The last reading it will ever show is the board's own final one,
			// not whatever the clock happened to have counted to.
			block.elapsed, block.hasElapsed = facts.Elapsed, facts.HasElapsed
			block.version++
			block.measured = false
		}
		// THE MONEY AND THE SHAPE MOVE WITH THE SNAPSHOT, NOT WITH THE MESSAGE.
		// A card is dressed once, from a journal row, and everything on it that
		// is a fact about the GRAPH — what the job has spent, how much of the
		// window it has burned, how many parts exist, whether any of them has
		// started — changes with no row being written at all. The reader put it
		// plainly: "the cost updates and runtime etc. should be running and
		// realtime in the main chat". The elapsed already ticked because the
		// clock ages it; the rest sat frozen at whatever it was when the last
		// status happened to land.
		block.refreshCard(facts, a.source)
		a.transcript.Invalidate(block.ID())
	}
}

// railIsLive says the sidebar is showing work that is still running.
//
// A rail card's clock is the one thing on that surface that moves (§18.2 bans
// the spinner from a durable card outright, and a rail card is the example the
// law names), and a clock nobody wakes for is a clock that stopped. The
// question is asked of the same rows the rail draws, through the same
// [rail.Attention.Live] the renderer uses to decide whether to age them, so the
// wake-up and the motion can never disagree about which rows are moving.
func (a *App) railIsLive() bool {
	if a.railModel == nil {
		return false
	}
	for _, row := range a.railModel.Rows() {
		if row.Attention().Live() && row.Meta.HasElapsed {
			return true
		}
	}
	return false
}

const (
	// liveTailWindow is how far back a frame looks for something still moving.
	liveTailWindow = 32
	// calmInterval is the wake-up cadence of a window whose motion is frozen:
	// one second, which is the resolution of the only cell still changing.
	calmInterval = time.Second
)

// refreshCardPreviews tells every live job card in the conversation what its
// running parts' recorders last said.
//
// It is the card half of the same read the record page uses (trace.go's
// [App.readCardTracesCmd] and recordtree.go's [recordPreview]); the LOOKUP is
// shared outright, because a recorder is named by the SPLICE and not by the node
// — a planner splices every part of a job in one transaction, so every part
// shares one created sequence and therefore one file — and a card that asked for
// its own part's id would come back empty for every part but one.
//
// Only a card whose preview actually MOVED is invalidated, so a recorder that
// grew somewhere else costs one string comparison and no repaint.
func (a *App) refreshCardPreviews() {
	if a.transcript == nil || a.source == nil || len(a.traces) == 0 {
		return
	}
	lo := a.transcript.Len() - liveTailWindow
	if lo < 0 {
		lo = 0
	}
	moved := false
	for i := lo; i < a.transcript.Len(); i++ {
		block, ok := a.transcript.Block(i).(*messageBlock)
		if !ok || block.card == dressNone || block.job == "" || len(block.parts) == 0 {
			continue
		}
		preview := recordPreview(a.source.workRecord(block.job), a.traces)
		if preview == nil {
			continue
		}
		changed := false
		for j := range block.parts {
			if block.parts[j].Life != rail.LifeWorking {
				continue
			}
			node, known := a.source.nodes[block.parts[j].Node]
			if !known {
				continue
			}
			line := preview(workRow{node: node})
			if line == block.parts[j].Preview {
				continue
			}
			block.parts[j].Preview = line
			changed = true
		}
		if !changed {
			continue
		}
		block.version++
		block.measured = false
		a.transcript.Invalidate(block.ID())
		moved = true
	}
	if moved {
		a.shell.Invalidate()
	}
}
