package session

// THE BELT NEVER WAITS FOR FURROW (tools_workspace.go).
//
// A cold launch used to hold its first frame behind `furrow --version` and
// `furrow status`, two to three seconds on a folder furrow was never pointed at.
// The tests here pin the property and not the seconds: they hold furrow's answer
// back with a gate the test owns, so a session that waits for it cannot get past
// the first assertion, and they count the launches rather than time them.
import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/furrow"
)

// installGatedFurrow is a furrow that answers `--version` at once and answers
// `status` with an attached folder only once the test creates the gate file.
// Every launch is appended to the launch log, and the status call's process id
// is written beside it, so the test can count launches and prove one is gone.
func installGatedFurrow(t *testing.T) (launches, gate, pidfile string) {
	t.Helper()
	dir := t.TempDir()
	launches = filepath.Join(dir, "launches")
	gate = filepath.Join(dir, "gate")
	pidfile = filepath.Join(dir, "pid")
	script := filepath.Join(dir, "furrow")
	writeFile(t, script, `#!/bin/sh
echo "$*" >> "`+launches+`"
if [ "$1" = "--version" ]; then echo "furrow 0.1.0"; exit 0; fi
echo $$ > "`+pidfile+`"
while [ ! -e "`+gate+`" ]; do sleep 0.02; done
echo '{"workspace":"'"$2"'","head":"aaaabbbbcccc0001","watcher_running":true}'
`)
	if err := os.Chmod(script, 0o755); err != nil {
		t.Fatalf("chmod the gated furrow: %v", err)
	}
	t.Setenv(furrow.BinaryEnvVar, script)
	furrow.Forget()
	t.Cleanup(furrow.Forget)
	return launches, gate, pidfile
}

func launchCount(t *testing.T, launches string) int {
	t.Helper()
	raw, err := os.ReadFile(launches)
	if os.IsNotExist(err) {
		return 0
	}
	if err != nil {
		t.Fatalf("read the launch log: %v", err)
	}
	return strings.Count(string(raw), "\n")
}

func workspaceVerbs(a *Agent) []string {
	var names []string
	for _, tool := range a.belt() {
		if strings.HasPrefix(tool.Name, "workspace_") {
			names = append(names, tool.Name)
		}
	}
	return names
}

// untilFurrow polls a condition. The deadline only turns a hang into a failure; no
// test asserts how long anything took.
func untilFurrow(t *testing.T, what string, ok func() bool) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for !ok() {
		if time.Now().After(deadline) {
			t.Fatalf("never happened: %s", what)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// While furrow has not answered, building the session and assembling its belt
// both return, and the belt carries no workspace verb. The status call is parked
// on the gate and stays parked, so a session that asks furrow itself cannot
// return before furrow's own five-second bound ends the wait; the detector below
// is shorter than that bound and exists only to turn "waited" into a failure.
func TestBuildingASessionDoesNotWaitForFurrow(t *testing.T) {
	launches, gate, _ := installGatedFurrow(t)
	t.Cleanup(func() { _ = os.WriteFile(gate, nil, 0o644) })

	type built struct {
		agent *Agent
		verbs []string
		err   error
	}
	done := make(chan built, 1)
	go func() {
		agent, err := newAgent(Config{Workspace: t.TempDir(), Model: "test/model", System: "SYSTEM"}, &scriptedCompleter{})
		if err != nil {
			done <- built{err: err}
			return
		}
		done <- built{agent: agent, verbs: workspaceVerbs(agent)}
	}()
	select {
	case got := <-done:
		if got.err != nil {
			t.Fatalf("newAgent: %v", got.err)
		}
		t.Cleanup(func() { _ = got.agent.Close() })
		if len(got.verbs) != 0 {
			t.Fatalf("the belt carried %v before furrow had answered; want none", got.verbs)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("building the session waited on furrow")
	}
	untilFurrow(t, "furrow being asked", func() bool { return launchCount(t, launches) >= 2 })
}

// On a folder furrow reports as watched, the four verbs do reach the belt, with
// the description and schema the model is shown.
func TestWatchedFolderVerbsReachTheBelt(t *testing.T) {
	_, gate, _ := installGatedFurrow(t)
	if err := os.WriteFile(gate, nil, 0o644); err != nil {
		t.Fatalf("open the gate: %v", err)
	}
	agent, _ := newTestAgent(t, &scriptedCompleter{}, nil)
	untilFurrow(t, "the belt carrying the workspace verbs", func() bool {
		return len(workspaceVerbs(agent)) == len(furrow.Names())
	})
	for _, tool := range agent.belt() {
		if strings.HasPrefix(tool.Name, "workspace_") && (tool.Description == "" || len(tool.Schema) == 0 || tool.Execute == nil) {
			t.Errorf("%s reached the belt without a description, schema or hand", tool.Name)
		}
	}
}

// One session, however many belts it assembles, asks furrow its two questions
// once.
func TestFurrowIsAskedOncePerSession(t *testing.T) {
	launches, gate, _ := installGatedFurrow(t)
	if err := os.WriteFile(gate, nil, 0o644); err != nil {
		t.Fatalf("open the gate: %v", err)
	}
	agent, _ := newTestAgent(t, &scriptedCompleter{}, nil)
	untilFurrow(t, "the belt carrying the workspace verbs", func() bool { return len(workspaceVerbs(agent)) > 0 })
	for i := 0; i < 5; i++ {
		_ = agent.belt()
	}
	if got := launchCount(t, launches); got != 2 {
		t.Fatalf("furrow was launched %d times for one session; want 2 (--version, status)", got)
	}
}

// Close cuts a question furrow is still answering and waits for it: when Close
// returns the process is gone.
func TestClosingJoinsAFurrowThatIsStillAnswering(t *testing.T) {
	_, _, pidfile := installGatedFurrow(t)
	agent, _ := newTestAgent(t, &scriptedCompleter{}, nil)
	var pid int
	untilFurrow(t, "furrow's status call starting", func() bool {
		raw, err := os.ReadFile(pidfile)
		if err != nil {
			return false
		}
		_, err = fmt.Sscan(string(raw), &pid)
		return err == nil && pid > 0
	})
	if err := agent.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}
	if err := syscall.Kill(pid, 0); err == nil {
		t.Fatalf("Close returned with furrow's process %d still alive", pid)
	}
}
