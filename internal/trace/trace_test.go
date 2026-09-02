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
	recorder.Header(context.Background(), RunHeader{Command: "chat", Started: time.Now()})
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
	if len(run) != 16 {
		t.Fatalf("run id %q is not sixteen hex characters", run)
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

// quiet points the state root at a temporary directory and puts the process
// back the way it was found: the switch off, no recorder open, and no run
// switched on by itself. Every test of the SCOPE needs that, because the scope
// is exactly the difference between one run and the process.
func quiet(t *testing.T) {
	t.Helper()
	t.Setenv("AFORGE_HOME", t.TempDir())
	runs.mutex.Lock()
	runs.by = nil
	runs.mutex.Unlock()
	enabled.mutex.Lock()
	enabled.by = nil
	enabled.mutex.Unlock()
	wasOn, wasAny := on.Load(), anyRun.Load()
	t.Cleanup(func() {
		on.Store(wasOn)
		anyRun.Store(wasAny)
		enabled.mutex.Lock()
		enabled.by = nil
		enabled.mutex.Unlock()
	})
	on.Store(false)
	anyRun.Store(false)
}

// THE RUN ID ON THE CONTEXT IS THE SCOPE OF THE SWITCH. This is the defect the
// two scopes exist to stop: one process holds two conversations, /debug is
// typed in one of them, and the other one's prompts and replies must not land
// in a folder its person never asked for.
func TestDebugRecordsOnlyTheRunItWasTypedIn(t *testing.T) {
	quiet(t)
	mine := WithRun(context.Background(), "aaaa1111")
	theirs := WithRun(context.Background(), "bbbb2222")

	if got := EnableRun(mine); got != "aaaa1111" {
		t.Fatalf("EnableRun named %q; want the run on the context", got)
	}
	if !EnabledRun(mine) {
		t.Fatalf("the run /debug was typed in does not report itself as recording")
	}
	if EnabledRun(theirs) {
		t.Fatalf("a second conversation reports itself as recording")
	}
	if Enabled() {
		t.Fatalf("/debug flipped the process-wide switch")
	}

	recorder := For(mine)
	if recorder == nil {
		t.Fatalf("the run /debug was typed in got no recorder")
	}
	if got := For(theirs); got != nil {
		t.Fatalf("a second conversation got the recorder %v; want nil", got)
	}
	if got, want := recorder.Folder(), Dir("aaaa1111"); got != want {
		t.Fatalf("folder %q, want %q", got, want)
	}
}

// A CONTEXT WITH NO RUN OF ITS OWN IS RECORDED ONLY BY THE PROCESS-WIDE SWITCH.
// The fallback to the process's run is what lets a headless errand's deeper
// layers record at all, and it must not be the road a /debug in one
// conversation takes into work that belongs to another.
func TestTheProcessRunFallbackDoesNotCarryOneRunsDebug(t *testing.T) {
	quiet(t)
	ctx := Begin(context.Background())
	if EnableRun(ctx) == "" {
		t.Fatalf("the door's own run could not be switched on")
	}
	if got := For(context.Background()); got != nil {
		t.Fatalf("a context with no run got the recorder %v after a /debug elsewhere; want nil", got)
	}
	// And with the process-wide switch on it records again, under the process's
	// own run — which is what --debug and AFORGE_DEBUG mean.
	Enable()
	recorder := For(context.Background())
	if recorder == nil {
		t.Fatalf("a context with no run got no recorder with the process switch on")
	}
	if got, want := recorder.Folder(), Dir(RunFrom(ctx)); got != want {
		t.Fatalf("the fallback recorded into %q, want the process's run %q", got, want)
	}
}

// THE PIN AND THE FLAG WERE GIVEN TO THE PROCESS, so every run it holds is
// recorded — each into its own folder, because two runs interleaving into one
// folder is the confusion the run id exists to end.
func TestTheProcessSwitchRecordsEveryRunInItsOwnFolder(t *testing.T) {
	quiet(t)
	on.Store(true)
	first, second := WithRun(context.Background(), "aaaa1111"), WithRun(context.Background(), "bbbb2222")
	one, two := For(first), For(second)
	if one == nil || two == nil {
		t.Fatalf("the process switch left a run without a recorder: %v %v", one, two)
	}
	if one.Folder() == two.Folder() {
		t.Fatalf("two runs share the folder %s", one.Folder())
	}
	OpenRun(first, RunHeader{Command: "chat", Started: time.Now()})
	OpenRun(second, RunHeader{Command: "chat", Started: time.Now()})
	for _, folder := range []string{one.Folder(), two.Folder()} {
		if _, err := os.Stat(filepath.Join(folder, RunFileName)); err != nil {
			t.Fatalf("no header under %s: %v", folder, err)
		}
	}
}

// A run switched on by itself writes its header and announces its own folder,
// which is what /debug promises the person who typed it.
func TestARunSwitchedOnByItselfWritesAndAnnouncesItsFolder(t *testing.T) {
	quiet(t)
	ctx := Begin(context.Background())
	EnableRun(ctx)
	OpenRun(ctx, RunHeader{Command: "chat", Model: "deepseek/deepseek-v4-flash", Started: time.Now()})
	folder := Dir(RunFrom(ctx))
	if _, err := os.Stat(filepath.Join(folder, RunFileName)); err != nil {
		t.Fatalf("no header under %s: %v", folder, err)
	}
	var out bytes.Buffer
	Announce(ctx, &out)
	if got, want := out.String(), "debug record: "+folder+"\n"; got != want {
		t.Fatalf("announcement: got %q, want %q", got, want)
	}
}

// A surface that never began a run has nothing to switch on, and saying so is
// the honest answer — the alternative is a record that goes nowhere.
func TestEnableRunOnAContextWithNoRunSwitchesNothingOn(t *testing.T) {
	quiet(t)
	if got := EnableRun(context.Background()); got != "" {
		t.Fatalf("EnableRun named %q on a context with no run", got)
	}
	if anyRun.Load() {
		t.Fatalf("a context with no run switched something on")
	}
}

// EVERY RECORD NAMES THE WORK IT BELONGED TO, not only the run. A long run is
// dozens of calls across a plan, and a folder in which they are told apart only
// by their timestamps is a folder somebody has to reconstruct the plan from.
func TestEveryRecordNamesTheNodeOnItsContext(t *testing.T) {
	ctx := fresh(t)
	work := WithNode(ctx, "task-3")
	recorder := For(work)
	recorder.Call(work, CallBody{CallID: "c0ffee01", Model: "m", Request: []byte(`{"a":1}`)})
	recorder.Tool(work, ToolEvent{CallID: "c0ffee01", Name: "bash", Status: "ok"})
	recorder.Decision(work, Decision{CallID: "c0ffee01", Kind: "lane", Choice: "frugal"})

	for _, document := range append(readEvents(t, recorder.Folder()), readCall(t, recorder.Folder(), "c0ffee01")) {
		if got := document["node"]; got != "task-3" {
			t.Fatalf("a %v record named the node %v, want task-3", document["kind"], got)
		}
	}
}

// The field wins over the context, because a feeder that names a node is closer
// to the work than the context is — a planner writing a record ABOUT a node it
// is not running is exactly that case.
func TestANamedNodeBeatsTheOneOnTheContext(t *testing.T) {
	ctx := fresh(t)
	work := WithNode(ctx, "task-3")
	recorder := For(work)
	recorder.Decision(work, Decision{Kind: "hedge", Node: "task-4"})
	events := readEvents(t, recorder.Folder())
	if got := events[len(events)-1]["node"]; got != "task-4" {
		t.Fatalf("the record named %v, want the node the feeder gave it", got)
	}
}

// THE EMPTINESS LAW APPLIES TO THE FILE TOO: a record nobody named a node for
// says nothing, rather than naming a node called "". And an empty node may not
// hide an outer one.
func TestARecordWithNoNodeSaysNothingAboutOne(t *testing.T) {
	ctx := fresh(t)
	recorder := For(ctx)
	recorder.Decision(ctx, Decision{Kind: "lane"})
	events := readEvents(t, recorder.Folder())
	if _, said := events[len(events)-1]["node"]; said {
		t.Fatalf("a record with no node still wrote one: %v", events[len(events)-1])
	}
	if got := NodeFrom(WithNode(WithNode(ctx, "task-3"), "  ")); got != "task-3" {
		t.Fatalf("an empty node hid the one already there: %q", got)
	}
}

// A CAPPED RUN SAYS SO EVEN WHEN THE FIRST RECORD IS THE ONE THAT DID NOT FIT.
// A call body is a file of its own, so a run whose first body is over the
// ceiling used to leave an empty folder and no line — indistinguishable from a
// run that recorded nothing at all.
func TestAFirstOversizedCallBodySaysTheRunWasCapped(t *testing.T) {
	ctx := fresh(t)
	t.Setenv(MaxMBEnvVar, "")
	recorder := For(ctx)
	recorder.max = 64
	recorder.Call(ctx, CallBody{CallID: "c0ffee01", Model: "m", Request: []byte(`{"prompt":"` + strings.Repeat("x", 4096) + `"}`)})
	if !recorder.Wrote() {
		t.Fatalf("a capped run wrote nothing at all")
	}
	events := readEvents(t, recorder.Folder())
	if len(events) != 1 || events[0]["kind"] != "capped" {
		t.Fatalf("the capped receipt is not the events file's one line: %v", events)
	}
	if _, err := os.Stat(filepath.Join(recorder.Folder(), CallsDirName, "c0ffee01.json")); !os.IsNotExist(err) {
		t.Fatalf("the body that did not fit was written anyway: %v", err)
	}
}

// TWO CALLS WITH NO ID DO NOT OVERWRITE EACH OTHER. The time alone names them,
// and two calls can land inside one millisecond — losing the very body somebody
// switched the record on for.
func TestTwoUnnamedCallsInTheSameMillisecondKeepBothBodies(t *testing.T) {
	ctx := fresh(t)
	recorder := For(ctx)
	recorder.Call(ctx, CallBody{Model: "m", Request: []byte(`{"which":"first"}`)})
	recorder.Call(ctx, CallBody{Model: "m", Request: []byte(`{"which":"second"}`)})
	entries, err := os.ReadDir(filepath.Join(recorder.Folder(), CallsDirName))
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 2 {
		t.Fatalf("two unnamed calls left %d files, want 2", len(entries))
	}
}

// readCall is one call body as a reader gets it: one file, named by the call id.
func readCall(t *testing.T, folder, call string) map[string]any {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(folder, CallsDirName, call+".json"))
	if err != nil {
		t.Fatal(err)
	}
	document := map[string]any{}
	if err := json.Unmarshal(raw, &document); err != nil {
		t.Fatal(err)
	}
	return document
}
