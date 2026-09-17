package main

// The execute() lifecycle, exercised the way the binary runs it: os.Args
// pointed at a real command, CODEAF_HOME in a temporary directory, and the
// endpoint at a dead loopback so a flush never reaches the real relay and the
// spool keeps what the run wrote for the assertions to read.
//
// The package is off under `go test` by design — the go-test rung is a
// production rule — and these tests do not weaken it. They use the one door
// past it, telemetry.EnableForTest, a func variable no environment variable
// or config value can reach.

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Agent-Field/codeaf/internal/home"
	"github.com/Agent-Field/codeaf/internal/telemetry"
)

// telemetryLifecycleHome points the process at a temporary home and a dead
// endpoint, and turns the ladder on for this test only. The endpoint refuses
// every connection, so the flush a lifecycle call ends with fails locally
// instead of draining the spool — an endpoint that answered would send the
// events and remove them, and the assertions below would have nothing to read.
func telemetryLifecycleHome(t *testing.T) {
	t.Helper()
	t.Setenv(home.EnvVar, t.TempDir())
	// Nothing listens here, and loopback dialing is refused immediately.
	t.Setenv("CODEAF_TELEMETRY_ENDPOINT", "http://127.0.0.1:1/telemetry")
	telemetry.EnableForTest(t, true)
}

// telemetrySpoolRows is what the spool holds, parsed. An absent spool is an
// empty slice, which is the assertion `codeaf version` makes.
func telemetrySpoolRows(t *testing.T) []map[string]any {
	t.Helper()
	body, err := os.ReadFile(telemetrySpoolPath(t))
	if err != nil {
		return nil
	}
	var rows []map[string]any
	for _, line := range strings.Split(strings.TrimSpace(string(body)), "\n") {
		if line == "" {
			continue
		}
		var row map[string]any
		if err := json.Unmarshal([]byte(line), &row); err != nil {
			t.Fatalf("spool line is not JSON: %v", err)
		}
		rows = append(rows, row)
	}
	return rows
}

// telemetrySpoolPath is the spool file inside the test's home.
func telemetrySpoolPath(t *testing.T) string {
	t.Helper()
	return filepath.Join(os.Getenv(home.EnvVar), "telemetry", "spool.jsonl")
}

// telemetryEventNames is the event names the spool holds, in order.
func telemetryEventNames(t *testing.T, rows []map[string]any) []string {
	t.Helper()
	var names []string
	for _, row := range rows {
		names = append(names, row["event_name"].(string))
	}
	return names
}

// telemetryRowNamed is the one spooled row with an event name, failed on when
// there is none. A fresh home spools first_run before the session events, so
// the tests below find the row they are about by name rather than by index.
func telemetryRowNamed(t *testing.T, rows []map[string]any, name string) map[string]any {
	t.Helper()
	for _, row := range rows {
		if row["event_name"] == name {
			return row
		}
	}
	t.Fatalf("no %s row in %v", name, telemetryEventNames(t, rows))
	return nil
}

// telemetryProps is a spooled row's props object, failed on when it carries
// none. The wire row nests every contract property under "props"; the
// identity fields sit beside it.
func telemetryProps(t *testing.T, row map[string]any) map[string]any {
	t.Helper()
	props, ok := row["props"].(map[string]any)
	if !ok {
		t.Fatalf("%v row carries no props", row["event_name"])
	}
	return props
}

// telemetryArgs points os.Args at the given words for the duration of one
// lifecycle call, because telemetryBegin reads the command line itself.
func telemetryArgs(args ...string) func() {
	previous := os.Args
	os.Args = append([]string{"codeaf"}, args...)
	return func() { os.Args = previous }
}

