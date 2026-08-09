package isolationfurrow

import (
	"errors"
	"math"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/Agent-Field/swe-pro-go/internal/jscompat"
)

type execCall struct {
	Args []string
	Cwd  string
}

func TestAdapterHappyPathsAndCommandConstruction(t *testing.T) {
	var calls []execCall
	runner := func(args []string, opts ExecOptions) (ExecResult, error) {
		copied := append([]string(nil), args...)
		calls = append(calls, execCall{Args: copied, Cwd: opts.Cwd})
		switch args[0] {
		case "fork":
			return ExecResult{
				Stdout: `{"plan":{"name":"a","destination":"/tmp/universe-a"},"result":{"name":"a","destination":"/tmp/universe-a","conflict_paths":[],"tier":"native-cow"}}`,
			}, nil
		case "merge":
			return ExecResult{Stdout: `{"preview_digest":"abc123"}`}, nil
		case "forks":
			return ExecResult{Stdout: `[
				{"fork_id":"f1","name":"a","destination":"/tmp/universe-a","conflict_paths":["src/a.ts"]},
				{"fork_id":"f2","name":"b","destination":"/tmp/universe-b","conflict_paths":["src/a.ts"]}
			]`}, nil
		default:
			return ExecResult{Stderr: "unknown command", ExitCode: 2}, nil
		}
	}

	isolation := CreateFurrowIsolation(CreateOptions{Binary: " furrow ", Exec: runner})
	if isolation == nil {
		t.Fatal("CreateFurrowIsolation = nil")
	}
	if got := isolation.Fork("/workspace", "a"); got != (ForkResult{Path: "/tmp/universe-a"}) {
		t.Fatalf("Fork = %+v", got)
	}
	merge := isolation.Merge("/workspace", "a", "bun test")
	if !merge.Merged || !strings.Contains(merge.Detail, "preview_digest") {
		t.Fatalf("Merge = %+v", merge)
	}
	wantForks := []Fork{
		{Name: "a", Path: "/tmp/universe-a", Conflicts: []string{"src/a.ts"}},
		{Name: "b", Path: "/tmp/universe-b", Conflicts: []string{"src/a.ts"}},
	}
	if got := isolation.Forks("/workspace"); !reflect.DeepEqual(got, wantForks) {
		t.Fatalf("Forks = %#v, want %#v", got, wantForks)
	}
	wantRadar := []RadarEntry{{File: "src/a.ts", Universes: []string{"a", "b"}}}
	if got := isolation.Radar("/workspace"); !reflect.DeepEqual(got, wantRadar) {
		t.Fatalf("Radar = %#v, want %#v", got, wantRadar)
	}
	wantCalls := []execCall{
		{Args: []string{"fork", "a", "--json"}, Cwd: "/workspace"},
		{Args: []string{"merge", "a", "--json", "--check", "bun test"}, Cwd: "/workspace"},
		{Args: []string{"forks", "--json"}, Cwd: "/workspace"},
		{Args: []string{"forks", "--json"}, Cwd: "/workspace"},
	}
	if !reflect.DeepEqual(calls, wantCalls) {
		t.Fatalf("calls = %#v, want %#v", calls, wantCalls)
	}
}

func TestForkOutputShapesAndNoisyJSON(t *testing.T) {
	tests := []struct {
		name   string
		stdout string
		want   string
	}{
		{"real nested result wins", `{"result":{"destination":" /result "},"destination":"/root","path":"/path","fork":{"path":"/nested"}}`, "/result"},
		{"root destination", `{"destination":" /root "}`, "/root"},
		{"root path", `{"path":" /path "}`, "/path"},
		{"nested fork path", `{"fork":{"path":" /nested "}}`, "/nested"},
		{"ANSI and surrounding logs", "before \x1b[31mred\x1b[0m\n{\"destination\":\"/logged\"}\nafter", "/logged"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			isolation := CreateFurrowIsolation(CreateOptions{
				Binary: "furrow",
				Exec: func([]string, ExecOptions) (ExecResult, error) {
					return ExecResult{Stdout: tc.stdout}, nil
				},
			})
			if got := isolation.Fork("/workspace", "x"); got.Path != tc.want || got.Error != "" {
				t.Fatalf("Fork = %+v, want path %q", got, tc.want)
			}
		})
	}
}

