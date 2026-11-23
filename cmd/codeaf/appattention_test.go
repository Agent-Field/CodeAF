package main

import (
	"context"
	"errors"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/appwire"
	"github.com/Agent-Field/codeaf/internal/remote"
	"github.com/Agent-Field/codeaf/internal/session"
)

// WHAT BLOCKS IS ONLY WHAT THE ENGINE KNOWS. A permission blocks its call; the
// model's own question blocks when its turn parks or it names work; every
// other lane is unknown — never a guessed false from an empty holding_up.
func TestAppAskBlockingIsWhatTheEngineKnows(t *testing.T) {
	is := func(b *bool, want string) bool {
		switch want {
		case "unknown":
			return b == nil
		case "true":
			return b != nil && *b
		}
		return b != nil && !*b
	}
	for _, c := range []struct {
		name string
		q    session.Question
		want string
	}{
		{"a permission", session.Question{Kind: session.QuestionConsent}, "true"},
		{"a parked question", session.Question{Kind: session.QuestionAsk, Blocking: session.Blocking{Turn: true}}, "true"},
		{"a question naming work", session.Question{Kind: session.QuestionAsk, Blocking: session.Blocking{Tasks: []string{"4"}}}, "true"},
		{"a question the turn carried on past", session.Question{Kind: session.QuestionAsk}, "false"},
		{"a proposed task", session.Question{Kind: session.QuestionTask}, "unknown"},
		{"a landing", session.Question{Kind: session.QuestionLanding}, "unknown"},
		{"a cap", session.Question{Kind: session.QuestionFuel}, "unknown"},
	} {
		if got := appAskBlocking(c.q); !is(got, c.want) {
			t.Errorf("%s: blocking = %v, want %s", c.name, got, c.want)
		}
	}
}

// clockingConversation is the fake conversation with the host's clock door.
type clockingConversation struct {
	*fakeConversation
	until time.Time
	err   error
	calls []bool
}

func (c *clockingConversation) ClockQuestion(_ context.Context, _ session.QuestionKind, _ string, start bool) (remote.QuestionClockReply, error) {
	c.calls = append(c.calls, start)
	if c.err != nil {
		return remote.QuestionClockReply{}, c.err
	}
	if start {
		return remote.QuestionClockReply{Changed: true, Deadline: c.until}, nil
	}
	return remote.QuestionClockReply{Changed: true}, nil
}

// A HELD ASK'S CLOCK WAITS IN THE ENGINE AND STARTS WHEN SHOWN, and the ask the
// surface draws carries the engine's own deadline; an ask the engine does not
// know holds nothing up is refused, and a host without the door says so.
func TestAppDeferAndShowAnAsksClock(t *testing.T) {
	conv := &fakeConversation{}
	backend, rec, agent := workBackend(t, conv)
	until := time.Now().Add(time.Hour).Round(time.Second)
	clocked := &clockingConversation{fakeConversation: conv, until: until}
	backend.locked(func() { backend.agents[agent.ID].live.conn.Conv = clocked })
	q := session.Question{ID: 3, Kind: session.QuestionAsk, Ask: session.AskChoice, Head: "Which shape?",
		Options: []session.AnswerOption{{Key: "a", Label: "A"}, {Key: "b", Label: "B"}},
		Pick:    &session.Pick{Key: "a"}, Deadline: time.Now().Add(12 * time.Second)}
	conv.raise(session.Event{Kind: session.EventQuestion, Question: &q})
	i := rec.waitFor(t, 5*time.Second, appAskEvent(appwire.EventAskOpened))
	rec.mu.Lock()
	ask := rec.data[i].(appwire.AskOpened).Ask
	rec.mu.Unlock()
	if ask.Blocking == nil || *ask.Blocking {
		t.Fatalf("a question that holds nothing up says blocking %v", ask.Blocking)
	}
	held, err := backend.DeferAsk(agent.ID, ask.ID)
	if err != nil || held == nil || held.ClockUntil != nil {
		t.Fatalf("defer = %+v %v", held, err)
	}
	shown, err := backend.ShowAsk(agent.ID, ask.ID)
	if err != nil || shown == nil || shown.ClockUntil == nil || !shown.ClockUntil.Equal(until) {
		t.Fatalf("shown = %+v %v", shown, err)
	}
	if len(clocked.calls) != 2 || clocked.calls[0] || !clocked.calls[1] {
		t.Fatalf("the host heard %v", clocked.calls)
	}

	consent := session.Question{ID: 4, Kind: session.QuestionConsent, Head: "Run go test?"}
	conv.raise(session.Event{Kind: session.EventQuestion, Question: &consent})
	rec.waitFor(t, 5*time.Second, func(event string, data any) bool {
		o, ok := data.(appwire.AskOpened)
		return ok && o.Ask.Kind == appwire.AskApproval
	})
	var permission string
	backend.locked(func() {
		for id, open := range backend.agents[agent.ID].live.questions {
			if open.Kind == session.QuestionConsent {
				permission = id
			}
		}
	})
	var wire *appwire.Error
	if _, err := backend.DeferAsk(agent.ID, permission); !errors.As(err, &wire) || wire.Code != appwire.CodeBadParams {
		t.Fatalf("a permission's clock was deferred: %v", err)
	}
	clocked.err = errors.New("engine: no such method")
	if _, err := backend.DeferAsk(agent.ID, ask.ID); err == nil {
		t.Fatal("a host that cannot defer was read as having deferred")
	}
}

