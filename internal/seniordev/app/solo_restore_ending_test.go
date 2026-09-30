//go:build !windows

package app

import (
	"bytes"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/delegate"
)

// heldVerification puts the person's edits after verification has started,
// which is the window in which a submitted tree can diverge from its freeze.
func heldVerification(t *testing.T, workspace string, fail bool) (string, string) {
	t.Helper()
	gate := t.TempDir()
	started := filepath.Join(gate, "started")
	release := filepath.Join(gate, "release")
	tail := "true"
	if fail {
		tail = "printf '[build failed]\\n'; exit 2"
	}
	makefile := fmt.Sprintf("build:\n\t@true\n\ntest:\n\t@touch %s; while test ! -f %s; do sleep 0.02; done; %s\n", started, release, tail)
	if err := writeFile(filepath.Join(workspace, "Makefile"), makefile); err != nil {
		t.Fatal(err)
	}
	if err := writeFile(filepath.Join(workspace, "delete-me.txt"), "original\n"); err != nil {
		t.Fatal(err)
	}
	if err := gitRun(workspace, "add", "Makefile", "delete-me.txt"); err != nil {
		t.Fatal(err)
	}
	if err := gitRun(workspace, "commit", "-m", "verification gate"); err != nil {
		t.Fatal(err)
	}
	return started, release
}

func releaseAfterPersonalEdits(t *testing.T, workspace, started, release, debugPath string, maximum time.Duration) {
	t.Helper()
	deadline := time.Now().Add(maximum)
	for {
		if _, err := os.Stat(started); err == nil {
			break
		}
		if time.Now().After(deadline) {
			debug, _ := os.ReadFile(debugPath)
			t.Fatalf("the project's make test did not start; child notes:\n%s", debug)
		}
		time.Sleep(20 * time.Millisecond)
	}
	if err := writeFile(filepath.Join(workspace, "README.md"), "person's later edit\n"); err != nil {
		t.Fatal(err)
	}
	if err := writeFile(filepath.Join(workspace, "notes.txt"), "person new note\n"); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(filepath.Join(workspace, "delete-me.txt")); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(release, nil, 0o600); err != nil {
		t.Fatal(err)
	}
}

func checkRescueEnding(t *testing.T, workspace string, data map[string]any, message string) {
	t.Helper()
	rescue, _ := data["rescue_path"].(string)
	if rescue == "" || !strings.Contains(message, "set aside in "+rescue) {
		t.Fatalf("terminal data = %#v; ending = %q, want the rescue folder", data, message)
	}
	manifest, _ := data["rescue_manifest"].(string)
	if data["rescue_deletions"] != true || manifest == "" || !strings.Contains(message, manifest) {
		t.Fatalf("terminal data = %#v; ending = %q, want the deletion manifest", data, message)
	}
	if body, err := os.ReadFile(filepath.Join(rescue, manifest)); err != nil || string(body) != "delete-me.txt\n" {
		t.Fatalf("deletion manifest = %q, %v", body, err)
	}
	for name, want := range map[string]string{
		"README.md": "person's later edit\n", "notes.txt": "person new note\n",
	} {
		got, err := os.ReadFile(filepath.Join(rescue, name))
		if err != nil || string(got) != want {
			t.Fatalf("rescued %s = %q, %v; want %q", name, got, err, want)
		}
	}
	if got, err := os.ReadFile(filepath.Join(workspace, "README.md")); err != nil || string(got) != "base\n" {
		t.Fatalf("restored README = %q, %v", got, err)
	}
	if _, err := os.Stat(filepath.Join(workspace, "notes.txt")); !os.IsNotExist(err) {
		t.Fatalf("later note remains in restored tree: %v", err)
	}
	if body, err := os.ReadFile(filepath.Join(workspace, "delete-me.txt")); err != nil || string(body) != "original\n" {
		t.Fatalf("candidate file was not restored after the deletion: %q, %v", body, err)
	}
}

