package tool

import (
	"context"
	"os"
	"os/exec"
	"sort"
	"strings"
	"sync"

	"github.com/Agent-Field/aforge-v2/internal/swepro/internal/config"
	formatpkg "github.com/Agent-Field/aforge-v2/internal/swepro/internal/format"
)

type formatterServices struct {
	mu       sync.Mutex
	services map[string]*formatpkg.Service
}

func newFormatterServices() *formatterServices {
	return &formatterServices{services: map[string]*formatpkg.Service{}}
}

func (r *Registry) settings() (config.Info, error) {
	worktree := r.workDir
	if r.instance != nil && r.instance.Worktree != "" {
		worktree = r.instance.Worktree
	}
	return r.config.Get(r.workDir, worktree)
}

func (r *Registry) formatterService() (*formatpkg.Service, error) {
	worktree := r.workDir
	if r.instance != nil && r.instance.Worktree != "" {
		worktree = r.instance.Worktree
	}
	key := r.workDir + "\x00" + worktree
	r.formatters.mu.Lock()
	defer r.formatters.mu.Unlock()
	if service := r.formatters.services[key]; service != nil {
		return service, nil
	}
	settings, err := r.settings()
	if err != nil {
		return nil, err
	}
	configuration := formatterConfiguration(settings["formatter"])
	service := formatpkg.NewService(
		formatpkg.Context{Directory: r.workDir, Worktree: worktree},
		configuration,
		formatpkg.Dependencies{
			Which: func(command string) (string, bool) {
				match, err := exec.LookPath(command)
				return match, err == nil
			},
			NpmWhich: func(ctx context.Context, name string) (string, bool) {
				return r.npm.Which(ctx, name)
			},
			ExperimentalOxfmt: config.ParseBoolean(
				config.Truthy,
				environmentValue("CODEAF_EXPERIMENTAL_OXFMT"),
			),
		},
		nil,
	)
	r.formatters.services[key] = service
	return service, nil
}

func environmentValue(name string) *string {
	value, ok := os.LookupEnv(name)
	if !ok {
		return nil
	}
	return &value
}

func formatterConfiguration(value any) formatpkg.Configuration {
	configuration := formatpkg.Configuration{}
	switch value := value.(type) {
	case bool:
		configuration.Enabled = value
	case map[string]any:
		configuration.Enabled = true
		keys := make([]string, 0, len(value))
		for key := range value {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		for _, key := range keys {
			entry, _ := value[key].(map[string]any)
			override := formatpkg.FormatterOverride{Key: key}
			override.Disabled, _ = entry["disabled"].(bool)
			override.Extensions = stringSliceSetting(entry["extensions"])
			override.Command = stringSliceSetting(entry["command"])
			if environment, ok := entry["environment"].(map[string]any); ok {
				override.Environment = make(map[string]string, len(environment))
				for name, raw := range environment {
					if item, ok := raw.(string); ok {
						override.Environment[name] = item
					}
				}
			}
			configuration.Overrides = append(configuration.Overrides, override)
		}
	}
	return configuration
}

func stringSliceSetting(value any) *[]string {
	raw, ok := value.([]any)
	if !ok {
		return nil
	}
	out := make([]string, 0, len(raw))
	for _, value := range raw {
		if item, ok := value.(string); ok {
			out = append(out, item)
		}
	}
	return &out
}

func formatMutationFile(ctx context.Context, service *formatpkg.Service, path string, bom bool) (string, error) {
	formatted, err := service.File(ctx, path)
	if err != nil {
		return "", err
	}
	if !formatted {
		data, err := os.ReadFile(path)
		if err != nil {
			return "", err
		}
		_, content := splitBOM(strings.ToValidUTF8(string(data), "\uFFFD"))
		return content, nil
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	_, content := splitBOM(strings.ToValidUTF8(string(data), "\uFFFD"))
	if err := os.WriteFile(path, []byte(joinBOM(content, bom)), 0o644); err != nil {
		return "", err
	}
	return content, nil
}
