package session

// The session's name.
//
// A conversation needs one word-sized handle — for the picker that lists
// sessions, for a window title, for a person deciding which of yesterday's
// three sessions to resume. Nobody wants to type it, and the first exchange
// already says what the session is about, so the session names itself once and
// then never again.
//
// Three properties are the whole design:
//
//   - ONE NAMING, ONCE, AND IT STARTS ON THE FIRST MESSAGE. It fires the moment
//     the person's first message is ACCEPTED by a session that has no name yet —
//     beside the answer, not behind it — and a session resumed from a journal
//     already has its name, the title line being read back at open. A namer that
//     ran every turn would be a tax on every turn, and a name that changed under
//     the person would make the picker unreadable.
//
//     IT USED TO RUN AT THE END OF THE TURN, and that is the change: a name is
//     what the conversation is called in the picker, in the rail and in the tab
//     the person is looking at WHILE the answer is being written, so a name that
//     arrives after a ten-minute answer arrives after the only minutes it was
//     needed for. The first message is enough to name a session — it is what the
//     person came to ask — and the answer, when there is one by then, is added
//     to the prompt as it always was.
//
//   - AND IT IS NEVER SERIAL WITH THE ANSWER. The errand runs on its own
//     goroutine, on the SESSION'S lifetime and not the turn's ([Agent.titleCtx]),
//     and nothing in the turn ever waits on it. Both halves of that matter: a
//     turn's context would cancel a name the moment a quick answer finished, and
//     a turn that waited would be the very thing this change is about.
//
//   - THE CHEAP MODEL. The model is resolved through internal/roles as
//     RoleTitle, which puts it on the low tier by default: a wrong title costs a
//     glance and is rewritten by the next session, so it is the archetypal cheap
//     call. A person who has configured nothing gets the session's own model,
//     which is roles.Resolve's floor and not a failure.
//
//   - ONLY A JOURNALED SESSION NAMES ITSELF. A session with no file has
//     nowhere to keep a name and nothing to be listed in — the name exists for
//     the picker, which lists files — so an in-memory conversation (a headless
//     one-shot, a test) pays for nothing. That is also why the gate reads "the
//     session file has no title yet": a resume already has its name.
//
//   - A NAME THAT IS NOT A NAME IS REFUSED, and the session stays unnamed. The
//     models this lands on are the cheapest ones configured, and a small model
//     answering with the instruction it was given is an ordinary failure — one
//     that named a real session on this machine "name this session in ≤8 words,
//     lowercase, no quotes". [cleanTitle] throws that answer away, [healedTitle]
//     throws away the ones already written down, and the person's opening words
//     stand in meanwhile. A session left unnamed asks again the next time it is
//     opened, on its next message: titleTried is a fact about this process, and
//     the title read back off the file is now empty.
//
//   - NAMING IS BOUNDED, INCLUDING UNUSABLE ANSWERS. Empty replies and invalid
//     labels fall through the same model ladder as request failures. Up to
//     every ask shares one titleWindow; another turn never adds an errand.
//
//   - IT NEVER BREAKS THE TURN, AND NOW IT NEVER DELAYS ONE EITHER. A failed,
//     empty or cancelled title leaves the session unnamed and says nothing.
//     There is no event kind for "a small thing did not work", and inventing one
//     — or borrowing EventError, which means the turn ended badly — would report
//     a fault about work the person never asked for. Its spend is billed the
//     moment it lands, against the model that answered, on the session's ledger
//     and never inside the turn's own sealed figure ([Agent.addAuxiliaryUsageAs]
//     — the ledger has always taken these outside the turn total).
//
//   - AND THE NAME REACHES A SURFACE WHETHER OR NOT THE TURN IS STILL GOING.
//     The turn's hub carries it when one is running, which is where every
//     surface already reads it from; the standing lane ([Agent.WatchTitle])
//     carries it when the answer finished first, which is now the ordinary case
//     for a short question. A surface reading both draws the same name twice,
//     which is one idempotent assignment.

import (
	"context"
	"errors"
	"strings"
	"sync"
	"time"
	"unicode"

	"github.com/Agent-Field/agentfield/sdk/go/ai"
	"github.com/Agent-Field/codeaf/internal/roles"
)

// titleSystem is all the system message says, and it is deliberately not the
// instruction. A cheap model reads the system message as CHARACTER and the end
// of the user message as THE THING TO DO — so the instruction goes last, where
// it is read, and the system line only says who is being asked.
const titleSystem = "You name conversations."

// titlePrompt requests the one name shared by every conversation surface.
// Width is a presentation choice, not a second naming job.
const titlePrompt = "Name this conversation with a descriptive 5-8 word phrase, lowercase, no quotes. Answer with the name only."

var errInvalidName = errors.New("naming response contained no usable name")

const legacyTitlePrompt = "Name this session in ≤8 words, lowercase, no quotes. Answer with the name only."

// titleClip bounds each half of the opening exchange handed to the namer. A
// title is derived from what the session is ABOUT, and the first paragraph of
// the question and of the answer says that; sending a 300KB tool-assisted reply
// would pay for a whole context to produce one short title.
const titleClip = 2000

