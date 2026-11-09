package appwire

import (
	"context"
	"encoding/json"
	"strings"
	"time"
)

// ── factory.agentfield_* ─────────────────────────────────────────────────────
//
// A factory can be a node on an AgentField control plane, so AgentField's
// triggers (Linear, Sentry, Slack, cron, any HMAC or bearer caller, other
// agents) are its intake (internal/factory/agentfield). These six methods
// connect, read and forget that connection, and add triggers from the control
// plane's own catalog:
//
//	factory.agentfield_connect     {factory, url, api_key?, base_url, node_id?, listen?, trust?}
//	factory.agentfield_status      {factory}
//	factory.agentfield_disconnect  {factory}
//	factory.agentfield_triggers    {factory}   GET /api/v1/triggers, this node's only
//	factory.agentfield_sources     {factory}   GET /api/v1/sources, with config schemas
//	factory.agentfield_trigger_add {factory, source, config?, secret_env_var?, event_types?}
//
// THE API KEY TRAVELS IN ONE DIRECTION ONLY: it is kept where the GitHub
// token is kept and no answer, event or error ever carries it back, and
// neither does the node's address secret. A connect checks the address and
// the key against the control plane's source catalog, binds the node's
// listener and registers BEFORE it answers; a connect that cannot do all
// three keeps nothing.
//
// Wave 1 connects only the factory on this machine.

// FeatureFactoryAgentField is announced in hello when the engine serves the
// six methods.
const FeatureFactoryAgentField = "factory.agentfield"

// FactoryAgentFieldConnectParams is one connect.
type FactoryAgentFieldConnectParams struct {
	Factory string `json:"factory"`
	// URL is the control plane, such as http://agentfield.local:8080.
	URL string `json:"url"`
	// APIKey is the control plane's API key; empty for one that runs without.
	// It is never sent back.
	APIKey string `json:"api_key,omitempty"`
	// BaseURL is the address the control plane reaches this factory on over
	// HTTP, such as http://10.0.0.5:8765. EMPTY IS LINK ONLY: the factory
	// binds no port and is reached down the link it opens (a laptop behind
	// NAT).
	BaseURL string `json:"base_url,omitempty"`
	// InternalToken is the control plane's internal token, which it sends as
	// the bearer of every HTTP dispatch; the node refuses an HTTP dispatch
	// without it. Not needed for a link-only node. Never sent back.
	InternalToken string `json:"internal_token,omitempty"`
	// NodeID is the node the factory registers as; empty is codeaf-factory.
	NodeID string `json:"node_id,omitempty"`
	// Listen is the address the node binds; empty is base_url's port, on
	// loopback when base_url is loopback and on every interface otherwise.
	Listen string `json:"listen,omitempty"`
	// Trust is the most an entry from this control plane is trusted: owner,
	// collaborator (the default) or stranger.
	Trust string `json:"trust,omitempty"`
}

// FactoryAgentFieldStatus is a factory's AgentField connection as a person
// reads it. Every field but factory and connected is absent when unknown.
type FactoryAgentFieldStatus struct {
	Factory   string `json:"factory"`
	Connected bool   `json:"connected"`
	URL       string `json:"url,omitempty"`
	NodeID    string `json:"node_id,omitempty"`
	BaseURL   string `json:"base_url,omitempty"`
	Listen    string `json:"listen,omitempty"`
	Trust     string `json:"trust,omitempty"`
	// KeyKept says an API key is kept (never what it is).
	KeyKept bool `json:"key_kept,omitempty"`
	// TokenKept says the internal token is kept (never what it is).
	TokenKept bool `json:"token_kept,omitempty"`
	// Listening is the address the node is answering on now; always empty
	// for a link-only node.
	Listening string `json:"listening,omitempty"`
	// Link is the link's state: up, connecting, not offered.
	Link      string     `json:"link,omitempty"`
	LinkSince *time.Time `json:"link_since,omitempty"`
	LinkUps   int        `json:"link_ups,omitempty"`
	// LastEventVia is the carrier the last call came by: link or http.
	LastEventVia string `json:"last_event_via,omitempty"`
	// RegisteredAt is when the control plane last took the registration.
	RegisteredAt *time.Time `json:"registered_at,omitempty"`
	// LeaseSeconds and LeaseUntil are the last lease the control plane gave.
	LeaseSeconds int        `json:"lease_seconds,omitempty"`
	LeaseUntil   *time.Time `json:"lease_until,omitempty"`
	// LastEvent is when a call last reached the node, from which source, and
	// what became of it ("filed job 12 (ENG-4)", "filed nothing: …").
	LastEvent       *time.Time `json:"last_event,omitempty"`
	LastEventSource string     `json:"last_event_source,omitempty"`
	LastEventWhat   string     `json:"last_event_what,omitempty"`
	// LastError is the last thing that went wrong, cleared by the next
	// registration or lease that works.
	LastError   string     `json:"last_error,omitempty"`
	LastErrorAt *time.Time `json:"last_error_at,omitempty"`
	// Executions is how many AgentField executions are waiting on a job.
	Executions int `json:"executions,omitempty"`
}

