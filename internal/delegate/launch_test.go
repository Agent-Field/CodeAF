//go:build !windows

package delegate

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// fakeProgram is a shell script that stands in for codeaf running a program:
// it writes its argv to the file FAKE_ARGS names, emits a hello, a stage, a
// v1 spend line (which the reader no longer knows, and ignores) and a step,
// then runs the body it was given.
func fakeProgram(t *testing.T, body string) string {
	t.Helper()
	dir := t.TempDir()
	script := filepath.Join(dir, "fake.sh")
	writeProgram(t, script, strings.Join([]string{
		`if [ -n "$FAKE_ARGS" ]; then printf '%s\n' "$@" > "$FAKE_ARGS"; fi`,
		`if [ -n "$FAKE_ENV" ]; then env > "$FAKE_ENV"; fi`,
		`echo '{"type":"hello","protocol":2,"delegate":"fake","stages":["implement"]}'`,
		`echo '{"type":"stage","stage":"implement","status":"running"}'`,
		`echo '{"type":"spend","cost_usd":0.01}'`,
		`echo '{"type":"step","command":"bash: true","observation":"ok"}'`,
		`echo 'a note for a person' >&2`,
		body,
	}, "\n"))
	return script
}

// fakeLaunch is the launch of the fake program the way a worker builds one:
// the program's line after the executable, and the child's environment.
func fakeLaunch(t *testing.T, script, workspace, brief string, ceilings Ceilings, api ModelAPI) Launch {
	t.Helper()
	program := Delegate{Name: "fake", Default: "run"}
	return Launch{
		Name: "fake",
		Bin:  script,
		Args: ChildArgs(program, workspace, brief, ceilings, RunFacts{}),
		Env:  ChildEnv(api),
		Dir:  workspace,
	}
}

func terminalLine(status, message string) string {
	return `echo '{"type":"terminal","status":"` + status + `","message":"` + message + `","data":{"cost_usd":0.02}}'`
}

func TestRunStartsTheProgramsLineAndReadsTheTerminal(t *testing.T) {
	script := fakeProgram(t, terminalLine("pass", "done"))
	args := filepath.Join(t.TempDir(), "args")
	t.Setenv("FAKE_ARGS", args)
	workspace := t.TempDir()
	stderr := filepath.Join(t.TempDir(), "stderr.log")
	sink := &recorder{}
	launch := fakeLaunch(t, script, workspace, "rewrite the thing", Ceilings{CostUSD: 1.5, Hours: 0.25}, ModelAPI{})
	launch.StderrPath = stderr
	result, err := Run(context.Background(), launch, sink)
	if err != nil {
		t.Fatal(err)
	}
	if result.ExitCode != 0 || result.Stopped || result.Reading.Terminal == nil || result.Reading.Terminal.Status != StatusPass {
		t.Fatalf("result = %+v", result)
	}
	got, _ := os.ReadFile(args)
	want := "fake\nrun\n--json\n--dir\n" + workspace + "\n--max-cost\n1.5\n--max-hours\n0.25\n--\nrewrite the thing\n"
	if string(got) != want {
		t.Fatalf("argv =\n%s\nwant\n%s", got, want)
	}
	if log, _ := os.ReadFile(stderr); !strings.Contains(string(log), "a note for a person") {
		t.Fatalf("stderr file = %q, want the program's note kept", log)
	}
	if sink.hello == nil || sink.hello.Delegate != "fake" || sink.steps[0] != "bash: true→ok" {
		t.Fatalf("sink = %+v", sink)
	}
	// The program's own word about money is not a record any more: the spend
	// line is the one line the reader dropped.
	if result.Reading.Ignored != 1 {
		t.Fatalf("ignored = %d, want the v1 spend line and nothing else", result.Reading.Ignored)
	}
}

func TestRunLeavesAnUnsetCeilingOffTheLine(t *testing.T) {
	script := fakeProgram(t, terminalLine("pass", "done"))
	args := filepath.Join(t.TempDir(), "args")
	t.Setenv("FAKE_ARGS", args)
	workspace := t.TempDir()
	if _, err := Run(context.Background(), fakeLaunch(t, script, workspace, "b", Ceilings{}, ModelAPI{}), nil); err != nil {
		t.Fatal(err)
	}
	got, _ := os.ReadFile(args)
	if string(got) != "fake\nrun\n--json\n--dir\n"+workspace+"\n--\nb\n" {
		t.Fatalf("argv =\n%s\nwant no --max-cost and no --max-hours at all", got)
	}
}

// THE BRIEF IS THE PERSON'S WORDS AND REACHES THE PROGRAM AS WRITTEN: after
// `--`, one element, whatever it spells — a flag, a placeholder, a key's name.
func TestRunHandsTheBriefOverVerbatim(t *testing.T) {
	script := fakeProgram(t, terminalLine("pass", "done"))
	args := filepath.Join(t.TempDir(), "args")
	t.Setenv("FAKE_ARGS", args)
	for _, brief := range []string{
		"https://github.com/o/r/pull/1 check {{ .Name }} escaping, and {{key}} in {{workspace}}",
		"--dir /etc --max-cost 999 read these as words",
	} {
		if _, err := Run(context.Background(), fakeLaunch(t, script, t.TempDir(), brief, Ceilings{}, ModelAPI{}), nil); err != nil {
			t.Fatalf("the launch refused a brief: %v", err)
		}
		got, _ := os.ReadFile(args)
		lines := strings.Split(strings.TrimRight(string(got), "\n"), "\n")
		if last := lines[len(lines)-1]; last != brief || lines[len(lines)-2] != "--" {
			t.Fatalf("the brief reached the program as\n%q\nwant it verbatim after --", lines)
		}
	}
}

