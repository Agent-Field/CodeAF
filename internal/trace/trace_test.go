package trace

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

// fresh points the state root at a temporary directory, turns the switch on,
// and forgets every recorder this process has opened, so one test cannot see
// another's folder. It returns the run's context.
func fresh(t *testing.T) context.Context {
	t.Helper()
	t.Setenv("AFORGE_HOME", t.TempDir())
	runs.mutex.Lock()
	runs.by = nil
	runs.mutex.Unlock()
	was := on.Load()
	t.Cleanup(func() { on.Store(was) })
	on.Store(true)
	return Begin(context.Background())
}

// A NIL RECORDER IS THE OFF SWITCH, and every method must survive it: the whole
// point of the shape is that a feeder site calls unconditionally.
func TestEveryMethodIsANoOpOnANilRecorder(t *testing.T) {
	var recorder *Recorder
	recorder.Call(context.Background(), CallBody{CallID: "abcd1234"})
	recorder.Tool(context.Background(), ToolEvent{Name: "bash"})
	recorder.Decision(context.Background(), Decision{Kind: "lane"})
	if recorder.Folder() != "" || recorder.Wrote() {
		t.Fatalf("a nil recorder reported a folder or a write")
	}
}

func TestForIsNilWhenTheSwitchIsOff(t *testing.T) {
	t.Setenv("AFORGE_HOME", t.TempDir())
	was := on.Load()
	t.Cleanup(func() { on.Store(was) })
	on.Store(false)
	if got := For(Begin(context.Background())); got != nil {
		t.Fatalf("For returned %v with the switch off; want nil", got)
	}
}

func TestForIsNilWithNoRunToBelongTo(t *testing.T) {
	t.Setenv("AFORGE_HOME", t.TempDir())
	was := on.Load()
	t.Cleanup(func() { on.Store(was) })
	on.Store(true)
	// A context with no run, in a process where no door has begun one: there is
	// nothing to join a record to, so there is no recorder.
	processRun.Store("")
	if got := For(context.Background()); got != nil {
		t.Fatalf("For returned %v with no run; want nil", got)
	}
}

func TestTheSwitchReadsBothItsSpellings(t *testing.T) {
	for _, test := range []struct {
		name  string
		env   map[string]string
		wants bool
	}{
		{name: "unset", env: map[string]string{}},
		{name: "the new pin", env: map[string]string{EnvVar: "1"}, wants: true},
		{name: "the old bodies pin", env: map[string]string{BodiesEnvVar: "1"}, wants: true},
		{name: "any value at all", env: map[string]string{EnvVar: "yes"}, wants: true},
		{name: "zero", env: map[string]string{EnvVar: "0"}},
		{name: "false", env: map[string]string{EnvVar: "FALSE"}},
		{name: "off", env: map[string]string{EnvVar: "off"}},
		{name: "blank", env: map[string]string{EnvVar: "   "}},
	} {
		t.Run(test.name, func(t *testing.T) {
			if got := envEnabled(func(name string) string { return test.env[name] }); got != test.wants {
				t.Fatalf("envEnabled(%v) = %v, want %v", test.env, got, test.wants)
			}
		})
	}
}

func TestEnableTurnsTheRecordOnForTheRestOfTheProcess(t *testing.T) {
	was := on.Load()
	t.Cleanup(func() { on.Store(was) })
	on.Store(false)
	if Enabled() {
		t.Fatalf("the switch was on before anything asked for it")
	}
	Enable()
	if !Enabled() {
		t.Fatalf("Enable did not turn the record on")
	}
}

// A run that records nothing leaves NOTHING BEHIND, which is what makes the
// switch safe to leave on in a shell profile.
func TestASwitchedOnRunThatRecordsNothingLeavesNoFolder(t *testing.T) {
	ctx := fresh(t)
	recorder := For(ctx)
	if recorder == nil {
		t.Fatalf("no recorder with the switch on")
	}
	if _, err := os.Stat(filepath.Dir(recorder.Folder())); !os.IsNotExist(err) {
		t.Fatalf("the trace root exists before anything was recorded: %v", err)
	}
	var out bytes.Buffer
	Announce(ctx, &out)
	if out.Len() != 0 {
		t.Fatalf("a run that wrote nothing announced %q", out.String())
	}
}