// titleLimit bounds the full name itself. Five to eight words asked for, 80 bytes accepted:
// the cap is a guard against a model that answers with a paragraph, not a
// second attempt at the instruction.
const titleLimit = 80

// A title is visible housekeeping. Its low-tier outer patience is shared with
// slower auxiliary work, so one naming ask states its own tighter worth. The
// two-minute parent still owns retries and cancellation across asks.
const titleAskWindow = 20 * time.Second

type conversationTitle struct{ full string }

// titleWindow bounds the WHOLE errand — every ask, every backoff and the waits
// inside them. The per-call bound is the role tier's ([roles.PatienceFor],
// auxiliary.go) and it is the right bound for one call; this is the bound on
// asking again, and without it an errand naming a conversation could outlive the
// conversation. Two minutes is past several asks on a healthy provider by a wide
// margin and far short of a person's patience with a session that has no name
// yet.
//
// AND IT IS THE WHOLE OF THE BOUND. There was a `titleAttempts = 3` beside it
// until 2026-09-11 — one of the six session-side ladders
// docs/design/recovery/DESIGN.md §7 names — and two bounds on one axis is the
// shape this wave deleted everywhere else: the count ended the errand early on a
// provider that was merely slow, and the window ended it anyway on one that was
// down. What is left is this deadline and the doubling wait under it, which is
// what makes a blip survivable and an outage noticed without anybody having to
// state how many of either.
const titleWindow = 2 * time.Minute

// startTitleLocked starts the session naming itself, if it has no name yet.
//
// It runs from [Agent.startTurnLocked] with a.mu held, immediately after the
// person's first message has been recorded — the gate and the mark are taken
// under that same hold, so two Submits racing cannot buy two names.
func (a *Agent) startTitleLocked() {
	if a.file == nil || a.titleTried || strings.TrimSpace(a.title) != "" || a.closed || a.titleCtx == nil {
		return
	}
	// AND A TASK NODE NAMES NOTHING. A name exists for the picker, which lists
	// CONVERSATIONS; a node is a step of one, drawn under the name the graph
	// gave it (taskname.go), and its journal is a record rather than a row
	// somebody chooses from.
	//
	// It used to be named anyway — harmlessly, because the naming ran at the end
	// of the turn, after everything the node had come to do. Starting it when
	// the node's brief is accepted put a second writer of the node's own
	// meta.json in the middle of its work, and a folder family measured the
	// result: [Agent.stampTitle]'s read-modify-write landed between the family's
	// own, and the parent's line came back as the line it had replaced.
	if a.config.InTask {
		return
	}
	question, answer := a.firstExchangeLocked()
	if question == "" {
		return
	}
	// The attempt is marked before the errand, not after it: this is ONE naming
	// per session, and a namer that started again on every later turn would be a
	// tax on every turn.
	a.titleTried = true
	ctx := a.titleCtx
	model := a.model
	a.titleJobs.Add(1)
	go func() {
		defer a.titleJobs.Done()
		a.nameSession(ctx, question, answer, model)
	}()
}

// maybeTitle is the same naming, asked for at the end of a turn.
//
// IT IS NOW A SECOND DOOR ONTO ONE ERRAND rather than the errand itself, and it
// is still here for the session whose first message was accepted before this
// gate could pass — a resume of an untitled journal whose reopening turn is a
// wake, a turn started with no message of its own at all. [Agent.startTitleLocked]
// refuses a session that is already naming itself, so the ordinary turn reaches
// this line and buys nothing.
func (a *Agent) maybeTitle(context.Context, *eventHub) {
	a.mu.Lock()
	a.startTitleLocked()
	a.mu.Unlock()
}

// nameSession is the errand: ask, and ask again if the wire was the reason
// there was no answer.
//
// It runs on the session's lifetime, on its own goroutine, and nothing waits on
// it. The window is taken here rather than by the caller because it bounds the
// LADDER — the caller's per-call bound is the role tier's, inside [Agent.callRole].
func (a *Agent) nameSession(ctx context.Context, question, answer, model string) {
	ctx, done := context.WithTimeout(ctx, titleWindow)
	defer done()
	// AND THE SAME WINDOW IS READ ON THE TURN'S OWN CLOCK, so that a scenario can
	// state two minutes without spending two of them (loop.go's [turnNow]). The
	// context above is what really cuts a request mid-flight; this is what ends
	// the LADDER, and they are the same figure.
	until := turnNow().Add(titleWindow)
	// THE LOOP WALKS THE WAIT AND NOT A COUNT. Each ask that comes to nothing
	// doubles what is paid before the next one, and the window above is what
	// ends the errand — so a provider having a bad second is waited out and one
	// that is down is given up on, without a number that had to guess which.
	for wait := time.Duration(0); ; wait = nextTitleWait(wait) {
		if wait > 0 {
			// AND THE WAIT IS CANCELLABLE. A close during a backoff is a
			// session that has left, and a sleep that ignored it would hold the
			// quit for the whole of the ladder.
			if err := titleBackoff(ctx, wait); err != nil {
				return
			}
		}
		// AND THE WINDOW IS READ AFTER THE WAIT, because what it bounds is the
		// ASKING: a wait that ran past the window has already answered the
		// question of whether there is time for another one.
		if ctx.Err() != nil || !turnNow().Before(until) {
			return
		}
		// AND NOTHING IS ASKED FOR A SESSION THAT ALREADY HAS ONE. Between two
		// attempts the session may have been named or closed, and a second ask
		// is then real money spent for an answer that [Agent.publishTitle] is
		// about to refuse.
		if !a.stillNeedsName() {
			return
		}
		title, again := a.askForName(ctx, question, answer, model)
		if title.full != "" {
			a.publishTitle(ctx, title)
			return
		}
		if !again {
			return
		}
	}
}

