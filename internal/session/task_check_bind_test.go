package session

// A DECLARED CHECK IS RUN AGAINST THE TASK'S OWN COPY.
//
// This is issue #886, and it is #566's law reaching the one field of the
// contract it never covered. A parent standing in the person's checkout writes
// that checkout's absolute paths into `checks` — the shape prompts/system.md
// asks for — and the checker ran them in a copy: the working directory was the
// copy, but an absolute argument is not a working-directory question, so
// `grep -q rewritten /person/folder/report.txt` read the untouched original,
// answered red, and correct work landed `your call · nobody could check it`.
//
// The commands below are asked of the door every road builds ([auditDoorFor]),
// and then RUN — in the copy the work happened in, in a second copy standing in
// for the clean restore the checker is really put in, and on the base the task
// was cut from — because the whole claim is about what a check ANSWERS.

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/Agent-Field/agentfield/sdk/go/ai"
)

// theReport is the one file this fixture is about, spelled once.
const theReport = "report.txt"

// TestADeclaredCheckIsBoundToTheTaskOwnCopy is the replication in #886. The
// proposal's check names the SOURCE checkout absolutely; the worker rewrites the
// file inside its own copy; the two directories then disagree, and which one the
// check reads is the whole question.
func TestADeclaredCheckIsBoundToTheTaskOwnCopy(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	source := newTestRepo(t)
	sourceReport := filepath.Join(source, theReport)
	writeFile(t, sourceReport, "old\n")
	mustGit(t, source, "add", "-A")
	mustGit(t, source, "-c", "user.name=t", "-c", "user.email=t@t", "commit", "-m", "the report")

	place := Place{Dir: t.TempDir(), Workspace: source, Owned: true}
	copyDir := filepath.Join(place.Trees(), "1")

	// The check the parent writes: one command, over the absolute source path.
	declared := "grep -q rewritten " + sourceReport
	proposal, err := json.Marshal(taskArguments{
		Title:       "Rewrite the report",
		Summary:     "two lines the person reads",
		Brief:       "rewrite " + sourceReport + "\n" + taskBriefMark,
		Deliverable: sourceReport,
		Acceptance:  sourceReport + " says rewritten",
		Checks:      []string{declared},
		Ground:      source,
		MaxSteps:    20,
	})
	if err != nil {
		t.Fatal(err)
	}

	// THE CLEAN RESTORE THE CHECKER IS REALLY PUT IN. A landed task's worktree is
	// removed, so what the worker left is taken while it is still standing in it
	// — which is also the honest fixture: the audit never runs its checks in the
	// worker's own tree (task_audit.go's [auditGroundFor]).
	restore := t.TempDir()
	var wrote sync.Once
	completer := &routedCompleter{
		parent: []step{
			func(context.Context, []ai.Message) (*ai.Response, error) {
				return toolResponse("call-task", "propose_task", string(proposal)), nil
			},
			finalText("handed off"),
		},
		child: []step{
			func(context.Context, []ai.Message) (*ai.Response, error) {
				wrote.Do(func() {
					writeFile(t, filepath.Join(copyDir, theReport), "rewritten\n")
					writeFile(t, filepath.Join(restore, theReport), readFile(t, filepath.Join(copyDir, theReport)))
				})
				return textResponse("the report says rewritten"), nil
			},
		},
	}

	agent, _ := newTestAgent(t, completer, func(config *Config) {
		config.Workspace = source
		config.Place = place
		config.AskConsent = false
		config.TaskAutoApproveSeconds = 0
		config.TaskAudit = false
		config.TaskRepairRounds = 0
	})
	collect(t, mustSubmit(t, agent, "make the report say rewritten"))

	node := agent.graph().node(1)
	if node == nil {
		t.Fatal("no node was admitted")
	}
	waitDoneNode(t, node)
	if got := readFile(t, filepath.Join(restore, theReport)); got != "rewritten\n" {
		t.Fatalf("the worker left %q in its copy, want the rewritten report", got)
	}

	door := agent.doorAfterTheFact(node, restore)
	if len(door.checks) != 1 {
		t.Fatalf("the door holds %q, want the one check the contract declared", door.checks)
	}
	bound := door.checks[0]

	// (1) THE CHECK NO LONGER NAMES THE PERSON'S OWN FOLDER. That address is the
	// whole defect: it is a directory the task never wrote in.
	if strings.Contains(bound, source) {
		t.Fatalf("the door's check is %q, which still names the person's checkout %q", bound, source)
	}

	// (2) AND IT ANSWERS GREEN WHERE THE WORK IS. The restore holds `rewritten`;
	// the check the checker would type reads it.
	if run := runOneCheck(context.Background(), restore, bound); !run.Ran || !run.Passed {
		t.Fatalf("the bound check %q did not pass on what the work would ship (ran=%v, tail=%q)", bound, run.Ran, firstLines(run.Tail, 2))
	}

	// (3) AND THE COMMAND AS THE CONTRACT WROTE IT STILL DOES NOT, which is the
	// defect this test exists for: the working directory was never the question.
	if run := runOneCheck(context.Background(), restore, declared); run.Passed {
		t.Fatalf("the declared check %q passed while standing in the copy — the fixture is not reproducing #886", declared)
	}

	// (4) AND THE SAME ONE COMMAND IS TRUE IN EVERY COPY. The before-reading
	// stands on the commit the work was cut from (task_baseline.go), so the check
	// has to answer about whichever copy it is run in rather than about one
	// directory named once — which is what makes "this work turned it red" a
	// finding somebody earned.
	base := t.TempDir()
	writeFile(t, filepath.Join(base, theReport), "old\n")
	if run := runOneCheck(context.Background(), base, bound); run.Passed {
		t.Fatalf("the bound check %q passed on the base the task was cut from, where the file still says old", bound)
	}
}

