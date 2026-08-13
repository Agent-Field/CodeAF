package chat

import (
	"sort"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/Agent-Field/aforge-v2/internal/store"
	"github.com/Agent-Field/aforge-v2/internal/tui2/blocks"
	"github.com/Agent-Field/aforge-v2/internal/tui2/palette"
	"github.com/Agent-Field/aforge-v2/internal/tui2/reltime"
	"github.com/Agent-Field/aforge-v2/internal/tui2/tokens"
)

// Chats: the product layer over sessions, on this side of the wire
// (audit-notes/chat-simplify.md Part 5).
//
// The engine already had the primitive — a session, per-room head cursors,
// delivery routed to the owning room, a scribe that names one — and the product
// never admitted it. This file is the admission's SURFACE half: the title chip
// that says which conversation you are in, the switcher that moves you to
// another, the ruled line that marks the join when it happens, and the one
// journal-only contract by which the head can move the window itself.
//
// The four laws it implements, stated once so the code below can be read
// against them:
//
//   - THREADS TALK TO THE COLLEAGUE. A thread is not a task room and not a
//     home: it is the conversation, and switching to one re-points the SESSION
//     — transcript, watermark, journal claim, composer — rather than pointing a
//     lens at a different slice of the one you are in. That is why
//     [palette.SwitchThread] is not [palette.JumpToRoom].
//   - CREATION AND SPLITTING ARE CONVERSATIONAL. `new thread` mints and walks
//     in; it never asks for a name, because naming is the scribe's job. The
//     SPLIT is not a gesture at all — the head settles it by journaling a row,
//     and this side reads the row (see [App.applyRoomSwitch]).
//   - ONE ORNAMENT. The unseen-delivery `●` in the switcher, and nothing else.
//     No counts, no badges, no second colour, and no ornament at all on the
//     title chip: a chip that grew a dot would be the bar row learning to
//     interrupt.
//   - SILENCE OVER MACHINERY. A thread the scribe has not named yet shows
//     NOTHING on the chip rather than an id (13.3.4), and the switcher's
//     `left at:` line is absent rather than invented for a thread with nothing
//     in it.

// -- the seam onto the engine --------------------------------------------------

// Thread is one working conversation, as this surface needs it.
//
// It is the surface's own shape rather than the engine's, for the reason
// [palette.SettingRow] is not config's: this package is a leaf of the TUI tree,
// and the fields below are the four facts a switcher row draws — everything
// else a session carries is somebody else's question.
type Thread struct {
	// SessionID is the room this thread IS. It is never drawn.
	SessionID string
	// Name is the scribe's name for it, empty until the scribe has run.
	Name string
	// Tags are the subjects the scribe filed it under, and they exist for the
	// switcher's FILTER alone. Nothing draws them: a row that grew a strip of
	// chips would be this surface learning to decorate, and 5.1's one-ornament
	// law spends the row's only mark on the unseen dot. What they buy is the
	// reader who remembers what a conversation was ABOUT and not what it ended
	// up being called.
	Tags []string
	// LeftAt is the line the conversation was left on — the last thing that was
	// not the person's own typing, flattened to one line. Empty is honest.
	LeftAt string
	// LastActive is when it last moved, for the row's relative time.
	LastActive time.Time
	// Unseen says a delivery landed here that this window has not shown anyone.
	Unseen bool
}

// ThreadReader is the engine's own thread INDEX.
//
// THE NAME IS THE CONTRACT, and it is spelled `index` rather than `open` on
// purpose. This seam was declared as an open-threads read and wired to
// `store.OpenThreads`, which returns only conversations with an unresolved arc
// — so the switcher listed nothing at all in a store full of finished work. An
// index lists EVERY conversation; whether one has an open loop is a decoration
// on its row (see [Thread.Unseen]) and may never be the filter that decides the
// row exists.
//
// It is an optional interface on the backend rather than a method on [Backend]
// for the reason [Rooms], [Graph] and [Ledger] are: a window driven by a stub in
// a test, or by a backend that is only a message log, must still open and still
// switch threads. *store.Store satisfies the [store.ThreadArc] form of it
// directly ([App.readThreads]'s second arm); this one is for a backend that
// would rather speak the surface's own shape.
type ThreadReader interface {
	// ThreadIndex is every thread worth listing, newest activity first, capped
	// at limit. Settled threads are listed like any other.
	ThreadIndex(limit int) ([]Thread, error)
}

