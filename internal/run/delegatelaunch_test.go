//go:build !windows

package run_test

import (
	"bufio"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/delegate"
	"github.com/Agent-Field/codeaf/internal/plandb"
	"github.com/Agent-Field/codeaf/internal/processgroup"
	"github.com/Agent-Field/codeaf/internal/run"
)

// rawLaunches is the launch every complete line of one log carries, in order,
// read as bytes on disk and not through a reader that could drop a field.
func rawLaunches(t *testing.T, path string) (kinds, launches []string) {
	t.Helper()
	file, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 64<<10), 4<<20)
	for scanner.Scan() {
		var line struct {
			Kind   string `json:"kind"`
			Launch string `json:"launch"`
		}
		if err := json.Unmarshal(scanner.Bytes(), &line); err != nil {
			t.Fatalf("a line does not parse: %s", scanner.Bytes())
		}
		kinds, launches = append(kinds, line.Kind), append(launches, line.Launch)
	}
	return kinds, launches
}

// ONE LAUNCH, ONE IDENTITY, STAMPED BY THE PRODUCER ON EVERYTHING IT KEEPS. The
// worker draws the launch before the process exists, writes the launch's start
// line before it, and stamps the record, every action line and every turn with
// the same id — the one the process's descendants are marked with. A second
// launch of the same task draws another, and the first launch's lines keep
// theirs: nothing ties a line to a process but this.
func TestEveryLaunchStampsItsRecordLinesAndTurnsWithOneIdentity(t *testing.T) {
	store := runOpenStore(t)
	taskDir := plandb.TaskDir(filepath.Dir(store.Path()), store.RootID())
	program, setup, _, _ := realChild(t, 0.01, "1")
	envFile := filepath.Join(t.TempDir(), "env")
	t.Setenv("FAKE_ENV", envFile)
	var seen []string
	for range 2 {
		if _, err := run.NewDelegateWorker(store, t.TempDir(), program, setup, 2.5, 0).Run(runContext(t), *store.Task(store.RootID())); err != nil {
			stderr, _ := os.ReadFile(filepath.Join(taskDir, "delegate-stderr.log"))
			t.Fatalf("run: %v\n%s", err, stderr)
		}
		record, ok := delegate.ReadProgram(taskDir)
		if !ok || !delegate.ValidLaunch(record.Launch) {
			t.Fatalf("record = %+v, want a launch identity", record)
		}
		environ, _ := os.ReadFile(envFile)
		if !strings.Contains("\n"+string(environ)+"\n", "\n"+processgroup.RunMarkerEnv+"="+record.Launch+"\n") {
			t.Fatalf("the process was not marked with its launch %s", record.Launch)
		}
		seen = append(seen, record.Launch)
	}
	if seen[0] == seen[1] {
		t.Fatalf("two launches share one identity %s", seen[0])
	}
	kinds, launches := rawLaunches(t, filepath.Join(taskDir, delegate.ActionsFile))
	// Each launch opens with its own start line, and every line after it until
	// the next start is that launch's.
	current := ""
	starts := 0
	for i, kind := range kinds {
		if kind == delegate.ActionStart {
			if launches[i] != seen[starts] {
				t.Fatalf("start %d carries %s, want %s", starts, launches[i], seen[starts])
			}
			current = launches[i]
			starts++
			continue
		}
		if current == "" || launches[i] != current {
			t.Fatalf("line %d (%s) carries %q under launch %q", i, kind, launches[i], current)
		}
	}
	if starts != 2 || kinds[len(kinds)-1] != delegate.ActionEnd {
		t.Fatalf("kinds = %v", kinds)
	}
	_, turnLaunches := rawLaunches(t, filepath.Join(taskDir, delegate.ConversationFile))
	if len(turnLaunches) == 0 || turnLaunches[0] != seen[0] || turnLaunches[len(turnLaunches)-1] != seen[1] {
		t.Fatalf("turns carry %v, want the first launch's then the second's (%v)", turnLaunches, seen)
	}
	for _, launch := range turnLaunches {
		if launch != seen[0] && launch != seen[1] {
			t.Fatalf("a turn carries %q", launch)
		}
	}
}

// A LAUNCH WHOSE PROCESS NEVER STARTED still opens its lines and leaves a
// record with its identity, so a reader knows this launch wrote nothing —
// rather than reading the previous launch's lines as this one's.
func TestALaunchThatFailedToSpawnStillOwnsItsIdentity(t *testing.T) {
	store := runOpenStore(t)
	taskDir := plandb.TaskDir(filepath.Dir(store.Path()), store.RootID())
	old := delegate.NewLaunch()
	if err := delegate.AppendAction(taskDir, delegate.EndAction(time.Date(2026, 10, 16, 9, 30, 0, 0, time.UTC), delegate.Terminal{Status: delegate.StatusPass, Message: "the old run's ending"})); err != nil {
		t.Fatal(err)
	}
	if err := delegate.WriteProgram(taskDir, delegate.ProgramRecord{Name: "fake", Launch: old}); err != nil {
		t.Fatal(err)
	}
	missing := filepath.Join(t.TempDir(), "no-such-program")
	_, err := run.NewDelegateWorker(store, t.TempDir(), delegate.Delegate{Name: "fake", Default: "run"}, run.DelegateSetup{Exe: missing}, 1, 0).Run(runContext(t), *store.Task(store.RootID()))
	if err == nil {
		t.Fatal("a program that cannot be started ran")
	}
	record, ok := delegate.ReadProgram(taskDir)
	if !ok || !delegate.ValidLaunch(record.Launch) || record.Launch == old || record.StartedAt.IsZero() || record.EndedAt.IsZero() {
		t.Fatalf("record = %+v", record)
	}
	kinds, launches := rawLaunches(t, filepath.Join(taskDir, delegate.ActionsFile))
	if len(kinds) != 2 || kinds[1] != delegate.ActionStart || launches[1] != record.Launch {
		t.Fatalf("lines = %v %v", kinds, launches)
	}
}
