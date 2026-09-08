package tui3

import (
	"strings"
	"time"

	"github.com/Agent-Field/aforge-v2/internal/session"
)

// feed is the reducer that turns one agent's event stream into transcript
// entries. It owns the list, the assistant block being streamed into, the
// reasoning block being streamed into, and the turn every new entry is stamped
// with — everything an event can move — and it knows nothing whatever about how
// any of that is drawn.
//
// ONE REDUCER PRODUCES EVERY TRANSCRIPT'S ENTRIES; A VIEW MAY INSTALL HOOKS,
// NEVER A COPY.
//
// The law is stated here because this codebase has already paid for breaking
// it. The task room grew a hand-copy of this whole family — roomFormTool,
// roomAnnounceTool, roomBeginTool, roomCloseTool, roomSettleCompaction and the
// rest of the eleven in room.go — and being the copy is exactly why a room did
// not learn that a tool FINISHED, why a retry inside a task was invisible, and
// why an end event paired by one rule out in the conversation and another in
// there: every event kind added since had to be wired twice, and the second
// wiring is the one nobody remembers. The copies are gone
// (docs/design/lens/DESIGN.md, Decision 1), and a surface that needs the
// reducer to do something extra installs a [feedHooks] function instead; it
// does not write a second reducer, which agrees with this one on the day it is
// written and drifts from it by the next wave.
//
// IT IS EMBEDDED IN [app] AND IN [taskRoom] RATHER THAN HELD BESIDE THEM, which
// is what keeps the several hundred existing readers of `entries`, `live`,
// `think` and `turn` spelled the way they have always been spelled on both
// surfaces: the fields moved house, not name.
//
// THE LENS MAY LOWER SALIENCE; IT MAY NOT DROP A FACT. What a page does with an
// entry — folds it, dims it, puts it behind a keypress — is the page's; whether
// the entry EXISTS is this file's, for every surface at once, and
// salience_test.go walks every event kind through both to hold it.
type feed struct {
	entries []entry
	// live is the assistant entry currently being streamed into, or -1.
	live int
	// think is the reasoning block currently streaming, or -1 (thinking.go).
	think int
	// turn counts the person's messages. It groups tool calls into clusters
	// and is what ctrl+o folds and unfolds.
	turn int
	// mdAt is when the live block's prefix was last promoted to markdown
	// (render.go's [promoteBlock]). It belongs beside `live` because it is that
	// block's clock and nothing else's: it is set where the block is opened, and
	// a surface holding its own copy would be a second answer to "how old is this
	// rendering" for one block.
	mdAt time.Time
	// settledTurn is the last turn whose BOUNDARY HAS PASSED — the turn
	// [app.settleTurn] has walked — and it is what makes a delta arriving after
	// that boundary land settled rather than opening a block nothing owns.
	//
	// THE DEFECT IT CLOSES (#225). A turn ends twice on a surface and, in
	// between, a stream can still speak: the tail of a reply the provider had
	// already buffered, a straggler behind a stop. That delta found no live
	// block, opened a second one under the answer, and the two costs landed
	// together — the new block was live with no boundary left to settle it, and
	// its mere presence demoted the answer above it into narration, which is
	// drawn PLAIN (hierarchy.go). What the person read was their markdown reply
	// come back as the characters it was typed as, until they asked something
	// else. It is zero until the first turn ends, and turn numbers count from
	// one, so nothing is settled by accident.
	//
	// A SURFACE THAT NEVER ARMS IT NEVER TAKES THE LATE ROAD, which is the task
	// room today: a node's page has one ending per step and no second one to fall
	// between, so it leaves this at zero and every delta grows the live block.
	settledTurn int
	// pendingReplyTags arrived before the first words of the answer they label.
	pendingReplyTags []session.TaskReplyTag
	// hooks is what this view asked the reducer to do on its behalf, and is the
	// only thing in here that differs between one surface and another.
	hooks feedHooks
}

// newFeed is the ONE way a feed is built, and it exists because THE ZERO VALUE
// OF THIS STRUCT IS NOT AN EMPTY TRANSCRIPT — it is a broken one.
//
// `live` and `think` are indices into the entry list and -1 is the sentinel for
// "no block is streaming", so a feed made by struct literal points both of them
// at entry zero. Every guard in here is written as `live >= 0 && live <
// len(entries) && kind == …`, which a zero index passes the moment anything at
// all has been appended: the first reasoning delta of the session grows whatever
// entry zero turned out to be, and the person's own opening question comes back
// with the model's working written into it.
//
// It was hand-spelled at four sites before the room adopted the reducer, which
// is three chances to forget and one silent failure when somebody does — so the
// sentinel is stated once, here, and a second surface cannot be built wrong.
func newFeed(hooks feedHooks) feed {
	return feed{live: -1, think: -1, hooks: hooks}
}

// feedHooks are the things a reducer cannot know on its own: what time it is,
// what the page does when the transcript grows, and the handful of acts that
// belong to ONE surface rather than to the transcript everybody keeps.
//
// EVERY HOOK IS OPTIONAL, and an unset one is a reducer that simply does not do
// that thing. That is the room's configuration in the lane after this one — no
// spawn card, no ambient counts — and it is also what keeps a bare app built in
// a test from dying on a clock nobody installed. The one hook that answers a
// question rather than performs an act falls back to the same answer the view's
// own method would have given.
//
// THE SEAM IS DELIBERATELY NARROW. A hook that a second surface would also want
// is not a hook: it is reducer behaviour that was only ever written once, in
// here, where both surfaces read it.
type feedHooks struct {
	// now is the clock the entries are stamped from. It is a hook and not
	// time.Now because a surface under test pins its own (see [app.now]), and a
	// transcript whose durations came from the wall clock could not be asserted
	// on at all.
	now func() time.Time
	// snap says this view draws a burst WHOLE rather than letting the live edge
	// walk it in (reveal.go). It is the screen-reader tier and nothing else —
	// both surfaces install [app.linear] — and it is a hook rather than a field
	// read here because the reducer knows nothing about how it is drawn, which
	// is the whole of what it is for. A nil hook paces, which is the right
	// default for a reducer nobody has told: an edge that walks is the ordinary
	// surface, and holding bytes back is never what a screen reader wants.
	snap func() bool
	// follow keeps the view at the growing edge when the transcript grows, for
	// the view's own definition of "at the edge" ([app.follow] honours the stick).
	follow func()
	// touch says the rows no longer match the entries and the next frame has to
	// rebuild them ([app.touch]).
	touch func()
	// forming fires on every fragment of a call that is still ARRIVING, before
	// the row it belongs to has been claimed. The chat draws the propose_task
	// spawn card off it, because a proposal is a BLOCK rather than a row and a
	// block that popped into existence whole is the defect that card exists to
	// close (task.go). No other surface has one, which is why the reducer does
	// not know the tool's name.
	forming func(ev session.Event)
	// closing fires when a call's RESULT lands, before the row it belongs to has
	// been found. The chat reads it for the one fact that tells a refused
	// proposal from a landed one — a forming card still standing (task.go).
	closing func(ev session.Event)
	// closed fires on the row a result just resolved. The chat learns a
	// backgrounded bash from it (background.go) and drops the cache behind its
	// ambient counts (app.go's hudStats), and both of those are sums over
	// finished calls that the transcript itself knows nothing about.
	//
	// IT IS HANDED A POINTER INTO THE ENTRY LIST AND MAY NOT APPEND TO IT. A
	// growing slice reallocates, and the row [feed.closeTool] is still writing
	// would then be a row in the list nobody is drawing any more — the failure
	// would be a call that silently stopped opening itself. The one hook that
	// does append is [feedHooks.forming], which fires before any row is claimed
	// and is safe for that reason.
	closed func(e *entry, ev session.Event)
	// retrying fires when a cut request is about to be asked again, after the
	// dead attempt's rows have gone and before the reason is written down. The
	// chat throws away the half-arrived proposal card there (task.go), which is
	// a block only the chat has; a node draws no card and installs nothing.
	//
	// ITS POSITION IN [feed.retry] IS THE WHOLE OF ITS CONTRACT: the card is
	// removed by truncating the last entry wherever it IS the last entry, so a
	// note written first would leave a hollow row behind it.
	retrying func()
}