func TestARunWritesItsCallsToolsAndDecisionsUnderItsOwnID(t *testing.T) {
	ctx := fresh(t)
	run := RunFrom(ctx)
	if len(run) != 8 {
		t.Fatalf("run id %q is not eight hex characters", run)
	}
	recorder := For(ctx)
	recorder.Call(ctx, CallBody{
		CallID:   "c0ffee01",
		Model:    "deepseek/deepseek-v4-flash",
		Request:  []byte(`{"model":"deepseek/deepseek-v4-flash","messages":[]}`),
		Response: []byte(`{"choices":[{"message":{"content":"hello there friend"}}]}`),
		Finish:   "stop",
	})
	recorder.Tool(ctx, ToolEvent{
		CallID: "c0ffee01", Name: "bash", Args: `{"command":"ls"}`, Result: "notes.txt",
		Started: time.Now(), Duration: 42 * time.Millisecond, Status: "ok",
	})
	recorder.Decision(ctx, Decision{
		CallID: "c0ffee01", Kind: "lane", Subject: "deepseek/deepseek-v4-flash",
		Choice: "fireworks", Reason: "the pinned lane answered first", Alternatives: []string{"together"},
	})

	folder := filepath.Join(os.Getenv("AFORGE_HOME"), DirName, TraceDirName, run)
	if got := recorder.Folder(); got != folder {
		t.Fatalf("folder: got %q, want %q", got, folder)
	}
	body := filepath.Join(folder, CallsDirName, "c0ffee01.json")
	raw, err := os.ReadFile(body)
	if err != nil {
		t.Fatalf("call body: %v", err)
	}
	var call map[string]any
	if err := json.Unmarshal(raw, &call); err != nil {
		t.Fatalf("call body is not one JSON document: %v", err)
	}
	if call["run"] != run || call["call"] != "c0ffee01" || call["finish"] != "stop" {
		t.Fatalf("call body does not name its run, call and finish: %v", call)
	}
	if _, ok := call["request"].(map[string]any); !ok {
		t.Fatalf("the request was not written as JSON: %v", call["request"])
	}

	events := readEvents(t, folder)
	if len(events) != 2 {
		t.Fatalf("events: got %d lines, want 2:\n%v", len(events), events)
	}
	if events[0]["kind"] != "tool" || events[0]["tool"] != "bash" || events[0]["ms"] != float64(42) {
		t.Fatalf("tool line: %v", events[0])
	}
	if events[1]["kind"] != "decision" || events[1]["reason"] != "the pinned lane answered first" {
		t.Fatalf("decision line: %v", events[1])
	}
	for i, event := range events {
		if event["run"] != run {
			t.Fatalf("line %d does not name the run: %v", i, event)
		}
		if _, ok := event["ts"].(string); !ok {
			t.Fatalf("line %d has no timestamp: %v", i, event)
		}
	}

	var out bytes.Buffer
	Announce(ctx, &out)
	if got, want := out.String(), "debug record: "+folder+"\n"; got != want {
		t.Fatalf("announcement: got %q, want %q", got, want)
	}
}

// THE RECORD IS A PERSON'S OWN DATA, so the folder is theirs and nobody else's.
func TestTheFolderIsPrivateAndSoAreItsFiles(t *testing.T) {
	ctx := fresh(t)
	recorder := For(ctx)
	recorder.Decision(ctx, Decision{Kind: "lane", Choice: "fireworks"})
	recorder.Call(ctx, CallBody{CallID: "aaaa1111", Request: []byte(`{}`)})

	folder := recorder.Folder()
	for path, want := range map[string]os.FileMode{
		folder:                                0o700,
		filepath.Join(folder, CallsDirName):   0o700,
		filepath.Join(folder, EventsFileName): 0o600,
		filepath.Join(folder, CallsDirName, "aaaa1111.json"): 0o600,
	} {
		info, err := os.Stat(path)
		if err != nil {
			t.Fatalf("stat %s: %v", path, err)
		}
		if got := info.Mode().Perm(); got != want {
			t.Fatalf("%s is %o, want %o", path, got, want)
		}
	}
}

