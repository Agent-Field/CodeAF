package tui3

// WHO OWNS ONE PIECE OF WORK, AND HOW THIS WINDOW REACHES THEM.
//
// Every row on the tasks place belongs to a CONVERSATION — the pair
// [session.TaskIndexEntry] identifies a row by is that conversation and the id
// inside it, because ids restart with every conversation. Until this file the
// surface asked one question about that pair ("is it mine?") and answered
// everything else with a read-only card. That is right for work whose
// conversation closed months ago and wrong for the rows a person actually
// presses, which are the ones happening right now.
//
// THE LANE INTO THOSE ROWS ALREADY EXISTS, AND `aforge chat` IS ALREADY ON IT.
// The ordinary local launch is a SURFACE talking to this workspace's engine over
// a unix socket (cmd/aforge's chatv3_local.go: `remote.Roam("", …)` with an
// empty machine label, dialling [enginehost.Attach]). The engine keys its
// conversations by [remote.Hello.Session], and a second hello carrying the same
// key is handed THE SAME LIVE SESSION rather than a new one — that is the whole
// of internal/enginehost's Host.open, and internal/remote has served several
// surfaces onto one conversation since version 2. So the task page can dial the
// engine it is already talking to, say which conversation, read that task's
// journal, and give the connection back — without restarting the work, moving
// it, or taking it off the window that owns it.
//
// FOUR REACHES, IN ORDER OF CERTAINTY, and every rung is a fact this surface
// already keeps rather than a new reading:
//
//   - HERE — the conversation on screen. Its live node has a room, as it always
//     did.
//   - OPEN — a conversation THIS PROCESS holds and is not drawing (keeper.go).
//     Reaching it is a switch, not an open: nothing is resumed, nothing is
//     closed, and both conversations go on running.
//   - ATTACH — a conversation the ENGINE is running that this window can join
//     as a second view ([Options.OpenTaskOwner]). The page is that task's own
//     transcript, live, with `esc` back to where the person was.
//
// AND WHERE THERE IS NO REACH AT ALL — no capability, an engine that refused, a
// conversation on no list this machine keeps — the card says the one short thing
// that is true. It does not claim the work is fine and does not promise where it
// will end up: this window cannot see either.
//
// THERE IS NO RUNG FOR "OPEN THE TRANSCRIPT INSTEAD", and its absence is a
// decision. Work that has LANDED already opens the card, which is the page that
// answers what a person came to a record row asking; work that is running is in
// a process that holds that transcript's lock, so opening the file would be
// refused by the flock even if the ladder tried. The row that owns this file's
// four rungs is the running one.
//
// A view attaches as a watcher ([remote.Hello.Watch]). The engine refuses that
// connection the keyboard even when it is unclaimed, and the page shows that it
// is reading. The view cannot steer or stop work in either conversation.

import (
	"path/filepath"
	"strings"

	tea "charm.land/bubbletea/v2"

	"github.com/Agent-Field/aforge-v2/internal/session"
)

// taskSessionOf is the conversation id a transcript path names: the session
// FOLDER, which is what [session.TaskIndexEntry.SessionID] carries, what
// [session.Place.ID] answers and what the presence file next door is keyed by.
//
// IT IS ONE FUNCTION BECAUSE FOUR CALLERS DO THIS ARITHMETIC. This window's own
// id, the keeper's held conversations, the classification of the away reading
// ([app.heldSessions]) and the world's rows all have to agree about what a
// conversation is CALLED, and four spellings of one path operation is four
// chances for a row to be judged foreign because somebody trimmed differently.
func taskSessionOf(file string) string {
	if file = strings.TrimSpace(file); file == "" {
		return ""
	}
	return filepath.Base(filepath.Dir(file))
}

// taskSheetSelfID is the conversation THIS WINDOW is sitting in.
//
// It is "" for a window whose journal has no folder — a conversation nothing has
// written yet — and [app.taskSheetOwnsEntry] reads that as "this window cannot
// say who it is", which is a reason to refuse a claim rather than to make one.
func (a *app) taskSheetSelfID() string { return taskSessionOf(a.file) }

// taskSameTranscript reports whether two paths name one journal. It is the
// comparison [app.convKey] makes, without the keeper's symlink resolution: both
// sides of this one came out of the same world reading or off the same wire.
func taskSameTranscript(a, b string) bool {
	a, b = strings.TrimSpace(a), strings.TrimSpace(b)
	if a == "" || b == "" {
		return false
	}
	return filepath.Clean(a) == filepath.Clean(b)
}

// taskReach is HOW FAR AWAY one row's owner is, in the only terms that change
// what a key does.
type taskReach uint8

const (
	// reachUnknown is a row whose conversation nothing on this surface can find:
	// no live node, no transcript, no row in the world's own walk. The card is
	// all there is, and it says so.
	reachUnknown taskReach = iota
	// reachHere is the conversation on screen. Its live node has a room.
	reachHere
	// reachOpen is a conversation THIS PROCESS holds and is not drawing.
	reachOpen
	// reachAttach is a conversation the engine is running that this window can
	// join as a second view, for the length of one page.
	reachAttach
	// reachAway is work another aforge window is running that this window has no
	// capability to join. It is the one reach with no door.
	reachAway
)