// now is the reducer's clock, or the wall clock when no view installed one —
// the same fallback [app.now] itself makes, so that a reducer with no hooks
// stamps the same times the app would have stamped.
//
// A VIEW'S OWN METHOD OF THIS NAME WINS INSIDE THE VIEW, and that is not a
// collision to be tidied away: `a.now()` inside [app] is app.go's method,
// `f.now()` inside here is this one, and this one is installed as a call THROUGH
// that method — so both spellings end at the same clock, which is the property
// that matters. The same is true of [feed.follow] and [feed.touch].
func (f *feed) now() time.Time {
	if f.hooks.now != nil {
		return f.hooks.now()
	}
	return time.Now()
}

// snap is the view's answer to "draw it whole", or false where no view said.
// See [feedHooks.snap] for why the default is to pace.
func (f *feed) snap() bool {
	if f.hooks.snap != nil {
		return f.hooks.snap()
	}
	return false
}

// follow tells the view the transcript grew at its end.
func (f *feed) follow() {
	if f.hooks.follow != nil {
		f.hooks.follow()
	}
}

// touch tells the view its rows are out of date.
func (f *feed) touch() {
	if f.hooks.touch != nil {
		f.hooks.touch()
	}
}

// ingest is the whole of what an event does TO THE ENTRIES, and it is the one
// place a new event kind gets wired.
//
// IT IS NOT THE EVENT PUMP AND MUST NOT BECOME ONE. An event does two separable
// things on a surface — it moves the transcript, and it asks the program loop
// for something (a fetch, a notification, a meter re-read) — and only the first
// is in here. The caller's own switch keeps the second, which is why a chat's
// EventToolEnd still speculatively fetches the file that was just written and a
// room's will not: that is a difference between the two pages, where the row
// the call left behind is not.
//
// An event this reducer has nothing to say about falls through and writes
// nothing, deliberately: the caller has already decided what else it means.
// lump says the text this event carries is one the WIRE buffered rather than one
// it wrote, and only the caller can answer it: a folded run of short deltas is
// not a lump however long the fold is, and the fold happens on the far side of
// this call (app.go's [waitEvent], reveal.go's header). It is carried as a
// parameter for that reason and read exactly once, in the two cases below.
func (f *feed) ingest(ev session.Event) {
	f.ingestStream(ev, isLump(len(ev.Text)))
}

// ingestStream is [feed.ingest] told whether the text it carries is a lump.
// Only a lane that FOLDS can answer that — app.go's [waitEvent] joins the run of
// short deltas that piled up behind a frame, and a fold of short deltas is not a
// lump however long the fold is (reveal.go). Every other lane delivers one wire
// event at a time and comes through [feed.ingest], which reads the length.
func (f *feed) ingestStream(ev session.Event, lump bool) {
	switch ev.Kind {
	case session.EventTextDelta:
		f.sayStream(ev.Text, lump)

	case session.EventReasoning:
		f.reasonStream(ev.Text, lump)

	case session.EventTaskReplyTags:
		f.takeReplyTags(ev.TaskReplyTags)

	case session.EventNudge:
		// The loop caught itself repeating a call: a dim one-liner, never an
		// interruption — the model is already being told, the person only needs
		// to see that it was.
		f.note(firstNonEmpty(ev.Hint, "stuck? nudged · "+ev.Tool))

	case session.EventGuardianAllowed:
		// The guardian answered for the person, and a gate that answers on
		// somebody's behalf and says nothing about it is a gate nobody can audit
		// (session's guardian.go). Same dim line, wherever the work is running.
		f.note("guardian allowed · " + ev.Tool)

	case session.EventNotice:
		// The adapter had to reshape the request to get it accepted — which
		// attempt it is on, and what it took off (internal/provider's
		// endpoints.go). Same dim one-liner as the nudge, and for the same
		// reason: it is already being handled, the person only needs to see it.
		f.note(ev.Text)

	case session.EventRetrying:
		f.retry(ev)

	case session.EventToolForming:
		// THE CALL IS ARRIVING. Nothing has been asked for yet — this is the
		// model writing the instruction, drawn while it writes it.
		f.formTool(ev)

	case session.EventToolAnnounced:
		f.announceTool(ev)

	case session.EventToolBegin:
		f.beginTool(ev)

	case session.EventToolFinished:
		f.finishTool(ev)

	case session.EventToolEnd:
		f.closeTool(ev, toolOK, "")

	case session.EventToolFailed:
		f.closeTool(ev, toolFailed, firstNonEmpty(ev.Hint, errText(ev.Err)))

	case session.EventCaption:
		f.nameStep(ev)

	case session.EventCompacting:
		f.openCompaction(firstNonEmpty(ev.Hint, "compacting"))

	case session.EventCompacted:
		// ALWAYS the other half of the pair, success or failure — a failed pass
		// says so in its hint and settles the same row, because a row left
		// spinning over a turn that moved on is the defect the pair exists to
		// close.
		f.closeLive()
		f.settleCompaction(firstNonEmpty(ev.Hint, "compacted"))
	}
}

