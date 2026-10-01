package tui3

import (
	"strings"

	tea "charm.land/bubbletea/v2"

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
// All three ANSWER IN THE TRANSCRIPT rather than opening a panel, which is
// /status and /cost's argument unchanged: somebody who asked what is remembered
// about them wants it where they can scroll back to it, not on a fullscreen
// sheet they have to leave before they can act on it.

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
	if !ok || !agent.Remembers() {
		return nil, false
	}
	return agent, true
}

// brainFor is [app.brain] with the two refusals every command shares said on
// the way out: this session being somewhere else, and memory being off.
func (a *app) brainFor() (memoryAgent, bool) {
	if a.hosted() {
		a.note(memoryRemoteWord)
		return nil, false
	}
	agent, ok := a.brain()
	if !ok {
		a.note(memoryOffNote)
	}
	return agent, ok
}

// noteFold is the fold every memory door shares: say what the engine answered,
// in the conversation it was asked of and in no other.
func (a *app) noteFold(text string) func(bool) tea.Cmd {
	return func(here bool) tea.Cmd {
		if here {
			a.note(text)
		}
		return nil
	}
}

// memoryOffNote is the one line every one of the three prints when this build
// is not remembering anything. It names the row that turns it on, because "no"
// without "and here is how to change that" is the half of an answer that sends
// somebody to the manual.
const memoryOffNote = "memory is off for this session · turn it on under /settings"

// runRemember is /remember: keep one thing across conversations.
func (a *app) runRemember(text string) tea.Cmd {
	a.noticeEvent(eventRemembered)
	agent, ok := a.brainFor()
	if !ok {
		return nil
	}
	if strings.TrimSpace(text) == "" {
		a.note("/remember <text> · what should be kept?")
		return nil
	}
	return a.offLoop(func() func(bool) tea.Cmd {
		title, err := agent.Remember(text)
		if err != nil {
			return a.noteFold("could not remember that · " + err.Error())
		}
		return a.noteFold("remembered · " + title)
	})
}

// runForget is /forget: drop the one thing that best matches.
//
// ONE, not every match. A query that matched three memories and silently
// dropped all three would be a person losing two things they never named, and
// the recovery — the store keeps a tombstone, not the row's contents in any
// place a surface can reach — is a database question rather than a keystroke.
func (a *app) runForget(query string) tea.Cmd {
	agent, ok := a.brainFor()
	if !ok {
		return nil
	}
	if strings.TrimSpace(query) == "" {
		a.note("/forget <query> · what should be dropped?")
		return nil
	}
	return a.offLoop(func() func(bool) tea.Cmd {
		title, err := agent.Forget(query)
		switch {
		case err != nil:
			return a.noteFold("could not forget that · " + err.Error())
		case title == "":
			return a.noteFold("nothing matched " + query)
		}
		return a.noteFold("forgot · " + title)
	})
}

// runMemories is /memories: the whole list, or the ones matching a word.
func (a *app) runMemories(query string) tea.Cmd {
	agent, ok := a.brainFor()
	if !ok {
		return nil
	}
	return a.offLoop(func() func(bool) tea.Cmd {
		lines, err := agent.Memories(query)
		if err != nil {
			return a.noteFold("could not read what is remembered · " + err.Error())
		}
		return a.noteFold(memoriesText(query, lines))
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
