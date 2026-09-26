//go:build !windows

package core

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"testing"
	"time"
)

func TestCoreHelperProcess(t *testing.T) {
	if os.Getenv("GO_CORE_HELPER") != "1" {
		return
	}
	separator := 0
	for i, arg := range os.Args {
		if arg == "--" {
			separator = i + 1
			break
		}
	}
	cwd, _ := os.Getwd()
	_ = json.NewEncoder(os.Stdout).Encode(map[string]any{
		"args": os.Args[separator:],
		"cwd":  cwd,
		"env":  os.Getenv("CORE_VALUE"),
	})
	os.Exit(0)
}

func TestSpawnerArgvEnvAndCwd(t *testing.T) {
	root := t.TempDir()
	spawner := NewSpawner()
	command := MakeCommand(os.Args[0], []string{
		"-test.run=TestCoreHelperProcess", "--", "space arg", "", "🙂",
	}, CommandOptions{
		Cwd: root,
		Env: []EnvVar{
			{Name: "GO_CORE_HELPER", Value: "1"},
			{Name: "CORE_VALUE", Value: "value"},
		},
	})
	stdout, stderr, code, err := spawner.Run(context.Background(), command)
	if err != nil || code != 0 {
		t.Fatalf("run code=%d err=%v stderr=%s", code, err, stderr)
	}
	var got struct {
		Args []string `json:"args"`
		Cwd  string   `json:"cwd"`
		Env  string   `json:"env"`
	}
	if err := json.Unmarshal(stdout, &got); err != nil {
		t.Fatalf("decode %q: %v", stdout, err)
	}
	if !reflect.DeepEqual(got.Args, []string{"space arg", "", "🙂"}) {
		t.Fatalf("args: %#v", got.Args)
	}
	// The child reports its folder as the kernel resolves it, and a temporary
	// folder on macOS is a symlink (/var/folders → /private/var/folders), so
	// the two are compared resolved: the same folder spelled two ways is the
	// same folder.
	if resolvedPath(t, got.Cwd) != resolvedPath(t, root) || got.Env != "value" {
		t.Fatalf("helper: %+v", got)
	}
}

func resolvedPath(t *testing.T, path string) string {
	t.Helper()
	resolved, err := filepath.EvalSymlinks(path)
	if err != nil {
		t.Fatalf("resolve %s: %v", path, err)
	}
	return resolved
}

func TestSpawnerPipeline(t *testing.T) {
	if _, err := exec.LookPath("printf"); err != nil {
		t.Skip("printf unavailable")
	}
	if _, err := exec.LookPath("tr"); err != nil {
		t.Skip("tr unavailable")
	}
	spawner := NewSpawner()
	handle, err := spawner.Spawn(context.Background(), Pipe(
		MakeCommand("printf", []string{"alpha\nbeta\n"}),
		MakeCommand("tr", []string{"a-z", "A-Z"}),
	))
	if err != nil {
		t.Fatal(err)
	}
	stdout, err := io.ReadAll(handle.Stdout)
	if err != nil {
		t.Fatal(err)
	}
	stderr, err := io.ReadAll(handle.Stderr)
	if err != nil {
		t.Fatal(err)
	}
	code, err := handle.Wait()
	if err != nil || code != 0 {
		t.Fatalf("wait code=%d err=%v stderr=%s", code, err, stderr)
	}
	if string(stdout) != "ALPHA\nBETA\n" {
		t.Fatalf("stdout: %q", stdout)
	}
}

func TestSpawnerMissingCommandIsTagged(t *testing.T) {
	_, err := NewSpawner().Spawn(context.Background(), MakeCommand("definitely-no-senior-dev-command", nil))
	var system *SystemError
	if !errors.As(err, &system) {
		t.Fatalf("error = %v", err)
	}
	if system.Tag != "NotFound" || system.Method != "spawn" {
		t.Fatalf("system error: %+v", system)
	}
}

func TestSpawnerContextCancellationKillsProcess(t *testing.T) {
	if _, err := exec.LookPath("sh"); err != nil {
		t.Skip("sh unavailable")
	}
	ctx, cancel := context.WithCancel(context.Background())
	handle, err := NewSpawner().Spawn(ctx, MakeCommand("sh", []string{"-c", "sleep 30"}))
	if err != nil {
		t.Fatal(err)
	}
	cancel()
	done := make(chan struct{})
	go func() {
		_, _ = handle.Wait()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("process did not stop")
	}
	if handle.IsRunning() {
		t.Fatal("handle still running")
	}
}

func TestBuildSpawnSpecEnvironmentModes(t *testing.T) {
	no := false
	spec, err := BuildSpawnSpec(MakeCommand("x", nil, CommandOptions{
		ExtendEnv: &no,
		EnvSet:    true,
		Env:       []EnvVar{{Name: "A", Value: "1"}, {Name: "B", Value: "2"}},
	}))
	if err != nil {
		t.Fatal(err)
	}
	if !spec.EnvSet || !reflect.DeepEqual(spec.Env, []string{"A=1", "B=2"}) {
		t.Fatalf("env: %#v", spec)
	}
	spec, err = BuildSpawnSpec(MakeCommand("echo", []string{"$HOME"}, CommandOptions{Shell: "true"}))
	if err != nil {
		t.Fatal(err)
	}
	if filepath.Base(spec.Path) != "sh" || !reflect.DeepEqual(spec.Args, []string{"-c", "echo $HOME"}) {
		t.Fatalf("shell spec: %#v", spec)
	}
}

func TestSortedFDs(t *testing.T) {
	got := SortedFDs(map[int]FDConfig{9: {}, 2: {}, 3: {}, 5: {}})
	if !reflect.DeepEqual(got, []int{3, 5, 9}) {
		t.Fatalf("fds: %v", got)
	}
}
