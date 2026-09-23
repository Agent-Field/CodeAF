package main

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

// shortBase makes a probe base with a short absolute path: macOS unix
// sockets cap around 104 bytes and tmux -S fails on longer ones.
//
// IT ENDS EVERY TMUX SERVER A TEST STARTED UNDER IT, BY PID, BEFORE THE BASE
// IS REMOVED. A probe `start` runs the binary inside a tmux server on
// <base>/<root>/tmux.sock, and only `finish` ends that session. A journey that
// fails before it finishes — TestFailingJourneyCannotPass does, on purpose —
// used to leave the server, the stub `codeaf chat` and its /bin/sh wrapper
// running; the RemoveAll below then deleted the socket, so nothing could ever
// reach them again. About 280 of them were found on one machine. The server's
// pid is read from the server itself and that one process is signalled; its
// sessions go with it.
func shortBase(t *testing.T) string {
	t.Helper()
	p, err := os.MkdirTemp("/tmp", "cbp-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		endTmuxServers(p)
		os.RemoveAll(p)
	})
	return p
}

// endTmuxServers ends the tmux server behind every probe socket under base and
// waits for each to be gone.
func endTmuxServers(base string) {
	sockets, _ := filepath.Glob(filepath.Join(base, "*", "tmux.sock"))
	for _, socket := range sockets {
		pid := tmuxServerPID(socket)
		if pid <= 0 {
			continue
		}
		_ = syscall.Kill(pid, syscall.SIGTERM)
		deadline := time.Now().Add(5 * time.Second)
		for syscall.Kill(pid, 0) == nil && time.Now().Before(deadline) {
			time.Sleep(20 * time.Millisecond)
		}
		if syscall.Kill(pid, 0) == nil {
			_ = syscall.Kill(pid, syscall.SIGKILL)
		}
	}
}

// tmuxServerPID is the pid of the server listening on one socket, asked of
// the server itself; 0 when none answers.
func tmuxServerPID(socket string) int {
	out, err := exec.Command("tmux", "-S", socket, "display-message", "-p", "#{pid}").Output()
	if err != nil {
		return 0
	}
	pid, err := strconv.Atoi(strings.TrimSpace(string(out)))
	if err != nil {
		return 0
	}
	return pid
}

// TestAJourneyThatNeverFinishesLeavesNoProcessBehind is the leak, pinned: a
// session is started and never finished, the way a failing journey leaves it,
// and once its test is over the tmux server holding it is gone.
func TestAJourneyThatNeverFinishesLeavesNoProcessBehind(t *testing.T) {
	var server int
	t.Run("journey", func(t *testing.T) {
		base := shortBase(t)
		bin := stubCodeaf(t)
		if c, out := runCLI(t, base, "start", "--session", "left", "--profile", "reviewer", "--bin", bin); c != 0 {
			t.Fatalf("start: exit %d\n%s", c, out)
		}
		server = tmuxServerPID(filepath.Join(base, "default", "tmux.sock"))
		if server <= 0 {
			t.Fatal("no tmux server is holding the started session")
		}
	})
	if server > 0 && syscall.Kill(server, 0) == nil {
		_ = syscall.Kill(server, syscall.SIGKILL)
		t.Fatalf("tmux server %d outlived the test that started it", server)
	}
}

// runCLI builds the probe binary once and runs one CLI invocation against a
// tmp probe base, so every test exercises the real subprocess boundary the
// way a coding agent does.
func runCLI(t *testing.T, base string, args ...string) (exitCode int, stdout string) {
	t.Helper()
	bin := t.TempDir() + "/codeaf-probe"
	if _, err := os.Stat(bin); os.IsNotExist(err) {
		out, err := exec.Command("go", "build", "-o", bin, ".").CombinedOutput()
		if err != nil {
			t.Fatalf("build probe: %v\n%s", err, out)
		}
	}
	cmd := exec.Command(bin, args...)
	cmd.Env = append(os.Environ(), "CODEAF_PROBE_BASE="+base)
	var out strings.Builder
	cmd.Stdout = &out
	cmd.Stderr = &out
	err := cmd.Run()
	return exitCodeOf(err), out.String()
}

