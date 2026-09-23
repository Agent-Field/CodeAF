//go:build !windows

package app

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"strings"

	"github.com/Agent-Field/codeaf/internal/seniordev/baked"
	configpkg "github.com/Agent-Field/codeaf/internal/seniordev/config"
	"github.com/Agent-Field/codeaf/internal/seniordev/engine/orclient"
	"github.com/Agent-Field/codeaf/internal/seniordev/engine/steploop"
	"github.com/Agent-Field/codeaf/internal/seniordev/permission"
	"github.com/Agent-Field/codeaf/internal/seniordev/session/overflow"
	"github.com/Agent-Field/codeaf/internal/seniordev/tool"
)

type seniorDevConfig struct {
	info       configpkg.Info
	service    *configpkg.Service
	global     permission.Ruleset
	agentRules map[string]permission.Ruleset
	bakedRules map[string]permission.Ruleset
	// variant is the run-level reasoning effort from --variant, applied to a
	// turn that names none so the configured event records what is sent.
	variant string
}

func loadSeniorDevConfig(workspace string) (*seniorDevConfig, error) {
	env := configpkg.NewEnv(os.LookupEnv)
	globalDir, _ := env.Get("SENIOR_DEV_CONFIG_DIR")
	service := configpkg.NewService(configpkg.Loader{GlobalDir: globalDir, Env: env})
	info, err := service.Get(workspace, workspace)
	if err != nil {
		return nil, err
	}
	result, err := newSeniorDevConfig(info)
	if result != nil {
		result.service = service
	}
	return result, err
}

func newSeniorDevConfig(info configpkg.Info) (*seniorDevConfig, error) {
	result := &seniorDevConfig{
		info: info, agentRules: map[string]permission.Ruleset{},
		bakedRules: map[string]permission.Ruleset{},
	}
	var err error
	result.global, err = configPermissionRules(info["permission"])
	if err != nil {
		return nil, fmt.Errorf("permission config: %w", err)
	}
	if err := refuseRetiredModelKnobs(info); err != nil {
		return nil, err
	}
	// The compaction block is parsed once here so a malformed block fails the
	// load instead of the first turn.
	if ovf, err := result.overflowConfig(); err != nil {
		return nil, fmt.Errorf("compaction config: %w", err)
	} else if err := overflow.ValidatePolicy(ovf); err != nil {
		return nil, fmt.Errorf("compaction config: %w", err)
	}
	for _, name := range baked.ListBakedAgents() {
		markdown, _ := baked.GetBakedAgentMarkdown(name)
		rules, parseErr := permission.RulesetFromFrontmatter(markdown)
		if parseErr != nil {
			return nil, fmt.Errorf("baked agent %q permissions: %w", name, parseErr)
		}
		result.bakedRules[name] = rules
	}
	for name, raw := range objectValue(info["agent"]) {
		agent := objectValue(raw)
		rules := toolPermissionRules(agent["tools"])
		configured, parseErr := configPermissionRules(agent["permission"])
		if parseErr != nil {
			return nil, fmt.Errorf("agent %q permission config: %w", name, parseErr)
		}
		result.agentRules[name] = permission.Merge(rules, configured)
	}
	return result, nil
}

func configPermissionRules(value any) (permission.Ruleset, error) {
	if value == nil {
		return nil, nil
	}
	switch value.(type) {
	case *configpkg.OrderedObject, string:
	default:
		return nil, fmt.Errorf("permission object did not preserve source order")
	}
	data, err := json.Marshal(value)
	if err != nil {
		return nil, err
	}
	parsed, err := permission.ParseConfigJSON(data)
	if err != nil {
		return nil, err
	}
	return permission.FromConfig(parsed), nil
}

func toolPermissionRules(settings any) permission.Ruleset {
	entries := []configpkg.OrderedEntry{}
	if ordered, ok := settings.(*configpkg.OrderedObject); ok {
		entries = ordered.Entries()
	} else {
		mapping := objectValue(settings)
		keys := make([]string, 0, len(mapping))
		for name := range mapping {
			keys = append(keys, name)
		}
		sort.Strings(keys)
		for _, name := range keys {
			entries = append(entries, configpkg.OrderedEntry{Key: name, Value: mapping[name]})
		}
	}
	rules := make(permission.Ruleset, 0, len(entries))
	for _, entry := range entries {
		name := entry.Key
		enabled, ok := entry.Value.(bool)
		if !ok {
			continue
		}
		permissionName := name
		if name == "write" || name == "edit" || name == "patch" || name == "apply_patch" {
			permissionName = "edit"
		}
		action := permission.ActionDeny
		if enabled {
			action = permission.ActionAllow
		}
		rules = append(rules, permission.Rule{
			Permission: permissionName, Pattern: "*", Action: action,
		})
	}
	return rules
}