func TestARunThatReachesTheCapSaysSoAndStops(t *testing.T) {
	ctx := fresh(t)
	recorder := For(ctx)
	// A ceiling in bytes rather than the megabytes the pin takes, so the test
	// reaches it in three records instead of thousands.
	recorder.max = 300
	for i := 0; i < 40; i++ {
		recorder.Decision(ctx, Decision{Kind: "lane", Choice: "fireworks", Reason: "the pinned lane answered first"})
	}
	events := readEvents(t, recorder.Folder())
	if len(events) < 2 {
		t.Fatalf("the cap stopped the record before it started: %v", events)
	}
	last := events[len(events)-1]
	if last["kind"] != "capped" {
		t.Fatalf("the record does not end by saying it was capped: %v", last)
	}
	for _, event := range events[:len(events)-1] {
		if event["kind"] != "decision" {
			t.Fatalf("a record before the cap line was not kept: %v", event)
		}
	}
	// And it STAYS stopped: the run keeps what it had rather than growing.
	before := len(events)
	recorder.Decision(ctx, Decision{Kind: "lane", Choice: "together"})
	if after := len(readEvents(t, recorder.Folder())); after != before {
		t.Fatalf("the record grew after the cap: %d lines, was %d", after, before)
	}
}

func TestTheOldestRunFolderIsPrunedWhole(t *testing.T) {
	home := t.TempDir()
	t.Setenv("AFORGE_HOME", home)
	t.Setenv(KeepEnvVar, "2")
	was := on.Load()
	t.Cleanup(func() { on.Store(was) })
	on.Store(true)

	root := filepath.Join(home, DirName, TraceDirName)
	var folders []string
	for i := 0; i < 3; i++ {
		runs.mutex.Lock()
		runs.by = nil
		runs.mutex.Unlock()
		ctx := Begin(context.Background())
		recorder := For(ctx)
		recorder.Decision(ctx, Decision{Kind: "lane", Choice: "fireworks"})
		folders = append(folders, recorder.Folder())
		// Folder mtimes on a fast machine are otherwise identical, and "oldest"
		// then means nothing.
		stamp := time.Now().Add(time.Duration(i-3) * time.Hour)
		if err := os.Chtimes(recorder.Folder(), stamp, stamp); err != nil {
			t.Fatalf("chtimes: %v", err)
		}
	}
	entries, err := os.ReadDir(root)
	if err != nil {
		t.Fatalf("read trace root: %v", err)
	}
	if len(entries) != 2 {
		t.Fatalf("kept %d run folders, want 2", len(entries))
	}
	if _, err := os.Stat(folders[0]); !os.IsNotExist(err) {
		t.Fatalf("the oldest run folder survived: %v", err)
	}
	for _, folder := range folders[1:] {
		if _, err := os.Stat(folder); err != nil {
			t.Fatalf("a kept run folder is gone: %v", err)
		}
	}
}

// ONE APPENDER WITH ONE MUTEX: records written from several goroutines may
// arrive in any order, but never halfway through each other's line.
func TestConcurrentRecordsNeverInterleave(t *testing.T) {
	ctx := fresh(t)
	recorder := For(ctx)
	var wait sync.WaitGroup
	for writer := 0; writer < 16; writer++ {
		wait.Add(1)
		go func(writer int) {
			defer wait.Done()
			for i := 0; i < 32; i++ {
				recorder.Tool(ctx, ToolEvent{
					Name:   "bash",
					Args:   strings.Repeat("x", 200),
					Status: "ok",
				})
			}
		}(writer)
	}
	wait.Wait()
	events := readEvents(t, recorder.Folder())
	if len(events) != 16*32 {
		t.Fatalf("got %d lines, want %d", len(events), 16*32)
	}
}

