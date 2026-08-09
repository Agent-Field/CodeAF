package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"strings"

	"github.com/Agent-Field/swe-pro-go/internal/baked"
	configpkg "github.com/Agent-Field/swe-pro-go/internal/config"
	"github.com/Agent-Field/swe-pro-go/internal/engine/orclient"
	"github.com/Agent-Field/swe-pro-go/internal/engine/steploop"
	"github.com/Agent-Field/swe-pro-go/internal/permission"
	"github.com/Agent-Field/swe-pro-go/internal/session/overflow"
	"github.com/Agent-Field/swe-pro-go/internal/tool"
)

type codeafConfig struct {
	info       configpkg.Info
	service    *configpkg.Service
	global     permission.Ruleset
	agentRules map[string]permission.Ruleset
	bakedRules map[string]permission.Ruleset
}

func loadCodeafConfig(workspace string) (*codeafConfig, error) {
	env := configpkg.NewEnv(os.LookupEnv)
	globalDir, _ := env.Get("CODEAF_CONFIG_DIR")
	service := configpkg.NewService(configpkg.Loader{GlobalDir: globalDir, Env: env})
	info, err := service.Get(workspace, workspace)
	if err != nil {
		return nil, err
	}
	result, err := newCodeafConfig(info)
	if result != nil {
		result.service = service
	}
	return result, err
}

func newCodeafConfig(info configpkg.Info) (*codeafConfig, error) {
	result := &codeafConfig{
		info: info, agentRules: map[string]permission.Ruleset{},
		bakedRules: map[string]permission.Ruleset{},
	}
	var err error
	result.global, err = configPermissionRules(info["permission"])
	if err != nil {
		return nil, fmt.Errorf("permission config: %w", err)
	}
	for _, name := range baked.ListBakedAgents() {
		markdown, _ := baked.GetBakedAgentMarkdown(name)
		rules, parseErr := permission.RulesetFromFrontmatter(markdown)
		if parseErr != nil {
			if containsString(baked.LoadBearingAgents(), name) {
				return nil, fmt.Errorf("baked agent %q permissions: %w", name, parseErr)
			}
			continue
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

func (cfg *codeafConfig) rulesForAgent(name string) permission.Ruleset {
	if cfg == nil {
		return nil
	}
	return permission.Merge(cfg.bakedRules[name], cfg.global, cfg.agentRules[name])
}

func (cfg *codeafConfig) registryOptions() tool.RegistryOptions {
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

func (cfg *codeafConfig) instructions() []string {
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

func (cfg *codeafConfig) agent(name string) map[string]any {
	if cfg == nil {
		return nil
	}
	return objectValue(objectValue(cfg.info["agent"])[name])
}

func (cfg *codeafConfig) overflowConfig() (overflow.Config, error) {
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

func (cfg *codeafConfig) configureTurn(value turn) (turn, error) {
	agent := cfg.agent(value.Agent)
	if disabled, _ := agent["disable"].(bool); disabled {
		return value, fmt.Errorf("agent %q is disabled by config", value.Agent)
	}
	if prompt, ok := agent["prompt"].(string); ok {
		value.AgentMarkdown = prompt
		value.AgentPromptVerbatim = true
	}
	if model, ok := agent["model"].(string); ok && model != "" {
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

func (cfg *codeafConfig) disabledTools(agent string, ids []string) map[string]bool {
	out := map[string]bool{}
	if cfg == nil {
		return out
	}
	for _, name := range permission.Disabled(ids, cfg.rulesForAgent(agent)).Values() {
		out[name] = true
	}
	return out
}

func (cfg *codeafConfig) options(agent, providerID, modelID string) *orclient.Object {
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

func (cfg *codeafConfig) provider(providerID string) map[string]any {
	if cfg == nil {
		return nil
	}
	return objectValue(objectValue(cfg.info["provider"])[providerID])
}

func (cfg *codeafConfig) model(providerID, modelID string) map[string]any {
	provider := cfg.provider(providerID)
	return objectValue(objectValue(provider["models"])[modelID])
}

func (cfg *codeafConfig) headers(providerID, modelID string) []orclient.HeaderPair {
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

func (cfg *codeafConfig) applyBackend(backend *openRouterBackend) {
	if cfg == nil || backend == nil {
		return
	}
	backend.config = cfg
	options := objectValue(cfg.provider("openrouter")["options"])
	if value, ok := options["apiKey"].(string); ok && value != "" {
		backend.apiKey = value
	}
	if value, ok := options["baseURL"].(string); ok && value != "" {
		backend.endpoint = openRouterEndpoint(value)
	}
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
	case json.Number:
		number, err := value.Float64()
		return number, err == nil
	default:
		return 0, false
	}
}

func containsString(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}

func splitConfiguredModel(value string) (string, string) {
	providerID, modelID, found := strings.Cut(value, "/")
	if !found {
		return "openrouter", value
	}
	return providerID, modelID
}
