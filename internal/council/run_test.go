package council

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/placegraph"
)

// fakeSink records what the runner wrote and where it escalated.
type fakeSink struct {
	mu        sync.Mutex
	said      []string
	escalated []Escalation
}

func (f *fakeSink) Say(chatID, speaker, text string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.said = append(f.said, speaker+": "+text)
	return nil
}

func (f *fakeSink) Escalate(_ context.Context, e Escalation) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.escalated = append(f.escalated, e)
	return nil
}

// script answers each ask in order and remembers the requests it saw.
type script struct {
	mu       sync.Mutex
	replies  []Answer
	requests []Request
	next     int
}

func (s *script) ask(_ context.Context, req Request) (Answer, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.requests = append(s.requests, req)
	if s.next >= len(s.replies) {
		return Answer{}, errors.New("script exhausted")
	}
	s.next++
	return s.replies[s.next-1], nil
}

func (r *rig) runner(t *testing.T, s *script) (*Runner, *fakeSink) {
	t.Helper()
	sink := &fakeSink{}
	run, err := NewRunner(r.store, r.places, s.ask, sink)
	if err != nil {
		t.Fatal(err)
	}
	return run, sink
}

func said(text string, cost float64) Answer { return Answer{Text: text, CostUSD: cost} }

func TestAgreementInTwoTurnsFilesTheOutcomeInBothPlaces(t *testing.T) {
	r := newRig(t)
	m, s := r.place(t, "Marketing"), r.place(t, "Software")
	if _, _, err := r.places.AddLine(placegraph.Line{PlaceID: s.ID, Text: "v2.4.1 ships Friday", Source: placegraph.LineSource{Kind: placegraph.LineYouWrote}}); err != nil {
		t.Fatal(err)
	}
	sc := &script{replies: []Answer{said("Can we promise v2.4?", 0.01), said("Decided: promise it for v2.4.1", 0.01)}}
	run, sink := r.runner(t, sc)

	c, err := run.Start(context.Background(), m.ID, s.ID, "When do we promise the export fix?")
	if err != nil {
		t.Fatal(err)
	}
	if c.State != StateDecided || c.Turns != 2 || c.Outcome != "promise it for v2.4.1" {
		t.Fatalf("council %+v", c)
	}
	snap, _ := r.places.Snapshot()
	for _, place := range []string{m.ID, s.ID} {
		var hit bool
		for _, l := range snap.Knowledge(place) {
			if l.Text == "promise it for v2.4.1" && l.Source.Kind == placegraph.LineSaidInChat && l.Source.ChatID == c.ChatID {
				hit = true
			}
		}
		if !hit {
			t.Errorf("place %s has no said-in-chat line for the outcome", place)
		}
	}
	if len(sink.escalated) != 0 || len(sink.said) != 2 {
		t.Fatalf("sink %+v", sink)
	}
	if !strings.Contains(sc.requests[1].System, "Software") || !strings.Contains(sc.requests[1].System, "v2.4.1 ships Friday") {
		t.Errorf("second speaker is Software and is shown its own knows: %q", sc.requests[1].System)
	}
	if strings.Contains(sc.requests[0].System, "v2.4.1 ships Friday") {
		t.Error("Marketing was shown Software's knows")
	}
}

func TestSixTurnsWithoutAgreementEscalatesToTheSharedParent(t *testing.T) {
	r := newRig(t)
	co := r.place(t, "Company")
	a, _, _ := r.places.CreatePlace(placegraph.NewPlace{Name: "Marketing", Parents: []string{co.ID}})
	b, _, _ := r.places.CreatePlace(placegraph.NewPlace{Name: "Software", Parents: []string{co.ID}})
	var replies []Answer
	for i := 0; i < 8; i++ {
		replies = append(replies, said("not yet", 0.01))
	}
	sc := &script{replies: replies}
	run, sink := r.runner(t, sc)

	c, err := run.Start(context.Background(), a.ID, b.ID, "topic")
	if err != nil {
		t.Fatal(err)
	}
	if c.State != StateEscalated || c.Turns != TurnCap || len(sc.requests) != TurnCap {
		t.Fatalf("council %+v after %d asks", c, len(sc.requests))
	}
	if len(sink.escalated) != 1 || sink.escalated[0].ToPlace != co.ID || sink.escalated[0].Reason != ReasonTurns {
		t.Fatalf("escalation %+v", sink.escalated)
	}
}

func TestDollarCapEscalatesToThePersonWhenNothingIsShared(t *testing.T) {
	r := newRig(t)
	a, b := r.place(t, "Marketing"), r.place(t, "Software")
	sc := &script{replies: []Answer{said("one", 0.10), said("two", 0.10), said("three", 0.10), said("four", 0.10)}}
	run, sink := r.runner(t, sc)

	c, err := run.Start(context.Background(), a.ID, b.ID, "topic")
	if err != nil {
		t.Fatal(err)
	}
	if c.State != StateEscalated || c.Turns != 3 {
		t.Fatalf("the turn that crosses $0.25 is the last: %+v", c)
	}
	if len(sink.escalated) != 1 || sink.escalated[0].ToPlace != "" || sink.escalated[0].Reason != ReasonDollars {
		t.Fatalf("escalation %+v", sink.escalated)
	}
}

