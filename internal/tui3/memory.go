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

// THE COMMANDS ONTO WHAT codeaf REMEMBERS.
//
// Every line codeaf keeps rides every conversation in its scope
// (internal/session's memory.go), so a person must be able to read it, add to
// it and take things out of it by hand — and every receipt says exactly what
// changed and where it holds. The conversation writes the receipt, because it
// is the one that knows which project a line kept here holds in.
//
//	/remember <text>   keep one line (here: this project; on home: everywhere)
//	/always <text>     the same, under the word people were taught
//	/forget <words>    drop the one line the words name — or list the matches
//	/forget            undo the newest memory change this conversation made
//	/memory [<words>]  open the memory place, or print the lines that match
//
// Receipts answer in the transcript so they stay scrollable, and on home's own
// message line when the command was typed there.

// memoryAgent is the slice of *session.Agent these three need. It is asserted
// rather than added to [Agent] for [taskAgent]'s reason: memory is OPTIONAL —
// a session with no store simply does not have it, and every scripted agent in
// this package's tests has never heard of one.
type memoryAgent interface {
	// Remembers is whether this conversation has a memory store at all — true
	// whatever the noticing row says, because lines the person kept hold either
	// way.
	Remembers() bool
	// Remember keeps one line and answers the receipt.
	Remember(text string) (string, error)
	// Forget drops the line a query names and answers the receipt; an empty
	// query undoes the conversation's newest memory change.
	Forget(query string) (string, error)
	// Memories lists what is kept, or the matches for a query.
	Memories(query string) ([]session.MemoryLine, error)
	// MoveMemory moves one line to where the person says it holds, and
	// MemoryProject is the owner this conversation keeps project lines under —
	// the two things only the conversation can resolve.
	MoveMemory(id, scope string) (string, error)
	MemoryProject() string
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

// memoryUnavailableNote is the one line every command prints when this
// conversation has no memory store at all — which happens only when the store
// could not be opened, and the launch said why. It names no setting: no setting
// turns memory off (the memory row governs only what is noticed for you).
const memoryUnavailableNote = "memory is unavailable in this conversation · the store could not be opened"

// memoryReplyMsg belongs to the conversation that issued the command. The
// agent alone is not enough: a remote handle can survive a transcript swap.
type memoryReplyMsg struct {
	agent Agent
	file  string
	text  string
	// home says the command was typed on home, so its receipt is said on home's
	// own message line as well as in the conversation behind it.
	home bool
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
	home := a.at(pageHome)
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
		return memoryReplyMsg{agent: origin.agent, file: origin.file, text: text, home: home,
			delivered: func() { ticket.finish(origin, grace) }}
	}
}

func (a *app) adoptMemoryReply(msg memoryReplyMsg) {
	if msg.delivered != nil {
		defer msg.delivered()
	}
	if msg.agent == a.agent && msg.file == a.file {
		// A RECEIPT FOR A COMMAND TYPED ON HOME IS SAID ON HOME. It arrives after
		// the dispatch that typed it has returned, so home's echo flag is down;
		// raising it around this one note is what puts the receipt where the
		// person is looking (app.go's [app.noteWritten]).
		if msg.home {
			a.echoHome = true
			defer func() { a.echoHome = false }()
		}
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

// runRemember is /remember: keep one line. Typed in a conversation it holds in
// that conversation's project; typed on home, with no conversation in front of
// the person, it holds for them everywhere. The conversation says which.
func (a *app) runRemember(text string) tea.Cmd {
	a.noticeEvent(eventRemembered)
	agent, ok := a.brain()
	if !ok {
		a.note(memoryUnavailableNote)
		return nil
	}
	if strings.TrimSpace(text) == "" {
		a.note("/remember <text> · what should be kept?")
		return nil
	}
	everywhere := a.at(pageHome)
	keeper, keeps := a.agent.(alwaysAgent)
	return a.memoryCall(func() (string, error) {
		var receipt string
		var err error
		if everywhere && keeps {
			receipt, err = keeper.RememberAlways(text, true)
		} else {
			receipt, err = agent.Remember(text)
		}
		if err != nil {
			if errors.Is(err, remote.ErrLate) {
				// A LATE RECEIPT IS NOT A FAILED WRITE. The engine keeps the save
				// running, and retrying here could keep the same words twice.
				return "saving that has not answered yet · check /memory before trying again", err
			}
			return "could not remember that · " + err.Error(), err
		}
		return receipt, nil
	})
}

// alwaysAgent is the slice of the agent that keeps a line for the person
// everywhere and lists what holds: /always, and /remember typed on home. Both
// the in-process session and the wire's agent answer it (internal/remote's
// memory.go).
type alwaysAgent interface {
	RememberAlways(text string, everywhere bool) (string, error)
	AlwaysMemories(everywhere bool) ([]session.MemoryLine, error)
}

// runAlways is /always: the same errand as /remember, kept because people were
// taught the word — and bare, the list of what holds here (everywhere, on
// home).
func (a *app) runAlways(text string) tea.Cmd {
	if strings.TrimSpace(text) != "" {
		return a.runRemember(text)
	}
	if _, ok := a.brain(); !ok {
		a.note(memoryUnavailableNote)
		return nil
	}
	lister, ok := a.agent.(alwaysAgent)
	if !ok {
		return a.runMemories("")
	}
	everywhere := a.at(pageHome)
	return a.memoryCall(func() (string, error) {
		lines, err := lister.AlwaysMemories(everywhere)
		if err != nil {
			return "could not read what is remembered · " + err.Error(), err
		}
		return memoriesText("", lines), nil
	})
}

// runForget is /forget. With words it drops the ONE line they name, and when
// they name more than one it drops nothing and lists them — removing every
// match, or the best guess, could erase lines the person never meant to name.
// Bare, it undoes the newest memory change this conversation made, which is
// what every "noticed and remembered" line points at.
func (a *app) runForget(query string) tea.Cmd {
	agent, ok := a.brain()
	if !ok {
		a.note(memoryUnavailableNote)
		return nil
	}
	return a.memoryCall(func() (string, error) {
		receipt, err := agent.Forget(strings.TrimSpace(query))
		if err != nil {
			if errors.Is(err, remote.ErrLate) {
				return "that has not answered yet · check /memory before trying again", err
			}
			return "could not forget that · " + err.Error(), err
		}
		return receipt, nil
	})
}

// runMemories prints a query's matches, or the whole list on a surface without
// the memory place. Its receipt remains scrollable in the issuing conversation.
func (a *app) runMemories(query string) tea.Cmd {
	agent, ok := a.brain()
	if !ok {
		a.note(memoryUnavailableNote)
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

// memoriesText renders the list: one line per memory, its words, then where it
// holds and who kept it. It is a function of its arguments so the emptiness law
// and the shape of a row are testable without a screen.
//
// THE EMPTY STATE IS ONE LINE AND IT NAMES THE REASON. Nothing remembered at
// all and nothing matching a word are different facts about the same store.
func memoriesText(query string, lines []session.MemoryLine) string {
	if len(lines) == 0 {
		if strings.TrimSpace(query) != "" {
			return "nothing remembered matches " + strings.TrimSpace(query)
		}
		return "nothing is remembered here yet · /remember <text> keeps a line"
	}
	rows := make([]string, 0, len(lines))
	for _, line := range lines {
		row := "· " + line.Text
		if line.Where != "" {
			row += "  · " + line.Where
		}
		if word := memoryOriginWord(line.Origin); word != "" {
			row += " · " + word
		}
		rows = append(rows, row)
	}
	return strings.Join(rows, "\n")
}