// taskOwner is one row's conversation and what can be done about it.
type taskOwner struct {
	// id is the conversation the work came out of, and "" for a row that names
	// none.
	id string
	// row is the world's own record of that conversation, and the zero value
	// where nothing has met it. It carries the transcript the doors below open
	// and the project directory they check.
	row session.SessionRow
	// window is what the OTHER window calls itself, on a row this surface can
	// only report rather than open.
	window string
	reach  taskReach
}

// name is what to CALL this owner on screen: the conversation's own title, then
// the project it is in, and nothing at all when neither is known — the emptiness
// law, applied to a name.
func (o taskOwner) name() string {
	if title := strings.TrimSpace(o.row.Title); title != "" {
		return title
	}
	if project := strings.TrimSpace(o.row.Project); project != "" {
		return project
	}
	return strings.TrimSpace(o.window)
}

// openable reports whether this owner is a conversation THIS PROCESS can be
// switched to — which needs both the rung and the transcript the switch is keyed
// by.
func (o taskOwner) openable() bool {
	return o.reach == reachOpen && strings.TrimSpace(o.row.Transcript) != ""
}

// taskOwnerOf answers who holds one row of work and how far away they are.
func (a *app) taskOwnerOf(item tasksItem) taskOwner {
	owner := taskOwner{id: strings.TrimSpace(item.entry.SessionID), window: item.window}
	// WORK ANOTHER CONVERSATION IS RUNNING RIGHT NOW is the row this whole file
	// is about, and it is answered first because the rungs below it are about
	// records: it has no row in any index — an ordinary task writes none until it
	// lands (internal/session's taskelsewhere.go) — so only the presence reading
	// and the world's own walk know anything about it at all.
	if item.away {
		owner.row = tasksRowFor(a.taskSheet.world, a.taskSheet.mine, item.entry)
		switch {
		case item.here:
			// ...and the window may be US. A second conversation of this process
			// writes the same presence file every other terminal reads, and the
			// way to it is `tab` rather than another terminal.
			if held, ok := a.taskOwnerHeld(owner.id); ok {
				owner.row, owner.reach = held, reachOpen
				return owner
			}
		case a.openTaskOwner != nil && strings.TrimSpace(owner.row.Transcript) != "":
			// THE ENGINE IS ALREADY RUNNING IT AND THIS WINDOW CAN JOIN. Nothing
			// is started: the hello names a conversation the host is holding, and
			// a host hands a second hello the session it already has.
			owner.reach = reachAttach
			return owner
		}
		owner.reach = reachAway
		return owner
	}
	entry := item.entry
	if a.taskSheetNodeFor(&entry) != nil {
		owner.reach = reachHere
		return owner
	}
	// AND EVERY OTHER ROW IS A RECORD, which this file has no rung for and needs
	// none: the card is already the page that answers what a person came to a
	// landed row asking, and it is what [tasksPlace.enter] opens. What is carried
	// out of here for it is the ROW the reading labelled the work with — the
	// world's own walk of the disk, and never this conversation standing in for a
	// conversation the scan did not meet ([tasksRowFor] states that rule and what
	// it costs).
	owner.row = item.row
	return owner
}

// taskOwnerHeld is the conversation this PROCESS holds under one id, and whether
// there is one. The conversation on screen is deliberately not one of them: it
// has already been answered by [reachHere] above, and a switch to where you are
// standing is not a door.
func (a *app) taskOwnerHeld(id string) (session.SessionRow, bool) {
	if strings.TrimSpace(id) == "" {
		return session.SessionRow{}, false
	}
	for _, held := range a.behind {
		file := strings.TrimSpace(held.conv.SessionFile)
		if file == "" || taskSessionOf(file) != id {
			continue
		}
		return session.SessionRow{
			ID: id, Transcript: file,
			Title:      strings.TrimSpace(held.side.titleOf()),
			ProjectDir: strings.TrimSpace(held.conv.Workspace),
		}, true
	}
	return session.SessionRow{}, false
}

// titleOf is what the surface was calling a stowed conversation, and "" for one
// with no aside at all — which a door reads as "no name", never as a crash.
func (s *aside) titleOf() string {
	if s == nil {
		return ""
	}
	return s.title
}

// ── the second view ─────────────────────────────────────────────────────────

