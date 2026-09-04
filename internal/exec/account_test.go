package exec

import (
	"strings"
	"testing"
	"unicode/utf8"
)

// A WORKER'S OWN COMMANDS AND THE FINISHED TREE'S CHECKS ARE TWO DIFFERENT
// FACTS. Both account renderings keep them under their own headings so a reader
// can tell what the leaf ran from what the closing photograph found.
func TestAnAccountNamesWhatTheWorkRanItself(t *testing.T) {
	account := &Account{
		Files:       []FileChange{{Path: "main.go", Change: ChangeChanged, Added: 1}},
		Commands:    []string{"go build ./...", "go test ./internal/shaped/"},
		CommandsRun: 2,
		Checks:      []Check{{Command: "go test ./...", Passed: true}},
	}
	for name, rendered := range map[string]string{
		"report": account.Report(),
		"lines":  strings.Join(account.Lines(), "\n"),
	} {
		commandAt := strings.Index(rendered, "What the work ran itself:")
		checkAt := strings.Index(rendered, "What the work ran to check itself, and what each one found:")
		if commandAt < 0 || checkAt < 0 || commandAt > checkAt {
			t.Errorf("%s does not keep the leaf's commands ahead of the separate check block:\n%s", name, rendered)
		}
		for _, command := range account.Commands {
			if !strings.Contains(rendered, "  "+command) {
				t.Errorf("%s does not name command %q:\n%s", name, command, rendered)
			}
		}
		if !strings.Contains(rendered, "  passed: go test ./...") {
			t.Errorf("%s lost what the closing check found:\n%s", name, rendered)
		}
	}

	cut := &Account{
		Files:       []FileChange{{Path: "main.go", Change: ChangeChanged}},
		Commands:    []string{"newest"},
		CommandsRun: 4,
	}
	if report := cut.Report(); !strings.Contains(report, "and 3 earlier commands, in the run's own record") {
		t.Errorf("the bounded command list does not say what it left out:\n%s", report)
	}
}

// AN ACCOUNT OF CHANGED FILES MUST SAY WHEN NOTHING CHECKED THEM. Silence here
// made a leaf that ran nothing read exactly like one whose check had nothing to
// report, while an account that carries a real check must not claim the absence.
func TestAnAccountWithNoCheckSaysSoRatherThanLeavingItOut(t *testing.T) {
	account := &Account{Files: []FileChange{{Path: "main.go", Change: ChangeChanged, Added: 1}}}
	if report := account.Report(); !strings.Contains(report, noCheckWords) {
		t.Errorf("report omitted the measured absence of a check:\n%s", report)
	}
	if summary := account.Summary(); !strings.Contains(summary, noCheckWords) {
		t.Errorf("summary omitted the measured absence of a check: %q", summary)
	}

	checked := &Account{
		Files:  []FileChange{{Path: "main.go", Change: ChangeChanged, Added: 1}},
		Checks: []Check{{Command: "go test ./...", Passed: true}},
	}
	if report := checked.Report(); strings.Contains(report, noCheckWords) {
		t.Errorf("an account with a check also says none ran:\n%s", report)
	}
	if summary := checked.Summary(); strings.Contains(summary, noCheckWords) {
		t.Errorf("a summary with a check also says none ran: %q", summary)
	}
}

// A COMMAND THAT LOOKS LIKE A TEST IS NEVER EVIDENCE THAT A CHECK PASSED. Its
// output was not parsed by the closing photograph, so Verified must continue to
// answer from Checks alone.
func TestCommandsAreNotChecks(t *testing.T) {
	account := &Account{Commands: []string{"go test ./..."}, CommandsRun: 1}
	if account.Verified() {
		t.Fatal("a shell command made the account read as checked")
	}
}

// AN UNREAD READING IS NOT A MEASURED ABSENCE, AND THE ACCOUNT MUST NOT SPELL
// IT AS ONE.
//
// [Account.rows] reaches its no-check branch whenever Checks is empty, and
// ranChecks empties Checks from four different worlds. In three of them a
// command demonstrably RAN on the finished tree and its answer could not be
// understood — the strategy would not start a second time, the suite failed to
// collect, the command was killed at its ceiling before naming a check
// (photograph.go). Saying "no check was run" of those is a guess wearing the
// words of a measurement, and the same outcome carries Verification.Unread, so
// a reader got both sentences at once: the revision pass renders that one while
// the gate renders these lines.
//
// The fourth world — nothing was ever asked of the tree — is the one the
// exception to the emptiness law was written for, and it keeps it.
func TestAReadingThatCouldNotBeReadSaysThatAndNotThatNothingRan(t *testing.T) {
	files := []FileChange{{Path: "main.go", Change: ChangeChanged, Added: 1}}

	for _, world := range []struct {
		what   string
		unread string
	}{
		{
			what:   "the strategy would not start a second time",
			unread: "the finished tree could not be read: `go test ./...` could not be started a second time",
		},
		{
			what:   "the suite failed to collect",
			unread: "`pytest` ran on the finished tree and its suite failed to collect, so no check of it ran",
		},
		{
			what:   "it was killed at its ceiling",
			unread: "`go test ./...` ran on the finished tree and was killed at its ceiling of 5m without naming a single check",
		},
	} {
		account := &Account{Files: files, Unread: world.unread}

		report := account.Report()
		if strings.Contains(report, noCheckWords) {
			t.Errorf("%s: the report calls an unread reading a measured absence:\n%s", world.what, report)
		}
		if !strings.Contains(report, world.unread) {
			t.Errorf("%s: the report does not say why the tree could not be read:\n%s", world.what, report)
		}

		summary := account.Summary()
		if strings.Contains(summary, noCheckWords) {
			t.Errorf("%s: the summary calls an unread reading a measured absence: %q", world.what, summary)
		}
		if !strings.Contains(summary, world.unread) {
			t.Errorf("%s: the summary does not say why the tree could not be read: %q", world.what, summary)
		}
	}

	// AND THE WORLD THE EXCEPTION WAS WRITTEN FOR IS UNTOUCHED. Nothing was
	// asked of the tree, so there is nothing to explain and the measured
	// absence stands. That world reaches here with an empty Unread even when
	// verify.Reading carried one, because [AccountFor] only carries the
	// sentence when a baseline was Taken — "this project declares no way of
	// checking itself" is the absence, not an explanation of a failure to read.
	nothing := &Account{Files: files}
	if report := nothing.Report(); !strings.Contains(report, noCheckWords) {
		t.Errorf("a run that attempted no reading lost the measured absence:\n%s", report)
	}
	if summary := nothing.Summary(); !strings.Contains(summary, noCheckWords) {
		t.Errorf("a run that attempted no reading lost the measured absence: %q", summary)
	}
}

// A COMMAND IS CUT AT A CHARACTER BOUNDARY, because this list is read by a
// person now. [snip] used to slice bytes, which put a broken rune in front of
// them at exactly ranArgumentBytes.
func TestABoundedCommandIsNotCutThroughACharacter(t *testing.T) {
	// A run of three-byte runes, so any byte-sliced cut lands mid-character.
	long := strings.Repeat("日", 200)
	cut := snip(long, ranArgumentBytes)
	if !utf8.ValidString(cut) {
		t.Fatalf("a clipped command is not valid UTF-8: %q", cut)
	}
	if !strings.HasSuffix(cut, "…") {
		t.Errorf("a clipped command does not say it was clipped: %q", cut)
	}
	if len(cut) > ranArgumentBytes+len("…") {
		t.Errorf("a clipped command outran its ceiling: %d bytes", len(cut))
	}
}
