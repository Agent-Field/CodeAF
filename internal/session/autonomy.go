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

// autonomyFile is deliberately inside the project: the same kind of question
// may deserve a different answer in two projects, and neither should leak.
func (a *Agent) autonomyFile() string {
	root := strings.TrimSpace(a.config.Workspace)
	if root == "" {
		return ""
	}
	return filepath.Join(root, ".aforge", "autonomy.json")
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
	if policy.Kind == PolicyRecommendThenAuto && policy.After <= 0 {
		return errors.New("recommend-then-auto needs a positive wait")
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
		return policy
	}
	if kind == AskAssumption {
		return Policy{Kind: PolicyRecommendThenAuto, After: awayAfter}
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
