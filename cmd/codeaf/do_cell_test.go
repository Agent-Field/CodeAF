package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/cell"
	"github.com/Agent-Field/codeaf/internal/cellstore"
	"github.com/Agent-Field/codeaf/internal/home"
	"github.com/Agent-Field/codeaf/internal/session"
)

// doInCell runs one scripted errand and answers the record it kept.
func doInCell(t *testing.T, workspace, task, cellRoot string) string {
	t.Helper()
	var stdout, stderr strings.Builder
	err := doErrand(doRequest{task: task, cellRoot: cellRoot, workspace: workspace, asJSON: true, keep: true,
		timeout: 60 * time.Second, stdout: &stdout, stderr: &stderr,
		newBeltCompleter: func(string) session.Completer { return finishingSeat(0) }})
	if err != nil {
		t.Fatalf("run: %v\n%s\n%s", err, stderr.String(), stdout.String())
	}
	lines := strings.Split(strings.TrimSpace(stderr.String()), "\n")
	return strings.TrimPrefix(lines[len(lines)-1], "record kept at ")
}

func sealedTurns(t *testing.T, root string) int {
	t.Helper()
	c, err := cell.OpenAt(root, filepath.Base(root))
	if err != nil {
		t.Fatal(err)
	}
	turns, err := cellstore.Turns(c)
	if err != nil {
		t.Fatal(err)
	}
	return len(turns)
}

func TestRunInCellSealsItsCallsAndContinuesFromThem(t *testing.T) {
	beltRunEnv(t)
	t.Setenv(cell.EnvVar, "1")
	t.Setenv("CODEAF_PLANDB_BIN", beltPlandbDoor(t))
	workspace := beltRepoWorkspace(t)

	first := doInCell(t, workspace, "write out.txt", "")
	root := cellOf(first)
	if root == "" {
		t.Fatalf("the record %s points at no cell", first)
	}
	sealed := sealedTurns(t, root)
	if sealed == 0 {
		t.Fatal("the run's tool calls sealed nothing")
	}

	next, err := continuedBrief(recordID(first), "and one more")
	if err != nil || next.cellRoot != root {
		t.Fatalf("continuedBrief = %+v, %v; want the cell %s", next, err, root)
	}
	if !strings.Contains(next.brief, continueSealedHeader) {
		t.Fatalf("the brief does not carry the sealed state:\n%s", next.brief)
	}
	second := doInCell(t, workspace, next.brief, next.cellRoot)
	if cellOf(second) != root {
		t.Fatalf("the continuation left the cell: %s", cellOf(second))
	}
	if again := sealedTurns(t, root); again <= sealed {
		t.Fatalf("the continuation sealed no turn: %d then %d", sealed, again)
	}
}

func TestRunWithCellsOffMakesNoCell(t *testing.T) {
	beltRunEnv(t)
	t.Setenv(cell.EnvVar, "0")
	t.Setenv("CODEAF_PLANDB_BIN", beltPlandbDoor(t))

	record := doInCell(t, beltRepoWorkspace(t), "write out.txt", "")
	if cellOf(record) != "" {
		t.Fatal("a run with cells off points at a cell")
	}
	if _, err := os.Stat(filepath.Join(record, cellPointer)); !os.IsNotExist(err) {
		t.Fatalf("a pointer was written: %v", err)
	}
	if _, err := os.Stat(home.Join("v3", "projects")); !os.IsNotExist(err) {
		t.Fatalf("a project bucket was made: %v", err)
	}
}

func TestPreCellRecordContinuesFromTheRecordAlone(t *testing.T) {
	home := beltRunEnv(t)
	folder := makeRecord(t, home, "77", "rename the logger")
	prior, err := readPriorRun(folder)
	if err != nil {
		t.Fatal(err)
	}
	if prior.cellRoot != "" || !prior.sealed.empty() {
		t.Fatalf("a pre-cell record reads as sealed: %+v", prior)
	}
	if brief := continuationBrief(prior, ""); strings.Contains(brief, continueSealedHeader) {
		t.Fatalf("a pre-cell brief names a sealed state:\n%s", brief)
	}
}

func TestSealedStateIsInTheBriefAndTheAssignmentSurvivesIt(t *testing.T) {
	prior := priorRun{assignment: "add a flag", ending: "it was stopped",
		sealed: sealedState{turns: 3, head: "abc", unfinished: []string{"- bash (external), started now"}}}
	brief := continuationBrief(prior, "finish it")
	for _, want := range []string{continueSealedHeader, "sealed 3 turns", "never finished", "not run again", "bash (external)"} {
		if !strings.Contains(brief, want) {
			t.Fatalf("brief lacks %q:\n%s", want, brief)
		}
	}
	if got := assignmentOf(brief); got != "add a flag" {
		t.Fatalf("assignmentOf = %q", got)
	}
}

func TestASealedChainIsNotAnEmptySession(t *testing.T) {
	t.Setenv(cell.EnvVar, "1")
	place, err := v3MintSession(t.TempDir(), t.TempDir(), "", false)
	if err != nil {
		t.Fatal(err)
	}
	if !v3EmptySession(place.Dir) {
		t.Fatal("a fresh cell is not empty")
	}
	if err := os.WriteFile(filepath.Join(place.Dir, cellstore.TurnsPath), []byte("{}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if v3EmptySession(place.Dir) {
		t.Fatal("a cell with sealed turns would be reaped as litter")
	}
}