func TestStructuredFailuresAndExecRejection(t *testing.T) {
	t.Run("unparseable prefers stderr", func(t *testing.T) {
		isolation := CreateFurrowIsolation(CreateOptions{
			Binary: "furrow",
			Exec: func([]string, ExecOptions) (ExecResult, error) {
				return ExecResult{Stdout: "not json", Stderr: " diagnostic "}, nil
			},
		})
		got := isolation.Fork("/workspace", "bad")
		want := "furrow fork returned unparseable output (exit 0): diagnostic"
		if got.Error != want {
			t.Fatalf("error = %q, want %q", got.Error, want)
		}
	})

	t.Run("nonzero prefers stderr", func(t *testing.T) {
		isolation := CreateFurrowIsolation(CreateOptions{
			Binary: "furrow",
			Exec: func([]string, ExecOptions) (ExecResult, error) {
				return ExecResult{Stdout: "stdout detail", Stderr: " conflict ", ExitCode: 3}, nil
			},
		})
		got := isolation.Merge("/workspace", "bad")
		want := "furrow merge failed (exit 3): conflict"
		if got.Merged || got.Detail != want {
			t.Fatalf("Merge = %+v, want detail %q", got, want)
		}
	})

	t.Run("no output fallback and NaN exit", func(t *testing.T) {
		isolation := CreateFurrowIsolation(CreateOptions{
			Binary: "furrow",
			Exec: func([]string, ExecOptions) (ExecResult, error) {
				return ExecResult{ExitCode: math.NaN()}, nil
			},
		})
		got := isolation.Fork("/workspace", "bad")
		want := "furrow fork failed (exit NaN): no output"
		if got.Error != want {
			t.Fatalf("Fork error = %q, want %q", got.Error, want)
		}
	})

	t.Run("rejected exec becomes exit 127", func(t *testing.T) {
		isolation := CreateFurrowIsolation(CreateOptions{
			Binary: "furrow",
			Exec: func([]string, ExecOptions) (ExecResult, error) {
				return ExecResult{Stdout: "discard me"}, errors.New("runner exploded")
			},
		})
		got := isolation.Fork("/workspace", "bad")
		want := "furrow fork failed (exit 127): runner exploded"
		if got.Error != want {
			t.Fatalf("Fork error = %q, want %q", got.Error, want)
		}
	})

	t.Run("panicking exec becomes exit 127", func(t *testing.T) {
		isolation := CreateFurrowIsolation(CreateOptions{
			Binary: "furrow",
			Exec: func([]string, ExecOptions) (ExecResult, error) {
				panic("runner panicked")
			},
		})
		got := isolation.Fork("/workspace", "bad")
		want := "furrow fork failed (exit 127): runner panicked"
		if got.Error != want {
			t.Fatalf("Fork error = %q, want %q", got.Error, want)
		}
	})
}

func TestFailureDetailsUseJavaScriptCodeUnitSlices(t *testing.T) {
	prefixInput := strings.Repeat("😀", 2100)
	prefix := responseDetail("fork", ExecResult{Stderr: prefixInput, ExitCode: 2})
	wantPrefix := "furrow fork failed (exit 2): " + strings.Repeat("😀", 2000)
	if prefix != wantPrefix {
		t.Fatalf("prefix has %d bytes, want %d", len(prefix), len(wantPrefix))
	}

	suffixInput := "discard" + strings.Repeat("😀", 300)
	isolation := CreateFurrowIsolation(CreateOptions{
		Binary: "furrow",
		Exec: func([]string, ExecOptions) (ExecResult, error) {
			return ExecResult{Stdout: suffixInput}, nil
		},
	})
	got := isolation.Merge("/workspace", "x")
	wantSuffix := strings.Repeat("😀", 200)
	if !got.Merged || got.Detail != wantSuffix {
		t.Fatalf("Merge detail has %d bytes, want %d", len(got.Detail), len(wantSuffix))
	}
}

