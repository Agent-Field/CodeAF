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

// installDelegates reads the conversation's delegates and puts their rows on
// the table. It runs when the surface is built and again when the conversation
// in front changes, because the registry is the conversation's. A hosted
// surface installs nothing: the registry lives on the far machine, and a row
// that opened a door there would be a command about somewhere else.
func (a *app) installDelegates() {
	if a.hosted() {
		installDelegateCommands(nil)
		return
	}
	agent, ok := a.delegateSeam()
	if !ok {
		installDelegateCommands(nil)
		return
	}
	// A collision is not said here — the surface is still being built and has
	// nowhere to draw a line yet — it is said where the person will look for
	// the missing row, on `/delegate`.
	installDelegateCommands(agent.Delegates().Rows)
}

// runDelegateCommand is `/<name> <brief>`: the brief goes to that delegate
// through the same door `/task` opens, and the answer lands as a task start.
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
	return a.startTaskDoorVia(brief, func(ctx context.Context) (uint64, string, string, error) {
		return agent.StartDelegate(ctx, name, brief)
	})
}

// openDelegate is `/delegate`: bare, the list; with a name and words, the
// delegate's own row run on those words.
func (a *app) openDelegate(rest string) tea.Cmd {
	if a.hosted() {
		a.note(a.remoteProfileWord("delegates"))
		return nil
	}
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
	report := agent.Delegates()
	report.Refused = append(report.Refused, delegateCollisionLines()...)
	if len(report.Rows) == 0 && len(report.Absent) == 0 && len(report.Refused) == 0 {
		a.note(delegateNothingWord)
		return nil
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
	a.note(strings.Join(lines, "\n"))
	return nil
}

// delegateUnknownWord answers `/delegate <name>` for a name no row carries.
func delegateUnknownWord(name string) string {
	return "no delegate is called " + name + " · /delegate lists the ones here"
}
