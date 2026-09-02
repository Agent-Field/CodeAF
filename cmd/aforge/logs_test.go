package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/aforge-v2/internal/calllog"
)

// TestMain switches the model-call log OFF for this package.
//
// The log is always on in the product, so a test binary that says nothing about
// it appends a row for every call these tests make — including the ones that
// deliberately dial a host that does not exist — into the developer's own
// ~/.aforge/logs/calls.jsonl, where it is noise in the one file somebody is
// reading to debug a real run. The tests below read fixtures instead.
func TestMain(m *testing.M) {
	if _, pinned := os.LookupEnv(calllog.EnvVar); !pinned {
		os.Setenv(calllog.EnvVar, calllog.OffValue)
	}
	os.Exit(m.Run())
}

// fixtureRows is the log a person could actually have, as it sits on disk: a
// refused attempt that taught the adapter something, the retry that landed —
// carrying the whole of what the row holds, the lane asked for and the lane
// that served, the wait before the first token, the deadline the watch was
// holding and the hedge that was fired against it — and one call that went out
// and has not come back. The first two belong to a run; the third carries no
// run at all, which is what almost every row on disk looks like today and is
// the case --run has to be honest about.
func fixtureRows() []string {
	return []string{
		`{"ts":"2026-08-28T21:12:41.000Z","id":"aaaaaaaa","phase":"start","run":"r-7f3a","tag":"compile","model":"z-ai/glm-5.3-flash","lane":"deepinfra","effort":"low","max_tokens":10240,"messages":2,"attempt":1}`,
		`{"ts":"2026-08-28T21:12:41.200Z","id":"aaaaaaaa","run":"r-7f3a","tag":"compile","model":"z-ai/glm-5.3-flash","lane":"deepinfra","served":"deepinfra","effort":"low","max_tokens":10240,"messages":2,"attempt":1,"status":400,"ms":200,"error":"Reasoning is mandatory for this endpoint","learned":["reasoning_mandatory"]}`,
		`{"ts":"2026-08-28T21:12:41.300Z","id":"bbbbbbbb","phase":"start","run":"r-7f3a","tag":"compile","model":"z-ai/glm-5.3-flash","effort":"low","max_tokens":10240,"messages":2,"attempt":2}`,
		`{"ts":"2026-08-28T21:12:53.000Z","id":"bbbbbbbb","run":"r-7f3a","tag":"compile","model":"z-ai/glm-5.3-flash","lane":"auto","served":"coreweave","effort":"low","max_tokens":10240,"messages":2,"attempt":2,"status":200,"ms":12700,"ttft_ms":420,"deadline_ms":8000,"action":"hedge","arms":2,"hedged":true,"waste_usd":0.0012,"finish":"stop","completion_tokens":466,"cost":0.0003}`,
		`{"ts":"2026-08-28T21:13:04.000Z","id":"cccccccc","phase":"start","tag":"leaf","node":"build","model":"z-ai/glm-5.3","lane":"novita","effort":"high","max_tokens":65536,"deadline_ms":30000,"messages":9,"tools":11,"attempt":1}`,
	}
}