type rescueEndSink struct {
	dir string
	err error
}

func (sink *rescueEndSink) Hello(delegate.Hello)       {}
func (sink *rescueEndSink) Stage(delegate.StageRecord) {}
func (sink *rescueEndSink) Step(delegate.StepRecord)   {}
func (sink *rescueEndSink) Terminal(terminal delegate.Terminal) {
	sink.err = delegate.AppendAction(sink.dir, delegate.EndAction(time.Now(), terminal))
}

func rescueChildProgram() delegate.Delegate {
	return delegate.Delegate{Name: "senior-dev", Default: "run", Commands: []delegate.Command{{
		Name: "run", Bind: func(*flag.FlagSet) delegate.Body {
			return func(ctx context.Context, host delegate.Host, args []string) error {
				host.Hello(Stages)
				ending := Run(ctx, host, Options{Goal: strings.Join(args, " "), High: "openrouter/fixture/vendor-model"}, os.Stderr)
				host.Terminal(ending)
				return nil
			}
		},
	}}}
}

// The child is the test executable, but the pipeline, process pipe, delegate
// host and HTTP model road are real. Only the model's answers are scripted.
func TestRescueShipEndingCrossesTheDelegateChild(t *testing.T) {
	if os.Getenv("CODEAF_RESCUE_TEST_CHILD") == "1" {
		program := rescueChildProgram()
		inv, err := delegate.Parse(program, []string{"run", "--dir", os.Getenv("CODEAF_RESCUE_TEST_WORKSPACE"), "--", "Add the feature."}, io.Discard)
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(2)
		}
		if delegate.RunChild(context.Background(), inv, os.Stdout) != delegate.StatusPass {
			os.Exit(1)
		}
		os.Exit(0)
	}
	state := t.TempDir()
	t.Setenv("CODEAF_HOME", state)
	catalog, err := filepath.Abs("../modelsdev/testdata/catalog.json")
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("SENIOR_DEV_MODELS_PATH", catalog)
	t.Setenv("SENIOR_DEV_DISABLE_MODELS_FETCH", "1")
	var calls atomic.Int32
	stub := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		steps := []struct{ name, arguments string }{
			{"write", `{"filePath":"feature.txt","content":"implemented\n"}`},
			{"write", `{"filePath":".senior-dev/checklist.md","content":"- [x] feature implemented\n"}`},
			{"write", `{"filePath":".senior-dev/pinned.txt","content":"make test\n"}`},
			{"submit", `{"reason":"feature implemented","evidence":"make test exit 0","checklist_satisfied":true}`},
		}
		index := int(calls.Add(1)) - 1
		if index > len(steps) {
			http.Error(writer, "unexpected model call", http.StatusBadRequest)
			return
		}
		writer.Header().Set("Content-Type", "text/event-stream")
		if index == len(steps) {
			_, _ = io.WriteString(writer, chatReply("done", 10))
			return
		}
		_, _ = io.WriteString(writer, toolCallReply(steps[index].name, steps[index].arguments))
	}))
	defer stub.Close()
	workspace, _ := guardWorkspace(t)
	started, release := heldVerification(t, workspace, false)
	t.Setenv("CODEAF_RESCUE_TEST_CHILD", "1")
	t.Setenv("CODEAF_RESCUE_TEST_WORKSPACE", workspace)
	self, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	type answer struct {
		result delegate.Result
		err    error
	}
	completed := make(chan answer, 1)
	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()
	sink := &rescueEndSink{dir: t.TempDir()}
	stderr := filepath.Join(t.TempDir(), "child-stderr.log")
	go func() {
		result, err := delegate.Run(ctx, delegate.Launch{
			Name: "senior-dev", Bin: self, Args: []string{"-test.run=^TestRescueShipEndingCrossesTheDelegateChild$"},
			Env: delegate.ChildEnv(delegate.ModelAPI{BaseURL: stub.URL + "/v1", Token: "stub-token"}), Dir: workspace,
			StderrPath: stderr,
		}, sink)
		completed <- answer{result, err}
	}()
	releaseAfterPersonalEdits(t, workspace, started, release, stderr, 75*time.Second)
	finished := <-completed
	if finished.err != nil {
		t.Fatal(finished.err)
	}
	terminal := finished.result.Reading.Terminal
	if terminal == nil {
		t.Fatal("child sent no terminal record")
	}
	data := map[string]any{}
	for key, raw := range terminal.Data {
		var value any
		if err := json.Unmarshal(raw, &value); err != nil {
			t.Fatal(err)
		}
		data[key] = value
	}
	checkRescueEnding(t, workspace, data, terminal.Message)
	if got := calls.Load(); got != 5 {
		t.Fatalf("model stub answered %d calls, want four tool calls and their final reply through the real model API", got)
	}
	if sink.err != nil {
		t.Fatal(sink.err)
	}
	actions, err := delegate.ReadActions(sink.dir, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(actions) != 1 || actions[0].Kind != delegate.ActionEnd || !strings.Contains(actions[0].Message, data["rescue_path"].(string)) {
		t.Fatalf("stored task end record = %#v", actions)
	}
}

