package appwire

import (
	"strings"
	"time"

	"github.com/Agent-Field/codeaf/internal/config"
)

// Wave 8: the Settings window (PROTOCOL.md). settings.get and settings.set
// carry every row; the lists a row opens — always-allow rules, memories, the
// spending export, pinned models — have methods of their own.

// The choices of the General rows.
const (
	AppearanceSystem = "system"
	AppearanceLight  = "light"
	AppearanceDark   = "dark"

	TextSmall  = "small"
	TextMedium = "medium"
	TextLarge  = "large"
)

// PinnedModelsMax is how many models can be pinned (⌥⌘1–3).
const PinnedModelsMax = 3

// Rule is one always-allow rule: a tool's standing answer, or a command
// pattern's.
type Rule struct {
	ID       string            `json:"id"`
	Text     string            `json:"text"`
	Origin   config.RuleOrigin `json:"origin"`
	Uses     uint64            `json:"uses"`
	LastUsed time.Time         `json:"last_used,omitzero"`
	Kind     string            `json:"kind"`
	Tool     string            `json:"tool,omitempty"`
	Match    string            `json:"match,omitempty"`
	Action   string            `json:"action"`
}

// The kinds of a rule.
const (
	RuleTool = "tool"
	RuleBash = "bash"
)

// Memory is one thing codeaf remembers: its words, whose it is, and who saved
// it.
type Memory struct {
	ID      string `json:"id"`
	Title   string `json:"title,omitempty"`
	Text    string `json:"text"`
	Owner   string `json:"owner"`
	Project string `json:"project,omitempty"`
	Origin  string `json:"origin,omitempty"`
}

// Settler is an engine that serves the Settings window's lists. It is
// asserted, as Crewer is.
type Settler interface {
	Rules() ([]Rule, error)
	RemoveRule(id string) error
	Memories(limit int) ([]Memory, error)
	ForgetMemory(id string) error
	ExportSpend(from, to time.Time) (string, error)
	PinModels(ids []string) (Settings, error)
}

func init() {
	for name, m := range settingsMethods {
		methods[name] = m
	}
}

func settler(e Engine, name string) (Settler, error) {
	s, ok := e.(Settler)
	if !ok {
		return nil, Errorf(CodeUnknownMethod, "this engine does not serve %s", name)
	}
	return s, nil
}

// settleCall decodes a method's params into P and answers what call answers.
func settleCall[P, R any](name string, call func(Settler, P) (*R, error)) method {
	return typed(func(e Engine, p P) (*R, error) {
		s, err := settler(e, name)
		if err != nil {
			return nil, err
		}
		return call(s, p)
	})
}

// exportDay reads one end of an export's range, a calendar day.
func exportDay(name, day string) (time.Time, error) {
	at, err := time.ParseInLocation("2006-01-02", strings.TrimSpace(day), time.Local)
	if err != nil {
		return time.Time{}, Errorf(CodeBadParams, "%s is a day, as 2026-10-15", name)
	}
	return at, nil
}

var settingsMethods = map[string]method{
	"rules.list": settleCall("rules.list", func(s Settler, _ struct{}) (*RulesResult, error) {
		rules, err := s.Rules()
		if err != nil {
			return nil, err
		}
		return &RulesResult{nonNil(rules)}, nil
	}),
	"rules.remove": settleCall("rules.remove", func(s Settler, p crewID) (*Empty, error) {
		if err := needID("rules.remove", p.ID); err != nil {
			return nil, err
		}
		return done(s.RemoveRule(p.ID))
	}),
	"rules.restore": typed(func(e Engine, p crewID) (*Empty, error) {
		if err := needID("rules.restore", p.ID); err != nil {
			return nil, err
		}
		s, ok := e.(interface{ RestoreRule(string) error })
		if !ok {
			return nil, Errorf(CodeUnknownMethod, "this engine does not serve rules.restore")
		}
		return done(s.RestoreRule(p.ID))
	}),
	"memory.list": settleCall("memory.list", func(s Settler, p struct {
		Limit int `json:"limit"`
	}) (*MemoriesResult, error) {
		if p.Limit <= 0 || p.Limit > 500 {
			p.Limit = 200
		}
		memories, err := s.Memories(p.Limit)
		if err != nil {
			return nil, err
		}
		return &MemoriesResult{nonNil(memories)}, nil
	}),
	"memory.forget": settleCall("memory.forget", func(s Settler, p crewID) (*Empty, error) {
		if err := needID("memory.forget", p.ID); err != nil {
			return nil, err
		}
		return done(s.ForgetMemory(p.ID))
	}),
	"spend.export": settleCall("spend.export", func(s Settler, p struct {
		From string `json:"from"`
		To   string `json:"to"`
	}) (*PathResult, error) {
		from, err := exportDay("from", p.From)
		if err != nil {
			return nil, err
		}
		to, err := exportDay("to", p.To)
		if err != nil {
			return nil, err
		}
		if to.Before(from) {
			return nil, Errorf(CodeBadParams, "to is before from")
		}
		path, err := s.ExportSpend(from, to.AddDate(0, 0, 1))
		if err != nil {
			return nil, err
		}
		return &PathResult{path}, nil
	}),
	"models.pin": settleCall("models.pin", func(s Settler, p struct {
		IDs []string `json:"ids"`
	}) (*SettingsResult, error) {
		if len(p.IDs) > PinnedModelsMax {
			return nil, Errorf(CodeBadParams, "at most %d models can be pinned", PinnedModelsMax)
		}
		var ids []string
		for _, id := range p.IDs {
			if id = strings.TrimSpace(id); id == "" {
				return nil, Errorf(CodeBadParams, "a pinned model needs an id")
			}
			ids = append(ids, id)
		}
		settings, err := s.PinModels(ids)
		if err != nil {
			return nil, err
		}
		return &SettingsResult{settings}, nil
	}),
}

// The answers of this file's methods.
type (
	RulesResult struct {
		Rules []Rule `json:"rules"`
	}
	MemoriesResult struct {
		Memories []Memory `json:"memories"`
	}
	PathResult struct {
		Path string `json:"path"`
	}
)
