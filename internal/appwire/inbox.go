package appwire

import (
	"encoding/json"
	"strings"
	"time"
)

// Wave 4: the Inbox (PROTOCOL.md; V04, V68 "The dock and the Inbox"). Every
// open ask from every agent is one item, answered from here or anywhere else.

// The Inbox's events.
const (
	EventInboxAdded    = "inbox.added"
	EventInboxResolved = "inbox.resolved"
	EventInboxCounts   = "inbox.counts"
)

// The Inbox's lists, and how an item was resolved.
const (
	InboxWaiting = "waiting"
	InboxLater   = "later"
	InboxDone    = "done"

	ResolvedNoLongerNeeded = "no_longer_needed"
)

// InboxItem is one ask in the Inbox, with where it comes from and the two
// turns before it.
type InboxItem struct {
	ID       string         `json:"id"`
	Ask      Ask            `json:"ask"`
	Where    InboxWhere     `json:"where"`
	At       time.Time      `json:"at"`
	List     string         `json:"list"`
	Answered *InboxAnswered `json:"answered,omitempty"`
	Context  []Entry        `json:"context"`
}

// InboxWhere is where an item comes from: its agent, its crew and task when
// it has them, and the names to show for the way there.
type InboxWhere struct {
	Agent string   `json:"agent"`
	Crew  string   `json:"crew,omitempty"`
	Task  string   `json:"task,omitempty"`
	Path  []string `json:"path"`
}

// InboxAnswered is how a done item was answered.
type InboxAnswered struct {
	Option string    `json:"option,omitempty"`
	Text   string    `json:"text,omitempty"`
	By     string    `json:"by"`
	At     time.Time `json:"at"`
	Where  string    `json:"where,omitempty"`
}

// InboxCounts are the Inbox's three counts.
type InboxCounts struct {
	Waiting   int `json:"waiting"`
	Later     int `json:"later"`
	DoneToday int `json:"done_today"`
}

// The data of the Inbox's events.
type (
	InboxAddedEvent struct {
		Item InboxItem `json:"item"`
	}
	InboxResolvedEvent struct {
		ID      string `json:"id"`
		By      string `json:"by"`
		Receipt string `json:"receipt"`
	}
)

// Inboxer is the engine's half of the Inbox.
type Inboxer interface {
	ListInbox(list, query string) ([]InboxItem, InboxCounts, error)
	// AnswerInbox holds an answer for a few seconds, which UndoInbox takes
	// back, and answers when the hold ends.
	AnswerInbox(id, option, text, why string) (time.Time, error)
	UndoInbox(id string) error
	LaterInbox(id string, later bool) error
	OpenInbox(id string) (agent, task string, err error)
}

func init() {
	for name, m := range inboxMethods {
		methods[name] = m
	}
}

func inboxer(e Engine, name string) (Inboxer, error) {
	i, ok := e.(Inboxer)
	if !ok {
		return nil, Errorf(CodeUnknownMethod, "this engine does not serve %s", name)
	}
	return i, nil
}

// inboxVerb is one Inbox verb that takes only an id.
func inboxVerb(name string, act func(Inboxer, string) error) method {
	return func(e Engine, raw json.RawMessage) (any, error) {
		i, err := inboxer(e, name)
		if err != nil {
			return nil, err
		}
		id, err := decodeID(raw, "id")
		if err != nil {
			return nil, err
		}
		return struct{}{}, act(i, id)
	}
}

var inboxMethods = map[string]method{
	"inbox.list": func(e Engine, raw json.RawMessage) (any, error) {
		i, err := inboxer(e, "inbox.list")
		if err != nil {
			return nil, err
		}
		var p struct {
			List  string `json:"list"`
			Query string `json:"query"`
		}
		if err := decode(raw, &p); err != nil {
			return nil, err
		}
		if p.List == "" {
			p.List = InboxWaiting
		}
		if p.List != InboxWaiting && p.List != InboxLater && p.List != InboxDone {
			return nil, Errorf(CodeBadParams, "list is waiting, later or done")
		}
		items, counts, err := i.ListInbox(p.List, strings.TrimSpace(p.Query))
		if err != nil {
			return nil, err
		}
		for k := range items {
			items[k].Context = nonNil(items[k].Context)
			items[k].Where.Path = nonNil(items[k].Where.Path)
		}
		return struct {
			Items  []InboxItem `json:"items"`
			Counts InboxCounts `json:"counts"`
		}{nonNil(items), counts}, nil
	},
	"inbox.answer": func(e Engine, raw json.RawMessage) (any, error) {
		i, err := inboxer(e, "inbox.answer")
		if err != nil {
			return nil, err
		}
		var p struct {
			ID     string `json:"id"`
			Option string `json:"option"`
			Text   string `json:"text"`
			Why    string `json:"why"`
		}
		if err := decode(raw, &p); err != nil {
			return nil, err
		}
		if strings.TrimSpace(p.ID) == "" || (strings.TrimSpace(p.Option) == "" && strings.TrimSpace(p.Text) == "") {
			return nil, Errorf(CodeBadParams, "inbox.answer needs an id and an option, some text, or both")
		}
		until, err := i.AnswerInbox(p.ID, strings.TrimSpace(p.Option), p.Text, p.Why)
		if err != nil {
			return nil, err
		}
		return struct {
			UndoUntil time.Time `json:"undo_until"`
		}{until}, nil
	},
	"inbox.undo":  inboxVerb("inbox.undo", func(i Inboxer, id string) error { return i.UndoInbox(id) }),
	"inbox.later": inboxVerb("inbox.later", func(i Inboxer, id string) error { return i.LaterInbox(id, true) }),
	"inbox.back":  inboxVerb("inbox.back", func(i Inboxer, id string) error { return i.LaterInbox(id, false) }),
	"inbox.open": func(e Engine, raw json.RawMessage) (any, error) {
		i, err := inboxer(e, "inbox.open")
		if err != nil {
			return nil, err
		}
		id, err := decodeID(raw, "id")
		if err != nil {
			return nil, err
		}
		agent, task, err := i.OpenInbox(id)
		if err != nil {
			return nil, err
		}
		return struct {
			Agent string `json:"agent"`
			Task  string `json:"task,omitempty"`
		}{agent, task}, nil
	},
}