// titleBackoff is the naming ladder's own wait seam — see THE TURN'S OWN CLOCK
// in loop.go for why it is not the turn's.
var titleBackoff = backoffWait

// nextTitleWait is what to pay before asking again: [retryBaseDelay] first, then
// double, and never more than what is left of [titleWindow] could hold anyway.
// The ceiling is arithmetic rather than patience — an unbounded doubling becomes
// a negative duration, and the window above is the real bound.
func nextTitleWait(paid time.Duration) time.Duration {
	if paid <= 0 {
		return retryBaseDelay
	}
	if paid >= titleWindow {
		return titleWindow
	}
	return paid * 2
}

// stillNeedsName is the errand's own gate, asked before every attempt: is this
// session still unnamed, and still open?
func (a *Agent) stillNeedsName() bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	return !a.closed && strings.TrimSpace(a.title) == ""
}

// askForName makes one call and returns the name it earned, or asks to be
// called again.
//
// A failed request or an unusable reply may spend another bounded attempt.
func (a *Agent) askForName(ctx context.Context, question, answer, model string) (name conversationTitle, again bool) {
	// One errand, through the one door errands go through (auxiliary.go): the
	// role's tier bounds how long one title may take, and a model that cannot
	// answer at all costs one fall-through down the ladder rather than the
	// session's name. No tools — the namer's only job is to produce the title pair.
	callCtx, cancel := context.WithTimeout(ctx, titleAskWindow)
	response, named, err := a.callRoleChecked(callCtx, roles.RoleTitle, model,
		[]ai.Message{
			textMessage("system", titleSystem),
			// THE INSTRUCTION IS LAST, after the exchange rather than above it.
			// The models this call lands on are the cheapest ones a person has
			// configured, and a small model asked in the system message answers
			// the system message: a real session on this machine was named
			// "name this session in ≤8 words, lowercase, no quotes" by one of
			// them. [cleanTitle] refuses that answer whatever it costs to make,
			// but the cheaper fix is to ask in the place it reads.
			textMessage("user", titleAsk(question, answer)),
		}, func(response *ai.Response, named string) bool {
			if cleanConversationTitle(response.Text()).full != "" {
				return true
			}
			a.addDetachedUsageAs(response, named, 1, auxRoleTitle)
			return false
		})
	cancel()
	if err != nil {
		// A CANCELLED ERRAND IS NOT A FAILED ONE and is never asked again: the
		// session is closing, or the whole window is spent, and both of those
		// are answers rather than accidents.
		if ctx.Err() != nil {
			return conversationTitle{}, false
		}
		// A RUNG THAT RAN OUT OF PATIENCE IS THE WIRE, and the reader below
		// cannot see it. Every errand carries the bound of its role's TIER
		// (auxiliary.go), which is a deadline on the CALL and not on this
		// errand; when it fires with the errand's own window still open, what
		// happened is a small model that did not answer in the seconds eight
		// words are worth — the same "no answer came back" a torn socket is, and
		// worth the same second ask on a different endpoint. [isRetryable] reads
		// a provider's words and a context deadline has none of them, so it is
		// asked as the typed error it is.
		if errors.Is(err, context.DeadlineExceeded) {
			return conversationTitle{}, true
		}
		return conversationTitle{}, errors.Is(err, errInvalidName) || errors.Is(err, errEmptyAnswer) || isRetryable(err.Error())
	}
	if response == nil {
		return conversationTitle{}, true
	}
	// BILLED AGAINST THE MODEL THAT ANSWERED, which is not always the one the
	// ladder resolved first, AND OFF EVERY TURN'S CLOCK. The errand outlives the
	// turn that started it by construction now, so by the time this lands the
	// running turn is very often a different one — or an abandoned one, whose
	// journal line is written from exactly the figure this would have moved
	// ([Agent.addDetachedUsageAs], loop.go).
	a.addDetachedUsageAs(response, named, 1, auxRoleTitle)
	return cleanConversationTitle(response.Text()), false
}

// titleAsk is the user message the namer reads: the opening exchange, then the
// instruction.
//
// THE REPLY IS OPTIONAL NOW, and that is the whole of what starting early cost.
// A name is derived from what the session is ABOUT, which the person's own first
// message says on its own; the reply, on the rare road that still names a
// session after a turn ([Agent.maybeTitle]), is added when there is one. An
// empty "First reply:" heading would be a small model told the conversation had
// no answer in it, which is a different question from the one being asked.
func titleAsk(question, answer string) string {
	ask := "First message:\n" + clip(question, titleClip)
	if answer != "" {
		ask += "\n\nFirst reply:\n" + clip(answer, titleClip)
	}
	return ask + "\n\n" + titlePrompt
}