// taskGuest is one attached view onto another conversation, held for exactly as
// long as the page that opened it (room.go's [taskRoom.guest]).
//
// IT IS READ-ONLY, AND THAT IS A DESIGN AND NOT A GAP. The whole ask was to be
// able to SEE work that is running in another conversation; typing into it needs
// this window to hold that conversation's keyboard, a message identity the other
// window would recognise, and a draft that belongs to the task rather than to
// the conversation on screen — three things the surface is growing in other lanes
// and none of which a reader needs. So the view attaches as a WATCHER
// ([remote.Hello.Watch]), the engine refuses it the keyboard by construction, and
// the page says it is reading.
type taskGuest struct {
	// The opening request allows a reversible page to reacquire this exact owner.
	ask taskOwnerAsk
	// session is the transcript the engine confirmed, and it is the guest's
	// IDENTITY: nothing is drawn until it matches what was asked for. sessionID
	// is the conversation that transcript belongs to, and it is what a per-task
	// key on this page must be built from — an id alone aliases the local graph.
	session   string
	sessionID string
	// owner is what to call that conversation on screen.
	owner string
	// trail is the work ABOVE this page inside that conversation, outermost
	// first, frozen at the moment the page was opened (roomcrumbs.go draws it).
	//
	// IT IS FROZEN BECAUSE THE RECORD IS THE ONLY PLACE IT EXISTS. This window has
	// no graph for another conversation's work — [taskGuest.node] is built from the
	// one row that was pressed, and resolving a parent id against [app.tasks] would
	// name THIS conversation's task of that number, which is the crossover the
	// whole guest lane is written to prevent. So the chain is read once, from the
	// record the row itself came off, and never re-asked: a photograph of the
	// family, exactly as the node beside it is a photograph of the work.
	//
	// AND NONE OF IT IS A DOOR. Opening one of these would mean a second attach to
	// somebody else's engine from inside a page that is already one; the crumbs
	// say where this sits and `esc` is still the way out.
	trail []string
	// node is this page's whole knowledge of the work, built from the row the
	// person pressed and OWNED BY THE PAGE.
	//
	// IT IS NEVER IN [app.tasks] AND NEVER READ OUT OF IT. Task ids restart with
	// every conversation, so the id on this page is very likely also the id of
	// some task of the conversation on screen — and a header, a state word, a
	// stop, a model change or a related-row click that resolved this page's id
	// against the local graph would describe, and then act on, THE WRONG PIECE OF
	// WORK. Every reader goes through [app.roomNode], which answers with this.
	node *taskNode
	// room reads one task's journal tail over that view.
	room func(id uint64, tail int) (session.TaskRecord, error)
	// lost is the engine saying this conversation is not the one it joined any
	// more — another window opened something else in that session. It is a FINAL
	// answer: the page keeps everything it read, says this, and stops asking.
	lost  bool
	close func() error
	freed bool

	// notices is the OWNER'S OWN task lane, and told says it has spoken at least
	// once. Until it has, everything on this page about what the work is doing is
	// the row the person pressed — true when it was read, and never re-asserted as
	// a present this window cannot see ([app.roomGuestStale] draws that line).
	//
	// stopWatch leaves the subscription; it is given back with the view, because
	// the lane and the connection are the same connection.
	notices   <-chan session.Event
	stopWatch func()
	told      bool

	// asking is what the conversation that OWNS this work is waiting on a person
	// for, oldest first, off its own questions lane ([TaskOwnerView.Questions]).
	//
	// IT IS DRAWN AND NEVER ANSWERED, and that is the whole posture of this page.
	// A window that came to READ one task must not decide for the window that
	// owns it — the wire refuses the answering door to a reading surface by
	// construction (internal/remote's watcherReads) — but a page that drew
	// `working` over a conversation which has stopped and is waiting on somebody
	// would be telling the same lie the owner's task lane was added to end.
	//
	// stopAsking leaves the subscription. It is given back with the view, because
	// the lane and the connection are the same connection.
	asking     []session.Question
	questions  <-chan session.Event
	stopAsking func()
}

// waiting is the question this page says the owner is stopped on: the oldest,
// which is the one the block in the owning window has at its head. Nothing is
// said where there is no lane or nothing open, which is the emptiness law — a
// page with no reading draws no row about one.
func (g *taskGuest) waiting() (session.Question, bool) {
	if g == nil || len(g.asking) == 0 {
		return session.Question{}, false
	}
	return g.asking[0], true
}

// taskGuestGoneWord is what a page says when the conversation under it was
// replaced. It names what happened rather than blaming the reading, and it does
// not offer a retry, because there is nothing to retry — the conversation this
// page was about is not open there any more.
const taskGuestGoneWord = "the conversation that was running this opened something else — this is the last of it"

// taskGuestGoneMark is the fragment of the engine's own refusal this page reads
// its final answer out of ([remote.ErrJoinedGone]). It is matched on a fragment
// because the sentence crosses a wire as text and the two packages do not import
// each other; the fragment is the part that carries the meaning.
const taskGuestGoneMark = "not open here any more"