// doorAfterTheFact is the door the audit builds, asked from a test. It is the
// production road — [Agent.checkCopy] then [auditDoorFor] — and not a second
// reading of the contract, so a check that reaches the checker any other way
// would not be covered by what this asserts.
func (a *Agent) doorAfterTheFact(node *TaskNode, dir string) auditDoor {
	return auditDoorFor(node, a.checkCopy(node, dir))
}

// TestAnAddressOutsideTheGroundIsLeftInTheCheckAsWritten is the other half of
// the law: what a check names outside the folder the work is about is NOT the
// task's to rewrite, and a relative check was already right in every copy.
func TestChecksBindOnlyWhatTheGroundHolds(t *testing.T) {
	ground := t.TempDir()
	// The copies live where a session keeps them, so that the sibling-tree rule
	// below has a real trees folder to be about ([Place.Trees]).
	trees := filepath.Join(t.TempDir(), "trees")
	copyDir := filepath.Join(trees, "1")
	elsewhere := t.TempDir()
	own := newTaskCopyOf(ground, copyDir, "", trees)

	for _, one := range []struct {
		name    string
		command string
		want    string
	}{
		{
			name:    "an address under the ground names the copy the check runs in",
			command: "grep -q rewritten " + filepath.Join(ground, theReport),
			want:    "grep -q rewritten ./" + theReport,
		},
		{
			name:    "the ground itself is the copy the check runs in",
			command: "test -d " + ground,
			want:    "test -d .",
		},
		{
			name:    "an address outside the ground stands as it was written",
			command: "grep -q rewritten " + filepath.Join(elsewhere, theReport),
			want:    "grep -q rewritten " + filepath.Join(elsewhere, theReport),
		},
		{
			name:    "a relative check is already true in every copy",
			command: "go test ./...",
			want:    "go test ./...",
		},
		{
			name:    "another tree of this conversation is this copy too (#839)",
			command: "grep -q rewritten " + filepath.Join(trees, "7", theReport),
			want:    "grep -q rewritten ./" + theReport,
		},
	} {
		t.Run(one.name, func(t *testing.T) {
			if got := own.bindCommand(one.command); got != one.want {
				t.Fatalf("the check runs as %q, want %q", got, one.want)
			}
		})
	}

	// AND A CHECKER STANDING ON THE GROUND ITSELF BINDS NOTHING. The session's
	// own reading after a task has landed is that case: the work is home, so the
	// address the contract wrote names the place that now holds it.
	home := "grep -q rewritten " + filepath.Join(ground, theReport)
	if got := standingOn(ground).bindCommand(home); got != home {
		t.Fatalf("a check run on the ground itself was rewritten to %q", got)
	}
}

// TestAGroundCheckThatNamesItsOwnScriptOpensTheDoor is the silent half of the
// same defect: a check whose FIRST WORD was an absolute path into the person's
// checkout named no file under the checker's feet, so it was not runnable there
// and was dropped from the door without a word to anybody.
func TestAGroundCheckThatNamesItsOwnScriptOpensTheDoor(t *testing.T) {
	ground := t.TempDir()
	copyDir := t.TempDir()
	writeFile(t, filepath.Join(copyDir, "run_tests.sh"), "#!/bin/sh\nexit 0\n")
	if err := os.Chmod(filepath.Join(copyDir, "run_tests.sh"), 0o755); err != nil {
		t.Fatal(err)
	}
	own := newTaskCopyOf(ground, copyDir, "", "")

	door := auditDoorFor(declaringNode(filepath.Join(ground, "run_tests.sh")), own)
	if len(door.checks) != 1 {
		t.Fatalf("the door holds %q, want the work's own script", door.checks)
	}
	if len(door.files) != 1 || door.files[0].path != filepath.Join(copyDir, "run_tests.sh") {
		t.Fatalf("the door's file checks are %+v, want the script inside the copy", door.files)
	}
}
