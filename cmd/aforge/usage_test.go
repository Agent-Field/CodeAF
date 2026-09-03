package main

import (
	"bytes"
	"errors"
	"strings"
	"testing"

	"github.com/Agent-Field/aforge-v2/internal/config"
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

// ONE SPELLING FOR ONE FLAG. `aforge --help` writes `--json` and the flag
// package writes `-json`, so a reader comparing the two help surfaces saw two
// conventions for one flag and had to guess whether both worked.
func TestEveryFlagIsSpelledTheWayTheUsageSpellsIt(t *testing.T) {
	out, _ := captureUsage(t)
	if code := exitCodeOf(runDo([]string{"--help"})); code != 0 {
		t.Fatalf("`aforge do --help` left with %d", code)
	}
	printed := out.String()
	for _, want := range []string{"--json", "--timeout", "--model"} {
		if !strings.Contains(printed, want) {
			t.Fatalf("`aforge do --help` never writes %q:\n%s", want, printed)
		}
	}
	// One dash is right for `-w` and `-o` and wrong for everything else, which
	// is exactly the split the usage table already keeps.
	for _, line := range strings.Split(printed, "\n") {
		trimmed := strings.TrimSpace(line)
		if !strings.HasPrefix(trimmed, "-") || strings.HasPrefix(trimmed, "--") {
			continue
		}
		if name := strings.Fields(strings.TrimPrefix(trimmed, "-"))[0]; len(name) > 1 {
			t.Fatalf("the flag row %q is spelled with one dash, and the usage table spells it with two", trimmed)
		}
	}
}

// A TYPO IS ANSWERED WITH THE COMMAND SOMEBODY MEANT, not with the whole book.
// `aforge lgos` used to print `unknown command` followed by every command and
// the entire environment table, so the one line that mattered scrolled off the
// top and the obvious next step was never named.
func TestAMisspelledCommandNamesTheNearestOne(t *testing.T) {
	for _, row := range []struct{ typed, meant string }{
		{"lgos", "logs"},
		{"doo", "do"},
		{"doctro", "doctor"},
		{"maunal", "manual"},
	} {
		said := unknownCommand(row.typed).Error()
		if !strings.Contains(said, "aforge "+row.meant) {
			t.Fatalf("`aforge %s` was answered with %q, and never names `aforge %s`", row.typed, said, row.meant)
		}
		if strings.Contains(said, "AFORGE_DAILY_BUDGET") {
			t.Fatalf("`aforge %s` dumped the environment table over the one line that matters:\n%s", row.typed, said)
		}
		if lines := strings.Count(said, "\n") + 1; lines > 2 {
			t.Fatalf("`aforge %s` answered with %d lines, want the miss and where the rest is:\n%s",
				row.typed, lines, said)
		}
	}
	// AND NOTHING IS SUGGESTED THAT NOBODY MEANT. A confident wrong suggestion
	// is worse than none: `quux` is three edits from `run`, which is not a typo
	// for anything.
	if said := unknownCommand("quux").Error(); strings.Contains(said, "did you mean") {
		t.Fatalf("`aforge quux` was answered with a guess:\n%s", said)
	}
}

// A MISSING QUOTED STRING IS ANSWERED WITH THAT COMMAND'S OWN LINE. `aforge do`
// with nothing after it used to scroll a hundred and twenty-seven lines past
// the reader for the sake of one missing argument.
func TestAMissingGoalShowsTheCommandAndNotTheWholeTable(t *testing.T) {
	said := noGoalGiven("do").Error()
	if !strings.Contains(said, "no goal given") {
		t.Fatalf("the miss is no longer named:\n%s", said)
	}
	if !strings.Contains(said, `aforge do   "<task>"`) {
		t.Fatalf("`aforge do` with no task never shows the shape it wanted:\n%s", said)
	}
	if strings.Contains(said, "AFORGE_DAILY_BUDGET") || strings.Contains(said, "aforge manual") {
		t.Fatalf("`aforge do` with no task printed the whole usage table:\n%s", said)
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

// THE USAGE NAMES EVERY FLAG A PERSON CAN TYPE. `--debug` — the whole debug
// record feature — and `--no-host` existed on four commands and appeared
// nowhere in the one page a headless caller reads.
func TestTheUsageNamesEveryFlagAPersonCanType(t *testing.T) {
	for _, flagName := range []string{"--debug", "--no-host"} {
		if !strings.Contains(usageText, flagName) {
			t.Fatalf("%s is a flag this binary takes and the usage text never mentions it", flagName)
		}
	}
}

// EXEC'S EXIT LADDER IS WRITTEN DOWN. A harness wrapping `aforge exec` could
// not branch on its six exit codes without reading the source, while the same
// page spelled out `do`'s and `run subharness`'s.
func TestExecsExitLadderIsWrittenWhereACallerLooks(t *testing.T) {
	shape := usageForCommand("exec")
	for _, want := range []string{"exit 0", "· 2 ", "· 3 ", "· 4 ", "· 5 ", "· 6 "} {
		if !strings.Contains(shape, want) {
			t.Fatalf("`aforge exec`'s usage does not say what %q means:\n%s", strings.TrimSpace(want), shape)
		}
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

// THE MISSING KEY IS THE MOST COMMON FIRST-RUN FAILURE, and it used to be
// answered five different ways: `do` said the cause and the remedy and then
// repeated itself in machine form on the next line, while `exec`, `plan`,
// `models` and `run` said only `error: OPENROUTER_API_KEY (or OPENAI_API_KEY)
// is required` — the cause with nothing to do about it.
func TestAMissingKeyIsAnsweredOnceWithTheRemedy(t *testing.T) {
	said := plainWords(config.ErrNoAPIKey.Error())
	if !strings.Contains(said, "OPENROUTER_API_KEY") {
		t.Fatalf("the missing-key answer no longer names the variable:\n%s", said)
	}
	if !strings.Contains(said, "run it again") {
		t.Fatalf("the missing-key answer says the cause and never the remedy:\n%s", said)
	}
	if lines := strings.Split(said, "\n"); len(lines) != 2 {
		t.Fatalf("the missing-key answer is %d lines, want the cause and the remedy:\n%s", len(lines), said)
	}
}
