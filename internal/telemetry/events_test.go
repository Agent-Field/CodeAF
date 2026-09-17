package telemetry

import (
	"encoding/json"
	"reflect"
	"regexp"
	"strings"
	"testing"
	"time"
)

// The wire's hash shapes, from the contract: an event id is 32 lowercase hex
// characters, an install or session hash is 64, a fault fingerprint is 16.
var (
	reEventID      = regexp.MustCompile(`^[0-9a-f]{32}$`)
	reIdentityHash = regexp.MustCompile(`^[0-9a-f]{64}$`)
	reFingerprint  = regexp.MustCompile(`^[0-9a-f]{16}$`)
)

// sentinels are the private things a person's machine is full of. Every
// constructor is fed them wherever a value could possibly flow, and the
// privacy law below holds that not one of them reaches marshalled bytes.
var sentinels = []string{
	"/Users/santosh/secret-project/leaky.go", // a path
	"person@example.com",                     // an email
	"hades.corp.internal",                    // a hostname
	"santosh",                                // a username
	"secret-project",                         // a repo name
	"why does my deploy keep failing after the auth change",      // a prompt
	"Sure - here is the rewritten handler you asked for",         // a model reply
	"func handler(w http.ResponseWriter) { w.WriteHeader(204) }", // code
	"sk-ant-api03-NOT-A-REAL-KEY",                                // an API key
	"deepseek-r1-distill",                                        // a model slug
	"fatal: authentication failed for 'https://git.internal/x'",  // raw error text
	"panic: the deploy froze mid-write",                          // a panic message
	"192.168.13.45",                                              // an IP
	"CODEAF_HOME=/Users/santosh/secret-project",                  // an env value
}

func TestBucketCountEdges(t *testing.T) {
	cases := []struct {
		count int
		want  string
	}{
		{-7, BucketZero},
		{0, BucketZero},
		{1, BucketOne},
		{2, BucketTwo5},
		{5, BucketTwo5},
		{6, BucketSix},
		{20, BucketSix},
		{21, BucketTwo1},
		{100, BucketTwo1},
		{101, Bucket100},
		{1 << 20, Bucket100},
	}
	for _, test := range cases {
		if got := BucketCount(test.count); got != test.want {
			t.Errorf("BucketCount(%d) = %q, want %q", test.count, got, test.want)
		}
	}
}

func TestBucketCostEdges(t *testing.T) {
	cases := []struct {
		cost float64
		want string
	}{
		{-1, CostZero},
		{0, CostZero},
		{0.001, CostUnder1c},
		{0.0099, CostUnder1c},
		{0.01, Cost1cTo10c},
		{0.05, Cost1cTo10c},
		{0.0999, Cost1cTo10c},
		{0.1, Cost10cTo1},
		{0.99, Cost10cTo1},
		{1, Cost1To10},
		{9.99, Cost1To10},
		{10, Cost10Plus},
		{1234.5, Cost10Plus},
	}
	for _, test := range cases {
		if got := BucketCost(test.cost); got != test.want {
			t.Errorf("BucketCost(%v) = %q, want %q", test.cost, got, test.want)
		}
	}
}

func TestBucketDurationEdges(t *testing.T) {
	cases := []struct {
		duration time.Duration
		want     string
	}{
		{-5 * time.Second, DurationUnder1m},
		{0, DurationUnder1m},
		{30 * time.Second, DurationUnder1m},
		{59 * time.Second, DurationUnder1m},
		{time.Minute, Duration1To5m},
		{4*time.Minute + 59*time.Second, Duration1To5m},
		{5 * time.Minute, Duration5To30m},
		{29 * time.Minute, Duration5To30m},
		{30 * time.Minute, Duration30mTo2h},
		{time.Hour + 59*time.Minute, Duration30mTo2h},
		{2 * time.Hour, Duration2hPlus},
		{7 * time.Hour, Duration2hPlus},
	}
	for _, test := range cases {
		if got := BucketDuration(test.duration); got != test.want {
			t.Errorf("BucketDuration(%v) = %q, want %q", test.duration, got, test.want)
		}
	}
}

func TestFirstRunCarriesNoSession(t *testing.T) {
	testHome(t)
	event := FirstRun(testNow)
	if event.Name != "first_run" {
		t.Fatalf("event name %q, want first_run", event.Name)
	}
	if event.SessionHash != "" {
		t.Error("first_run carries no session hash: an install has no run yet")
	}
	raw, err := jsonMarshal(event)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), "session_id_hash") {
		t.Error("the first_run wire row must not carry session_id_hash")
	}
}

