package session

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func completedRemains(command string, after CheckRun) Remains {
	return Remains{
		Acceptance:   "finish the requested behavior and run `" + command + "`",
		Landed:       true,
		Landings:     []Landing{{ID: 1, Title: "implement the request", State: TaskDone, Merged: true}},
		Checks:       []CheckRun{after},
		BaselineRead: true,
	}
}

func TestANewNamedFailureCannotHideInsideTheSameRedCommand(t *testing.T) {
	command := "python -m pytest -q"
	remains := completedRemains(command, CheckRun{Command: command, Ran: true, Failures: []string{"tests/test_api.py::test_b"}})
	remains.WasFailing = []string{command}
	remains.WasFailingTests = map[string][]string{command: {"tests/test_api.py::test_a"}}

	decision := budgetLeft(t).Decide(remains)
	if decision.Verb != DecideCarryOn {
		t.Fatalf("red A before and red B after was treated as the same failure: %+v", decision)
	}
	if !strings.Contains(decision.Brief, "test_b") {
		t.Fatalf("repair brief omitted the new failure identity:\n%s", decision.Brief)
	}
}

func TestTheSameNamedFailureRemainsBaselineRed(t *testing.T) {
	command := "python -m pytest -q"
	failure := "tests/test_api.py::test_a"
	remains := completedRemains(command, CheckRun{Command: command, Ran: true, Failures: []string{failure}})
	remains.WasFailing = []string{command}
	remains.WasFailingTests = map[string][]string{command: {failure}}

	if decision := budgetLeft(t).Decide(remains); decision.Verb != DecideDone {
		t.Fatalf("an unchanged baseline failure became this work's debt: %+v", decision)
	}
}

func TestAnUnreadBaselineDoesNotInventResponsibility(t *testing.T) {
	command := "python -m pytest -q"
	remains := completedRemains(command, CheckRun{Command: command, Ran: true, Failures: []string{"tests/test_api.py::test_b"}})
	remains.Unread = []string{command}

	if decision := budgetLeft(t).Decide(remains); decision.Verb != DecideDone {
		t.Fatalf("an unread baseline was guessed green: %+v", decision)
	}
}

func TestRequestedAndNonTestValidationFailuresStillBlockCompletion(t *testing.T) {
	for _, test := range []struct {
		name string
		run  CheckRun
	}{
		{name: "requested behavior test", run: CheckRun{Command: "python -m pytest tests/test_requested.py", Ran: true, Failures: []string{"tests/test_requested.py::test_contract"}}},
		{name: "arbitrary artifact validation", run: CheckRun{Command: "./validate-artifact release.tar", Ran: true, Tail: "manifest mismatch"}},
	} {
		t.Run(test.name, func(t *testing.T) {
			decision := budgetLeft(t).Decide(completedRemains(test.run.Command, test.run))
			if decision.Verb != DecideCarryOn {
				t.Fatalf("a validation that turned red did not block completion: %+v", decision)
			}
		})
	}
}

func TestTaskBaselineAlsoComparesFailuresInsideARedCommand(t *testing.T) {
	command := "python -m pytest -q"
	before := checkPhotograph{red: []string{command}, failures: map[string][]string{command: {"tests/test_api.py::test_a"}}, read: true}
	after := checkPhotograph{red: []string{command}, failures: map[string][]string{command: {"tests/test_api.py::test_b"}}, read: true}
	ground := checkGround{before: before, after: after}
	if got := ground.turnedRed(); len(got) != 1 || got[0] != command {
		t.Fatalf("task baseline hid a new failure inside an old red command: %#v", got)
	}
	ground.after.failures[command] = []string{"tests/test_api.py::test_a"}
	if got := ground.turnedRed(); len(got) != 0 {
		t.Fatalf("task baseline blamed unchanged old red on this work: %#v", got)
	}
}

func TestCheckReadingsExtractChangedFailureIdentities(t *testing.T) {
	dir := t.TempDir()
	check := "sh check.sh"
	writeFailure := func(name string) {
		t.Helper()
		body := "#!/bin/sh\nprintf 'FAILED tests/test_api.py::" + name + "\\n'\nexit 1\n"
		if err := os.WriteFile(filepath.Join(dir, "check.sh"), []byte(body), 0o755); err != nil {
			t.Fatal(err)
		}
	}

	writeFailure("test_a")
	before := readChecksOn(context.Background(), dir, []string{check})
	writeFailure("test_b")
	after := readChecksOn(context.Background(), dir, []string{check})
	ground := checkGround{before: before, after: after}

	if got := ground.turnedRed(); len(got) != 1 || got[0] != check {
		t.Fatalf("executed red A before and red B after was hidden: %#v (before %#v, after %#v)", got, before.failures, after.failures)
	}
}