// nameStep is the narrator speaking about ONE step (session's caption.go). It
// mints no transcript block: the sentence and the family it named are written
// onto the call the batch opened with, and both pages derive the same caption
// from the same entry list.
//
// IT KEYS ON THE ANCHOR AND NEVER ON "THE NEWEST ROW". The engine sends the
// batch's first call id with the event, and this walks for THAT row. The reason
// is a race the old keying could not survive: the narrator's goroutine checks
// that its batch is still open and can then be descheduled, so its answer can
// arrive after that batch ended, after the next one began, and after the next
// one's rows are on screen. Keyed by recency, a sentence about the finished step
// retitled the running one — silently, on the row a person is watching. Keyed by
// the anchor, an event that names a step this feed is not holding is simply
// dropped, which is the correct thing to do with news about work that is over.
//
// THE ANCHORLESS EVENT IS AN OLDER ENGINE and is served exactly as it always
// was: a host built before the anchor existed sends captions with no id, and
// dropping them would silently take the narration away from every mixed-version
// link. That path keeps the recency rule and therefore keeps the old race; every
// build that ships the anchor is free of it.
//
// BOTH HALVES OR NEITHER. The sentence and the family arrive in one event and
// are written in one assignment, so no frame can draw the new words beside the
// old mark. An event with no family CLEARS the field rather than leaving a
// previous one standing: the mark then comes off the tools, which is right about
// this batch, where a stale family would be right about the last one.
func (f *feed) nameStep(ev session.Event) {
	text := strings.TrimSpace(ev.Text)
	if text == "" {
		return
	}
	if anchor := strings.TrimSpace(ev.CallID); anchor != "" {
		for i := len(f.entries) - 1; i >= 0; i-- {
			e := &f.entries[i]
			if e.kind != entryTool || e.callID != anchor {
				continue
			}
			e.caption, e.captionCat = text, ev.Category
			f.touch()
			return
		}
		return
	}
	for i := len(f.entries) - 1; i >= 0; i-- {
		e := &f.entries[i]
		if e.turn != f.turn {
			return
		}
		if e.kind == entryTool {
			e.caption, e.captionCat = text, ev.Category
			f.touch()
			return
		}
	}
}

// openCompaction draws the row for a pass that has just started.
//
// THE PASS IS VISIBLE WHILE IT RUNS. The session sends its start the moment the
// cut is made and before the summarizer is called, and that call is the slowest
// thing on this surface that draws nothing: a turn that stops for eight seconds
// with no spinner, no text and no tool row is indistinguishable from a hang.
func (f *feed) openCompaction(hint string) {
	f.closeLive()
	f.entries = append(f.entries, entry{
		kind:  entryCompact,
		text:  hint,
		turn:  f.turn,
		began: f.now(),
	})
	f.follow()
	f.touch()
}

// formTool draws — and then keeps redrawing — the row for a call that is STILL
// ARRIVING (session.EventToolForming).
//
// THE GAP THIS CLOSES: a `write` whose body is the file and a `propose_task`
// whose brief is three paragraphs take seconds to stream, and until this
// existed the surface said nothing at all for those seconds. The announcement
// fires when the call is WHOLE; the row now exists from the first fragment, and
// says what it honestly can — how much has arrived, then the tool, then what it
// is about — gaining detail rather than appearing finished.
//
// NOTHING HERE IS UNMARSHALED. ev.ArgsText is half a JSON object, and half a
// JSON object is not a payload: the row holds the SIZE of what has arrived, the
// gloss session built from the fields that have closed, and — for the one tool
// whose whole substance is one string — the tail of that string as it streams
// ([formingPreviewField]). A surface that unmarshaled a prefix would be drawing
// a call the model has not finished asking for; [session.PartialString] is the
// other thing, a tolerant scan of one field that answers with what has arrived
// and never invents the rest.
func (f *feed) formTool(ev session.Event) {
	// AND THE VIEW GETS THE FRAGMENT FIRST. The chat's spawn card forms from
	// this same event, because a proposal is a BLOCK rather than a row and a
	// block that popped into existence whole is the defect that card is about
	// (task.go) — but which tool is worth a block is a fact about ONE page, so
	// the reducer hands over every fragment and lets the view recognise its own
	// ([feedHooks.forming]).
	if f.hooks.forming != nil {
		f.hooks.forming(ev)
	}
	at := claimForming(f.entries, ev)
	if at < 0 {
		f.closeLive()
		f.entries = append(f.entries, entry{
			kind: entryTool, tool: ev.Tool, text: ev.Hint, turn: f.turn,
			status: toolForming, callID: ev.CallID, bytes: ev.Bytes,
			formed: formingPreview(ev.Tool, ev.ArgsText),
		})
		f.follow()
		f.touch()
		return
	}
	e := &f.entries[at]
	// Every field is taken FORWARD only. The id, the name and the gloss each
	// land once and then repeat on every fragment after them, and a later event
	// that happened to carry less than the one before it must not un-say what
	// the row already knows.
	e.callID = firstNonEmpty(ev.CallID, e.callID)
	e.tool = firstNonEmpty(ev.Tool, e.tool)
	e.text = firstNonEmpty(ev.Hint, e.text)
	if ev.Bytes > e.bytes {
		e.bytes = ev.Bytes
	}
	// The live text is taken forward the same way, and for the same reason: the
	// name arrives on one fragment and the body on the ones after it, so the
	// FIRST fragment of a write is a text this cannot read yet — and an empty
	// answer must not wipe what the row was already showing.
	e.formed = firstNonEmpty(formingPreview(firstNonEmpty(ev.Tool, e.tool), ev.ArgsText), e.formed)
	f.touch()
}

// formingPreviewField names the argument a call that is still ARRIVING is shown
// the contents of, per tool. It has one entry, and the shortness of the table is
// the decision rather than an omission.
//
// `write` qualifies because its content is APPENDED TO and never revised: what
// has arrived is the beginning of the file and will still be the beginning of
// the file when the call is whole, so a person reading it is reading something
// true. Nothing else on the belt is like that.
//
// `edit` is the near miss and it is deliberately absent. Its block is a DIFF,
// and a diff needs both sides whole — half an old_string against a new_string
// nobody has started sending is not a change, it is a claim about one — so a
// live edit block would redraw itself into a different diff as the second half
// arrived, which is the exact "read the same diff twice" defect [app.previewHead]
// is written to avoid. And mechanically it could not be had cheaply anyway: bare
// spells an edit as {path, edits:[{oldText, newText}]}, and the streamed strings
// are therefore NESTED, where session's forming scanner deliberately does not
// look (its toolhint.go). The whole diff still lands the instant the call is
// announced, which is the moment it becomes true.
var formingPreviewField = map[string]string{"write": "content"}

