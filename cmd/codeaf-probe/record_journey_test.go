package main

import (
	"encoding/json"
	"os"
	"os/exec"
	"strings"
	"testing"

	"github.com/Agent-Field/codeaf/internal/probe"
)

// TestRecordingCorrespondence proves the evidence file records exactly what
// the CLI did and saw: every act line carries the observation the CLI
// returned, and every observe line matches the observe envelope the agent
// got back — read from disk, not from memory.
func TestRecordingCorrespondence(t *testing.T) {
	base := shortBase(t)
	bin := stubCodeaf(t)
	if c, out := runCLI(t, base, "start", "--session", "rec", "--profile", "reviewer", "--bin", bin); c != 0 {
		t.Fatalf("start: exit %d\n%s", c, out)
	}
	// Two acts across independent invocations, each with its observation.
	c, out := runCLI(t, base, "act", "--session", "rec", "--text", "hello probe\n")
	if c != 0 {
		t.Fatalf("act 1: exit %d\n%s", c, out)
	}
	first := out
	c, out = runCLI(t, base, "act", "--session", "rec", "--text", "second line\n")
	if c != 0 {
		t.Fatalf("act 2: exit %d\n%s", c, out)
	}
	second := out
	c, out = runCLI(t, base, "observe", "--session", "rec", "--diff")
	if c != 0 {
		t.Fatalf("observe: exit %d\n%s", c, out)
	}
	obsOut := out
	c, out = runCLI(t, base, "finish", "--session", "rec")
	if c != 0 {
		t.Fatalf("finish: exit %d\n%s", c, out)
	}

	// Read the recording back from disk.
	probe.ProbeBase = base
	t.Cleanup(func() { probe.ProbeBase = "" })
	m, err := probe.Open("default")
	if err != nil {
		t.Fatalf("open base: %v", err)
	}
	recs, err := probe.ReadRecords(m, "rec")
	if err != nil {
		t.Fatalf("read records: %v", err)
	}
	// Two acts, one observe, one finish-outcome = 4 step records.
	if len(recs) != 4 {
		t.Fatalf("want 4 records, got %d:\n%v", len(recs), recs)
	}
	if recs[0].Verb != "act" || recs[1].Verb != "act" || recs[2].Verb != "observe" || recs[3].Verb != "outcome" {
		t.Fatalf("unexpected verb order: %s %s %s %s", recs[0].Verb, recs[1].Verb, recs[2].Verb, recs[3].Verb)
	}
	for i, want := range []string{first, second} {
		r := recs[i]
		if r.Observation == nil || r.Observation.Snapshot == "" {
			t.Fatalf("act %d record carries no observation snapshot", i+1)
		}
		if !strings.Contains(r.Observation.Snapshot, "ok:") {
			t.Fatalf("act %d record's observation is not a rendered snapshot", i+1)
		}
		// The recorded observation corresponds to what the CLI returned:
		// the CLI's envelope carries the same snapshot and revision.
		env := mustEnvelope(t, want)
		var returned struct {
			Observation   probe.ObserveData `json:"observation"`
			RevisionAfter int               `json:"revision_after"`
		}
		if err := json.Unmarshal(env.Data, &returned); err != nil {
			t.Fatalf("parse act envelope %d: %v", i+1, err)
		}
		if returned.RevisionAfter != r.Revision {
			t.Errorf("act %d: recorded revision %d != CLI revision_after %d", i+1, r.Revision, returned.RevisionAfter)
		}
		if returned.Observation.Snapshot != "" && returned.Observation.Snapshot != r.Observation.Snapshot {
			t.Errorf("act %d: recorded snapshot differs from what the CLI returned", i+1)
		}
		if got := r.Step; got != i+1 {
			t.Errorf("step index %d out of order: got %d", i+1, got)
		}
	}
	// The observe record's diff corresponds to the CLI's --diff answer.
	env := mustEnvelope(t, obsOut)
	if !strings.Contains(string(env.Data), "+ok: second") {
		t.Fatalf("observe envelope lacks the diff the CLI printed: %s", obsOut)
	}
	if recs[2].Observation == nil || !strings.Contains(recs[2].Observation.Diff, "+ok: second") {
		t.Errorf("observe record's recorded diff does not match the CLI's diff")
	}
	if recs[3].Verb != "outcome" || recs[3].Outcome != "ok" {
		t.Errorf("finish must record an honest ok outcome, got %+v", recs[3])
	}
	// Private perms: the recording is nobody else's business.
	if st, err := os.Stat(probe.RecordingPath(m, "rec")); err != nil {
		t.Fatalf("recording file missing: %v", err)
	} else if st.Mode().Perm()&0o077 != 0 {
		t.Errorf("recording file is group/world readable: %v", st.Mode())
	}
	if st, err := os.Stat(base + "/default/recordings"); err == nil && st.Mode().Perm()&0o077 != 0 {
		t.Errorf("recordings dir is group/world accessible: %v", st.Mode())
	}
}