// threadTails is the half of the fallback read the message log cannot answer:
// where each room's last non-user row sits. *store.Store answers it with one
// indexed aggregate per room, which is what makes the fallback affordable at
// the moment a door opens.
type threadTails interface {
	SessionLastNonUserMessageSeq(sessionID string) (int64, error)
}

// roomSwitchKind is the typed message part by which the head moves this window
// (chat-simplify.md 5.4). The deterministic answer gate settles a split by
// posting a system row in the OLD thread carrying this part, whose payload is
// the new session id; the surface applies it on its poll and the head simply
// serves the new room. No in-process channel, no second seam.
//
// The engine half landed (store.PartRoomSwitch, store.RoomSwitchTarget); the
// constant is its alias and the reader below goes through the store's own
// decoder, so the payload spelling lives in exactly one package.
const roomSwitchKind = store.PartRoomSwitch

// roomSwitchTarget is the one place a journaled row becomes a room switch.
//
// It is a function variable so a test can drive the contract without a real
// row. Everything downstream — the closing row, the watermark discipline, the
// atomic re-point — is written against the session id it returns and does not
// care where the id came from.
var roomSwitchTarget = func(message store.Message) string {
	for i := range message.Parts {
		if target, ok := store.RoomSwitchTarget(message.Parts[i]); ok {
			return target
		}
	}
	return ""
}

// -- reading the threads -------------------------------------------------------

// maxThreadRows bounds the switcher. It is a switcher and not an archive: the
// palette already searches every room the store holds, and a list longer than
// this is one a person scrolls rather than scans. Threads sink when quiet
// (5.1 law 5), so the cap only ever drops the ones nobody has touched.
const maxThreadRows = 12

// leftAtRead is how many rows the fallback reads to recover one thread's last
// line. It is 1 — the read is aimed at a sequence the store just handed over —
// and it is named so the cost of the whole fallback is legible: two indexed
// queries per listed thread, paid once when the door opens and never per frame.
const leftAtRead = 1

// readThreads is the switcher's whole read, taken at the moment its door opens.
//
// It is deliberately NOT on the poll path. A thread list is the answer to a
// question a person asks a few times an hour; paying for it on every cadence
// tick would put a per-room query behind a timer for a surface nobody is
// looking at, which is the cost internal/tui2/chat has spent five waves
// removing from everything else.
func (a *App) readThreads() []Thread {
	if reader, ok := a.backend.(ThreadReader); ok {
		if threads, err := reader.ThreadIndex(maxThreadRows); err == nil {
			return a.markSeen(threads)
		}
		// A read that failed is not a store with no threads in it. Fall through
		// to the reads that exist rather than claiming an empty list.
	}
	// The engine's own projection speaks in its vocabulary, not this package's;
	// the adaptation is four field names, done here so the store never has to
	// know what a switcher row is.
	//
	// IT IS ThreadIndex AND NOT OpenThreads, and the distinction is the whole of
	// a bug that made this feature look absent. OpenThreads answers "what is
	// still alive" — it DROPS every conversation whose last exchange was
	// finished properly — and a switcher driven from it showed a store with
	// three real sessions in it as a list of none: one preselected `new thread`
	// row reading `1/1`, whose enter abandoned the thread the reader was
	// standing in. A switcher is an INDEX. Whether a thread has an open loop is
	// a decoration on its row, never the filter that decides it exists.
	if reader, ok := a.backend.(interface {
		ThreadIndex(limit int) ([]store.ThreadArc, error)
	}); ok {
		if arcs, err := reader.ThreadIndex(maxThreadRows); err == nil {
			threads := make([]Thread, 0, len(arcs))
			for _, arc := range arcs {
				threads = append(threads, Thread{
					SessionID:  arc.SessionID,
					Name:       arc.Title,
					Tags:       arc.Tags,
					LeftAt:     arc.Left,
					LastActive: arc.LastActive,
					Unseen:     arc.UnseenDelivery,
				})
			}
			return a.markSeen(threads)
		}
	}
	if a.source == nil || a.source.rooms == nil {
		return nil
	}
	sessions, err := a.source.rooms.Sessions()
	if err != nil {
		return nil
	}
	// Sessions come back newest-active first, which is the order the switcher
	// wants (5.2's "newest-activity first, quiet threads sink"). The sort is
	// still made explicit, because that ordering is this surface's promise and
	// not the store's to change underneath it.
	sort.SliceStable(sessions, func(i, j int) bool {
		return sessions[i].LastActive.After(sessions[j].LastActive)
	})
	threads := make([]Thread, 0, maxThreadRows)
	for i := range sessions {
		session := sessions[i]
		if len(threads) >= maxThreadRows && session.ID != a.session {
			// The thread you are IN is always listed, even when it has aged out
			// of the top of the list: a switcher that cannot show you where you
			// are is a switcher that can strand you (scope.go's roomRows keeps
			// the same rule for the rail).
			continue
		}
		threads = append(threads, Thread{
			SessionID:  session.ID,
			Name:       strings.TrimSpace(session.Title),
			Tags:       session.Tags,
			LeftAt:     a.leftAt(session.ID),
			LastActive: session.LastActive,
		})
	}
	return a.markSeen(threads)
}