// FactoryAgentFieldTrigger is one trigger that targets the factory's node.
type FactoryAgentFieldTrigger struct {
	ID         string   `json:"id"`
	Source     string   `json:"source"`
	Reasoner   string   `json:"reasoner"`
	EventTypes []string `json:"event_types,omitempty"`
	Enabled    bool     `json:"enabled"`
	// ManagedBy is "ui" for a trigger a person added, "code" for one an
	// agent declared.
	ManagedBy string `json:"managed_by,omitempty"`
	// SecretEnvVar names the control plane's environment variable that holds
	// the source's signing secret (its name only, never its value).
	SecretEnvVar string `json:"secret_env_var,omitempty"`
	// IngestURL is where the source posts its events: the control plane's
	// /sources/<id>.
	IngestURL   string     `json:"ingest_url,omitempty"`
	Events24h   int        `json:"events_24h,omitempty"`
	Failed24h   int        `json:"failed_24h,omitempty"`
	LastEventAt *time.Time `json:"last_event_at,omitempty"`
	// EntrySource is the source a job this trigger files carries,
	// agentfield:<source> with an underscore written as a hyphen
	// (agentfield:generic-bearer): what a routing rule's when.sources names.
	EntrySource string `json:"entry_source"`
	// Label is the trigger in a person's words: its source's label.
	Label string `json:"label,omitempty"`
	// WebhookURL is where the outside system posts its events: the same
	// address as ingest_url, and absent for a source the control plane runs
	// itself (kind loop, as cron), which nothing posts to.
	WebhookURL string     `json:"webhook_url,omitempty"`
	CreatedAt  *time.Time `json:"created_at,omitempty"`
}

// FactoryAgentFieldTriggers is the triggers that target the factory's node,
// and the control plane's source catalog a new one is picked from.
type FactoryAgentFieldTriggers struct {
	Factory  string                     `json:"factory"`
	NodeID   string                     `json:"node_id"`
	Sources  []FactoryAgentFieldSource  `json:"sources"`
	Triggers []FactoryAgentFieldTrigger `json:"triggers"`
}

// FactoryAgentFieldSource is one source of the control plane's catalog.
type FactoryAgentFieldSource struct {
	Name string `json:"name"`
	// Kind is "http" (the source posts to the ingest URL) or "loop" (the
	// control plane runs it, as cron).
	Kind           string          `json:"kind"`
	SecretRequired bool            `json:"secret_required"`
	ConfigSchema   json.RawMessage `json:"config_schema,omitempty"`
	// Label is the source in a person's words, such as Linear or Signed
	// webhook.
	Label string `json:"label"`
	// ConfigFields is the top-level fields its config schema names, for a
	// form to draw; absent when it names none.
	ConfigFields []FactoryAgentFieldConfigField `json:"config_fields,omitempty"`
}

// FactoryAgentFieldConfigField is one field of a source's config. Secret says
// a form draws it as a password field: its value is never shown again.
type FactoryAgentFieldConfigField struct {
	Name   string `json:"name"`
	Label  string `json:"label"`
	Secret bool   `json:"secret,omitempty"`
}

// FactoryAgentFieldSources is the control plane's source catalog.
type FactoryAgentFieldSources struct {
	Factory string                    `json:"factory"`
	Sources []FactoryAgentFieldSource `json:"sources"`
}

