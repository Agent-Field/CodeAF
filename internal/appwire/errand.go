package appwire

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/Agent-Field/codeaf/internal/effort"
	"github.com/Agent-Field/codeaf/internal/roles"
	"github.com/Agent-Field/codeaf/internal/session"
)

// ── agents.errand: a short, detached, metered completion on an agent's door ──
//
// The app brings its own prompt; the engine brings the agent's rails (its
// admission, provider and keys, one-model promise, job budget, receipts and
// lifetime). Nothing here knows what the errand is for: purpose is a ledger
// label and the key of its one-in-flight slot. session's errand.go is the core.

const (
	FeatureErrand       = "agents.errand"
	FeatureErrandModels = "agents.errand.models"
	// CodeModelNotAllowed refuses a role or model the agent's model policy
	// does not let an errand use: unknown, not served here, or one-model mode.
	CodeModelNotAllowed = "model_not_allowed"
	// ErrandNotOpen and ErrandUnavailable are the two reasons the app door adds
	// to session's: the agent is not held by this engine (agents.open first),
	// or the engine that holds it has no errand door (an older remote owner).
	ErrandNotOpen     = "not_open"
	ErrandUnavailable = "unavailable"
)

// ErrandCaps is what this engine's errand accepts. It rides the hello and
// agents.errand.models, so a client reads its limits rather than guessing.
type ErrandCaps struct {
	// V is the highest ask version this engine answers.
	V int `json:"v"`
	// Roles are the role names an errand may name.
	Roles             []string `json:"roles"`
	Model             bool     `json:"model"`
	Efforts           []string `json:"efforts"`
	JSON              bool     `json:"json"`
	JSONSchema        bool     `json:"json_schema"`
	Images            bool     `json:"images"`
	MaxInputBytes     int      `json:"max_input_bytes"`
	MaxMessages       int      `json:"max_messages"`
	MaxSchemaBytes    int      `json:"max_schema_bytes"`
	MaxCacheKey       int      `json:"max_cache_key"`
	MaxTokens         int      `json:"max_tokens"`
	DefaultTokens     int      `json:"default_tokens"`
	DefaultDeadlineMS int      `json:"default_deadline_ms"`
	MaxDeadlineMS     int      `json:"max_deadline_ms"`
}

// CurrentErrandCaps is this build's errand, read from session's own limits.
func CurrentErrandCaps() ErrandCaps {
	names := []string{}
	for _, role := range roles.Registered() {
		names = append(names, string(role))
	}
	return ErrandCaps{
		V: session.ErrandVersion, Roles: names, Model: true,
		Efforts: []string{string(effort.Low), string(effort.Medium), string(effort.High), string(effort.XHigh), string(effort.Max)},
		JSON:    true, JSONSchema: true,
		MaxInputBytes: session.ErrandMaxInputBytes, MaxMessages: session.ErrandMaxMessages,
		MaxSchemaBytes: session.ErrandMaxSchemaBytes, MaxCacheKey: session.ErrandMaxCacheKey,
		MaxTokens: session.ErrandMaxTokens, DefaultTokens: session.ErrandDefaultTokens,
		DefaultDeadlineMS: int(session.ErrandDefaultDeadline / time.Millisecond),
		MaxDeadlineMS:     int(session.ErrandMaxDeadline / time.Millisecond),
	}
}

// ErrandMessage is one message of the errand's dialogue.
type ErrandMessage struct {
	Role string `json:"role" enum:"user,assistant"`
	Text string `json:"text"`
}

// ErrandParams is one agents.errand request. Unknown fields are ignored, so a
// newer client's additions reach an older engine as absent.
type ErrandParams struct {
	V          int             `json:"v"`
	Agent      string          `json:"agent"`
	Purpose    string          `json:"purpose"`
	Role       string          `json:"role"`
	Model      string          `json:"model"`
	Effort     string          `json:"effort"`
	System     string          `json:"system"`
	Messages   []ErrandMessage `json:"messages"`
	JSON       bool            `json:"json"`
	JSONSchema json.RawMessage `json:"json_schema"`
	MaxTokens  int             `json:"max_tokens"`
	DeadlineMS int             `json:"deadline_ms"`
	Cache      string          `json:"cache"`
}

// ErrandUsage is the call's tokens, and its price when the provider said one.
type ErrandUsage struct {
	In      int     `json:"in"`
	Out     int     `json:"out"`
	CostUSD float64 `json:"cost_usd,omitempty"`
}

// Errand is an errand's answer. Text is "" exactly when Reason is set.
type Errand struct {
	Text     string      `json:"text"`
	Model    string      `json:"model,omitempty"`
	Provider string      `json:"provider,omitempty"`
	Role     string      `json:"role,omitempty"`
	Usage    ErrandUsage `json:"usage"`
	MS       int64       `json:"ms"`
	Cached   bool        `json:"cached,omitempty"`
	Reason   string      `json:"reason,omitempty" enum:"closed,stopped,over_budget,busy,deadline,cancelled,model_failed,invalid_json,not_open,unavailable"`
}

