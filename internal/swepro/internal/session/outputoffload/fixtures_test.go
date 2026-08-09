package outputoffload

import (
	"bufio"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/Agent-Field/swe-pro-go/internal/jscompat"
)

type fixture struct {
	Name     string `json:"name"`
	Fn       string `json:"fn"`
	ArgsJSON string `json:"args_json"`
	OutJSON  string `json:"out_json"`
}

type fixtureOptions struct {
	FullOutputPath   *string  `json:"fullOutputPath"`
	Path             *string  `json:"path"`
	MaxChars         *float64 `json:"maxChars"`
	Mode             string   `json:"mode"`
	EscalationWanted bool     `json:"escalationWanted"`
}

type recordingSink struct {
	fail   bool
	path   string
	output string
	writes int
}

func (s *recordingSink) WriteOutput(path, output string) (string, error) {
	s.writes++
	s.path, s.output = path, output
	if s.fail {
		return "", errors.New("fixture failure")
	}
	return path, nil
}

func loadFixtures(t *testing.T) []fixture {
	t.Helper()
	f, err := os.Open("testdata/fixtures.json")
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	out := []fixture{}
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 1<<20), 1<<24)
	for sc.Scan() {
		var fx fixture
		if err := json.Unmarshal(sc.Bytes(), &fx); err != nil {
			t.Fatal(err)
		}
		out = append(out, fx)
	}
	if err := sc.Err(); err != nil {
		t.Fatal(err)
	}
	return out
}

func callFixture(t *testing.T, fx fixture) any {
	t.Helper()
	var raw []json.RawMessage
	if err := json.Unmarshal([]byte(fx.ArgsJSON), &raw); err != nil {
		t.Fatal(err)
	}
	switch fx.Fn {
	case "extractRelevant":
		var output string
		var opts *fixtureOptions
		if err := json.Unmarshal(raw[0], &output); err != nil {
			t.Fatal(err)
		}
		if err := json.Unmarshal(raw[1], &opts); err != nil {
			t.Fatal(err)
		}
		var real *ExtractRelevantOptions
		if opts != nil {
			real = &ExtractRelevantOptions{
				FullOutputPath: opts.FullOutputPath, Path: opts.Path, MaxChars: opts.MaxChars,
			}
		}
		return ExtractRelevant(output, real)
	case "offloadLargeOutput":
		var input OutputOffloadInput
		var opts *fixtureOptions
		var sinkMode string
		if err := json.Unmarshal(raw[0], &input); err != nil {
			t.Fatal(err)
		}
		if err := json.Unmarshal(raw[1], &opts); err != nil {
			t.Fatal(err)
		}
		if err := json.Unmarshal(raw[2], &sinkMode); err != nil {
			t.Fatal(err)
		}
		sink := &recordingSink{fail: sinkMode == "failure"}
		offloader := Offloader{Sink: sink}
		var real any
		if opts != nil {
			if opts.Mode == "directHook" {
				real = DistillHook(func(string) (string, error) {
					t.Fatal("distill hook must never be called")
					return "", nil
				})
			} else {
				real = &OutputOffloadOptions{EscalationWanted: opts.EscalationWanted}
				if opts.Mode == "optionsHook" {
					real.(*OutputOffloadOptions).Hook = func(string) (string, error) {
						t.Fatal("distill hook must never be called")
						return "", nil
					}
				}
			}
		}
		return offloader.OffloadLargeOutput(input, real)
	default:
		t.Fatalf("unknown fn %q", fx.Fn)
		return nil
	}
}

