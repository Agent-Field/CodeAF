package tui3

// ── /autonomy — WHAT THIS PROJECT DOES WHILE NOBODY IS THERE ────────────────
//
// THE SHEET, EXACTLY AS IT IS DRAWN:
//
//	questions while you are away
//	permission       ask me
//	choice           recommend, auto in 30s
//	judgement        ask me
//	clarification    ask me · never runs on a clock
//	confirmation     ask me · destructive always asks
//	landing          decide yourself
//	assumptions      recommend, auto in 10m
//	already done     ask me
//	/autonomy <kind> ask · recommend <duration> · decide
//
// ── WHY IT IS PER PROJECT AND NOT PER PROFILE ──
//
// The engine stores it in the project (`.aforge/autonomy.json`, session's
// autonomy.go), and the reason is the whole point of the setting: the same shape
// of question deserves a different answer in two pieces of work. `permission` on
// a scratch repository somebody is exploring is a key press; `permission` on the
// thing that deploys is a decision. A profile-wide rule would make one of those
// two wrong everywhere.
//
// ── THE TWO ROWS NOBODY MAY CHANGE ──
//
// CONFIRMATION ALWAYS ASKS. It is the shape asked before something destructive,
// and stop.go's law — "there is no bypass key, no modifier that skips the
// question, and no don't-ask-me-again" — is that sentence about this row.
//
// CLARIFICATION NEVER RUNS ON A CLOCK. The answer is information only the person
// has, so a clock could not take it: there is nothing for it to take.
//
// The engine refuses both at its own door ([session.Agent.SetAutonomy]) and this
// sheet says so on the rows, because a setting that looks changeable and is not
// is worse than one that says why.

import (
	"strings"
	"time"

	"github.com/Agent-Field/aforge-v2/internal/session"
)

// The sheet's words, spelled once and quoted in the manual exactly.
const (
	autonomyHeadWord    = "questions while you are away"
	autonomyAskWord     = "ask me"
	autonomyDecideWord  = "decide yourself"
	autonomyAlwaysWord  = "destructive always asks"
	autonomyNoClockWord = "never runs on a clock"
	// autonomyUsageWord is the foot, and it is the only place the sheet names a
	// door. An earlier draft put `· change` on every row, which is a word with
	// no key behind it — furniture that tells somebody a thing is changeable
	// without telling them how.
	autonomyUsageWord = "/autonomy <kind> ask · recommend <duration> · decide"
	// autonomyNoProjectWord is the refusal for a conversation with no project to
	// store rules in. It says what is missing rather than that something failed.
	autonomyNoProjectWord = "this conversation has no project to keep question rules in"
	// autonomyColumn is how wide the kind column is. It is one number so the
	// rows and the headings cannot drift apart.
	autonomyColumn = 16
)

// autonomyKinds is every shape a rule can be written for, in the kind table's
// own order (docs/design/questions/DESIGN.md), which is [questionShapeOrder]'s.
var autonomyKinds = []session.AskKind{
	session.AskPermission, session.AskChoice, session.AskJudgement,
	session.AskClarification, session.AskConfirmation, session.AskLanding,
	session.AskAssumption, session.AskRatify,
}

// autonomyAgent is the autonomy half of the agent under this surface, when it
// has one. It is an optional assertion for [questionAgent]'s reason exactly: an
// agent that has never heard of question rules keeps everything else it had.
//
// IT WIDENS [questionDialDoor] RATHER THAN SITTING BESIDE IT, so `D` and this
// sheet cannot end up asking two different objects whether this project keeps
// rules: the room's key needs only the write, and reading the rows back needs
// both.
type autonomyAgent interface {
	questionDialDoor
	Autonomy() map[session.AskKind]session.Policy
}

// autonomySheetText is the whole sheet as one block of prose, which is how a
// slash command answers on this surface.
func (a *app) autonomySheetText() string {
	agent, ok := a.agent.(autonomyAgent)
	if !ok {
		return autonomyNoProjectWord
	}
	rules := agent.Autonomy()
	lines := make([]string, 0, len(autonomyKinds)+2)
	lines = append(lines, autonomyHeadWord)
	for _, kind := range autonomyKinds {
		word, tail := autonomyRuleWord(rules[kind]), ""
		switch kind {
		case session.AskConfirmation:
			word, tail = autonomyAskWord, " · "+autonomyAlwaysWord
		case session.AskClarification:
			word, tail = autonomyAskWord, " · "+autonomyNoClockWord
		}
		// THE COLUMN IS THE KIND'S OWN SPELLING AND NOT THE SHEET'S PROSE FOR
		// IT ([questionShapeWord]), because this column is also what a person
		// TYPES: `/autonomy permission ask` is the door, and a sheet that read
		// `asking permission` would be a sheet you cannot copy a word out of.
		name := string(kind)
		if pad := autonomyColumn - len(name); pad > 0 {
			name += strings.Repeat(" ", pad)
		}
		lines = append(lines, name+" "+word+tail)
	}
	return strings.Join(append(lines, autonomyUsageWord), "\n")
}