// leftAt is the line one thread was left on: its newest row that the person did
// not type themselves.
//
// It reads the AGENT's side deliberately. "left at" is what the conversation
// was saying when you walked away, and a thread whose last row is your own
// question has not said anything yet — quoting the question back would make the
// switcher a list of things the reader already knows they asked.
func (a *App) leftAt(session string) string {
	tails, ok := a.backend.(threadTails)
	if !ok || session == "" {
		return ""
	}
	seq, err := tails.SessionLastNonUserMessageSeq(session)
	if err != nil || seq <= 0 {
		return ""
	}
	messages, err := a.backend.Messages(session, seq-1, leftAtRead)
	if err != nil || len(messages) == 0 {
		return ""
	}
	return blocks.Flatten(firstLine(messages[0].Body))
}

// markSeen fills the unseen ornament and records what this window has read.
//
// THE WINDOW MAY ONLY CLAIM WHAT IT HAS SEEN. A thread this window has never
// visited carries NO dot, and that is conservative on purpose: the dot means
// "something landed here since you were last in it", which is a statement about
// the reader — and a fresh window that dotted every thread in the store would
// be announcing that the product is new rather than that anything happened. The
// engine's own [ThreadReader] answers this properly for every thread (it has the
// per-room head cursors); this is the honest local approximation until it does.
func (a *App) markSeen(threads []Thread) []Thread {
	for i := range threads {
		id := threads[i].SessionID
		if id == a.session {
			// You are standing in it. Whatever landed here, you are looking at
			// it — so the visit is recorded and the row never dots.
			a.noteThreadSeen(id, threads[i].LastActive)
			threads[i].Unseen = false
			continue
		}
		seen, visited := a.threadSeen[id]
		threads[i].Unseen = visited && threads[i].LastActive.After(seen)
	}
	return threads
}

// noteThreadSeen records that this window has this thread's state on screen.
func (a *App) noteThreadSeen(id string, at time.Time) {
	if id == "" {
		return
	}
	if a.threadSeen == nil {
		a.threadSeen = make(map[string]time.Time, 4)
	}
	if was, held := a.threadSeen[id]; !held || at.After(was) {
		a.threadSeen[id] = at
	}
}

// threadName is the scribe's name for one thread, or "" when it has none.
//
// It NEVER falls back to an id and never to a placeholder word. The title chip
// draws nothing at all for an unnamed thread (5.3), which is this function's
// empty string reaching the bar row unchanged.
func (a *App) threadName(session string) string {
	if a.source == nil || session == "" {
		return ""
	}
	return strings.TrimSpace(a.source.titles[session])
}

// -- the switcher --------------------------------------------------------------