func TestAPersonsMessagePausesThenSteersTheNextTurn(t *testing.T) {
	r := newRig(t)
	a, b := r.place(t, "Marketing"), r.place(t, "Software")
	entered := make(chan struct{})
	release := make(chan struct{})
	var once sync.Once
	sc := &script{replies: []Answer{said("first", 0.01), said("Decided: ship on Monday", 0.01)}}
	gated := func(ctx context.Context, req Request) (Answer, error) {
		ans, err := sc.ask(ctx, req)
		once.Do(func() { close(entered); <-release })
		return ans, err
	}
	sink := &fakeSink{}
	run, _ := NewRunner(r.store, r.places, gated, sink)
	c, err := r.store.Begin(a.ID, b.ID, "topic")
	if err != nil {
		t.Fatal(err)
	}
	done := make(chan Council, 1)
	go func() { got, _ := run.Run(context.Background(), c.ID); done <- got }()

	<-entered
	if _, err := run.Pause(c.ID); err != nil {
		t.Fatal(err)
	}
	close(release)
	// The turn in flight finishes, then the loop holds.
	deadline := time.Now().Add(5 * time.Second)
	for {
		cur, _ := r.store.Get(c.ID)
		if cur.Turns == 1 && cur.State == StatePaused {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("never paused after one turn: %+v", cur)
		}
		time.Sleep(5 * time.Millisecond)
	}
	select {
	case <-done:
		t.Fatal("a paused discussion kept going")
	case <-time.After(50 * time.Millisecond):
	}

	if _, err := run.Steer(c.ID, "  Monday, not Friday "); err != nil {
		t.Fatal(err)
	}
	got := <-done
	if got.State != StateDecided || got.Turns != 2 {
		t.Fatalf("person messages are not turns: %+v", got)
	}
	if !strings.Contains(sc.requests[1].User, "Monday, not Friday") {
		t.Errorf("the next turn was not steered: %q", sc.requests[1].User)
	}
	if sink.said[1] != SpeakerPerson+": Monday, not Friday" {
		t.Errorf("chat lines %v", sink.said)
	}
}

func TestACappedTopicCannotReopenInsideTheHour(t *testing.T) {
	r := newRig(t)
	a, b := r.place(t, "Marketing"), r.place(t, "Software")
	sc := &script{replies: []Answer{said("Decided: yes", 0.01)}}
	run, _ := r.runner(t, sc)
	if _, err := run.Start(context.Background(), a.ID, b.ID, "Topic"); err != nil {
		t.Fatal(err)
	}
	// Nothing is asked: the store refuses first.
	if _, err := run.Start(context.Background(), b.ID, a.ID, "  topic "); !errors.Is(err, ErrCooldown) {
		t.Fatalf("err %v", err)
	}
	if len(sc.requests) != 1 {
		t.Fatalf("a refused start asked the model: %d", len(sc.requests))
	}
	r.clock.Advance(Cooldown)
	sc.replies = append(sc.replies, said("Decided: still yes", 0.01))
	if _, err := run.Start(context.Background(), a.ID, b.ID, "topic"); err != nil {
		t.Fatalf("after the hour: %v", err)
	}
}

func TestAFailedReplyLeavesTheDiscussionPausedAndResumable(t *testing.T) {
	r := newRig(t)
	a, b := r.place(t, "Marketing"), r.place(t, "Software")
	sc := &script{replies: []Answer{said("first", 0.01)}}
	run, _ := r.runner(t, sc)
	c, err := run.Start(context.Background(), a.ID, b.ID, "topic")
	if err == nil {
		t.Fatal("exhausted script should fail the second turn")
	}
	if c.State != StatePaused || c.Turns != 1 {
		t.Fatalf("council %+v", c)
	}
	sc.mu.Lock()
	sc.replies = append(sc.replies, said("Decided: ok", 0.01))
	sc.mu.Unlock()
	if _, err := run.Resume(c.ID); err != nil {
		t.Fatal(err)
	}
	got, err := run.Run(context.Background(), c.ID)
	if err != nil || got.State != StateDecided {
		t.Fatalf("got %+v err %v", got, err)
	}
}

func TestNearestSharedParentPrefersTheCloserAncestor(t *testing.T) {
	r := newRig(t)
	top := r.place(t, "Company")
	mid, _, _ := r.places.CreatePlace(placegraph.NewPlace{Name: "Product", Parents: []string{top.ID}})
	a, _, _ := r.places.CreatePlace(placegraph.NewPlace{Name: "Web", Parents: []string{mid.ID}})
	b, _, _ := r.places.CreatePlace(placegraph.NewPlace{Name: "Api", Parents: []string{mid.ID}})
	snap, _ := r.places.Snapshot()
	if got := NearestSharedParent(snap, a.ID, b.ID); got != mid.ID {
		t.Fatalf("got %q want %q", got, mid.ID)
	}
	if got := NearestSharedParent(snap, a.ID, mid.ID); got != mid.ID {
		t.Fatalf("a place above the other is the shared parent, got %q", got)
	}
	lone := r.place(t, "Elsewhere")
	snap, _ = r.places.Snapshot()
	if got := NearestSharedParent(snap, a.ID, lone.ID); got != "" {
		t.Fatalf("got %q", got)
	}
}