func autonomyRuleWord(rule session.Policy) string {
	switch rule.Kind {
	case session.PolicyDecide:
		return autonomyDecideWord
	case session.PolicyRecommendThenAuto:
		return "recommend, auto in " + shortAutonomyDuration(rule.After)
	default:
		return autonomyAskWord
	}
}

func shortAutonomyDuration(after time.Duration) string {
	if after <= 0 {
		return "now"
	}
	return after.Round(time.Second).String()
}

func (a *app) changeAutonomy(words string) string {
	parts := strings.Fields(words)
	if len(parts) < 2 {
		return autonomyUsageWord
	}
	kind := autonomyKindNamed(parts[0])
	if kind == "" {
		return "there is no question kind called " + parts[0] + " · " + autonomyUsageWord
	}
	// THE TWO REFUSALS ARE THE ENGINE'S AND ARE NOT RE-SPELLED HERE. Its door
	// turns down a rule over a confirmation and over a clarification, in its own
	// words, and this command prints them; a copy of the test on this side would
	// be a second definition of a law that has one (ONE SOURCE OF TRUTH).
	rule := session.Policy{Kind: session.PolicyAsk}
	switch parts[1] {
	case "ask":
	case "decide":
		rule.Kind = session.PolicyDecide
	case "recommend":
		if len(parts) != 3 {
			return autonomyUsageWord
		}
		after, err := time.ParseDuration(parts[2])
		if err != nil || after <= 0 {
			return "that duration is not understood: " + parts[2]
		}
		rule.Kind, rule.After = session.PolicyRecommendThenAuto, after
	default:
		return "choose ask, recommend <duration>, or decide"
	}
	agent, ok := a.agent.(autonomyAgent)
	if !ok {
		return autonomyNoProjectWord
	}
	if err := agent.SetAutonomy(kind, rule); err != nil {
		// THE ENGINE'S REFUSAL IS THE PERSON'S TO READ, whole. It already ends
		// in something they can do, and re-wording it here would be a second
		// account of one rule.
		return err.Error()
	}
	a.autonomyChanged()
	return string(kind) + " · " + autonomyRuleWord(rule) + " · for this project"
}

// autonomyKindNamed reads one word as a question kind, by the engine's own
// spelling for it and by the words the sheet prints for it — somebody changing
// a row types what they just read, and `/autonomy asking permission ask` is not
// a thing anybody would type, but `/autonomy permission ask` is.
func autonomyKindNamed(word string) session.AskKind {
	word = strings.TrimSpace(strings.ToLower(word))
	for _, kind := range autonomyKinds {
		if word == string(kind) {
			return kind
		}
	}
	return ""
}

// ── the rule a question is answered under ───────────────────────────────────

// autonomyRuled reports whether this project has written down a rule for one
// shape of question. It is what puts `· your rule` on a running clock
// ([app.questionClockWord]).
//
// IT IS READ WHEN A QUESTION IS RAISED AND NEVER ON THE DRAW PATH. The rows
// live in a file, and the draw path is rebuilt from nothing on every frame; a
// read per frame would be a file opened sixty times a second to answer a
// question whose answer changes when somebody types a slash command.
//
// AND AN ABSENT DOOR IS NO RULE RATHER THAN AN UNKNOWN ONE. A surface with no
// autonomy door — a fake in a test, a conversation with no project — has no
// stored rows to wear, so the row says nothing, which is the emptiness law.
func (a *app) autonomyRuled(kind session.AskKind) bool {
	if kind == "" {
		return false
	}
	if a.autonomyRules == nil {
		agent, ok := a.agent.(autonomyAgent)
		if !ok {
			return false
		}
		rules := agent.Autonomy()
		if rules == nil {
			rules = map[session.AskKind]session.Policy{}
		}
		a.autonomyRules = rules
	}
	rule, ok := a.autonomyRules[kind]
	return ok && rule.Kind != "" && rule.Kind != session.PolicyAsk
}

// autonomyChanged forgets the cached rows, so the next question raised reads
// what was just written rather than what was there before it.
func (a *app) autonomyChanged() { a.autonomyRules = nil }
