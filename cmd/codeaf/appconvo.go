package main

// ── codeaf app: THE CONVERSATION, THE DOCK, THE QUEUE, TASKS AS CHATS ───────
//
// Wave 3b (PROTOCOL.md, V65–V70). Everything here is the engine's own
// machinery said on the app's wire: a steer is the engine's Steer, consumed or
// fallen through by its own word; a thought is its reasoning stream; a summary
// is its compaction; a retry, a fallback and a failure are its retry news; an
// ask's choices, suggestion, clock, evidence and the tasks it holds up are the
// question's own fields; a task's pause, brief, notes and their read receipts
// are its task door's.
//
// TWO THINGS ARE THE APP'S OWN. The queue is kept here, because the engine's
// follow-up queue cannot be edited or reordered in place; a queued message is
// sent as an ordinary message when the turn before it ends. And the files and
// links a message carries are written into the words the engine is sent, as a
// person would have written them, because the engine takes words.

import (
	"bytes"
	"errors"
	"io"
	"io/fs"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/Agent-Field/codeaf/internal/appwire"
	"github.com/Agent-Field/codeaf/internal/guard"
	"github.com/Agent-Field/codeaf/internal/redact"
	"github.com/Agent-Field/codeaf/internal/remote"
	"github.com/Agent-Field/codeaf/internal/session"
	"github.com/Agent-Field/codeaf/internal/tui3"
)

const (
	// appPasteLines and appPasteChars are where a paste becomes a card.
	appPasteLines = 12
	appPasteChars = 2000
	// appEvidenceLines is how many lines of an ask's evidence it carries.
	appEvidenceLines = 8
	// appFilesKept bounds one walk of a workspace for files.find.
	appFilesKept = 50000
	// appFilesFresh is how long one walk is answered from before another.
	appFilesFresh = 30 * time.Second
	// appNoteBeat is how often a task's unread notes are looked at.
	appNoteBeat = 2 * time.Second
	// appOutputKeep is how much of a long output is kept: the last 512 KB.
	appOutputKeep = 512 << 10
)

// appTrimmedLine is the line that stands where earlier output was dropped.
const appTrimmedLine = "… earlier output trimmed to the last 512 KB"

// appQueued is one message of the person's waiting for the turn to end. text
// is the words the model will be sent; shown, when set, is the person's own
// words and the reads text was written from, which the queue and the entry
// show instead of it. shown is this message's own copy, never shared.
type appQueued struct {
	id, text, clientID string
	shown              *appSending
}

// appAttach is one attachment the composer added, with what it carries.
type appAttach struct {
	view    appwire.Attachment
	content string
	// chars is a paste's length in characters, counted once.
	chars int
}

// appNotePending is a note to a task not yet read.
type appNotePending struct {
	entry appwire.Entry
	text  string
	index int
}

// ── steering ────────────────────────────────────────────────────────────────

// Steer puts the person's words into the running turn at its next step. A
// steer the turn ends before reading falls through, and becomes the next
// message, as the engine does it.
func (b *appBackend) Steer(agent, text, clientID string) error {
	return b.steer(agent, text, clientID, nil)
}

// SteerMessage is [appBackend.Steer] with the person's own words and the reads
// text was written from, kept on the correction's line in the record and on
// the turn's message if it falls through.
func (b *appBackend) SteerMessage(agent string, message appwire.AgentMessage) error {
	shown, err := appMarkOf(message, "a correction")
	if err != nil {
		return err
	}
	return b.steer(agent, message.Text, message.ClientID, shown)
}

var (
	_ appwire.MessageSender  = (*appBackend)(nil)
	_ appwire.MessageSteerer = (*appBackend)(nil)
	_ appwire.MessageQueuer  = (*appBackend)(nil)
	_ appSteerer             = (*remote.Agent)(nil)
	_ appMarkProbe           = (*remote.Agent)(nil)
)

// appSteerer is a conversation's door for a correction that carries the
// person's mark. A [remote.Agent] is one; whether its host keeps the mark is
// [appMarkProbe]'s to say.
type appSteerer interface {
	SteerMessage(message session.Submission) (<-chan session.Event, error)
}

// appMarkProbe is a conversation that can say, before anything is asked of
// it, whether its host keeps a marked message, a marked correction and a
// marked follow-up. A conversation that cannot say is held in this process
// and has the door it was built with.
type appMarkProbe interface {
	MessageMarksKnown() bool
	SteerMarksKnown() bool
}

// appMarkOf is a correction's or a queued message's mark, checked whole and
// kept as the record will keep it: the person's own words and the reads.
// Attachments are refused: neither road carries one.
func appMarkOf(message appwire.AgentMessage, what string) (*appSending, error) {
	if len(message.Attachments) > 0 {
		return nil, appwire.Errorf(appwire.CodeBadParams, "%s does not carry attachments; send them as a message", what)
	}
	reads, err := appKeptReads(message.Reads)
	if err != nil {
		return nil, err
	}
	said := message.Text
	if message.Said != nil {
		said = *message.Said
	}
	return &appSending{text: said, reads: reads}, nil
}

// appKeepsQueuedMarks is whether conv's host keeps a queued message's mark
// when the queue sends it, which goes by SubmitMessage.
func appKeepsQueuedMarks(conv appConversation) bool {
	probe, ok := conv.(appMarkProbe)
	return !ok || probe.MessageMarksKnown()
}

func (b *appBackend) steer(agent, text, clientID string, shown *appSending) error {
	a, err := b.find(agent)
	if err != nil {
		return err
	}
	live := b.liveOf(a)
	running := false
	if live != nil {
		b.locked(func() { running = live.running })
	}
	if !running {
		return appwire.Errorf(appwire.CodeBusy, "nothing is running to steer; send it as a message")
	}
	// A CORRECTION'S MARK IS KEPT OR THE CORRECTION IS NOT MADE: a host that
	// cannot keep it is refused before the turn hears a word, and the person's
	// draft stays theirs.
	var events <-chan session.Event
	if shown != nil {
		steerer, ok := live.conn.Conv.(appSteerer)
		if probe, known := live.conn.Conv.(appMarkProbe); !ok || (known && !probe.SteerMarksKnown()) {
			return appwire.Errorf(appwire.CodeUnknownMethod, "this conversation cannot keep a correction's said or reads; send it as a message")
		}
		events, err = steerer.SteerMessage(appSubmission(text, shown))
	} else {
		events, err = live.conn.Conv.Steer(text)
	}
	if err != nil {
		return appEngineError(err)
	}
	entryText, reads := text, []appwire.MessageRead(nil)
	if shown != nil {
		entryText, reads = shown.text, appReadsOf(shown.reads)
	}
	var entry appwire.Entry
	b.locked(func() {
		live.steers++
		entry = appwire.Entry{ID: "st" + strconv.FormatUint(live.turn, 10) + "." + strconv.Itoa(live.steers), Agent: agent, Turn: live.turn, Kind: appwire.EntrySteer, At: b.now(), Text: entryText, ClientID: clientID, State: appwire.SteerPending, Reads: reads}
	})
	live.box.Event(appwire.EventEntryAdded, appwire.EntryAdded{Entry: entry})
	guard.Go("app/steer", func() { b.followSteer(a, live, entry, text, shown, events) })
	return nil
}

