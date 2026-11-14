package tui3

import (
	"context"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/Agent-Field/codeaf/internal/session"
)

// streamingErrand fans out running-turn events like the session, so a second
// Submit really would repeat the reply rather than hiding the defect.
type streamingErrand struct {
	errandAgent
	listeners []chan session.Event
	wakes     chan (<-chan session.Event)
	running   bool
}

func (e *streamingErrand) Submit(_ context.Context, words string) (<-chan session.Event, error) {
	e.sent = append(e.sent, words)
	ch := make(chan session.Event, 256)
	if len(e.sent) == 1 || e.running {
		e.listeners = append(e.listeners, ch)
		e.running = true
	} else {
		ch <- automationProposal(77, words)
		ch <- session.Event{Kind: session.EventTurnDone}
		close(ch)
	}
	return ch, nil
}

func (e *streamingErrand) Wakes() <-chan (<-chan session.Event) { return e.wakes }

func (e *streamingErrand) emit(ev session.Event) {
	for _, ch := range e.listeners {
		ch <- ev
	}
}

func (e *streamingErrand) finish() {
	e.emit(session.Event{Kind: session.EventTurnDone})
	for _, ch := range e.listeners {
		close(ch)
	}
	e.listeners = nil
	e.running = false
}

// errandPump runs sends on the test loop and leaves only channel reads off it.
// This lets the test type between deltas without dropping an outstanding read.
type errandPump struct {
	t        *testing.T
	a        *app
	messages chan tea.Msg
}

func (p *errandPump) run(cmd tea.Cmd) {
	if cmd == nil {
		return
	}
	if strings.Contains(cmdSymbol(cmd), ".waitErrand") {
		go func() { p.messages <- cmd() }()
		return
	}
	switch m := cmd().(type) {
	case tea.BatchMsg:
		for _, next := range m {
			p.run(next)
		}
	case errandMsg:
		p.messages <- m
	}
}

func (p *errandPump) until(done func() bool) {
	p.t.Helper()
	deadline := time.After(3 * time.Second)
	for !done() {
		select {
		case msg := <-p.messages:
			if m, ok := msg.(errandMsg); ok {
				p.run(p.a.errandUpdate(m))
			}
		case <-deadline:
			p.t.Fatal("ask here never reached the expected state")
		}
	}
}

func TestAskHereStreamingFollowUpAndIdleControl(t *testing.T) {
	for _, during := range []bool{true, false} {
		name := "idle"
		if during {
			name = "streaming"
		}
		t.Run(name, func(t *testing.T) {
			lab := newErrandLab(t)
			a := lab.app("")
			a.openHome()
			e := &streamingErrand{wakes: make(chan (<-chan session.Event), 8)}
			a.errand = func(ErrandOrders) (Agent, error) { return e, nil }
			p := &errandPump{t: t, a: a, messages: make(chan tea.Msg, 512)}
			p.run(a.askHere("Explain theatre programmes"))
			ex := theExchange(a)
			defer close(e.wakes)
			prefix := strings.Repeat("chronological order by date. ", 8)
			suffix := strings.Repeat("The principle is clear. ", 40)
			e.emit(text(session.EventTextDelta, prefix))
			p.until(func() bool { return len(ex.rows) > 1 && ex.rows[1].text == prefix })
			if !during {
				e.emit(text(session.EventTextDelta, suffix))
				e.finish()
				p.until(func() bool { return !ex.working })
			}
			typeHome(a, "Remind me in 2 minutes to read the programme")
			p.run(a.homeKey(key("enter")))
			if during {
				// The follow-up must not subscribe to the reply already being drawn.
				if len(e.sent) != 1 {
					t.Errorf("mid-stream follow-up submitted again: %d sends", len(e.sent))
				}
				// A newer draft's tray belongs to that draft, not the queued send.
				ex.chips = []chip{{path: "/tmp/new-draft.txt", file: true}}
				for _, word := range strings.Fields(suffix) {
					e.emit(text(session.EventTextDelta, word+" "))
				}
				e.finish()
			}
			p.until(func() bool { return ex.card != nil && !ex.working })
			if during && (len(ex.chips) != 1 || ex.chips[0].path != "/tmp/new-draft.txt") {
				t.Fatal("queued follow-up consumed the newer draft's attachments")
			}
			var reply strings.Builder
			for _, row := range ex.rows {
				if row.kind == exchangeReply {
					reply.WriteString(row.text)
				}
			}
			if got, want := reply.String(), prefix+suffix; got != want {
				t.Fatalf("reply = %q; want exactly %q", got, want)
			}
			if frame := homeText(a); !strings.Contains(frame, "Save") {
				t.Fatalf("card is not shown in pane:\n%s", frame)
			}
			readCard(t, a)
			p.run(a.homeKey(key(session.AutomationNoKey)))
			if len(e.answered) != 1 {
				t.Fatal("pane did not deliver the card answer")
			}
			if strings.Contains(homeText(a), homeAskThinkWord+" · ") {
				t.Fatal("exchange stayed at thinking")
			}
		})
	}
}

func TestAskHereReceivesWakeCardAfterReply(t *testing.T) {
	lab := newErrandLab(t)
	a := lab.app("")
	a.openHome()
	e := &streamingErrand{wakes: make(chan (<-chan session.Event), 8)}
	a.errand = func(ErrandOrders) (Agent, error) { return e, nil }
	p := &errandPump{t: t, a: a, messages: make(chan tea.Msg, 512)}
	p.run(a.askHere("Explain theatre programmes"))
	ex := theExchange(a)
	defer close(e.wakes)
	e.emit(text(session.EventTextDelta, "The reply."))
	wake := make(chan session.Event, 3)
	wake <- automationProposal(78, "Programme time")
	wake <- session.Event{Kind: session.EventTurnDone}
	close(wake)
	e.wakes <- wake
	e.finish()
	p.until(func() bool { return ex.card != nil && !ex.working })
	if ex.rows[1].text != "The reply." {
		t.Fatalf("reply corrupted: %q", ex.rows[1].text)
	}
	readCard(t, a)
	p.run(a.homeKey(key(session.AutomationNoKey)))
	if len(e.answered) != 1 {
		t.Fatal("wake card is not answerable in pane")
	}
}