func TestScrubTakesTheCredentialOutOfEveryShapeItKnows(t *testing.T) {
	Secret("hunter2-the-configured-key")
	for _, test := range []struct {
		name string
		in   string
		out  string
	}{
		{"a header field", `{"Authorization":"Bearer sk-or-v1-abcdef123456"}`, `{"credential":"[redacted]"}`},
		{"an api-key field", `{"x-api-key":"abcdef123456"}`, `{"credential":"[redacted]"}`},
		{"an underscored field", `{"api_key":"abcdef123456"}`, `{"credential":"[redacted]"}`},
		{"a bearer in prose", `curl -H "auth: Bearer abcdef123456789"`, `curl -H "auth: Bearer [redacted]"`},
		{"a key by its shape", `the key sk-or-v1-abcdef123456 leaked`, `the key [redacted] leaked`},
		{"the configured key itself", `token=hunter2-the-configured-key`, `token=[redacted]`},
		{"an ordinary body", `{"messages":[{"role":"user","content":"hello"}]}`, `{"messages":[{"role":"user","content":"hello"}]}`},
	} {
		t.Run(test.name, func(t *testing.T) {
			if got := string(Scrub([]byte(test.in))); got != test.out {
				t.Fatalf("Scrub(%q) = %q, want %q", test.in, got, test.out)
			}
		})
	}
}

func TestARecordedBodyCarriesNoCredential(t *testing.T) {
	ctx := fresh(t)
	Secret("hunter2-the-configured-key")
	recorder := For(ctx)
	recorder.Call(ctx, CallBody{
		CallID:  "bbbb2222",
		Request: []byte(`{"authorization":"Bearer sk-or-v1-abcdef123456","key":"hunter2-the-configured-key"}`),
		Error:   "401 from the endpoint with sk-or-v1-abcdef123456",
	})
	raw, err := os.ReadFile(filepath.Join(recorder.Folder(), CallsDirName, "bbbb2222.json"))
	if err != nil {
		t.Fatalf("call body: %v", err)
	}
	for _, forbidden := range []string{"sk-or-v1-abcdef123456", "hunter2-the-configured-key", "Bearer sk-", "authorization"} {
		if bytes.Contains(raw, []byte(forbidden)) {
			t.Fatalf("the record holds %q:\n%s", forbidden, raw)
		}
	}
}

func TestTheTwoCeilingsAreOverridableByTheirPins(t *testing.T) {
	getenv := func(pairs map[string]string) func(string) string {
		return func(name string) string { return pairs[name] }
	}
	if got := maxRunBytes(getenv(map[string]string{})); got != MaxRunBytes {
		t.Fatalf("default cap: got %d, want %d", got, MaxRunBytes)
	}
	if got := maxRunBytes(getenv(map[string]string{MaxMBEnvVar: "4"})); got != 4<<20 {
		t.Fatalf("pinned cap: got %d, want %d", got, 4<<20)
	}
	if got := maxRunBytes(getenv(map[string]string{MaxMBEnvVar: "nonsense"})); got != MaxRunBytes {
		t.Fatalf("a typo moved the cap to %d", got)
	}
	if got := keepRuns(getenv(map[string]string{})); got != KeepRuns {
		t.Fatalf("default retention: got %d, want %d", got, KeepRuns)
	}
	if got := keepRuns(getenv(map[string]string{KeepEnvVar: "3"})); got != 3 {
		t.Fatalf("pinned retention: got %d, want 3", got)
	}
	if got := keepRuns(getenv(map[string]string{KeepEnvVar: "0"})); got != KeepRuns {
		t.Fatalf("a zero retention would delete every folder: got %d", got)
	}
}

// A WRITE FAILURE IS NEVER A FAILED RUN: it silences this run's record after one
// line, and the run carries on.
func TestAWriteFailureSilencesTheRunAfterOneLine(t *testing.T) {
	ctx := fresh(t)
	recorder := For(ctx)
	var complaint bytes.Buffer
	was := stderr
	stderr = &complaint
	t.Cleanup(func() { stderr = was })
	// A file where the folder has to go: nothing under it can be created.
	if err := os.MkdirAll(filepath.Dir(recorder.Folder()), 0o700); err != nil {
		t.Fatalf("prepare: %v", err)
	}
	if err := os.WriteFile(recorder.Folder(), []byte("not a folder"), 0o600); err != nil {
		t.Fatalf("prepare: %v", err)
	}
	recorder.Decision(ctx, Decision{Kind: "lane", Choice: "fireworks"})
	recorder.Decision(ctx, Decision{Kind: "lane", Choice: "together"})
	recorder.Tool(ctx, ToolEvent{Name: "bash", Status: "ok"})
	if got := strings.Count(complaint.String(), "\n"); got != 1 {
		t.Fatalf("a silenced record complained %d times:\n%s", got, complaint.String())
	}
	if !strings.Contains(complaint.String(), recorder.Folder()) {
		t.Fatalf("the complaint does not name the path: %q", complaint.String())
	}
}

