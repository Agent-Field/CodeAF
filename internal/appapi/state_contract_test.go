package appapi

import (
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/session"
)

// stateContract drives one agent in the shape `app serve` runs: the engine's
// NeedsPerson is a pushed fact that can lag or not cover a question, the
// question lane announces the card, and answers pass the Inbox Undo window.
type stateContract struct {
	*askFixture
	t     *testing.T
	clock *inboxTestClock
	mark  uint64
}

func newStateContract(t *testing.T) *stateContract {
	f := newAskFixture(t)
	f.server.inbox.cancel()
	<-f.server.inbox.done
	t.Cleanup(f.server.Close)
	a := f.registry
	f.server.SetInboxSource("engine", &engineInboxSource{s: f.server, a: a})
	c := &stateContract{askFixture: f, t: t, clock: &inboxTestClock{at: a.now()}}
	f.server.inbox.now = func() time.Time { return c.clock.at }
	f.server.inbox.after = func(_ time.Duration, fn func()) *time.Timer {
		c.clock.callbacks = append(c.clock.callbacks, fn)
		return nil
	}
	a.mu.Lock()
	conn := a.live[f.id].conn
	a.watchAsks(f.id, &conn)
	a.live[f.id].conn = conn
	a.mu.Unlock()
	// The engine takes an answer after the send returns, as the real one does.
	f.door.lag = true
	if err := a.UpdateState(f.id, "running"); err != nil {
		t.Fatal(err)
	}
	return c
}

func (c *stateContract) facts(needs bool) {
	c.engine.mu.Lock()
	c.engine.needs = needs
	c.engine.mu.Unlock()
}

func (c *stateContract) stored() string {
	a := c.registry
	a.mu.Lock()
	defer a.mu.Unlock()
	state := ""
	_ = a.disk(func(d *agentDisk) error { state = d.Records[c.id].Agent.State; return nil }, false)
	return state
}

// step runs one action and pins what the desk reads after it: the stored
// state, the view, and whether agent.changed was published for the agent.
func (c *stateContract) step(name string, act func(), want string, wantChanged bool) {
	c.t.Helper()
	_, mark, _, _, _ := c.server.Events().read(0)
	act()
	batch, _, _, _, _ := c.server.Events().read(mark)
	changed := false
	for _, event := range batch {
		changed = changed || event.kind == EventAgentChanged && event.agent == c.id
	}
	view := readAgent(c.t, c.request(c.t, "GET", "/app/v1/agents/"+c.id, ""), 200).State
	if stored := c.stored(); stored != want || view != want || changed != wantChanged {
		c.t.Fatalf("%s: stored %s, view %s, agent.changed %v; want %s, changed %v", name, stored, view, changed, want, wantChanged)
	}
}

func (c *stateContract) windowEnds() {
	c.clock.at = c.clock.at.Add(inboxAnswerHold)
	c.clock.callbacks[len(c.clock.callbacks)-1]()
}

// The state contract for a question that waits on the person, for both kinds:
// a permission ask, which the engine's pushed word covers, and a question the
// model asks, which it does not.
func TestAgentStateContractForBothQuestionKinds(t *testing.T) {
	asked := time.Date(2026, 10, 13, 12, 0, 0, 0, time.UTC)
	kinds := []struct {
		name     string
		question session.Question
		answer   string
		pushed   bool
	}{
		{"permission", permissionQuestion(51), `{"verb":"allow_once"}`, true},
		{"model question", session.Question{ID: 52, Kind: session.QuestionAsk, Ask: session.AskClarification, Head: "Which one?", Asked: asked, Blocking: session.Blocking{Turn: true}}, `{"words":"the first"}`, false},
	}
	for _, kind := range kinds {
		for _, sent := range []bool{true, false} {
			name := kind.name + " sent"
			if !sent {
				name = kind.name + " send fails"
			}
			t.Run(name, func(t *testing.T) {
				c := newStateContract(t)
				q := kind.question
				c.step("card waiting", func() {
					c.facts(kind.pushed)
					c.door.put(q)
					c.door.events <- session.Event{Kind: session.EventQuestion, Question: &q}
					// The question lane is its own goroutine; this waits for
					// its one store, with no bound on how soon it comes.
					turnWait(t, func() bool { return c.stored() == "needs_you" })
				}, "needs_you", true)
				item := "/app/v1/inbox/" + inboxItems(t, c.server)[0].ID
				answer := func() { turnStatus(t, inboxReq(t, c.server, "POST", item+"/answer", kind.answer), 200) }
				c.step("answered inside the Undo window", answer, "running", true)
				c.step("Undo", func() { turnStatus(t, inboxReq(t, c.server, "POST", item+"/undo", `{}`), 200) }, "needs_you", true)
				c.step("answered again", answer, "running", true)
				if !sent {
					c.door.refused = true
					c.step("failed send", c.windowEnds, "needs_you", true)
					return
				}
				c.step("window end before the engine takes the answer", c.windowEnds, "running", false)
				// The pushed word still says what it said while the card waited.
				c.step("answer consumed, engine word lags", c.door.consume, "running", false)
				c.step("engine word catches up", func() { c.facts(false) }, "running", false)
			})
			if !sent || !kind.pushed {
				continue
			}
			// A later need the engine reports with no question (a spending cap)
			// is read again once work has shown after the answer.
			t.Run(kind.name+" later need", func(t *testing.T) {
				c := newStateContract(t)
				q := kind.question
				c.facts(true)
				c.door.put(q)
				item := "/app/v1/inbox/" + inboxItems(t, c.server)[0].ID
				turnStatus(t, inboxReq(t, c.server, "POST", item+"/answer", kind.answer), 200)
				c.windowEnds()
				c.step("answer consumed, engine word lags", c.door.consume, "running", false)
				c.registry.workShown(c.registry.live[c.id])
				if view := readAgent(t, c.request(t, "GET", "/app/v1/agents/"+c.id, ""), 200).State; view != "needs_you" {
					t.Fatalf("a need after work showed reads %s", view)
				}
			})
		}
	}
}