// formingPreview is the streamed text a forming row draws, or "" for a call this
// surface previews nothing of.
func formingPreview(tool, argsText string) string {
	field, previewed := formingPreviewField[tool]
	if !previewed {
		return ""
	}
	text, _ := session.PartialString(argsText, field)
	return text
}

// claimForming finds the row this forming event belongs to, or -1 for a call
// nothing has been drawn for yet.
//
// It walks NEWEST FIRST, which is what makes an id landing late harmless: the
// wire sends the id on the first fragment in practice and is not required to,
// so a row can exist with no id at all, and the row that identity belongs to is
// the most recent one still waiting for one.
//
// Once two rows have ids they cannot be confused, which is the whole reason the
// id is kept: a batch of three parallel writes forms three rows that interleave
// fragment by fragment, and matching on the tool name alone would fold all
// three into whichever was drawn first.
//
// IT TAKES THE LIST rather than reading [app.entries], because the task room
// runs the same lane over a list of its own (room.go) and a second copy of this
// walk is a second answer to "which call is this" waiting to drift from the
// first.
func claimForming(es []entry, ev session.Event) int {
	for i := len(es) - 1; i >= 0; i-- {
		e := &es[i]
		if !e.forming() {
			continue
		}
		if e.callID != "" {
			if e.callID == ev.CallID {
				return i
			}
			continue
		}
		// A row with no id yet: this event is that row's if it does not name a
		// different call, which — with no ids on either side — is as far as
		// "same call" can honestly be decided.
		if ev.Tool == "" || e.tool == "" || e.tool == ev.Tool {
			return i
		}
	}
	return -1
}

// claimFormed finds the forming row an ANNOUNCEMENT (or a begin) completes, or
// -1. It is [claimForming]'s mirror and walks the other way: the oldest
// unfinished row of that tool is the one the batch announces first, which is
// the order the ordering law promises them in.
//
// THE ID IS THE ANSWER WHEREVER THERE IS ONE. session's announcement carries the
// call's id (its loop.go), so a batch of three parallel writes pairs exactly;
// the walk by name below is what is left for a provider that streams no ids at
// all, and it is a convention rather than a fact — which is why it is second.
//
// It takes the list for [claimForming]'s reason: the room runs it too.
func claimFormed(es []entry, ev session.Event) int {
	loose := -1
	for i := range es {
		e := &es[i]
		if !e.forming() {
			continue
		}
		if ev.CallID != "" && e.callID != "" {
			if e.callID == ev.CallID {
				return i
			}
			continue
		}
		if e.tool == ev.Tool {
			return i
		}
		// A row whose name never landed can only be matched by position, and it
		// is the LAST resort: a named row for this tool outranks it wherever
		// one exists.
		if e.tool == "" && loose < 0 {
			loose = i
		}
	}
	return loose
}

// announceTool draws the row for a call the model has finished asking for
// (session.EventToolAnnounced). Nothing has started, so the row is queued: a
// dim ◌, no spinner, and — for an edit or a write — the change it is ABOUT to
// make, previewed underneath from the arguments (toolview.go).
//
// IT ADOPTS THE FORMING ROW rather than drawing a second one: the row the
// person has been watching fill in is this call, and the announcement is that
// row's next state — the pulse stops, the arguments arrive, the ink comes up.
// One call, one line, from the first fragment to the last.
//
// A row is only ever announced once, but a surface that attached mid-batch may
// see a begin with no announcement and must not draw a second line for it, so
// the pairing rule lives in [feed.claimAnnounced] and both events use it.
func (f *feed) announceTool(ev session.Event) {
	if at := claimFormed(f.entries, ev); at >= 0 {
		e := &f.entries[at]
		e.status = toolQueued
		// THE ID IS KEPT AT THE ONE MOMENT IT IS OFFERED. A row that formed
		// before the wire said an id has none, and the announcement is where
		// session first names the call (its loop.go) — dropping it here left two
		// concurrent calls of one name with nothing to tell them apart, so
		// whichever finished first took the other's "took 4s".
		e.callID = firstNonEmpty(ev.CallID, e.callID)
		e.tool = firstNonEmpty(ev.Tool, e.tool)
		e.text = firstNonEmpty(ev.Hint, e.text)
		e.detail.Args = firstNonEmpty(ev.Args, e.detail.Args)
		// The streamed tail is let go here: the whole payload has landed, so the
		// preview under this row is now drawn from the arguments, and holding the
		// last eight kilobytes of every file the session ever wrote would be the
		// transcript keeping a copy nothing reads.
		e.formed = ""
		f.follow()
		f.touch()
		return
	}
	f.closeLive()
	f.entries = append(f.entries, entry{
		kind: entryTool, tool: ev.Tool, text: ev.Hint, turn: f.turn,
		status: toolQueued, detail: toolDetail{Args: ev.Args},
	})
	f.follow()
	f.touch()
}

// beginTool is EXECUTION STARTED. It adopts the row the announcement drew —
// the same call, one line, which is the whole point of announcing it — and
// starts that row's clock. A begin nobody announced draws its own row, which is
// every provider that does not stream tool calls and every surface that
// attached late.
func (f *feed) beginTool(ev session.Event) {
	at := f.claimAnnounced(ev)
	if at < 0 {
		// A call that formed and then began with no announcement between them.
		// The ordering law says that cannot happen, and a row left pulsing at a
		// call that is already running would be the surface believing the law
		// over the event in its hand.
		at = claimFormed(f.entries, ev)
	}
	if at >= 0 {
		e := &f.entries[at]
		e.status = toolRunning
		e.began = f.now()
		// The id is taken here for [feed.announceTool]'s reason — this is the
		// other door a forming row leaves by, and a surface that attached
		// mid-batch sees this event and never the announcement.
		e.callID = firstNonEmpty(ev.CallID, e.callID)
		e.detail.Args = firstNonEmpty(ev.Args, e.detail.Args)
		e.text = firstNonEmpty(ev.Hint, e.text)
		// Let the streamed tail go, for [feed.announceTool]'s reason — this is the
		// other door a forming row leaves by.
		e.formed = ""
		f.follow()
		f.touch()
		return
	}
	f.closeLive()
	f.entries = append(f.entries, entry{
		kind: entryTool, tool: ev.Tool, text: ev.Hint, turn: f.turn,
		status: toolRunning, began: f.now(), detail: toolDetail{Args: ev.Args},
		// AND THE ROW MINTED HERE TAKES THE ID TOO. Every other door onto a tool
		// row records it and this one did not, which left the rows drawn for a
		// provider that does not stream its calls — and for a surface that
		// attached mid-batch — as the only rows in the conversation with no
		// identity. Anything that pairs by id then cannot find them: the end and
		// the figure fall back to matching by tool name, and a caption, which has
		// only the id to go on, is dropped outright ([feed.nameStep]). It is the
		// same string the branch above adopts, from the same field.
		callID: ev.CallID,
	})
	f.follow()
	f.touch()
}