// followSteer reads one steer's own stream: consumed at a step, or fallen
// through, after which the same stream is the turn its words start. text is
// the words the model was sent and shown the person's mark, if any.
func (b *appBackend) followSteer(a *appAgent, live *appLive, entry appwire.Entry, text string, shown *appSending, events <-chan session.Event) {
	for event := range events {
		switch event.Kind {
		case session.EventSteerConsumed:
			b.locked(func() {
				step := live.steps
				entry.Step = &step
			})
			entry.State, entry.At = appwire.SteerConsumed, b.now()
			live.box.Event(appwire.EventEntryAdded, appwire.EntryAdded{Entry: entry})
		case session.EventSteerFellThrough:
			entry.State, entry.At = appwire.SteerFellThrough, b.now()
			b.locked(func() { live.adopting++ })
			live.box.Event(appwire.EventEntryAdded, appwire.EntryAdded{Entry: entry})
			b.adoptTurn(a, live, text, entry.ClientID, shown, events)
			return
		}
	}
}

// adoptTurn makes the rest of a fallen-through steer's stream the agent's
// next turn, once the turn it missed has said its last. Its user entry shows
// the person's mark, as the record's line for it does.
func (b *appBackend) adoptTurn(a *appAgent, live *appLive, text, clientID string, shown *appSending, events <-chan session.Event) {
	var done chan struct{}
	b.locked(func() { done = live.pumpDone })
	if done != nil {
		<-done
	}
	turn, n, id, ok := uint64(0), 0, "", false
	b.locked(func() {
		live.adopting--
		if a.live != live || live.running {
			return
		}
		ok = true
		id = a.view.ID
		n = a.users
		a.users = n + 1
		live.running, live.stopping = true, false
		turn = b.turns.Add(1)
		live.turn, live.steps, live.steers = turn, 0, 0
		live.retries, live.retrySaid, live.retryPhase = 0, false, ""
		live.pumpDone = make(chan struct{})
		if a.marks == nil {
			a.marks = map[int]appTurnMark{}
		}
		a.marks[n] = appTurnMark{turn: turn, clientID: clientID}
		a.view.State = appwire.StateWorking
		a.view.UpdatedAt = b.now()
	})
	if !ok {
		for range events {
		}
		return
	}
	entryText, reads := text, []appwire.MessageRead(nil)
	if shown != nil {
		entryText, reads = shown.text, appReadsOf(shown.reads)
	}
	live.box.Event(appwire.EventTurnStarted, appwire.TurnStarted{Agent: id, Turn: turn})
	live.box.Event(appwire.EventEntryAdded, appwire.EntryAdded{Entry: appwire.Entry{
		ID: appUserEntryID(n), Agent: id, Turn: turn, Kind: appwire.EntryUser, At: b.now(), Text: entryText, ClientID: clientID, Reads: reads,
	}})
	long := b.watchLong(id, live, turn, b.now())
	b.newTurnCalls(a)
	tails := b.tickTools(a, id, live)
	defer long.Stop()
	defer tails()
	b.pump(a, id, live, turn, n, text, shown, events)
	b.sendQueued(a, live)
}

// ── the queue ───────────────────────────────────────────────────────────────

// Queue keeps a message for when the running turn ends, or sends it now when
// nothing is running.
func (b *appBackend) Queue(agent, text, clientID string) ([]appwire.Queued, error) {
	return b.queue(agent, text, clientID, nil)
}

// QueueMessage is [appBackend.Queue] with the person's own words and the reads
// text was written from. The queue keeps all three and shows the person's
// words; the message, once sent, is an ordinary one carrying them.
func (b *appBackend) QueueMessage(agent string, message appwire.AgentMessage) ([]appwire.Queued, error) {
	shown, err := appMarkOf(message, "a queued message")
	if err != nil {
		return nil, err
	}
	return b.queue(agent, message.Text, message.ClientID, shown)
}

func (b *appBackend) queue(agent, text, clientID string, shown *appSending) ([]appwire.Queued, error) {
	a, err := b.find(agent)
	if err != nil {
		return nil, err
	}
	live := b.liveOf(a)
	busy := false
	if live != nil {
		b.locked(func() { busy = live.running || live.adopting > 0 })
	}
	if !busy {
		if _, err := b.send(agent, text, clientID, shown); err != nil {
			return nil, err
		}
		return b.queued(a), nil
	}
	// A QUEUED MARK IS ONE THE HOST WILL KEEP, or nothing is queued: the
	// refusal comes now, with the draft still the person's, and never as a
	// message that failed when the turn ended.
	if shown != nil && !appKeepsQueuedMarks(live.conn.Conv) {
		return nil, appwire.Errorf(appwire.CodeUnknownMethod, "this conversation cannot keep a queued message's said or reads; send it when the turn ends")
	}
	var queued []appwire.Queued
	b.locked(func() {
		a.queueN++
		a.queue = append(a.queue, appQueued{id: "q" + strconv.Itoa(a.queueN), text: text, clientID: clientID, shown: shown})
		queued = appQueuedOf(a.queue)
	})
	b.emit.Emit(appwire.EventQueueChanged, appwire.QueueChanged{Agent: agent, Queued: queued})
	return queued, nil
}

// appQueuedOf is the queue as the app sees it: the person's own words, never
// the words the model will be sent, and a copy of each message's reads.
func appQueuedOf(queue []appQueued) []appwire.Queued {
	out := make([]appwire.Queued, 0, len(queue))
	for _, q := range queue {
		view := appwire.Queued{ID: q.id, Text: q.text}
		if q.shown != nil {
			view.Text, view.Reads = q.shown.text, appReadsOf(q.shown.reads)
		}
		out = append(out, view)
	}
	return out
}

func (b *appBackend) queued(a *appAgent) []appwire.Queued {
	var out []appwire.Queued
	b.locked(func() { out = appQueuedOf(a.queue) })
	return out
}

// changeQueue applies one change to a queued message, refusing one that has
// already been sent.
func (b *appBackend) changeQueue(agent, id string, change func(a *appAgent, at int)) ([]appwire.Queued, error) {
	a, err := b.find(agent)
	if err != nil {
		return nil, err
	}
	var queued []appwire.Queued
	found := false
	b.locked(func() {
		for i, q := range a.queue {
			if q.id == id {
				found = true
				change(a, i)
				break
			}
		}
		queued = appQueuedOf(a.queue)
	})
	if !found {
		return nil, appwire.Errorf(appwire.CodeSent, "that message has already been sent")
	}
	b.emit.Emit(appwire.EventQueueChanged, appwire.QueueChanged{Agent: agent, Queued: queued})
	return queued, nil
}

// EditQueued changes a queued message's words. A PLAIN EDIT REPLACES THE WHOLE
// MESSAGE: the said and reads it carried are dropped with the old words, so
// nothing is shown or kept beside words that were not written from it.
func (b *appBackend) EditQueued(agent, id, text string) ([]appwire.Queued, error) {
	return b.changeQueue(agent, id, func(a *appAgent, at int) { a.queue[at].text, a.queue[at].shown = text, nil })
}

// EditQueuedMessage replaces a queued message with another carrying its own
// said and reads. The whole replacement is checked before the queue is
// touched, so a refusal leaves every queued message as it was; a message
// already sent answers sent and nothing is queued.
func (b *appBackend) EditQueuedMessage(agent, id string, message appwire.AgentMessage) ([]appwire.Queued, error) {
	shown, err := appMarkOf(message, "a queued message")
	if err != nil {
		return nil, err
	}
	a, err := b.find(agent)
	if err != nil {
		return nil, err
	}
	if live := b.liveOf(a); live != nil && !appKeepsQueuedMarks(live.conn.Conv) {
		return nil, appwire.Errorf(appwire.CodeUnknownMethod, "this conversation cannot keep a queued message's said or reads")
	}
	return b.changeQueue(agent, id, func(a *appAgent, at int) { a.queue[at].text, a.queue[at].shown = message.Text, shown })
}