// tookGuestRecord folds one reading into a page read through another
// conversation, and re-answers the one question the reading itself cannot.
//
// THE JOURNAL DOES NOT SAY WHETHER THE WORK IS OVER. [session.TaskRecord] is a
// report, a tail and whether the file is still there — no state — so a page that
// took its state from the reading would be inventing one. The node this page
// draws was built from the row a person pressed, and that row is a PHOTOGRAPH: it
// said `running` at the instant the tasks place was read, and it will go on
// saying so an hour after the work landed.
//
// AND THE STATE IS NOT RE-ASKED HERE AT ALL ANY MORE. It used to be read off
// [app.taskSheetAwayRows] — the presence directory of the workspace THIS window
// is sitting in — and that reading was wrong twice over. It is the wrong
// MACHINE'S question: a conversation in another project writes its presence
// somewhere this window never looks, so a guest page onto one found nothing and
// read the absence as "the work is over" the instant it opened. And absence is
// not evidence anyway: a presence file goes stale, is rewritten on a beat, and
// says nothing at all about a task that has simply not been mentioned in it.
//
// WHAT IS TRUE ABOUT SOMEBODY ELSE'S WORK IS WHAT THEY SAY ABOUT IT. The owner's
// engine already publishes exactly that — its task lane, replayed whole on open
// ([TaskOwnerView.Watch]) — and that is now the one source, folded in by
// [app.tookGuestNotice]. This function keeps the half the reading really does
// answer: whether there was anything to read at all.
func (a *app) tookGuestRecord(msg roomRecordMsg) {
	guest, room := a.roomGuest(), a.room
	if guest == nil || room == nil {
		return
	}
	if msg.err == nil {
		return
	}
	// The engine refusing on identity is not a failure to READ. Another window
	// opened something else in that session; there is nothing more to have, so
	// the page keeps what it has and says the final thing rather than retrying.
	// AND THE OWNER'S LANE IS GIVEN BACK IN THE SAME BREATH ([taskGuest.dropWatch]):
	// a final answer is final about the subscription too, and a lane held until
	// the page closes is a reader parked on a conversation that has already said
	// its last word to this window.
	if strings.Contains(msg.err.Error(), taskGuestGoneMark) {
		guest.lost, room.readFailed = true, false
		guest.dropWatch()
	}
}

// taskGuestNoticeMsg is one thing the owner's conversation said about its own
// work, on its way to the page that is reading it.
type taskGuestNoticeMsg struct {
	gen int
	ev  session.Event
	// closed is the lane ending — the connection went, or the view was given
	// back. Nothing is re-armed after it.
	closed bool
}

// waitGuestNotices takes one notice off the owner's lane and asks for the next,
// in the shape every other lane on this surface is pumped in (room.go's
// [waitRoom]).
func waitGuestNotices(ch <-chan session.Event, gen int) tea.Cmd {
	return func() tea.Msg {
		ev, ok := <-ch
		if !ok {
			return taskGuestNoticeMsg{gen: gen, closed: true}
		}
		return taskGuestNoticeMsg{gen: gen, ev: ev}
	}
}

// tookGuestNotice folds ONE of the owner's own notices into the page, and it is
// the only thing on this surface allowed to say what another conversation's work
// is doing.
//
// IT MATCHES ON THE ID WITHIN THAT CONVERSATION, which is safe here and nowhere
// else: the lane belongs to the joined session and to no other, so a notice on it
// is about that session's numbering by construction — the same reason the guest's
// node is off the local graph entirely.
func (a *app) tookGuestNotice(msg taskGuestNoticeMsg) tea.Cmd {
	room := a.room
	if room == nil || room.gen != msg.gen || room.guest == nil {
		return nil
	}
	guest := room.guest
	if guest.lost {
		// A LOST PAGE STOPS LISTENING AS WELL AS ASKING. The subscription is
		// released here rather than left for the page's close ([taskGuest.dropWatch]
		// is idempotent, so the close releasing it again is a no-op): a lane
		// nobody re-arms is not a lane that was given back, and holding it keeps
		// this window a reader on a conversation that has already given its
		// final answer.
		guest.dropWatch()
		return nil
	}
	if msg.closed || guest.notices == nil {
		// THE LANE IS OVER AND NOTHING IS RE-ARMED. A command built around a nil
		// channel never returns, so asking for the next notice here would park a
		// goroutine for the life of the window rather than end the subscription.
		guest.notices = nil
		a.roomTouched()
		a.touch()
		return nil
	}
	notice := msg.ev.Task
	if notice == nil || notice.ID != room.id {
		return waitGuestNotices(guest.notices, msg.gen)
	}
	guest.told = true
	if guest.node != nil {
		guest.node.stopped, guest.node.ending = notice.Stopped, notice.Ending
		live := taskLiveLines(notice)
		guest.node.doing, guest.node.mending, guest.node.waiting = live.doing, live.mending, live.waiting
		if model := strings.TrimSpace(notice.Model); model != "" {
			guest.node.model = model
		}
		if state := session.TaskState(strings.TrimSpace(string(notice.State))); state != "" {
			guest.node.state = state
		}
		if title := strings.TrimSpace(notice.Title); title != "" {
			guest.node.title = title
		}
		if notice.Elapsed > 0 {
			guest.node.elapsed = notice.Elapsed
		}
	}
	// A NODE THE OWNER CALLS SETTLED IS SETTLED, and one it calls live is not.
	// Both directions matter: a page opened over running work must stop saying so
	// when it lands, and a page opened over a row that had already landed must not
	// keep the foot up if the owner revives it.
	was := room.done
	room.setDone(roomRowDone(guest.node))
	a.roomTouched()
	a.touch()
	next := waitGuestNotices(guest.notices, msg.gen)
	if was && !room.done {
		// AND THE JOURNAL BEAT IS PUT BACK when work this page had settled starts
		// again. [app.farRoomRead] stops reading on a page it believes is over, so
		// nothing else would ever ask that conversation for another line.
		return tea.Batch(next, farRoomTick(room.gen))
	}
	return next
}

// taskGuestQuestionMsg is one question the owner's conversation raised, withdrew
// or had answered, on its way to the page that is reading it.
type taskGuestQuestionMsg struct {
	gen int
	ev  session.Event
	// closed is the lane ending — the connection went, or the view was given
	// back. Nothing is re-armed after it.
	closed bool
}

