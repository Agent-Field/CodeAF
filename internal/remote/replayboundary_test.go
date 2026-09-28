package remote

import (
	"github.com/Agent-Field/codeaf/internal/session"
	"testing"
)

type cursorObservedAgent struct {
	*observedAgent
	cursor session.ReplayCursor
}

func (a *cursorObservedAgent) AttachReplayCursor() ([]session.DisplayEntry, <-chan session.Event, func(), session.ReplayCursor) {
	entries, events, stop := a.AttachReplay()
	return entries, events, stop, a.cursor
}

func TestObserveOwnsCoveredFollowButNotTheNextTurn(t *testing.T) {
	cursor := session.ReplayCursor{Owner: "engine-one", Turn: 3}
	far := &cursorObservedAgent{observedAgent: &observedAgent{fakeAgent: &fakeAgent{model: "m"}}, cursor: cursor}
	loop, err := Loopback(Hello{}, Options{Boot: func(Hello) (*Engine, error) { return &Engine{Agent: far}, nil }})
	if err != nil {
		t.Fatal(err)
	}
	defer loop.Close()
	agent := loop.Client.Agent()
	_, observer, stop := agent.AttachReplay()
	defer stop()
	ev := <-observer
	if !ev.ReplayObserved || agent.ReplayCovers(ev) {
		t.Fatal("atomic observer lost ownership")
	}
	old := session.Event{Kind: session.EventTextDelta, Text: "same answer", ReplayCursor: cursor}
	s := loop.Client.stream(101)
	s.push(1, mustJSON(WireEvent(old)))
	s.finish()
	loop.Client.followStream(101, "check")
	turn := <-loop.Client.Follow()
	if turn.Covered == nil || !turn.Covered() {
		t.Fatal("completed canonical replay not covered")
	}
	if !agent.ReplayCovers(old) {
		t.Fatal("already-dispatched old event not covered")
	}
	newer := old
	newer.ReplayCursor.Turn++
	s = loop.Client.stream(102)
	loop.Client.followStream(102, "again")
	next := <-loop.Client.Follow()
	if next.Covered() {
		t.Fatal("pre-token new turn was discarded")
	}
	s.push(1, mustJSON(WireEvent(newer)))
	s.finish()
	if next.Covered() || agent.ReplayCovers(newer) {
		t.Fatal("new live delta discarded")
	}
	observerEvent(t, next.Events, "same answer")
	replaced := old
	replaced.ReplayCursor.Owner = "engine-two"
	if agent.ReplayCovers(replaced) {
		t.Fatal("restarted engine inherited old replay floor")
	}
	loop.Client.rememberReplay(session.ReplayCursor{Owner: "engine-one", Turn: 1})
	if !agent.ReplayCovers(old) {
		t.Fatal("delayed older snapshot moved boundary backward")
	}
}
