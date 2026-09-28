//go:build !windows

package format

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

type runCall struct {
	command     []string
	cwd         string
	environment map[string]string
}

func TestDisabledConfiguration(t *testing.T) {
	service := NewService(Context{}, Configuration{}, Dependencies{}, nil)
	status, err := service.Status(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(status) != 0 {
		t.Fatalf("status = %#v", status)
	}
	formatted, err := service.File(context.Background(), "file.go")
	if err != nil || formatted {
		t.Fatalf("File = %v, %v", formatted, err)
	}
}

func TestCustomFormatterRunsAndReplacesFirstPlaceholder(t *testing.T) {
	extensions := []string{".x"}
	command := []string{"fmt", "--file=$FILE", "$FILE-$FILE"}
	var calls []runCall
	service := NewService(
		Context{Directory: "/repo"},
		Configuration{Enabled: true, Overrides: []FormatterOverride{{
			Key: "custom", Extensions: &extensions, Command: &command,
			Environment: map[string]string{"MODE": "fix"},
		}}},
		Dependencies{},
		func(_ context.Context, command []string, cwd string, environment map[string]string) (int, error) {
			calls = append(calls, runCall{command, cwd, environment})
			return 0, nil
		},
	)
	formatted, err := service.File(context.Background(), "/repo/a.x")
	if err != nil {
		t.Fatal(err)
	}
	if !formatted {
		t.Fatal("custom formatter did not match")
	}
	want := []runCall{{
		command: []string{"fmt", "--file=/repo/a.x", "/repo/a.x-$FILE"},
		cwd:     "/repo", environment: map[string]string{"MODE": "fix"},
	}}
	if !reflect.DeepEqual(calls, want) {
		t.Fatalf("calls = %#v, want %#v", calls, want)
	}
}

func TestAllMatchingFormattersRunAndFailuresAreIgnored(t *testing.T) {
	extensions := []string{".x"}
	first := []string{"first", "$FILE"}
	second := []string{"second", "$FILE"}
	var commands [][]string
	service := NewService(
		Context{Directory: "/repo"},
		Configuration{Enabled: true, Overrides: []FormatterOverride{
			{Key: "first", Extensions: &extensions, Command: &first},
			{Key: "second", Extensions: &extensions, Command: &second},
		}},
		Dependencies{},
		func(_ context.Context, command []string, _ string, _ map[string]string) (int, error) {
			commands = append(commands, command)
			if command[0] == "first" {
				return 1, errors.New("spawn failed")
			}
			return 9, nil
		},
	)
	formatted, err := service.File(context.Background(), "/repo/a.x")
	if err != nil || !formatted {
		t.Fatalf("File = %v, %v", formatted, err)
	}
	want := [][]string{{"first", "/repo/a.x"}, {"second", "/repo/a.x"}}
	if !reflect.DeepEqual(commands, want) {
		t.Fatalf("commands = %#v, want %#v", commands, want)
	}
}

func TestEnabledCommandsCacheOnlySuccessfulProbe(t *testing.T) {
	available := false
	checks := 0
	dependencies := Dependencies{Which: func(name string) (string, bool) {
		if name != "gofmt" {
			return "", false
		}
		checks++
		return "/bin/gofmt", available
	}}
	service := NewService(Context{}, Configuration{Enabled: true}, dependencies, nil)
	for range 2 {
		if _, err := service.Status(context.Background()); err != nil {
			t.Fatal(err)
		}
	}
	if checks != 2 {
		t.Fatalf("false probe checks = %d, want 2", checks)
	}
	available = true
	for range 2 {
		if _, err := service.Status(context.Background()); err != nil {
			t.Fatal(err)
		}
	}
	if checks != 3 {
		t.Fatalf("enabled probe checks = %d, want 3", checks)
	}
}

func TestPrettierDiscoveryAndExecution(t *testing.T) {
	root := t.TempDir()
	directory := filepath.Join(root, "src")
	if err := os.Mkdir(directory, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(
		filepath.Join(root, "package.json"),
		[]byte(`{"devDependencies":{"prettier":"3.0.0"}}`),
		0o644,
	); err != nil {
		t.Fatal(err)
	}
	var calls []runCall
	service := NewService(
		Context{Directory: directory, Worktree: root},
		Configuration{Enabled: true},
		Dependencies{NpmWhich: func(_ context.Context, name string) (string, bool) {
			if name == "prettier" {
				return "/npm/prettier", true
			}
			return "", false
		}},
		func(_ context.Context, command []string, cwd string, environment map[string]string) (int, error) {
			calls = append(calls, runCall{command, cwd, environment})
			return 0, nil
		},
	)
	formatted, err := service.File(context.Background(), filepath.Join(directory, "a.ts"))
	if err != nil || !formatted {
		t.Fatalf("File = %v, %v", formatted, err)
	}
	want := []runCall{{
		command: []string{"/npm/prettier", "--write", filepath.Join(directory, "a.ts")},
		cwd:     directory, environment: map[string]string{"BUN_BE_BUN": "1"},
	}}
	if !reflect.DeepEqual(calls, want) {
		t.Fatalf("calls = %#v, want %#v", calls, want)
	}
}

func TestLinkedRuffAndUVDisable(t *testing.T) {
	service := NewService(
		Context{},
		Configuration{Enabled: true, Overrides: []FormatterOverride{{Key: "uv", Disabled: true}}},
		Dependencies{},
		nil,
	)
	status, err := service.Status(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	for _, item := range status {
		if item.Name == "ruff" || item.Name == "uv" {
			t.Fatalf("linked formatter survived: %#v", item)
		}
	}
}

// Every built-in registers under the same string a config override names, so
// disabling one by the name it is known by removes it. These three are the
// cases that used to carry a separate config key and silently survive.
func TestDisablingABuiltinByNameRemovesIt(t *testing.T) {
	service := NewService(
		Context{},
		Configuration{Enabled: true, Overrides: []FormatterOverride{
			{Key: "clang-format", Disabled: true},
			{Key: "air", Disabled: true},
			{Key: "uv", Disabled: true},
		}},
		Dependencies{},
		nil,
	)
	status, err := service.Status(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	for _, item := range status {
		switch item.Name {
		case "clang-format", "air", "uv":
			t.Errorf("%s survived being disabled", item.Name)
		}
	}
}

// Every built-in's key and name agree, so an override reaches the formatter
// it names instead of registering a second entry beside it.
func TestEveryBuiltinKeyMatchesItsName(t *testing.T) {
	for _, item := range Builtins(Dependencies{}) {
		if item.Key != item.Name {
			t.Errorf("builtin key %q does not match name %q", item.Key, item.Name)
		}
	}
}

func TestEmptyCommandStillCountsAsFormatter(t *testing.T) {
	extensions := []string{".x"}
	command := []string{}
	var calls int
	service := NewService(
		Context{},
		Configuration{Enabled: true, Overrides: []FormatterOverride{{
			Key: "empty", Extensions: &extensions, Command: &command,
		}}},
		Dependencies{},
		func(_ context.Context, command []string, _ string, _ map[string]string) (int, error) {
			calls++
			if len(command) != 0 {
				t.Fatalf("command = %#v", command)
			}
			return 1, errors.New("missing command")
		},
	)
	formatted, err := service.File(context.Background(), "a.x")
	if err != nil || !formatted || calls != 1 {
		t.Fatalf("File = %v, %v; calls=%d", formatted, err, calls)
	}
}

func TestFileExtension(t *testing.T) {
	cases := map[string]string{
		".bashrc":  "",
		"a.ts":     ".ts",
		"a.":       ".",
		"..":       "",
		"...":      ".",
		"..foo":    ".foo",
		".foo.bar": ".bar",
	}
	for path, want := range cases {
		if got := fileExtension(path); got != want {
			t.Errorf("fileExtension(%q) = %q, want %q", path, got, want)
		}
	}
}
