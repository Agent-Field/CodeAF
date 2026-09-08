package session

import (
	"context"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/aforge-v2/internal/verify"
)

// TestACheckAlreadyRunOverAnUnchangedTreeIsNotRunAgain pins C1: an answer over
// the same non-empty tree state is returned whole, without starting a process.
func TestACheckAlreadyRunOverAnUnchangedTreeIsNotRunAgain(t *testing.T) {
	agent, tree := checkMemoryAgent(t, Budget{})
	command, counter := countedCheck(t)

	first, moved := agent.checkNow(context.Background(), tree, command)
	if moved || !first.Ran || !first.Passed {
		t.Fatalf("the first reading was not a clean pass: moved=%v %+v", moved, first)
	}
	second, moved := agent.checkNow(context.Background(), tree, command)
	if moved || second != first {
		t.Fatalf("the remembered answer changed: moved=%v\nfirst: %+v\nsecond: %+v", moved, first, second)
	}
	if executions := checkExecutions(t, counter); executions != 1 {
		t.Fatalf("the unchanged tree started the command %d times, want one", executions)
	}
}

// TestACheckThatMovedTheTreeIsNeverRemembered pins C2: pace survives a writing
// command, but its answer does not.
func TestACheckThatMovedTheTreeIsNeverRemembered(t *testing.T) {
	agent, tree := checkMemoryAgent(t, Budget{})
	command, counter := countedCheck(t)
	command += "; printf x >> source.txt"

	for reading := 1; reading <= 2; reading++ {
		if _, moved := agent.checkNow(context.Background(), tree, command); !moved {
			t.Fatalf("reading %d did not report that the check moved the tree", reading)
		}
	}
	if executions := checkExecutions(t, counter); executions != 2 {
		t.Fatalf("the writing check was remembered after %d executions", executions)
	}
	if _, known := agent.sessionCheckMemories().paceFor(tree, command); !known {
		t.Fatal("the writing check lost its pace with its unusable answer")
	}
}

// TestATreeThatMovedMakesTheCheckRunAgain pins C3: an answer belongs to one
// state, and an empty state never provides a hit.
func TestATreeThatMovedMakesTheCheckRunAgain(t *testing.T) {
	agent, tree := checkMemoryAgent(t, Budget{})
	command, counter := countedCheck(t)
	if _, moved := agent.checkNow(context.Background(), tree, command); moved {
		t.Fatal("a check that wrote outside the tree was said to move it")
	}
	writeFile(t, filepath.Join(tree, "source.txt"), "a materially different tree state\n")
	if _, moved := agent.checkNow(context.Background(), tree, command); moved {
		t.Fatal("the read-only check was said to move the changed tree")
	}
	if executions := checkExecutions(t, counter); executions != 2 {
		t.Fatalf("the changed tree started the command %d times, want two", executions)
	}

	empty := t.TempDir()
	emptyCommand, emptyCounter := countedCheck(t)
	agent.config.Workspace = empty
	agent.checkNow(context.Background(), empty, emptyCommand)
	agent.checkNow(context.Background(), empty, emptyCommand)
	if executions := checkExecutions(t, emptyCounter); executions != 2 {
		t.Fatalf("an empty tree state became a memory hit after %d executions", executions)
	}
}

// TestASessionCheckWindowIsBoundedByWhatIsLeftOfTheWall pins C4 at the one
// sizing door, on the Steward's movable clock.
func TestASessionCheckWindowIsBoundedByWhatIsLeftOfTheWall(t *testing.T) {
	agent, tree := checkMemoryAgent(t, Budget{Wall: 10 * time.Minute})
	steward := agent.steward()
	steward.now = func() time.Time { return steward.started.Add(7 * time.Minute) }

	window, fits := agent.checkWindow(tree, "go test ./...")
	if !fits || window != 3*time.Minute {
		t.Fatalf("the check got window=%s fits=%v, want the three minutes left", window, fits)
	}
}

// TestTheAuditWindowIsBoundedByWhatIsLeftOfTheWall pins C4 for the node audit:
// the wall bounds a checking door, but reading always retains its floor.
func TestTheAuditWindowIsBoundedByWhatIsLeftOfTheWall(t *testing.T) {
	agent, _ := checkMemoryAgent(t, Budget{Wall: 10 * time.Minute})
	steward := agent.steward()
	door := auditDoor{checks: []string{"go test ./..."}}
	steward.now = func() time.Time { return steward.started.Add(7 * time.Minute) }
	if window := agent.auditWindowFor(door); window != 3*time.Minute {
		t.Fatalf("the audit got %s, want the three minutes left", window)
	}
	steward.now = func() time.Time { return steward.started.Add(10 * time.Minute) }
	if window := agent.auditWindowFor(door); window != auditReadingDeadline {
		t.Fatalf("the spent wall gave the audit %s, want the reading floor %s", window, auditReadingDeadline)
	}

	// The existing test seam still wins outright, including below the product's
	// reading floor.
	agent.config.auditWindow = time.Second
	if window := agent.auditWindowFor(door); window != time.Second {
		t.Fatalf("the test seam was bounded to %s", window)
	}
}