// fixtureLog writes those rows where the reader will find them.
func fixtureLog(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "calls.jsonl")
	lines := strings.Join(fixtureRows(), "\n") + "\n"
	if err := os.WriteFile(path, []byte(lines), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

// stoppedClock is a fixed now, so an in-flight call's age is a fact rather than
// a race against the test runner.
func stoppedClock(t *testing.T) func() time.Time {
	t.Helper()
	moment, err := time.Parse(time.RFC3339, "2026-08-28T21:16:16Z")
	if err != nil {
		t.Fatal(err)
	}
	return func() time.Time { return moment }
}

func TestLogsRendersTheLastCallsOnePerLine(t *testing.T) {
	var out strings.Builder
	if err := runLogsWith([]string{"--tail", "2"}, &out, fixtureLog(t), stoppedClock(t)); err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSpace(out.String()), "\n")
	// The path first, then the calls: a person who has to be told where to look
	// should not have to run a second command to find out.
	if len(lines) != 3 {
		t.Fatalf("--tail 2 should print the path and two calls; got %d lines:\n%s", len(lines), out.String())
	}
	if !strings.HasSuffix(lines[0], "calls.jsonl") {
		t.Errorf("the first line should be the path: %q", lines[0])
	}
	answered, inFlight := lines[1], lines[2]
	for _, want := range []string{"21:12:53", "compile", "z-ai/glm-5.3-flash", "low", "max 10240",
		"→ 200", "12.7s", "stop", "466 tok", "$0.0003"} {
		if !strings.Contains(answered, want) {
			t.Errorf("the answered call's line is missing %q: %q", want, answered)
		}
	}
	// A call that went out and has not come back is the line this command
	// exists for.
	for _, want := range []string{"leaf", "#build", "max 65536", "⋯ in flight", "3m12s"} {
		if !strings.Contains(inFlight, want) {
			t.Errorf("the in-flight call's line is missing %q: %q", want, inFlight)
		}
	}
}

func TestLogsShowsARefusalAndWhatItTaught(t *testing.T) {
	var out strings.Builder
	if err := runLogsWith([]string{"--tail", "3"}, &out, fixtureLog(t), stoppedClock(t)); err != nil {
		t.Fatal(err)
	}
	body := out.String()
	for _, want := range []string{"→ 400", "0.2s", "Reasoning is mandatory", "learned reasoning_mandatory"} {
		if !strings.Contains(body, want) {
			t.Errorf("the refused attempt's line is missing %q:\n%s", want, body)
		}
	}
	// A start whose end has arrived is not a line of its own: two rows about one
	// attempt would double the log a person reads.
	if strings.Count(body, "in flight") != 1 {
		t.Errorf("only the unmatched start is in flight:\n%s", body)
	}
}

func TestLogsPathPrintsOnlyThePath(t *testing.T) {
	path := fixtureLog(t)
	var out strings.Builder
	if err := runLogsWith([]string{"--path"}, &out, path, stoppedClock(t)); err != nil {
		t.Fatal(err)
	}
	if got := strings.TrimSpace(out.String()); got != path {
		t.Fatalf("--path printed %q, want just %q", got, path)
	}
}

func TestLogsSaysSoWhenTheLogIsSwitchedOff(t *testing.T) {
	var out strings.Builder
	if err := runLogsWith(nil, &out, "", stoppedClock(t)); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "off") {
		t.Fatalf("an off log should say so rather than printing nothing: %q", out.String())
	}
}

func TestLogsOnAMachineThatHasNeverCalledAModelPrintsThePathAndNothingElse(t *testing.T) {
	var out strings.Builder
	absent := filepath.Join(t.TempDir(), "logs", "calls.jsonl")
	if err := runLogsWith(nil, &out, absent, stoppedClock(t)); err != nil {
		t.Fatalf("a missing log is not an error: %v", err)
	}
	if got := strings.TrimSpace(out.String()); got != absent {
		t.Fatalf("nothing has been logged, so there is nothing to draw: %q", got)
	}
}