// claimAnnounced finds the queued row this begin belongs to, or -1.
//
// The payload is matched FIRST and the tool name only after: a batch of three
// edits to three files announces three rows, and pairing by name alone would
// start the clock on whichever of them was drawn first. Identical arguments are
// the one case where the two rules disagree and it does not matter — two calls
// with the same name and the same payload are the same work, in either order.
func (f *feed) claimAnnounced(ev session.Event) int {
	fallback := -1
	for i := range f.entries {
		e := &f.entries[i]
		if e.kind != entryTool || e.status != toolQueued || e.tool != ev.Tool {
			continue
		}
		if ev.Args != "" && e.detail.Args == ev.Args {
			return i
		}
		if fallback < 0 {
			fallback = i
		}
	}
	return fallback
}

// claimRunning finds the live row an END belongs to, or -1.
//
// ARGS MATCH FIRST, THEN OLDEST-OF-THAT-NAME. The payload is a PREFERENCE and
// not a key: session renders a call's arguments for the end event and a room
// renders them again off the node's journal, which agree for an ordinary call
// and can differ on one long enough to be clipped — so the walk by name is what
// is left, exactly as it is for an announcement ([feed.claimAnnounced]).
//
// It is the law on BOTH surfaces as of the lens design's ruling 3
// (docs/design/lens/DESIGN.md). The chat took the oldest row of that name and
// nothing else, which is right by convention and wrong half the time the
// convention does not hold: two `bash` calls running at once resolve in whatever
// order they finish, and the room already had to solve this because its page can
// hold one row off the journal and one off the live lane at the same time.
func claimRunning(es []entry, ev session.Event) int {
	fallback := -1
	for i := range es {
		e := &es[i]
		if e.kind != entryTool || !e.status.live() || e.tool != ev.Tool {
			continue
		}
		if ev.Args != "" && e.detail.Args == ev.Args {
			return i
		}
		if fallback < 0 {
			fallback = i
		}
	}
	return fallback
}

// closeTool resolves the live line this result belongs to ([claimRunning] says
// which one, and why).
//
// The end event carries the call's Args as well as its Output — session sends
// a self-contained end — so both are taken from it here rather than kept from
// the begin: a row rebuilt from one event is a row that cannot disagree with
// itself. The failure text is a fallback for the Output, because a tool that
// failed before it ran has a reason and no result.
func (f *feed) closeTool(ev session.Event, status toolState, why string) {
	// AND THE VIEW GETS THE RESULT BEFORE ANY ROW IS CLAIMED. A PROPOSE_TASK
	// RESULT WITH A FORMING CARD IS A REFUSAL — a proposal that landed has
	// already replaced that block with its question, so the presence of the card
	// is the one fact that tells the two outcomes apart — and a card is a thing
	// only the chat has, so the reducer hands the result over and the view knows
	// what it means ([feedHooks.closing]).
	if f.hooks.closing != nil {
		f.hooks.closing(ev)
	}
	if at := claimRunning(f.entries, ev); at >= 0 {
		e := &f.entries[at]
		e.status = status
		e.ended = f.now()
		e.detail.Args = firstNonEmpty(ev.Args, e.detail.Args)
		e.detail.Output = firstNonEmpty(ev.Output, why)
		// AND THE VIEW IS HANDED THE ROW THIS RESULT RESOLVED, once, here: what
		// a closed call means to the page around it — a bash that turned into a
		// job, the ambient counts that are sums over finished calls — is the
		// view's arithmetic and not the transcript's ([feedHooks.closed]).
		if f.hooks.closed != nil {
			f.hooks.closed(e, ev)
			// AND THE ROW IS FOUND AGAIN AFTERWARDS. The hook is handed a pointer
			// into a slice it is forbidden to grow ([feedHooks.closed] states the
			// rule), and this is the belt beside that brace: an append anywhere
			// under the hook would move the list out from under the writes below,
			// and A FAILURE OPENS ITSELF is not a law worth losing silently.
			e = &f.entries[at]
		}
		if why != "" && status == toolFailed {
			e.text = strings.TrimSpace(e.text + " — " + why)
		}
		// A FAILURE OPENS ITSELF. Everything else on this surface waits to be
		// asked, because a quiet line is a success and success has nothing to
		// read; a call that failed is the one row whose detail is the reason the
		// person is looking at the screen, and making them click for it is
		// making them click for the only thing that happened.
		if status == toolFailed {
			e.open = true
		}
		f.follow()
		f.touch()
		return
	}
	// A close with no open line still deserves to be seen rather than
	// silently dropped: the session said something happened.
	if status == toolFailed {
		f.entries = append(f.entries, entry{
			kind: entryTool, tool: ev.Tool, text: why, turn: f.turn, status: toolFailed,
			open:   true,
			detail: toolDetail{Args: ev.Args, Output: firstNonEmpty(ev.Output, why)},
		})
		f.follow()
		f.touch()
	}
}

// finishTool stops one row's clock at ITS OWN finish and writes what the call
// took (session.EventToolFinished).
//
// It closes nothing. The row keeps its spinner and its live status until the
// result arrives with the batch, because until then the surface genuinely does
// not know whether the call succeeded — what it knows, and what this writes, is
// that this call is no longer the reason anybody is waiting.
//
// The pairing is [feed.claimAnnounced]'s rule, one state later: the payload
// first and the tool name only after, because a batch of three bash calls
// finishing in any order pairs by name alone onto whichever row was drawn
// first, and the two identical calls where the rules disagree are the same work
// either way. A row that already has its figure is never taken twice.
func (f *feed) finishTool(ev session.Event) {
	fallback := -1
	for i := range f.entries {
		e := &f.entries[i]
		if e.kind != entryTool || !e.status.live() || e.ran > 0 || e.tool != ev.Tool {
			continue
		}
		if ev.CallID != "" && e.callID != "" {
			if e.callID == ev.CallID {
				fallback = i
				break
			}
			continue
		}
		if ev.Args != "" && e.detail.Args == ev.Args {
			fallback = i
			break
		}
		if fallback < 0 {
			fallback = i
		}
	}
	if fallback < 0 {
		return
	}
	e := &f.entries[fallback]
	if ev.Took > 0 && e.ran == 0 {
		e.ran = ev.Took
	}
	f.touch()
}