// waitGuestQuestions takes one event off the owner's questions lane and asks for
// the next, in the shape every other lane on this surface is pumped in.
func waitGuestQuestions(ch <-chan session.Event, gen int) tea.Cmd {
	return func() tea.Msg {
		ev, ok := <-ch
		if !ok {
			return taskGuestQuestionMsg{gen: gen, closed: true}
		}
		return taskGuestQuestionMsg{gen: gen, ev: ev}
	}
}

// tookGuestQuestion folds ONE of the owner's questions into the page.
//
// IT KEEPS THE ENGINE'S OWN ORDER, oldest first, replacing a question already
// held rather than appending it — the lane replays everything open on arrival
// and re-emits a question whose words changed, and a page that appended would
// say a conversation was waiting on two answers when it is waiting on one.
func (a *app) tookGuestQuestion(msg taskGuestQuestionMsg) tea.Cmd {
	room := a.room
	if room == nil || room.gen != msg.gen || room.guest == nil {
		return nil
	}
	guest := room.guest
	if guest.lost || msg.closed || guest.questions == nil {
		// A LANE THAT ENDED IS NOT RE-ARMED. A command built around a nil channel
		// never returns, so asking for the next event here would park a goroutine
		// for the life of the window rather than end the subscription.
		guest.questions = nil
		a.roomTouched()
		a.touch()
		return nil
	}
	if asked := msg.ev.Question; asked != nil {
		token := questionTokenOf(*asked)
		switch msg.ev.Kind {
		case session.EventQuestion:
			replaced := false
			for at := range guest.asking {
				if questionTokenOf(guest.asking[at]) == token {
					guest.asking[at], replaced = *asked, true
					break
				}
			}
			if !replaced {
				guest.asking = append(guest.asking, *asked)
			}
		case session.EventQuestionWithdrawn, session.EventQuestionAnswered:
			kept := guest.asking[:0]
			for _, standing := range guest.asking {
				if questionTokenOf(standing) == token {
					continue
				}
				kept = append(kept, standing)
			}
			guest.asking = kept
		}
	}
	a.roomTouched()
	a.touch()
	return waitGuestQuestions(guest.questions, msg.gen)
}

// roomGuestStale reports that this page will NEVER hear from the conversation
// that owns the work — so everything it says about what the work is doing is the
// row the person pressed, and is not a present this window can see.
//
// IT IS ASKED OF THE LANE AND NOT OF THE CLOCK. A view that HAS the owner's lane
// is told the truth about its row in the same breath it opens, because the
// subscription replays the roster on arrival ([TaskOwnerView.Watch]) — so a page
// that waited on a timer would draw a caveat for a few milliseconds on every
// open and teach a person to ignore it. What is worth saying is the case where
// there is no lane at all: a door that could read a journal and not ask its
// owner anything. The page then still shows what it read, and says once that the
// state beside it is the last thing this window was told rather than now.
func (a *app) roomGuestStale() bool {
	guest := a.roomGuest()
	return guest != nil && !guest.lost && guest.notices == nil
}

// roomGuestStaleWord is that sentence. It names WHEN the claim was true rather
// than casting doubt on the page, because the page is not in doubt — the rows on
// it were read from the owner's own journal.
const roomGuestStaleWord = "current status unavailable — showing the last known state"

// dropWatch gives the owner's notice lane back, once, ahead of the page itself:
// a page that has its final answer ([taskGuest.lost]) has nothing left to hear.
// The stop is nilled in the same motion, so the ordinary release on the way out
// of the room cannot close the subscription a second time.
func (g *taskGuest) dropWatch() {
	if g == nil {
		return
	}
	if g.stopWatch != nil {
		g.stopWatch()
		g.stopWatch = nil
	}
	g.notices = nil
	// AND THE QUESTIONS LANE GOES WITH IT. A page that has its final answer has
	// nothing left to hear on either subscription, and what it last knew about a
	// question is not re-asserted as a present this window can see.
	if g.stopAsking != nil {
		g.stopAsking()
		g.stopAsking = nil
	}
	g.questions, g.asking = nil, nil
}

// release gives the view's connection back, once. It is idempotent because the
// page can be replaced and closed on one keystroke ([app.newRoom] closes the old
// page before building the next), and a door closed twice on a socket is an
// error nobody can act on.
func (g *taskGuest) release() {
	if g == nil || g.freed {
		return
	}
	g.freed = true
	// THE SUBSCRIPTIONS GO FIRST AND THEN THE CONNECTION, because the lanes ride
	// the connection: leaving one afterwards would be leaving a lane on a socket
	// that has already gone.
	if g.stopWatch != nil {
		g.stopWatch()
	}
	if g.stopAsking != nil {
		g.stopAsking()
	}
	if g.close != nil {
		_ = g.close()
	}
}