// RemoveQueued takes a queued message out.
func (b *appBackend) RemoveQueued(agent, id string) ([]appwire.Queued, error) {
	return b.changeQueue(agent, id, func(a *appAgent, at int) { a.queue = append(a.queue[:at], a.queue[at+1:]...) })
}

// MoveQueued puts a queued message at another place in the queue.
func (b *appBackend) MoveQueued(agent, id string, to int) ([]appwire.Queued, error) {
	return b.changeQueue(agent, id, func(a *appAgent, at int) {
		moved := a.queue[at]
		rest := append(append([]appQueued(nil), a.queue[:at]...), a.queue[at+1:]...)
		to = min(to, len(rest))
		a.queue = append(append(append([]appQueued(nil), rest[:to]...), moved), rest[to:]...)
	})
}

// sendQueued sends the first queued message once a turn has ended and
// nothing else is about to start.
func (b *appBackend) sendQueued(a *appAgent, live *appLive) {
	var next appQueued
	var queued []appwire.Queued
	var agent string
	ok := false
	b.locked(func() {
		if a.live != live || live.running || live.adopting > 0 || len(a.queue) == 0 {
			return
		}
		next, a.queue = a.queue[0], a.queue[1:]
		queued, agent, ok = appQueuedOf(a.queue), a.view.ID, true
	})
	if !ok {
		return
	}
	b.emit.Emit(appwire.EventQueueChanged, appwire.QueueChanged{Agent: agent, Queued: queued})
	if _, err := b.send(agent, next.text, next.clientID, next.shown); err != nil {
		live.box.Event(appwire.EventEntryAdded, appwire.EntryAdded{Entry: appwire.Entry{
			ID: "qx" + next.id, Agent: agent, Kind: appwire.EntryError, At: b.now(), Text: "the queued message was not sent: " + err.Error(),
		}})
	}
}

// ── the turn's own entries ──────────────────────────────────────────────────

// appFooter is one quiet line at the end of a turn.
func appFooter(id, agent string, turn uint64, kind, text string, at time.Time) appwire.Entry {
	return appwire.Entry{ID: id, Agent: agent, Turn: turn, Kind: appwire.EntryFooter, Footer: kind, At: at, Text: text}
}

// ── the decision dock ───────────────────────────────────────────────────────

// appAskFacts adds the dock's facts to an ask from its question.
func appAskFacts(agent string, ask *appwire.Ask, q session.Question) {
	ask.Reason = strings.TrimSpace(q.Reason)
	ask.Irreversible = ask.Irreversible || q.Stakes == session.StakesIrreversible
	ask.Source = &appwire.AskSource{Kind: "reply"}
	if q.Subject.Kind == session.SubjectNode && q.Subject.ID != 0 {
		ask.Source = &appwire.AskSource{Kind: "task", Task: appTaskID(agent, q.Subject.ID)}
	} else if q.Kind == session.QuestionTask && q.ID != 0 {
		ask.Source = &appwire.AskSource{Kind: "task", Task: appTaskID(agent, q.ID)}
	}
	var lines []string
	for _, block := range q.Attach {
		body := strings.TrimRight(block.Body, "\n")
		if body == "" {
			continue
		}
		lines = append(lines, strings.Split(redact.Secrets(body), "\n")...)
	}
	if len(lines) > 0 {
		shown := lines[:min(len(lines), appEvidenceLines)]
		ask.Evidence = &appwire.Evidence{Lines: shown, More: len(lines) - len(shown)}
	}
	if q.Kind != session.QuestionConsent && len(q.Options) > 0 {
		for _, option := range q.Options {
			choice := appwire.AskChoice{ID: option.Key, Label: option.Label, Detail: strings.TrimSpace(option.Body)}
			if q.Pick != nil && q.Pick.Key == option.Key {
				choice.Suggested = true
			}
			ask.Choices = append(ask.Choices, choice)
		}
	}
	if !q.Deadline.IsZero() && !ask.Irreversible {
		until := q.Deadline
		ask.ClockUntil = &until
	}
	ask.SayWhy = q.Kind == session.QuestionConsent
}

// batchLocked is the calls asked about together with q, open and answered,
// and the open asks of the batch. Callers hold b.mu.
func (b *appBackend) batchLocked(live *appLive, q session.Question) ([]appwire.BatchItem, []string) {
	if q.Batch == "" || q.Kind != session.QuestionConsent {
		return nil, nil
	}
	var items []appwire.BatchItem
	var open []string
	for _, ask := range live.asks {
		sibling, ok := live.questions[ask.ID]
		if !ok || sibling.Batch != q.Batch || sibling.Kind != session.QuestionConsent {
			continue
		}
		call := live.consents[sibling.ID]
		text := tui3.ToolTitle(call.Tool, call.Args, "")
		if text == "" {
			text = sibling.Head
		}
		items = append(items, appwire.BatchItem{ID: ask.ID, Text: redact.Secrets(text)})
		open = append(open, ask.ID)
	}
	for _, done := range live.batchDone[q.Batch] {
		items = append(items, done)
	}
	if len(items) < 2 {
		return nil, open
	}
	sort.SliceStable(items, func(i, j int) bool { return items[i].ID < items[j].ID })
	return items, open
}

// restateBatch says every open ask of a batch again with the batch as it
// stands now.
func (b *appBackend) restateBatch(live *appLive, q session.Question) {
	if q.Batch == "" {
		return
	}
	var asks []appwire.Ask
	b.locked(func() {
		items, open := b.batchLocked(live, q)
		if len(items) < 2 {
			return
		}
		for _, id := range open {
			for i := range live.asks {
				if live.asks[i].ID == id {
					live.asks[i].Batch = items
					asks = append(asks, live.asks[i])
				}
			}
		}
	})
	for _, ask := range asks {
		live.box.Event(appwire.EventAskOpened, appwire.AskOpened{Ask: ask})
	}
}