// settleCompaction stops the compaction row's clock: the LAST one still running
// takes the finished hint and the end time, and turns into the rule.
//
// Last rather than first, which is what [feed.closeTool] does and for the
// opposite reason: tool calls overlap and resolve in any order, while a session
// compacts one pass at a time (session holds a compacting flag across it), so
// the only unfinished row there can be is the newest one — and walking backwards
// finds it without reading the whole conversation.
//
// A settle with NO row to settle is not an error and is not dropped: a resumed
// session replays a transcript that already contains passes nobody watched, and
// an older session predates the start event entirely. Those get a row born
// finished — the divider they always drew, with no duration claimed, because a
// pass this surface did not see the start of has no honest elapsed time.
func (f *feed) settleCompaction(text string) {
	for i := len(f.entries) - 1; i >= 0; i-- {
		e := &f.entries[i]
		if e.kind != entryCompact || !e.ended.IsZero() {
			continue
		}
		e.text, e.ended = text, f.now()
		e.stale = true
		// AND THE PAGE IS TOLD, HERE. Ingest is the whole of what an event does
		// (see [feed.ingest]), so a settle that left the repaint to its caller was
		// a divider that painted in the chat — where the pump happened to touch
		// afterwards for its own reasons — and did not paint anywhere else.
		f.touch()
		return
	}
	now := f.now()
	f.entries = append(f.entries, entry{
		kind: entryCompact, text: text, turn: f.turn, began: now, ended: now,
	})
	f.follow()
	f.touch()
}

// resolveUnfinished stops the clock on every call that was still in the air when
// the turn ended. It is [app.dropForming] widened by two states, and it is what
// a node's page runs when its lane closes: a lane that has ended is the last
// word there will ever be about the calls on it — no announcement, no begin, no
// end is coming — so a row left in a live state is a page animating work that is
// over.
//
// THE DEFECT IT CLOSES IS A ROW THAT COMES BACK TO LIFE. A row that never got
// its end — an interrupt between the begin and the result, a retry that threw
// away the attempt those announcements belonged to, a stream that died — kept
// `toolRunning` with no end stamped on it. That looked settled for as long as
// the surface was idle, because both the spinner and the age are drawn only
// while the session is working (toolview.go's [app.mark] and [app.countClock]).
// Then the NEXT turn started, the session was working again, and the abandoned
// row began spinning a second time — with an age measured from a beginning
// minutes or hours earlier. "view_image always seems to be running" is that row.
//
// The end stamp is what makes it permanent: both of those renderers stop at a
// row that has an end on it, whatever the session is doing afterwards.
//
// Every row is RESOLVED, never removed, and the STATUS IS LEFT ALONE: the call
// was asked for, which is a fact about what happened, and a row that vanished
// would take that fact with it — while "failed" would be a claim about
// something nobody watched.
func (f *feed) resolveUnfinished() {
	now := f.now()
	for i := range f.entries {
		e := &f.entries[i]
		if e.kind != entryTool || !e.ended.IsZero() {
			continue
		}
		if e.forming() || e.status.live() {
			e.ended = now
			e.stale = true
		}
	}
}

// closeLive ends the assistant block being streamed into. A block nobody is
// writing any more is a finished document, so it renders as one.
//
// THE STALE FLAG IS THE WHOLE OF THE SETTLE, and it is load-bearing rather than
// tidy. [app.entryRows] hands back the rows it built last time unless something
// says otherwise, and a settled entry is not one of the shapes that bypass the
// cache — so without marking it here the block would keep the rows it was drawn
// with mid-stream: unrendered markdown, and the live ink of render.go's growing
// edge left bright on an answer that finished minutes ago. Setting both in one
// statement is deliberate: the two facts are one event.
func (f *feed) closeLive() { leaveLive(f.entries, &f.live) }

// settleThought collapses the streaming block WITHOUT letting go of it. It is
// what a text delta does to the reasoning above it: the block folds to its one
// row the moment the answer starts, but the turn is not done thinking just
// because it has started talking — some providers put reasoning and answer on
// the wire INTERLEAVED, a few tokens of each at a time, and a surface that
// treated every one of those hand-offs as a new phase sawed a single sentence
// into a stack of two-token blocks with `thought for 0s` rows between them,
// splitting words in half ("thre" / "ad gets saved"). So the pointer is kept:
// the next reasoning delta grows THIS block's count on its settled row, and the
// answer below streams on unbroken. Only a real boundary — a tool call, the
// turn settling — seals the block ([feed.collapseThought]) so that a genuinely
// new stretch of thinking gets a row of its own.
func (f *feed) settleThought() {
	if f.think < 0 || f.think >= len(f.entries) || f.entries[f.think].kind != entryThinking {
		return
	}
	e := &f.entries[f.think]
	if !e.settled {
		settleBlock(e)
		if !e.latched {
			e.open = false
		}
		f.touch()
	}
}

// collapseThought closes the streaming block. It is called by the event pump for
// the turn's first non-reasoning event, and again when the turn settles — a turn
// that streamed nothing else still has to leave a closed block behind.
//
// IT DOES NOT CLOSE A BLOCK THE PERSON OPENED. That is the whole of the latch
// (see [entry.latched]): the automatic collapse is this surface's opinion about
// a block nobody has said anything about, and it stops being anybody's opinion
// the moment somebody presses ctrl+e. A block opened mid-stream stays open
// through the settle, through every later delta of the turn, and until the same
// person closes it again.
func (f *feed) collapseThought() {
	if f.think < 0 {
		return
	}
	if e := leaveLive(f.entries, &f.think); e != nil && e.kind == entryThinking && !e.latched {
		e.open = false
	}
	f.touch()
}

// ── WHAT THE MODEL SAYS ─────────────────────────────────────────────────────

// say grows the assistant block the turn is streaming into, opening one when the
// last thing on the page was anything else.
//
// IT DOES NOT ASK FOR A REPAINT. Deltas are the flood and the frame clock is
// what turns a flood into a frame ([app.paint]); what this marks is the block
// stale and the view following, and the surface decides when to draw. A page
// whose row cache is keyed on one dirty flag says so in its own [feedHooks.follow]
// and gets its repaint that way (room.go).
//
// A DELTA THAT ARRIVES AFTER ITS TURN'S BOUNDARY IS APPENDED TO WHAT IT BELONGS
// TO, AND LANDS SETTLED (#225, [feed.settledTurn]). Between a turn's two endings
// a stream can still speak — the tail of a reply the provider had already
// buffered, a straggler behind a stop — and the obvious thing to do with those
// words opened a SECOND block under the answer. That was two defects in one
// line: the new block was live with no boundary left to settle it, so it drew
// its markdown raw until the next question closed it; and its presence demoted
// the answer above it into narration, which is drawn plain (hierarchy.go). The
// words belonged to the paragraph above them the whole time, which is [feed.said]'s
// law read from the other end — a page is the only record anybody reads back.
func (f *feed) say(text string) { f.sayStream(text, false) }

