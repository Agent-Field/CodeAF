package main

// ── codeaf app: THE INBOX ──────────────────────────────────────────────────
//
// Every open ask of every agent this app holds is one Inbox item (PROTOCOL.md,
// Wave 4). The Inbox is not a second store of questions: it is read off the
// same events the app already says — an ask opening, an ask closing and the
// receipt that follows — so an ask answered anywhere (the dock, a task's
// page, a crew, the terminal) leaves the Inbox in the same instant it leaves
// the conversation, and one answered from the Inbox closes everywhere else the
// same way, through the conversation's one answer door.
//
// AN ANSWER FROM THE INBOX IS HELD FIVE SECONDS before it is sent, so a
// person who answered the wrong row can take it back; nothing reaches the
// engine until the hold ends.
//
// An agent with an open ask is never let go by the idle sweep (app.go's
// [appBackend.sweepAgent]), which is what keeps its asks answerable here.

import (
	"strings"
	"sync"
	"time"

	"github.com/Agent-Field/codeaf/internal/appwire"
	"github.com/Agent-Field/codeaf/internal/guard"
)

const (
	// appInboxHold is how long an Inbox answer waits before it is sent.
	appInboxHold = 5 * time.Second
	// appInboxRecent is how many of an agent's recent entries are kept for
	// an item's context, and appInboxDone how many done items are kept.
	appInboxRecent = 60
	appInboxDone   = 500
)

// appInbox is the Inbox's state: the open items, the done ones, the recent
// conversation of each agent, and the answers being held.
type appInbox struct {
	mu      sync.Mutex
	next    appwire.Emitter
	items   map[string]*appwire.InboxItem
	order   []string
	done    []appwire.InboxItem
	recent  map[string][]appwire.Entry
	closing map[string]appwire.AskClosed
	held    map[string]*appInboxHeld
	from    map[string]string
}

// appInboxHeld is one Inbox answer waiting out its hold.
type appInboxHeld struct {
	timer *time.Timer
	until time.Time
	sent  bool
}

func (in *appInbox) locked(fn func()) {
	in.mu.Lock()
	defer in.mu.Unlock()
	fn()
}

// appInboxTap is the backend's emitter with the Inbox listening: every event
// goes out as it is, and the Inbox says what it changed after it.
type appInboxTap struct {
	b    *appBackend
	next appwire.Emitter
}

func (t *appInboxTap) Emit(event string, data any) {
	t.next.Emit(event, data)
	t.b.inboxSaw(event, data)
}

// newAppInbox listens on emit and answers the emitter the backend uses.
func (b *appBackend) newAppInbox(emit appwire.Emitter) appwire.Emitter {
	b.inbox = &appInbox{next: emit, items: map[string]*appwire.InboxItem{}, recent: map[string][]appwire.Entry{}, closing: map[string]appwire.AskClosed{}, held: map[string]*appInboxHeld{}, from: map[string]string{}}
	return &appInboxTap{b: b, next: emit}
}

// appInboxID is an item's id: its agent and its ask.
func appInboxID(agent, ask string) string { return agent + "/" + ask }

// appInboxSplit is an item id's agent and ask.
func appInboxSplit(id string) (string, string, bool) {
	agent, ask, ok := strings.Cut(id, "/")
	return agent, ask, ok && agent != "" && ask != ""
}

// inboxSaw folds one event the app said into the Inbox.
func (b *appBackend) inboxSaw(event string, data any) {
	in := b.inbox
	if in == nil {
		return
	}
	switch v := data.(type) {
	case appwire.AskOpened:
		b.inboxOpened(v.Ask)
	case appwire.AskClosed:
		id := appInboxID(v.Agent, v.ID)
		var held *appInboxHeld
		in.locked(func() {
			in.closing[id] = v
			held = in.held[id]
			if held != nil && !held.sent {
				held.timer.Stop()
			}
			delete(in.held, id)
		})
	case appwire.EntryAdded:
		entry := v.Entry
		switch entry.Kind {
		case appwire.EntryUser, appwire.EntryAnswer, appwire.EntryUpdate:
			in.locked(func() {
				recent := append(in.recent[entry.Agent], entry)
				if len(recent) > appInboxRecent {
					recent = recent[len(recent)-appInboxRecent:]
				}
				in.recent[entry.Agent] = recent
			})
		case appwire.EntryReceipt:
			if ask, ok := strings.CutPrefix(entry.ID, "r"); ok {
				b.inboxResolved(appInboxID(entry.Agent, ask), entry)
			}
		}
	}
}

