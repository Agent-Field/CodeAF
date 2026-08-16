package chat

// One runner for every ITEM verb in the product — the chips a detail page or a
// notebook row draws against one particular thing (a charter, a service, a
// belief, a way of working). The registry entry says what the chip needs before
// it may act, and this file is where those needs are honored once rather than
// per surface:
//
//   - Entry.Confirm: the verb cannot be undone, so the first activation ARMS it
//     — the question goes to the status line — and only a second activation (or
//     `y`) fires. Esc, or doing anything else, stands down. The question is the
//     registry's own words, so every surface asks the same way.
//   - Entry.Steer (command.ErrSteerVerb): the verb carries an argument only a
//     sentence can hold, so it is a thing to SAY (the one-mouth law). The
//     sentence is seeded into the composer with the keyboard handed over —
//     the finished form of "the surface says what to say". A draft already in
//     progress is never overwritten; the sentence is offered on the status
//     line instead, exactly as before.
//
// Everything else fires immediately through the commander's one door,
// [VerbInvoker], with [App.runFooterVerb] as the engineless fallback.

import (
	"errors"
	"strings"

	tea "charm.land/bubbletea/v2"

	"github.com/Agent-Field/aforge-v2/internal/command"
	"github.com/Agent-Field/aforge-v2/internal/registry"
	"github.com/Agent-Field/aforge-v2/internal/tui2/homes"
	"github.com/Agent-Field/aforge-v2/internal/tui2/keychip"
)

// armedVerb is a destructive verb waiting for its second yes. Subject is part
// of the identity: arming "retire" on one charter and firing it on another
// would be the exact mistake the confirmation exists to prevent.
type armedVerb struct {
	id      string
	subject string
	name    string
}

// runItemVerb is the one path from any drawn item chip to the thing it does.
func (a *App) runItemVerb(id, subject, name string) tea.Cmd {
	entry, found := registry.ByID(id)
	if found && entry.Confirm != "" {
		armed := a.verbArmed
		if armed == nil || armed.id != id || armed.subject != subject {
			a.verbArmed = &armedVerb{id: id, subject: subject, name: name}
			// The two answers are verb·key chips (§16, [keychip]) and not the
			// bare `y yes · esc no` this row used to spell: the word that names
			// the act comes first, the key reads as annotation after it, and the
			// arming line says the same thing the footer and the consent dialog
			// say in the same shape.
			a.status.err = entry.Confirm + "  " +
				keychip.Text(registry.ChipFor("yes", "y"), registry.ChipFor("no", "esc"))
			a.refresh()
			return nil
		}
	}
	a.verbArmed = nil
	invoker, ok := a.commander.(VerbInvoker)
	if !ok {
		return a.runFooterVerb(id)
	}
	_, err := invoker.InvokeVerb(id, subject, "")
	switch {
	case err == nil:
		a.status.err = ""
	case errors.Is(err, command.ErrSteerVerb) && found:
		sentence := entry.SteerFor(name)
		if cmd, seeded := a.seedComposer(sentence); seeded {
			a.status.err = ""
			a.refresh()
			return cmd
		}
		a.status.err = "say it: " + sentence
	default:
		a.status.err = err.Error()
	}
	a.refresh()
	return nil
}

// verbKey answers the keyboard while a destructive verb is armed. `y` is the
// second yes; esc stands down; any other key stands down AND keeps its ordinary
// meaning, so an armed question never traps the reader.
func (a *App) verbKey(msg tea.KeyPressMsg) (tea.Cmd, bool) {
	if a.verbArmed == nil {
		return nil, false
	}
	switch msg.String() {
	case "y":
		armed := a.verbArmed
		return a.runItemVerb(armed.id, armed.subject, armed.name), true
	case "esc":
		a.verbArmed = nil
		a.status.err = ""
		a.refresh()
		return nil, true
	default:
		a.verbArmed = nil
		a.status.err = ""
		a.refresh()
		return nil, false
	}
}

// seedComposer puts a steering sentence into the draft and hands the keyboard
// over, so the reader finishes the sentence rather than starting it. It
// refuses — and reports so — when a draft is already in progress, because a
// chip must never eat words the person already typed.
func (a *App) seedComposer(text string) (tea.Cmd, bool) {
	seeder, ok := a.composer.(interface{ Seed(string) bool })
	if !ok || !seeder.Seed(text) {
		return nil, false
	}
	return a.setPageFocus(false), true
}

// notebookVerbSubject turns a notebook row id into the subject the command
// funnel wants: the craft's NAME, the skill or belief fact's number, the
// question's id — the row id with its kind prefix taken back off.
func notebookVerbSubject(row string) string {
	for _, prefix := range []string{
		homes.BeliefRowPrefix, homes.CraftRowPrefix,
		homes.SkillRowPrefix, homes.QuestionRowPrefix,
	} {
		if strings.HasPrefix(row, prefix) {
			return strings.TrimPrefix(row, prefix)
		}
	}
	return row
}

// notebookVerbName is what the row is ABOUT in a person's words — the seed a
// steering verb drops into the composer, never an id.
func (a *App) notebookVerbName(row string) string {
	state := &a.source.homes
	switch {
	case strings.HasPrefix(row, homes.BeliefRowPrefix):
		id := strings.TrimPrefix(row, homes.BeliefRowPrefix)
		for i := range state.Notebook.Beliefs {
			if state.Notebook.Beliefs[i].ID == id {
				body := state.Notebook.Beliefs[i].Body
				if cut := strings.IndexByte(body, '\n'); cut >= 0 {
					body = body[:cut]
				}
				return strings.TrimSpace(body)
			}
		}
	case strings.HasPrefix(row, homes.CraftRowPrefix):
		return strings.TrimPrefix(row, homes.CraftRowPrefix)
	case strings.HasPrefix(row, homes.SkillRowPrefix):
		id := strings.TrimPrefix(row, homes.SkillRowPrefix)
		for i := range state.Knowhow.Skills {
			if state.Knowhow.Skills[i].ID == id {
				return state.Knowhow.Skills[i].Name
			}
		}
	}
	return ""
}