// TestFailingJourneyCannotPass runs a deliberately failing journey — it
// asserts on a screen state that never occurs — and proves the failure is
// loud: the journey exits non-zero AND the recording carries an honest
// failed outcome with the reason. Nothing silently passes.
func TestFailingJourneyCannotPass(t *testing.T) {
	base := shortBase(t)
	bin := stubCodeaf(t)
	probeBin := t.TempDir() + "/codeaf-probe"
	if out, err := exec.Command("go", "build", "-o", probeBin, ".").CombinedOutput(); err != nil {
		t.Fatalf("build probe: %v\n%s", err, out)
	}
	// The journey, exactly as CI would run it: a shell script driving the
	// real binary. Its assertion is on a screen state that never occurs.
	script := `#!/bin/sh
set -u
export CODEAF_PROBE_BASE="` + base + `"
P="` + probeBin + `"
B="` + bin + `"
$P start --session fj --profile reviewer --bin "$B" >/dev/null || exit 2
$P act --session fj --text "hello journey" >/dev/null || exit 2
$P act --session fj --keys Enter >/dev/null || exit 2
if $P observe --session fj | grep -q "SCREEN_STATE_THAT_NEVER_OCCURS"; then
  $P record-outcome --session fj --outcome ok --reason "marker found"
  exit 0
fi
$P record-outcome --session fj --outcome failed \
  --reason "journey assertion failed: SCREEN_STATE_THAT_NEVER_OCCURS not on screen"
exit 1
`
	jp := t.TempDir() + "/failing-journey.sh"
	if err := os.WriteFile(jp, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	err := exec.Command("/bin/sh", jp).Run()
	// The journey MUST fail loudly: a non-zero exit is the whole point.
	if err == nil {
		t.Fatal("failing journey exited 0 — a failure silently passed, which must never happen")
	}
	ee, ok := err.(*exec.ExitError)
	if !ok || ee.ExitCode() == 0 {
		t.Fatalf("journey exit not observable as non-zero: %v", err)
	}
	// And the recording says so honestly.
	probe.ProbeBase = base
	t.Cleanup(func() { probe.ProbeBase = "" })
	m, _ := probe.Open("default")
	recs, err := probe.ReadRecords(m, "fj")
	if err != nil {
		t.Fatalf("read records: %v", err)
	}
	var last probe.Record
	found := false
	for _, r := range recs {
		if r.Verb == "outcome" {
			last, found = r, true
		}
	}
	if !found {
		t.Fatalf("no outcome record in a failed journey's recording: %v", recs)
	}
	if last.Outcome != "failed" {
		t.Errorf("failed journey recorded outcome %q, want failed", last.Outcome)
	}
	if !strings.Contains(last.Reason, "SCREEN_STATE_THAT_NEVER_OCCURS") {
		t.Errorf("outcome reason does not name the failed assertion: %q", last.Reason)
	}
	// A passing journey, for contrast, records outcome ok — the difference
	// is in the evidence, not in hope.
	s2 := strings.Replace(strings.Replace(script, "SCREEN_STATE_THAT_NEVER_OCCURS", "ok: hello journey", -1), "exit 1", "exit 0", -1)
	s2 = strings.ReplaceAll(s2, "fj ", "fj2 ")
	jp2 := t.TempDir() + "/passing-journey.sh"
	if err := os.WriteFile(jp2, []byte(s2), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := exec.Command("/bin/sh", jp2).Run(); err != nil {
		t.Fatalf("passing journey failed: %v", err)
	}
	recs2, _ := probe.ReadRecords(m, "fj2")
	for _, r := range recs2 {
		if r.Verb == "outcome" && r.Outcome != "ok" {
			t.Errorf("passing journey recorded outcome %q", r.Outcome)
		}
	}
}

// chattyStubCodeaf writes a stand-in that keeps the pane moving forever, so
// a bounded wait can be proved to time out against a real tmux pane.
func chattyStubCodeaf(t *testing.T) string {
	t.Helper()
	p := t.TempDir() + "/codeaf"
	script := "#!/bin/sh\necho ready\ni=0\nwhile true; do i=$((i+1)); echo tick-$i; sleep 0.1; done\n"
	if err := os.WriteFile(p, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	return p
}

// TestRecordVerbReadsRecording drives the real CLI: a session that was acted
// on, then read back through the `record` verb, must return the same lines
// the recording holds — one JSON object on stdout.
func TestRecordVerbReadsRecording(t *testing.T) {
	base := shortBase(t)
	bin := stubCodeaf(t)
	if c, out := runCLI(t, base, "start", "--session", "recv", "--profile", "reviewer", "--bin", bin); c != 0 {
		t.Fatalf("start: exit %d\n%s", c, out)
	}
	defer runCLI(t, base, "finish", "--session", "recv")
	if c, out := runCLI(t, base, "act", "--session", "recv", "--text", "hello probe\n"); c != 0 {
		t.Fatalf("act: exit %d\n%s", c, out)
	}
	c, out := runCLI(t, base, "record", "--session", "recv")
	if c != 0 {
		t.Fatalf("record: exit %d\n%s", c, out)
	}
	var env struct {
		OK   bool `json:"ok"`
		Data struct {
			SessionID string         `json:"session_id"`
			Count     int            `json:"count"`
			Records   []probe.Record `json:"records"`
		} `json:"data"`
	}
	if err := json.Unmarshal([]byte(strings.TrimSpace(out)), &env); err != nil {
		t.Fatalf("record stdout not one JSON object: %q (%v)", out, err)
	}
	if !env.OK || env.Data.Count < 1 || len(env.Data.Records) != env.Data.Count {
		t.Fatalf("bad record envelope: %+v", env)
	}
	if env.Data.Records[0].Verb != "act" {
		t.Fatalf("first record verb %q, want act", env.Data.Records[0].Verb)
	}
}

// TestActWaitTimeoutExitsTimeout proves act --wait reports TIMEOUT on the
// wire, not BAD_REQUEST, when the bounded wait runs out.
func TestActWaitTimeoutExitsTimeout(t *testing.T) {
	base := shortBase(t)
	bin := chattyStubCodeaf(t)
	if c, out := runCLI(t, base, "start", "--session", "tw", "--profile", "reviewer", "--bin", bin); c != 0 {
		t.Fatalf("start: exit %d\n%s", c, out)
	}
	defer runCLI(t, base, "finish", "--session", "tw")
	// The chatty stub keeps the pane moving, so quiet is never reached.
	c, out := runCLI(t, base, "act", "--session", "tw", "--wait", "200,800")
	if c == 0 {
		t.Fatalf("wait-only act on chatty pane succeeded:\n%s", out)
	}
	var e envelope
	if err := json.Unmarshal([]byte(strings.TrimSpace(out)), &e); err != nil {
		t.Fatalf("act timeout stdout not JSON: %q (%v)", out, err)
	}
	if e.Error == nil || e.Error.Code != "TIMEOUT" {
		t.Fatalf("want error code TIMEOUT, got %+v", e.Error)
	}
}

// TestWaitReportsCurrentRevision proves the wait verb returns the session's
// current revision even when the wait times out.
func TestWaitReportsCurrentRevision(t *testing.T) {
	base := shortBase(t)
	bin := chattyStubCodeaf(t)
	if c, out := runCLI(t, base, "start", "--session", "wrev", "--profile", "reviewer", "--bin", bin); c != 0 {
		t.Fatalf("start: exit %d\n%s", c, out)
	}
	defer runCLI(t, base, "finish", "--session", "wrev")
	for i := 0; i < 2; i++ {
		if c, out := runCLI(t, base, "act", "--session", "wrev", "--text", "x\n"); c != 0 {
			t.Fatalf("act %d: exit %d\n%s", i, c, out)
		}
	}
	// The chatty stub keeps the pane moving from here on.
	c, out := runCLI(t, base, "wait", "--session", "wrev", "--quiet", "200", "--timeout", "800")
	if c != 0 {
		t.Fatalf("wait: exit %d\n%s", c, out)
	}
	var wd struct {
		OK   bool `json:"ok"`
		Data struct {
			Settled  bool   `json:"settled"`
			Reason   string `json:"reason"`
			Revision int    `json:"revision"`
		} `json:"data"`
	}
	if err := json.Unmarshal([]byte(strings.TrimSpace(out)), &wd); err != nil {
		t.Fatalf("wait stdout not JSON: %q (%v)", out, err)
	}
	if wd.Data.Settled || wd.Data.Reason != "timeout" || wd.Data.Revision != 2 {
		t.Fatalf("want timeout at revision 2, got %+v", wd.Data)
	}
}
