package tui3

// ── THE CARD'S LIVE LINK ────────────────────────────────────────────────────
//
// A door that can ask to be let in by link ([LivePairLinker]) gives the opened
// card a link that is good while the card stays open: it is shown the moment it
// exists, and the card ends on the sentence for how the wait ended - Paired when
// a device said yes, the reason when it did not. Closing the card takes the
// request back. The work runs off the frame; its news comes down one road, like
// the /pair panel's, and a late word from a card that was closed is dropped.

import (
	"context"

	tea "charm.land/bubbletea/v2"

	"github.com/Agent-Field/codeaf/internal/pair"
)

// LivePairLinker is the face of a [Pairing] door that asks to join by link and
// waits for the answer. It calls ui.Invited once the link exists and returns
// when the wait is over. Every error it returns is already a sentence.
type LivePairLinker interface {
	PairByLink(ctx context.Context, ui pair.LinkUI) (pair.LinkJoined, error)
}

// cardRun is one wait for an answer: the context that ends it and its road.
type cardRun struct {
	ctx    context.Context
	cancel context.CancelFunc
	events chan cardEvent
}

// cardEvent is one thing the door tells the card; each kind files itself.
type cardEvent interface{ apply(m *addMachine) }

type (
	cardInvited struct{ link, check string }
	cardEnded   struct {
		line   string
		paired bool
	}
)

func (e cardInvited) apply(m *addMachine) { m.link, m.check = e.link, e.check }
func (e cardEnded) apply(m *addMachine)   { m.ended, m.paired = e.line, e.paired }

// cardMsg is one event arriving; the run names the wait it belongs to.
type cardMsg struct {
	run   *cardRun
	event cardEvent
}

func (r *cardRun) send(e cardEvent) {
	select {
	case r.events <- e:
	case <-r.ctx.Done():
	}
}

// Invited is the door showing the link.
func (r *cardRun) Invited(in pair.Invite) {
	r.send(cardInvited{link: in.Ref.URL(), check: in.Check})
}

// wait hands the next event to the loop, and nothing once the wait is closed.
func (r *cardRun) wait() tea.Cmd {
	return func() tea.Msg {
		select {
		case e := <-r.events:
			return cardMsg{run: r, event: e}
		case <-r.ctx.Done():
			return nil
		}
	}
}

// startLive begins the wait and returns the command that reads its news.
func (a *app) startLive(door LivePairLinker) tea.Cmd {
	ctx, cancel := context.WithCancel(context.Background())
	run := &cardRun{ctx: ctx, cancel: cancel, events: make(chan cardEvent, 4)}
	a.addMachine.run = run
	go func() {
		joined, err := door.PairByLink(ctx, run)
		run.send(endOf(joined, err))
	}()
	return run.wait()
}

// endOf is how a finished wait reads on the card.
func endOf(joined pair.LinkJoined, err error) cardEvent {
	if err != nil {
		return cardEnded{line: err.Error()}
	}
	return cardEnded{line: pair.JoinedFleetLine(joined.Fleet.Workspaces), paired: true}
}

// stopLive takes the request back, if one is waiting.
func (m *addMachine) stopLive() {
	if m.run != nil {
		m.run.cancel()
	}
	m.run, m.link, m.check, m.ended, m.paired = nil, "", "", "", false
}

// tookCard files one event and re-arms the road until the wait has ended.
func (a *app) tookCard(msg cardMsg) tea.Cmd {
	m := &a.addMachine
	if msg.run != m.run || !m.open {
		return nil
	}
	msg.event.apply(m)
	a.touch()
	if _, over := msg.event.(cardEnded); over {
		return nil
	}
	return m.run.wait()
}

// liveRows are the card's rows for the link or for how the wait ended.
func (m addMachine) liveRows(ink, dim func(string) string, width int) []string {
	switch {
	case m.ended != "":
		return []string{ink(fit(m.ended, width))}
	case m.link == "":
		return []string{dim(addMachineMaking)}
	}
	rows := []string{ink(fit(m.link, width))}
	if m.check != "" {
		rows = append(rows, dim(fit(addMachineCheck+m.check, width)))
	}
	return rows
}