func TestMergeExplicitJSONAndZeroExitFallback(t *testing.T) {
	outputs := []string{
		`{"merged":true,"detail":" landed "}`,
		`{"merged":true,"detail":" "}`,
		`{"merged":false}`,
		"also not json",
		"",
	}
	wants := []MergeResult{
		{Merged: true, Detail: "landed"},
		{Merged: true, Detail: "merged"},
		{Merged: false, Detail: "not merged"},
		{Merged: true, Detail: "also not json"},
		{Merged: true, Detail: "merged"},
	}
	index := 0
	isolation := CreateFurrowIsolation(CreateOptions{
		Binary: "furrow",
		Exec: func(args []string, _ ExecOptions) (ExecResult, error) {
			if len(args) != 3 {
				t.Fatalf("merge without check args = %#v", args)
			}
			result := ExecResult{Stdout: outputs[index]}
			index++
			return result, nil
		},
	})
	for i, want := range wants {
		if got := isolation.Merge("/workspace", "x", ""); got != want {
			t.Errorf("case %d: Merge = %+v, want %+v", i, got, want)
		}
	}
}

func TestForksParsingIsAllOrNothing(t *testing.T) {
	outputs := []string{
		`{"forks":[{"name":" a ","path":" /a ","conflicts":[" x ","", " y "]}]}`,
		`{"universes":[{"name":"b","destination":"/b"}]}`,
		`[{"name":"a","path":"/a"},{"name":"","path":"/bad"}]`,
		`{"forks":"wrong","universes":[{"name":"not-used","path":"/x"}]}`,
	}
	wants := [][]Fork{
		{{Name: "a", Path: "/a", Conflicts: []string{"x", "y"}}},
		{{Name: "b", Path: "/b", Conflicts: []string{}}},
		{},
		{},
	}
	index := 0
	isolation := CreateFurrowIsolation(CreateOptions{
		Binary: "furrow",
		Exec: func([]string, ExecOptions) (ExecResult, error) {
			result := ExecResult{Stdout: outputs[index]}
			index++
			return result, nil
		},
	})
	for i, want := range wants {
		if got := isolation.Forks("/workspace"); !reflect.DeepEqual(got, want) {
			t.Errorf("case %d: Forks = %#v, want %#v", i, got, want)
		}
	}
}

func TestRadarDeduplicatesAndSorts(t *testing.T) {
	isolation := CreateFurrowIsolation(CreateOptions{
		Binary: "furrow",
		Exec: func([]string, ExecOptions) (ExecResult, error) {
			return ExecResult{Stdout: `[
				{"name":"z","path":"/z","conflicts":["src/z","src/a","src/a"]},
				{"name":"a","path":"/a","conflicts":["src/z"]},
				{"name":"z","path":"/z2","conflicts":["src/z"]}
			]`}, nil
		},
	})
	want := []RadarEntry{
		{File: "src/a", Universes: []string{"z"}},
		{File: "src/z", Universes: []string{"a", "z"}},
	}
	if got := isolation.Radar("/workspace"); !reflect.DeepEqual(got, want) {
		t.Fatalf("Radar = %#v, want %#v", got, want)
	}
}

func TestParseRadarSourceShape(t *testing.T) {
	value := parseOutput(`{"conflicts":[{"file":" x ","universes":[" a ","","b"]}]}`)
	got, ok := parseRadar(value)
	want := []RadarEntry{{File: "x", Universes: []string{"a", "b"}}}
	if !ok || !reflect.DeepEqual(got, want) {
		t.Fatalf("parseRadar = %#v, %v; want %#v, true", got, ok, want)
	}
}

func makeExecutable(t *testing.T, path string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
		t.Fatal(err)
	}
}