// TestTelemetryTaskSessionSpoolsStartedAndEnded runs one session through the
// lifecycle's own begin and end and reads what it left in the spool: a fresh
// install spools first_run first, then the task session's session_started and
// session_ended, both with mode "task", and the ended event carries the exit
// code's stop reason.
func TestTelemetryTaskSessionSpoolsStartedAndEnded(t *testing.T) {
	telemetryLifecycleHome(t)
	restore := telemetryArgs("plan", "run", "p.json")
	defer restore()

	session := telemetryBegin()
	if session.mode != telemetry.ModeTask || session.resumed {
		t.Fatalf("plan run: mode=%q resumed=%v, want task, false", session.mode, session.resumed)
	}
	if session.sessionID == "" {
		t.Fatalf("plan run minted no session id")
	}
	telemetryEnd(session, 0)

	rows := telemetrySpoolRows(t)
	names := telemetryEventNames(t, rows)
	if len(names) != 3 || names[0] != "first_run" || names[1] != "session_started" || names[2] != "session_ended" {
		t.Fatalf("spool holds %v, want [first_run session_started session_ended]", names)
	}
	startedRow := telemetryRowNamed(t, rows, "session_started")
	endedRow := telemetryRowNamed(t, rows, "session_ended")
	started := telemetryProps(t, startedRow)
	ended := telemetryProps(t, endedRow)
	if started["mode"] != "task" || ended["mode"] != "task" {
		t.Fatalf("modes were %v and %v, want task", started["mode"], ended["mode"])
	}
	if ended["stop_reason"] != "done" {
		t.Fatalf("stop_reason was %v, want done for exit 0", ended["stop_reason"])
	}
	if startedRow["session_id_hash"] != endedRow["session_id_hash"] {
		t.Fatalf("started and ended hashed different session ids")
	}
}

// TestTelemetrySessionEndedCarriesTheSessionCounters: the end event is built
// from the process's own tally (telemetry.Snapshot), so a turn, a failed model
// call with its cost, and a tool call counted during the run reach the spooled
// session_ended line as the contract's bands. The counters are process-wide
// and this test is the only one in the package that touches them, so their
// count is exactly the three calls made here.
func TestTelemetrySessionEndedCarriesTheSessionCounters(t *testing.T) {
	telemetryLifecycleHome(t)
	restore := telemetryArgs("do", "fix the bug")
	defer restore()

	telemetry.CountTurn()
	telemetry.CountModelCall(false, 0.02)
	telemetry.CountToolCall(true)

	session := telemetryBegin()
	telemetryEnd(session, 0)

	ended := telemetryProps(t, telemetryRowNamed(t, telemetrySpoolRows(t), "session_ended"))
	want := map[string]string{
		"turns":              "1",
		"model_calls":        "1",
		"model_calls_failed": "1",
		"tool_calls":         "1",
		"tool_calls_failed":  "0",
		"cost_usd":           "0.01-0.1",
	}
	for key, value := range want {
		if ended[key] != value {
			t.Errorf("session_ended %s = %v, want %s", key, ended[key], value)
		}
	}
}

// TestTelemetryModeIsReadFromTheCommandLine is the mode table: only session
// commands emit, chat and resume are chat, the task verbs are task, and
// nothing else is a session at all.
func TestTelemetryModeIsReadFromTheCommandLine(t *testing.T) {
	cases := []struct {
		args    []string
		mode    telemetry.Mode
		resumed bool
		session bool
	}{
		{[]string{"chat"}, telemetry.ModeChat, false, true},
		{[]string{"resume", "abc"}, telemetry.ModeChat, true, true},
		{[]string{"do", "fix the bug"}, telemetry.ModeTask, false, true},
		{[]string{"exec", "ls"}, telemetry.ModeTask, false, true},
		{[]string{"run", "p.json"}, telemetry.ModeTask, false, true},
		{[]string{"plan", "run", "p.json"}, telemetry.ModeTask, false, true},
		{[]string{"version"}, "", false, false},
		{[]string{"help"}, "", false, false},
		{[]string{"doctor"}, "", false, false},
		{[]string{"telemetry", "status"}, "", false, false},
		{[]string{"plan", "new"}, "", false, false},
		{[]string{"logs"}, "", false, false},
	}
	for _, want := range cases {
		mode, resumed, session := telemetryMode(want.args)
		if mode != want.mode || resumed != want.resumed || session != want.session {
			t.Errorf("%v: mode=%q resumed=%v session=%v, want %q %v %v",
				want.args, mode, resumed, session, want.mode, want.resumed, want.session)
		}
	}
}

