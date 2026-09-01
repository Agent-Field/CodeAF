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
// rest of the eleven in room.go — and being the copy is exactly why a room
// still does not learn that a tool FINISHED, and why a retry inside a task is
// invisible: every event kind added since had to be wired twice, and the second
// wiring is the one nobody remembers. So a surface that needs the reducer to do
// something extra installs a [feedHooks] function; it does not write a second
// reducer, which agrees with this one on the day it is written and drifts from
// it by the next wave.
//
// IT IS EMBEDDED IN [app] RATHER THAN HELD BESIDE IT, which is what keeps the
// several hundred existing readers of `a.entries`, `a.live`, `a.think` and
// `a.turn` spelled the way they have always been spelled: the fields moved
// house, not name. The room adopts the same reducer in the lane that deletes
// its mirrors (docs/design/lens/DESIGN.md, Decision 1).
type feed struct {
	entries []entry
	// live is the assistant entry currently being streamed into, or -1.
	live int
	// think is the reasoning block currently streaming, or -1 (thinking.go).
	think int
	// turn counts the person's messages. It groups tool calls into clusters
	// and is what ctrl+o folds and unfolds.
	turn int
	// hooks is what this view asked the reducer to do on its behalf, and is the
	// only thing in here that differs between one surface and another.
	hooks feedHooks
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
	closed func(e *entry, ev session.Event)
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
func (f *feed) ingest(ev session.Event) {
	switch ev.Kind {
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

// closeTool marks the oldest still-running line for that tool. Oldest rather
// than newest because tools run in parallel and finish in any order, and the
// first one begun is the first one a person watching the column expects to
// resolve.
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
	for i := range f.entries {
		e := &f.entries[i]
		if e.kind != entryTool || !e.status.live() || e.tool != ev.Tool {
			continue
		}
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
		return
	}
	now := f.now()
	f.entries = append(f.entries, entry{
		kind: entryCompact, text: text, turn: f.turn, began: now, ended: now,
	})
}

// resolveUnfinished stops the clock on every call that was still in the air when
// the turn ended. It is [app.dropForming] widened by two states, and it is the
// law room.go still keeps a hand-copy of at a node's lane close
// ([app.roomResolveUnfinished]) — one of the eleven this reducer exists to
// replace.
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
// Every row is RESOLVED, never removed, and the STATUS IS LEFT ALONE, for
// [app.roomResolveUnfinished]'s reasons exactly: the call was asked for, which
// is a fact about what happened, and "failed" would be a claim about something
// nobody watched.
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
func (f *feed) closeLive() {
	if f.live >= 0 && f.live < len(f.entries) {
		e := &f.entries[f.live]
		e.settled, e.stale = true, true
	}
	f.live = -1
}

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
		e.settled, e.stale = true, true
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
	if f.think < len(f.entries) && f.entries[f.think].kind == entryThinking {
		e := &f.entries[f.think]
		e.settled, e.stale = true, true
		if !e.latched {
			e.open = false
		}
	}
	f.think = -1
	f.touch()
}
