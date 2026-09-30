package tui3

import (
	"context"
	"errors"
	"strings"
	"sync"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/Agent-Field/codeaf/internal/remote"
	"github.com/Agent-Field/codeaf/internal/roles"
	"github.com/Agent-Field/codeaf/internal/session"
)

// THE THREE COMMANDS ONTO WHAT codeaf REMEMBERS ABOUT YOU.
//
// Memory is otherwise invisible by design: a small model decides before each
// message which remembered lines bear on it and the rest of the time nothing is
// said about any of it (internal/session's memory.go). That is the right default
// and it is also exactly why these three exist — a thing that quietly carries
// facts about a person between sessions has to be a thing that person can read,
// add to and empty by hand.
//
// Saves, removals and queries answer in the transcript so their receipts stay
// scrollable. Bare /memory and /memories open the place when it is available.

// memoryAgent is the slice of *session.Agent these three need. It is asserted
// rather than added to [Agent] for [taskAgent]'s reason: memory is OPTIONAL —
// a session with no store simply does not have it, and every scripted agent in
// this package's tests has never heard of one.
type memoryAgent interface {
	// Remembers is the wiring question, and it is on this interface rather than
	// left to an error because the two answers read differently on screen:
	// nothing remembered YET is an empty store, and memory OFF is a row in
	// /settings. A surface that could only see a failed call would say the first
	// when it meant the second.
	Remembers() bool
	// Remember keeps one thing and answers with the title it landed under.
	Remember(text string) (string, error)
	// Forget drops the best match and answers with the title it dropped, or ""
	// when nothing matched.
	Forget(query string) (string, error)
	// Memories lists what is kept, or the matches for a query.
	Memories(query string) ([]session.MemoryLine, error)
}

// brain is the agent under this surface, when it has one at all.
func (a *app) brain() (memoryAgent, bool) {
	agent, ok := a.agent.(memoryAgent)
	if ok && agent.Remembers() {
		return agent, true
	}
	if memory, ok := a.memory.(memoryAgent); ok && memory.Remembers() {
		return memory, true
	}
	return nil, false
}

// memoryOffNote is the one line every one of the three prints when this build
// is not remembering anything. It names the row that turns it on, because "no"
// without "and here is how to change that" is the half of an answer that sends
// somebody to the manual.
const memoryOffNote = "memory is off for this session · turn it on under /settings"

// memoryReplyMsg belongs to the conversation that issued the command. The
// agent alone is not enough: a remote handle can survive a transcript swap.
type memoryReplyMsg struct {
	agent Agent
	file  string
	text  string
	// delivered releases the next operation only after this receipt has been
	// routed, so command scheduling cannot reverse the transcript's order.
	delivered func()
}

// memoryConversation names one conversation in one surface. An agent handle may
// survive a transcript swap, and another conversation must have its own queue.
type memoryConversation struct {
	surface *app
	agent   Agent
	file    string
}

type memoryTicket struct {
	previous <-chan struct{}
	done     chan struct{}
	once     sync.Once
	delivery sync.Once
	stop     func() bool
	// after keeps the grace clock local to this operation, so a controlled
	// clock can prove timeout ordering without waiting out model patience.
	after func(time.Duration) <-chan time.Time
}

// memoryTails holds only outstanding operations, not a permanent conversation
// registry. Issuing a command links its predecessor before Bubble Tea can run
// either command. Delivery or surface cancellation removes the final tail, so
// a receipt dropped during shutdown cannot retain a surface and its agent.
var memoryTails = struct {
	sync.Mutex
	tickets map[memoryConversation]*memoryTicket
}{tickets: make(map[memoryConversation]*memoryTicket)}

func queueMemory(origin memoryConversation) *memoryTicket {
	memoryTails.Lock()
	defer memoryTails.Unlock()
	ticket := &memoryTicket{done: make(chan struct{}), after: time.After}
	if previous := memoryTails.tickets[origin]; previous != nil {
		ticket.previous = previous.done
	}
	memoryTails.tickets[origin] = ticket
	// The cancellation hook is registered while the tail lock is held, so its
	// retirement cannot read stop before AfterFunc has returned it. Each ticket
	// is covered even when Bubble Tea never schedules its command or receipt.
	ticket.stop = context.AfterFunc(origin.surface.ctx, func() { ticket.retire(origin) })
	return ticket
}

func (ticket *memoryTicket) retire(origin memoryConversation) {
	ticket.once.Do(func() {
		memoryTails.Lock()
		defer memoryTails.Unlock()
		if ticket.stop != nil {
			ticket.stop()
		}
		close(ticket.done)
		if memoryTails.tickets[origin] == ticket {
			delete(memoryTails.tickets, origin)
		}
	})
}

func (ticket *memoryTicket) finish(origin memoryConversation, grace <-chan time.Time) {
	ticket.delivery.Do(func() {
		if grace == nil {
			ticket.retire(origin)
			return
		}
		// THE WIRE DEADLINE DOES NOT STOP THE WRITE. Older engines cannot
		// report its eventual completion, so the successor waits one full
		// decider patience after the timeout rather than racing that write.
		go func() {
			select {
			case <-grace:
				ticket.retire(origin)
			case <-ticket.done:
			}
		}()
	})
}