// inboxOpened adds an ask to the Inbox, or says it again as it now stands.
func (b *appBackend) inboxOpened(ask appwire.Ask) {
	in := b.inbox
	id := appInboxID(ask.Agent, ask.ID)
	var item appwire.InboxItem
	known := false
	in.locked(func() {
		if have := in.items[id]; have != nil {
			known = true
			have.Ask = ask
			have.List = appwire.InboxWaiting
			if ask.Later {
				have.List = appwire.InboxLater
			}
			item = *have
		}
	})
	if !known {
		item = appwire.InboxItem{ID: id, Ask: ask, Where: b.inboxWhere(ask), At: b.now(), List: appwire.InboxWaiting}
		if ask.Later {
			item.List = appwire.InboxLater
		}
		in.locked(func() {
			item.Context = appInboxContext(in.recent[ask.Agent])
			kept := item
			in.items[id] = &kept
			in.order = append(in.order, id)
			delete(in.closing, id)
		})
	}
	in.next.Emit(appwire.EventInboxAdded, appwire.InboxAddedEvent{Item: item})
	b.inboxCounts()
}

// appInboxContext is the two turns before a question: from the person's
// second-to-last message on.
func appInboxContext(recent []appwire.Entry) []appwire.Entry {
	users := 0
	start := 0
	for i := len(recent) - 1; i >= 0; i-- {
		if recent[i].Kind == appwire.EntryUser {
			users++
			start = i
			if users == 2 {
				break
			}
		}
	}
	return append([]appwire.Entry{}, recent[start:]...)
}

// inboxWhere is where an ask comes from: its agent, crew and task, and the
// names on the way there.
func (b *appBackend) inboxWhere(ask appwire.Ask) appwire.InboxWhere {
	where := appwire.InboxWhere{Agent: ask.Agent}
	var title, taskTitle string
	b.locked(func() {
		if a := b.agents[ask.Agent]; a != nil {
			title = a.view.Title
			if ask.Source != nil && ask.Source.Task != "" && a.live != nil {
				where.Task = ask.Source.Task
				taskTitle = a.live.tasks[ask.Source.Task].Title
			}
		}
	})
	if taskTitle != "" {
		where.Path = append(where.Path, taskTitle)
	}
	if crews, err := appReadCrews(); err == nil {
		for _, crew := range crews {
			for _, member := range crew.Members {
				if member.Agent == ask.Agent {
					where.Crew = crew.ID
					where.Path = append([]string{crew.Name}, where.Path...)
				}
			}
		}
	}
	if title != "" {
		at := 0
		if where.Crew != "" {
			at = 1
		}
		where.Path = append(where.Path[:at], append([]string{title}, where.Path[at:]...)...)
	}
	return where
}

// inboxResolved moves an item to done once its receipt is said.
func (b *appBackend) inboxResolved(id string, receipt appwire.Entry) {
	in := b.inbox
	var closed appwire.AskClosed
	open := false
	where := ""
	in.locked(func() {
		item := in.items[id]
		if item == nil {
			return
		}
		open = true
		closed = in.closing[id]
		where = in.from[id]
		delete(in.items, id)
		delete(in.closing, id)
		delete(in.from, id)
		for i, have := range in.order {
			if have == id {
				in.order = append(in.order[:i], in.order[i+1:]...)
				break
			}
		}
		by := closed.By
		if by == "" {
			by = appwire.ClosedByElsewhere
		}
		at := closed.At
		if at.IsZero() {
			at = b.now()
		}
		item.List = appwire.InboxDone
		item.Answered = &appwire.InboxAnswered{Option: closed.Option, By: by, At: at, Where: where}
		in.done = append(in.done, *item)
		if len(in.done) > appInboxDone {
			in.done = in.done[len(in.done)-appInboxDone:]
		}
	})
	if !open {
		return
	}
	by := closed.By
	switch {
	case strings.HasPrefix(receipt.Text, "No longer needed"):
		by = appwire.ResolvedNoLongerNeeded
	case by == "":
		by = appwire.ClosedByElsewhere
	}
	in.next.Emit(appwire.EventInboxResolved, appwire.InboxResolvedEvent{ID: id, By: by, Receipt: receipt.Text})
	b.inboxCounts()
}

// countsLocked are the Inbox's counts now. Callers hold in.mu.
func (b *appBackend) countsLocked() appwire.InboxCounts {
	in := b.inbox
	var counts appwire.InboxCounts
	for _, item := range in.items {
		if item.List == appwire.InboxLater {
			counts.Later++
		} else {
			counts.Waiting++
		}
	}
	now := b.now().Local()
	midnight := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())
	for _, item := range in.done {
		if item.Answered != nil && !item.Answered.At.Before(midnight) {
			counts.DoneToday++
		}
	}
	return counts
}

// inboxCounts says the counts, which drive the rail badge and the Dock badge.
func (b *appBackend) inboxCounts() {
	var counts appwire.InboxCounts
	b.inbox.locked(func() { counts = b.countsLocked() })
	b.inbox.next.Emit(appwire.EventInboxCounts, counts)
}