func TestEventWireShapeFollowsTheContract(t *testing.T) {
	testHome(t)
	started := SessionStarted(ModeTask, true, "session-abc", testNow)
	raw, err := jsonMarshal(started)
	if err != nil {
		t.Fatal(err)
	}
	var row map[string]json.RawMessage
	if err := json.Unmarshal(raw, &row); err != nil {
		t.Fatalf("the wire row must parse: %v", err)
	}
	for _, key := range []string{"event_name", "event_id", "install_id_hash", "session_id_hash", "event_time", "props"} {
		if _, ok := row[key]; !ok {
			t.Errorf("the wire row is missing %q", key)
		}
	}
	for _, key := range []string{"session_id", "install_id", "prompt", "user", "model"} {
		if _, ok := row[key]; ok {
			t.Errorf("the wire row must not carry %q", key)
		}
	}
	if started.Name != "session_started" {
		t.Errorf("event name %q, want session_started", started.Name)
	}
	if !reEventID.MatchString(started.ID) {
		t.Errorf("event_id %q, want 32 lowercase hex characters", started.ID)
	}
	if !reIdentityHash.MatchString(started.InstallHash) {
		t.Errorf("install_id_hash %q, want 64 lowercase hex characters", started.InstallHash)
	}
	if !reIdentityHash.MatchString(started.SessionHash) {
		t.Errorf("session_id_hash %q, want 64 lowercase hex characters", started.SessionHash)
	}
	if parsed, err := time.Parse(time.RFC3339, started.Time); err != nil || !parsed.Equal(testNow) {
		t.Errorf("event_time = %q (%v), want RFC3339 UTC at the build time", started.Time, err)
	}
	first, err := jsonMarshal(FirstRun(testNow))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(first), "session_id_hash") {
		t.Error("first_run must not carry session_id_hash on the wire")
	}
}

func TestSessionStartedCollapsesUnknownModes(t *testing.T) {
	testHome(t)
	event := SessionStarted(Mode("interactive"), false, "session-1", testNow)
	if got := event.Props["mode"]; got != string(ModeChat) {
		t.Errorf("an unknown mode became %v, want chat", got)
	}
	if got := SessionStarted(ModeTask, false, "s", testNow).Props["mode"]; got != "task" {
		t.Errorf("task became %v", got)
	}
	if got := SessionStarted(ModeChat, true, "s", testNow).Props["resumed"]; got != true {
		t.Errorf("resumed became %v, want true", got)
	}
}

func TestSessionEndedBucketsAndClamps(t *testing.T) {
	testHome(t)
	event := SessionEnded(Mode("interactive"), SessionStats{
		Duration:         90 * time.Minute,
		Turns:            3,
		ModelCalls:       9,
		ModelCallsFailed: 1,
		ToolCalls:        21,
		ToolCallsFailed:  101,
		CostUSD:          0.42,
		StopReason:       "blew up",
		ExitCode:         99,
	}, "session-clamp", testNow)
	want := map[string]any{
		"mode":               "chat",
		"duration":           Duration30mTo2h,
		"turns":              BucketTwo5,
		"model_calls":        BucketSix,
		"model_calls_failed": BucketOne,
		"tool_calls":         BucketTwo1,
		"tool_calls_failed":  Bucket100,
		"cost_usd":           Cost10cTo1,
		"stop_reason":        StopUnknown,
		"exit_code":          5,
	}
	for key, expected := range want {
		if event.Props[key] != expected {
			t.Errorf("%s = %v, want %v", key, event.Props[key], expected)
		}
	}
	clamped := SessionEnded(ModeChat, SessionStats{StopReason: StopBudget, ExitCode: -3}, "s", testNow)
	if clamped.Props["exit_code"] != 0 {
		t.Errorf("a negative exit code became %v, want 0", clamped.Props["exit_code"])
	}
	if got := clamped.Props["stop_reason"]; got != StopBudget {
		t.Errorf("a recognised stop reason became %v, want %q", got, StopBudget)
	}
	if got := SessionEnded(ModeChat, SessionStats{CostUSD: 0.004}, "s", testNow).Props["cost_usd"]; got != CostUnder1c {
		t.Errorf("a sub-cent cost became %v, want %q", got, CostUnder1c)
	}
}

