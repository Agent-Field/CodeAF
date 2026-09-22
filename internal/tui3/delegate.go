package tui3

// DELEGATES ON THE SURFACE: one command row per installed delegate, generated at
// launch from the registry the conversation holds, and `/delegate`, the list of
// them (docs/design/delegate/DESIGN.md). A delegate row runs like `/task`: the
// words after it are the brief, the same door opens, a run starts, the turn
// goes on.
//
// THE ROWS ARE APPENDED TO THE LIVE TABLE AND NEVER TO THE LITERAL. The static
// table keeps its static gate (manual_test.go walks it); the rows here exist
// only while their delegate does, and the manual law for them is checked where
// the row comes into existence, by the loader, against the delegate's own page.

import (
	"context"
	"strconv"
	"strings"
	"sync"

	tea "charm.land/bubbletea/v2"

	"github.com/Agent-Field/codeaf/internal/session"
)

// delegateAgent is what this surface asks a conversation about delegates: the
// list, and the door.
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

// The words `/delegate` says when there is nothing to list.
const (
	delegateNothingWord   = "no delegates here — a delegate is an outside program codeaf can hand a whole task to; a manifest under ~/.codeaf/delegates adds one"
	delegateUsageWordTail = " <brief> · hands the whole task to that program"
)

// baseCommands is the literal table as this file found it, so the live table
// can be rebuilt from it however many times a surface installs rows: a second
// install replaces the first rather than stacking on it.
var (
	baseCommands   = append([]command(nil), commands...)
	delegateRowsMu sync.Mutex
	delegateRows   map[string]bool
	// delegateCollisions is every delegate the last install left off the table
	// for wearing a built-in command's name, in the sentence `/delegate` draws.
	delegateCollisions []string
)

// installDelegateCommands rebuilds the live command table as the literal plus
// one row per delegate. A name that collides with a built-in row or alias is
// left out — the loader refused nothing, so the collision is said here, in the
// note the caller draws — because [checkCommands]'s law holds for generated
// rows too: a word may not mean two things.
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
	delegateCollisions = refused
	return refused
}

// collisions is what the last install would not seat.
func delegateCollisionLines() []string {
	delegateRowsMu.Lock()
	defer delegateRowsMu.Unlock()
	return append([]string(nil), delegateCollisions...)
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

// installDelegates asks the conversation for its delegates OFF THE LOOP and,
// when the answer comes back, puts their rows on the table. It is asked at the
// launch and again when the conversation in front changes, because the registry
// is the conversation's — and over `--host` it is the far machine's, which is
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
			// A collision is not said here — it is said where the person will
			// look for the missing row, on `/delegate`.
			if here {
				installDelegateCommands(report.Rows)
			}
			return nil
		}
	})
}

// runDelegateCommand is `/<name> <brief>`: the brief goes to that delegate
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
		a.note("could not start the task · this session has no delegate door")
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

// openDelegate is `/delegate`: bare, the list; with a name and words, the
// delegate's own row run on those words. The list is a door, so it is asked off
// the loop and said when it comes back.
func (a *app) openDelegate(rest string) tea.Cmd {
	if name, brief, _ := strings.Cut(strings.TrimSpace(rest), " "); name != "" {
		if !isDelegateCommand(name) {
			a.note(delegateUnknownWord(name))
			return nil
		}
		return a.runDelegateCommand(name, brief)
	}
	agent, ok := a.delegateSeam()
	if !ok {
		a.note(delegateNothingWord)
		return nil
	}
	return a.offLoop(func() func(here bool) tea.Cmd {
		report := agent.Delegates()
		return func(here bool) tea.Cmd {
			if here {
				a.note(delegateListNote(report))
			}
			return nil
		}
	})
}

// delegateListNote is what `/delegate` says: one line per delegate that can
// run, then the ones whose program is not there, then the manifests that were
// not added and why — the loader's and this surface's own collisions alike.
func delegateListNote(report session.DelegateReport) string {
	report.Refused = append(report.Refused, delegateCollisionLines()...)
	if len(report.Rows) == 0 && len(report.Absent) == 0 && len(report.Refused) == 0 {
		return delegateNothingWord
	}
	lines := make([]string, 0, len(report.Rows)+len(report.Absent)+len(report.Refused))
	for _, row := range report.Rows {
		lands := "lands its work on your branch"
		if row.Lands == "text" {
			lands = "answers in the conversation"
		}
		lines = append(lines, "/"+row.Name+" <brief> · "+row.Description+" · "+lands+" · "+row.Bin)
	}
	for _, absent := range report.Absent {
		lines = append(lines, "not here: "+absent)
	}
	for _, refusal := range report.Refused {
		lines = append(lines, "not added: "+refusal)
	}
	return strings.Join(lines, "\n")
}

// delegateUnknownWord answers `/delegate <name>` for a name no row carries.
func delegateUnknownWord(name string) string {
	return "no delegate is called " + name + " · /delegate lists the ones here"
}
