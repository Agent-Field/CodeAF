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

// fixtureLog writes a log a person could actually have: a call that landed, a
// refused attempt that taught the adapter something, and one that went out and
// has not come back.
func fixtureLog(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "calls.jsonl")
	lines := strings.Join([]string{
		`{"ts":"2026-08-28T21:12:41.000Z","id":"aaaaaaaa","phase":"start","tag":"compile","model":"z-ai/glm-5.3-flash","effort":"low","max_tokens":10240,"messages":2,"attempt":1}`,
		`{"ts":"2026-08-28T21:12:41.200Z","id":"aaaaaaaa","tag":"compile","model":"z-ai/glm-5.3-flash","effort":"low","max_tokens":10240,"messages":2,"attempt":1,"status":400,"ms":200,"error":"Reasoning is mandatory for this endpoint","learned":["reasoning_mandatory"]}`,
		`{"ts":"2026-08-28T21:12:41.300Z","id":"bbbbbbbb","phase":"start","tag":"compile","model":"z-ai/glm-5.3-flash","effort":"low","max_tokens":10240,"messages":2,"attempt":2}`,
		`{"ts":"2026-08-28T21:12:53.000Z","id":"bbbbbbbb","tag":"compile","model":"z-ai/glm-5.3-flash","effort":"low","max_tokens":10240,"messages":2,"attempt":2,"status":200,"ms":12700,"finish":"stop","completion_tokens":466,"cost":0.0003}`,
		`{"ts":"2026-08-28T21:13:04.000Z","id":"cccccccc","phase":"start","tag":"leaf","node":"build","model":"z-ai/glm-5.3","effort":"high","max_tokens":65536,"messages":9,"tools":11,"attempt":1}`,
	}, "\n") + "\n"
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
