package exec

import (
	"strings"
	"testing"
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