// The sentences a page read through somebody else's conversation says.
const (
	// roomGuestLane is the box's placeholder on a page this window is READING.
	// The box does not offer to steer — a placeholder promising a delivery the
	// door cannot make is the exact lie the steer guard exists to prevent — and it
	// keeps the way out, because that is the one key this page does have.
	roomGuestLane = "Reading this task"
	// roomGuestReadingWord is what is said if a line is sent anyway — through the
	// room's own note, with the words left in the box.
	roomGuestReadingWord = "this window is reading this task — go to the conversation that owns it to steer or stop it"
	// roomGuestOwnerWord opens the line naming whose conversation this page is,
	// so nothing on it can be read as this window's own work.
	roomGuestOwnerWord = "reading in "
	// roomGuestAskedWord is the clause after a question the OWNER is waiting on:
	// what is true about it here, which is that this page is not where it gets
	// answered. It names the door rather than refusing — the conversation a
	// person wants is `esc` and one row on home away, and going there brings the
	// question with it.
	//
	// IT IS SHORT BECAUSE IT SHARES A ROW WITH THE QUESTION ITSELF. The head is
	// the asker's own sentence and can be long; the room's body is narrower than
	// the screen whenever the task rail is up; and this clause is the half that
	// would be lost to the truncation, which is the half that says the key a
	// person is about to look for is not here.
	roomGuestAskedWord = "answer it in that conversation"
	// taskOwnerOpeningWord stands on the tasks page while the engine is being
	// asked. It is a beat or two on a socket, and the page a person just pressed
	// saying nothing at all is what makes a surface feel broken.
	taskOwnerOpeningWord = "opening that task…"
	// taskOwnerBusyWord is the engine refusing, or the link not answering. The
	// reason travels with it: `could not open that conversation` alone is a
	// sentence a person cannot act on, and the row is still there to press again.
	taskOwnerBusyWord = "could not open that conversation"
	// taskOwnerWrongWord is the identity check failing — the engine answered
	// about a conversation other than the one that was asked for. Nothing is
	// drawn: a transcript under the wrong name is the failure this whole lane
	// exists to end, and it is worse than no page at all.
	taskOwnerWrongWord = "the engine opened a different conversation — nothing was shown"
)

// ── going there ─────────────────────────────────────────────────────────────

// openTaskInOwner takes the person to the conversation that owns one piece of
// work, standing in that task's own room.
//
// IT IS EXISTING MECHANISMS AND NO THIRD. A conversation this process holds
// comes forward through the keeper's own switch ([app.bringForward]); the room
// it lands in is the one the aside already carries, which is how a switch has
// always put a person back where they were (detach.go's [app.restoreAside]). So
// this AIMS the aside and then switches — it does not open a room, does not
// build an agent and does not touch the conversation being left, which goes into
// the keeper still running.
//
// A CONVERSATION NOBODY HERE HOLDS GOES THROUGH THE OTHER PLACES' OWN LADDER
// ([app.openConversationRow]): the keeper first, then one stat of the project
// folder, then open-beside — and its refusal, when another process holds the
// flock, is that ladder's own sentence on the router's message line.
// It reports whether it took the person anywhere, so a caller with a card to
// fall back on knows whether to draw it.
func (a *app) openTaskInOwner(owner taskOwner, item tasksItem) (tea.Cmd, bool) {
	if !owner.openable() {
		return nil, false
	}
	// THE ROOM IS AIMED BEFORE THE SWITCH AND THE SWITCH IS THE LADDER'S. Aiming
	// is one field on a conversation this process is already holding, and doing it
	// first is what makes the two acts one gesture; the ladder below then does
	// what it does everywhere else, including standing this place down and saying
	// its own refusal when the conversation turns out to be another process's.
	a.aimHeldAtRoom(owner.row.Transcript, taskSheetEntryID(item.entry.ID))
	return a.openConversationRow(owner.row), true
}

// aimHeldAtRoom points a stowed conversation's aside at one of its tasks, so the
// switch that follows lands in that task's room rather than at the foot of its
// transcript. It reports whether there was a conversation to aim.
//
// IT WRITES ONE FIELD AND READS NONE. The aside is the person's own state in
// that conversation — their draft, their scroll, their half-answered card — and
// a door that rebuilt it would throw all of that away; [aside.room] is the one
// field that says "the page they were on", and this is a person choosing a
// different one.
func (a *app) aimHeldAtRoom(file string, id uint64) bool {
	key := a.convKey(file)
	held := a.behind[key]
	if held == nil {
		return false
	}
	if held.side == nil {
		held.side = &aside{}
	}
	if id != 0 {
		held.side.room = id
	}
	return true
}