// TestLogsShowsTheWholeRowAndNotHalfOfIt is the law of this reader: a field
// that is on the record is on the line. The half that used to be dropped — who
// was asked, who answered, the wait before the first token, the deadline the
// watch held, and what was done about a silence — is the half somebody opens
// this command to see.
func TestLogsShowsTheWholeRowAndNotHalfOfIt(t *testing.T) {
	var out strings.Builder
	if err := runLogsWith(nil, &out, fixtureLog(t), stoppedClock(t)); err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSpace(out.String()), "\n")
	if len(lines) != 4 {
		t.Fatalf("the path and three calls; got %d lines:\n%s", len(lines), out.String())
	}
	refused, answered, inFlight := lines[1], lines[2], lines[3]
	// The router overrode the preference on the call that landed, and that
	// difference is the single most useful thing on the line.
	for _, want := range []string{"auto→coreweave", "first token 0.4s", "deadline 8.0s",
		"acted hedge", "2 arms", "hedged", "waste $0.0012"} {
		if !strings.Contains(answered, want) {
			t.Errorf("the answered call's line is missing %q: %q", want, answered)
		}
	}
	// Asked for and served by the same machine is one name and not an arrow
	// pointing at itself.
	if !strings.Contains(refused, "deepinfra") || strings.Contains(refused, "deepinfra→") {
		t.Errorf("a lane that served what was asked for prints once: %q", refused)
	}
	// A deadline was set on the call still in flight and never reached, which
	// is the row that says the deadline was set in the right place.
	if !strings.Contains(inFlight, "deadline 30.0s") {
		t.Errorf("the in-flight call's deadline is missing: %q", inFlight)
	}
	// The emptiness law: the retry's start row carried no deadline and no lane,
	// so nothing stands in for them.
	if strings.Contains(refused, "acted") || strings.Contains(refused, "arms") {
		t.Errorf("a call nothing was done about prints nothing about it: %q", refused)
	}
}

func TestLogsFiltersByTagModelAndNode(t *testing.T) {
	path := fixtureLog(t)
	for _, probe := range []struct {
		flags   []string
		want    []string
		notWant []string
	}{
		{[]string{"--tag", "leaf"}, []string{"leaf"}, []string{"compile"}},
		{[]string{"--model", "z-ai/glm-5.3-flash"}, []string{"compile"}, []string{"#build"}},
		{[]string{"--node", "build"}, []string{"#build"}, []string{"compile"}},
		// Combined, and by AND: a tag that is not on that node keeps nothing.
		{[]string{"--tag", "compile", "--node", "build"}, nil, []string{"compile", "leaf"}},
	} {
		var out strings.Builder
		if err := runLogsWith(probe.flags, &out, path, stoppedClock(t)); err != nil {
			t.Fatalf("%v: %v", probe.flags, err)
		}
		body := out.String()
		for _, want := range probe.want {
			if !strings.Contains(body, want) {
				t.Errorf("%v should have kept %q:\n%s", probe.flags, want, body)
			}
		}
		for _, gone := range probe.notWant {
			if strings.Contains(body, gone) {
				t.Errorf("%v should have dropped %q:\n%s", probe.flags, gone, body)
			}
		}
	}
}

// TestLogsByCallIDShowsBothRowsOfThatAttempt: naming one call is asking for its
// whole story, and the story is what went out as well as what came back.
func TestLogsByCallIDShowsBothRowsOfThatAttempt(t *testing.T) {
	var out strings.Builder
	if err := runLogsWith([]string{"--id", "aaaaaaaa"}, &out, fixtureLog(t), stoppedClock(t)); err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSpace(out.String()), "\n")
	if len(lines) != 3 {
		t.Fatalf("the path and both rows of one attempt; got %d:\n%s", len(lines), out.String())
	}
	// The row that went out, and NOT called still in flight when its answer is
	// on the line underneath it.
	if !strings.Contains(lines[1], "sent") || strings.Contains(lines[1], "in flight") {
		t.Errorf("the first row is the one that went out: %q", lines[1])
	}
	if !strings.Contains(lines[2], "→ 400") {
		t.Errorf("the second row is the one that came back: %q", lines[2])
	}
}

// TestLogsFiltersByRunAndSaysNothingForARowThatHasNoRun holds the honest half
// of --run: the run id is not written yet, and a row without one never matches
// rather than matching everything.
func TestLogsFiltersByRunAndSaysNothingForARowThatHasNoRun(t *testing.T) {
	path := fixtureLog(t)
	var kept strings.Builder
	if err := runLogsWith([]string{"--run", "r-7f3a"}, &kept, path, stoppedClock(t)); err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSpace(kept.String()), "\n")
	if len(lines) != 3 {
		t.Fatalf("the path and that run's two answered calls; got %d:\n%s", len(lines), kept.String())
	}
	// The leaf call carries no run at all, so it is not this run's.
	if strings.Contains(kept.String(), "#build") {
		t.Errorf("a row with no run of its own is not in any run:\n%s", kept.String())
	}
	var missing strings.Builder
	if err := runLogsWith([]string{"--run", "r-none"}, &missing, path, stoppedClock(t)); err != nil {
		t.Fatal(err)
	}
	if got := strings.TrimSpace(missing.String()); got != path {
		t.Errorf("a run nothing belongs to prints the path and no calls: %q", got)
	}
}