// AnswerAskWith is asks.answer whole: an option and words, a reason with
// them, one call of a batch or all of them, putting it aside for later, or
// handing the choice back to the agent's own suggestion.
func (b *appBackend) AnswerAskWith(agent, id string, answer appwire.AskAnswer) error {
	if handled, err := b.answerOrderAsk(id, answer.Option); handled {
		return err
	}
	if handled, err := b.answerCrewAsk(agent, id, answer.Option, answer.Text, answer.Why); handled {
		return err
	}
	a, err := b.find(agent)
	if err != nil {
		return err
	}
	live := b.liveOf(a)
	if live == nil {
		return b.AnswerAsk(agent, id, answer.Option, answer.Text)
	}
	var q session.Question
	open := false
	b.locked(func() { q, open = live.questions[id] })
	if answer.Later {
		if !open {
			return appwire.Errorf(appwire.CodeNotFound, "the ask %s is not open", id)
		}
		var ask appwire.Ask
		b.locked(func() {
			for i := range live.asks {
				if live.asks[i].ID == id {
					live.asks[i].Later = true
					ask = live.asks[i]
				}
			}
		})
		live.box.Event(appwire.EventAskOpened, appwire.AskOpened{Ask: ask})
		return nil
	}
	text := strings.TrimSpace(strings.TrimSpace(answer.Text) + "\n" + strings.TrimSpace(answer.Why))
	if answer.YouDecide {
		if !open {
			return appwire.Errorf(appwire.CodeNotFound, "the ask %s is not open", id)
		}
		if q.Pick == nil || q.Pick.Key == "" {
			return appwire.Errorf(appwire.CodeBadParams, "this ask has no suggested answer to hand back")
		}
		label := q.Pick.Key
		for _, option := range q.Options {
			if option.Key == q.Pick.Key {
				label = option.Label
			}
		}
		b.locked(func() { live.answered[id] = appAnsweredMark + appDecideMark + label })
		resolved := session.Answer{Kind: q.Kind, ID: q.ID, Ref: q.Ref, Ask: q.Ask, Key: q.Pick.Key, Picked: []string{q.Pick.Key}, Change: text}
		if err := live.conn.Conv.ResolveQuestion(resolved); err != nil {
			b.locked(func() { delete(live.answered, id) })
			return appEngineError(err)
		}
		return nil
	}
	targets := []string{id}
	if answer.BatchItem != "" {
		targets = []string{answer.BatchItem}
	} else if open {
		b.locked(func() {
			if _, siblings := b.batchLocked(live, q); len(siblings) > 1 {
				targets = siblings
			}
		})
	}
	for _, target := range targets {
		var sibling session.Question
		var callText string
		b.locked(func() {
			sibling = live.questions[target]
			call := live.consents[sibling.ID]
			callText = redact.Secrets(tui3.ToolTitle(call.Tool, call.Args, ""))
		})
		if err := b.AnswerAsk(agent, target, answer.Option, text); err != nil {
			return err
		}
		if sibling.Batch != "" {
			state := "allowed"
			if answer.Option == appwire.OptionDeny {
				state = "denied"
			}
			b.locked(func() {
				if live.batchDone == nil {
					live.batchDone = map[string][]appwire.BatchItem{}
				}
				live.batchDone[sibling.Batch] = append(live.batchDone[sibling.Batch], appwire.BatchItem{ID: target, Text: callText, State: state})
			})
		}
	}
	if q.Batch != "" {
		b.restateBatch(live, q)
	}
	return nil
}

// appDecideMark marks an ask the person handed back to the agent's own pick.
const appDecideMark = "decide:"

// HoldAsk stops an ask's clock. Only a proposed task has one: its hold is the
// task's own.
func (b *appBackend) HoldAsk(agent, id string) error {
	a, err := b.find(agent)
	if err != nil {
		return err
	}
	live := b.liveOf(a)
	if live == nil {
		return appwire.Errorf(appwire.CodeNotFound, "the ask %s is not open", id)
	}
	var q session.Question
	open := false
	b.locked(func() { q, open = live.questions[id] })
	if !open {
		return appwire.Errorf(appwire.CodeNotFound, "the ask %s is not open", id)
	}
	if q.Kind != session.QuestionTask || q.Deadline.IsZero() {
		return nil
	}
	return b.ActOnTask(appTaskID(agent, q.ID), "hold", "", "")
}

// ── files and links in the composer ─────────────────────────────────────────

// appWalk is one walk of a workspace's files, kept a little while.
type appWalk struct {
	at    time.Time
	paths []string
	dirs  map[string]bool
}

// changedInChat is every file this agent's calls changed, with its counts.
func (b *appBackend) changedInChat(a *appAgent) map[string]appwire.FileChange {
	changed := map[string]appwire.FileChange{}
	log := b.callLog(a)
	log.locked(func() {
		for _, id := range log.order {
			if c := log.calls[id]; c != nil {
				for _, f := range c.facts.Files {
					was := changed[f.Path]
					changed[f.Path] = appwire.FileChange{Path: f.Path, Add: was.Add + f.Add, Del: was.Del + f.Del}
				}
			}
		}
	})
	return changed
}

// walkFiles is the workspace's files and folders, relative, from the last
// walk when it is fresh.
func (b *appBackend) walkFiles(a *appAgent, workspace string) appWalk {
	var walk appWalk
	b.locked(func() {
		if a.walk != nil && b.now().Sub(a.walk.at) < appFilesFresh {
			walk = *a.walk
		}
	})
	if walk.paths != nil {
		return walk
	}
	walk = appWalk{at: b.now(), paths: []string{}, dirs: map[string]bool{}}
	_ = filepath.WalkDir(workspace, func(path string, entry fs.DirEntry, err error) error {
		if err != nil || len(walk.paths) >= appFilesKept {
			if entry != nil && entry.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		rel, relErr := filepath.Rel(workspace, path)
		if relErr != nil || rel == "." {
			return nil
		}
		name := entry.Name()
		if entry.IsDir() {
			if strings.HasPrefix(name, ".") || name == "node_modules" || name == "vendor" || name == "target" || name == "dist" || name == "__pycache__" {
				return filepath.SkipDir
			}
			walk.dirs[rel] = true
		}
		walk.paths = append(walk.paths, rel)
		return nil
	})
	b.locked(func() { a.walk = &walk })
	return walk
}

// FindFiles answers the files a reference may name: the ones changed in this
// chat first, then the workspace's, best match first.
func (b *appBackend) FindFiles(agent, query string, limit int) ([]appwire.FileGroup, error) {
	a, err := b.find(agent)
	if err != nil {
		return nil, err
	}
	if limit <= 0 {
		limit = 50
	}
	var workspace string
	b.locked(func() { workspace = a.view.Workspace })
	query = strings.ToLower(strings.TrimSpace(query))
	matches := func(path string) int {
		lower := strings.ToLower(path)
		base := strings.ToLower(filepath.Base(path))
		switch {
		case query == "":
			return 1
		case strings.HasPrefix(base, query):
			return 3
		case strings.Contains(base, query):
			return 2
		case strings.Contains(lower, query):
			return 1
		}
		return 0
	}
	var groups []appwire.FileGroup
	changed := b.changedInChat(a)
	seen := map[string]bool{}
	var mine []appwire.FoundFile
	for path, change := range changed {
		rel := path
		if r, err := filepath.Rel(workspace, path); err == nil && !strings.HasPrefix(r, "..") {
			rel = r
		}
		if matches(rel) == 0 {
			continue
		}
		add, del := change.Add, change.Del
		mine = append(mine, appwire.FoundFile{Path: rel, Add: &add, Del: &del})
		seen[rel] = true
	}
	sort.Slice(mine, func(i, j int) bool { return mine[i].Path < mine[j].Path })
	if len(mine) > 0 {
		groups = append(groups, appwire.FileGroup{Label: "Changed in this chat", Files: mine[:min(len(mine), limit)]})
	}
	if workspace != "" {
		walk := b.walkFiles(a, workspace)
		type scored struct {
			path  string
			score int
		}
		var found []scored
		for _, path := range walk.paths {
			if seen[path] {
				continue
			}
			if score := matches(path); score > 0 {
				found = append(found, scored{path, score})
			}
		}
		sort.SliceStable(found, func(i, j int) bool {
			if found[i].score != found[j].score {
				return found[i].score > found[j].score
			}
			return len(found[i].path) < len(found[j].path)
		})
		var files []appwire.FoundFile
		for _, f := range found[:min(len(found), limit)] {
			files = append(files, appwire.FoundFile{Path: f.path, Dir: walk.dirs[f.path]})
		}
		if len(files) > 0 {
			groups = append(groups, appwire.FileGroup{Label: filepath.Base(workspace), Files: files})
		}
	}
	return groups, nil
}

// statFile is one path as the composer shows it.
func (b *appBackend) statFile(workspace string, changed map[string]appwire.FileChange, path string) appwire.FileStat {
	full := path
	if !filepath.IsAbs(full) {
		full = filepath.Join(workspace, path)
	}
	full = filepath.Clean(full)
	stat := appwire.FileStat{Path: path, State: appwire.FileExists}
	if rel, err := filepath.Rel(workspace, full); workspace == "" || err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		stat.State = appwire.FileOutside
		return stat
	}
	info, err := os.Stat(full)
	if err != nil {
		stat.State = appwire.FileMissing
		return stat
	}
	size := info.Size()
	stat.Size = &size
	if !info.IsDir() {
		if file, err := os.Open(full); err == nil {
			head := make([]byte, 8000)
			n, _ := io.ReadFull(file, head)
			_ = file.Close()
			stat.Binary = bytes.IndexByte(head[:n], 0) >= 0
		}
	}
	if change, ok := changed[full]; ok {
		stat.State = appwire.FileChanged
		add, del := change.Add, change.Del
		stat.Add, stat.Del = &add, &del
	} else if change, ok := changed[path]; ok {
		stat.State = appwire.FileChanged
		add, del := change.Add, change.Del
		stat.Add, stat.Del = &add, &del
	}
	return stat
}

