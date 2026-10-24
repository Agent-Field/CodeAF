package session

import (
	"context"
	"errors"
	"strings"
	"unicode"

	"github.com/Agent-Field/agentfield/sdk/go/ai"
	"github.com/Agent-Field/codeaf/internal/roles"
)

// A team's name, suggested once.
//
// A person grouping conversations on the conversations view is offered a name
// for the group before they type one. When the conversations share a project
// folder the surface offers the folder's name and never asks; otherwise it asks
// here, once, over the conversations' own titles, and a person who has started
// typing has already answered.
//
// IT IS THE CONVERSATION NAMER'S ERRAND WITH A SMALLER QUESTION. It goes through
// the same door ([Agent.callRoleChecked]) on the same role ([roles.RoleTitle]),
// so it lands on the cheap tier a person has configured for names and is billed
// the same way: against the model that answered, off every turn's clock. It is
// one ask with no ladder of retries of its own, because the caller holds a word
// already and a slow answer is simply not used.

// teamNameAsk is the instruction, last in the user message for the reason
// titleSystem's comment gives; teamNameAskShort is the same for a name of at
// most two words, which is what a team made of one conversation is given
// (internal/tui3's helpers.go): a team named for one piece of work wants the
// work's name, and a third word is where a small model starts describing it.
const (
	teamNameAsk      = "Give these conversations one short shared name: one to three plain lowercase words, no quotes. Answer with the name only."
	teamNameAskShort = "Give this work one short name: one or two plain lowercase words, no quotes. Answer with the name only."
)

// teamNameClip bounds each title handed to the namer, and teamNameTitles how
// many are sent: a name for a group is read off a handful of short titles.
const (
	teamNameClip   = 120
	teamNameTitles = 12
)

// teamNameWords is the most words a suggested team name keeps.
const teamNameWords = 3

// NameTeam asks the naming role for a short name for a group of
// conversations, given their titles. It makes one call, which the caller
// bounds with ctx, and returns the name or an error; an answer that is not a
// name is an error, never a name.
func (a *Agent) NameTeam(ctx context.Context, titles []string) (string, error) {
	return a.NameTeamWords(ctx, titles, teamNameWords)
}

// NameTeamWords is [Agent.NameTeam] for a name of at most words words: two
// asks the short question ([teamNameAskShort]), and anything else, or a number
// out of range, the group's. The answer is cut to that many words whatever the
// model said.
func (a *Agent) NameTeamWords(ctx context.Context, titles []string, words int) (string, error) {
	instruction := teamNameAsk
	if words <= 0 || words > teamNameWords {
		words = teamNameWords
	}
	if words <= 2 {
		instruction = teamNameAskShort
	}
	clean := func(raw string) string { return cleanTeamNameIn(raw, words, instruction) }
	var lines []string
	for _, t := range titles {
		if t = strings.TrimSpace(t); t != "" && len(lines) < teamNameTitles {
			lines = append(lines, "- "+clip(t, teamNameClip))
		}
	}
	if len(lines) == 0 {
		return "", errInvalidName
	}
	a.mu.Lock()
	model, closed := a.model, a.closed
	a.mu.Unlock()
	if closed {
		return "", errors.New("the conversation is closed")
	}
	ask := "Conversations:\n" + strings.Join(lines, "\n") + "\n\n" + instruction
	response, named, err := a.callRoleChecked(withDetachedUsage(ctx), roles.RoleTitle, model,
		[]ai.Message{textMessage("system", titleSystem), textMessage("user", ask)},
		func(response *ai.Response, named string) bool {
			if clean(response.Text()) != "" {
				return true
			}
			a.addDetachedUsageAs(response, named, 1, auxRoleTitle)
			return false
		})
	if err != nil {
		return "", err
	}
	if response == nil {
		return "", errEmptyAnswer
	}
	a.addDetachedUsageAs(response, named, 1, auxRoleTitle)
	name := clean(response.Text())
	if name == "" {
		return "", errInvalidName
	}
	return name, nil
}

// cleanTeamName is a model's answer as a team name: the first line cleaned as
// a title is ([cleanTitle] refuses an echoed instruction or a sentence about
// the speaker), lowercased, cut to [teamNameWords] words, and with anything but
// letters, digits and inner hyphens taken off each word. It is "" when nothing
// usable is left.
func cleanTeamName(raw string) string { return cleanTeamNameIn(raw, teamNameWords, teamNameAsk) }

// cleanTeamNameIn is [cleanTeamName] cut to limit words, refusing an answer
// that echoes instruction, the ask the model was given.
func cleanTeamNameIn(raw string, limit int, instruction string) string {
	title := cleanTitle(raw)
	if title == "" {
		return ""
	}
	var words []string
	for _, w := range strings.Fields(strings.ToLower(title)) {
		w = strings.TrimFunc(w, func(r rune) bool { return !unicode.IsLetter(r) && !unicode.IsDigit(r) })
		w = strings.Map(func(r rune) rune {
			if unicode.IsLetter(r) || unicode.IsDigit(r) || r == '-' {
				return r
			}
			return -1
		}, w)
		if w != "" {
			words = append(words, w)
		}
		if len(words) == limit {
			break
		}
	}
	name := strings.Join(words, " ")
	// A small model that answers with the instruction has named nothing.
	if len(words) >= 2 && strings.Contains(strings.ToLower(instruction), name) {
		return ""
	}
	return name
}