// threadsKey is the bare key that opens the switcher (5.2's J3: "one key `t`").
// threadsChord and threadsCtrl are the same door for a room where every
// printable key belongs to the draft — the pair the registry records.
//
// WHY THERE ARE TWO CHORDS, and why the ctrl one is the one the catalog leads
// with. The alt spelling is not wrong: Bubble Tea v2 reports an ESC-prefixed
// `alt+t` as exactly the string below, in BOTH input protocols — the legacy
// decoder clears [Key.Text] and sets ModAlt when it unwraps an ESC prefix, and
// the Kitty decoder clears Text whenever a modifier above ModShift is present,
// so [Key.String] falls through to Keystroke() and spells it "alt+t" either
// way. The binding matched what the library produces.
//
// IT NEVER PRODUCED IT. On macOS the Option key is a COMPOSE key by default:
// Terminal.app and iTerm2 both send Option+t as the precomposed glyph `†`, one
// printable rune with no modifier bit on it. Bubble Tea sees Text="†", and
// [Key.String] returns the text rather than a keystroke — so the surface is
// handed "†" and the case below is never entered. No amount of correcting the
// binding reaches a key the terminal is eating before the program starts.
//
// That is the whole reason a chord-only door was the wrong door, and v1 already
// learned it once: internal/tui binds ctrl+t beside alt+g for its task list and
// says so in a comment ("Option only reaches the program as alt+g"). So the
// control spelling is added here rather than swapped in — a reader whose
// terminal DOES deliver Option keeps the chord in their fingers — and the
// catalog is pointed at the control one, because the `?` sheet may only teach a
// key that actually fires on the machine the reader is sitting at.
//
// ctrl+t is free on this surface. It is v1's rail toggle, not v2's: v2 reaches
// the rail through ctrl+o ([App.toggleRail]) and has never bound ctrl+t.
const (
	threadsKey   = "t"
	threadsChord = "alt+t"
	threadsCtrl  = "ctrl+t"
)

// threadsEntryID is the registry row for the switcher, named once so the door,
// the key the `?` sheet teaches and the palette row cannot drift apart.
const threadsEntryID = "key.threads"

// openSwitcher raises the thread switcher, reading the threads on the way in.
//
// IT OPENS POINTING SOMEWHERE WHEN THE READER HAS ALREADY SAID WHERE. Standing
// on a task's record page, the thread the job was commissioned from is the
// thread this key is most likely being pressed about — it is the one the page's
// own `for:` row names — so the cursor lands on it and enter is the whole
// gesture. Everywhere else the cursor opens at the top, which is the newest
// thread and the one a switcher is usually opened to leave for.
func (a *App) openSwitcher() tea.Cmd {
	a.buildSwitcher()
	a.switcher.Reset()
	threads := a.readThreads()
	a.switcher.SetThreads(switcherRows(threads, a.session, a.now()))
	if focus := a.provenanceThread(); focus != "" {
		a.switcher.Select(focus)
	} else {
		a.switcher.Select(a.session)
	}
	return a.raise(overlayThreads, a.switcher)
}

func (a *App) buildSwitcher() {
	if a.switcher != nil {
		return
	}
	a.switcher = palette.NewSwitcher(palette.Options{
		Styler:     a.style,
		Linear:     a.linear,
		Invalidate: a.shell.Invalidate,
		OnChoose:   a.choose,
		OnClose:    a.closeOverlay,
	})
}

// switcherRows is the projection onto the component's own row shape: everything
// formatted once, here, so the render is a pure function of its state.
func switcherRows(threads []Thread, current string, now time.Time) []palette.Thread {
	rows := make([]palette.Thread, 0, len(threads))
	for i := range threads {
		rows = append(rows, palette.Thread{
			ID:      threads[i].SessionID,
			Name:    threads[i].Name,
			Tags:    threads[i].Tags,
			LeftAt:  threads[i].LeftAt,
			When:    reltime.Short(threads[i].LastActive, now),
			Unseen:  threads[i].Unseen,
			Current: threads[i].SessionID == current,
		})
	}
	return rows
}

// node is one node of the board as this source last read it. It is the same map
// every other reader of the snapshot walks; this names the lookup so a caller
// asking one question does not have to know the map exists.
func (s *scopeSource) node(id string) (store.Node, bool) {
	if s == nil || s.nodes == nil || id == "" {
		return store.Node{}, false
	}
	node, found := s.nodes[id]
	return node, found
}

// provenanceThread is the session an open record page's work was commissioned
// from, or "" when there is no page or the page's node names none.
func (a *App) provenanceThread() string {
	if a.view == nil || a.view.kind != viewNode || a.source == nil {
		return ""
	}
	node, known := a.source.node(a.view.node)
	if !known {
		return ""
	}
	session := strings.TrimSpace(node.Provenance.SessionID)
	if session == a.session {
		// Naming the thread you are already in would make the `for:` row a door
		// back to the room it is drawn in. See [App.forThreadRow].
		return ""
	}
	return session
}