// ErrandModelsParams names the agent whose routes to read; empty reads the
// profile's routes with no agent behind them.
type ErrandModelsParams struct {
	Agent string `json:"agent"`
}

// ErrandRoute is where one role's errand goes now.
type ErrandRoute struct {
	Role     string `json:"role"`
	Model    string `json:"model"`
	Provider string `json:"provider,omitempty"`
	Effort   string `json:"effort,omitempty"`
	JSON     *bool  `json:"json,omitempty"`
}

// ErrandModel is one model an errand may name. JSON and JSONSchema are
// absent when no catalog knows.
type ErrandModel struct {
	ID           string `json:"id"`
	Provider     string `json:"provider"`
	ProviderName string `json:"provider_name,omitempty"`
	JSON         *bool  `json:"json,omitempty"`
	JSONSchema   *bool  `json:"json_schema,omitempty"`
}

// ErrandModels is every role and model this engine can serve an errand on
// right now. OneModel, when set, is the only model the agent's errands use.
type ErrandModels struct {
	Caps     ErrandCaps    `json:"caps"`
	OneModel string        `json:"one_model,omitempty"`
	Roles    []ErrandRoute `json:"roles"`
	Models   []ErrandModel `json:"models"`
}

// Errander is the engine's errand door.
type Errander interface {
	Errand(ctx context.Context, agent string, ask session.ErrandAsk) (session.ErrandAnswer, error)
	ErrandModels(ErrandModelsParams) (ErrandModels, error)
}

func withErrandFeature(h Hello, e Engine) Hello {
	if _, ok := e.(Errander); !ok {
		return h
	}
	caps := CurrentErrandCaps()
	h.Errand = &caps
	return withFeature(withFeature(h, FeatureErrand), FeatureErrandModels)
}

// ask is the request as session's errand, checked against its limits.
func (p ErrandParams) ask() (session.ErrandAsk, error) {
	if p.V > session.ErrandVersion {
		return session.ErrandAsk{}, Errorf(CodeBadParams, "this engine answers errand v%d, not v%d", session.ErrandVersion, p.V)
	}
	if p.Agent == "" {
		return session.ErrandAsk{}, Errorf(CodeBadParams, "agent is required")
	}
	messages := make([]session.ErrandMessage, len(p.Messages))
	for i, m := range p.Messages {
		messages[i] = session.ErrandMessage(m)
	}
	ask, err := session.ErrandAsk{
		Purpose: p.Purpose, Role: p.Role, Model: p.Model, Effort: p.Effort,
		System: p.System, Messages: messages, JSON: p.JSON, JSONSchema: p.JSONSchema,
		MaxTokens: p.MaxTokens, Deadline: time.Duration(p.DeadlineMS) * time.Millisecond, Cache: p.Cache,
	}.Normalize()
	if err != nil {
		return ask, Errorf(CodeBadParams, "%v", err)
	}
	return ask, nil
}

// errandError is a session refusal as the wire spells it.
func errandError(err error) error {
	switch {
	case errors.Is(err, session.ErrErrandAsk):
		return Errorf(CodeBadParams, "%v", err)
	case errors.Is(err, session.ErrErrandRoute):
		return Errorf(CodeModelNotAllowed, "%v", err)
	}
	return err
}

func errandOf(answer session.ErrandAnswer) *Errand {
	return &Errand{
		Text: answer.Text, Model: answer.Model, Provider: answer.Provider, Role: answer.Role,
		Usage: ErrandUsage(answer.Usage), MS: answer.Took.Milliseconds(), Cached: answer.Cached, Reason: answer.Reason,
	}
}

// errandGrace is how long past the errand's own deadline the wire waits, so
// the engine's deadline answers with its reason rather than the wire's.
const errandGrace = 2 * time.Second

func init() {
	methods[FeatureErrand] = typed(func(e Engine, p ErrandParams) (*Errand, error) {
		door, ok := e.(Errander)
		if !ok {
			return nil, Errorf(CodeUnknownMethod, "errands are unavailable")
		}
		ask, err := p.ask()
		if err != nil {
			return nil, err
		}
		ctx, cancel := context.WithTimeout(context.Background(), ask.Deadline+errandGrace)
		defer cancel()
		answer, err := door.Errand(ctx, p.Agent, ask)
		if err != nil {
			return nil, errandError(err)
		}
		return errandOf(answer), nil
	})
	methods[FeatureErrandModels] = typed(func(e Engine, p ErrandModelsParams) (*ErrandModels, error) {
		door, ok := e.(Errander)
		if !ok {
			return nil, Errorf(CodeUnknownMethod, "errands are unavailable")
		}
		models, err := door.ErrandModels(p)
		if err != nil {
			return nil, err
		}
		models.Caps = CurrentErrandCaps()
		models.Roles, models.Models = nonNil(models.Roles), nonNil(models.Models)
		return &models, nil
	})
}