// StatFiles answers what each path is now: there, changed in this chat,
// missing, or outside the workspace.
func (b *appBackend) StatFiles(agent string, paths []string) ([]appwire.FileStat, error) {
	a, err := b.find(agent)
	if err != nil {
		return nil, err
	}
	var workspace string
	b.locked(func() { workspace = a.view.Workspace })
	changed := b.changedInChat(a)
	out := make([]appwire.FileStat, 0, len(paths))
	for _, path := range paths {
		if strings.TrimSpace(path) != "" {
			out = append(out, b.statFile(workspace, changed, path))
		}
	}
	return out, nil
}

// prepareAttach is one attachment made ready to ride on a message: a file
// stat'ed against the workspace, a link parsed, a paste counted. It is the one
// reading of an attachment, shared by a chip staged with attachments.add and a
// source named on agents.create. present says a file must be there now — a
// create has no chip to show a missing file on, so it refuses one instead.
// A file is read no further than its stat: the model is sent its path, and
// reads it with its own tools.
func (b *appBackend) prepareAttach(workspace string, changed map[string]appwire.FileChange, attachment appwire.Attachment, text string, present bool) (appAttach, error) {
	kept := appAttach{view: attachment}
	switch attachment.Kind {
	case appwire.AttachFile:
		stat := b.statFile(workspace, changed, attachment.Path)
		if present && stat.State == appwire.FileOutside {
			// A path outside the workspace is still a path: it is there when
			// it is there.
			if _, err := os.Stat(appAttachFull(workspace, attachment.Path)); err != nil {
				stat.State = appwire.FileMissing
			}
		}
		if present && stat.State == appwire.FileMissing {
			return appAttach{}, appwire.Errorf(appwire.CodeBadParams, "there is no file or folder at %s", attachment.Path)
		}
		kept.view.State, kept.view.Add, kept.view.Del = stat.State, stat.Add, stat.Del
		kept.view.Title = filepath.Base(attachment.Path)
	case appwire.AttachLink:
		parsed, err := url.Parse(strings.TrimSpace(attachment.URL))
		if err != nil || parsed.Host == "" {
			return appAttach{}, appwire.Errorf(appwire.CodeBadParams, "that is not a link")
		}
		kept.view.URL, kept.view.Title = parsed.String(), parsed.Host
	case appwire.AttachPaste:
		chars := utf8.RuneCountInString(text)
		if chars > appwire.MaxPasteChars {
			return appAttach{}, appwire.Errorf(appwire.CodeBadParams, "one paste holds at most %d characters", appwire.MaxPasteChars)
		}
		lines := strings.Count(strings.TrimRight(text, "\n"), "\n") + 1
		kept.view.Title, kept.view.Lines, kept.content, kept.chars = "Pasted text", &lines, text, chars
	default:
		return appAttach{}, appwire.Errorf(appwire.CodeBadParams, "kind is file, link or paste")
	}
	return kept, nil
}

// appAttachFull is a path as the workspace reads it.
func appAttachFull(workspace, path string) string {
	if !filepath.IsAbs(path) {
		path = filepath.Join(workspace, path)
	}
	return filepath.Clean(path)
}

// appAttachWords is the message the model is sent: the person's words, then
// each attachment as words of its own. It is the one spelling of attachments
// as context, whichever door they came in by. Words that are empty leave no
// blank lines at the top.
func appAttachWords(text string, kept []appAttach) string {
	var words strings.Builder
	words.WriteString(text)
	for _, attach := range kept {
		if words.Len() > 0 {
			words.WriteString("\n\n")
		}
		switch attach.view.Kind {
		case appwire.AttachFile:
			words.WriteString("Attached file: " + attach.view.Path)
		case appwire.AttachLink:
			words.WriteString("Link: " + attach.view.URL)
		case appwire.AttachPaste:
			words.WriteString("Pasted text:\n```\n" + strings.TrimRight(attach.content, "\n") + "\n```")
		}
	}
	return words.String()
}

// appAttachShown is what the person's entry shows for a message with
// attachments: their own words and the attachments, not the words the model
// was sent. No attachments is nil, the entry shows what was sent.
func appAttachShown(text string, kept []appAttach) *appSending {
	if len(kept) == 0 {
		return nil
	}
	views := make([]appwire.Attachment, 0, len(kept))
	described := make([]session.MessageAttachment, 0, len(kept))
	for _, attach := range kept {
		views = append(views, attach.view)
		described = append(described, appDescribed(attach))
	}
	return &appSending{text: text, attachments: views, described: described}
}

// appDescribed is one attachment as the record keeps it: what it is, what it
// is called and where it is, and its size where this side measured one — a
// paste, whose words the model is sent whole. A file's size is left unsaid:
// this side reads no further than its stat's state, and an unmeasured size is
// absent rather than zero. Never its contents, and never a fact that changes
// after the send, such as whether a file has changed.
func appDescribed(attach appAttach) session.MessageAttachment {
	described := session.MessageAttachment{
		ID: attach.view.ID, Kind: attach.view.Kind, Name: attach.view.Title, Path: attach.view.Path, URL: attach.view.URL,
	}
	if attach.view.Kind == appwire.AttachPaste {
		bytes := int64(len(attach.content))
		described.Bytes = &bytes
		if attach.view.Lines != nil {
			lines := *attach.view.Lines
			described.Lines = &lines
		}
	}
	return described
}

// appAttachmentsOf is a recorded message's attachments as an entry shows them,
// nil for a message the record keeps none for.
func appAttachmentsOf(person *session.PersonMark) []appwire.Attachment {
	if person == nil || len(person.Attachments) == 0 {
		return nil
	}
	views := make([]appwire.Attachment, 0, len(person.Attachments))
	for _, d := range person.Attachments {
		view := appwire.Attachment{ID: d.ID, Kind: d.Kind, Path: d.Path, URL: d.URL, Title: d.Name}
		if d.Lines != nil {
			lines := *d.Lines
			view.Lines = &lines
		}
		views = append(views, view)
	}
	return views
}