// -- switching -----------------------------------------------------------------

// switchThread moves the window into another working conversation.
//
// It is [App.switchRoom] plus the three things a THREAD switch owes that a rail
// room switch did not: the join is marked in the transcript (see
// [App.markThreadBreak]), the map is put away so the reader lands in the
// conversation rather than on the list they navigated from, and the poll is
// kicked so the new thread's journal is on screen in the next frame rather than
// on the next cadence tick.
//
// EVERY RE-POINT IS ATOMIC AND THEY ARE ALL IN switchRoom. The transcript, the
// read watermark, the journal claim, the live turn, the status line, the rail's
// own source and the composer's binding all move together, because carrying any
// one of them across would put one thread's tail in another thread's window.
// The GHOST this closes is the one the poll chain would otherwise leave: a read
// issued for the old session can land after the switch, and applyPoll now
// refuses it by name (poll.go's session guard).
func (a *App) switchThread(session string) tea.Cmd {
	session = strings.TrimSpace(session)
	if session == "" || session == a.session {
		return nil
	}
	// What this window had read of the thread it is LEAVING, recorded before the
	// leaving: after the switch a.session names somewhere else and the fact is
	// no longer recoverable.
	a.noteThreadSeen(a.session, a.now())
	a.switchRoom(session)
	// The rail's threads section is drawn from this index (scope.go's
	// roomRows), and a switch changes two rows of it at once: the one you left
	// stops saying `you are here` and starts saying what it was left at, and the
	// one you arrived in does the reverse. It is one of the three moments the
	// index is re-read — this is a door opening, which is exactly what
	// [App.readThreads] is priced for.
	if a.source != nil {
		a.source.setThreads(a.readThreads())
	}
	a.markThreadBreak(session)
	scope := a.setScope(false)
	a.refresh()
	return tea.Batch(scope, a.startPoll())
}

// markThreadBreak opens the arriving thread with the join (5.3's `thread break`
// row): one ruled line carrying the thread's name, and nothing else.
//
// It is [blocks.Ruled] and therefore the product's ONE disclosure grammar for a
// boundary — no box, no colour block, no band. The breathing room is the block's
// own: a blank row above the rule and one below it, which is §16's padding
// rhythm and the same two blanks a section word takes everywhere else on this
// surface.
//
// A thread with no name draws NO break at all. The rule exists to say which
// conversation you have arrived in, and a rule with nothing on it says only that
// something happened — which the empty transcript underneath it already said.
func (a *App) markThreadBreak(session string) {
	name := a.threadName(session)
	if name == "" || a.transcript == nil {
		return
	}
	a.transcript.Append(&threadBreakBlock{
		id:    threadBreakID,
		title: name,
		style: a.style,
	})
	a.shell.Invalidate()
}

// threadBreakID is the block id of the join. It is a constant rather than a
// per-switch id because a transcript holds exactly one: switchRoom resets the
// list before this is appended, so the join is always the first block of the
// thread it opens and there is never a second one to collide with.
const threadBreakID = "thread-break"

// threadBreakBlock is the join, as a transcript block.
//
// It is its own type rather than a [blocks.TextBlock] with a rule in it for the
// reason [noteBlock] is its own type: a text block paints its body at one state,
// and this row is a boundary rather than a body — the rule and the word on it
// are one composed line that [blocks.Ruled] already knows how to shed under
// width pressure.
type threadBreakBlock struct {
	id    string
	title string
	style *tokens.Styler

	width    int
	measured bool
	rows     []string
}

var _ blocks.Block = (*threadBreakBlock)(nil)

// ID is the anchor and cache key.
func (b *threadBreakBlock) ID() string { return b.id }

// IsFinalized is always true: a boundary has nothing left to do.
func (b *threadBreakBlock) IsFinalized() bool { return true }

// SettledRows is every row.
func (b *threadBreakBlock) SettledRows(width int) int { return len(b.Rows(width)) }

// Version never moves. The join is written once, when the thread is entered.
func (b *threadBreakBlock) Version() uint64 { return 0 }

// End is completed — a boundary is not a turn that could have been cut.
func (b *threadBreakBlock) End() blocks.EndState { return blocks.EndCompleted }

