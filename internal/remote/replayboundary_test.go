package remote

import (
	"github.com/Agent-Field/codeaf/internal/session"
	"io"
	"sync"
	"testing"
	"time"
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
	loop.Client.rememberReplay(session.ReplayCursor{Owner: "engine-one", Turn: 1}, 0)
	if !agent.ReplayCovers(old) {
		t.Fatal("delayed older snapshot moved boundary backward")
	}
}

func TestObserverReconnectRequestsFreshAtomicReplay(t *testing.T) {
	far := &cursorObservedAgent{observedAgent: &observedAgent{fakeAgent: &fakeAgent{model: "m"}}, cursor: session.ReplayCursor{Owner: "engine", Turn: 1}}
	sess := NewSession(&Engine{Agent: far}, true)
	defer sess.Close()
	var mu sync.Mutex
	var pipes []io.ReadWriteCloser
	dial := func() (io.ReadWriteCloser, error) {
		surface, engine := Pipe()
		mu.Lock()
		pipes = append(pipes, surface)
		mu.Unlock()
		go func() {
			_ = ServeAttach(engine, engine, AttachOptions{Open: func(Hello) (*Session, error) { return sess, nil }})
			_ = engine.Close()
		}()
		return surface, nil
	}
	client, err := Roam("test", Hello{}, Roaming{Dial: dial, Window: 3 * time.Second})
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	_, events, stop := client.Agent().AttachReplay()
	defer stop()
	observerEvent(t, events, "before")
	mu.Lock()
	first := pipes[0]
	mu.Unlock()
	_ = first.Close()
	select {
	case next := <-client.Follow():
		if !next.Replay || next.Events != nil {
			t.Fatalf("reconnect reused stale observer: %+v", next)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("reconnect did not request replay")
	}
	_, fresh, leave := client.Agent().AttachReplay()
	defer leave()
	observerEvent(t, fresh, "before")
	far.mu.Lock()
	far.readers[len(far.readers)-1] <- session.Event{Kind: session.EventTextDelta, Text: "after reconnect", ReplayCursor: far.cursor}
	far.mu.Unlock()
	ev := <-fresh
	if ev.Text != "after reconnect" || client.Agent().ReplayCovers(ev) {
		t.Fatalf("new observer tail lost: %+v", ev)
	}
}

func TestAnOldObserveResponseCannotReplaceNewOwnership(t *testing.T) {
	c := &Client{}
	c.rememberReplay(session.ReplayCursor{Owner: "replacement", Turn: 2}, 20)
	c.rememberReplay(session.ReplayCursor{Owner: "previous", Turn: 99}, 10)
	if c.replayCursor.Owner != "replacement" {
		t.Fatal("old response replaced current owner")
	}
}
