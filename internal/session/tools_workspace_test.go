//go:build unix

package session

// THE BELT NEVER WAITS FOR FURROW (tools_workspace.go).
//
// A cold launch used to hold its first frame behind `furrow --version` and
// `furrow status`, two to three seconds on a folder furrow was never pointed at.
// The tests here pin the property and not the seconds. Furrow is a script whose
// `status` call announces that it has started on one named pipe and then parks
// on another until the test lets it go, so "the session did not wait" is the
// observable fact that the build, or a Close, came back while that process was
// still alive. Nothing here measures how long anything took; the timeouts are
// safety nets that turn a hang into a loud failure.
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

// safety is how long a test waits for something that must happen before it
// gives up and fails. It is never an assertion about speed.
const furrowTestSafety = 60 * time.Second

type gatedFurrow struct {
	launches, pidfile, ready, gate string
}

// installGatedFurrow is a furrow that answers `--version` at once and answers
// `status` with an attached folder only once the test releases the gate. Every
// launch is appended to the launch log.
func installGatedFurrow(t *testing.T) *gatedFurrow {
	t.Helper()
	dir := t.TempDir()
	g := &gatedFurrow{
		launches: filepath.Join(dir, "launches"),
		pidfile:  filepath.Join(dir, "pid"),
		ready:    filepath.Join(dir, "ready"),
		gate:     filepath.Join(dir, "gate"),
	}
	for _, fifo := range []string{g.ready, g.gate} {
		if err := syscall.Mkfifo(fifo, 0o600); err != nil {
			t.Fatalf("make the pipe %s: %v", fifo, err)
		}
	}
	script := filepath.Join(dir, "furrow")
	writeFile(t, script, `#!/bin/sh
echo "$*" >> "`+g.launches+`"
if [ "$1" = "--version" ]; then echo "furrow 0.1.0"; exit 0; fi
echo $$ > "`+g.pidfile+`"
echo started > "`+g.ready+`"
cat "`+g.gate+`" > /dev/null
echo '{"workspace":"'"$2"'","head":"aaaabbbbcccc0001","watcher_running":true}'
`)
	if err := os.Chmod(script, 0o755); err != nil {
		t.Fatalf("chmod the gated furrow: %v", err)
	}
	t.Setenv(furrow.BinaryEnvVar, script)
	furrow.Forget()
	t.Cleanup(furrow.Forget)
	// A test that fails with furrow parked must not leave a process behind: a
	// non-blocking open succeeds only if the script is already reading.
	t.Cleanup(func() {
		if f, err := os.OpenFile(g.gate, os.O_WRONLY|syscall.O_NONBLOCK, 0); err == nil {
			_ = f.Close()
		}
	})
	return g
}

// started blocks until furrow's status call is running and answers its pid.
func (g *gatedFurrow) started(t *testing.T) int {
	t.Helper()
	done := make(chan struct{})
	go func() {
		_, _ = os.ReadFile(g.ready)
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(furrowTestSafety):
		t.Fatal("furrow's status call never started")
	}
	raw, err := os.ReadFile(g.pidfile)
	if err != nil {
		t.Fatalf("read furrow's pid: %v", err)
	}
	var pid int
	if _, err := fmt.Sscan(string(raw), &pid); err != nil {
		t.Fatalf("furrow's pid %q: %v", raw, err)
	}
	return pid
}

// release lets the parked status call answer.
func (g *gatedFurrow) release(t *testing.T) {
	t.Helper()
	done := make(chan struct{})
	go func() {
		_ = os.WriteFile(g.gate, nil, 0o600)
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(furrowTestSafety):
		t.Fatal("furrow was not waiting at the gate")
	}
}

func (g *gatedFurrow) launchCount(t *testing.T) int {
	t.Helper()
	raw, err := os.ReadFile(g.launches)
	if os.IsNotExist(err) {
		return 0
	}
	if err != nil {
		t.Fatalf("read the launch log: %v", err)
	}
	return strings.Count(string(raw), "\n")
}

func processAlive(pid int) bool { return syscall.Kill(pid, 0) == nil }

func workspaceVerbs(tools []string) []string {
	var verbs []string
	for _, name := range tools {
		if strings.HasPrefix(name, "workspace_") {
			verbs = append(verbs, name)
		}
	}
	return verbs
}

// carried is what the model is actually shown: the belt as the turn reads it.
func carried(a *Agent) []string {
	var names []string
	for _, tool := range a.beltTools() {
		names = append(names, tool.Name)
	}
	return names
}

func furrowSession(t *testing.T, workspace string) *Agent {
	t.Helper()
	agent, err := newAgent(Config{Workspace: workspace, Model: "test/model", System: "SYSTEM"}, &scriptedCompleter{})
	if err != nil {
		t.Fatalf("newAgent: %v", err)
	}
	t.Cleanup(func() { _ = agent.Close() })
	return agent
}

