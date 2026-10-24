package appwire

import (
	"encoding/json"
	"math"
	"strings"
)

// The methods of setup, keys, settings and money (PROTOCOL.md, Wave 2). They
// join the one dispatch table, so they are refused, framed and answered by
// exactly the rules every other method is.
func init() {
	for name, m := range setupMethods {
		methods[name] = m
	}
}

type ProviderSignInCanceler interface{ CancelSignIn(string, string) error }

type ProviderSignInStart struct {
	URL     string `json:"url"`
	Attempt string `json:"attempt"`
}
type ProviderSignInStarter interface {
	StartSignIn(string) (ProviderSignInStart, error)
}
type providerCancelParams struct {
	Provider string `json:"provider"`
	Attempt  string `json:"attempt"`
}

var setupMethods = map[string]method{
	"providers.sign_in_start": typed(func(e Engine, p ProviderParams) (*ProviderSignInStart, error) {
		provider, err := required("provider", p.Provider)
		if err != nil {
			return nil, err
		}
		starter, ok := e.(ProviderSignInStarter)
		if !ok {
			return nil, Errorf(CodeUnknownMethod, "account sign-in is unavailable")
		}
		result, err := starter.StartSignIn(strings.ToLower(strings.TrimSpace(provider)))
		return &result, err
	}),
	"providers.sign_in_cancel": typed(func(e Engine, p providerCancelParams) (*Empty, error) {
		provider, err := required("provider", p.Provider)
		if err != nil {
			return nil, err
		}
		c, ok := e.(ProviderSignInCanceler)
		if !ok {
			return nil, Errorf(CodeUnknownMethod, "provider sign-in cancellation is unavailable")
		}
		attempt, err := required("attempt", p.Attempt)
		if err != nil {
			return nil, err
		}
		return done(c.CancelSignIn(strings.ToLower(strings.TrimSpace(provider)), attempt))
	}),
	"providers.list": typed(func(e Engine, _ struct{}) (*ProvidersResult, error) {
		providers, err := e.ListProviders()
		if err != nil {
			return nil, err
		}
		return &ProvidersResult{nonNil(providers)}, nil
	}),
	"keys.check": shaped[KeyParams](func(e Engine, raw json.RawMessage) (*KeyCheck, error) {
		connector, whole := e.(ProviderConnector)
		p, err := decodeKey(raw, "keys.check", whole)
		if err != nil {
			return nil, err
		}
		var check KeyCheck
		if whole {
			check, err = connector.CheckProvider(p)
		} else {
			check, err = e.CheckKey(p.Provider, p.Key)
		}
		if err != nil {
			return nil, err
		}
		return &check, nil
	}),
	"keys.set": shaped[KeyParams](func(e Engine, raw json.RawMessage) (*Empty, error) {
		connector, whole := e.(ProviderConnector)
		p, err := decodeKey(raw, "keys.set", whole)
		if err != nil {
			return nil, err
		}
		if whole {
			return done(connector.SetProvider(p))
		}
		return done(e.SetKey(p.Provider, p.Key))
	}),
	"keys.remove": typed(func(e Engine, p ProviderParams) (*Empty, error) {
		provider, err := required("provider", p.Provider)
		if err != nil {
			return nil, err
		}
		return done(e.RemoveKey(strings.ToLower(strings.TrimSpace(provider))))
	}),
	"providers.sign_in": typed(func(e Engine, p ProviderParams) (*URLResult, error) {
		provider, err := required("provider", p.Provider)
		if err != nil {
			return nil, err
		}
		url, err := e.SignIn(strings.ToLower(strings.TrimSpace(provider)))
		if err != nil {
			return nil, err
		}
		return &URLResult{url}, nil
	}),
	"setup.get": typed(func(e Engine, _ struct{}) (*setupResult, error) {
		setup, err := e.Setup()
		if err != nil {
			return nil, err
		}
		return &setupResult{setup}, nil
	}),
	"setup.set": typed(func(e Engine, p Setup) (*setupResult, error) {
		if p.Step == (SetupStep{}) {
			return nil, Errorf(CodeBadParams, "setup.set needs a step")
		}
		if !p.Step.Valid() {
			return nil, Errorf(CodeBadParams, "%s", setupStepWords)
		}
		if p.DayCapUSD != nil && !money(*p.DayCapUSD) {
			return nil, Errorf(CodeBadParams, "day_cap_usd must be a positive amount")
		}
		if p.Start != "" && p.Start != SetupAgents {
			return nil, Errorf(CodeBadParams, "start is %q or nothing", SetupAgents)
		}
		setup, err := e.SetSetup(p)
		if err != nil {
			return nil, err
		}
		return &setupResult{setup}, nil
	}),
	"settings.set": shaped[SettingsPatchWire](func(e Engine, raw json.RawMessage) (*SettingsResult, error) {
		patch, err := decodeSettingsPatch(raw)
		if err != nil {
			return nil, err
		}
		settings, err := e.SetSettings(patch)
		if err != nil {
			return nil, err
		}
		return &SettingsResult{settings}, nil
	}),
	"caps.raise": typed(func(e Engine, p CapRaise) (*Empty, error) {
		switch p.Scope {
		case CapDay:
		case CapAgent:
			if strings.TrimSpace(p.ID) == "" {
				return nil, Errorf(CodeBadParams, "raising an agent's cap needs its id")
			}
		default:
			return nil, Errorf(CodeBadParams, "scope is agent or day")
		}
		if p.For != RaiseToday && p.For != RaiseGood {
			return nil, Errorf(CodeBadParams, "for is today or good")
		}
		if !money(p.ToUSD) {
			return nil, Errorf(CodeBadParams, "to_usd must be a positive amount")
		}
		return done(e.RaiseCap(p))
	}),
}

