package session

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const awayAfter = 10 * time.Minute

// autonomyClock is THE ONE DERIVATION of how long a question's clock runs before
// the asker's own pick is taken, and it is deliberately the only arithmetic in
// this program about that length.
//
// THE NUMBER IS THE PERSON'S OWN. `set` is what they chose for this shape of
// question ([Agent.SetAutonomy]); where they chose a shape and no length, it is
// [awayAfter] — the boundary this program already uses everywhere else to
// conclude that nobody is at the keyboard. So a pick is taken for somebody only
// after exactly as long as it would take to decide they are not coming, and
// there is no second figure anywhere that could drift from it.
//
// A CLOCK IS OPT-IN AND STAYS OPT-IN (owner ruling, 2026-09-11): nothing here
// gives a question a clock. [Agent.autonomyFor] decides WHETHER there is one —
// only where the person set a rule, plus the assumption kind, whose whole shape
// is that its options stand unless somebody strikes them — and this decides only
// how long.
func autonomyClock(set time.Duration) time.Duration {
	if set > 0 {
		return set
	}
	return awayAfter
}

// autonomyFile is deliberately inside the project: the same kind of question
// may deserve a different answer in two projects, and neither should leak.
func (a *Agent) autonomyFile() string {
	root := strings.TrimSpace(a.config.Workspace)
	if root == "" {
		return ""
	}
	return filepath.Join(root, ".codeaf", "autonomy.json")
}

func (a *Agent) legacyAutonomyFile() string {
	root := strings.TrimSpace(a.config.Workspace)
	if root == "" {
		return ""
	}
	return filepath.Join(root, ".aforge", "autonomy.json") // legacy-name
}

// SetAutonomy is the one door surfaces use for the D-key promise.
func (a *Agent) SetAutonomy(kind AskKind, policy Policy) error {
	// THE TWO ROWS NOBODY MAY CHANGE, refused at the door that writes them so
	// that no surface has to hold a second copy of the rule.
	//
	// CONFIRMATION ALWAYS ASKS. It is what is asked before something
	// destructive, and stop.go's law — "no bypass key, no modifier that skips
	// the question, and no don't-ask-me-again" — is that sentence about this
	// shape. CLARIFICATION NEVER RUNS ON A CLOCK, because the answer is
	// information only the person has: there is nothing for a clock to take.
	if kind == AskConfirmation && policy.Kind != PolicyAsk {
		return errors.New("confirmation is asked before something destructive · it always asks")
	}
	if kind == AskClarification && policy.Kind != PolicyAsk {
		return errors.New("clarification never runs on a clock · only you have that answer")
	}
	if policy.Kind == "" {
		policy.Kind = PolicyAsk
	}
	if policy.Kind == PolicyRecommendThenAuto {
		// A LENGTH IS OPTIONAL AND NEVER MISSING. A row set from the settings
		// sheet says "recommend then go" and names no number, and refusing it
		// would make the one door harder to reach than the slash command it
		// exists to replace ([autonomyClock] is where the figure comes from).
		policy.After = autonomyClock(policy.After)
	}
	path := a.autonomyFile()
	if path == "" {
		return errors.New("this conversation has no project for autonomy settings")
	}
	settings := a.readAutonomy()
	settings[kind] = policy
	data, err := json.MarshalIndent(settings, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	tmp := path + ".new"
	if err := os.WriteFile(tmp, append(data, '\n'), 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

func (a *Agent) readAutonomy() map[AskKind]Policy {
	settings := make(map[AskKind]Policy)
	data, err := os.ReadFile(a.autonomyFile())
	if os.IsNotExist(err) {
		data, err = os.ReadFile(a.legacyAutonomyFile())
	}
	if err == nil {
		_ = json.Unmarshal(data, &settings)
	}
	return settings
}

// Autonomy returns this project's explicit rows for a surface. The map is a
// copy: changing a row still goes through SetAutonomy, where the safety floors
// and atomic write live.
func (a *Agent) Autonomy() map[AskKind]Policy {
	stored := a.readAutonomy()
	rules := make(map[AskKind]Policy, len(stored))
	for kind, policy := range stored {
		rules[kind] = policy
	}
	return rules
}

func (a *Agent) autonomyFor(kind AskKind) Policy {
	// The same two rows, read back. A file edited by hand cannot make either of
	// them run on a clock either — SetAutonomy is the door, and this is the
	// floor under it.
	if kind == AskClarification || kind == AskConfirmation {
		return Policy{Kind: PolicyAsk}
	}
	if policy, ok := a.readAutonomy()[kind]; ok {
		if policy.Kind == PolicyRecommendThenAuto {
			policy.After = autonomyClock(policy.After)
		}
		return policy
	}
	if kind == AskAssumption {
		// THE ONE KIND WITH A CLOCK NOBODY ASKED FOR, and it is the kind whose
		// own shape is that nothing is decided at the end of it: an assumptions
		// card's options STAND unless they are struck, so the clock ends a
		// reading and not a decision (docs/design/questions/DESIGN.md's table).
		return Policy{Kind: PolicyRecommendThenAuto, After: autonomyClock(0)}
	}
	return Policy{Kind: PolicyAsk}
}

func memoryScopeForAnswer(scope AnswerScope) string {
	switch scope {
	case ScopeProject, ScopeTask:
		return "project"
	default:
		return "user"
	}
}