// sinceRecord shapes items the way a page is cut, so the count is read from
// the same entries and ids the app holds.
func sinceRecord(items []session.DisplayEntry) ([]appwire.Entry, []int) {
	at := time.Date(2026, 10, 15, 9, 0, 0, 0, time.UTC)
	for i := range items {
		items[i].At = at.Add(time.Duration(i) * time.Minute)
	}
	return appEntryShapes("agent", items, at, nil)
}

var (
	sincePerson = func(text string) session.DisplayEntry { return session.DisplayEntry{Role: "user", Text: text} }
	sinceDone   = func(text string) session.DisplayEntry {
		return session.DisplayEntry{Role: "assistant", Text: text, Answer: true}
	}
	sinceSaid = func(text string) session.DisplayEntry { return session.DisplayEntry{Role: "assistant", Text: text} }
	sinceCall = func(id string) session.DisplayEntry {
		return session.DisplayEntry{Role: "tool", Tool: "bash", CallID: id, Answered: true}
	}
	sinceWake  = session.DisplayEntry{Role: "aside", Text: "the task landed", ForModel: true}
	sinceTeam  = session.DisplayEntry{Role: "aside", Text: "from @api", Team: []session.TeamLine{{From: "api"}}}
	sinceNoted = session.DisplayEntry{Role: "aside", Text: "a job exited"}
)

// TURNS ARE TOLD BY THE RECORD'S OWN MARKS. A person's message opens one; a
// wake or a crew's delivery opens one the conversation started; a completed
// reply ends one; work after an end is a new one. Where the marks cannot tell
// one turn from two, the count is not given at all.
func TestAppSinceCountsTurnsByTheRecordsOwnMarks(t *testing.T) {
	for _, c := range []struct {
		name  string
		items []session.DisplayEntry
		after string
		turns int // -1: not known
	}{
		{"two of the person's turns", []session.DisplayEntry{sincePerson("a"), sinceDone("ok"), sincePerson("b"), sinceCall("c1"), sinceDone("done"), sincePerson("c"), sinceDone("done")}, "a0.0", 2},
		{"two turns the conversation started itself", []session.DisplayEntry{sincePerson("a"), sinceDone("ok"), sinceWake, sinceCall("c1"), sinceDone("landed"), sinceTeam, sinceDone("thanks")}, "a0.0", 2},
		{"work after a completed reply with no opener", []session.DisplayEntry{sincePerson("a"), sinceDone("ok"), sinceCall("c1"), sinceDone("one"), sinceCall("c2"), sinceDone("two")}, "a0.0", 2},
		{"one turn's call and answer", []session.DisplayEntry{sincePerson("a"), sinceDone("ok"), sinceWake, sinceCall("c1"), sinceSaid("update"), sinceCall("c2"), sinceDone("done")}, "a0.0", 1},
		{"a turn still running", []session.DisplayEntry{sincePerson("a"), sinceDone("ok"), sincePerson("b"), sinceCall("c1")}, "a0.0", 1},
		{"a note the session wrote is no turn", []session.DisplayEntry{sincePerson("a"), sinceDone("ok"), sinceNoted}, "a0.0", 0},
		{"replies with no completion mark", []session.DisplayEntry{sincePerson("a"), sinceDone("ok"), sinceCall("c1"), sinceSaid("one"), sinceSaid("two")}, "a0.0", -1},
		{"a wake inside a turn that never ended", []session.DisplayEntry{sincePerson("a"), sinceDone("ok"), sincePerson("b"), sinceCall("c1"), sinceWake, sinceDone("done")}, "a0.0", -1},
	} {
		all, from := sinceRecord(c.items)
		since := appSinceOf(all, from, c.items, c.after, func(int) []appwire.FileChange { return nil })
		if !since.Found {
			t.Errorf("%s: the place was not found in %+v", c.name, all)
			continue
		}
		switch {
		case c.turns < 0 && since.Turns != nil:
			t.Errorf("%s: a count the marks cannot make was given: %d", c.name, *since.Turns)
		case c.turns >= 0 && (since.Turns == nil || *since.Turns != c.turns):
			t.Errorf("%s: turns = %v, want %d", c.name, since.Turns, c.turns)
		}
	}
}