// KeyParams is keys.check's and keys.set's params. The key is read by
// [decodeKey], which never puts it into an error's words.
//
// Region and Address are the person's answers for a provider that asks them,
// read by keys.check; Connection is what a passing keys.check answered, handed
// back to keys.set so the provider is kept as it was proved. The key may be
// empty for a provider that takes none ([ProviderConnector]).
type KeyParams struct {
	Provider   string      `json:"provider"`
	Key        string      `json:"key"`
	Region     string      `json:"region,omitempty"`
	Address    string      `json:"address,omitempty"`
	Connection *Connection `json:"connection,omitempty"`
}

// KeyGitHub is keys.set's provider for the GitHub token of the factory on
// this machine, which is no model provider. The app keeps a token pasted in
// factory setup in its keychain beside the model keys and hands it back with
// keys.set at every launch; codeaf holds it in memory for the factory to read
// GitHub with, and nothing else hears of it.
const KeyGitHub = "github"

// ProviderParams is the params of a method that takes one provider.
type ProviderParams struct {
	Provider string `json:"provider"`
}

// SettingsPatchWire is settings.set's params as they travel: every field is
// optional, and a field present is changed. A money field sent as null means
// none. [decodeSettingsPatch] reads exactly these fields and refuses any other.
type SettingsPatchWire struct {
	DefaultModel       *string  `json:"default_model,omitempty"`
	AsksBefore         *string  `json:"asks_before,omitempty"`
	WorkFolder         *string  `json:"work_folder,omitempty"`
	Effort             *string  `json:"effort,omitempty"`
	BudgetUSD          *float64 `json:"budget_usd,omitempty" nullable:"true"`
	DayCapUSD          *float64 `json:"day_cap_usd,omitempty" nullable:"true"`
	TaskCapUSD         *float64 `json:"task_cap_usd,omitempty" nullable:"true"`
	ProposedTaskClockS *int     `json:"proposed_task_clock_s,omitempty"`
	IdleCloseDays      *int     `json:"idle_close_days,omitempty"`
	Appearance         *string  `json:"appearance,omitempty" enum:"system,light,dark"`
	TextSize           *string  `json:"text_size,omitempty" enum:"small,medium,large"`
	OpenAtLogin        *bool    `json:"open_at_login,omitempty"`
	DockBadge          *bool    `json:"dock_badge,omitempty"`
	ChecksModel        *string  `json:"checks_model,omitempty"`
	PlannerModel       *string  `json:"planner_model,omitempty"`
	FallbackModel      *string  `json:"fallback_model,omitempty"`
}

type setupResult struct {
	Setup Setup `json:"setup"`
}

// money says a figure is a real positive amount of dollars.
func money(usd float64) bool {
	return usd > 0 && !math.IsNaN(usd) && !math.IsInf(usd, 0)
}

