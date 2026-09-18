package run_test

// THE RUN'S OWN `plandb` IS THE ONLY ONE A WORKER CAN REACH.
//
// A run worker plans with `plandb` — add, task add-dep, context, done — and
// those verbs must hit THIS run's store, not the host's. The run's binary and
// store live beside each other (the session's shim in <storeDir>/bin/plandb,
// the store at <storeDir>/plandb.db), and every command the belt runs for a
// worker carries them: the front of its PATH and the bound store. These tests
// hold the two halves: `command -v plandb` resolves to the run's binary even
// where a host `plandb` sits earlier on the PATH, and `plandb add` from a cwd
// outside the run's tree still writes the run's store. The second asserts on
// the store, never on stdout — what matters is where the row landed.

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/Agent-Field/agentfield/sdk/go/ai"
	"github.com/Agent-Field/codeaf/internal/plandb"
	"github.com/Agent-Field/codeaf/internal/run"
)

// bashArguments wraps one command as the bash tool's arguments, JSON-encoded so
// a command holding quotes still parses as the one JSON object the belt wants.
func bashArguments(t *testing.T, command string) string {
	t.Helper()
	args, err := json.Marshal(struct {
		Command string `json:"command"`
	}{command})
	if err != nil {
		t.Fatal(err)
	}
	return string(args)
}

// realPlandbCLI builds the real plan CLI once for the tests that need one whose
// writes land in a real store, and answers its path. The build is the same road
// internal/session's end-to-end proof takes; a machine that cannot build it
// skips rather than assert on a stub that writes nothing.
var (
	realCLIOnce sync.Once
	realCLIPath string
	realCLIErr  error
)

func realPlandbCLI(t *testing.T) string {
	t.Helper()
	realCLIOnce.Do(func() {
		dir, err := os.MkdirTemp("", "plandb-cli")
		if err != nil {
			realCLIErr = err
			return
		}
		out := filepath.Join(dir, "plandb")
		build := exec.Command("go", "build", "-o", out, "github.com/Agent-Field/codeaf/cmd/plandb")
		if output, err := build.CombinedOutput(); err != nil {
			realCLIErr = fmt.Errorf("go build cmd/plandb: %v\n%s", err, output)
			return
		}
		realCLIPath = out
	})
	if realCLIErr != nil {
		t.Skipf("cannot build the real plandb CLI: %v", realCLIErr)
	}
	return realCLIPath
}

// realPlandbDoor is the CLI behind a codeaf-shaped door: the resolver's override
// names a binary that answers `<bin> plandb ...`, so this wrapper strips the
// leading `plandb` word and execs the real CLI the way `codeaf plandb` does. It
// is what the shim execs when a store-writing test needs a binary whose writes
// land in a real store rather than a stub's silence.
func realPlandbDoor(t *testing.T) string {
	t.Helper()
	cli := realPlandbCLI(t)
	door := filepath.Join(t.TempDir(), "plandb-door")
	script := "#!/bin/sh\nif [ \"$1\" = plandb ]; then shift; fi\nexec " + cli + " \"$@\"\n"
	if err := os.WriteFile(door, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	return door
}

// A HOST `plandb` EARLIER ON THE PATH STILL RESOLVES TO THE RUN'S. The decoy is
// a real executable named `plandb` ahead of everything, and the command is
// chained (`true && …`) so it is not the first word of the line: the run's
// binding must reach the whole command, not only the first simple command.
func TestBashWorkerResolvesTheRunsPlandbOverAHostOne(t *testing.T) {
	t.Setenv("CODEAF_TASK_BELT", "bash")
	t.Setenv("CODEAF_PLANDB_BIN", stubCLI(t))
	decoy := t.TempDir()
	if err := os.WriteFile(filepath.Join(decoy, "plandb"), []byte("#!/bin/sh\necho DECOY\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", decoy+string(os.PathListSeparator)+os.Getenv("PATH"))
	store := runOpenStore(t)
	storeDir := filepath.Dir(store.Path())
	seat := &seat{script: []step{
		func(context.Context, []ai.Message) (*ai.Response, error) {
			return toolReply(`{"command":"true && command -v plandb"}`), nil
		},
		func(context.Context, []ai.Message) (*ai.Response, error) {
			return textReply("checked the plan binary"), nil
		},
	}}
	worker := run.NewBashWorker(store, t.TempDir(), "test/model", seat)

	if _, err := worker.Run(run.WithStepsPerTask(runContext(t), 9), *store.Task(store.RootID())); err != nil {
		t.Fatalf("the worker's run failed: %v", err)
	}

	steps, err := run.Trajectory(storeDir, store.RootID())
	if err != nil {
		t.Fatalf("read the trajectory: %v", err)
	}
	if len(steps) != 1 {
		t.Fatalf("the trajectory reads %d steps, want the one the worker took", len(steps))
	}
	want := filepath.Join(storeDir, "bin", "plandb")
	got := strings.TrimSpace(steps[0].Observation)
	if got != want {
		t.Fatalf("command -v plandb answered %q, want the run's own binary %q", got, want)
	}
	if strings.Contains(got, decoy) {
		t.Fatalf("command -v plandb answered the host's plandb %q, want the run's %q", got, want)
	}
}

// `plandb add` FROM A CWD OUTSIDE THE RUN'S TREE STILL WRITES THE RUN'S STORE.
// The command cd's to a fresh directory that holds no store and no ancestor
// store, so only the bound PLANDB_DB can put the row in the run's plan — and the
// assertion reads the run's store, not the CLI's stdout.
func TestBashWorkerPlandbWritesTheRunsStoreFromAnyCwd(t *testing.T) {
	t.Setenv("CODEAF_TASK_BELT", "bash")
	t.Setenv("CODEAF_PLANDB_BIN", realPlandbDoor(t))
	elsewhere := t.TempDir()
	store := runOpenStore(t)
	storeDir := filepath.Dir(store.Path())
	const title = "A task written from outside the tree"
	command := "cd " + elsewhere + ` && plandb add '` + title + `' --description 'landed from outside'`
	seat := &seat{script: []step{
		func(context.Context, []ai.Message) (*ai.Response, error) {
			return toolReply(bashArguments(t, command)), nil
		},
		func(context.Context, []ai.Message) (*ai.Response, error) {
			return textReply("added the task"), nil
		},
	}}
	worker := run.NewBashWorker(store, t.TempDir(), "test/model", seat)

	if _, err := worker.Run(run.WithStepsPerTask(runContext(t), 9), *store.Task(store.RootID())); err != nil {
		t.Fatalf("the worker's run failed: %v", err)
	}

	// ASSERT ON THE STORE. A fresh handle reads what the CLI wrote; the row is
	// there only when the bound store is the run's own.
	fresh, err := plandb.Open(store.Path(), "", "", "", "")
	if err != nil {
		t.Fatalf("re-open the run's store: %v", err)
	}
	defer fresh.Close()
	found := false
	for _, task := range fresh.Tasks() {
		if task.Title == title {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("the added task %q is not in the run's store at %s; the CLI wrote somewhere else", title, store.Path())
	}
	if _, err := os.Stat(filepath.Join(storeDir, "bin", "plandb")); err != nil {
		t.Fatalf("the run's plandb shim is not where the PATH points: %v", err)
	}
}