// publishTitle records the name and puts it in front of whoever is watching.
//
// A NAME ALREADY THERE WINS. Between the errand starting and this line the
// session may have been named by somebody with more authority than a cheap model
// — that is what [Agent.setTitleIfUnnamed] is asked, under the one lock, and a
// refusal here is silent because nothing went wrong: the session has a name.
func (a *Agent) publishTitle(ctx context.Context, title conversationTitle) {
	// A NAME THAT ARRIVED AFTER THE WINDOW OR AFTER THE QUIT IS NOT WRITTEN. A
	// provider that ignores a cancelled context still returns eventually, and a
	// journal line appended to a session that has closed is a write racing the
	// file's own close for a name nobody is waiting for.
	if ctx.Err() != nil {
		return
	}
	if !a.setTitleIfUnnamed(title.full) {
		return
	}
	// The title the handle is read off exists now (handlepick.go).
	a.chooseTeamHandlesLater(title.full, nil)
	event := Event{Kind: EventTitleChanged, Text: title.full}
	a.mu.Lock()
	hub := a.hub
	watchers := make([]*eventStream, len(a.titleWatchers))
	copy(watchers, a.titleWatchers)
	a.mu.Unlock()
	// THE TURN'S HUB WHEN THERE IS ONE, because that is where every surface
	// reading this conversation's turn already is, and the standing lane for the
	// case this change created: an answer that finished before the name did. A
	// surface on both roads assigns the same name twice.
	if hub != nil {
		hub.send(event)
	}
	for _, watcher := range watchers {
		watcher.send(event)
	}
}

// TitleChanges is the standing subscription to the name this session gives
// itself, for [Agent.HarnessDesigns]' reason: the name is now minted beside the
// turn rather than inside it, so its event very often has no turn stream left to
// arrive on. A surface subscribes once and holds it for the life of the session.
func (a *Agent) TitleChanges() <-chan Event {
	lane, _ := a.WatchTitle()
	return lane
}

// WatchTitle is [Agent.TitleChanges] with a way to stop, on
// [Agent.WatchHarnessDesigns]' terms: same subscription, stop takes the watcher
// off the list and ends its pump, never nil, and calling it twice is calling it
// once.
//
// A NAME ALREADY MINTED IS REPLAYED TO THE NEWCOMER, which is the ordinary case
// rather than a corner: a conversation left in the background is named while
// nobody is subscribed to it, and a surface that came back to a session with a
// name would otherwise draw the person's opening words under a session that has
// been properly named for an hour.
func (a *Agent) WatchTitle() (<-chan Event, func()) {
	stream := newEventStream()
	a.mu.Lock()
	if a.closed {
		a.mu.Unlock()
		stream.close()
		return stream.out, func() {}
	}
	a.titleWatchers = append(a.titleWatchers, stream)
	if title := strings.TrimSpace(a.title); title != "" {
		stream.send(Event{Kind: EventTitleChanged, Text: title})
	}
	a.mu.Unlock()
	var once sync.Once
	return stream.out, func() {
		once.Do(func() {
			a.mu.Lock()
			a.titleWatchers = dropWatcher(a.titleWatchers, stream)
			a.mu.Unlock()
			stream.leave()
		})
	}
}

// waitForTitle joins a naming errand that is mid-write, and gives up on one
// that is not.
//
// IT IS A JOIN AND NOT A CANCEL. Close has already cancelled [Agent.titleCtx]
// before it reaches this line, precisely so that nothing here can put an errand
// in front of the turn's own cancellation; what is left to do is give an errand
// that already has a name the moment it needs to append it.
//
// The wait is bounded twice over — [closeGrace], and the fact that the context
// under the errand is cancelled — and the goroutine it parks on the group is
// the wait itself rather than a watcher left behind: a provider that ignores
// its context keeps that one goroutine and nothing else, and every gate the
// errand still has to pass ([Agent.setTitleIfUnnamed], [Agent.publishTitle])
// refuses a closed session, so what it comes back to is a no-op.
func (a *Agent) waitForTitle() {
	settled := make(chan struct{})
	go func() {
		a.titleJobs.Wait()
		close(settled)
	}()
	timer := time.NewTimer(closeGrace)
	defer timer.Stop()
	select {
	case <-settled:
	case <-timer.C:
	}
}

// setTitleIfUnnamed records the name in the agent and in the journal, and
// answers whether it took.
//
// THE CHECK AND THE ASSIGNMENT ARE ONE ACT under a.mu, and that is the whole
// point of this door. The namer is now a goroutine running beside the turn, so
// between its gate and its answer a name may have arrived from somewhere with
// more authority — today the only other writer is a resume reading the journal's
// title line at open (agent.go), and a surface that lets a person rename a
// conversation would be the second. A read-then-write would let the cheap
// model's answer land on top of it, and the journal line and meta.json below
// would then be written from two names in whichever order the goroutines ran.
//
// false is a session that already has a name — or one that has closed, which is
// the same answer for the same reason: this is the last gate in front of a
// journal append, and a session that has left is not one anything may still be
// written to. Nothing is journaled, nothing is stamped, and no event is sent.
func (a *Agent) setTitleIfUnnamed(title string, _ ...string) bool {
	a.mu.Lock()
	if a.closed || strings.TrimSpace(a.title) != "" {
		a.mu.Unlock()
		return false
	}
	a.title = title
	file := a.file
	a.mu.Unlock()
	if file != nil {
		file.appendTitle(title, "")
	}
	// The folder's row says what the journal says. Until now it has carried the
	// person's opening words as a placeholder (placemeta.go); this is the name
	// the conversation actually earned.
	a.stampTitle(title)
	return true
}