// WHAT CAME AFTER A PLACE: entries, its first id, the moments, and distinct
// files; past the bound the files count is not said at all.
func TestAppSinceCountsEntriesAndFilesAfterAPlace(t *testing.T) {
	items := []session.DisplayEntry{sincePerson("a"), sinceDone("ok"), sinceCall("c1"), sinceDone("one"), sincePerson("b"), sinceCall("c2"), sinceDone("two")}
	all, from := sinceRecord(items)
	files := map[string][]appwire.FileChange{"tc1": {{Path: "a.go"}, {Path: "b.go"}}, "tc2": {{Path: "a.go"}}}
	read := func(i int) []appwire.FileChange { return files[all[i].ID] }

	since := appSinceOf(all, from, items, "a0.0", read)
	if !since.Found || since.Entries != 5 || since.First != "tc1" || since.Files == nil || *since.Files != 2 {
		t.Fatalf("since a0.0 = %+v", since)
	}
	if since.At == nil || !since.At.Equal(all[1].At) || since.LastAt == nil || !since.LastAt.Equal(all[len(all)-1].At) {
		t.Fatalf("the moments = %+v", since)
	}
	last := all[len(all)-1].ID
	if nothing := appSinceOf(all, from, items, last, read); !nothing.Found || nothing.Turns == nil || *nothing.Turns != 0 || nothing.Entries != 0 || *nothing.Files != 0 {
		t.Fatalf("nothing new = %+v", nothing)
	}
	if gone := appSinceOf(all, from, items, "a9.9", read); gone.Found || gone.Files != nil || gone.Turns != nil {
		t.Fatalf("a place the record does not hold = %+v", gone)
	}

	long := []session.DisplayEntry{sincePerson("a"), sinceDone("ok")}
	for n := 0; n <= appSinceFacts; n++ {
		long = append(long, sinceCall("c"+strconv.Itoa(n)))
	}
	longAll, longFrom := sinceRecord(long)
	reads := 0
	over := appSinceOf(longAll, longFrom, long, "a0.0", func(int) []appwire.FileChange { reads++; return nil })
	if over.Files != nil || reads != appSinceFacts {
		t.Fatalf("past the bound: files %v after %d reads", over.Files, reads)
	}
}

// roomConversation is the fake conversation with one task's room journal.
type roomConversation struct {
	*fakeConversation
	journal []byte
	reads   int
}

func (c *roomConversation) TaskRoom(id uint64, tail int) (session.TaskRecord, error) {
	c.reads++
	if id != 4 {
		return session.TaskRecord{}, errors.New("no such task")
	}
	return session.TaskRecord{Kept: true, Journal: c.journal}, nil
}

// A TASK'S PLACE IS COUNTED FROM ITS OWN RECORD, read once a call: the entries
// after it with the task's own ids, and nothing for a place the record does not
// hold or an engine with no room.
func TestAppTaskSinceCountsTheTasksOwnRecord(t *testing.T) {
	conv := &fakeConversation{}
	backend, _, agent := workBackend(t, conv)
	var journal strings.Builder
	journal.WriteString(`{"type":"session","version":1,"id":"n4","cwd":"/tmp/lab"}` + "\n")
	for n := range 3 {
		journal.WriteString(`{"type":"message","role":"user","content":"step ` + strconv.Itoa(n) + `"}` + "\n")
		journal.WriteString(`{"type":"message","role":"assistant","content":"did ` + strconv.Itoa(n) + `"}` + "\n")
	}
	room := &roomConversation{fakeConversation: conv, journal: []byte(journal.String())}
	task := agent.ID + ":4"

	if since, err := backend.TaskSince(task, task+"/a1.0"); err != nil || since.Found {
		t.Fatalf("an engine with no room = %+v, %v", since, err)
	}
	backend.locked(func() { backend.agents[agent.ID].live.conn.Conv = room })
	page, _, err := backend.TaskEntries(task, "", 200)
	if err != nil || len(page) != 6 {
		t.Fatalf("the task's entries = %+v, %v", page, err)
	}
	reads := room.reads
	since, err := backend.TaskSince(task, page[1].ID)
	if err != nil || !since.Found || since.Entries != 4 || since.First != page[2].ID || since.Files == nil || *since.Files != 0 {
		t.Fatalf("since %s = %+v, %v", page[1].ID, since, err)
	}
	if since.Turns != nil && *since.Turns != 2 {
		t.Fatalf("two of the person's turns counted as %d", *since.Turns)
	}
	if room.reads != reads+1 {
		t.Fatalf("one count read the room %d times", room.reads-reads)
	}
	for _, place := range []string{task + "/note0", "a1.0", task + "/u99.0"} {
		if gone, err := backend.TaskSince(task, place); err != nil || gone.Found || gone.Turns != nil || gone.Entries != 0 {
			t.Fatalf("a place the record does not hold (%s) = %+v, %v", place, gone, err)
		}
	}
	if _, err := backend.TaskSince("nonsense", "x"); err == nil {
		t.Fatal("a task id that is not one was counted")
	}
}