// sayStream is [feed.say] told whether the burst is a lump the live edge should
// walk. The bare form is text handed over WHOLE, with no wire behind it that
// could have lumped it, so it paces nothing.
func (f *feed) sayStream(text string, lump bool) {
	if text == "" {
		return
	}
	// Turn numbers count from one, so the zero this field holds before the
	// first turn ends cannot match the turn a delta belongs to.
	if late := f.settledTurn > 0 && f.turn == f.settledTurn; late {
		f.growSettledAnswer(text)
		return
	}
	if f.live < 0 || f.live >= len(f.entries) || f.entries[f.live].kind != entryAssistant {
		f.entries = append(f.entries, entry{kind: entryAssistant, turn: f.turn,
			replyTags: append([]session.TaskReplyTag(nil), f.pendingReplyTags...)})
		f.pendingReplyTags = nil
		f.live = len(f.entries) - 1
		f.mdAt = f.now()
	}
	e := &f.entries[f.live]
	e.text += text
	// AND THE LIVE EDGE OPENS OR EXTENDS HERE, on the bytes that were just
	// appended and nowhere else (reveal.go). It is in the reducer rather than in
	// a view because both surfaces stream into these blocks and a second spelling
	// would be a second answer to "how much of this is drawn".
	e.catchReveal(len(text), lump, f.snap())
	e.stale = true
	f.follow()
}

// growSettledAnswer is where a late delta goes: onto the LAST assistant block of
// the turn that has already ended, still settled.
//
// NOTHING IS LEFT LIVE, which is the whole point — [feed.live] stays -1, so the
// next boundary has nothing to find and the next question settles nothing that
// was not already settled. The block is marked stale because its text changed
// and [app.entryRows] hands back what it drew last time until something says
// otherwise ([feed.closeLive] states that law).
//
// WHICH BLOCK IT IS, IS THE CLASSIFIER'S OWN QUESTION ASKED BACKWARDS. The walk
// steps over exactly what [workEntry] steps over — a note, a divider, a
// withdrawn correction — because those are the lines the SURFACE wrote at the
// boundary and not work the model did: the two lines a turn ends with (what it
// changed, what it cost) sit under the reply on purpose, and a walk that stopped
// on them would append a second block under the answer and demote it, which is
// the defect this exists to close. Anything else — a tool row, a card — stops
// the walk: words after a call belong after the call, and gluing them onto the
// narration in front of it would put them in the wrong place on the page.
//
// A turn whose tail is not an answer — a call that failed, a stopped turn that
// never spoke — gets a block of its own, settled on arrival: the words did
// happen, and the alternative is a surface quietly dropping something a person
// watched arrive.
func (f *feed) growSettledAnswer(text string) {
	for i := len(f.entries) - 1; i >= 0; i-- {
		e := &f.entries[i]
		if e.turn != f.turn {
			break
		}
		if e.kind == entryNote || e.kind == entryDivider || entryWithdrawn(e) {
			continue
		}
		if e.kind != entryAssistant {
			break
		}
		e.text += text
		settleBlock(e)
		f.follow()
		f.touch()
		return
	}
	f.entries = append(f.entries, entry{kind: entryAssistant, turn: f.turn, text: text,
		settled: true, stale: true,
		replyTags: append([]session.TaskReplyTag(nil), f.pendingReplyTags...)})
	f.pendingReplyTags = nil
	f.follow()
	f.touch()
}

// reason grows the turn's reasoning block, opening one on the first delta.
//
// IT IS CALLED `reason` AND NOT `think` because [feed.think] is the index of the
// block it grows, and one name cannot be both. The two are the same story from
// the two ends the code needs it from: the field is where the block is, this is
// what puts words in it.
//
// Like a text delta it does NOT ask for a repaint per chunk — it marks the block
// stale and lets the frame clock decide when a flood becomes a frame. The one
// exception is the block's first delta, which appends an ENTRY: a structural
// change the layout has to see.
func (f *feed) reason(text string) { f.reasonStream(text, false) }

// reasonStream is [feed.reason] on [feed.sayStream]'s terms.
func (f *feed) reasonStream(text string, lump bool) {
	if text == "" {
		return
	}
	if f.think < 0 || f.think >= len(f.entries) || f.entries[f.think].kind != entryThinking {
		// The reply in progress is closed first, so the block lands above the
		// answer rather than splitting a paragraph that is still being written.
		f.closeLive()
		now := f.now()
		f.entries = append(f.entries, entry{
			kind: entryThinking, turn: f.turn, began: now, ended: now,
			// A BLOCK OPENED AFTER ITS TURN'S BOUNDARY IS BORN SETTLED (#225,
			// [feed.settledTurn]). The boundary that would have closed it has
			// already gone by, and a thought block left open would stay expanded
			// over the next turn — the very thing [feed.collapseThought] runs at
			// the settle to prevent.
			settled: f.settledTurn > 0 && f.turn == f.settledTurn,
		})
		f.think = len(f.entries) - 1
		f.follow()
		f.touch()
	}
	e := &f.entries[f.think]
	e.text += text
	// The reasoning block walks its edge on the conversation's own terms
	// ([feed.say] says why this lives in the reducer).
	e.catchReveal(len(text), lump, f.snap())
	e.ended = f.now()
	e.stale = true
	f.follow()
}

// takeReplyTags labels the answer with the task reports it is written from
// (render.go's reply-tag row: cause above consequence).
//
// TAGS CAN ARRIVE BEFORE THE FIRST WORD THEY LABEL, which is why they are held
// rather than dropped: the engine names the reports the turn is about to answer
// from as it picks them up, and the block that carries them may not exist yet.
// [feed.say] empties the held list onto the block it opens.
func (f *feed) takeReplyTags(tags []session.TaskReplyTag) {
	f.pendingReplyTags = append(f.pendingReplyTags, tags...)
	if f.live < 0 || f.live >= len(f.entries) || f.entries[f.live].kind != entryAssistant {
		return
	}
	e := &f.entries[f.live]
	e.replyTags = append(e.replyTags, f.pendingReplyTags...)
	e.stale = true
	f.pendingReplyTags = nil
}

// ── WHAT THE SURFACE ITSELF SAYS ────────────────────────────────────────────