// NO PROVIDER KEY IS INHERITED BY A PROGRAM. The child's environment is this
// process's with provider keys and model redirections taken out and the
// model API's two names put in.
func TestTheChildsEnvironmentCarriesTheAPIAndNoKey(t *testing.T) {
	script := fakeProgram(t, terminalLine("pass", "done"))
	envFile := filepath.Join(t.TempDir(), "env")
	t.Setenv("FAKE_ENV", envFile)
	t.Setenv("OPENROUTER_API_KEY", "sk-or-v1-parent")
	t.Setenv("OPENAI_API_KEY", "sk-parent")
	t.Setenv("DEEPSEEK_API_KEY", "sk-deepseek")
	t.Setenv("CODEAF_BASE_URL", "https://elsewhere.example/v1")
	t.Setenv("SOMETHING_ELSE", "kept")
	api := ModelAPI{BaseURL: "http://127.0.0.1:9/v1", Token: "run-token"}
	if _, err := Run(context.Background(), fakeLaunch(t, script, t.TempDir(), "b", Ceilings{}, api), nil); err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(envFile)
	environ := string(data)
	for _, gone := range []string{"sk-or-v1-parent", "sk-parent", "sk-deepseek", "elsewhere.example"} {
		if strings.Contains(environ, gone) {
			t.Fatalf("the child inherited %q:\n%s", gone, environ)
		}
	}
	for _, kept := range []string{"SOMETHING_ELSE=kept", EnvModelAPI + "=http://127.0.0.1:9/v1", EnvModelToken + "=run-token"} {
		if !strings.Contains(environ, kept) {
			t.Fatalf("the child's environment lacks %q:\n%s", kept, environ)
		}
	}
}

func TestRunAnswersNoTerminalWhenTheProgramExitsWithoutOne(t *testing.T) {
	script := fakeProgram(t, "exit 3")
	result, err := Run(context.Background(), fakeLaunch(t, script, t.TempDir(), "b", Ceilings{}, ModelAPI{}), nil)
	if !errors.Is(err, ErrNoTerminal) {
		t.Fatalf("err = %v, want ErrNoTerminal", err)
	}
	if result.ExitCode != 3 || result.Reading.LastStage != "implement" {
		t.Fatalf("result = %+v, want the exit code and the last stage seen kept", result)
	}
}

func TestRunTerminatesOnCancelAndKeepsATerminalWrittenInTheGrace(t *testing.T) {
	// The program traps TERM, writes its terminal and exits; the sleep is what
	// the signal interrupts.
	script := fakeProgram(t, strings.Join([]string{
		`trap '` + strings.ReplaceAll(terminalLine("budget-exhausted", "stopped by the parent"), "'", `'"'"'`) + `; exit 0' TERM`,
		`sleep 30 &`,
		`wait $!`,
	}, "\n"))
	ctx, cancel := context.WithCancel(context.Background())
	sink := newRecorder()
	go func() {
		// Cancel once the program has said its first word, so the trap is armed.
		select {
		case <-sink.spoke:
		case <-time.After(5 * time.Second):
		}
		time.Sleep(50 * time.Millisecond)
		cancel()
	}()
	launch := fakeLaunch(t, script, t.TempDir(), "b", Ceilings{}, ModelAPI{})
	launch.Grace = 5 * time.Second
	result, err := Run(ctx, launch, sink)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v, want the context's own", err)
	}
	if !result.Stopped || result.Killed {
		t.Fatalf("result = %+v, want stopped by SIGTERM and not killed", result)
	}
	if result.Reading.Terminal == nil || result.Reading.Terminal.Status != StatusBudget {
		t.Fatalf("terminal = %+v, want the one the program wrote on its way out", result.Reading.Terminal)
	}
}

func TestRunKillsAProgramThatIgnoresTerm(t *testing.T) {
	script := fakeProgram(t, strings.Join([]string{
		`trap '' TERM`,
		`sleep 30`,
	}, "\n"))
	// THE PROGRAM MUST HAVE ITS TRAP BEFORE THE STOP ARRIVES. Three hundred
	// milliseconds was the whole of its life here, and on a loaded box the
	// shell had not yet run `trap` when TERM came, so it died of the TERM and
	// the test read a program that honours TERM as one the launch failed to
	// kill. It is given a second and a half to get there; the grace that
	// follows is what the test is about.
	ctx, cancel := context.WithTimeout(context.Background(), 1500*time.Millisecond)
	defer cancel()
	started := time.Now()
	launch := fakeLaunch(t, script, t.TempDir(), "b", Ceilings{}, ModelAPI{})
	launch.Grace = 200 * time.Millisecond
	result, err := Run(ctx, launch, nil)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("err = %v", err)
	}
	if !result.Stopped || !result.Killed || result.Reading.Terminal != nil {
		t.Fatalf("result = %+v, want stopped, killed, no terminal", result)
	}
	if time.Since(started) > 5*time.Second {
		t.Fatalf("the launch took %s to give up on a program that ignores TERM", time.Since(started))
	}
}

func TestRunRefusesAProgramThatIsNotThere(t *testing.T) {
	_, err := Run(context.Background(), Launch{Name: "gone", Bin: filepath.Join(t.TempDir(), "gone"), Dir: t.TempDir()}, nil)
	if err == nil || !strings.Contains(err.Error(), "start gone") {
		t.Fatalf("err = %v", err)
	}
}

// writeProgram writes an executable shell script.
func writeProgram(t *testing.T, path, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("#!/bin/sh\n"+body), 0o755); err != nil {
		t.Fatal(err)
	}
}
