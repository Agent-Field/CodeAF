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

var setupMethods = map[string]method{
	"providers.list": func(e Engine, _ json.RawMessage) (any, error) {
		providers, err := e.ListProviders()
		if err != nil {
			return nil, err
		}
		return struct {
			Providers []Provider `json:"providers"`
		}{nonNil(providers)}, nil
	},
	"keys.check": func(e Engine, raw json.RawMessage) (any, error) {
		provider, key, err := decodeKey(raw, "keys.check")
		if err != nil {
			return nil, err
		}
		return e.CheckKey(provider, key)
	},
	"keys.set": func(e Engine, raw json.RawMessage) (any, error) {
		provider, key, err := decodeKey(raw, "keys.set")
		if err != nil {
			return nil, err
		}
		return struct{}{}, e.SetKey(provider, key)
	},
	"keys.remove": func(e Engine, raw json.RawMessage) (any, error) {
		provider, err := decodeID(raw, "provider")
		if err != nil {
			return nil, err
		}
		return struct{}{}, e.RemoveKey(strings.ToLower(strings.TrimSpace(provider)))
	},
	"providers.sign_in": func(e Engine, raw json.RawMessage) (any, error) {
		provider, err := decodeID(raw, "provider")
		if err != nil {
			return nil, err
		}
		url, err := e.SignIn(strings.ToLower(strings.TrimSpace(provider)))
		if err != nil {
			return nil, err
		}
		return struct {
			URL string `json:"url"`
		}{url}, nil
	},
	"setup.get": func(e Engine, _ json.RawMessage) (any, error) {
		setup, err := e.Setup()
		if err != nil {
			return nil, err
		}
		return setupResult{setup}, nil
	},
	"setup.set": func(e Engine, raw json.RawMessage) (any, error) {
		var p Setup
		if err := decode(raw, &p); err != nil {
			return nil, err
		}
		if p.Step == (SetupStep{}) {
			return nil, Errorf(CodeBadParams, "setup.set needs a step")
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
		return setupResult{setup}, nil
	},
	"settings.set": func(e Engine, raw json.RawMessage) (any, error) {
		patch, err := decodeSettingsPatch(raw)
		if err != nil {
			return nil, err
		}
		settings, err := e.SetSettings(patch)
		if err != nil {
			return nil, err
		}
		return struct {
			Settings Settings `json:"settings"`
		}{settings}, nil
	},
	"caps.raise": func(e Engine, raw json.RawMessage) (any, error) {
		var p CapRaise
		if err := decode(raw, &p); err != nil {
			return nil, err
		}
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
		return struct{}{}, e.RaiseCap(p)
	},
}

type setupResult struct {
	Setup Setup `json:"setup"`
}

// money says a figure is a real positive amount of dollars.
func money(usd float64) bool {
	return usd > 0 && !math.IsNaN(usd) && !math.IsInf(usd, 0)
}

// decodeKey reads a provider and a key, both required. The key is never put
// into an error's words.
func decodeKey(raw json.RawMessage, name string) (string, string, error) {
	var p struct {
		Provider string `json:"provider"`
		Key      string `json:"key"`
	}
	if err := json.Unmarshal(raw, &p); err != nil {
		return "", "", Errorf(CodeBadParams, "%s needs a provider and a key, both strings", name)
	}
	provider, key := strings.ToLower(strings.TrimSpace(p.Provider)), strings.TrimSpace(p.Key)
	if provider == "" || key == "" {
		return "", "", Errorf(CodeBadParams, "%s needs a provider and a key", name)
	}
	if strings.ContainsAny(key, "\r\n\x00") {
		return "", "", Errorf(CodeBadParams, "a key is one line")
	}
	return provider, key, nil
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
		default:
			err = Errorf(CodeBadParams, "settings has no field %s", name)
		}
		if err != nil {
			return SettingsPatch{}, err
		}
	}
	return patch, nil
}