// While furrow has not answered, building the session returns, and the belt
// carries no workspace verb. The status call is parked on the gate for the whole
// test, so a session that asked furrow itself could only come back after furrow
// was killed; here it comes back while furrow is still alive.
func TestBuildingASessionDoesNotWaitForFurrow(t *testing.T) {
	g := installGatedFurrow(t)
	built := make(chan *Agent, 1)
	go func() {
		agent, err := newAgent(Config{Workspace: t.TempDir(), Model: "test/model", System: "SYSTEM"}, &scriptedCompleter{})
		if err != nil {
			t.Errorf("newAgent: %v", err)
		}
		built <- agent
	}()
	var agent *Agent
	select {
	case agent = <-built:
	case <-time.After(furrowTestSafety):
		t.Fatal("building the session waited on furrow")
	}
	if agent == nil {
		return
	}
	t.Cleanup(func() { _ = agent.Close() })
	pid := g.started(t)
	if !processAlive(pid) {
		t.Fatal("furrow's status call was already over when the session came back; it should still be parked")
	}
	if verbs := workspaceVerbs(carried(agent)); len(verbs) != 0 {
		t.Fatalf("the belt carried %v before furrow had answered; want none", verbs)
	}
	g.release(t)
}

// On a folder furrow reports as watched, the four verbs reach the belt THE MODEL
// READS, even though the session was built before furrow answered.
func TestWatchedFolderVerbsReachTheModel(t *testing.T) {
	g := installGatedFurrow(t)
	agent := furrowSession(t, t.TempDir())
	g.started(t)
	g.release(t)
	answered(t, agent)
	if got := len(workspaceVerbs(carried(agent))); got != len(furrow.Names()) {
		t.Fatalf("the belt carries %d workspace verbs; want %d", got, len(furrow.Names()))
	}
	for _, tool := range agent.beltTools() {
		if strings.HasPrefix(tool.Name, "workspace_") && (tool.Description == "" || len(tool.Schema) == 0 || tool.Execute == nil) {
			t.Errorf("%s reached the belt without a description, schema or hand", tool.Name)
		}
	}
	// The request's definitions travel with the tools, one for one.
	if len(agent.beltDefinitions()) != len(agent.beltTools()) {
		t.Fatalf("the request carries %d definitions for %d tools", len(agent.beltDefinitions()), len(agent.beltTools()))
	}
}

// answered waits for the background question to end, which is also the moment
// the verbs are armed.
func answered(t *testing.T, a *Agent) {
	t.Helper()
	select {
	case <-a.furrow.idle():
	case <-time.After(furrowTestSafety):
		t.Fatal("furrow's question never ended")
	}
}

// One session, however many belts it assembles, asks furrow its two questions
// once.
func TestFurrowIsAskedOncePerSession(t *testing.T) {
	g := installGatedFurrow(t)
	agent := furrowSession(t, t.TempDir())
	g.started(t)
	g.release(t)
	answered(t, agent)
	for i := 0; i < 5; i++ {
		_ = agent.belt()
	}
	if got := g.launchCount(t); got != 2 {
		t.Fatalf("furrow was launched %d times for one session; want 2 (--version, status)", got)
	}
}

// Close cuts a question furrow is still answering and waits for it: when Close
// returns the process is gone.
func TestClosingJoinsAFurrowThatIsStillAnswering(t *testing.T) {
	g := installGatedFurrow(t)
	agent := furrowSession(t, t.TempDir())
	pid := g.started(t)
	if err := agent.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}
	if processAlive(pid) {
		t.Fatalf("Close returned with furrow's process %d still alive", pid)
	}
}

// Two sessions on one folder share one question. The second one closing must not
// wait behind the first one's furrow: it is not the one asking.
func TestClosingOneSessionDoesNotWaitBehindAnothersFurrow(t *testing.T) {
	g := installGatedFurrow(t)
	workspace := t.TempDir()
	first := furrowSession(t, workspace)
	pid := g.started(t)
	second := furrowSession(t, workspace)
	closed := make(chan error, 1)
	go func() { closed <- second.Close() }()
	select {
	case err := <-closed:
		if err != nil {
			t.Fatalf("close: %v", err)
		}
	case <-time.After(furrowTestSafety):
		t.Fatal("closing the second session waited on the first session's furrow")
	}
	if !processAlive(pid) {
		t.Fatal("the first session's furrow was cut by the second session closing")
	}
	g.release(t)
	answered(t, first)
}

// The first request of a session waits for a question that is already running,
// and gets the verbs because of it; with a cap too short to matter it goes out
// without them and the arming that follows is still correct.
func TestTheFirstRequestSettlesAQuestionAlreadyRunning(t *testing.T) {
	g := installGatedFurrow(t)
	agent := furrowSession(t, t.TempDir())
	g.started(t)
	settled := make(chan struct{})
	go func() {
		agent.furrow.settle(furrowTestSafety)
		close(settled)
	}()
	g.release(t)
	select {
	case <-settled:
	case <-time.After(furrowTestSafety):
		t.Fatal("the first request was not released by furrow's answer")
	}
	if got := len(workspaceVerbs(carried(agent))); got != len(furrow.Names()) {
		t.Fatalf("the request that waited carries %d workspace verbs; want %d", got, len(furrow.Names()))
	}
}

// The wait is a cap: with furrow parked the request goes out without the verbs,
// and it only ever waits the first time.
func TestTheFirstRequestDoesNotWaitForAParkedFurrow(t *testing.T) {
	g := installGatedFurrow(t)
	agent := furrowSession(t, t.TempDir())
	pid := g.started(t)
	agent.furrow.settle(time.Nanosecond)
	if !processAlive(pid) {
		t.Fatal("settling cut furrow")
	}
	if verbs := workspaceVerbs(carried(agent)); len(verbs) != 0 {
		t.Fatalf("the request carried %v with furrow still parked", verbs)
	}
	// Spent: a second look returns at once even with a long cap.
	agent.furrow.settle(furrowTestSafety)
	g.release(t)
}
