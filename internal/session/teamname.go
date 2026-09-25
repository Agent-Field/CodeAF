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
// titleSystem's comment gives.
const teamNameAsk = "Give these conversations one short shared name: one to three plain lowercase words, no quotes. Answer with the name only."

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
	ask := "Conversations:\n" + strings.Join(lines, "\n") + "\n\n" + teamNameAsk
	response, named, err := a.callRoleChecked(ctx, roles.RoleTitle, model,
		[]ai.Message{textMessage("system", titleSystem), textMessage("user", ask)},
		func(response *ai.Response, named string) bool {
			if cleanTeamName(response.Text()) != "" {
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
	name := cleanTeamName(response.Text())
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
func cleanTeamName(raw string) string {
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
		if len(words) == teamNameWords {
			break
		}
	}
	name := strings.Join(words, " ")
	// A small model that answers with the instruction has named nothing.
	if len(words) >= 2 && strings.Contains(strings.ToLower(teamNameAsk), name) {
		return ""
	}
	return name
}
