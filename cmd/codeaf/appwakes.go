package main

import (
	"strings"
	"sync"
	"time"

	"github.com/Agent-Field/codeaf/internal/appwire"
	"github.com/Agent-Field/codeaf/internal/guard"
	"github.com/Agent-Field/codeaf/internal/session"
)

// ── turns the conversation starts on its own ────────────────────────────────
//
// A conversation starts a turn nobody typed when something lands that it must
// answer: a task it handed out finishes, a crew's manager sends it a directive,
// a member's report wakes its manager, a member reads its first brief. The
// engine hands each such turn to whoever watches for them — across a
// connection, as a turn that connection did not start — the same stream a sent
// message gets back, and the terminal draws it as any other turn. The app does the same: a woken turn is a turn on
// the wire, with no message of the person's in front of it.
//
// A MEMBER THE PERSON PAUSED does not take one. The crew's app layer keeps the
// pause (appcrewlayer.go), and a turn woken while it holds is stopped at once;
// what woke it is still waiting on the crew's log for its next turn.

// appWakeDoor is a conversation held in this process that hands over the
// turns it starts itself.
type appWakeDoor interface {
	WatchWakes() (<-chan (<-chan session.Event), func())
}

// appTurnElsewhere is one turn the app did not start: the words that opened
// it when another window sent some, and its events.
type appTurnElsewhere struct {
	said   string
	events <-chan session.Event
}

// watchWakes adopts every turn the conversation starts on its own while it is
// held, and answers the function that stops watching.
//
// A CONVERSATION REACHED ACROSS A CONNECTION — every one the app opens — does
// not hand its wakes over as a local subscription: the engine announces each
// as a turn this connection did not start (internal/remote's wakelane.go), the
// same way it announces a turn another window sent. Both are adopted here, and
// a wake held only in this process is read from the session's own lane.
func (b *appBackend) watchWakes(a *appAgent, live *appLive) func() {
	if live.conn.Follow != nil {
		turns := live.conn.Follow()
		stop := make(chan struct{})
		guard.Go("app/wakes", func() {
			for {
				select {
				case <-stop:
					return
				case turn := <-turns:
					// A replay marker carries no turn: the welcome already
					// told the app what was running.
					if turn.Events == nil {
						continue
					}
					b.wokenTurn(a, live, appTurnElsewhere{said: turn.Said, events: turn.Events})
				}
			}
		})
		var once sync.Once
		return func() { once.Do(func() { close(stop) }) }
	}
	door, ok := live.conn.Conv.(appWakeDoor)
	if !ok {
		return func() {}
	}
	lanes, stop := door.WatchWakes()
	guard.Go("app/wakes", func() {
		for events := range lanes {
			b.wokenTurn(a, live, appTurnElsewhere{events: events})
		}
	})
	return stop
}

// wokenTurn is one turn the app did not start, pumped like any other once the
// turn before it has said its last.
func (b *appBackend) wokenTurn(a *appAgent, live *appLive, elsewhere appTurnElsewhere) {
	events := elsewhere.events
	var done chan struct{}
	b.locked(func() { done = live.pumpDone })
	if done != nil {
		<-done
	}
	turn, n, id, ok := uint64(0), 0, "", false
	transcript := ""
	var view appwire.Agent
	b.locked(func() {
		if a.live != live || live.running || live.closed {
			return
		}
		ok = true
		id = a.view.ID
		// A woken turn answers under the person's last message, as the
		// listing names it: its answers follow the ones already there. A turn
		// another window sent is a message of the person's of its own.
		n = max(a.users-1, 0)
		if elsewhere.said != "" {
			n = a.users
			a.users = n + 1
		}
		transcript = a.transcript
		live.running, live.stopping = true, false
		turn = b.turns.Add(1)
		live.turn, live.steps, live.steers = turn, 0, 0
		live.retries, live.retrySaid, live.retryPhase = 0, false, ""
		live.pumpDone = make(chan struct{})
		// A TURN THAT WAKES WHILE AN ASK WAITS ON THE PERSON STILL NEEDS THEM,
		// exactly as a turn the person sends does ([appBackend.startLocked]).
		// A woken turn's question can reach the question lane before this
		// turn is adopted here, and a crew member mostly runs woken turns: a
		// plain working would put "running" on its row and on its crew while
		// it sat blocked on the person's word.
		a.view.State = appwire.StateWorking
		if live.needsYou() {
			a.view.State = appwire.StateNeedsYou
		}
		a.view.UpdatedAt = b.now()
		view = a.view
	})
	if !ok {
		for range events {
		}
		return
	}
	// Within this hold the last turn's counts are known; a conversation just
	// opened reads its answers from the transcript, and its live notices
	// began with this hold.
	known := elsewhere.said != ""
	if known {
		b.locked(func() { live.msgBase, live.noteBase = 0, 0 })
	}
	if !known {
		b.locked(func() {
			if live.lastN == n && (live.lastMsgs > 0 || live.lastNotices > 0) {
				live.msgBase, live.noteBase, known = live.lastMsgs, live.lastNotices, true
			}
		})
	}
	if !known {
		msgs := appAnswersUnder(id, transcript, n)
		b.locked(func() { live.msgBase, live.noteBase = msgs, 0 })
	}
	if b.crewPaused(id) {
		// The person paused this member: the turn ends before it does anything,
		// and the line that woke it stays on the crew's log.
		live.conn.Conv.Interrupt()
	}
	live.box.Event(appwire.EventAgentChanged, appwire.AgentChanged{Agent: view})
	live.box.Event(appwire.EventTurnStarted, appwire.TurnStarted{Agent: id, Turn: turn})
	if elsewhere.said != "" {
		live.box.Event(appwire.EventEntryAdded, appwire.EntryAdded{Entry: appwire.Entry{
			ID: appUserEntryID(n), Agent: id, Turn: turn, Kind: appwire.EntryUser, At: b.now(), Text: elsewhere.said,
		}})
	}
	long := b.watchLong(id, live, turn, b.now())
	b.newTurnCalls(a)
	tails := b.tickTools(a, id, live)
	defer long.Stop()
	defer tails()
	b.pump(a, id, live, turn, n, elsewhere.said, nil, events)
	b.sendQueued(a, live)
}

// appAnswersUnder counts the answers the transcript already holds under the
// person's message n, so a woken turn's answers are named after them rather
// than over them.
func appAnswersUnder(agent, transcript string, n int) int {
	if strings.TrimSpace(transcript) == "" {
		return 0
	}
	items := appRecordItems(session.ReadTranscript(transcript))
	prefix := strings.TrimSuffix(appAnswerEntryID(n, 0), "0")
	answers := 0
	for _, entry := range appEntriesOf(agent, items, time.Time{}, nil) {
		if strings.HasPrefix(entry.ID, prefix) {
			answers++
		}
	}
	return answers
}