// said puts one of the PERSON'S OWN lines — or one of the surface's — into the
// transcript without cutting the answer that is still streaming in two.
//
// THE DEFECT IT FIXES. A message sent while a reply was streaming went in the
// obvious way — close the live block, append the line — and the very next delta
// found no live block and opened a second one under it. What the reader saw was
// one flowing answer with somebody else's sentence wedged between two of its
// paragraphs, as though the model had quoted them mid-thought. The words were in
// the right place in TIME and in the wrong place on the PAGE, and the page is
// the only record anybody reads back.
//
// SO THE STREAMED BLOCK STAYS WHOLE. The line is appended after it and the live
// index is left where it was, which is still valid — appending never moves an
// earlier entry — so the next delta grows the block it was already growing and
// the person's line stays below it. A tool row is deliberately NOT treated this
// way: a call lands in place, between two paragraphs, because that is where it
// happened and the reply is written around it.
func (f *feed) said(e entry) {
	live := f.live
	f.entries = append(f.entries, e)
	if live < 0 || live >= len(f.entries)-1 || f.entries[live].kind != entryAssistant {
		// The pointer named nothing that is still growing, so it is let go
		// through the one door rather than by hand (livestate.go).
		abandonLive(f.entries, &f.live)
		return
	}
	f.live = live
}

// note appends a surface-side line — a nudge, a notice, a slash command's
// answer, an error. It is never sent anywhere.
//
// THE SAME SENTENCE TWICE RUNNING IS ONE SENTENCE. Half the lines in this lane
// are the surface answering an act a person repeats while they work out what to
// do next: /files on a machine that has made nothing answers [filesNothingWord]
// every single time, /subharness on a build with none answers [subNothingWord],
// and a refusal answers whatever it refused.
// Four presses used to leave four identical lines stacked in the transcript,
// which is the emptiness law's own complaint said about repetition — the screen
// counting how many times it had nothing to report. So a note whose words are
// already the last thing in the transcript is not written again; it is brought
// back into view, which is the whole of what the person was going to read.
//
// IT ASKS ABOUT THE LAST ENTRY AND NEVER ABOUT THE WHOLE TRANSCRIPT. Anything at
// all landing in between — an answer, a tool call, another note — puts the
// repeat in a new place, where it is news again: "nothing stands here yet" under
// the reply that just talked about standing orders is a different sentence from
// the one four lines up, and a transcript that swallowed it would be answering a
// deliberate command with silence.
func (f *feed) note(text string) { f.noteWritten(text, false, nil) }

// noteWritten is the one body behind [feed.note] and the chat's two richer doors
// ([app.noteFacts], [app.noteBlock]), so the repeat rule, the fact list and the
// block flag cannot disagree about what a note is.
//
// A NOTE DOES NOT CUT THE REPLY IN TWO (#225). It used to close the live block,
// so a line the surface wrote in the middle of a streaming answer — a notice
// about a reshaped request, a nudge — sent the very next delta into a SECOND
// assistant block. The reader then got a reply in two halves with the first one
// demoted into narration and drawn plain (hierarchy.go, workfold.go's
// [workEntry]). It is exactly the shape [feed.said] already closes for the
// person's own line, and it takes the same door: the note lands after the block
// and the block goes on growing.
func (f *feed) noteWritten(text string, block bool, facts []string) {
	if n := len(f.entries); n > 0 && f.entries[n-1].kind == entryNote && f.entries[n-1].text == text {
		// The repeat is brought back into view rather than written again (above),
		// and its data are refreshed with it: the same sentence built a second time
		// may have been built from a different reading, and a stale fact list would
		// lift the words of the frame before this one.
		f.entries[n-1].facts, f.entries[n-1].block = facts, block
		f.follow()
		f.touch()
		return
	}
	f.said(entry{kind: entryNote, text: text, turn: f.turn, facts: facts, block: block})
	f.follow()
	f.touch()
}

// ── AN ATTEMPT THAT NEVER HAPPENED ──────────────────────────────────────────

// retry is a cut request being asked again (internal/provider's streamguard.go).
//
// EVERYTHING THE DEAD ATTEMPT DREW GOES, because the engine has already thrown
// away everything the dead attempt SAID: the text belongs to a response that
// will never exist, and half a dead answer sitting above the live one is the
// surface telling a story the transcript does not contain.
//
// It is one method rather than the four calls it used to be at each pump,
// because a room that had three of the four would be a room drawing an attempt
// that never ran — which is exactly what a room did, by having none of them.
func (f *feed) retry(ev session.Event) {
	f.dropLive()
	f.dropRetryingFormingTools()
	f.resolveUnfinished()
	// A PARTIAL PROPOSAL BELONGS TO THE DEAD ATTEMPT TOO, and it is the view's
	// because a card is ([feedHooks.retrying], which states why it fires here and
	// not after the note).
	if f.hooks.retrying != nil {
		f.hooks.retrying()
	}
	f.note(ev.Text)
}

// dropLive throws away the assistant block the CURRENT attempt was streaming
// into, because that attempt has been cut and its text is void.
//
// It is the one place on this surface where something a person watched arrive is
// REMOVED rather than settled, and the asymmetry is the point: an interrupt
// leaves the partial reply on screen because the engine keeps it in the
// transcript, while a cut stream leaves nothing anywhere. A row the transcript
// does not contain must not stay on the page — the next question would be
// answered underneath somebody else's abandoned sentence, and the person would
// have no way of telling which of the two the model actually read.
//
// The block is truncated when it is the last thing on screen, which is what a
// cut mid-text always leaves, and emptied otherwise: removing an entry from the
// middle would move every index after it, and the forming rows, the selection
// and the thought marker are all held by index.
func (f *feed) dropLive() {
	at := f.live
	if e := abandonLive(f.entries, &f.live); e == nil || e.kind != entryAssistant {
		return
	}
	if at == len(f.entries)-1 {
		f.entries = f.entries[:at]
	} else {
		f.entries[at].text = ""
		f.entries[at].stale = true
	}
	f.touch()
}

// dropRetryingFormingTools removes calls that were still being spelled when a
// provider request was cut. The session discards those partial calls rather
// than recording them, so settling their rows as cancelled would leave a call
// on screen that never existed in the transcript.
//
// Forming rows are normally the newest entries. The empty assistant fallback is
// the same one [feed.dropLive] uses when a later row holds an index in place.
func (f *feed) dropRetryingFormingTools() {
	for i := len(f.entries) - 1; i >= 0; i-- {
		e := &f.entries[i]
		if e.turn != f.turn {
			break
		}
		if e.kind != entryTool || e.status != toolForming {
			continue
		}
		if i == len(f.entries)-1 {
			f.entries = f.entries[:i]
			continue
		}
		f.entries[i] = entry{kind: entryAssistant, turn: f.turn, stale: true}
	}
	f.touch()
}