// memoryCall leaves every store or wire wait off Update. Only the receipt
// returns to the surface, so typing and repainting never wait for the engine.
func (a *app) memoryCall(call func() (string, error)) tea.Cmd {
	origin := memoryConversation{surface: a, agent: a.agent, file: a.file}
	ctx := a.ctx
	ticket := queueMemory(origin)
	return func() tea.Msg {
		if ticket.previous != nil {
			select {
			case <-ticket.previous:
			case <-ctx.Done():
			}
		}
		// Cancellation can make both select arms ready. Check it again before
		// starting the successor, so releasing abandoned tickets starts no work.
		if ctx.Err() != nil {
			ticket.retire(origin)
			return nil
		}
		text, err := call()
		if ctx.Err() != nil {
			ticket.retire(origin)
			return nil
		}
		var grace <-chan time.Time
		if errors.Is(err, remote.ErrLate) {
			grace = ticket.after(roles.PatienceFor(roles.RoleReflex))
		}
		return memoryReplyMsg{agent: origin.agent, file: origin.file, text: text,
			delivered: func() { ticket.finish(origin, grace) }}
	}
}

func (a *app) adoptMemoryReply(msg memoryReplyMsg) {
	if msg.delivered != nil {
		defer msg.delivered()
	}
	if msg.agent == a.agent && msg.file == a.file {
		a.note(msg.text)
		return
	}
	for _, held := range a.behind {
		if held != nil && held.side != nil && held.conv.Agent == msg.agent && held.conv.SessionFile == msg.file {
			// A receipt waits with its conversation, just like a held send's
			// receipt, and appears when the person brings that conversation back.
			held.side.parkNotes = append(held.side.parkNotes, msg.text)
			return
		}
	}
}

// runRemember is /remember: keep one thing across conversations.
func (a *app) runRemember(text string) tea.Cmd {
	a.noticeEvent(eventRemembered)
	agent, ok := a.brain()
	if !ok {
		a.note(memoryOffNote)
		return nil
	}
	if strings.TrimSpace(text) == "" {
		a.note("/remember <text> · what should be kept?")
		return nil
	}
	return a.memoryCall(func() (string, error) {
		title, err := agent.Remember(text)
		if err != nil {
			if errors.Is(err, remote.ErrLate) {
				// A LATE RECEIPT IS NOT A FAILED WRITE. The engine keeps the save
				// running, and retrying here could keep the same words twice.
				return "saving that has not answered yet · check /memory before trying again", err
			}
			return "could not remember that · " + err.Error(), err
		}
		return "remembered · " + title, nil
	})
}

// runForget drops ONE match because silently removing every match could erase
// notes the person never meant to name, with no surface door to their contents.
func (a *app) runForget(query string) tea.Cmd {
	agent, ok := a.brain()
	if !ok {
		a.note(memoryOffNote)
		return nil
	}
	if strings.TrimSpace(query) == "" {
		a.note("/forget <query> · what should be dropped?")
		return nil
	}
	return a.memoryCall(func() (string, error) {
		title, err := agent.Forget(query)
		if err != nil {
			return "could not forget that · " + err.Error(), err
		}
		if title == "" {
			return "nothing matched " + query, nil
		}
		return "forgot · " + title, nil
	})
}

// runMemories prints a query's matches, or the whole list on a surface without
// the memory place. Its receipt remains scrollable in the issuing conversation.
func (a *app) runMemories(query string) tea.Cmd {
	agent, ok := a.brain()
	if !ok {
		a.note(memoryOffNote)
		return nil
	}
	return a.memoryCall(func() (string, error) {
		lines, err := agent.Memories(query)
		if err != nil {
			return "could not read what is remembered · " + err.Error(), err
		}
		return memoriesText(query, lines), nil
	})
}

// memoriesText renders the list: one memory per line, its title, what it says,
// and the id that names it. It is a function of its arguments so the emptiness
// law and the shape of a row are testable without a screen.
//
// THE EMPTY STATE IS ONE LINE AND IT NAMES THE REASON. Nothing remembered at
// all and nothing matching a word are different facts about the same store, and
// a person who typed a query wants to know which one they got.
func memoriesText(query string, lines []session.MemoryLine) string {
	if len(lines) == 0 {
		if strings.TrimSpace(query) != "" {
			return "nothing remembered matches " + strings.TrimSpace(query)
		}
		return "nothing is remembered yet"
	}
	rows := make([]string, 0, len(lines))
	for _, line := range lines {
		row := line.Text
		if title := strings.TrimSpace(line.Title); title != "" && title != line.Text {
			row = title + " — " + line.Text
		}
		if line.ID != "" {
			row += "  (" + line.ID + ")"
		}
		rows = append(rows, row)
	}
	return strings.Join(rows, "\n")
}
