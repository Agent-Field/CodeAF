package config

import (
	"fmt"
	"strings"
	"unicode"

	"github.com/Agent-Field/codeaf/internal/modelsource"
	"github.com/Agent-Field/codeaf/internal/provider"
)

// ConnectionOutcomeWord is the one person-facing account of a model-service
// key check. Both the chat panel and the terminal command call this function so
// a service cannot be accepted in one voice and refused in another.
func ConnectionOutcomeWord(service string, outcome modelsource.Outcome) string {
	service = strings.TrimSpace(service)
	switch outcome.Kind {
	case modelsource.OutcomeConnected:
		line := service + " is connected"
		if door := strings.TrimSpace(outcome.Door.Name); door != "" {
			line += " · " + door
		}
		if outcome.Listed && outcome.Models > 0 {
			word := "models"
			if outcome.Models == 1 {
				word = "model"
			}
			line += fmt.Sprintf(" · %d %s", outcome.Models, word)
		}
		if outcome.PlanPaused {
			overflow := ""
			if outcome.Overflow != nil {
				overflow = outcome.Overflow.Name
			}
			line += " · " + provider.PlanPauseSentence(outcome.PlanReset, overflow)
		}
		return line
	case modelsource.OutcomeRefused:
		line := service + " refused that key"
		if said := ConnectionDetailWords(outcome.VendorSaid, 120); said != "" {
			line += " — " + said
		}
		return line
	case modelsource.OutcomeAccountCannotPay:
		// THE ONE SENTENCE for an authenticated account with no funds, shared by
		// the two moments a person meets it: the connect row, and a turn a vendor
		// refused for the same reason. IT NAMES THE SERVICE AND NEVER THE STATUS.
		// `error: API error (429): …` is what the turn drew before this existed —
		// three pieces of machinery vocabulary on a line a person reads, and a
		// number that tells them nothing they can act on. The vendor's own words
		// are the only part that says what to do, and the top-up link may be at
		// the end. The display wraps this whole sentence instead of cutting it.
		line := service + " accepted the key but the account cannot pay"
		if said := ConnectionDetailWords(outcome.VendorSaid, 0); said != "" {
			line += " — " + said
		}
		return line
	case modelsource.OutcomeUnanswered:
		return service + " did not answer · nothing was saved"
	case modelsource.OutcomeWrongShape:
		return "that is not the shape of a " + service + " key — they start with sk-"
	}
	return service + " did not connect"
}

// CodexConnectionWord is the browser road's shared success sentence. Missing
// account details stay absent, and a fallback list is named without inventing
// an account or plan.
func CodexConnectionWord(email, plan string, outcome modelsource.Outcome) string {
	line := "codex connected"
	email, plan = strings.TrimSpace(email), strings.TrimSpace(plan)
	if email != "" && plan != "" {
		line += " · " + email + " · " + plan + " plan"
	}
	if !outcome.Refreshed {
		line += " · model list was not refreshed"
	}
	return line
}

// ConnectionDetailWords keeps a vendor's sentence on one bounded line. A cut
// retreats to a word boundary so the shared outcome never ends in half a word.
func ConnectionDetailWords(words string, limit int) string {
	words = strings.TrimSpace(strings.Join(strings.Fields(words), " "))
	runes := []rune(words)
	if limit <= 0 || len(runes) <= limit {
		return words
	}
	cut := limit
	for cut > 0 && !unicode.IsSpace(runes[cut]) {
		cut--
	}
	if cut == 0 {
		cut = limit
	}
	return strings.TrimSpace(string(runes[:cut]))
}