// appSubmission is the message the engine is asked to run and keep: the words
// the model is sent, and the person's own words, attachments and reads beside
// them.
func appSubmission(text string, shown *appSending) session.Submission {
	said := shown.text
	submission := session.Submission{Text: text, Said: &said, Attachments: shown.described, Reads: shown.reads}
	if shown.composed != nil {
		submission.Media, submission.Annotation = shown.composed.media, shown.composed.annotation
	}
	return submission
}

// appKeptReads is a message's reads as the record will keep them, or the
// refusal the engine would answer, as bad_params before anything is done.
func appKeptReads(reads []appwire.MessageRead) ([]session.MessageRead, error) {
	if len(reads) == 0 {
		return nil, nil
	}
	given := make([]session.MessageRead, 0, len(reads))
	for _, read := range reads {
		items := make([]session.MessageReference, 0, len(read.Items))
		for _, item := range read.Items {
			items = append(items, session.MessageReference(item))
		}
		given = append(given, session.MessageRead{
			Source: session.MessageReference(read.Source), Count: read.Count, Missing: read.Missing,
			Unavailable: read.Unavailable, Unread: read.Unread, More: read.More, Complete: read.Complete,
			Warnings: read.Warnings, Items: items,
		})
	}
	kept, err := session.KeptReads(given)
	if err != nil {
		return nil, appwire.Errorf(appwire.CodeBadParams, "%s", plainWords(err.Error()))
	}
	return kept, nil
}

// appReadsOf is a message's reads as an entry shows them, nil for a message
// sent without any.
func appReadsOf(reads []session.MessageRead) []appwire.MessageRead {
	if len(reads) == 0 {
		return nil
	}
	out := make([]appwire.MessageRead, 0, len(reads))
	for _, read := range reads {
		var items []appwire.MessageReference
		for _, item := range read.Items {
			items = append(items, appwire.MessageReference(item))
		}
		out = append(out, appwire.MessageRead{
			Source: appwire.MessageReference(read.Source), Count: read.Count, Missing: read.Missing,
			Unavailable: read.Unavailable, Unread: read.Unread, More: read.More, Complete: read.Complete,
			Warnings: append([]string(nil), read.Warnings...), Items: items,
		})
	}
	return out
}

// appPersonReads is a recorded message's reads as an entry shows them.
func appPersonReads(person *session.PersonMark) []appwire.MessageRead {
	if person == nil {
		return nil
	}
	return appReadsOf(person.Reads)
}

// appAttachChars is the characters of paste a message carries, checked
// against the one bound a message has whichever door it came in by.
func appAttachChars(kept []appAttach) error {
	total := 0
	for _, attach := range kept {
		total += attach.chars
	}
	if total > appwire.MaxAttachChars {
		return appwire.Errorf(appwire.CodeBadParams, "the pastes come to %d characters; one message carries at most %d", total, appwire.MaxAttachChars)
	}
	return nil
}

// AddAttachment keeps a file, a link or a long paste for the next message.
func (b *appBackend) AddAttachment(agent string, attachment appwire.Attachment, text string) (appwire.Attachment, error) {
	a, err := b.find(agent)
	if err != nil {
		return appwire.Attachment{}, err
	}
	if attachment.Kind == appwire.AttachPaste {
		// A SHORT PASTE IS WORDS, NOT A CARD: the composer puts it into the
		// message, and only a long one is kept to the side.
		lines := strings.Count(strings.TrimRight(text, "\n"), "\n") + 1
		if lines <= appPasteLines && len(text) <= appPasteChars {
			return appwire.Attachment{}, appwire.Errorf(appwire.CodeBadParams, "a paste of %d lines goes into the message as text", lines)
		}
	}
	var workspace string
	b.locked(func() { workspace = a.view.Workspace })
	var changed map[string]appwire.FileChange
	if attachment.Kind == appwire.AttachFile {
		changed = b.changedInChat(a)
	}
	kept, err := b.prepareAttach(workspace, changed, attachment, text, false)
	if err != nil {
		return appwire.Attachment{}, err
	}
	b.locked(func() {
		a.attachN++
		kept.view.ID = "at" + strconv.Itoa(a.attachN)
		if a.attach == nil {
			a.attach = map[string]appAttach{}
		}
		a.attach[kept.view.ID] = kept
	})
	return kept.view, nil
}

// RemoveAttachment drops one staged attachment and nothing else. A missing or
// already sent id is fine, so cleaning up after a failed send is harmless.
func (b *appBackend) RemoveAttachment(agent, id string) error {
	a, err := b.find(agent)
	if err != nil {
		return err
	}
	b.locked(func() { delete(a.attach, id) })
	return nil
}

// SendWith sends a message with its attachments, written into the words the
// engine is sent, and keeps them on the person's entry.
func (b *appBackend) SendWith(agent, text, clientID string, ids []string) (uint64, error) {
	turn, _, err := b.sendMessage(agent, appwire.AgentMessage{Text: text, ClientID: clientID, Attachments: ids})
	return turn, err
}

// SendMessage sends a message with what the app knows about it beyond the words
// the model reads: the person's own words, which the entry and the record show
// in place of Text, the staged attachments, and the reads Text was written
// from.
func (b *appBackend) SendMessage(agent string, message appwire.AgentMessage) (uint64, error) {
	turn, _, err := b.sendMessage(agent, message)
	return turn, err
}

// sendMessage is [appBackend.SendMessage], answering what the person's entry
// shows, nil when that is the words sent.
//
// THE PERSON'S WORDS ARE THEIRS: Said, when the app gave it, is what the entry
// and the record show, and never the expanded Text the model is sent. Reads are
// checked whole before anything else, and kept exactly as the record will keep
// them, so the live entry and every later reading say the same.
//
// STAGED ATTACHMENTS ARE CONSUMED ONLY BY A TURN THAT STARTED. They are read,
// not taken, while the turn is asked for, and dropped once it has begun; a
// send that is refused — busy, a missing id, a conversation that would not
// open — leaves every chip staged for the retry, and a chip added meanwhile is
// a new id nothing here touches. The agent's gate is held from the read to the
// drop, so two sends cannot both carry one chip into two turns, and each turn
// carries exactly the snapshot it read.
func (b *appBackend) sendMessage(agent string, message appwire.AgentMessage) (uint64, *appSending, error) {
	ids := message.Attachments
	if err := appwire.CheckAttachIDs(ids); err != nil {
		return 0, nil, err
	}
	reads, err := appKeptReads(message.Reads)
	if err != nil {
		return 0, nil, err
	}
	a, err := b.find(agent)
	if err != nil {
		return 0, nil, err
	}
	a.gate.Lock()
	defer a.gate.Unlock()
	var kept []appAttach
	var missing string
	b.locked(func() {
		for _, id := range ids {
			attach, ok := a.attach[id]
			if !ok {
				missing = id
				return
			}
			kept = append(kept, attach)
		}
	})
	if missing != "" {
		return 0, nil, appwire.Errorf(appwire.CodeNotFound, "there is no attachment %s", missing)
	}
	if err := appAttachChars(kept); err != nil {
		return 0, nil, err
	}
	said := message.Text
	if message.Said != nil {
		said = *message.Said
	}
	shown := appAttachShown(said, kept)
	if shown == nil && (message.Said != nil || len(reads) > 0) {
		shown = &appSending{text: said}
	}
	if shown != nil {
		shown.reads = reads
	}
	// THE PICTURES ARE RESOLVED AND CHECKED WHOLE BEFORE THE CONVERSATION IS
	// OPENED: a message the engine would refuse leaves every chip staged.
	var workspace string
	b.locked(func() { workspace = a.view.Workspace })
	composed, err := appComposedOf(workspace, kept, message.Submission)
	if err != nil {
		return 0, nil, err
	}
	if composed != nil {
		if shown == nil {
			shown = &appSending{text: said, reads: reads}
		}
		shown.composed = composed
	}
	turn, err := b.sendLocked(a, appAttachWords(message.Text, kept), message.ClientID, shown)
	if err != nil {
		return 0, nil, err
	}
	b.locked(func() {
		for _, id := range ids {
			delete(a.attach, id)
		}
	})
	b.recordAttachedMedia(a, kept)
	return turn, shown, nil
}

