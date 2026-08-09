package tool

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

// The fallback only earns its keep if it answers the way ripgrep does. These
// tests run the same invocation through both runners against one real tree and
// compare the answers, so a divergence shows up here rather than as a role
// quietly reading the wrong files. They skip when rg is absent — which is the
// very situation the fallback exists for.

func parityFixture(t *testing.T) string {
	t.Helper()
	workDir := t.TempDir()
	for _, args := range [][]string{
		{"init", "-q"},
		{"config", "user.email", "test@example.com"},
		{"config", "user.name", "test"},
	} {
		command := exec.Command("git", append([]string{"-C", workDir}, args...)...)
		if output, err := command.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v (%s)", args, err, output)
		}
	}
	for _, directory := range []string{"src", filepath.Join("src", "inner"), "build"} {
		if err := os.MkdirAll(filepath.Join(workDir, directory), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	files := map[string]string{
		".gitignore":                           "build/\n*.log\n",
		"top.go":                               "package top\n// needle at top\n",
		".hidden.go":                           "package hidden\n// needle hidden\n",
		"notes.txt":                            "needle in text\n",
		"debug.log":                            "needle in an ignored log\n",
		filepath.Join("src", "a.go"):           "package a\nfunc A() {} // needle\n",
		filepath.Join("src", "inner", "b.go"):  "package b\n// needle deeper\n",
		filepath.Join("build", "generated.go"): "package generated\n// needle generated\n",
	}
	for name, content := range files {
		if err := os.WriteFile(filepath.Join(workDir, name), []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	// rg does not follow symlinks by default. Including one keeps the parity
	// comparison honest about that; a platform without symlink support just
	// exercises one dimension less.
	if err := os.Symlink(
		filepath.Join(workDir, "top.go"), filepath.Join(workDir, "link.go"),
	); err != nil {
		t.Logf("symlink unsupported on this platform: %v", err)
	}
	command := exec.Command("git", "-C", workDir, "add", "-A")
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("git add: %v (%s)", err, output)
	}
	return workDir
}

func requireRipgrep(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("rg"); err != nil {
		t.Skip("rg not on PATH")
	}
}

// globPaths runs a --files invocation through a runner and returns the sorted
// path set, which is what glob.go consumes.
func globPaths(t *testing.T, runner ripgrepRunner, workDir string, args []string) []string {
	t.Helper()
	result, err := runner.Run(context.Background(), workDir, args)
	if err != nil {
		t.Fatalf("%T: %v", runner, err)
	}
	if result.code != 0 && result.code != 1 {
		t.Fatalf("%T: unexpected code %d (%s)", runner, result.code, result.stderr)
	}
	paths := []string{}
	for _, line := range strings.Split(string(result.stdout), "\n") {
		if line == "" {
			continue
		}
		paths = append(paths, cleanRipgrepPath(line))
	}
	sort.Strings(paths)
	return paths
}

// grepHits runs a --json invocation and returns sorted "path:line:text" keys.
func grepHits(t *testing.T, runner ripgrepRunner, workDir string, args []string) []string {
	t.Helper()
	result, err := runner.Run(context.Background(), workDir, args)
	if err != nil {
		t.Fatalf("%T: %v", runner, err)
	}
	if result.code != 0 && result.code != 1 && result.code != 2 {
		t.Fatalf("%T: unexpected code %d (%s)", runner, result.code, result.stderr)
	}
	hits := []string{}
	for _, line := range strings.Split(string(result.stdout), "\n") {
		if line == "" {
			continue
		}
		var event ripgrepJSONLine
		if err := json.Unmarshal([]byte(line), &event); err != nil {
			t.Fatalf("%T: invalid event %q", runner, line)
		}
		if event.Type != "match" {
			continue
		}
		hits = append(hits, cleanRipgrepPath(event.Data.Path.Text)+
			":"+itoa(event.Data.LineNumber)+
			":"+strings.TrimSuffix(event.Data.Lines.Text, "\n"))
	}
	sort.Strings(hits)
	return hits
}

func TestFallbackGlobParityWithRipgrep(t *testing.T) {
	requireRipgrep(t)
	workDir := parityFixture(t)

	for _, pattern := range []string{"*.go", "**/*.go", "src/*.go", "*.txt", "*.rs"} {
		t.Run(pattern, func(t *testing.T) {
			args := []string{
				"--no-config", "--files", "--glob=!.git/*", "--hidden", "--glob=" + pattern, ".",
			}
			want := globPaths(t, execRipgrepRunner{}, workDir, args)
			got := globPaths(t, builtinSearchRunner{}, workDir, args)
			if strings.Join(want, "|") != strings.Join(got, "|") {
				t.Fatalf("glob %q\n rg: %v\nfallback: %v", pattern, want, got)
			}
		})
	}
}

func TestFallbackGrepParityWithRipgrep(t *testing.T) {
	requireRipgrep(t)
	workDir := parityFixture(t)

	cases := []struct {
		name    string
		pattern string
		include string
	}{
		{name: "literal", pattern: "needle"},
		{name: "anchored", pattern: "^package"},
		{name: "charclass", pattern: "func [A-Z]"},
		{name: "include-go", pattern: "needle", include: "*.go"},
		{name: "no-match", pattern: "absolutely-not-present"},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			args := []string{"--no-config", "--json", "--hidden", "--glob=!.git/*", "--no-messages"}
			if testCase.include != "" {
				args = append(args, "--glob="+testCase.include)
			}
			args = append(args, "--", testCase.pattern, ".")
			want := grepHits(t, execRipgrepRunner{}, workDir, args)
			got := grepHits(t, builtinSearchRunner{}, workDir, args)
			if strings.Join(want, "|") != strings.Join(got, "|") {
				t.Fatalf("grep %q\n rg: %v\nfallback: %v", testCase.pattern, want, got)
			}
		})
	}
}