func exitCodeOf(err error) int {
	if err == nil {
		return 0
	}
	if ee, ok := err.(*exec.ExitError); ok {
		return ee.ExitCode()
	}
	return -1
}

// stubCodeaf writes a long-lived stand-in for the pinned codeaf binary: a
// shell prompt loop that echoes typed lines, so real tmux text/keys flow.
func stubCodeaf(t *testing.T) string {
	t.Helper()
	p := t.TempDir() + "/codeaf"
	script := "#!/bin/sh\nwhile IFS= read -r line; do echo \"ok: $line\"; done\necho ready\n"
	if err := os.WriteFile(p, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	return p
}

type envelope struct {
	OK    bool            `json:"ok"`
	Data  json.RawMessage `json:"data"`
	Error *struct {
		Code    string `json:"code"`
		Message string `json:"message"`
	} `json:"error"`
}

func mustEnvelope(t *testing.T, stdout string) envelope {
	t.Helper()
	var e envelope
	if err := json.Unmarshal([]byte(strings.TrimSpace(stdout)), &e); err != nil {
		t.Fatalf("stdout is not one JSON object: %q (%v)", stdout, err)
	}
	return e
}

// TestSharedPersistentSession proves two independent CLI invocations share
// one persistent session, and observe returns a non-empty rendered snapshot.
func TestSharedPersistentSession(t *testing.T) {
	base := shortBase(t)
	bin := stubCodeaf(t)
	if c, out := runCLI(t, base, "start", "--session", "s1", "--profile", "reviewer", "--bin", bin); c != 0 {
		t.Fatalf("start: exit %d\n%s", c, out)
	}
	// A second, independent invocation: type into the same session.
	if c, out := runCLI(t, base, "act", "--session", "s1", "--text", "hello probe\n"); c != 0 {
		t.Fatalf("act (2nd invocation): exit %d\n%s", c, out)
	}
	// A third: observe must see the typed text through the persistent pane.
	c, out := runCLI(t, base, "observe", "--session", "s1")
	if c != 0 {
		t.Fatalf("observe (3rd invocation): exit %d\n%s", c, out)
	}
	e := mustEnvelope(t, out)
	var obs struct {
		Revision int    `json:"revision"`
		Snapshot string `json:"snapshot"`
		Cursor   struct {
			X int `json:"x"`
			Y int `json:"y"`
		} `json:"cursor"`
	}
	if err := json.Unmarshal(e.Data, &obs); err != nil {
		t.Fatalf("observe data: %v", err)
	}
	if obs.Revision < 1 {
		t.Errorf("revision did not carry across invocations: %d", obs.Revision)
	}
	if !strings.Contains(obs.Snapshot, "ok: hello probe") {
		t.Errorf("snapshot does not render the typed line:\n%q", obs.Snapshot)
	}
	if strings.TrimSpace(obs.Snapshot) == "" {
		t.Error("rendered snapshot is empty")
	}
	// Diff observation works too.
	c, out = runCLI(t, base, "act", "--session", "s1", "--text", "second\n")
	if c != 0 {
		t.Fatalf("act 2: exit %d\n%s", c, out)
	}
	c, out = runCLI(t, base, "observe", "--session", "s1", "--diff")
	if c != 0 || !strings.Contains(out, "+ok: second") {
		t.Fatalf("observe --diff: exit %d\n%s", c, out)
	}
}

// TestStaleRevisionRejected proves act with a wrong --expect-revision exits
// non-zero with STALE_REVISION and sends nothing.
func TestStaleRevisionRejected(t *testing.T) {
	base := shortBase(t)
	bin := stubCodeaf(t)
	if c, out := runCLI(t, base, "start", "--session", "st", "--profile", "reviewer", "--bin", bin); c != 0 {
		t.Fatalf("start: exit %d\n%s", c, out)
	}
	c, out := runCLI(t, base, "act", "--session", "st", "--text", "x\n", "--expect-revision", "999")
	if c == 0 {
		t.Fatalf("stale act must exit non-zero, got %d\n%s", c, out)
	}
	e := mustEnvelope(t, out)
	if e.OK || e.Error == nil || e.Error.Code != "STALE_REVISION" {
		t.Fatalf("expected STALE_REVISION error, got %s", out)
	}
	// The honest snapshot observation is still available afterwards.
	c, out = runCLI(t, base, "observe", "--session", "st")
	if c != 0 || !strings.Contains(out, "revision") {
		t.Fatalf("observe after stale act: exit %d\n%s", c, out)
	}
}

func TestWaitContractVerbPrepareFinish(t *testing.T) {
	base := shortBase(t)
	bin := stubCodeaf(t)
	// contract: machine-readable schema of all verbs.
	if c, out := runCLI(t, base, "contract"); c != 0 || !strings.Contains(out, `"verbs"`) {
		t.Fatalf("contract: exit %d\n%s", c, out)
	}
	// prepare --bin reuses the same identity on a second call.
	c, out := runCLI(t, base, "prepare", "--bin", bin)
	if c != 0 || !strings.Contains(out, `"sha"`) {
		t.Fatalf("prepare: exit %d\n%s", c, out)
	}
	if c, out := runCLI(t, base, "prepare", "--bin", bin); c != 0 || !strings.Contains(out, `"reused":true`) {
		t.Fatalf("prepare reuse: exit %d\n%s", c, out)
	}
	if c, out := runCLI(t, base, "start", "--session", "w", "--profile", "p", "--bin", bin); c != 0 {
		t.Fatalf("start: exit %d\n%s", c, out)
	}
	// wait on a quiet screen settles.
	if c, out := runCLI(t, base, "wait", "--session", "w", "--quiet", "200", "--timeout", "3000"); c != 0 || !strings.Contains(out, `"settled":true`) {
		t.Fatalf("wait: exit %d\n%s", c, out)
	}
	// NO_SESSION for an unknown session, non-zero.
	if c, out := runCLI(t, base, "observe", "--session", "nope"); c == 0 || !strings.Contains(out, "NO_SESSION") {
		t.Fatalf("observe unknown: exit %d\n%s", c, out)
	}
	// finish cleans the owned session; a second finish is NO_SESSION.
	if c, out := runCLI(t, base, "finish", "--session", "w"); c != 0 {
		t.Fatalf("finish: exit %d\n%s", c, out)
	}
	if c, out := runCLI(t, base, "finish", "--session", "w"); c == 0 || !strings.Contains(out, "NO_SESSION") {
		t.Fatalf("second finish: exit %d\n%s", c, out)
	}
	if _, err := os.Stat(base + "/default/homes/w"); !os.IsNotExist(err) {
		t.Errorf("session home not removed")
	}
}

// TestAtomicActWaitObserve proves the required atomic act+wait+observe:
// act with a mutation AND --wait succeeds (wait is a modifier, not a
// mutually-exclusive mutation) and returns a non-empty rendered snapshot
// in one invocation.
func TestAtomicActWaitObserve(t *testing.T) {
	base := shortBase(t)
	bin := stubCodeaf(t)
	if c, out := runCLI(t, base, "start", "--session", "aw", "--profile", "reviewer", "--bin", bin); c != 0 {
		t.Fatalf("start: exit %d\n%s", c, out)
	}
	c, out := runCLI(t, base, "act", "--session", "aw", "--text", "atomic line\n", "--wait", "200,3000")
	if c != 0 {
		t.Fatalf("atomic act+wait must exit 0, got %d\n%s", c, out)
	}
	e := mustEnvelope(t, out)
	var obs struct {
		Observation struct {
			Snapshot string `json:"snapshot"`
		} `json:"observation"`
	}
	if err := json.Unmarshal(e.Data, &obs); err != nil {
		t.Fatalf("act data: %v", err)
	}
	if !strings.Contains(obs.Observation.Snapshot, "atomic line") || strings.TrimSpace(obs.Observation.Snapshot) == "" {
		t.Errorf("atomic act+wait returned no rendered observation:\n%q", obs.Observation.Snapshot)
	}
	// text + keys together is still one mutation too many.
	if c, out := runCLI(t, base, "act", "--session", "aw", "--text", "a", "--keys", "Enter"); c == 0 {
		t.Errorf("text+keys must still be rejected\n%s", out)
	}
}