func TestFaultEventBucketsScopeAndMode(t *testing.T) {
	testHome(t)
	event := FaultEvent(Fault{Mode: "exec", Scope: "somewhere-else", Stack: nil}, "session-f", testNow)
	if got := event.Props["mode"]; got != "other" {
		t.Errorf("an unknown fault mode became %v, want other", got)
	}
	if got := event.Props["scope"]; got != ScopeMain {
		t.Errorf("an unknown scope became %v, want main", got)
	}
	if got := FaultEvent(Fault{Mode: string(ModeTask), Scope: ScopeGoroutine, Stack: []byte("github.com/Agent-Field/codeaf/internal/x.One")}, "s", testNow).Props["scope"]; got != ScopeGoroutine {
		t.Errorf("goroutine scope became %v", got)
	}
	if got := FaultEvent(Fault{Mode: string(ModeTask), Scope: ScopeSurface, Stack: nil}, "s", testNow).Props["mode"]; got != "task" {
		t.Errorf("task mode became %v", got)
	}
	if !reFingerprint.MatchString(event.Props["fingerprint"].(string)) {
		t.Errorf("fingerprint %v, want 16 lowercase hex characters", event.Props["fingerprint"])
	}
}

// TestAllowlistedPropsMatchTheConstructors holds the two halves of the
// allowlist together: what the constructors emit and what the table allows
// must be the same set, event by event.
func TestAllowlistedPropsMatchTheConstructors(t *testing.T) {
	testHome(t)
	events := []Event{
		FirstRun(testNow),
		SessionStarted(ModeChat, false, "session-1", testNow),
		SessionEnded(ModeChat, SessionStats{Duration: time.Minute, Turns: 1, CostUSD: 1}, "session-1", testNow),
		FaultEvent(Fault{Mode: string(ModeChat), Scope: ScopeMain}, "session-1", testNow),
	}
	if len(events) != len(AllowlistedEvents()) {
		t.Fatalf("the constructors cover %d events, the allowlist names %d", len(events), len(AllowlistedEvents()))
	}
	for _, event := range events {
		allowed := map[string]bool{}
		for _, name := range AllowlistedProps(event.Name) {
			allowed[name] = true
		}
		if len(event.Props) != len(allowed) {
			t.Errorf("%s: the constructor emits %d props, the allowlist names %d", event.Name, len(event.Props), len(allowed))
		}
		for key := range event.Props {
			if !allowed[key] {
				t.Errorf("%s: prop %q is not on the allowlist", event.Name, key)
			}
		}
	}
	if got := AllowlistedProps("nosuchevent"); len(got) != 0 {
		t.Errorf("AllowlistedProps(nosuchevent) = %v, want none", got)
	}
	wantEvents := map[string]bool{"first_run": true, "session_started": true, "session_ended": true, "fault": true}
	gotEvents := map[string]bool{}
	for _, name := range AllowlistedEvents() {
		gotEvents[name] = true
	}
	if !reflect.DeepEqual(gotEvents, wantEvents) {
		t.Errorf("AllowlistedEvents = %v, want the contract's four", AllowlistedEvents())
	}
	common := map[string]bool{}
	for _, name := range CommonPropNames() {
		common[name] = true
	}
	if len(common) != 6 {
		t.Errorf("the contract fixes six common props, CommonPropNames lists %d", len(common))
	}
}

// TestNothingSentinelEverReachesTheWire is the privacy law: every constructor
// is built from inputs stuffed with the private things a person's machine is
// full of, and no marshalled event may carry any of them.
func TestNothingSentinelEverReachesTheWire(t *testing.T) {
	testHome(t)
	stack := []byte(strings.Join([]string{
		"panic: " + sentinels[11] + " for " + sentinels[1],
		"goroutine 1 [running]:",
		"github.com/Agent-Field/codeaf/internal/session.(*Runner).step(0xc000123, 0x2)",
		"\t" + sentinels[0] + ":412 +0x88",
		"github.com/Agent-Field/codeaf/internal/model.(*Client).call(...)",
		"\t/Users/" + sentinels[3] + "/" + sentinels[4] + "/client.go:97",
		"github.com/Agent-Field/codeaf/cmd/codeaf.runChat()",
		"\t/Users/" + sentinels[3] + "/main.go:201",
	}, "\n"))
	events := []Event{
		FirstRun(testNow),
		SessionStarted(Mode(sentinels[2]), true, "session "+sentinels[1], testNow),
		SessionEnded(ModeChat, SessionStats{
			Duration: 42 * time.Minute, Turns: 3, ModelCalls: 9, ModelCallsFailed: 1,
			ToolCalls: 12, ToolCallsFailed: 2, CostUSD: 0.4, StopReason: StopDone, ExitCode: 0,
		}, "session "+sentinels[0], testNow),
		FaultEvent(Fault{Mode: sentinels[9], Scope: sentinels[2], Stack: stack}, "session "+sentinels[8], testNow),
	}
	for _, event := range events {
		raw, err := jsonMarshal(event)
		if err != nil {
			t.Fatalf("%s: %v", event.Name, err)
		}
		for _, sentinel := range sentinels {
			if strings.Contains(string(raw), sentinel) {
				t.Errorf("%s carries %q on the wire", event.Name, sentinel)
			}
		}
	}
}