// FactoryAgentFieldTriggerAddParams adds one trigger targeting the factory's
// intake.
type FactoryAgentFieldTriggerAddParams struct {
	Factory string `json:"factory"`
	// Source is a catalog source's name, such as linear or generic_bearer.
	Source string `json:"source"`
	// Config is the source's config, shaped by its config_schema.
	Config json.RawMessage `json:"config,omitempty"`
	// SecretEnvVar names the control plane's environment variable holding
	// the source's signing secret; required by a source that needs one.
	SecretEnvVar string   `json:"secret_env_var,omitempty"`
	EventTypes   []string `json:"event_types,omitempty"`
}

// FactoryAgentFieldAddTriggerParams is factory.agentfield_add_trigger: one
// trigger aimed at the factory's intake. It is factory.agentfield_trigger_add
// in the shape the desk's trigger page sends.
type FactoryAgentFieldAddTriggerParams struct {
	Factory    string          `json:"factory"`
	Source     string          `json:"source"`
	EventTypes []string        `json:"event_types,omitempty"`
	Config     json.RawMessage `json:"config,omitempty"`
	// SecretEnvVar names the control plane's environment variable holding
	// the source's signing secret; a source that needs one is refused
	// without it. The control plane keeps the secret itself, never codeaf.
	SecretEnvVar string `json:"secret_env_var,omitempty"`
}

// FactoryAgentFieldAddedTrigger is the trigger made, and what the person does
// next to make it fire.
type FactoryAgentFieldAddedTrigger struct {
	Factory string                         `json:"factory"`
	Trigger FactoryAgentFieldTrigger       `json:"trigger"`
	Setup   *FactoryAgentFieldTriggerSetup `json:"setup,omitempty"`
}

// FactoryAgentFieldTriggerSetup is what the outside system needs: the
// address it posts to, and the name of the control plane's environment
// variable its signing secret is read from. Secret is never filled: AgentField
// reads its secrets from its own environment and never hands one back.
type FactoryAgentFieldTriggerSetup struct {
	WebhookURL   string `json:"webhook_url,omitempty"`
	Secret       string `json:"secret,omitempty"`
	SecretEnvVar string `json:"secret_env_var,omitempty"`
}

// FeatureFactoryAgentFieldTriggers is announced in hello when the engine
// serves factory.agentfield_triggers with its source catalog and
// factory.agentfield_add_trigger.
const FeatureFactoryAgentFieldTriggers = "factory.agentfield_triggers"

// FactoryAgentFieldTriggerAdder is the engine's half of
// factory.agentfield_add_trigger.
type FactoryAgentFieldTriggerAdder interface {
	FactoryAgentFieldAddTrigger(ctx context.Context, p FactoryAgentFieldAddTriggerParams) (FactoryAgentFieldAddedTrigger, error)
}

func withFactoryAgentFieldTriggersFeature(hello Hello, e Engine) Hello {
	if _, ok := e.(Factory); !ok {
		return hello
	}
	_, lists := e.(FactoryAgentField)
	if _, adds := e.(FactoryAgentFieldTriggerAdder); lists && adds {
		hello = withFeature(hello, FeatureFactoryAgentFieldTriggers)
	}
	return hello
}

// FactoryAgentField is the engine's half of the six methods.
type FactoryAgentField interface {
	FactoryAgentFieldConnect(ctx context.Context, p FactoryAgentFieldConnectParams) (FactoryAgentFieldStatus, error)
	FactoryAgentFieldStatus(ctx context.Context, id string) (FactoryAgentFieldStatus, error)
	FactoryAgentFieldDisconnect(ctx context.Context, id string) (FactoryAgentFieldStatus, error)
	FactoryAgentFieldTriggers(ctx context.Context, id string) (FactoryAgentFieldTriggers, error)
	FactoryAgentFieldSources(ctx context.Context, id string) (FactoryAgentFieldSources, error)
	FactoryAgentFieldTriggerAdd(ctx context.Context, p FactoryAgentFieldTriggerAddParams) (FactoryAgentFieldTrigger, error)
}

func withFactoryAgentFieldFeature(hello Hello, e Engine) Hello {
	if _, ok := e.(Factory); !ok {
		return hello
	}
	if _, ok := e.(FactoryAgentField); ok {
		hello = withFeature(hello, FeatureFactoryAgentField)
	}
	return hello
}