// firstExchangeLocked returns the session's opening question and the first
// thing the assistant said back, both as plain text.
//
// It reads from the front of the transcript rather than from the turn that just
// ended, which matters for the session whose first turn is not its first
// message — a resume that was never titled, a session whose opening turn was
// interrupted. The name should describe what the conversation is about, and
// that is where it was stated.
func (a *Agent) firstExchangeLocked() (string, string) {
	question, answer := "", ""
	for _, message := range a.messages {
		switch message.Role {
		case "user":
			if question == "" {
				question = messageContentText(message)
			}
		case "assistant":
			if question != "" && answer == "" {
				answer = messageContentText(message)
			}
		}
		if question != "" && answer != "" {
			break
		}
	}
	return strings.TrimSpace(question), strings.TrimSpace(answer)
}

func messageContentText(message ai.Message) string {
	var text strings.Builder
	for _, part := range message.Content {
		if part.Type == "text" && part.Text != "" {
			if text.Len() > 0 {
				text.WriteString("\n")
			}
			text.WriteString(part.Text)
		}
	}
	return text.String()
}

func cleanConversationTitle(raw string) conversationTitle {
	var full string
	labeled := false
	for _, line := range strings.Split(raw, "\n") {
		line = stripMarkup(strings.TrimSpace(line))
		line = strings.Trim(line, `"'“”`)
		// The labels are how a paired answer is read, and a model that says
		// "Sure!" before one has still answered rather than changed the label.
		line = stripInterjection(line)
		lower := strings.ToLower(line)
		switch {
		case strings.HasPrefix(lower, "full:"):
			labeled = true
			full = cleanTitle(strings.TrimSpace(line[len("full:"):]))
		case strings.HasPrefix(lower, "tab:"):
			labeled = true
			// A legacy paired answer contributes only its full title.
		}
	}
	// Old providers and saved test fixtures answer one plain line. It remains a
	// valid full title, preserving compatibility with older providers.
	if full == "" && labeled {
		return conversationTitle{}
	}
	if full == "" {
		full = cleanTitle(raw)
	}
	if full == "" {
		return conversationTitle{}
	}
	return conversationTitle{full: full}
}

// cleanTitle takes the first line and strips the things a model adds against
// the instruction: the throat-clearing it opens with ("Title:", "Sure, here is
// the name:"), the MARKUP it emphasises with, surrounding quotes, a trailing
// full stop, and the separators of a name answered as a SLUG. Then it REFUSES an
// answer that is not a name at all — the instruction handed back, an opener with
// nothing behind it, or A CLAUSE ABOUT THE SPEAKER RATHER THAN A NAME FOR THE
// WORK ([opensAsPlan]). That last refusal is what keeps a namer that answered
// its plan from being shortened into a label: it runs HERE, before any caller
// cuts the answer to a few words, because the front of "I'll start by creating
// the four bakery landing pages" is not a name at any length.
//
// The slug is the one worth explaining. The instruction asks for words, and a
// model that has spent its life reading identifiers sometimes answers
// "porting_the_parser" — which is the right words welded into a filename.
// A name is read by a person, in a status line and in a list of yesterday's
// sessions, so the welding is undone at the moment the name is minted rather
// than at each of the places it is drawn.
//
// Only a ONE-TOKEN answer is touched. A title that already has a space in it is
// words, and a hyphen inside words is a hyphen somebody meant ("port-b failures"
// keeps it).
//
// A REFUSAL IS THE EMPTY STRING, and every caller already knows what to do with
// it: the session stays unnamed ([Agent.maybeTitle] returns), the task namer
// leaves the row's title where it was ([cleanTaskName]), the shaper falls back
// to the person's own first words (task_shape.go). None of them is a blank row
// — a session with no name of its own is drawn under the person's opening
// words, which meta.json has carried since their first message (placemeta.go's
// [Agent.stampUserLocked]).
func titleHasControlTokens(raw string) bool {
	return strings.Contains(raw, "<｜") || strings.Contains(raw, "<|") || strings.Contains(raw, "[im_start]") || strings.Contains(raw, "[im_end]")
}

func cleanTitle(raw string) string {
	// Model control tokens are not titles, including when emitted before prose.
	// Refusing the candidate retains the prompt-derived name and permits retry.
	if titleHasControlTokens(raw) {
		return ""
	}
	title := stripMarkup(strings.TrimSpace(firstLine(raw)))
	title = strings.Trim(title, `"'“”`)
	title = stripOpener(title)
	title = strings.Trim(title, `"'“”`)
	title = strings.TrimRight(title, ".")
	title = strings.TrimSpace(title)
	if title == "" {
		return ""
	}
	if !strings.ContainsAny(title, " \t") {
		title = strings.NewReplacer("_", " ", "-", " ").Replace(title)
		title = strings.Join(strings.Fields(title), " ")
	}
	title = clip(strings.TrimSpace(title), titleLimit)
	if namesTheInstruction(title) || unusableName(title) || opensAsPlan(title) {
		return ""
	}
	return title
}

