package remote

import (
	"sync"
	"testing"

	"github.com/Agent-Field/aforge-v2/internal/session"
)

// The source is scripted; the client, server, standing subscription and frames
// are real. No running turn is available to carry the name in these tests.
type titleLaneAgent struct {
	*fakeAgent
	titleMu  sync.Mutex
	watchers map[chan session.Event]bool
}

func (a *titleLaneAgent) WatchTitle() (<-chan session.Event, func()) {
	ch := make(chan session.Event, 1)
	a.titleMu.Lock()
	if a.watchers == nil {
		a.watchers = make(map[chan session.Event]bool)
	}
	a.watchers[ch] = true
	if title := a.Title(); title != "" {
		ch <- session.Event{Kind: session.EventTitleChanged, Text: title}
	}
	a.titleMu.Unlock()
	var once sync.Once
	return ch, func() { once.Do(func() { a.titleMu.Lock(); delete(a.watchers, ch); close(ch); a.titleMu.Unlock() }) }
}

func (a *titleLaneAgent) named(title string) {
	a.titleMu.Lock()
	defer a.titleMu.Unlock()
	a.name(title)
	for ch := range a.watchers {
		ch <- session.Event{Kind: session.EventTitleChanged, Text: title}
	}
}

func TestIdleHostedTitleArrivesWithCurrentFacts(t *testing.T) {
	far := &titleLaneAgent{fakeAgent: &fakeAgent{}}
	loop := laneLoop(t, far)
	names, stop := loop.Client.Agent().WatchTitle()
	defer stop()
	waitFor(t, "standing title subscription", func() bool { far.titleMu.Lock(); defer far.titleMu.Unlock(); return len(far.watchers) == 1 })
	asked := loop.CallsMade()
	far.named("cancellation ownership")
	ev := nextLane(t, names)
	if ev.Kind != session.EventTitleChanged || ev.Text != "cancellation ownership" || loop.Client.Agent().Title() != ev.Text {
		t.Fatalf("title event/cache disagree: %+v / %q", ev, loop.Client.Agent().Title())
	}
	if loop.CallsMade() != asked {
		t.Fatal("title update needed another request")
	}
}

func TestHostedTitleReplaysToNewSubscription(t *testing.T) {
	far := &titleLaneAgent{fakeAgent: &fakeAgent{title: "already named"}}
	loop := laneLoop(t, far)
	names, stop := loop.Client.Agent().WatchTitle()
	defer stop()
	ev := nextLane(t, names)
	if ev.Text != "already named" || loop.Client.Agent().Title() != ev.Text {
		t.Fatalf("replay lost title: %+v", ev)
	}
}

func titlePayload(rev uint64, name string) []byte {
	return mustClientJSON(FactsPush{Rev: rev, Facts: session.Facts{Title: name}})
}

func TestTitleAndStatusPushOrderingNeverLosesTheVisibleName(t *testing.T) {
	for _, order := range []string{"title first", "newer status first"} {
		t.Run(order, func(t *testing.T) {
			lane := newStream()
			c := &Client{titles: lane}
			c.facts.fill(&FactsPush{Rev: 1})
			newer := mustClientJSON(FactsPush{Rev: 3, Facts: session.Facts{Title: "repair ownership", Spent: session.Usage{CostUSD: 0.2}}})
			if order == "title first" {
				c.titleFrame(titlePayload(2, "repair ownership"))
				c.factsFrame(newer)
			} else {
				c.factsFrame(newer)
				c.titleFrame(titlePayload(2, "repair ownership"))
			}
			// An older unnamed snapshot is harmless in either arrival order.
			c.factsFrame(mustClientJSON(FactsPush{Rev: 1}))
			lane.finish()
			var events []session.Event
			for ev := range lane.events() {
				events = append(events, ev)
			}
			if len(events) != 1 || events[0].Text != "repair ownership" {
				t.Fatalf("visible title notifications = %+v", events)
			}
			if got := c.facts.read(); got.Title != "repair ownership" || got.Spent.CostUSD != 0.2 {
				t.Fatalf("facts regressed: %+v", got)
			}
		})
	}
}

func TestPreviousConversationTitleCannotCrossNewWelcome(t *testing.T) {
	lane := newStream()
	c := &Client{titles: lane}
	// The new conversation's welcome is newer than every old lane snapshot.
	c.facts.fill(&FactsPush{Rev: 10, Facts: session.Facts{Title: "current conversation"}})
	c.titleFrame(titlePayload(9, "old conversation"))
	c.factsFrame(mustClientJSON(FactsPush{Rev: 8, Facts: session.Facts{Title: "older conversation"}}))
	lane.finish()
	for ev := range lane.events() {
		t.Errorf("old conversation reached current title lane: %+v", ev)
	}
	if got := c.facts.read().Title; got != "current conversation" {
		t.Fatalf("current title = %q", got)
	}
}

func TestTitleSubscriptionSurvivesARepairedConnection(t *testing.T) {
	for _, duringGap := range []bool{false, true} {
		name := "still pending"
		if duringGap {
			name = "named while disconnected"
		}
		t.Run(name, func(t *testing.T) {
			first := Welcome{Version: Version, SessionFile: "/j.jsonl", Persistent: true, Facts: &FactsPush{Rev: 1}}
			second := first
			second.Facts = &FactsPush{Rev: 2}
			if duringGap {
				second.Facts.Facts.Title = "reconnected name"
			}
			client, r := roam(t, script{welcome: first}, script{welcome: second})
			titles, stop := client.Agent().WatchTitle()
			defer stop()
			sawWatch := func(e *scripted) bool {
				if e == nil {
					return false
				}
				e.mu.Lock()
				defer e.mu.Unlock()
				for _, call := range e.calls {
					if call.Method == MethodTitleWatch {
						return true
					}
				}
				return false
			}
			waitFor(t, "first title subscription", func() bool { return sawWatch(r.engine(0)) })
			r.cut(0)
			waitFor(t, "replacement title subscription", func() bool { return sawWatch(r.engine(1)) })
			if !duringGap {
				r.engine(1).send(Frame{Kind: string(laneTitle), Payload: titlePayload(3, "reconnected name")})
			}
			ev := nextLane(t, titles)
			if ev.Text != "reconnected name" || client.Agent().Title() != ev.Text {
				t.Fatalf("reconnect lost name: %+v / %q", ev, client.Agent().Title())
			}
		})
	}
}
