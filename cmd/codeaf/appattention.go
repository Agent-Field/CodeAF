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
	"strconv"
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
	a, err := b.findCrewOwner(agent)
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
		since = appSinceOf(all, from, items, after, func(i int) []appwire.FileChange {
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

// TaskSince counts what one task's own record holds after one of its entries:
// the same room journal tail tasks.entries reads, read once, shaped once.
// Entry ids are the task's ("<task>/<entry>"); an anchor that is not on the
// record (a note, a sub-task row, a step line, or a place older than the tail
// the room keeps) is not found and nothing is counted.
func (b *appBackend) TaskSince(id, after string) (appwire.Since, error) {
	agent, local, ok := appSplitID(id)
	if !ok {
		return appwire.Since{}, appwire.Errorf(appwire.CodeBadParams, "%s is not a task id", id)
	}
	n, err := strconv.ParseUint(local, 10, 64)
	if err != nil {
		return appwire.Since{}, appwire.Errorf(appwire.CodeBadParams, "%s is not a task id", id)
	}
	prefix := id + "/"
	after = strings.TrimSpace(after)
	if !strings.HasPrefix(after, prefix) {
		return appwire.Since{}, nil
	}
	a, live, err := b.heldLive(agent)
	if err != nil {
		return appwire.Since{}, err
	}
	journal, ok := live.conn.Conv.(appTaskJournal)
	if !ok {
		return appwire.Since{}, nil
	}
	record, err := journal.TaskRoom(n, session.TaskJournalTail)
	if err != nil {
		return appwire.Since{}, appEngineError(err)
	}
	items := appRecordItems(session.ReadTranscriptBytes(record.Journal))
	all, from := appEntryShapes(agent, items, b.now(), nil)
	since := appSinceOf(all, from, items, strings.TrimPrefix(after, prefix), func(i int) []appwire.FileChange {
		if i >= len(from) || from[i] < 0 {
			return nil
		}
		return b.appEntryFacts(a, items[from[i]]).Files
	})
	if since.First != "" {
		since.First = prefix + since.First
	}
	return since, nil
}

// appSinceOf is [appBackend.Since] over shaped entries: from is each entry's
// item, items the record's items, and files reads one tool entry's changed
// files, at most [appSinceFacts] times.
func appSinceOf(all []appwire.Entry, from []int, items []session.DisplayEntry, after string, files func(int) []appwire.FileChange) appwire.Since {
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
	if place < len(from) && from[place] >= 0 && from[place] < len(items) {
		if turns, known := appSinceTurns(items[from[place]:]); known {
			since.Turns = &turns
		}
	}
	paths := map[string]bool{}
	tools, over := 0, false
	for i := place + 1; i < len(all); i++ {
		entry := all[i]
		since.Entries++
		if since.First == "" {
			since.First = entry.ID
		}
		last := entry.At
		since.LastAt = &last
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
	if !over {
		n := len(paths)
		since.Files = &n
	}
	return since
}

// appSinceTurns counts the turns after the first item, the place read, and
// says whether the record could tell them apart.
//
// A TURN IS TOLD BY THE RECORD'S OWN MARKS, never by words: the person's
// message opens one; a line the session wrote to its model (a wake, a landing
// note) or a crew's delivery opens one the conversation started itself; a
// reply marked complete ([session.DisplayEntry.Answer], not an update) or one
// a stop cut short ends one. Work after an end with no opener of its own is a
// new turn too.
//
// WHERE THE MARKS CANNOT SAY, THE COUNT IS NOT GIVEN. An opener inside a turn
// that never said it ended is a note steered in or a turn that ended unsaid;
// a reply with no completion mark followed by more work is a record written
// before the mark, or a turn that failed. Either could be one turn or two.
func appSinceTurns(items []session.DisplayEntry) (int, bool) {
	if len(items) == 0 {
		return 0, true
	}
	turns, known := 0, true
	inTurn := !appTurnEnds(items[0])
	for i := 1; i < len(items); i++ {
		item := items[i]
		switch {
		case item.Role == "user":
			turns, inTurn = turns+1, true
		case item.Role == "aside" && item.Interrupted:
			if !inTurn {
				turns++
			}
			inTurn = false
		case item.Role == "aside" && (item.ForModel || len(item.Team) > 0):
			if inTurn {
				known = false
			}
			turns, inTurn = turns+1, true
		case item.Role == "assistant" || (item.Role == "tool" && (item.CallID != "" || item.Tool != "")):
			if !inTurn {
				turns, inTurn = turns+1, true
			}
			if appTurnEnds(item) {
				inTurn = false
			} else if item.Role == "assistant" && i+1 < len(items) && items[i+1].Role != "tool" && items[i+1].Role != "user" {
				known = false
			}
		}
	}
	return turns, known
}

// appTurnEnds reports whether one item is a turn's recorded end: a reply
// marked complete and not an update, or an answer a stop cut short.
func appTurnEnds(item session.DisplayEntry) bool {
	return (item.Role == "assistant" && item.Answer && !item.Addressed) || (item.Role == "aside" && item.Interrupted)
}