// readEvents reads the run's appended file back as documents, failing the test
// on any line that is not one whole JSON object — which is how a line torn in
// half by a second goroutine shows up.
func readEvents(t *testing.T, folder string) []map[string]any {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(folder, EventsFileName))
	if err != nil {
		t.Fatalf("events: %v", err)
	}
	var events []map[string]any
	for i, line := range strings.Split(strings.TrimSuffix(string(raw), "\n"), "\n") {
		if line == "" {
			continue
		}
		var event map[string]any
		if err := json.Unmarshal([]byte(line), &event); err != nil {
			t.Fatalf("line %d is not one JSON object (%v): %s", i+1, err, line)
		}
		events = append(events, event)
	}
	return events
}

// THE DOOR'S HEADER IS WHAT CREATES THE FOLDER, and it is the one file a
// switched-on run always has.
func TestTheDoorsHeaderOpensTheFolderAndNamesTheRun(t *testing.T) {
	ctx := fresh(t)
	started := time.Now()
	OpenRun(ctx, RunHeader{
		Command:   "chat",
		Model:     "deepseek/deepseek-v4-flash",
		Build:     "dev+69029c4e",
		Workspace: "/home/someone/project",
		Started:   started,
	})
	folder := Dir(RunFrom(ctx))
	raw, err := os.ReadFile(filepath.Join(folder, RunFileName))
	if err != nil {
		t.Fatalf("run.json: %v", err)
	}
	var header map[string]any
	if err := json.Unmarshal(raw, &header); err != nil {
		t.Fatalf("run.json is not one JSON document: %v", err)
	}
	for field, want := range map[string]string{
		"kind":      "run",
		"run":       RunFrom(ctx),
		"command":   "chat",
		"model":     "deepseek/deepseek-v4-flash",
		"build":     "dev+69029c4e",
		"workspace": "/home/someone/project",
		"started":   started.Format(timeLayout),
	} {
		if header[field] != want {
			t.Fatalf("run.json %s: got %v, want %q", field, header[field], want)
		}
	}
	for path, want := range map[string]os.FileMode{
		folder:                             0o700,
		filepath.Join(folder, RunFileName): 0o600,
	} {
		info, err := os.Stat(path)
		if err != nil {
			t.Fatalf("stat %s: %v", path, err)
		}
		if got := info.Mode().Perm(); got != want {
			t.Fatalf("%s is %o, want %o", path, got, want)
		}
	}
	// And the door can now say where the record went, which is the whole reason
	// the header is written before anything else happens.
	var out bytes.Buffer
	Announce(ctx, &out)
	if got, want := out.String(), "debug record: "+folder+"\n"; got != want {
		t.Fatalf("announcement: got %q, want %q", got, want)
	}
}

// WITH THE SWITCH OFF THE DOOR CREATES NOTHING, which is what makes --debug
// safe to leave out rather than something a person has to remember to clean up.
func TestADoorWithTheSwitchOffCreatesNoFolder(t *testing.T) {
	home := t.TempDir()
	t.Setenv("AFORGE_HOME", home)
	runs.mutex.Lock()
	runs.by = nil
	runs.mutex.Unlock()
	was := on.Load()
	t.Cleanup(func() { on.Store(was) })
	on.Store(false)

	ctx := Begin(context.Background())
	OpenRun(ctx, RunHeader{Command: "do", Model: "deepseek/deepseek-v4-flash", Started: time.Now()})
	if _, err := os.Stat(filepath.Join(home, DirName, TraceDirName)); !os.IsNotExist(err) {
		t.Fatalf("a run with the record off left a trace root: %v", err)
	}
	var out bytes.Buffer
	Announce(ctx, &out)
	if out.Len() != 0 {
		t.Fatalf("a run with the record off announced %q", out.String())
	}
}
