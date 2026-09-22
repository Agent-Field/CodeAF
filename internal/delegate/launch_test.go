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

// fakeProgram is a shell script that behaves as a delegate: it writes its argv
// to the file FAKE_ARGS names, emits a stage, a spend and a step, then runs
// the body it was given.
func fakeProgram(t *testing.T, body string) Manifest {
	t.Helper()
	dir := t.TempDir()
	script := filepath.Join(dir, "fake.sh")
	writeProgram(t, script, strings.Join([]string{
		`if [ -n "$FAKE_ARGS" ]; then printf '%s\n' "$@" > "$FAKE_ARGS"; fi`,
		`echo '{"type":"stage","stage":"implement","status":"running"}'`,
		`echo '{"type":"spend","cost_usd":0.01}'`,
		`echo '{"type":"step","command":"bash: true","observation":"ok"}'`,
		`echo 'a note for a person' >&2`,
		body,
	}, "\n"))
	return Manifest{
		Name:        "fake",
		Description: "a fake delegate",
		Bin:         script,
		BinPath:     script,
		Argv:        []string{"run", "--dir", FillWorkspace, "--max-cost", FillCostUSD, "--max-hours", FillHours, "--", FillBrief},
		Env:         map[string]string{"FAKE_KEY": FillKey},
	}
}

func terminalLine(status, message string) string {
	return `echo '{"type":"terminal","status":"` + status + `","message":"` + message + `","data":{"cost_usd":0.02}}'`
}

func TestRunFillsTheArgvAndReadsTheTerminal(t *testing.T) {
	m := fakeProgram(t, terminalLine("pass", "done"))
	args := filepath.Join(t.TempDir(), "args")
	t.Setenv("FAKE_ARGS", args)
	workspace := t.TempDir()
	stderr := filepath.Join(t.TempDir(), "stderr.log")
	sink := &recorder{}
	result, err := Run(context.Background(), Launch{
		Manifest:   m,
		Fills:      Fills{Brief: "rewrite the thing", Workspace: workspace, CostUSD: 1.5, Hours: 0.25, Key: "sk-test"},
		StderrPath: stderr,
	}, sink)
	if err != nil {
		t.Fatal(err)
	}
	if result.ExitCode != 0 || result.Stopped || result.Reading.Terminal == nil || result.Reading.Terminal.Status != StatusPass {
		t.Fatalf("result = %+v", result)
	}
	got, _ := os.ReadFile(args)
	want := "run\n--dir\n" + workspace + "\n--max-cost\n1.5\n--max-hours\n0.25\n--\nrewrite the thing\n"
	if string(got) != want {
		t.Fatalf("argv =\n%s\nwant\n%s", got, want)
	}
	if log, _ := os.ReadFile(stderr); !strings.Contains(string(log), "a note for a person") {
		t.Fatalf("stderr file = %q, want the program's note kept", log)
	}
	if sink.spend[0] != 0.01 || sink.steps[0] != "bash: true→ok" {
		t.Fatalf("sink = %+v", sink)
	}
}

func TestRunDropsACeilingFlagWhoseValueIsUnset(t *testing.T) {
	m := fakeProgram(t, terminalLine("pass", "done"))
	args := filepath.Join(t.TempDir(), "args")
	t.Setenv("FAKE_ARGS", args)
	workspace := t.TempDir()
	if _, err := Run(context.Background(), Launch{Manifest: m, Fills: Fills{Brief: "b", Workspace: workspace}}, nil); err != nil {
		t.Fatal(err)
	}
	got, _ := os.ReadFile(args)
	if string(got) != "run\n--dir\n"+workspace+"\n--\nb\n" {
		t.Fatalf("argv =\n%s\nwant no --max-cost and no --max-hours at all", got)
	}
}

func TestRunAnswersNoTerminalWhenTheProgramExitsWithoutOne(t *testing.T) {
	m := fakeProgram(t, "exit 3")
	result, err := Run(context.Background(), Launch{Manifest: m, Fills: Fills{Brief: "b", Workspace: t.TempDir()}}, nil)
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
	m := fakeProgram(t, strings.Join([]string{
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
	result, err := Run(ctx, Launch{Manifest: m, Fills: Fills{Brief: "b", Workspace: t.TempDir()}, Grace: 5 * time.Second}, sink)
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
	m := fakeProgram(t, strings.Join([]string{
		`trap '' TERM`,
		`sleep 30`,
	}, "\n"))
	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
	defer cancel()
	started := time.Now()
	result, err := Run(ctx, Launch{Manifest: m, Fills: Fills{Brief: "b", Workspace: t.TempDir()}, Grace: 200 * time.Millisecond}, nil)
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
	m := Manifest{Name: "gone", Bin: filepath.Join(t.TempDir(), "gone"), Argv: []string{FillBrief}}
	_, err := Run(context.Background(), Launch{Manifest: m, Fills: Fills{Brief: "b", Workspace: t.TempDir()}}, nil)
	if err == nil || !strings.Contains(err.Error(), "start gone") {
		t.Fatalf("err = %v", err)
	}
}