// CheckAgentFieldConnect bounds a connect without asking anyone. It never
// quotes the key in a refusal.
func CheckAgentFieldConnect(p FactoryAgentFieldConnectParams) (FactoryAgentFieldConnectParams, error) {
	var err error
	if p.Factory, err = factoryID(strings.TrimSpace(p.Factory)); err != nil {
		return p, err
	}
	p.URL, p.APIKey, p.BaseURL, p.InternalToken = strings.TrimSpace(p.URL), strings.TrimSpace(p.APIKey), strings.TrimSpace(p.BaseURL), strings.TrimSpace(p.InternalToken)
	p.NodeID, p.Listen, p.Trust = strings.TrimSpace(p.NodeID), strings.TrimSpace(p.Listen), strings.TrimSpace(p.Trust)
	switch {
	case p.URL == "":
		return p, Errorf(CodeBadParams, "url is required: the AgentField control plane, such as http://agentfield.local:8080")
	case strings.ContainsAny(p.APIKey, " \t\r\n\x00"):
		return p, Errorf(CodeBadParams, "an API key is one word")
	case len(p.APIKey) > 4096:
		return p, Errorf(CodeBadParams, "an API key is at most 4096 bytes")
	case strings.ContainsAny(p.InternalToken, " \t\r\n\x00") || len(p.InternalToken) > 4096:
		return p, Errorf(CodeBadParams, "the internal token is one word of at most 4096 bytes")
	}
	return p, nil
}

// CheckAgentFieldTriggerAdd bounds a trigger add.
func CheckAgentFieldTriggerAdd(p FactoryAgentFieldTriggerAddParams) (FactoryAgentFieldTriggerAddParams, error) {
	var err error
	if p.Factory, err = factoryID(strings.TrimSpace(p.Factory)); err != nil {
		return p, err
	}
	p.Source, p.SecretEnvVar = strings.TrimSpace(p.Source), strings.TrimSpace(p.SecretEnvVar)
	switch {
	case p.Source == "":
		return p, Errorf(CodeBadParams, "source is required: a name from factory.agentfield_sources, such as linear")
	case len(p.Config) > 64<<10:
		return p, Errorf(CodeBadParams, "config is at most 64 KiB")
	case len(p.EventTypes) > 50:
		return p, Errorf(CodeBadParams, "at most 50 event types")
	}
	if len(p.Config) > 0 {
		var obj map[string]any
		if json.Unmarshal(p.Config, &obj) != nil {
			return p, Errorf(CodeBadParams, "config is a JSON object shaped by the source's config_schema")
		}
	}
	var types []string
	for _, t := range p.EventTypes {
		t = strings.TrimSpace(t)
		switch {
		case t == "":
			continue
		case len(t) > 100 || strings.ContainsAny(t, " \t\r\n\x00"):
			return p, Errorf(CodeBadParams, "an event type is one word of at most 100 characters, such as Issue.create")
		}
		types = append(types, t)
	}
	p.EventTypes = types
	return p, nil
}