// Rows is the blank, the rule, and the blank.
//
// The two blanks are the whole of the spacing decision and they are stated
// here rather than left to the caller: a block that had to be appended between
// two spacer rows would be a rhythm somebody has to remember, and the next
// caller would forget one of them.
func (b *threadBreakBlock) Rows(width int) []string {
	if width < 1 {
		width = 1
	}
	if b.measured && b.width == width {
		return b.rows
	}
	rule := blocks.Ruled{Title: b.title, State: blocks.StateChrome}.Render(width, b.styler())
	b.rows = append(b.rows[:0], "", rule, "")
	b.width, b.measured = width, true
	return b.rows
}

func (b *threadBreakBlock) styler() blocks.Styler {
	if b.style == nil {
		return nil
	}
	return b.style
}

// -- the room-switch contract (5.4) --------------------------------------------

// roomSwitchIntent is one settled split, waiting to be performed.
//
// It is recorded during the absorb and performed after it, because the act
// resets the very transcript the absorb is appending into. spoken says the
// journaled row carried the head's OWN words, in which case the surface adds
// none of its own — see [App.drainRoomSwitch].
type roomSwitchIntent struct {
	target string
	spoken bool
}

// roomSwitchNote is what the thread being LEFT says on its way out, when the
// head did not say it itself.
const roomSwitchNote = "continuing in "

// noteRoomSwitchSeen records that this window has acted on one settlement row,
// and reports whether it is the first time.
//
// THE TRANSCRIPT'S OWN DEDUPE IS NOT ENOUGH, and the gap is the reason this
// exists. A switch RESETS the transcript, so walking back into the thread the
// split was journaled in re-reads that row into an empty block list, where
// [blocks.Transcript.IndexOf] has never heard of it — and the window would be
// yanked straight back out of the room the reader had just deliberately
// returned to, forever. The journal sequence is the row's identity and it
// outlives the transcript, so the claim is kept against that.
//
// The set is bounded by the thing it counts: a settled split is one row and a
// window sees a handful in a session. It is deliberately NOT persisted — a
// window that opens fresh on a thread whose conversation moved elsewhere SHOULD
// follow it, once, which is J7's "recall reaches them forever" read forwards.
func (a *App) noteRoomSwitchSeen(seq int64) bool {
	if seq <= 0 {
		return true
	}
	if a.roomSwitched == nil {
		a.roomSwitched = make(map[int64]bool, 2)
	}
	if a.roomSwitched[seq] {
		return false
	}
	a.roomSwitched[seq] = true
	return true
}

// drainRoomSwitch performs a settled split and clears it.
//
// It runs from the poll's own call site rather than from inside applyPoll, and
// the position is load-bearing: [App.afterPoll] takes the read chain down before
// this runs, so the [App.startPoll] inside the switch arms the chain for the
// NEW thread instead of colliding with the old one's in-flight read.
func (a *App) drainRoomSwitch() tea.Cmd {
	intent := a.roomSwitch
	a.roomSwitch = roomSwitchIntent{}
	if intent.target == "" {
		return nil
	}
	return a.applyRoomSwitch(intent)
}

// applyRoomSwitch performs the split the head settled in the journal.
//
// THE THREE GUARDS ARE THE WHOLE CONTRACT, and each closes a way the same row
// could be acted on twice:
//
//   - ONLY ROWS OF THE CURRENT SESSION. A poll issued before a switch can land
//     after it, and a row from the thread the reader has left may not move the
//     window a second time.
//   - ONLY ONCE. The row is acted on by the pass that first raises the
//     watermark past it (poll.go's absorb), and after the switch the new
//     session's reads cannot contain it at all — different session, different
//     rows. The absorbed-once test is the same [blocks.Transcript.IndexOf]
//     guard every other row goes through.
//   - ONLY FORWARD. A part naming the session the window is already in is a
//     no-op rather than a reset, so a replayed or mis-addressed row cannot
//     throw away a transcript to arrive where it already is.
//
// The closing row is painted BEFORE the leave, in the thread being left, which
// is the order the contract's words are in: "posting a system row in the old
// thread (continuing in <name>)". A reader who scrolls back into the old thread
// afterwards finds the sentence where the conversation stopped.
func (a *App) applyRoomSwitch(intent roomSwitchIntent) tea.Cmd {
	target := strings.TrimSpace(intent.target)
	if target == "" || target == a.session {
		return nil
	}
	// THE CLOSING ROW IS DRAWN ONLY WHEN NOBODY SAID IT. 5.4's settlement row
	// usually carries the head's own sentence about why it is splitting, and
	// that row is already on screen — the absorb appended it a moment ago. A
	// receipt underneath it would be §19's same-fact-twice at the last thing a
	// reader sees in a thread they are leaving. A part-only row says nothing, and
	// then the surface owes the sentence.
	if !intent.spoken {
		if name := a.threadName(target); name != "" && a.transcript != nil {
			a.transcript.Append(&noteBlock{
				id:    roomSwitchNoteID,
				text:  roomSwitchNote + name,
				style: a.style,
			})
		}
	}
	return a.switchThread(target)
}