// TestACheckThatCannotFitTheWallIsNotStarted pins C5 for both the unknown pace
// floor and the pace retained after an unusable answer.
func TestACheckThatCannotFitTheWallIsNotStarted(t *testing.T) {
	t.Run("unknown pace", func(t *testing.T) {
		agent, tree := checkMemoryAgent(t, Budget{Wall: 10 * time.Minute})
		command, counter := countedCheck(t)
		steward := agent.steward()
		steward.now = func() time.Time {
			return steward.started.Add(10*time.Minute - verify.ShortestUsefulReading/2)
		}

		run, moved := agent.checkNow(context.Background(), tree, command)
		if moved || run.Ran || run.Unread != notEnoughTimeToRun+command {
			t.Fatalf("the unaffordable check came back as %+v, moved=%v", run, moved)
		}
		if executions := checkExecutions(t, counter); executions != 0 {
			t.Fatalf("the unaffordable check started %d times", executions)
		}
	})

	t.Run("remembered pace", func(t *testing.T) {
		agent, tree := checkMemoryAgent(t, Budget{Wall: 10 * time.Minute})
		command, counter := countedCheck(t)
		state := treeStateNow(tree)
		agent.sessionCheckMemories().remember(tree, command, state, CheckRun{Command: command, Ran: true}, 2*time.Minute, true)
		steward := agent.steward()
		steward.now = func() time.Time { return steward.started.Add(8*time.Minute + 30*time.Second) }

		run, _ := agent.checkNow(context.Background(), tree, command)
		if run.Ran || run.Unread == "" {
			t.Fatalf("a check whose remembered pace cannot fit was started: %+v", run)
		}
		if executions := checkExecutions(t, counter); executions != 0 {
			t.Fatalf("the check whose pace cannot fit started %d times", executions)
		}
	})
}

// TestAnUnstartedCheckIsNotCountedRed pins C6: unread is evidence about a run,
// not a failing answer from one.
func TestAnUnstartedCheckIsNotCountedRed(t *testing.T) {
	const command = "tox -e py"
	remains := Remains{
		Acceptance:   "the suite passes",
		Landed:       true,
		Landings:     []Landing{{ID: 1, Title: "fix it", State: TaskDone, Merged: true, Checked: true}},
		Checks:       []CheckRun{{Command: command, Unread: notEnoughTimeToRun + command}},
		BaselineRead: true,
	}
	if red := remains.redChecks(); len(red) != 0 {
		t.Fatalf("an unstarted check was counted red: %v", red)
	}
	for _, unmet := range remains.unmet() {
		if strings.Contains(unmet, command) {
			t.Fatalf("an unstarted check was named as unmet: %q", unmet)
		}
	}
	steward := NewSteward("fix it", Budget{Wall: time.Hour}, nil)
	if decision := steward.Decide(remains); decision.Verb != DecideDone {
		t.Fatalf("an unstarted check stopped done: %+v", decision)
	}
}

// TestWithNoWallEveryCheckRunsAsItAlwaysDid pins C7: a Steward with a money
// ceiling but no wall keeps the old five-minute window, starts the check, and
// still remembers its answer over an unchanged tree.
func TestWithNoWallEveryCheckRunsAsItAlwaysDid(t *testing.T) {
	agent, tree := checkMemoryAgent(t, Budget{})
	command, counter := countedCheck(t)
	steward := agent.steward()
	if steward == nil {
		t.Fatal("the money-only session did not get a Steward")
	}
	if budget := steward.Budget(); budget.USD == 0 || budget.Wall != 0 {
		t.Fatalf("the no-wall session got budget %+v, want money only", budget)
	}
	if window, fits := agent.checkWindow(tree, command); !fits || window != sessionCheckWindow {
		t.Fatalf("the no-wall check got window=%s fits=%v", window, fits)
	}
	first, _ := agent.checkNow(context.Background(), tree, command)
	second, _ := agent.checkNow(context.Background(), tree, command)
	if !first.Ran || !first.Passed || second != first {
		t.Fatalf("the no-wall readings changed: first=%+v second=%+v", first, second)
	}
	if executions := checkExecutions(t, counter); executions != 1 {
		t.Fatalf("tree memory was lost without a wall: %d executions", executions)
	}
}

