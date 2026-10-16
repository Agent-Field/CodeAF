package main

// appattention.go — the facts a calm surface needs to hold a question that
// holds nothing up, and to say what is new since the person last read.
//
// TWO VERBS ON A QUESTION'S CLOCK. A surface holding a question until the
// person pauses defers its clock (asks.defer) and starts it when the question
// is finally shown (asks.shown); internal/session's askclock.go keeps the clock
// and its five-minute bound. Only a question the engine KNOWS holds nothing up
// is deferred ([appAskBlocking] false): anything else is refused, so a surface
// that guessed is told so rather than obeyed.
//
// ONE BOUNDED READING OF WHAT CAME AFTER A PLACE (agents.since): the turns,
// the entries and the distinct files changed after one entry of the record,
// counted from the same cached shapes a page is cut from. No page is dressed
// and nothing is sent but the counts.

import (
	"context"
	"strings"
	"time"

	"github.com/Agent-Field/codeaf/internal/appwire"
	"github.com/Agent-Field/codeaf/internal/remote"
	"github.com/Agent-Field/codeaf/internal/session"
)

// appClockWait is how long a clock verb waits for the conversation's host.
const appClockWait = 5 * time.Second

// appSinceFacts is how many tool entries after the place have their facts
// read for the files count. Past it the count is not known and is not said.
const appSinceFacts = 400

// appQuestionClocker is the conversation's clock door; a [remote.Agent] is one.
type appQuestionClocker interface {
	ClockQuestion(ctx context.Context, kind session.QuestionKind, token string, start bool) (remote.QuestionClockReply, error)
}

// DeferAsk keeps one open ask's clock from starting until [appBackend.ShowAsk].
func (b *appBackend) DeferAsk(agent, id string) (*appwire.Ask, error) {
	return b.clockAsk(agent, id, false)
}

// ShowAsk starts a deferred ask's clock now and answers the ask as it stands.
// An ask whose clock was never deferred is answered unchanged.
func (b *appBackend) ShowAsk(agent, id string) (*appwire.Ask, error) {
	return b.clockAsk(agent, id, true)
}

func (b *appBackend) clockAsk(agent, id string, start bool) (*appwire.Ask, error) {
	a, err := b.find(agent)
	if err != nil {
		return nil, err
	}
	live := b.liveOf(a)
	if live == nil {
		return nil, appwire.Errorf(appwire.CodeNotFound, "the ask %s is not open", id)
	}
	var q session.Question
	open := false
	b.locked(func() { q, open = live.questions[id] })
	if !open {
		return nil, appwire.Errorf(appwire.CodeNotFound, "the ask %s is not open", id)
	}
	if blocks := appAskBlocking(q); !start && (blocks == nil || *blocks) {
		return nil, appwire.Errorf(appwire.CodeBadParams, "the ask %s is not known to hold nothing up, so its clock is not deferred", id)
	}
	clocker, ok := live.conn.Conv.(appQuestionClocker)
	if !ok {
		return nil, appwire.Errorf(appwire.CodeEngine, "this conversation cannot defer a question's clock")
	}
	ctx, cancel := context.WithTimeout(context.Background(), appClockWait)
	defer cancel()
	reply, err := clocker.ClockQuestion(ctx, q.Kind, q.Token(), start)
	if err != nil {
		return nil, appEngineError(err)
	}
	var ask *appwire.Ask
	b.locked(func() {
		q, open = live.questions[id]
		if !open {
			return
		}
		q.Deadline = reply.Deadline
		live.questions[id] = q
		shaped := appAskOf(agent, q)
		appAskFacts(agent, &shaped, q)
		shaped.HoldingUp = askHoldingUpLocked(live, shaped)
		for i := range live.asks {
			if live.asks[i].ID == id {
				live.asks[i] = shaped
			}
		}
		ask = &shaped
	})
	return ask, nil
}

// Since counts what the agent's record holds after one entry.
func (b *appBackend) Since(agent, after string) (appwire.Since, error) {
	a, err := b.find(agent)
	if err != nil {
		return appwire.Since{}, err
	}
	after = strings.TrimSpace(after)
	var transcript string
	var at time.Time
	marks := map[int]appTurnMark{}
	b.locked(func() {
		transcript, at = a.transcript, a.view.UpdatedAt
		for n, mark := range a.marks {
			marks[n] = mark
		}
	})
	if transcript == "" {
		return appwire.Since{}, nil
	}
	var since appwire.Since
	err = appRecords.WithRecord(transcript, func(record session.Record) error {
		items := appRecordItems(record)
		all, from := appEntryShapes(agent, items, at, marks)
		since = appSinceOf(all, after, func(i int) []appwire.FileChange {
			if i >= len(from) || from[i] < 0 {
				return nil
			}
			return b.appEntryFacts(a, items[from[i]]).Files
		})
		return nil
	})
	if err != nil {
		return appwire.Since{}, appEntriesBusy(err)
	}
	return since, nil
}

// appSinceOf is [appBackend.Since] over shaped entries; files reads one tool
// entry's changed files and is called at most [appSinceFacts] times.
//
// A TURN is what the person sees as one: each of their messages opens one, and
// what the conversation did on its own before the next of their messages — a
// wake, a crew's delivery, the rest of a reply — is one more.
func appSinceOf(all []appwire.Entry, after string, files func(int) []appwire.FileChange) appwire.Since {
	place := -1
	for i, entry := range all {
		if entry.ID == after {
			place = i
			break
		}
	}
	if place < 0 {
		return appwire.Since{}
	}
	since := appwire.Since{Found: true}
	read := all[place].At
	since.At = &read
	paths := map[string]bool{}
	tools, turns, inTurn, over := 0, 0, false, false
	for i := place + 1; i < len(all); i++ {
		entry := all[i]
		since.Entries++
		if since.First == "" {
			since.First = entry.ID
		}
		last := entry.At
		since.LastAt = &last
		switch entry.Kind {
		case appwire.EntryUser:
			turns, inTurn = turns+1, true
		case appwire.EntryAnswer, appwire.EntryUpdate, appwire.EntryTool:
			if !inTurn {
				turns, inTurn = turns+1, true
			}
		}
		if entry.Kind != appwire.EntryTool || over {
			continue
		}
		if tools == appSinceFacts {
			over = true
			continue
		}
		tools++
		for _, change := range files(i) {
			if path := strings.TrimSpace(change.Path); path != "" {
				paths[path] = true
			}
		}
	}
	since.Turns = turns
	if !over {
		n := len(paths)
		since.Files = &n
	}
	return since
}