// unusableName refuses empty-subject answers that small namers have returned.
// They describe the failed naming call rather than the conversation or task.
func unusableName(name string) bool {
	switch strings.Join(normalizedWords(name), " ") {
	case "untitled", "nothing to name", "no title", "no name", "n a", "none", "null":
		return true
	}
	return false
}

// stripMarkup takes the markdown off a name.
//
// A NAME IS DRAWN AS PLAIN TEXT WHEREVER IT IS DRAWN — a status line, a rail row
// twenty-four columns wide, a list of yesterday's sessions — so the asterisks a
// model reaches for when it wants a title to look like a title arrive on screen
// as asterisks. A row reading `**refactor beta.py — 15+ single-rename steps**`
// was measured on the rail, and the same emphasis rides a session's name whenever
// a namer decides a heading is what was asked for.
//
// THE MARKS ARE THE ONES A MODEL USES FOR A LABEL: emphasis, a code span, a
// heading and a list marker. Emphasis and code are markup wherever they stand,
// so they come out of the middle as well as the ends; a hash, a quote's angle
// bracket and a list marker mean nothing except at the FRONT of a line, so they
// are trimmed only there and a name that is about `#4` keeps it. A dash, plus or
// numbered marker is markup only when a space separates it from the words, which
// keeps real names such as `-v flag handling` and `v1.2.3 release notes` whole.
// An asterisk marker already comes out with emphasis rather than needing the
// same rule in a second place. The underscore is deliberately left alone — it
// is a character inside identifiers a person may genuinely have named, and
// [cleanTitle] already unwelds the one case where it is a separator.
func stripMarkup(title string) string {
	title = strings.TrimLeft(title, "#> \t")
	title = strings.NewReplacer("*", "", "`", "").Replace(title)
	title = stripListMarker(title)
	return strings.TrimSpace(title)
}

// stripListMarker removes one list marker because a name is one line rather
// than a nested list. The separating space is the evidence that punctuation is
// a marker instead of the first character of the name itself.
func stripListMarker(title string) string {
	if strings.HasPrefix(title, "- ") || strings.HasPrefix(title, "+ ") {
		return title[2:]
	}
	digits := 0
	for digits < len(title) && title[digits] >= '0' && title[digits] <= '9' {
		digits++
	}
	if digits > 0 && digits+1 < len(title) && (title[digits] == '.' || title[digits] == ')') && title[digits+1] == ' ' {
		return title[digits+2:]
	}
	return title
}

// ── an answer that is not a name ────────────────────────────────────────────
//
// A CHEAP MODEL ECHOING ITS OWN INSTRUCTION IS AN ORDINARY FAILURE and has to
// be survivable. A real session on this machine was called "name this session
// in ≤8 words, lowercase, no quotes" — the namer's instruction, answered
// verbatim by whichever of nex-n2-mini, ling-2.6-flash and glm-latest that
// turn's auxiliary calls landed on, cleaned by a hand that only trimmed, and
// then drawn title-cased across home. Asking better (the instruction is last in
// the user message now) makes it rarer; refusing the answer is what makes it
// harmless.
//
// instructionPhrases are the openings of the two namers' instructions, and one
// of them appearing anywhere in an answer means the model handed the
// instruction back rather than doing what it said. They are matched against the
// normalized answer, so case, quotes and punctuation do not hide an echo.
var instructionPhrases = []string{
	"name this session",
	"name this conversation twice",
	"name this piece of work",
	"what is this work trying to find out",
}

// instructionEchoDivisor sets how much of a prompt an answer may borrow: an
// answer holding one in every two of a prompt's distinct words — HALF of them —
// is read as that prompt rather than as a name. Half is far past coincidence
// for eight words or fewer, and it catches the echo that was reworded on the
// way back ("session name in 8 words, lowercase, no quotes"), which no phrase
// test can.
const instructionEchoDivisor = 2

// namesTheInstruction reports whether a cleaned name is one of the namers' own
// instructions rather than an answer to it.
//
// BOTH INSTRUCTIONS ARE TESTED WHICHEVER NAMER ASKED, because one hand cleans
// both ([cleanTaskName], task_shape.go's parseShapedBrief) and a name that is
// the OTHER prompt is no more a name than one that is this prompt.
func namesTheInstruction(name string) bool {
	said := normalizedWords(name)
	if len(said) == 0 {
		return false
	}
	joined := " " + strings.Join(said, " ") + " "
	for _, phrase := range instructionPhrases {
		if strings.Contains(joined, " "+strings.Join(normalizedWords(phrase), " ")+" ") {
			return true
		}
	}
	spoken := make(map[string]bool, len(said))
	for _, word := range said {
		spoken[word] = true
	}
	for _, prompt := range []string{titlePrompt, legacyTitlePrompt, taskNamePrompt, jobNamePrompt, captionPrompt} {
		asked := 0
		shared := 0
		for _, word := range uniqueWords(normalizedWords(prompt)) {
			asked++
			if spoken[word] {
				shared++
			}
		}
		if asked > 0 && shared*instructionEchoDivisor >= asked {
			return true
		}
	}
	return false
}