// decodeKey reads a provider and a key, and the answers and connection beside
// them. The provider is required, and so is the key unless keyless says the
// engine decides which providers take none. The key is never put into an
// error's words.
func decodeKey(raw json.RawMessage, name string, keyless bool) (KeyParams, error) {
	var p KeyParams
	if err := json.Unmarshal(raw, &p); err != nil {
		return KeyParams{}, Errorf(CodeBadParams, "%s needs a provider and a key, both strings", name)
	}
	p.Provider, p.Key = strings.ToLower(strings.TrimSpace(p.Provider)), strings.TrimSpace(p.Key)
	p.Region, p.Address = strings.TrimSpace(p.Region), strings.TrimSpace(p.Address)
	if p.Provider == "" || p.Key == "" && !keyless {
		return KeyParams{}, Errorf(CodeBadParams, "%s needs a provider and a key", name)
	}
	if strings.ContainsAny(p.Key, "\r\n\x00") {
		return KeyParams{}, Errorf(CodeBadParams, "a key is one line")
	}
	return p, nil
}

// decodeSettingsPatch reads settings.set's params: the fields present, each
// in its own shape, and nothing it does not know.
func decodeSettingsPatch(raw json.RawMessage) (SettingsPatch, error) {
	var fields map[string]json.RawMessage
	if err := decode(raw, &fields); err != nil {
		return SettingsPatch{}, err
	}
	var patch SettingsPatch
	text := func(name string, into **string) error {
		var s string
		if err := json.Unmarshal(fields[name], &s); err != nil {
			return Errorf(CodeBadParams, "%s must be a string", name)
		}
		s = strings.TrimSpace(s)
		*into = &s
		return nil
	}
	whole := func(name string, into **int) error {
		var n int
		if err := json.Unmarshal(fields[name], &n); err != nil || n < 0 {
			return Errorf(CodeBadParams, "%s must be a whole number, zero or more", name)
		}
		*into = &n
		return nil
	}
	flag := func(name string, into **bool) error {
		var b bool
		if err := json.Unmarshal(fields[name], &b); err != nil {
			return Errorf(CodeBadParams, "%s must be true or false", name)
		}
		*into = &b
		return nil
	}
	oneOf := func(name string, into **string, allowed ...string) error {
		if err := text(name, into); err != nil {
			return err
		}
		for _, a := range allowed {
			if **into == a {
				return nil
			}
		}
		return Errorf(CodeBadParams, "%s is %s", name, strings.Join(allowed, ", "))
	}
	figure := func(name string, into **Figure) error {
		if strings.TrimSpace(string(fields[name])) == "null" {
			*into = &Figure{}
			return nil
		}
		var usd float64
		if err := json.Unmarshal(fields[name], &usd); err != nil || !money(usd) {
			return Errorf(CodeBadParams, "%s must be a positive amount, or null for none", name)
		}
		*into = &Figure{USD: &usd}
		return nil
	}
	for name := range fields {
		var err error
		switch name {
		case "default_model":
			err = text(name, &patch.DefaultModel)
		case "asks_before":
			err = text(name, &patch.AsksBefore)
		case "work_folder":
			err = text(name, &patch.WorkFolder)
		case "effort":
			err = text(name, &patch.Effort)
		case "budget_usd":
			err = figure(name, &patch.BudgetUSD)
		case "day_cap_usd":
			err = figure(name, &patch.DayCapUSD)
		case "task_cap_usd":
			err = figure(name, &patch.TaskCapUSD)
		case "proposed_task_clock_s":
			err = whole(name, &patch.ProposedTaskClockS)
		case "idle_close_days":
			err = whole(name, &patch.IdleCloseDays)
		case "appearance":
			err = oneOf(name, &patch.Appearance, AppearanceSystem, AppearanceLight, AppearanceDark)
		case "text_size":
			err = oneOf(name, &patch.TextSize, TextSmall, TextMedium, TextLarge)
		case "open_at_login":
			err = flag(name, &patch.OpenAtLogin)
		case "dock_badge":
			err = flag(name, &patch.DockBadge)
		case "checks_model":
			err = text(name, &patch.ChecksModel)
		case "planner_model":
			err = text(name, &patch.PlannerModel)
		case "fallback_model":
			err = text(name, &patch.FallbackModel)
		default:
			err = Errorf(CodeBadParams, "settings has no field %s", name)
		}
		if err != nil {
			return SettingsPatch{}, err
		}
	}
	return patch, nil
}

// The answers of this file's methods.
type (
	ProvidersResult struct {
		Providers []Provider `json:"providers"`
	}
	URLResult struct {
		URL string `json:"url"`
	}
)