// openOwnerRoom opens ONE TASK OF ANOTHER CONVERSATION as a page in this window,
// through a second view onto the engine that is already running it.
//
// NOTHING ABOUT THE OWNER MOVES. The view attaches as a returning surface, so
// the keyboard stays with the window that has it (internal/remote's driver.go);
// the conversation is not resumed, not swapped and not interrupted; and the page
// is given back on the way out, which releases this view's connection and
// nothing else ([app.closeRoom]).
//
// THE IDENTITY IS CHECKED BEFORE A SINGLE ROW IS DRAWN. A view that came back
// about a different conversation — a key that did not match, a session the
// engine had replaced — would put one task's transcript under another task's
// name, and every id on the page would be read against the wrong graph. It is
// closed and refused instead.
//
// AND THE PAGE'S `done` IS THE ROW'S. This window's roster has no row for a task
// in another conversation, and [roomRowDone] answers `over` for a node it has
// never seen — which would draw `task finished` under work that is running next
// door. What the tasks place said about the row is the only reading this window
// has, so it is what the page opens with.
//
// AND THE ASK HAPPENS OFF THE PROGRAM LOOP. Reaching the engine is a socket and
// a round trip, and this surface's loop draws every frame and answers every key:
// a call made here would freeze the list — the cursor, `esc`, the filter, the
// whole window — for as long as another process took to answer, which on the one
// path where the engine is wedged is forever. So the key starts the ask and
// returns, the page says it is opening, and [app.tookTaskOwner] builds the room
// when the answer lands.
//
// A REFUSAL FALLS BACK TO THE CARD rather than to nothing. The person pressed a
// row and is owed a page; the reason it is the card is put beside it, and the row
// is still there to press again.
func (a *app) openOwnerRoom(owner taskOwner, item tasksItem) (tea.Cmd, bool) {
	if a.openTaskOwner == nil {
		return nil, false
	}
	transcript := strings.TrimSpace(owner.row.Transcript)
	if transcript == "" {
		return nil, false
	}
	// THE ASK IS NUMBERED, and the number is what makes a slow answer harmless.
	// A person who presses a row, changes their mind, presses another and then
	// leaves the page altogether has two views in flight onto two conversations;
	// each one arrives claiming a page, and only the LAST one asked for is still
	// wanted. The rest are closed on arrival — this window's client and nothing
	// else — so a conversation is never left with a reader nobody can see
	// ([app.tookTaskOwner]).
	a.taskOwnerGen++
	ask := taskOwnerAsk{
		gen:  a.taskOwnerGen,
		item: item,
		name: owner.name(),
		file: transcript,
		dir:  strings.TrimSpace(owner.row.ProjectDir),
	}
	a.taskOwnerAt = ask
	a.pageMsg = taskOwnerOpeningWord
	a.touch()
	open := a.openTaskOwner
	return func() tea.Msg {
		view, err := open(TaskOwnerAsk{Session: ask.file, Workspace: ask.dir})
		return taskOwnerMsg{gen: ask.gen, view: view, err: err}
	}, true
}

// taskOwnerAsk is one attach in flight: which row asked, and what it asked for.
// It is held so the answer can be turned into a page without going back to a
// reading that may have been replaced while the socket was busy.
type taskOwnerAsk struct {
	trail     []string
	fromStart bool
	front     string
	gen       uint64
	item      tasksItem
	name      string
	file      string
	dir       string
}

// taskOwnerMsg is the engine's answer to one of those.
type taskOwnerMsg struct {
	gen  uint64
	view TaskOwnerView
	err  error
}

// tookTaskOwner turns one answer into the page, or into the reason there is not
// one.
//
// EVERY ROAD OUT OF HERE THAT IS NOT A PAGE GIVES THE VIEW BACK. A stale
// generation, a conversation that came back under the wrong name, a view with no
// reader on it — each of them holds a live connection to somebody else's engine,
// and dropping the value on the floor would leave that connection attached for
// the life of this window.
func (a *app) tookTaskOwner(msg taskOwnerMsg) tea.Cmd {
	ask := a.taskOwnerAt
	stale := msg.gen != a.taskOwnerGen || (ask.fromStart && (a.startingChat() || a.file != ask.front || a.roomOpen()))
	if !stale {
		a.taskOwnerAt = taskOwnerAsk{}
	}
	release := func() {
		if msg.view.Close != nil {
			_ = msg.view.Close()
		}
	}
	switch {
	case stale:
		// Somebody pressed something else, or left. Nothing is drawn and nothing
		// is said — the page they are on now is the answer to what they did last.
		release()
		return nil
	case msg.err != nil:
		release()
		a.pageMsg = taskOwnerBusyWord + railSep + strings.TrimSpace(msg.err.Error())
		a.touch()
		return a.taskSheetAwayCard(ask.item)
	case !taskSameTranscript(msg.view.Session, ask.file) || msg.view.Room == nil:
		release()
		a.pageMsg = taskOwnerWrongWord
		a.touch()
		return a.taskSheetAwayCard(ask.item)
	}
	guest := &taskGuest{
		ask:       ask,
		session:   msg.view.Session,
		sessionID: taskSessionOf(msg.view.Session),
		owner:     ask.name,
		node:      taskGuestNode(ask.item),
		// The chain is read HERE, while the record that names it is still standing:
		// the list is closed three lines down, and after that this window has no
		// way to say what this piece of work was cut out of.
		trail: a.taskGuestTrail(ask.item),
		room:  msg.view.Room,
		close: msg.view.Close,
	}
	if ask.trail != nil {
		guest.trail = ask.trail
	}
	// AND THE OWNER IS ASKED WHAT ITS WORK IS DOING, once, on the connection that
	// is already open. The lane replays the whole roster the moment it is taken,
	// so the first thing this page hears is the truth about the row it opened on —
	// and everything after it is the owner correcting its own answer.
	if msg.view.Watch != nil {
		guest.notices, guest.stopWatch = msg.view.Watch()
	}
	// AND WHETHER THAT WORK HAS STOPPED AND IS WAITING ON A PERSON. It is the one
	// thing the roster cannot say — a node sitting on a question is still
	// `running` — and it is the same shape of reading: a standing subscription
	// that replays what is already open, so the first thing this page hears is
	// every question that conversation is holding (internal/remote's
	// questionlane.go).
	if msg.view.Questions != nil {
		guest.questions, guest.stopAsking = msg.view.Questions()
	}
	// The list is stood down before the page goes up, so `esc` from the room is
	// the ordinary way back to the conversation and the rail beside it is this
	// window's own — which is what makes this a page rather than a mode.
	a.pageMsg = ""
	a.closeTaskSheet()
	room := a.newRoom(guest.node.id, tasksLabel(ask.item.entry))
	room.guest = guest
	// AND THE PAGE'S `done` IS THE ROW'S. This window's roster has no row for a
	// task in another conversation, and [roomRowDone] answers `over` for a node it
	// has never seen — which would draw `task finished` under work running next
	// door. What the tasks place said about the row is the only reading this
	// window has.
	room.done = !ask.item.runs
	a.room = room
	// The foreign transcript is part of the recipient key because task ids restart.
	a.retargetComposer(guestRecipient(guest.session, room.id))
	a.sel = -1
	a.dropHover()
	a.touch()
	// AND THE PAGE SAYS IT IS LOADING ONLY IF A READ IS ACTUALLY ON THE WAY
	// (room.go's [app.armRoomRecord]). A view whose owner handed back no room
	// reader answers nil here, and the flag used to be raised above this line
	// regardless — which left `loading this task's conversation…` under a header
	// with a running clock and nothing coming to replace it, for ever.
	a.armRoomRecord()
	if guest.notices != nil {
		// THREE PUMPS, ONE PAGE: the journal reading on its own beat, the owner's
		// notices whenever the owner has something to say, and the owner's
		// questions. All are stamped with this room's generation, so leaving the
		// page discards every one of them.
		a.roomPump = tea.Batch(a.roomPump, waitGuestNotices(guest.notices, room.gen))
	}
	if guest.questions != nil {
		a.roomPump = tea.Batch(a.roomPump, waitGuestQuestions(guest.questions, room.gen))
	}
	return a.takeRoomPump()
}