// isOpener reads the words in front of a colon and says whether they are an
// announcement. Either end decides it: an interjection at the front ("sure,
// here is the name"), or one of the words a label ends on at the back ("title",
// "session name", "the session is about", "the title is").
func isOpener(words []string) bool {
	if isInterjection(words[0]) {
		return true
	}
	switch words[len(words)-1] {
	case "title", "name", "is", "about", "called", "answer", "caption", "full", "tab":
		return true
	}
	return false
}

// openerLimit and openerWords bound what may be read as throat-clearing. A
// label is short and stands at the very front; anything longer is a sentence
// the model meant, and cutting at a colon inside one would take half a name
// away.
const (
	openerLimit = 48
	openerWords = 6
)

// stripOpener removes the announcement a model puts in front of a name it was
// asked to give on its own — "Title:", "Session name:", "Sure, here is the
// title:", "Sure, ...", "The session is about:".
//
// IT IS A LABEL TEST AND NOT A COLON TEST. Cutting at every leading colon would
// rewrite "fix: nil map crash" into "nil map crash", so the words in front of
// the colon have to read as an announcement: an interjection at the front, or
// one of the words a label ends on at the back.
//
// An opener with NOTHING after it leaves the empty string, and [cleanTitle]
// refuses that. A model that answered "Sure, here is the title:" and put the
// name on the next line has not answered this call — firstLine has already
// taken the only line the answer is read from.
func stripOpener(title string) string {
	title = stripInterjection(title)
	// Twice, because "Sure: Title: porting the parser" is two announcements and
	// stopping after the first would keep half of one.
	for range 2 {
		colon := strings.Index(title, ":")
		if colon <= 0 || colon > openerLimit {
			break
		}
		words := normalizedWords(title[:colon])
		if len(words) == 0 || len(words) > openerWords || !isOpener(words) {
			break
		}
		title = strings.TrimSpace(title[colon+1:])
	}
	// AND AN ANSWER MADE OF NOTHING BUT ANNOUNCEMENT is an opener with nothing
	// behind it however it was punctuated — "Sure", "Here is the title", "the
	// name". A name has a word in it that is about the conversation.
	if words := normalizedWords(title); len(words) > 0 && len(words) <= openerWords && allOpenerWords(words) {
		return ""
	}
	return title
}

// stripInterjection removes the one-word agreement a model may put before an
// answer. The paired label reader and the plain-name reader share this exact
// rule so their idea of "Sure," and "Okay!" cannot drift apart.
func stripInterjection(title string) string {
	if cut := strings.IndexAny(title, ",!"); cut > 0 && cut <= openerLimit {
		if words := normalizedWords(title[:cut]); len(words) == 1 && isInterjection(words[0]) {
			return strings.TrimSpace(title[cut+1:])
		}
	}
	// AND IT NEEDS NO PUNCTUATION WHEN "SO" IS CARRYING IT. A model writing the
	// way somebody talks opens "okay so the bakery pages" with no comma at all,
	// and `so` after an agreement is that comma spelled as a word. Something has
	// to be left behind it, or this would strip an answer down to nothing.
	if fields := strings.Fields(title); len(fields) > 2 && strings.EqualFold(fields[1], "so") {
		if words := normalizedWords(fields[0]); len(words) == 1 && isInterjection(words[0]) {
			return strings.Join(fields[2:], " ")
		}
	}
	return title
}

// openerVocabulary is the words an announcement is built out of. It is used
// only to test whether an answer is ENTIRELY announcement, so a name that
// happens to contain one of them ("the parser name") is untouched — it also
// contains a word about the conversation, which is what makes it a name.
var openerVocabulary = map[string]bool{
	"sure": true, "ok": true, "okay": true, "certainly": true, "absolutely": true,
	"here": true, "heres": true, "is": true, "are": true, "the": true, "a": true,
	"an": true, "this": true, "that": true, "it": true, "its": true, "i": true,
	"ll": true, "would": true, "call": true, "title": true, "titled": true,
	"name": true, "named": true, "session": true, "for": true, "of": true,
	"and": true, "your": true, "my": true, "answer": true, "about": true,
	"called": true, "as": true, "follows": true, "caption": true,
}

// allOpenerWords reports whether every word of an answer is announcement.
func allOpenerWords(words []string) bool {
	for _, word := range words {
		if !openerVocabulary[word] {
			return false
		}
	}
	return true
}

// ── an answer that was never a name ─────────────────────────────────────────
//
// A REASONING MODEL NARRATES ITS PLAN BEFORE IT FOLLOWS ONE, and a call asking
// for a name does not stop it doing that. Measured in the field (#942), with
// every tier and role pinned to a reasoning model, the namer answered "I'll
// start by creating the four bakery landing pages one at a time." — 264
// completion tokens about what it was about to do. The reader took the first
// three words of it and a task stood on the rail called `I'll start by`, while
// the node's own record still carried the person's words right beside the name
// it had been given.
//
// THE CUT IS WHAT MADE A SENTENCE LOOK LIKE A LABEL. Nothing refused an answer
// for being a sentence; it was shortened until it was the right length to pass
// for a name. So an answer is refused BY ITS SHAPE AND AT ANY LENGTH: a name is
// a noun phrase, and a clause about the speaker is not one. A noun phrase that
// merely ran long ("launch post for existing users") is still cut to three
// words and is still a name.
//
// THE TEST IS THE GRAMMAR AND NOT A LIST OF SENTENCES: a first-person subject,
// followed by the word that makes it the subject of something about to happen —
// an auxiliary, the remnant the fold in [normalizedWords] leaves of one written
// as a contraction, or the person `let` is addressed about. BOTH HALVES ARE
// REQUIRED, which is what keeps a real name that opens on one of those words
// ("I/O error fix") out of it.
//
// The throat-clearing a plan opens on — "First, I will …", "Okay so I'll …" —
// is NOT a second rule here. It is an opener, and [stripInterjection] is the one
// place this file takes an opener off an answer, so it is taken off there and
// this reads what is left.