// roomSwitchNoteID is the closing row's block id.
const roomSwitchNoteID = "room-switch-note"

// -- the attribution row (5.3's `attribution`) ---------------------------------

// forThreadLead opens the record page's attribution row. It is a preposition
// and not a label: `for:` reads as the sentence the row is ("this work was done
// for that conversation"), where `thread:` would read as a field name — and §14
// spends no cells on naming the kind of a thing the reader can see.
const forThreadLead = "for  "

// forThreadID is the block id of the attribution row.
const forThreadID = "record-for-thread"

// forThreadRow is the record page's `for: <thread>` line, or nil when the job's
// provenance names no thread this window can put a name to.
//
// IT IS ABSENT RATHER THAN VAGUE. A job with no session on its provenance, a
// session the store has forgotten, a thread the scribe has not named, and the
// thread the reader is already standing in all produce nothing — because in
// every one of those cases the row would either name an id (13.3.4) or offer a
// door onto the room it is drawn in.
func (a *App) forThreadRow(session string) blocks.Block {
	session = strings.TrimSpace(session)
	if session == "" || session == a.session {
		return nil
	}
	name := a.threadName(session)
	if name == "" {
		return nil
	}
	return &forThreadBlock{
		id:      forThreadID,
		session: session,
		name:    name,
		style:   a.style,
	}
}

// forThreadBlock is the attribution row: one quiet line, and a door.
type forThreadBlock struct {
	id      string
	session string
	name    string
	style   *tokens.Styler

	width    int
	measured bool
	rows     []string
}

var _ blocks.Block = (*forThreadBlock)(nil)

// ID is the anchor and cache key.
func (b *forThreadBlock) ID() string { return b.id }

// IsFinalized is always true.
func (b *forThreadBlock) IsFinalized() bool { return true }

// SettledRows is every row.
func (b *forThreadBlock) SettledRows(width int) int { return len(b.Rows(width)) }

// Version never moves: a job's provenance is written at admission and does not
// change afterwards.
func (b *forThreadBlock) Version() uint64 { return 0 }

// End is completed.
func (b *forThreadBlock) End() blocks.EndState { return blocks.EndCompleted }

// Rows is the lead word and the thread's name, at the record's own body indent.
//
// TWO TIERS, and they are the row's grammar: the preposition is chrome and the
// NAME is the thing — one tier up, because it is a door and 5.22's checklist
// forbids an interactive control living permanently in the dimmest tier.
func (b *forThreadBlock) Rows(width int) []string {
	if width < 1 {
		width = 1
	}
	if b.measured && b.width == width {
		return b.rows
	}
	// TRUNCATION HAPPENS BEFORE PAINTING, always. Cutting a painted string can
	// take its reset with it and leave the rest of the frame wearing this row's
	// colour (panes.go states the same rule on the bar). So the two halves are
	// fitted as plain text first, and only what survives is painted.
	lead := strings.Repeat(" ", blocks.BodyIndent) + forThreadLead
	lead = blocks.Truncate(lead, width)
	name := blocks.Truncate(b.name, width-blocks.Width(lead))
	if b.style == nil {
		b.rows = append(b.rows[:0], lead+name)
	} else {
		b.rows = append(b.rows[:0],
			b.style.Paint(lead, blocks.StateChrome, blocks.HueNone)+
				b.style.Paint(name, blocks.StateSettled, blocks.HueNone))
	}
	b.width, b.measured = width, true
	return b.rows
}