// appSending is the words and attachments of a message whose entry shows
// them instead of the words the engine was given.
type appSending struct {
	text        string
	attachments []appwire.Attachment
	// described is the same attachments as the record keeps them
	// ([appDescribed]), in the same order.
	described []session.MessageAttachment
	// reads are the reads the words were written from, as the record keeps
	// them ([appKeptReads]).
	reads []session.MessageRead
	// composed is the message's pictures and annotation, and what the engine
	// answered once it kept it (appsubmission.go); nil on a message of words.
	composed *appComposed
}

// ── tasks as conversations ──────────────────────────────────────────────────

// RetryTask runs a failed task again from where it failed, with the same
// brief.
func (b *appBackend) RetryTask(id string) error {
	agent, local, ok := appSplitID(id)
	if !ok {
		return appwire.Errorf(appwire.CodeBadParams, "%s is not a task id", id)
	}
	_, live, err := b.heldLive(agent)
	if err != nil {
		return err
	}
	n, err := strconv.ParseUint(local, 10, 64)
	if err != nil {
		return appwire.Errorf(appwire.CodeBadParams, "%s is not a task id", id)
	}
	if err := live.conn.Conv.RetryTask(n); err != nil {
		return appEngineError(err)
	}
	return nil
}

// NoteTask sends a note to a task and answers its entry; the note is marked
// read when the task's next step carries it.
func (b *appBackend) NoteTask(id, text, clientID string) (appwire.Entry, error) {
	agent, local, ok := appSplitID(id)
	if !ok {
		return appwire.Entry{}, appwire.Errorf(appwire.CodeBadParams, "%s is not a task id", id)
	}
	_, live, err := b.heldLive(agent)
	if err != nil {
		return appwire.Entry{}, err
	}
	before := 0
	if view, found, err := live.conn.Conv.AppTask(local); err == nil && found {
		before = len(view.Notes)
	}
	var entry appwire.Entry
	b.locked(func() {
		live.noteN++
		entry = appwire.Entry{ID: "tn" + strconv.Itoa(live.noteN), Agent: agent, Kind: appwire.EntryNote, At: b.now(), Text: text, ClientID: clientID, From: "you", State: appwire.NoteSending}
		entry.ToolFacts.Task = id
	})
	live.box.Event(appwire.EventEntryAdded, appwire.EntryAdded{Entry: entry})
	handed := false
	if err := b.ActOnTask(id, "note", "", text); err != nil {
		// A TASK HANDED OVER FROM INSIDE THE WORK runs as a crew of its own and
		// is not on the engine's task list, so its note goes through the door the
		// task's own room speaks through. Nothing there says at which step the
		// note was read, so it stays waiting.
		if err := b.steerHandedTask(live, local, text, err); err != nil {
			return appwire.Entry{}, err
		}
		handed = true
	}
	entry.State = appwire.NoteWaiting
	live.box.Event(appwire.EventNoteChanged, appwire.NoteChanged{Task: id, ID: entry.ID, State: appwire.NoteWaiting})
	if handed {
		return entry, nil
	}
	start := false
	b.locked(func() {
		if live.notes == nil {
			live.notes = map[string][]appNotePending{}
		}
		start = len(live.notes[id]) == 0
		live.notes[id] = append(live.notes[id], appNotePending{entry: entry, text: text, index: before})
	})
	if start {
		guard.Go("app/note-read", func() { b.watchNotes(live, id, local) })
	}
	return entry, nil
}

// appTaskSteerer is the door a task's own room sends words through.
type appTaskSteerer interface {
	SteerTask(id uint64, text string) (session.SteerReceipt, error)
}

// steerHandedTask sends a note to a task the engine's task list does not name,
// and answers the original refusal when that door cannot take it either.
func (b *appBackend) steerHandedTask(live *appLive, local, text string, refused error) error {
	var wire *appwire.Error
	if !errors.As(refused, &wire) || wire.Code != appwire.CodeNotFound {
		return refused
	}
	n, err := strconv.ParseUint(local, 10, 64)
	if err != nil {
		return refused
	}
	steerer, ok := live.conn.Conv.(appTaskSteerer)
	if !ok {
		return refused
	}
	if _, err := steerer.SteerTask(n, text); err != nil {
		return appTaskError(err)
	}
	return nil
}

// watchNotes looks at a task's notes until every note sent to it is read.
func (b *appBackend) watchNotes(live *appLive, id, local string) {
	tick := time.NewTicker(appNoteBeat)
	defer tick.Stop()
	for range tick.C {
		var pending []appNotePending
		gone := false
		b.locked(func() {
			pending = append(pending, live.notes[id]...)
			gone = live.closed
		})
		if len(pending) == 0 || gone {
			return
		}
		view, found, err := live.conn.Conv.AppTask(local)
		if err != nil || !found {
			continue
		}
		var read []appwire.NoteChanged
		var left []appNotePending
		for _, note := range pending {
			if note.index < len(view.Notes) && view.Notes[note.index].ReadAtStep != nil {
				step := *view.Notes[note.index].ReadAtStep
				read = append(read, appwire.NoteChanged{Task: id, ID: note.entry.ID, State: appwire.NoteRead, ReadAtStep: &step})
				continue
			}
			left = append(left, note)
		}
		b.locked(func() { live.notes[id] = left })
		for _, changed := range read {
			live.box.Event(appwire.EventNoteChanged, changed)
		}
		if appTaskSettled(view.State) {
			b.locked(func() { delete(live.notes, id) })
			return
		}
	}
}

// appTaskSettled says a task's state is one no step follows.
func appTaskSettled(state string) bool {
	switch state {
	case "done", "failed", "stopped", "cancelled", "declined":
		return true
	}
	return false
}