// ListInbox answers one of the Inbox's lists, newest first, narrowed by a
// query over its words and where it comes from.
func (b *appBackend) ListInbox(list, query string) ([]appwire.InboxItem, appwire.InboxCounts, error) {
	in := b.inbox
	query = strings.ToLower(query)
	matches := func(item appwire.InboxItem) bool {
		if query == "" {
			return true
		}
		words := strings.ToLower(item.Ask.Title + " " + item.Ask.Detail + " " + item.Ask.Reason + " " + strings.Join(item.Where.Path, " "))
		return strings.Contains(words, query)
	}
	var items []appwire.InboxItem
	var counts appwire.InboxCounts
	in.locked(func() {
		counts = b.countsLocked()
		if list == appwire.InboxDone {
			for i := len(in.done) - 1; i >= 0; i-- {
				if matches(in.done[i]) {
					items = append(items, in.done[i])
				}
			}
			return
		}
		for i := len(in.order) - 1; i >= 0; i-- {
			item := in.items[in.order[i]]
			if item != nil && item.List == list && matches(*item) {
				items = append(items, *item)
			}
		}
	})
	return items, counts, nil
}

// AnswerInbox holds an answer for five seconds, then sends it through the
// conversation's own answer door.
func (b *appBackend) AnswerInbox(id, option, text, why string) (time.Time, error) {
	agent, ask, ok := appInboxSplit(id)
	if !ok {
		return time.Time{}, appwire.Errorf(appwire.CodeBadParams, "%s is not an Inbox item", id)
	}
	in := b.inbox
	var refused error
	var until time.Time
	in.locked(func() {
		switch {
		case in.items[id] == nil:
			refused = appwire.Errorf(appwire.CodeNotFound, "the Inbox item %s is no longer waiting", id)
		case in.held[id] != nil:
			refused = appwire.Errorf(appwire.CodeBusy, "the Inbox item %s is already being answered", id)
		default:
			until = b.now().Add(appInboxHold)
			held := &appInboxHeld{until: until}
			held.timer = time.AfterFunc(appInboxHold, func() {
				defer guard.Recover("app/inbox-answer")
				send := false
				in.locked(func() {
					if in.held[id] == held {
						held.sent, send = true, true
						in.from[id] = "inbox"
					}
				})
				if !send {
					return
				}
				// ONE INBOX ITEM IS ONE QUESTION: an ask that came in a batch is
				// answered alone here, and its siblings stay in the Inbox.
				if err := b.AnswerAskWith(agent, ask, appwire.AskAnswer{Option: option, Text: text, Why: why, BatchItem: ask}); err != nil {
					in.locked(func() {
						delete(in.held, id)
						delete(in.from, id)
					})
				}
			})
			in.held[id] = held
		}
	})
	return until, refused
}

// UndoInbox takes back an Inbox answer still in its hold: nothing is sent and
// the item stays waiting.
func (b *appBackend) UndoInbox(id string) error {
	in := b.inbox
	var refused error
	in.locked(func() {
		held := in.held[id]
		switch {
		case held == nil:
			refused = appwire.Errorf(appwire.CodeNotFound, "there is no answer to take back for %s", id)
		case held.sent:
			refused = appwire.Errorf(appwire.CodeSent, "that answer has already been sent")
		default:
			held.timer.Stop()
			delete(in.held, id)
		}
	})
	return refused
}

// LaterInbox puts an item aside, or brings it back to waiting; the dock reads
// the same mark.
func (b *appBackend) LaterInbox(id string, later bool) error {
	agent, askID, ok := appInboxSplit(id)
	if !ok {
		return appwire.Errorf(appwire.CodeBadParams, "%s is not an Inbox item", id)
	}
	a, err := b.find(agent)
	if err != nil {
		return err
	}
	live := b.liveOf(a)
	if live == nil {
		return appwire.Errorf(appwire.CodeNotFound, "the Inbox item %s is no longer waiting", id)
	}
	var ask appwire.Ask
	found := false
	b.locked(func() {
		for i := range live.asks {
			if live.asks[i].ID == askID {
				live.asks[i].Later = later
				ask, found = live.asks[i], true
			}
		}
	})
	if !found {
		return appwire.Errorf(appwire.CodeNotFound, "the Inbox item %s is no longer waiting", id)
	}
	live.box.Event(appwire.EventAskOpened, appwire.AskOpened{Ask: ask})
	return nil
}

// OpenInbox answers where an item leads: its agent, and its task when it has
// one.
func (b *appBackend) OpenInbox(id string) (string, string, error) {
	in := b.inbox
	var where appwire.InboxWhere
	found := false
	in.locked(func() {
		if item := in.items[id]; item != nil {
			where, found = item.Where, true
			return
		}
		for _, item := range in.done {
			if item.ID == id {
				where, found = item.Where, true
			}
		}
	})
	if !found {
		return "", "", appwire.Errorf(appwire.CodeNotFound, "there is no Inbox item %s", id)
	}
	return where.Agent, where.Task, nil
}