// TestTheTerminalReadingRunsOneSuiteWhenTheTreeHasNotMoved pins C8: the
// before-reading pays for the command and the terminal reader receives it.
func TestTheTerminalReadingRunsOneSuiteWhenTheTreeHasNotMoved(t *testing.T) {
	agent, tree := checkMemoryAgent(t, Budget{Wall: time.Hour})
	declared, counter := countedSessionCheck(t, tree)
	steward := agent.steward()
	steward.setAcceptance("the work is complete when `" + declared + "` passes")
	checks := agent.sessionChecks()
	if len(checks) != 1 {
		t.Fatalf("the acceptance produced checks %q, want the counted command", checks)
	}

	agent.readBaseline(context.Background(), checks)
	ran, _, _ := agent.terminalAudit(context.Background())
	if len(ran) != 1 || !ran[0].Passed || !ran[0].Ran {
		t.Fatalf("the terminal reading did not receive the green answer: %+v", ran)
	}
	if executions := checkExecutions(t, counter); executions != 1 {
		t.Fatalf("the baseline and terminal readers ran one unchanged suite %d times", executions)
	}
}

// TestNoSessionCheckOpensAWindowTheWallBoundDidNotSize pins C9 without line
// numbers: no WithTimeout in the session check file may take the flat constant.
func TestNoSessionCheckOpensAWindowTheWallBoundDidNotSize(t *testing.T) {
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, "principal_audit.go", nil, 0)
	if err != nil {
		t.Fatalf("parsing principal_audit.go: %v", err)
	}
	found := 0
	ast.Inspect(file, func(node ast.Node) bool {
		call, ok := node.(*ast.CallExpr)
		if !ok || len(call.Args) < 2 {
			return true
		}
		selector, ok := call.Fun.(*ast.SelectorExpr)
		if !ok || selector.Sel.Name != "WithTimeout" {
			return true
		}
		pkg, ok := selector.X.(*ast.Ident)
		if !ok || pkg.Name != "context" {
			return true
		}
		found++
		if duration, ok := call.Args[1].(*ast.Ident); ok && duration.Name == "sessionCheckWindow" {
			t.Errorf("%s: a session check opened the flat window instead of one sized by checkWindow", fset.Position(call.Pos()))
		}
		return true
	})
	if found < 2 {
		t.Fatalf("found %d check-road windows, want the check and whole-photograph windows", found)
	}
}

func checkMemoryAgent(t *testing.T, budget Budget) (*Agent, string) {
	t.Helper()
	tree := t.TempDir()
	writeFile(t, filepath.Join(tree, "source.txt"), "the tree as read\n")
	if !budget.Set() {
		budget = Budget{USD: 1}
	}
	agent, _ := newTestAgent(t, &scriptedCompleter{}, func(config *Config) {
		config.Workspace = tree
		config.Unattended = true
		config.Budget = budget
	})
	return agent, tree
}

func countedCheck(t *testing.T) (string, string) {
	t.Helper()
	counter := filepath.Join(t.TempDir(), "executions")
	return "printf 'x\\n' >> " + shellQuoted(counter), counter
}

func countedSessionCheck(t *testing.T, tree string) (string, string) {
	t.Helper()
	counter := filepath.Join(t.TempDir(), "executions")
	const name = "check.sh"
	path := filepath.Join(tree, name)
	content := "#!/bin/sh\nprintf 'x\\n' >> " + shellQuoted(counter) + "\n"
	if err := os.WriteFile(path, []byte(content), 0o755); err != nil {
		t.Fatalf("writing the counted session check: %v", err)
	}
	return name, counter
}

func checkExecutions(t *testing.T, counter string) int {
	t.Helper()
	content, err := os.ReadFile(counter)
	if os.IsNotExist(err) {
		return 0
	}
	if err != nil {
		t.Fatalf("reading the check counter: %v", err)
	}
	return strings.Count(string(content), "\n")
}

// Cancellation is not an answer about the tree and must not poison a later
// reading of the same command after the caller has a live context again.
func TestACancelledCheckDoesNotPoisonTheNextReading(t *testing.T) {
	agent, tree := checkMemoryAgent(t, Budget{})
	command, counter := countedCheck(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	first, _ := agent.checkNow(ctx, tree, command)
	if first.Ran {
		t.Fatalf("cancelled check was read: %+v", first)
	}
	second, _ := agent.checkNow(context.Background(), tree, command)
	if !second.Ran || !second.Passed {
		t.Fatalf("a fresh reading reused cancellation: %+v", second)
	}
	if count := checkExecutions(t, counter); count != 1 {
		t.Fatalf("executions = %d, want one fresh run", count)
	}
}
