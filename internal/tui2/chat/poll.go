package chat

import (
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/Agent-Field/aforge-v2/internal/store"
	"github.com/Agent-Field/aforge-v2/internal/thread"
	"github.com/Agent-Field/aforge-v2/internal/tui2/blocks"
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
		return pollResultMsg{journal: seq, messages: messages,
			more: len(messages) >= messagePage}
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
	if result.quiet || len(result.messages) == 0 {
		traceJournal(a, result, 0)
		return
	}
	appended := a.absorb(result.messages)
	traceJournal(a, result, appended)
	a.refresh()
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
	for i := range messages {
		message := messages[i]
		if message.Seq > a.watermark {
			a.watermark = message.Seq
		}
		if _, exists := a.transcript.IndexOf(messageID(message.Seq)); exists {
			continue
		}
		sanitizeMessage(&message)
		a.transcript.Append(newMessageBlock(message, a.style))
		appended++
		if message.Role == store.RoleUser {
			a.status.turns++
		}
		if a.turn.active && endsTurn(message, a.turn.since) {
			settled = true
		}
	}
	if settled {
		a.endTurn()
		return appended
	}
	a.attachLive()
	return appended
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
func (a *App) postCmd(text string) tea.Cmd {
	text = strings.TrimSpace(text)
	if text == "" || a.backend == nil {
		return nil
	}
	backend := a.backend
	message := store.Message{SessionID: a.session, Role: store.RoleUser, Body: text}
	return func() tea.Msg {
		posted, err := thread.Post(backend, message)
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
		a.status.err = result.err.Error()
		a.shell.Invalidate()
		return
	}
	a.status.err = ""
	a.detachLive()
	if _, exists := a.transcript.IndexOf(messageID(result.message.Seq)); !exists {
		message := result.message
		sanitizeMessage(&message)
		a.transcript.Append(newMessageBlock(message, a.style))
		a.status.turns++
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
	a.turn = liveTurn{
		active: true,
		since:  since,
		await: &awaitingBlock{
			clock:         a.transcript.Clock(),
			style:         a.style,
			phase:         "thinking",
			interruptible: a.commander != nil,
			motion:        !a.linear,
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

// newReplyBlock is the streamed reply's block: one header naming who is
// speaking, and a body that grows. Its header is static on purpose — the moving
// glyph belongs to the awaiting line below it, so the reply's first row is
// byte-stable and everything above the tail stays out of the rebuild.
func (a *App) newReplyBlock() *blocks.TextBlock {
	block := blocks.NewText("live-reply", blocks.Header{
		Title: "aforge",
		State: blocks.StateChrome,
	})
	block.Styler = a.style
	block.BodyState = blocks.StateSettled
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

// startTick arms the shared animation clock, and only while a turn is live. A
// settled window ticks for nothing, which is what lets it cost nothing.
func (a *App) startTick() tea.Cmd {
	if a.ticking || !a.turn.active || a.linear {
		return nil
	}
	a.ticking = true
	return tea.Tick(blocks.DefaultInterval, func(time.Time) tea.Msg { return animTickMsg{} })
}