// taskGuestTrail is the work above one row of the record, inside ITS OWN
// conversation, outermost first.
//
// IT IS THE RECORD'S OWN SHAPE AND NOT A SECOND WALK. [tasksTreeOf] is what the
// tasks place draws its families from, and its `up` map is scoped to one
// conversation by construction — a parent id is only ever matched against rows
// of the same session, because ids restart with every one of them. So a chain
// read here cannot cross into another conversation's work or into this window's.
//
// THE WHOLE READING IS WALKED AND NOT THE FILTERED ONE. A person who typed three
// letters to find this row narrowed the page, not the family; the filter keeps
// the rows above a hit for exactly this reason, but the unfiltered reading is the
// one that cannot have a hole in it.
//
// A CONVERSATION IS NOT A CRUMB HERE: the guest trail's root is already whose
// conversation this is ([roomGuestOwnerWord]), and the tree hangs its roots under
// a conversation key rather than under another task, so the walk simply ends.
func (a *app) taskGuestTrail(item tasksItem) []string {
	tree := a.taskSheet.reading.tree()
	var up []string
	seen := make(map[tasksKey]bool)
	for at := tasksKeyOf(item.entry); !seen[at]; {
		seen[at] = true
		parent, ok := tree.up[at]
		if !ok || parent.chat() {
			break
		}
		found, ok := tree.at[parent]
		if !ok {
			break
		}
		// A PIECE OF WORK NOTHING NAMED IS NOT A CRUMB. The record carries rows with
		// no words on them, and `main ▸ 7 ▸ this` has told a person nothing — the
		// same refusal the kin line already made about a parent it could not name.
		word := tasksLabel(found.entry)
		if strings.TrimSpace(word) == "" {
			break
		}
		up = append(up, word)
		at = parent
	}
	for i, j := 0, len(up)-1; i < j; i, j = i+1, j-1 {
		up[i], up[j] = up[j], up[i]
	}
	return up
}

// taskGuestNode is the page's own node for one piece of another conversation's
// work: everything the row carried, and nothing invented.
//
// IT IS BUILT FROM THE ROW AND MARKED `restored`, which is the surface's existing
// word for a node this window never watched (task.go's [taskNode.restored]). That
// is exactly true here — this window has no lane onto the work and no history of
// it — and it is what stops the clocks from guessing an age off a moment this
// terminal happened to open the page.
//
// THE MODEL IS EMPTY BECAUSE NOBODY HERE KNOWS IT. The presence file the row was
// minted from carries a title and a state, not a model, and the emptiness law
// says an unknown draws as nothing rather than as this conversation's own.
func taskGuestNode(item tasksItem) *taskNode {
	id := taskSheetEntryID(item.entry.ID)
	title := strings.TrimSpace(item.entry.Title)
	if title == "" {
		title = strings.TrimSpace(item.entry.Label)
	}
	return &taskNode{
		id:       id,
		ident:    identFor(id),
		title:    title,
		label:    strings.TrimSpace(item.entry.Label),
		state:    session.TaskState(strings.TrimSpace(item.entry.Status)),
		ended:    item.entry.EndedAt,
		met:      item.entry.EndedAt,
		restored: true,
	}
}
