package session

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/furrow"
)

// A LANDED FORK IS RETIRED ONCE. The checkpoint of a closed conversation keeps
// naming its landed forks forever, so a sweep that reads only the checkpoint
// asks furrow about the same forks on every launch and logs "retired" every
// time, for a fork that was gone days ago. Measured on a laptop, 2026-10-26:
// the same pair, every launch, for days.
//
// The second sweep below is a new process (furrow's answers are forgotten), as
// every launch is, and it must not run furrow for the fork at all.
func TestASweptLandedForkStaysRetired(t *testing.T) {
	installFakeFurrow(t)
	calls := countFurrowCalls(t)
	binary := os.Getenv(furrow.BinaryEnvVar)
	root := filepath.Join(t.TempDir(), "projects")
	created := time.Now()
	dir, tree := newSweptUniverseSession(t, root, "cccc7777cccc7777", dirtyRepo(t), created)
	// The work lands while furrow is away, so the copy is left for a sweep.
	writeFile(t, filepath.Join(tree.dir, "landed.txt"), "landed\n")
	t.Setenv(furrow.BinaryEnvVar, filepath.Join(t.TempDir(), "missing"))
	furrow.Forget()
	if merged, detail, _, _ := tree.comeHome("land it", []string{"landed.txt"}, gitSignature{}); merged != mergeMerged {
		t.Fatalf("landing = %s: %s", merged, detail)
	}
	document, ok := loadTaskCheckpoint((Place{Dir: dir}).Tasks())
	if !ok {
		t.Fatal("no checkpoint")
	}
	document.Nodes[0].State = TaskDone
	document.Nodes[0].Merge = mergeMerged
	writeCheckpoint(t, (Place{Dir: dir}).Tasks(), document)
	t.Setenv(furrow.BinaryEnvVar, binary)
	furrow.Forget()
	// Swept on the day it was last used: a conversation that is kept, not litter.
	sweptAt := created.Add(-30 * 24 * time.Hour)

	var first []string
	SweepPlaces(root, sweptAt, func(line string) { first = append(first, line) })
	if !saidSomethingAbout(first, tree.universe) {
		t.Fatalf("the first sweep did not retire %s: %v", tree.universe, first)
	}
	if _, err := os.Stat(tree.dir); !os.IsNotExist(err) {
		t.Fatalf("the landed copy is still there: %v", err)
	}
	if _, err := os.Stat(dir); err != nil {
		t.Fatalf("a kept conversation was reaped: %v", err)
	}

	furrow.Forget()
	before := calls()
	var second []string
	SweepPlaces(root, sweptAt, func(line string) { second = append(second, line) })
	if saidSomethingAbout(second, tree.universe) {
		t.Fatalf("the second sweep retired %s again: %v", tree.universe, second)
	}
	if ran := calls()[len(before):]; len(ran) != 0 {
		t.Fatalf("the second sweep ran furrow for a retired fork: %q", ran)
	}
}

// countFurrowCalls wraps the installed furrow in a script that writes one line
// per invocation, and answers the lines so far.
func countFurrowCalls(t *testing.T) func() []string {
	t.Helper()
	inner := os.Getenv(furrow.BinaryEnvVar)
	dir := t.TempDir()
	log := filepath.Join(dir, "calls")
	script := filepath.Join(dir, "furrow")
	writeFile(t, script, "#!/bin/sh\necho \"$*\" >> '"+log+"'\nexec '"+inner+"' \"$@\"\n")
	if err := os.Chmod(script, 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv(furrow.BinaryEnvVar, script)
	furrow.Forget()
	return func() []string {
		content, err := os.ReadFile(log)
		if err != nil {
			return nil
		}
		return strings.Split(strings.TrimSpace(string(content)), "\n")
	}
}