func TestFixtureParity(t *testing.T) {
	fixtures := loadFixtures(t)
	if len(fixtures) < 25 {
		t.Fatalf("expected at least 25 fixtures, got %d", len(fixtures))
	}
	seen := map[string]int{}
	for _, fx := range fixtures {
		seen[fx.Fn]++
		t.Run(fx.Fn+"/"+fx.Name, func(t *testing.T) {
			got, err := jscompat.Stringify(callFixture(t, fx))
			if err != nil {
				t.Fatal(err)
			}
			if string(got) != fx.OutJSON {
				t.Errorf("args=%s\n got: %s\nwant: %s", fx.ArgsJSON, got, fx.OutJSON)
			}
		})
	}
	for _, fn := range []string{"extractRelevant", "offloadLargeOutput"} {
		if seen[fn] == 0 {
			t.Errorf("no fixtures for %s", fn)
		}
	}
}

func TestDiskSinkWritesLargeOutput(t *testing.T) {
	root := t.TempDir()
	output := strings.Repeat("x", OFFLOAD_THRESHOLD_CHARS+1)
	result := OffloadLargeOutput(OutputOffloadInput{
		Output: output, Workspace: root, ToolName: "bash", CallID: "call/1",
	}, nil)
	if result.OffloadPath == nil {
		t.Fatal("missing offload path")
	}
	wantPath := filepath.Join(root, ".codeaf", "tool-output", "call_1.log")
	if *result.OffloadPath != wantPath {
		t.Fatalf("path=%q want=%q", *result.OffloadPath, wantPath)
	}
	got, err := os.ReadFile(wantPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != output {
		t.Fatal("saved output differs")
	}
}

func TestUTF16SurrogateCut(t *testing.T) {
	got := utf16SliceTo("a💩b", 2)
	want := []byte{'a', 0xed, 0xa0, 0xbd}
	if string(want) != got {
		t.Fatalf("got bytes %x want %x", []byte(got), want)
	}
}

func TestDiskSinkDisambiguatesSanitizedCallIDCollisions(t *testing.T) {
	// Finding 7: parallel calls whose IDs sanitize to the same basename retain
	// distinct files and each inline handle names its own spill.
	workspace := t.TempDir()
	offloader := Offloader{Sink: DiskSink{}}
	inputs := []OutputOffloadInput{
		{Output: "output from slash", Workspace: workspace, ToolName: "bash", CallID: "call/a"},
		{Output: "output from question", Workspace: workspace, ToolName: "bash", CallID: "call?a"},
	}
	results := make([]OutputOffloadResult, len(inputs))
	start := make(chan struct{})
	var wait sync.WaitGroup
	for index := range inputs {
		wait.Add(1)
		go func(index int) {
			defer wait.Done()
			<-start
			results[index] = offloader.OffloadLargeOutput(
				inputs[index], OutputOffloadOptions{Force: true},
			)
		}(index)
	}
	close(start)
	wait.Wait()
	if results[0].OffloadPath == nil || results[1].OffloadPath == nil {
		t.Fatalf("offload paths = %#v", results)
	}
	if *results[0].OffloadPath == *results[1].OffloadPath {
		t.Fatalf("colliding calls shared path %q", *results[0].OffloadPath)
	}
	for index, result := range results {
		if !strings.Contains(result.Inline, *result.OffloadPath) {
			t.Fatalf("result %d does not name own path: %#v", index, result)
		}
		contents, err := os.ReadFile(*result.OffloadPath)
		if err != nil {
			t.Fatal(err)
		}
		if string(contents) != inputs[index].Output {
			t.Fatalf("result %d contents = %q, want %q", index, contents, inputs[index].Output)
		}
	}
}

func TestDiskSinkCollisionSearchIsBounded(t *testing.T) {
	// Final-scan finding 2: the O_EXCL scan must fail after a bounded number
	// of candidates instead of looping past the run deadline.
	dir := t.TempDir()
	base := filepath.Join(dir, "call.log")
	if err := os.WriteFile(base, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	for index := 2; index <= 100; index++ {
		name := filepath.Join(dir, "call-"+jscompat.FormatNumber(float64(index))+".log")
		if err := os.WriteFile(name, []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := (DiskSink{}).WriteOutput(base, "payload"); err == nil {
		t.Fatal("expected bounded-collision error, got nil")
	}
}
