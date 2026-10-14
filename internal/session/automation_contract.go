package session

// automation_contract.go is the shape the engine and a surface agree on for
// automations: the card a proposal raises, the answer it waits for, the door a
// conversation's config carries, and the question the card is on the block as.
// docs/design/automations/DESIGN.md is the design.
//
// THE QUESTION IS BUILT ONCE, HERE. The standing card it replaces was built in
// the engine AND again in the surface, and the two copies disagreed about
// whether the turn was stopped on it. A surface that wants to draw this card
// reads [AutomationQuestion] and adds nothing of its own to the answers.

import (
	"strings"
	"time"

	"github.com/Agent-Field/codeaf/internal/automation"
)

// QuestionAutomation is an automation card: should this be saved?
const QuestionAutomation QuestionKind = "automation"

// The keys an automation card answers with. They never move, and the no is
// every card's outright no ([DeclineKey]) rather than a digit of its own.
const (
	AutomationSaveKey   = "1"
	AutomationRunNowKey = "2"
	AutomationNoKey     = DeclineKey
)

// AutomationNotice is one automation card, or one report about an automation.
type AutomationNotice struct {
	// ID names the card until it is answered. A report carries none.
	ID         uint64                `json:"id,omitempty"`
	Automation automation.Automation `json:"automation"`
	// WhenWords is the schedule as the person would say it; Exact is the same
	// schedule exactly — the moment, or the rhythm with its zone and line.
	WhenWords string `json:"whenWords,omitempty"`
	Exact     string `json:"exact,omitempty"`
	// Next is when it would first run.
	Next time.Time `json:"next,omitempty"`
	// CostWords is a watch's estimated cost a day, and empty when nobody can
	// say: an unknown price is drawn as nothing, never as $0.00.
	CostWords string `json:"costWords,omitempty"`
	// Options are the card's answers, from [AutomationOptions].
	Options []AnswerOption `json:"options,omitempty"`
	// Update is set on a report: "saved", "paused", "resumed", "deleted",
	// "running". Text is its one line.
	Update string `json:"update,omitempty"`
	Text   string `json:"text,omitempty"`
}

// AutomationAnswer is what the person said to a card.
type AutomationAnswer struct {
	// Save keeps it.
	Save bool `json:"save,omitempty"`
	// RunNow keeps it and does it once, now, in this turn — while the person
	// is watching, so any approval it needs is asked of them and an "always"
	// answer is banked for the unattended runs after it.
	RunNow bool `json:"runNow,omitempty"`
	// Change is the person's correction in their own words. Nothing is saved;
	// the model proposes again.
	Change string `json:"change,omitempty"`
}

// Automations is the door to the automations store a conversation's config
// carries. Nil is off.
type Automations struct {
	Store *automation.Store
	// Zone is the zone a new automation's rhythm is read in. Empty asks the
	// machine ([LocalZone]).
	Zone string
}

// ResolveAutomation delivers the person's answer to the card that is waiting
// on it. An answer for a card nobody is waiting on is dropped.
func (a *Agent) ResolveAutomation(id uint64, answer AutomationAnswer) {
	a.mu.Lock()
	answers, waiting := a.automationAnswers[id]
	if waiting {
		delete(a.automationAnswers, id)
	}
	a.mu.Unlock()
	if waiting {
		answers <- answer
	}
}

// AutomationHead is the card's first line: what KIND of thing is being asked.
func AutomationHead(item automation.Automation) string {
	switch item.Kind() {
	case automation.KindReminder:
		return "wants to remind you"
	case automation.KindWatch:
		return "wants to watch for something"
	}
	return "wants to schedule work"
}

// AutomationRunsNow reports whether "save and run it now" means anything for
// this automation. A reminder has nothing to try — its whole content is the
// moment — and work kept in a separate worktree cannot run in an attended turn,
// which works in the checkout.
func AutomationRunsNow(item automation.Automation) bool {
	switch {
	case item.Kind() == automation.KindReminder:
		return false
	case item.Worktree:
		return false
	}
	return true
}

// AutomationOptions are one card's answers. The keys do not move: 1 saves, 2
// saves and runs it now, 0 saves nothing. The decline is last and safe.
func AutomationOptions(item automation.Automation) []AnswerOption {
	options := []AnswerOption{{Key: AutomationSaveKey, Label: "Save", Consequence: automationSaveConsequence(item)}}
	if AutomationRunsNow(item) {
		label, consequence := "Save and run it now", "Saves it, then does it once now, here, while you watch."
		if item.Kind() == automation.KindWatch {
			label, consequence = "Save and check it now", "Saves it, then takes one look now, here, while you watch."
		}
		options = append(options, AnswerOption{Key: AutomationRunNowKey, Label: label, Consequence: consequence})
	}
	return append(options, AnswerOption{Key: AutomationNoKey, Label: "Don't save", Consequence: "Nothing is saved and nothing runs.", Safe: true})
}

// automationSaveConsequence is the one dim line under "Save".
func automationSaveConsequence(item automation.Automation) string {
	switch {
	case item.Kind() == automation.KindWatch:
		return "It looks on that rhythm while codeaf is open, and tells you when it happens."
	case item.Schedule.Repeats():
		return "It runs on that rhythm while codeaf is open, until you stop it."
	}
	return "It runs once, then. If codeaf is closed then, it runs when you next open it."
}

// AutomationChangeHint is what the correction box says.
const AutomationChangeHint = "say what to change — when, what it does, where it runs"

// AutomationAskReason is why every automation card is up.
const AutomationAskReason = "nothing is saved until you say so"

// AutomationQuestion is one card as a question on the block — the ONE
// construction of it, which the engine publishes and a surface draws.
func AutomationQuestion(notice AutomationNotice) Question {
	options := notice.Options
	if len(options) == 0 {
		options = AutomationOptions(notice.Automation)
	}
	return Question{
		ID:      notice.ID,
		Kind:    QuestionAutomation,
		Ask:     AskChoice,
		Form:    FormCard,
		Asker:   Asker{Kind: AskerModel},
		Head:    AutomationHead(notice.Automation),
		Reason:  AutomationAskReason,
		Subject: SubjectRef{Kind: SubjectOrder, ID: notice.ID, Name: strings.TrimSpace(notice.Automation.Title)},
		Options: options,
		Stakes:  StakesReversible,
		// THE TURN IS STOPPED ON IT: the `automation` call waits for the answer
		// and the card carries no clock.
		Blocking: Blocking{Turn: true},
		Scope:    []AnswerScope{ScopeOnce},
		Input:    InputShape{Kind: InputText, Prompt: AutomationChangeHint},
	}
}

// automationAnswerOf reads a key, or words, as an automation answer. Words
// alone are a correction.
func automationAnswerOf(key, words string) (AutomationAnswer, bool) {
	switch strings.TrimSpace(key) {
	case "":
		if strings.TrimSpace(words) == "" {
			return AutomationAnswer{}, false
		}
		return AutomationAnswer{Change: strings.TrimSpace(words)}, true
	case AutomationSaveKey:
		return AutomationAnswer{Save: true}, true
	case AutomationRunNowKey:
		return AutomationAnswer{Save: true, RunNow: true}, true
	case AutomationNoKey:
		return AutomationAnswer{}, true
	}
	return AutomationAnswer{}, false
}
