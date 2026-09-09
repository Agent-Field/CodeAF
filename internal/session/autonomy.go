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
	if kind == AskClarification && policy.Kind != PolicyAsk {
		return errors.New("clarification always waits for an answer")
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

func (a *Agent) autonomyFor(kind AskKind) Policy {
	if kind == AskClarification {
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