func TestShipWithoutLaterEditsHasNoRescueSentence(t *testing.T) {
	workspace := testRepoWithEntrypoints(t)
	base := strings.TrimSpace(gitOutput(context.Background(), workspace, "rev-parse", "HEAD"))
	runner := newPipeline(cliArgs{}, workspace, pipelineDeps{
		Backend: &soloScriptedBackend{}, Events: newEventWriter(io.Discard), Notes: io.Discard,
	})
	defer runner.runtime.Close()
	outcome, err := runner.runSolo(context.Background(), "Add the feature.", base)
	if err != nil {
		t.Fatal(err)
	}
	ending := endingOf(pipelineResult{Status: delegate.StatusPass, Terminal: outcome.TerminalData})
	if _, hasRescue := outcome.TerminalData["rescue_path"]; hasRescue || strings.Contains(ending.Message, "set aside") {
		t.Fatalf("unchanged submitted tree named a rescue: data=%#v ending=%q", outcome.TerminalData, ending.Message)
	}
}

func TestFailedSuiteRestoreNamesRescuedEditsInEnding(t *testing.T) {
	state := t.TempDir()
	t.Setenv("CODEAF_HOME", state)
	workspace, _ := guardWorkspace(t)
	started, release := heldVerification(t, workspace, true)
	base := strings.TrimSpace(gitOutput(context.Background(), workspace, "rev-parse", "HEAD"))
	var events bytes.Buffer
	runner := newPipeline(cliArgs{}, workspace, pipelineDeps{Events: newEventWriter(&events), Notes: io.Discard})
	defer runner.runtime.Close()
	stateOfRun := &soloState{baseSHA: base}
	if err := runner.soloCaptureStart(stateOfRun); err != nil {
		t.Fatal(err)
	}
	if err := writeFile(filepath.Join(workspace, "broken.go"), "package broken\nfunc (\n"); err != nil {
		t.Fatal(err)
	}
	finished := make(chan soloOutcome, 1)
	go func() {
		outcome := soloOutcome{}
		runner.soloShip(context.Background(), stateOfRun, &outcome, nil)
		finished <- outcome
	}()
	releaseAfterPersonalEdits(t, workspace, started, release, "", 15*time.Second)
	outcome := <-finished
	if !outcome.SuiteDead || outcome.RestoreSource == "" {
		t.Fatalf("failed suite was not restored: %#v", outcome)
	}
	ending := endingOf(pipelineResult{Status: delegate.StatusFail, Terminal: outcome.TerminalData})
	checkRescueEnding(t, workspace, outcome.TerminalData, ending.Message)
}
