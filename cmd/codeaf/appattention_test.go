package main

import (
	"context"
	"errors"
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

// WHAT CAME AFTER A PLACE IS COUNTED FROM THE SHAPES: the person's messages
// are turns, what the conversation did on its own first is one more, files are
// distinct paths, and past the bound the files count is not said at all.
func TestAppSinceCountsTurnsAndFilesAfterAPlace(t *testing.T) {
	at := time.Date(2026, 10, 15, 9, 0, 0, 0, time.UTC)
	all := []appwire.Entry{
		{ID: "u0", Kind: appwire.EntryUser, At: at},
		{ID: "a0.0", Kind: appwire.EntryAnswer, At: at.Add(time.Minute)},
		{ID: "tc1", Kind: appwire.EntryTool, At: at.Add(2 * time.Minute)},
		{ID: "a0.1", Kind: appwire.EntryAnswer, At: at.Add(3 * time.Minute)},
		{ID: "u1", Kind: appwire.EntryUser, At: at.Add(4 * time.Minute)},
		{ID: "tc2", Kind: appwire.EntryTool, At: at.Add(5 * time.Minute)},
		{ID: "a1.0", Kind: appwire.EntryAnswer, At: at.Add(6 * time.Minute)},
	}
	files := map[int][]appwire.FileChange{2: {{Path: "a.go"}, {Path: "b.go"}}, 5: {{Path: "a.go"}}}
	read := func(i int) []appwire.FileChange { return files[i] }

	since := appSinceOf(all, "a0.0", read)
	if !since.Found || since.Turns != 2 || since.Entries != 5 || since.First != "tc1" || since.Files == nil || *since.Files != 2 {
		t.Fatalf("since a0.0 = %+v", since)
	}
	if since.At == nil || !since.At.Equal(at.Add(time.Minute)) || since.LastAt == nil || !since.LastAt.Equal(at.Add(6*time.Minute)) {
		t.Fatalf("the moments = %+v", since)
	}
	if nothing := appSinceOf(all, "a1.0", read); !nothing.Found || nothing.Turns != 0 || nothing.Entries != 0 || *nothing.Files != 0 {
		t.Fatalf("nothing new = %+v", nothing)
	}
	if gone := appSinceOf(all, "a9.9", read); gone.Found || gone.Files != nil {
		t.Fatalf("a place the record does not hold = %+v", gone)
	}

	long := []appwire.Entry{{ID: "u0", Kind: appwire.EntryUser}}
	reads := 0
	for n := 0; n <= appSinceFacts; n++ {
		long = append(long, appwire.Entry{ID: "t" + time.Duration(n).String(), Kind: appwire.EntryTool})
	}
	over := appSinceOf(long, "u0", func(int) []appwire.FileChange { reads++; return nil })
	if over.Files != nil || reads != appSinceFacts {
		t.Fatalf("past the bound: files %v after %d reads", over.Files, reads)
	}
}
