//go:build !windows

// Formatter service
package format

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"sync"

	"github.com/Agent-Field/codeaf/internal/seniordev/util"
)

type Status struct {
	Name       string   `json:"name"`
	Extensions []string `json:"extensions"`
	Enabled    bool     `json:"enabled"`
}

type FormatterOverride struct {
	Key         string
	Disabled    bool
	Extensions  *[]string
	Command     *[]string
	Environment map[string]string
}

type Configuration struct {
	// Enabled=false (no formatter config) disables every formatter.
	Enabled   bool
	Overrides []FormatterOverride
}

type CommandRunner func(context.Context, []string, string, map[string]string) (int, error)

type Service struct {
	context Context
	deps    Dependencies
	runner  CommandRunner

	mu         sync.Mutex
	formatters orderedFormatters
	commands   map[string]cachedCommand
}

type cachedCommand struct {
	command []string
	enabled bool
	set     bool
}

type orderedFormatters struct {
	order []string
	items map[string]Info
}

func (o *orderedFormatters) set(key string, value Info) {
	if _, exists := o.items[key]; !exists {
		o.order = append(o.order, key)
	}
	o.items[key] = value
}

func (o *orderedFormatters) delete(key string) {
	if _, exists := o.items[key]; !exists {
		return
	}
	delete(o.items, key)
	for i, item := range o.order {
		if item == key {
			o.order = append(o.order[:i], o.order[i+1:]...)
			return
		}
	}
}

func (o *orderedFormatters) values() []Info {
	out := make([]Info, 0, len(o.order))
	for _, key := range o.order {
		out = append(out, o.items[key])
	}
	return out
}

func NewService(instance Context, configuration Configuration, dependencies Dependencies, runner CommandRunner) *Service {
	if runner == nil {
		runner = func(ctx context.Context, command []string, cwd string, environment map[string]string) (int, error) {
			if len(command) == 0 {
				return 1, errors.New("Command is required")
			}
			result, err := util.RunProcess(ctx, command, util.RunOptions{
				ProcessOptions: util.ProcessOptions{Cwd: cwd, Env: environment},
				NoThrow:        true,
			})
			return result.Code, err
		}
	}
	service := &Service{
		context:    instance,
		deps:       dependencies,
		runner:     runner,
		formatters: orderedFormatters{items: make(map[string]Info)},
		commands:   make(map[string]cachedCommand),
	}
	if !configuration.Enabled {
		return service
	}
	builtins := Builtins(dependencies)
	byExportKey := map[string]Info{}
	for _, item := range builtins {
		byExportKey[item.Key] = item
		service.formatters.set(item.Name, item)
	}

	linkedDisabled := false
	for _, override := range configuration.Overrides {
		if (override.Key == "ruff" || override.Key == "uv") && override.Disabled {
			linkedDisabled = true
		}
	}
	for _, override := range configuration.Overrides {
		name := override.Key
		if (name == "ruff" || name == "uv") && linkedDisabled {
			service.formatters.delete("ruff")
			service.formatters.delete("uv")
			continue
		}
		if override.Disabled {
			service.formatters.delete(name)
			continue
		}
		builtIn, exists := byExportKey[name]
		info := Info{Key: name, Name: name, Extensions: []string{}}
		if exists {
			info = builtIn
			info.Name = name
		}
		if override.Extensions != nil {
			info.Extensions = append([]string(nil), (*override.Extensions)...)
		}
		if override.Environment != nil {
			environment := map[string]string{}
			for key, value := range info.Environment {
				environment[key] = value
			}
			for key, value := range override.Environment {
				environment[key] = value
			}
			info.Environment = environment
		}
		if !exists || override.Command != nil {
			command := override.Command
			info.Enabled = func(_ context.Context, _ Context) ([]string, bool, error) {
				if command == nil {
					return nil, false, nil
				}
				return append([]string(nil), (*command)...), true, nil
			}
		}
		service.formatters.set(name, info)
	}
	return service
}

func (s *Service) getCommand(ctx context.Context, item Info) ([]string, bool, error) {
	s.mu.Lock()
	cached := s.commands[item.Name]
	if cached.set && cached.enabled {
		command := append([]string(nil), cached.command...)
		s.mu.Unlock()
		return command, true, nil
	}
	s.mu.Unlock()

	command, enabled, err := item.Enabled(ctx, s.context)
	if err != nil {
		return nil, false, err
	}
	s.mu.Lock()
	s.commands[item.Name] = cachedCommand{
		command: append([]string(nil), command...), enabled: enabled, set: true,
	}
	s.mu.Unlock()
	return command, enabled, nil
}

func (s *Service) Status(ctx context.Context) ([]Status, error) {
	out := []Status{}
	for _, formatter := range s.formatters.values() {
		_, enabled, err := s.getCommand(ctx, formatter)
		if err != nil {
			return nil, err
		}
		out = append(out, Status{
			Name: formatter.Name, Extensions: append([]string(nil), formatter.Extensions...), Enabled: enabled,
		})
	}
	return out, nil
}

func (s *Service) File(ctx context.Context, path string) (bool, error) {
	extension := fileExtension(path)
	type match struct {
		item    Info
		command []string
	}
	matches := []match{}
	for _, formatter := range s.formatters.values() {
		if !containsExtension(formatter.Extensions, extension) {
			continue
		}
		command, enabled, err := s.getCommand(ctx, formatter)
		if err != nil {
			return false, err
		}
		if enabled {
			matches = append(matches, match{item: formatter, command: command})
		}
	}
	if len(matches) == 0 {
		return false, nil
	}
	for _, match := range matches {
		replaced := make([]string, len(match.command))
		for i, argument := range match.command {
			replaced[i] = strings.Replace(argument, "$FILE", path, 1)
		}
		_, _ = s.runner(ctx, replaced, s.context.Directory, match.item.Environment)
	}
	return true, nil
}

func containsExtension(extensions []string, target string) bool {
	for _, extension := range extensions {
		if extension == target {
			return true
		}
	}
	return false
}

// fileExtension returns the final dotted suffix of path's base name; a
// leading dot alone (".bashrc") is not an extension.
func fileExtension(path string) string {
	base := filepath.Base(path)
	lastDot := strings.LastIndexByte(base, '.')
	if lastDot <= 0 {
		return ""
	}
	if base == ".." {
		return ""
	}
	return base[lastDot:]
}