func (cfg *seniorDevConfig) rulesForAgent(name string) permission.Ruleset {
	if cfg == nil {
		return nil
	}
	return permission.Merge(cfg.bakedRules[name], cfg.global, cfg.agentRules[name])
}

func (cfg *seniorDevConfig) registryOptions() tool.RegistryOptions {
	if cfg == nil {
		return tool.RegistryOptions{}
	}
	return tool.RegistryOptions{
		Instructions:             cfg.instructions(),
		Config:                   cfg.service,
		AllowExternalDirectories: true,
		PermissionRules: func(_ context.Context, call steploop.ToolCall) permission.Ruleset {
			return cfg.rulesForAgent(call.Agent)
		},
	}
}

func (cfg *seniorDevConfig) instructions() []string {
	if cfg == nil {
		return nil
	}
	values, _ := cfg.info["instructions"].([]any)
	out := make([]string, 0, len(values))
	for _, value := range values {
		if text, ok := value.(string); ok {
			out = append(out, text)
		}
	}
	return out
}

func (cfg *seniorDevConfig) agent(name string) map[string]any {
	if cfg == nil {
		return nil
	}
	return objectValue(objectValue(cfg.info["agent"])[name])
}

func (cfg *seniorDevConfig) overflowConfig() (overflow.Config, error) {
	if cfg == nil {
		return overflow.Config{}, nil
	}
	raw, err := json.Marshal(map[string]any{
		"compaction": cfg.info["compaction"],
	})
	if err != nil {
		return overflow.Config{}, err
	}
	var result overflow.Config
	if err := json.Unmarshal(raw, &result); err != nil {
		return overflow.Config{}, err
	}
	return result, nil
}

func (cfg *seniorDevConfig) configureTurn(value turn) (turn, error) {
	// Baked frontmatter is an executable agent contract, not model-visible
	// decoration. Apply its deterministic controls first. An explicitly chosen
	// pool model remains authoritative; a baked model is only a default when the
	// caller supplied no model. Project config below has final precedence.
	//
	// A baked agent's temperature is deliberately not applied: no generation
	// parameter is sent, so the provider's own default applies.
	value = applyBakedTurnControls(value)
	if cfg != nil && value.Variant == "" {
		value.Variant = cfg.variant
	}
	return applyConfiguredTurnControls(value, cfg.agent(value.Agent))
}

// applyBakedTurnControls applies the deterministic controls in a baked
// agent's frontmatter. The other frontmatter control, `tier:`, is read by
// baked.TierFor at call time: it selects the router pool, not a turn field.
func applyBakedTurnControls(value turn) turn {
	metadata, ok := baked.GetBakedAgentMetadata(value.Agent)
	if !ok {
		return value
	}
	if value.ProviderID == "" && value.ModelID == "" {
		if model, ok := metadata["model"].(string); ok && model != "" && model != "inherit" {
			value.ProviderID, value.ModelID = splitConfiguredModel(model)
		}
	}
	if steps, ok := configNumber(metadata["steps"]); ok && steps > 0 {
		value.MaxSteps = &steps
	} else if steps, ok := configNumber(metadata["maxSteps"]); ok && steps > 0 {
		value.MaxSteps = &steps
	}
	return value
}

func applyConfiguredTurnControls(value turn, agent map[string]any) (turn, error) {
	if disabled, _ := agent["disable"].(bool); disabled {
		return value, fmt.Errorf("agent %q is disabled by config", value.Agent)
	}
	if prompt, ok := agent["prompt"].(string); ok {
		value.AgentMarkdown = prompt
		value.AgentPromptVerbatim = true
	}
	if model, ok := agent["model"].(string); ok && model != "" && model != "inherit" {
		value.ProviderID, value.ModelID = splitConfiguredModel(model)
	}
	if variant, ok := agent["variant"].(string); ok {
		value.Variant = variant
	}
	if steps, ok := configNumber(agent["steps"]); ok && steps > 0 {
		value.MaxSteps = &steps
	} else if steps, ok := configNumber(agent["maxSteps"]); ok && steps > 0 {
		value.MaxSteps = &steps
	}
	return value, nil
}

func (cfg *seniorDevConfig) disabledTools(agent string, ids []string) map[string]bool {
	out := map[string]bool{}
	if cfg == nil {
		return out
	}
	for _, name := range permission.Disabled(ids, cfg.rulesForAgent(agent)).Values() {
		out[name] = true
	}
	return out
}

func (cfg *seniorDevConfig) options(agent, providerID, modelID string) *orclient.Object {
	result := orclient.NewObject()
	if cfg == nil {
		return result
	}
	model := cfg.model(providerID, modelID)
	for _, source := range []map[string]any{objectValue(model["options"]), objectValue(cfg.agent(agent)["options"])} {
		data, err := json.Marshal(source)
		if err != nil {
			continue
		}
		parsed, err := orclient.ParseObject(data)
		if err == nil {
			result = orclient.MergeOptions(result, parsed)
		}
	}
	return result
}