// TestTelemetryVersionSpoolsNothingAndCreatesNoDirectory: a non-session
// command emits nothing and sends nothing, not even the directory the spool
// would live in.
func TestTelemetryVersionSpoolsNothingAndCreatesNoDirectory(t *testing.T) {
	telemetryLifecycleHome(t)

	if code := telemetryFakeExecute("version"); code != 0 {
		t.Fatalf("version exited %d", code)
	}
	if rows := telemetrySpoolRows(t); len(rows) != 0 {
		t.Fatalf("version spooled %v", telemetryEventNames(t, rows))
	}
	if _, err := os.Stat(filepath.Join(os.Getenv(home.EnvVar), "telemetry")); !os.IsNotExist(err) {
		t.Fatalf("version created a telemetry directory")
	}
}

// TestTelemetryOffWritesNothingAtAll: CODEAF_TELEMETRY=off is the ladder's
// first rung and nothing may be written for a session command either. The
// ladder is read honestly here — EnableForTest(false) — so the off rung is
// what stops the writes, not the test override.
func TestTelemetryOffWritesNothingAtAll(t *testing.T) {
	telemetryLifecycleHome(t)
	telemetry.EnableForTest(t, false)
	t.Setenv("CODEAF_TELEMETRY", "off")
	restore := telemetryArgs("do", "fix the bug")
	defer restore()

	session := telemetryBegin()
	telemetryEnd(session, 0)
	if rows := telemetrySpoolRows(t); len(rows) != 0 {
		t.Fatalf("%d events written with telemetry off: %v", len(rows), telemetryEventNames(t, rows))
	}
	if _, err := os.Stat(filepath.Join(os.Getenv(home.EnvVar), "telemetry")); !os.IsNotExist(err) {
		t.Fatalf("a telemetry directory was created with telemetry off")
	}
}

// TestTelemetryNoticePrintsOnceAcrossTwoInvocations: the notice is shown once
// per install. The first task session marks it shown and spools first_run
// once; the second session of the same install marks nothing and spools no
// second first_run.
func TestTelemetryNoticePrintsOnceAcrossTwoInvocations(t *testing.T) {
	telemetryLifecycleHome(t)
	restore := telemetryArgs("do", "fix the bug")
	defer restore()

	if telemetry.NoticeShown() {
		t.Fatalf("a fresh home read as notice-already-shown")
	}
	// A task command shows the notice even with piped stderr, because a task
	// runs unattended and its person may never open a chat.
	first := telemetryBegin()
	if !telemetry.NoticeShown() {
		t.Fatalf("the notice was not marked shown for a task session")
	}
	telemetryEnd(first, 0)

	rows := telemetrySpoolRows(t)
	firstRuns := 0
	for _, row := range rows {
		if row["event_name"] == "first_run" {
			firstRuns++
		}
	}
	if firstRuns != 1 {
		t.Fatalf("%d first_run events for one install, want 1", firstRuns)
	}

	second := telemetryBegin()
	telemetryEnd(second, 0)
	// The first session's first_run is still in the spool — the flush could not
	// send and kept every line — so the check is that the second invocation
	// added none: one first_run for the install across both invocations.
	firstRuns = 0
	for _, row := range telemetrySpoolRows(t) {
		if row["event_name"] == "first_run" {
			firstRuns++
		}
	}
	if firstRuns != 1 {
		t.Fatalf("two invocations spooled %d first_run events, want 1", firstRuns)
	}
}

// TestTelemetryNoticeNeverPrintsUnderJSON: a --json stdout must stay
// machine-clean, so a --json session never shows the notice; the events wait
// in the spool until a session that can show it runs.
func TestTelemetryNoticeNeverPrintsUnderJSON(t *testing.T) {
	telemetryLifecycleHome(t)
	restore := telemetryArgs("do", "--json", "fix the bug")
	defer restore()

	if !telemetryHasJSON(os.Args[1:]) {
		t.Fatalf("telemetryHasJSON missed --json")
	}
	session := telemetryBegin()
	telemetryEnd(session, 0)
	if telemetry.NoticeShown() {
		t.Fatalf("the notice was marked shown under --json")
	}
}

// telemetryFakeExecute runs execute() in-process with os.Args pointed at the
// given words. execute() returns a code and does not exit the process, which
// is what makes this test cheap enough to run alongside the others.
func telemetryFakeExecute(args ...string) int {
	restore := telemetryArgs(args...)
	defer restore()
	return execute()
}