func init() {
	const connect = "factory.agentfield_connect"
	methods[connect] = shaped[FactoryAgentFieldConnectParams, FactoryAgentFieldStatus](func(e Engine, raw json.RawMessage) (*FactoryAgentFieldStatus, error) {
		a, err := agentFieldEngine(e, connect)
		if err != nil {
			return nil, err
		}
		var p FactoryAgentFieldConnectParams
		// A strict decode's message quotes the bad input, which may be the key
		// itself, so this one says only what shape was wanted.
		if err := decodeStrict(connect, raw, &p); err != nil {
			return nil, Errorf(CodeBadParams, "%s takes {factory, url, api_key, base_url, node_id, listen, trust}, all strings", connect)
		}
		if p, err = CheckAgentFieldConnect(p); err != nil {
			return nil, err
		}
		ctx, cancel := factoryCall()
		defer cancel()
		out, err := a.FactoryAgentFieldConnect(ctx, p)
		if err != nil {
			return nil, err
		}
		return &out, nil
	})
	for name, call := range map[string]func(FactoryAgentField, context.Context, string) (FactoryAgentFieldStatus, error){
		"factory.agentfield_status":     FactoryAgentField.FactoryAgentFieldStatus,
		"factory.agentfield_disconnect": FactoryAgentField.FactoryAgentFieldDisconnect,
	} {
		methods[name] = typed(func(e Engine, p FactoryRef) (*FactoryAgentFieldStatus, error) {
			return agentFieldRef(e, name, p, call)
		})
	}
	methods["factory.agentfield_triggers"] = typed(func(e Engine, p FactoryRef) (*FactoryAgentFieldTriggers, error) {
		return agentFieldRef(e, "factory.agentfield_triggers", p, FactoryAgentField.FactoryAgentFieldTriggers)
	})
	methods["factory.agentfield_sources"] = typed(func(e Engine, p FactoryRef) (*FactoryAgentFieldSources, error) {
		return agentFieldRef(e, "factory.agentfield_sources", p, FactoryAgentField.FactoryAgentFieldSources)
	})
	const add = "factory.agentfield_trigger_add"
	methods[add] = shaped[FactoryAgentFieldTriggerAddParams, FactoryAgentFieldTrigger](func(e Engine, raw json.RawMessage) (*FactoryAgentFieldTrigger, error) {
		a, err := agentFieldEngine(e, add)
		if err != nil {
			return nil, err
		}
		var p FactoryAgentFieldTriggerAddParams
		// A config may hold a value meant to stay private, and a decode's
		// message quotes the bad input, so this one says only the shape.
		if err := decodeStrict(add, raw, &p); err != nil {
			return nil, Errorf(CodeBadParams, "%s takes {factory, source, config, secret_env_var, event_types}; config is an object and event_types a list of words", add)
		}
		if p, err = CheckAgentFieldTriggerAdd(p); err != nil {
			return nil, err
		}
		ctx, cancel := factoryCall()
		defer cancel()
		out, err := a.FactoryAgentFieldTriggerAdd(ctx, p)
		if err != nil {
			return nil, err
		}
		return &out, nil
	})
	const addTrigger = "factory.agentfield_add_trigger"
	methods[addTrigger] = shaped[FactoryAgentFieldAddTriggerParams, FactoryAgentFieldAddedTrigger](func(e Engine, raw json.RawMessage) (*FactoryAgentFieldAddedTrigger, error) {
		a, ok := e.(FactoryAgentFieldTriggerAdder)
		if _, isFactory := e.(Factory); !ok || !isFactory {
			return nil, Errorf(CodeUnknownMethod, "this engine does not serve %s", addTrigger)
		}
		var p FactoryAgentFieldAddTriggerParams
		// THE CONFIG NEVER COMES BACK IN A REFUSAL: a decode's message quotes
		// the bad input, so this one says only what shape was wanted, as
		// factory.agentfield_connect does for its key.
		if err := decodeStrict(addTrigger, raw, &p); err != nil {
			return nil, Errorf(CodeBadParams, "%s takes {factory, source, event_types, config, secret_env_var}; config is an object and event_types a list of words", addTrigger)
		}
		checked, err := CheckAgentFieldTriggerAdd(FactoryAgentFieldTriggerAddParams{Factory: p.Factory, Source: p.Source, Config: p.Config, SecretEnvVar: p.SecretEnvVar, EventTypes: p.EventTypes})
		if err != nil {
			return nil, err
		}
		p.Factory, p.Source, p.SecretEnvVar, p.EventTypes = checked.Factory, checked.Source, checked.SecretEnvVar, checked.EventTypes
		ctx, cancel := factoryCall()
		defer cancel()
		out, err := a.FactoryAgentFieldAddTrigger(ctx, p)
		if err != nil {
			return nil, err
		}
		return &out, nil
	})
}

func agentFieldRef[R any](e Engine, name string, p FactoryRef, call func(FactoryAgentField, context.Context, string) (R, error)) (*R, error) {
	a, err := agentFieldEngine(e, name)
	if err != nil {
		return nil, err
	}
	id, err := factoryID(strings.TrimSpace(p.Factory))
	if err != nil {
		return nil, err
	}
	ctx, cancel := factoryCall()
	defer cancel()
	out, err := call(a, ctx, id)
	if err != nil {
		return nil, err
	}
	return &out, nil
}

func agentFieldEngine(e Engine, name string) (FactoryAgentField, error) {
	a, ok := e.(FactoryAgentField)
	if _, isFactory := e.(Factory); !ok || !isFactory {
		return nil, Errorf(CodeUnknownMethod, "this engine does not serve %s", name)
	}
	return a, nil
}
