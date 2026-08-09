package instruction

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/Agent-Field/aforge-v2/internal/swepro/internal/engine/msgmodel"
	"github.com/Agent-Field/aforge-v2/internal/swepro/internal/jscompat"
)

type fixture struct {
	Name     string `json:"name"`
	ArgsJSON string `json:"args_json"`
	OutJSON  string `json:"out_json"`
}

func TestLoadedFixtures(t *testing.T) {
	file, err := os.Open("testdata/fixtures.json")
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		var value fixture
		_ = json.Unmarshal(scanner.Bytes(), &value)
		t.Run(value.Name, func(t *testing.T) {
			var args []json.RawMessage
			_ = json.Unmarshal([]byte(value.ArgsJSON), &args)
			var messages []msgmodel.WithParts
			if err := json.Unmarshal(args[0], &messages); err != nil {
				t.Fatal(err)
			}
			got, _ := jscompat.Stringify(Loaded(messages).Values())
			if string(got) != value.OutJSON {
				t.Errorf("got %s want %s", got, value.OutJSON)
			}
		})
	}
}

type staticHTTP map[string]string

func (client staticHTTP) Fetch(_ context.Context, url string) ([]byte, error) {
	value, ok := client[url]
	if !ok {
		return nil, errors.New("fetch")
	}
	return []byte(value), nil
}

func TestSystemPathsAndSystemOrdering(t *testing.T) {
	root := t.TempDir()
	configDir := filepath.Join(root, "config")
	home := filepath.Join(root, "home")
	worktree := filepath.Join(root, "repo")
	directory := filepath.Join(worktree, "src", "pkg")
	for _, dir := range []string{configDir, home, directory} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	write(t, filepath.Join(configDir, "AGENTS.md"), "global")
	write(t, filepath.Join(home, ".claude", "CLAUDE.md"), "home")
	write(t, filepath.Join(worktree, "AGENTS.md"), "project-root")
	write(t, filepath.Join(worktree, "src", "AGENTS.md"), "project-src")
	write(t, filepath.Join(directory, "CLAUDE.md"), "loses-to-agents")
	write(t, filepath.Join(worktree, "extra.md"), "extra")

	service := New(Options{
		Config: Config{Instructions: []string{
			"extra.md", "https://example.test/rules", "https://example.test/empty",
		}},
		Global:   Global{Config: configDir, Home: home},
		Instance: Instance{Directory: directory, Worktree: worktree},
		FS:       OSFileSystem{},
		HTTP: staticHTTP{
			"https://example.test/rules": "remote",
			"https://example.test/empty": "",
		},
	})
	paths := service.SystemPaths().Values()
	wantPaths := []string{
		filepath.Join(configDir, "AGENTS.md"),
		filepath.Join(worktree, "src", "AGENTS.md"),
		filepath.Join(worktree, "AGENTS.md"),
		filepath.Join(worktree, "extra.md"),
	}
	if !reflect.DeepEqual(paths, wantPaths) {
		t.Fatalf("paths=%v want=%v", paths, wantPaths)
	}
	got := service.System(context.Background())
	want := []string{
		"Instructions from: " + wantPaths[0] + "\nglobal",
		"Instructions from: " + wantPaths[1] + "\nproject-src",
		"Instructions from: " + wantPaths[2] + "\nproject-root",
		"Instructions from: " + wantPaths[3] + "\nextra",
		"Instructions from: https://example.test/rules\nremote",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("system=%v want=%v", got, want)
	}
}

func TestResolveClaimsBeforeReadAndPrefixEscapesRoot(t *testing.T) {
	root := t.TempDir()
	project := filepath.Join(root, "repo")
	outside := filepath.Join(root, "repo2", "pkg")
	if err := os.MkdirAll(outside, 0o755); err != nil {
		t.Fatal(err)
	}
	rules := filepath.Join(root, "repo2", "AGENTS.md")
	write(t, rules, "outside")
	fs := &failOnceFS{FileSystem: OSFileSystem{}, failPath: rules}
	service := New(Options{
		Instance: Instance{Directory: project, Worktree: project}, FS: fs,
		Flags: Flags{DisableClaudeCodePrompt: true},
	})
	target := filepath.Join(outside, "file.go")
	if got := service.Resolve(nil, target, "m1"); len(got) != 0 {
		t.Fatalf("first resolve=%#v", got)
	}
	if got := service.Resolve(nil, target, "m1"); len(got) != 0 {
		t.Fatalf("claimed failed read was retried: %#v", got)
	}
	service.Clear("m1")
	got := service.Resolve(nil, target, "m1")
	if len(got) != 1 || got[0].Filepath != rules {
		t.Fatalf("outside prefix resolve=%#v", got)
	}
}

type failOnceFS struct {
	FileSystem
	failPath string
	failed   bool
}

func (filesystem *failOnceFS) ReadFileString(path string) (string, error) {
	if path == filesystem.failPath && !filesystem.failed {
		filesystem.failed = true
		return "", errors.New("transient")
	}
	return filesystem.FileSystem.ReadFileString(path)
}

func write(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}
