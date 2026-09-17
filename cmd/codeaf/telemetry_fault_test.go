package main

// The crash site's one telemetry obligation, held down: reportFault leaves a
// fault event in the spool carrying the contract's three properties, and the
// panic's own text reaches neither a key nor a value of it.
//
// The event is read out of the spool rather than off the wire because that is
// where a dying process leaves it. The flush reportFault makes is a no-op until
// the notice has been shown, and a test's home has not shown it, so the line is
// still on disk here rather than on its way to the endpoint.

import (
	"bytes"
	"regexp"
	"strings"
	"testing"

	"github.com/Agent-Field/codeaf/internal/config"
	"github.com/Agent-Field/codeaf/internal/telemetry"
)

// faultDetail is the panic value reportFault is handed: the runtime's words, a
// path on the person's machine, and a token shaped like a credential. A panic
// text carries whatever the failing line was holding, which is exactly why the
// contract gives the fault event no property for it.
const faultDetail = "runtime error: index out of range [3:] reading " +
	"/Users/somebody/private/notes.md (OPENROUTER_API_KEY=sk-or-v1-abcdef)"

// faultStack is a fake dump: two codeaf frames for the fingerprint to hash,
// each with a file half carrying a machine path of its own.
const faultStack = `goroutine 1 [running]:
main.runDo(0x0?)
	/Users/somebody/work/codeaf/cmd/codeaf/do.go:42 +0x1c8
github.com/Agent-Field/codeaf/internal/session.(*Agent).Run(0x0?, {0x0?, 0x0?})
	/Users/somebody/work/codeaf/internal/session/agent.go:100 +0x60
`

// faultLeaks are the strings that must not appear anywhere in a spooled row:
// the panic text whole and in its sensitive pieces, and the stack's paths,
// which are the same leak through another door.
var faultLeaks = []string{
	faultDetail,
	"index out of range",
	"/Users/somebody/private/notes.md",
	"OPENROUTER_API_KEY",
	"sk-or-v1-abcdef",
	"/Users/somebody/work/codeaf",
}

var reFaultFingerprint = regexp.MustCompile(`^[0-9a-f]{16}$`)

// TestReportFaultSpoolsAFaultEventAndNotThePanicText runs one crash through
// reportFault and reads what it left behind: a single fault event, scoped to
// the main goroutine, carrying the session's mode and a fingerprint hashed from
// the stack — and nothing that says what panicked. The exit code and the
// sentence on stderr are asserted too, because the count is allowed to cost
// this function neither of them.
func TestReportFaultSpoolsAFaultEventAndNotThePanicText(t *testing.T) {
	telemetryLifecycleHome(t)
	// The fault log is appended under the profile, and a profile pinned by the
	// environment would put it outside this test's home.
	t.Setenv(config.ProfileDirEnv, "")

	previous := currentTelemetrySession
	currentTelemetrySession = telemetrySession{mode: telemetry.ModeChat, sessionID: "fault-session"}
	t.Cleanup(func() { currentTelemetrySession = previous })

	stderr := &bytes.Buffer{}
	if code := reportFault(stderr, faultDetail, []byte(faultStack)); code != 1 {
		t.Fatalf("exit code = %d, want 1", code)
	}
	if shown := stderr.String(); !strings.Contains(shown, "codeaf hit an internal fault and had to stop") {
		t.Fatalf("the calm sentence did not reach stderr: %q", shown)
	}

	rows := telemetrySpoolRows(t)
	faults := 0
	var fault map[string]any
	for _, row := range rows {
		// Every row is walked, not just the fault's: an asynchronous append
		// from an earlier test in this binary can land in this home, so the
		// leak check reads all of them and the count below reads only the ones
		// named fault.
		assertNoFaultLeak(t, row)
		if row["event_name"] == "fault" {
			faults++
			fault = row
		}
	}
	if faults != 1 {
		t.Fatalf("%d fault events for one crash, want 1 (spool holds %v)",
			faults, telemetryEventNames(t, rows))
	}

	props, ok := fault["props"].(map[string]any)
	if !ok {
		t.Fatalf("the fault row carries no props object: %v", fault)
	}
	if props["scope"] != telemetry.ScopeMain {
		t.Fatalf("scope = %v, want main", props["scope"])
	}
	if props["mode"] != string(telemetry.ModeChat) {
		t.Fatalf("mode = %v, want the session's chat: the event is built off currentTelemetrySession",
			props["mode"])
	}
	if fingerprint, _ := props["fingerprint"].(string); !reFaultFingerprint.MatchString(fingerprint) {
		t.Fatalf("fingerprint = %q, want 16 lowercase hex characters", fingerprint)
	}
}

// assertNoFaultLeak fails on the first key or value of a parsed spool row that
// carries any of the panic's text. Walking the row is the assertion the
// contract actually makes: it is not enough that the fault event has no
// property named for the panic message, because a key or a value anywhere in
// the row could hold one.
func assertNoFaultLeak(t *testing.T, row map[string]any) {
	t.Helper()
	for _, text := range spoolRowText(row) {
		for _, leak := range faultLeaks {
			if strings.Contains(text, leak) {
				t.Fatalf("the panic text reached the spool: %q holds %q", text, leak)
			}
		}
	}
}

// spoolRowText is every key and every string value in a parsed row, at any
// depth. Anything that is neither — a number, a bool — cannot carry text and is
// left out.
func spoolRowText(value any) []string {
	switch typed := value.(type) {
	case map[string]any:
		var out []string
		for key, child := range typed {
			out = append(out, key)
			out = append(out, spoolRowText(child)...)
		}
		return out
	case []any:
		var out []string
		for _, child := range typed {
			out = append(out, spoolRowText(child)...)
		}
		return out
	case string:
		return []string{typed}
	}
	return nil
}