// refuseRetiredModelKnobs refuses, by name, the three config keys that used
// to decide how senior-dev reached a model and no longer can: a service's
// `apiKey` and `baseURL`, and any `providerRouting` block.
//
// A REMOVED KNOB FAILS LOUDLY, which is senior-dev's rule for every knob it
// retires. It reaches a model only through the model API codeaf serves the
// run, which holds the key, the address and the routing itself; a config that
// still set them and was quietly ignored would label a run with a behaviour it
// did not have, and an apiKey or baseURL honoured would be a second road to a
// model that codeaf could not meter, cap or show.
func refuseRetiredModelKnobs(info configpkg.Info) error {
	for providerID, rawProvider := range objectValue(info["provider"]) {
		provider := objectValue(rawProvider)
		options := objectValue(provider["options"])
		for _, key := range []string{"apiKey", "baseURL"} {
			if _, set := options[key]; set && providerID == orclient.Service {
				return fmt.Errorf("provider %q options.%s is not read: senior-dev reaches a model only through the model API codeaf serves it — remove the key", providerID, key)
			}
		}
		if provider["providerRouting"] != nil {
			return fmt.Errorf("provider %q providerRouting is not read: codeaf's model funnel decides which upstream serves a call — remove the block", providerID)
		}
		for modelID, rawModel := range objectValue(provider["models"]) {
			if objectValue(rawModel)["providerRouting"] != nil {
				return fmt.Errorf("provider %q model %q providerRouting is not read: codeaf's model funnel decides which upstream serves a call — remove the block", providerID, modelID)
			}
		}
	}
	for name, raw := range objectValue(info["agent"]) {
		if objectValue(raw)["providerRouting"] != nil {
			return fmt.Errorf("agent %q providerRouting is not read: codeaf's model funnel decides which upstream serves a call — remove the block", name)
		}
	}
	return nil
}

func (cfg *seniorDevConfig) provider(providerID string) map[string]any {
	if cfg == nil {
		return nil
	}
	return objectValue(objectValue(cfg.info["provider"])[providerID])
}

func (cfg *seniorDevConfig) model(providerID, modelID string) map[string]any {
	provider := cfg.provider(providerID)
	return objectValue(objectValue(provider["models"])[modelID])
}

func (cfg *seniorDevConfig) headers(providerID, modelID string) []orclient.HeaderPair {
	if cfg == nil {
		return nil
	}
	values := map[string]string{}
	for _, source := range []map[string]any{
		objectValue(cfg.provider(providerID)["options"]),
		cfg.model(providerID, modelID),
	} {
		for name, raw := range objectValue(source["headers"]) {
			if value, ok := raw.(string); ok {
				values[name] = value
			}
		}
	}
	names := make([]string, 0, len(values))
	for name := range values {
		names = append(names, name)
	}
	sort.Strings(names)
	result := make([]orclient.HeaderPair, 0, len(names))
	for _, name := range names {
		result = append(result, orclient.HeaderPair{Name: name, Value: values[name]})
	}
	return result
}

func (cfg *seniorDevConfig) applyBackend(backend *modelAPIBackend) {
	if cfg == nil || backend == nil {
		return
	}
	backend.config = cfg
	options := objectValue(cfg.provider(orclient.Service)["options"])
	if value, exists := options["timeout"]; exists {
		if disabled, ok := value.(bool); ok && !disabled {
			backend.totalTimeoutMS = -1
		} else if number, ok := configNumber(value); ok {
			if number == 0 {
				backend.totalTimeoutMS = -1
			} else if number > 0 {
				backend.totalTimeoutMS = number
			}
		}
	}
	if value, exists := options["chunkTimeout"]; exists {
		if disabled, ok := value.(bool); ok && !disabled {
			backend.chunkTimeoutMS = -1
		} else if number, ok := configNumber(value); ok {
			if number == 0 {
				backend.chunkTimeoutMS = -1
			} else if number > 0 {
				backend.chunkTimeoutMS = number
			}
		}
	}
}

func objectValue(value any) map[string]any {
	object, _ := value.(map[string]any)
	return object
}

func configNumber(value any) (float64, bool) {
	switch value := value.(type) {
	case float64:
		return value, true
	case float32:
		return float64(value), true
	case int:
		return float64(value), true
	case int64:
		return float64(value), true
	case uint64:
		return float64(value), true
	case json.Number:
		number, err := value.Float64()
		return number, err == nil
	default:
		return 0, false
	}
}

func splitConfiguredModel(value string) (string, string) {
	providerID, modelID, found := strings.Cut(value, "/")
	if !found {
		return orclient.Service, value
	}
	return providerID, modelID
}