// planSubjects is who a plan is about. The fold in [normalizedWords] reads every
// mark as a space, so a contraction arrives as its stem and a remnant ("I'll" is
// "i ll", "let's" is "let s") and the pronoun itself is the only spelling that
// has to be written down. It sits beside [openerVocabulary] because the two are
// the same kind of thing: the words a model reaches for when it is not
// answering the question it was asked.
var planSubjects = map[string]bool{"i": true, "we": true, "let": true, "lets": true}

// planPredicates is the word that follows that subject when the sentence is
// about what the speaker is about to do: the auxiliary or modal a finite clause
// hangs on, the remnant a contraction leaves of one, or the person `let` is
// addressed about. IT IS A CLOSED CLASS OF GRAMMAR AND NOT A LIST OF OBSERVED
// SENTENCES, which is why it is this short and why a preamble nobody has met yet
// is already refused.
var planPredicates = map[string]bool{
	"ll": true, "m": true, "ve": true, "d": true, "re": true, "s": true,
	"will": true, "would": true, "shall": true, "should": true, "can": true,
	"could": true, "may": true, "might": true, "must": true, "am": true,
	"is": true, "are": true, "was": true, "were": true, "have": true,
	"has": true, "had": true, "do": true, "does": true, "did": true,
	"need": true, "going": true, "me": true, "us": true,
}

// opensAsPlan reports whether an answer opens as a clause about the speaker
// rather than as a name for the work. It runs on the REPAIRED answer and before
// any caller cuts it, because the shape it refuses is the FRONT of a sentence:
// read after the cut to three words it would be handed "I'll start by", which
// is the same three words a label would have been.
func opensAsPlan(title string) bool {
	words := normalizedWords(title)
	return len(words) > 1 && planSubjects[words[0]] && planPredicates[words[1]]
}

// isInterjection is the handful of words a model clears its throat with before
// it answers: the agreement it opens on, and the SEQUENCING ADVERB a plan
// narrates itself with ("First, I will write the four pages"). They are one list
// because they are one thing to a reader — a word about the answering rather
// than about the work — and because the plan a reasoning namer opens on (#942)
// has to have its opener taken off it HERE, before [opensAsPlan] reads what is
// left, or the pronoun that decides it is never the first word.
func isInterjection(word string) bool {
	switch word {
	case "sure", "ok", "okay", "certainly", "absolutely", "here", "heres",
		"first", "next", "then", "now", "alright", "so":
		return true
	}
	return false
}

// normalizedWords reduces text to the lowercase words in it, with every mark
// that is not a letter or a digit read as a space. It is what makes "Name This
// Session in ≤8 Words, Lowercase, No Quotes." and the instruction it came from
// the same sequence of words.
func normalizedWords(text string) []string {
	folded := strings.Map(func(r rune) rune {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			return unicode.ToLower(r)
		}
		return ' '
	}, text)
	return strings.Fields(folded)
}

// uniqueWords drops the repeats, so a prompt that says "words" twice does not
// count for two against the share an echo has to reach.
func uniqueWords(words []string) []string {
	seen := make(map[string]bool, len(words))
	kept := make([]string, 0, len(words))
	for _, word := range words {
		if seen[word] {
			continue
		}
		seen[word] = true
		kept = append(kept, word)
	}
	return kept
}

// healedTitle is what a STORED name is worth on the way back in.
//
// The refusal above is minted at the moment a name is made, and the names that
// were already accepted and written down are still on disk — a journal's title
// line, a folder's meta.json. So every reader of a stored name comes through
// here: the replay a resume is built from (sessionfile.go), the picker's
// forward scan ([Peek]) and the identity file ([LoadMeta]).
//
// NOTHING IS REWRITTEN. The journal is append-only and meta.json is a citation
// rebuilt from it; a stored echo is simply not read as a name, which leaves the
// session unnamed — drawn under the person's opening words — and leaves
// [Agent.titleTried] false, so the namer gets its one call again on the next
// completed turn and the next good name is appended as every name always is.
func healedTitle(stored string) string {
	stored = strings.TrimSpace(stored)
	if stored == "" || titleHasControlTokens(stored) || namesTheInstruction(stored) || unusableName(stored) {
		return ""
	}
	// Old paired replies sometimes kept their formatting label as part of the name.
	if strings.HasPrefix(strings.ToLower(stripMarkup(stored)), "full:") {
		return cleanConversationTitle(stored).full
	}
	return stored
}