// TestLogsJSONPassesTheRowsThroughUntouched: another program reads what comes
// out, so the bytes are the file's own and the path header is not in front of
// them.
func TestLogsJSONPassesTheRowsThroughUntouched(t *testing.T) {
	var out strings.Builder
	if err := runLogsWith([]string{"--json", "--tag", "compile"}, &out, fixtureLog(t), stoppedClock(t)); err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSpace(out.String()), "\n")
	want := fixtureRows()[:4]
	if len(lines) != len(want) {
		t.Fatalf("every compile row, one per line; got %d:\n%s", len(lines), out.String())
	}
	for index, line := range lines {
		if line != want[index] {
			t.Errorf("row %d was rewritten:\n got %s\nwant %s", index, line, want[index])
		}
	}
}

// TestLogsBodySaysSoWhenNothingWasRecorded is what --body does today for almost
// every call: the trace and the failures folder are both written by work that
// has not landed, and saying there is nothing is the truthful answer.
func TestLogsBodySaysSoWhenNothingWasRecorded(t *testing.T) {
	var out strings.Builder
	if err := runLogsWith([]string{"--body", "aaaaaaaa"}, &out, fixtureLog(t), stoppedClock(t)); err != nil {
		t.Fatal(err)
	}
	if got := strings.TrimSpace(out.String()); got != "no body recorded for aaaaaaaa" {
		t.Fatalf("--body with nothing on disk said %q", got)
	}
}

// TestLogsBodyPrintsWhatIsOnDisk covers both places a body can be: the trace a
// switched-on run wrote, and the failures folder the one call that went wrong
// is kept in.
func TestLogsBodyPrintsWhatIsOnDisk(t *testing.T) {
	path := fixtureLog(t)
	dir := filepath.Dir(path)
	for _, probe := range []struct {
		where []string
		id    string
		body  string
	}{
		{[]string{"trace", "r-7f3a", "calls"}, "bbbbbbbb", `{"request":"who are you","response":"a model"}`},
		{[]string{"failures"}, "aaaaaaaa", `{"error":"Reasoning is mandatory for this endpoint"}`},
	} {
		folder := filepath.Join(append([]string{dir}, probe.where...)...)
		if err := os.MkdirAll(folder, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(folder, probe.id+".json"), []byte(probe.body), 0o644); err != nil {
			t.Fatal(err)
		}
		var out strings.Builder
		if err := runLogsWith([]string{"--body", probe.id}, &out, path, stoppedClock(t)); err != nil {
			t.Fatal(err)
		}
		if got := strings.TrimSpace(out.String()); got != probe.body {
			t.Errorf("--body %s printed %q, want %q", probe.id, got, probe.body)
		}
	}
}

// TestLogsTailCountsWhatSurvivedTheFilter: --tail is the last N of what a
// person asked to see, not the last N of the file with the rest thrown away
// afterwards — the second reading would show nothing at all on a busy log.
func TestLogsTailCountsWhatSurvivedTheFilter(t *testing.T) {
	var out strings.Builder
	if err := runLogsWith([]string{"--tag", "compile", "--tail", "1"}, &out, fixtureLog(t), stoppedClock(t)); err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSpace(out.String()), "\n")
	if len(lines) != 2 || !strings.Contains(lines[1], "→ 200") {
		t.Fatalf("the newest compile call and nothing else:\n%s", out.String())
	}
}
