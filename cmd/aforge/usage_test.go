package main

import (
	"bytes"
	"errors"
	"strings"
	"testing"
)

// exitCodeOf reads an error the way [execute] does, so a test can assert the
// number the shell actually sees rather than the shape of the error value.
func exitCodeOf(err error) int {
	var status exitStatus
	switch {
	case err == nil:
		return 0
	case errors.As(err, &status):
		return int(status)
	default:
		return 1
	}
}

// captureUsage points the help streams at buffers for the length of one call.
func captureUsage(t *testing.T) (out, errs *bytes.Buffer) {
	t.Helper()
	out, errs = &bytes.Buffer{}, &bytes.Buffer{}
	previousOut, previousErr := usageOut, usageErr
	usageOut, usageErr = out, errs
	t.Cleanup(func() { usageOut, usageErr = previousOut, previousErr })
	return out, errs
}

// ASKING FOR HELP IS NOT A FAILURE.
//
// `--help` on every one of these doors used to answer with Go's own internal
// string — `error: flag: help requested` — and exit 1. Nine of them printed
// that line and NOTHING ELSE, so there was no way at all to learn what `aforge
// why` or `aforge logs` take; the seven that did print a usage block still
// ended with the word "error" under text that is not one, and a Makefile that
// ran `aforge do --help` to check the binary read a failing command.
func TestAskingForHelpIsNotAFailure(t *testing.T) {
	t.Setenv("AFORGE_HOME", t.TempDir())
	for _, door := range []struct {
		name string
		run  func([]string) error
	}{
		{"doctor", runDoctor},
		{"logs", runLogs},
		{"why", runWhy},
		{"competence", runCompetence},
		{"wake", runWake},
		{"rebuild", runRebuild},
		{"notebook", runNotebook},
		{"cache", runCache},
		{"do", runDo},
		{"exec", runExec},
		{"run", runExecute},
		{"plan", runPlan},
		{"revise", runRevise},
		{"services", runServices},
		{"models", runModels},
		{"show", runShow},
	} {
		t.Run(door.name, func(t *testing.T) {
			out, errs := captureUsage(t)
			err := door.run([]string{"--help"})
			if code := exitCodeOf(err); code != 0 {
				t.Fatalf("`aforge %s --help` left with %d, want 0 — asking for help is not a failure\n%s%s",
					door.name, code, errs.String(), out.String())
			}
			printed := out.String()
			if strings.TrimSpace(printed) == "" {
				t.Fatalf("`aforge %s --help` printed nothing at all, so there is no way to learn what it takes",
					door.name)
			}
			if strings.Contains(printed, "flag: help requested") {
				t.Fatalf("`aforge %s --help` printed Go's own internal string:\n%s", door.name, printed)
			}
			if !strings.Contains(printed, "aforge "+door.name) {
				t.Fatalf("`aforge %s --help` never names the command it is about:\n%s", door.name, printed)
			}
			if errs.Len() != 0 {
				t.Fatalf("`aforge %s --help` wrote to stderr, where a caller reads failures:\n%s",
					door.name, errs.String())
			}
		})
	}
}

// A REAL FLAG ERROR IS STILL AN ERROR, and it is one fact said once.
//
// It used to be said twice: the flag package printed `flag provided but not
// defined: -nosuchflag` and the whole flag list, and then the dispatch printed
// the same sentence again under `error:`, leaving the reader to work out that
// the two were one thing.
func TestABadFlagIsRefusedOnceAndOnStderr(t *testing.T) {
	out, errs := captureUsage(t)
	err := runDo([]string{"a task", "--nosuchflag"})
	if code := exitCodeOf(err); code == 0 {
		t.Fatalf("a bad flag left with 0, and a script cannot tell it from a run that worked:\n%s", errs.String())
	}
	said := errs.String()
	if count := strings.Count(said, "flag provided but not defined"); count != 1 {
		t.Fatalf("the same refusal is printed %d times, want once:\n%s", count, said)
	}
	if !strings.Contains(said, "aforge do") {
		t.Fatalf("a bad flag never says which command it was refused by:\n%s", said)
	}
	if out.Len() != 0 {
		t.Fatalf("a refusal was written to stdout, which is where the answer goes:\n%s", out.String())
	}
}

// ONE SOURCE OF TRUTH: a command's shape is written once, in the table `aforge
// --help` prints, and every per-command usage is a reading of that table.
func TestACommandsUsageIsReadOutOfTheOneTable(t *testing.T) {
	if shape := usageForCommand("do"); !strings.Contains(shape, "exit 0 the whole of it stands") {
		t.Fatalf("`do`'s usage lost the exit ladder that is written in usageText:\n%s", shape)
	}
	// `aforge run subharness` is dispatched somewhere else entirely, and its
	// line is not `aforge run`'s.
	if shape := usageForCommand("run"); strings.Contains(shape, "subharness") {
		t.Fatalf("`run`'s usage borrowed the subharness runner's line:\n%s", shape)
	}
	if shape := usageForCommand("run subharness"); !strings.Contains(shape, "--input") {
		t.Fatalf("`run subharness` has no line of its own:\n%s", shape)
	}
	if shape := usageForCommand("nosuchcommand"); shape != "" {
		t.Fatalf("a command with no line in the table invented one:\n%s", shape)
	}
}

// A DOOR THAT PARSES NO FLAGS STILL ANSWERS THE FIRST GESTURE. `aforge show
// --help` used to answer `open --help: no such file or directory` — a
// filesystem error about a flag — and `aforge models --help` ran the command
// with the flag silently ignored.
func TestProbingAFlaglessCommandWithHelpIsNotAnError(t *testing.T) {
	for _, door := range []struct {
		name string
		run  func([]string) error
	}{
		{"show", runShow},
		{"models", runModels},
		{"cache", runCache},
	} {
		out, _ := captureUsage(t)
		err := door.run([]string{"--help"})
		if code := exitCodeOf(err); code != 0 {
			t.Fatalf("`aforge %s --help` left with %d: %v", door.name, code, err)
		}
		if strings.Contains(out.String(), "no such file or directory") {
			t.Fatalf("`aforge %s --help` answered with a filesystem error:\n%s", door.name, out.String())
		}
	}
}