// TaskEntries is a task's own conversation: its journal read as entries, its
// notes both ways, and its own tasks.
func (b *appBackend) TaskEntries(id, before string, limit int) ([]appwire.Entry, bool, error) {
	agent, local, ok := appSplitID(id)
	if !ok {
		return nil, false, appwire.Errorf(appwire.CodeBadParams, "%s is not a task id", id)
	}
	a, live, err := b.heldLive(agent)
	if err != nil {
		return nil, false, err
	}
	n, err := strconv.ParseUint(local, 10, 64)
	if err != nil {
		return nil, false, appwire.Errorf(appwire.CodeBadParams, "%s is not a task id", id)
	}
	at := b.now()
	var all []appwire.Entry
	if journal, ok := live.conn.Conv.(appTaskJournal); ok {
		record, err := journal.TaskRoom(n, session.TaskJournalTail)
		if err != nil {
			return nil, false, appEngineError(err)
		}
		read := session.ReadTranscriptBytes(record.Journal)
		items := appRecordItems(read)
		for _, entry := range appEntriesOf(agent, items, at, nil) {
			entry.ID = id + "/" + entry.ID
			all = append(all, entry)
		}
	}
	if view, found, err := live.conn.Conv.AppTask(local); err == nil && found {
		// A HANDED-OVER TASK KEEPS NO ROOM OF ITS OWN: its workers' steps are
		// on its record instead, and they are its log, one line a step.
		if len(all) == 0 {
			for i, line := range view.WorkLog {
				if line = strings.TrimSpace(line); line != "" {
					entry := appwire.Entry{ID: id + "/step" + strconv.Itoa(i+1), Agent: agent, Kind: appwire.EntryNotice, At: at, Text: redact.Secrets(line), Quiet: true}
					entry.ToolFacts.Task = id
					all = append(all, entry)
				}
			}
		}
		for i, note := range view.Notes {
			entry := appwire.Entry{ID: id + "/note" + strconv.Itoa(i), Agent: agent, Kind: appwire.EntryNote, At: at, Text: redact.Secrets(note.Text), From: "you", State: appwire.NoteWaiting}
			if note.Direction != 0 {
				entry.From, entry.State = "task", ""
			}
			if note.ReadAtStep != nil {
				step := *note.ReadAtStep
				entry.State, entry.ReadAtStep = appwire.NoteRead, &step
			}
			entry.ToolFacts.Task = id
			all = append(all, entry)
		}
	}
	tasks, err := b.ListTasks(agent)
	if err == nil {
		for _, task := range tasks {
			if task.Parent == id {
				entry := appwire.Entry{ID: id + "/" + task.ID, Agent: agent, Kind: appwire.EntryNotice, At: at, Text: task.Title + " · " + task.State, Ref: task.ID}
				entry.ToolFacts.Task = task.ID
				all = append(all, entry)
			}
		}
	}
	_ = a
	end := len(all)
	if before != "" {
		end = -1
		for i, entry := range all {
			if entry.ID == before {
				end = i
				break
			}
		}
		if end < 0 {
			return nil, false, appwire.Errorf(appwire.CodeNotFound, "this task has no entry %s", before)
		}
	}
	start := max(end-limit, 0)
	return append([]appwire.Entry{}, all[start:end]...), start > 0, nil
}

// appTaskExtras fills what a task list says about each task's place among
// the others: what waits on it, and how many of its own tasks are done.
func appTaskExtras(tasks []appwire.Task) {
	byID := map[string]int{}
	for i := range tasks {
		byID[tasks[i].ID] = i
	}
	children, done := map[string]int{}, map[string]int{}
	for _, task := range tasks {
		if task.Parent != "" {
			children[task.Parent]++
			if task.State == appwire.TaskDone {
				done[task.Parent]++
			}
		}
	}
	for waited, waiters := range appWaiters(tasks) {
		if i, ok := byID[waited]; ok {
			tasks[i].HoldingUp = waiters
		}
	}
	for i := range tasks {
		if count := children[tasks[i].ID]; count > 0 {
			finished := done[tasks[i].ID]
			tasks[i].Children, tasks[i].ChildrenDone = &count, &finished
		}
	}
}

// appTaskOpen is a task that has not ended: neither done nor failed.
func appTaskOpen(task appwire.Task) bool {
	return task.State != appwire.TaskDone && task.State != appwire.TaskFailed
}

// appWaiters is the task graph read backwards, from the engine's own
// dependencies (Task.WaitsOn): for each task, the open tasks that wait on it
// directly, in the order the list gives them. Task.holding_up and
// Ask.holding_up are both read from it.
func appWaiters(tasks []appwire.Task) map[string][]string {
	waiters := map[string][]string{}
	for _, task := range tasks {
		if !appTaskOpen(task) {
			continue
		}
		for _, waited := range task.WaitsOn {
			waiters[waited] = append(waiters[waited], task.ID)
		}
	}
	return waiters
}

// appHoldingUpLimit bounds how many tasks an ask says it holds up.
const appHoldingUpLimit = 256

// appAskHoldingUp is what an ask from a task holds up: that task, then every
// open task whose dependencies reach it, nearest first, each once however the
// graph loops, and no more than [appHoldingUpLimit]. It is nil for an ask
// that comes from no task: what a model names as blocked is never read here.
func appAskHoldingUp(source string, tasks []appwire.Task) []string {
	if source == "" {
		return nil
	}
	waiters := appWaiters(tasks)
	out := []string{source}
	seen := map[string]bool{source: true}
	for i := 0; i < len(out) && len(out) < appHoldingUpLimit; i++ {
		for _, w := range waiters[out[i]] {
			if seen[w] {
				continue
			}
			seen[w] = true
			out = append(out, w)
			if len(out) == appHoldingUpLimit {
				break
			}
		}
	}
	return out
}

// askHoldingUpLocked is an ask's holding_up from the agent's task graph as it
// stands. Callers hold b.mu.
func askHoldingUpLocked(live *appLive, ask appwire.Ask) []string {
	if ask.Source == nil || ask.Source.Task == "" {
		return nil
	}
	tasks := make([]appwire.Task, 0, len(live.tasks))
	for _, task := range live.tasks {
		tasks = append(tasks, task)
	}
	sort.Slice(tasks, func(i, j int) bool { return tasks[i].ID < tasks[j].ID })
	return appAskHoldingUp(ask.Source.Task, tasks)
}

// ── long output ─────────────────────────────────────────────────────────────

// appTrimOutput keeps only the last 512 KB of a file of output, with a line
// saying so, once it has ended.
func appTrimOutput(path string) {
	info, err := os.Stat(path)
	if err != nil || info.Size() <= appOutputKeep {
		return
	}
	file, err := os.Open(path)
	if err != nil {
		return
	}
	defer file.Close()
	tail := make([]byte, appOutputKeep)
	if _, err := file.ReadAt(tail, info.Size()-appOutputKeep); err != nil && !errors.Is(err, io.EOF) {
		return
	}
	if cut := bytes.IndexByte(tail, '\n'); cut >= 0 {
		tail = tail[cut+1:]
	}
	_ = os.WriteFile(path, append([]byte(appTrimmedLine+"\n"), tail...), 0o600)
}

// appTrimmedStart is the first line of a process's output that the last
// 512 KB of it holds, for a reader that starts at the beginning.
func appTrimmedStart(path string) int {
	info, err := os.Stat(path)
	if err != nil || info.Size() <= appOutputKeep {
		return 0
	}
	file, err := os.Open(path)
	if err != nil {
		return 0
	}
	defer file.Close()
	lines := 0
	left := info.Size() - appOutputKeep
	buf := make([]byte, 64<<10)
	for left > 0 {
		n, err := file.Read(buf[:min(int64(len(buf)), left)])
		lines += bytes.Count(buf[:n], []byte{'\n'})
		left -= int64(n)
		if err != nil {
			break
		}
	}
	return lines + 1
}

// recordAttachedMedia notes the files a person attached to a sent message in
// the media index, so they can be listed as sources (origin attachment). Only a
// file that reads as an image, a sound or a video is noted, and only its head is
// read. It happens after the message is sent and its failure is silence.
func (b *appBackend) recordAttachedMedia(a *appAgent, kept []appAttach) {
	var workspace string
	b.locked(func() { workspace = a.view.Workspace })
	id := filepath.Base(filepath.Dir(a.transcript))
	for _, attach := range kept {
		if attach.view.Kind == appwire.AttachFile {
			session.RecordAttachment(appMediaIndex(), id, appAttachFull(workspace, attach.view.Path))
		}
	}
}
