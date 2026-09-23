package tui3

// THE PROGRAMS CODEAF CARRIES, ON THE SURFACE: one command row per program the
// engine's build carries — `/senior-dev <brief>` — generated at launch from the
// list the conversation holds (internal/delegate/builtin, handed in by the
// launch). A program's row runs like `/task`: the words after it are the
// brief, the same door opens, a run starts, the turn goes on.
//
// THE ROWS ARE APPENDED TO THE LIVE TABLE AND NEVER TO THE LITERAL. The static
// table keeps its static gate (manual_test.go walks it); these rows exist only
// in a build that carries their program, and over `--host` only when the FAR
// machine's build does — which is right, because the program runs there.

import (
	"context"
	"strconv"
	"strings"
	"sync"

	tea "charm.land/bubbletea/v2"

	"github.com/Agent-Field/codeaf/internal/session"
)

// delegateAgent is what this surface asks a conversation about the programs:
// the list, and the door.
type delegateAgent interface {
	Delegates() session.DelegateReport
	StartDelegate(context.Context, string, string) (uint64, string, string, error)
}

func (a *app) delegateSeam() (delegateAgent, bool) {
	if a.agent == nil {
		return nil, false
	}
	agent, ok := a.agent.(delegateAgent)
	return agent, ok
}

// delegateUsageWordTail is what a program's row says under a bare `/<name>`.
const delegateUsageWordTail = " <brief> · hands the whole task to that program"

// baseCommands is the literal table as this file found it, so the live table
// can be rebuilt from it however many times a surface installs rows: a second
// install replaces the first rather than stacking on it.
var (
	baseCommands   = append([]command(nil), commands...)
	delegateRowsMu sync.Mutex
	delegateRows   map[string]bool
)

// installDelegateCommands rebuilds the live command table as the literal plus
// one row per program. A name that collides with a row or alias of the literal
// table is left out and answered back, because [checkCommands]'s law holds for
// generated rows too: a word may not mean two things. The build's own test
// keeps any program from being named that way (cmd/codeaf), so this is the
// guard a far engine of another build would need.
func installDelegateCommands(rows []session.DelegateRow) []string {
	delegateRowsMu.Lock()
	defer delegateRowsMu.Unlock()
	table := append([]command(nil), baseCommands...)
	installed := map[string]bool{}
	var refused []string
	for _, row := range rows {
		name := strings.TrimSpace(row.Name)
		if name == "" {
			continue
		}
		candidate := command{name: name, args: "<brief>", desc: row.Description}
		if err := checkCommands(append(append([]command(nil), table...), candidate)); err != nil || baseNames()[name] {
			refused = append(refused, name+": its name is already a command here — not added")
			continue
		}
		table = append(table, candidate)
		installed[name] = true
	}
	commands = table
	delegateRows = installed
	return refused
}

// baseNames is every word the literal table answers to: names and aliases.
func baseNames() map[string]bool {
	names := map[string]bool{}
	for _, c := range baseCommands {
		names[c.name] = true
		for _, word := range c.alias {
			names[word] = true
		}
	}
	return names
}

// isDelegateCommand says whether a typed word is one of the installed rows.
func isDelegateCommand(name string) bool {
	delegateRowsMu.Lock()
	defer delegateRowsMu.Unlock()
	return delegateRows[name]
}

// installDelegates asks the conversation for its programs OFF THE LOOP and,
// when the answer comes back, puts their rows on the table. It is asked at the
// launch and again when the conversation in front changes, because the list is
// the engine's — and over `--host` it is the far machine's build, which is
// right: the program and the run are there, and the door crosses the wire
// (internal/remote's Delegate.List). It rides [app.besideLine] because nobody
// pressed for it: a read that waited in the door line behind a person's gesture
// would be a row arriving after the keystroke that wanted it.
func (a *app) installDelegates() tea.Cmd {
	agent, ok := a.delegateSeam()
	if !ok {
		installDelegateCommands(nil)
		return nil
	}
	return a.besideLine(func() func(here bool) tea.Cmd {
		report := agent.Delegates()
		return func(here bool) tea.Cmd {
			if here {
				installDelegateCommands(report.Rows)
			}
			return nil
		}
	})
}

// runDelegateCommand is `/<name> <brief>`: the brief goes to that program
// through a door of its own — asked off the loop like every door — and the
// answer lands as a task start, on the message `/task` lands on.
func (a *app) runDelegateCommand(name, brief string) tea.Cmd {
	brief = strings.TrimSpace(brief)
	if brief == "" {
		a.note("usage: /" + name + delegateUsageWordTail)
		return nil
	}
	agent, ok := a.delegateSeam()
	if !ok {
		a.note("could not start the task · this session cannot hand work to /" + name)
		return nil
	}
	ctx := a.ctx
	conv := a.taskDoorNotes(brief)
	return a.offLoop(func() func(here bool) tea.Cmd {
		id, title, note, err := agent.StartDelegate(ctx, name, brief)
		return func(bool) tea.Cmd {
			return func() tea.Msg {
				return taskStartedMsg{
					kind: "single", id: strconv.FormatUint(id, 10), title: title,
					err: err, note: note, brief: brief, conv: conv,
				}
			}
		}
	})
}