func TestResolveFurrowBinaryOrder(t *testing.T) {
	root := t.TempDir()
	repo := filepath.Join(root, "repo")
	cwd := filepath.Join(root, "cwd")
	pathDir := filepath.Join(root, "path")
	repoBinary := filepath.Join(repo, "vendor", "bin", "furrow")
	cwdBinary := filepath.Join(cwd, "vendor", "bin", "furrow")
	pathBinary := filepath.Join(pathDir, "furrow")
	makeExecutable(t, pathBinary)
	makeExecutable(t, cwdBinary)
	makeExecutable(t, repoBinary)

	if got := resolveFurrowBinaryAt(repo, cwd, pathDir, "linux"); got != repoBinary {
		t.Fatalf("with repo binary = %q, want %q", got, repoBinary)
	}
	if err := os.Chmod(repoBinary, 0o644); err != nil {
		t.Fatal(err)
	}
	if got := resolveFurrowBinaryAt(repo, cwd, pathDir, "linux"); got != cwdBinary {
		t.Fatalf("with cwd binary = %q, want %q", got, cwdBinary)
	}
	if err := os.Chmod(cwdBinary, 0o644); err != nil {
		t.Fatal(err)
	}
	if got := resolveFurrowBinaryAt(repo, cwd, ":"+pathDir, "linux"); got != pathBinary {
		t.Fatalf("with PATH binary = %q, want %q", got, pathBinary)
	}
	if err := os.Chmod(pathBinary, 0o644); err != nil {
		t.Fatal(err)
	}
	if got := resolveFurrowBinaryAt(repo, cwd, pathDir, "linux"); got != "" {
		t.Fatalf("without executable = %q, want empty", got)
	}
}

func TestResolveAndCreateFromCwdVendor(t *testing.T) {
	originalCwd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	cwd := t.TempDir()
	t.Cleanup(func() { _ = os.Chdir(originalCwd) })
	if err := os.Chdir(cwd); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", "")
	binary := filepath.Join(cwd, "vendor", "bin", "furrow")
	makeExecutable(t, binary)
	if got := ResolveFurrowBinary(); got != binary {
		t.Fatalf("ResolveFurrowBinary = %q, want %q", got, binary)
	}
	if got := CreateFurrowIsolation(CreateOptions{
		Exec: func([]string, ExecOptions) (ExecResult, error) { return ExecResult{}, nil },
	}); got == nil {
		t.Fatal("CreateFurrowIsolation with resolved cwd binary = nil")
	}
}

func TestDefaultExecShellsOutAndCapturesStreams(t *testing.T) {
	binary := filepath.Join(t.TempDir(), "furrow")
	script := `#!/bin/sh
if [ "$1" = "fork" ]; then
  printf '{"destination":"%s/%s"}' "$PWD" "$2"
  exit 0
fi
printf 'out'
printf 'conflict' >&2
exit 7
`
	if err := os.WriteFile(binary, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	workspace := t.TempDir()
	isolation := CreateFurrowIsolation(CreateOptions{Binary: binary})
	fork := isolation.Fork(workspace, "leaf")
	if fork.Path != filepath.Join(workspace, "leaf") || fork.Error != "" {
		t.Fatalf("Fork = %+v", fork)
	}
	merge := isolation.Merge(workspace, "leaf")
	want := "furrow merge failed (exit 7): conflict"
	if merge.Merged || merge.Detail != want {
		t.Fatalf("Merge = %+v, want detail %q", merge, want)
	}
}

func TestResultJSONShapes(t *testing.T) {
	cases := []struct {
		value any
		want  string
	}{
		{ForkResult{Path: "/x"}, `{"path":"/x"}`},
		{ForkResult{Error: "bad"}, `{"error":"bad"}`},
		{[]Fork{}, `[]`},
		{[]RadarEntry{}, `[]`},
	}
	for _, tc := range cases {
		got, err := jscompat.Stringify(tc.value)
		if err != nil {
			t.Fatal(err)
		}
		if string(got) != tc.want {
			t.Errorf("Stringify(%#v) = %s, want %s", tc.value, got, tc.want)
		}
	}
}
