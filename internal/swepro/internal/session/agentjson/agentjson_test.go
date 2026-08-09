package agentjson

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Agent-Field/aforge-v2/internal/swepro/internal/baked"
)

type testDecision struct {
	Decision string `json:"decision"`
	Reason   string `json:"reason"`
}

func strictDecisionSchema() Schema[testDecision] {
	return SchemaFunc[testDecision](func(raw json.RawMessage) Validation[testDecision] {
		var object map[string]json.RawMessage
		if err := json.Unmarshal(raw, &object); err != nil {
			return Validation[testDecision]{Issues: []Issue{{Message: err.Error()}}}
		}
		issues := []Issue{}
		for key := range object {
			if key != "decision" && key != "reason" {
				issues = append(issues, Issue{Message: `Unrecognized key: "` + key + `"`})
			}
		}
		var value testDecision
		if err := json.Unmarshal(raw, &value); err != nil {
			return Validation[testDecision]{Issues: []Issue{{Message: err.Error()}}}
		}
		if value.Decision != "yes" && value.Decision != "no" {
			issues = append(issues, Issue{
				Path:    []string{"decision"},
				Message: `Invalid option: expected one of "yes"|"no"`,
			})
		}
		if value.Reason == "" {
			issues = append(issues, Issue{
				Path:    []string{"reason"},
				Message: "Too small: expected string to have >=1 characters",
			})
		}
		return Validation[testDecision]{Data: value, Issues: issues}
	})
}

type recordingClient struct {
	mu       sync.Mutex
	requests []Request
	run      func(context.Context, Request) error
}

func (c *recordingClient) Run(ctx context.Context, request Request) error {
	c.mu.Lock()
	c.requests = append(c.requests, request)
	c.mu.Unlock()
	if c.run != nil {
		return c.run(ctx, request)
	}
	return nil
}

func (c *recordingClient) snapshot() []Request {
	c.mu.Lock()
	defer c.mu.Unlock()
	return append([]Request(nil), c.requests...)
}

func testInput(path string) Input[testDecision] {
	fallback := testDecision{Decision: "no", Reason: "fallback"}
	return Input[testDecision]{
		Agent: "merger", ParentSessionID: "ses_parent",
		Workspace: "/workspace", TaskPrompt: "decide", OutputPath: path,
		Schema: strictDecisionSchema(), Fallback: &fallback,
	}
}

func testDeps(client Client) Dependencies {
	counter := 0
	return Dependencies{
		Resolver: ResolverFunc(func(tier baked.Tier) []string {
			if tier != baked.TierHigh {
				panic("unexpected tier " + tier)
			}
			return []string{"openrouter/vendor/model"}
		}),
		Client: client,
		NewID: func(prefix string) string {
			counter++
			return prefix + "_" + intString(counter)
		},
	}
}

func writeDecision(t *testing.T, path string, value testDecision) {
	t.Helper()
	raw, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, raw, 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestDispatchFirstTryConsumesArtifactAndCarriesBakedAgent(t *testing.T) {
	path := filepath.Join(t.TempDir(), "nested", "decision.json")
	client := &recordingClient{}
	client.run = func(_ context.Context, request Request) error {
		if request.AgentMarkdown == "" || !strings.Contains(request.AgentMarkdown, "<Role>") {
			t.Fatal("baked merger markdown missing from request")
		}
		if request.Model != (Model{ModelID: "vendor/model", ProviderID: "openrouter"}) {
			t.Fatalf("model = %#v", request.Model)
		}
		writeDecision(t, request.OutputPath, testDecision{Decision: "yes", Reason: "clean"})
		return errors.New("prompt failed after writing") // folded; valid artifact still wins
	}
	result, err := DispatchJSON(context.Background(), testInput(path), testDeps(client))
	if err != nil {
		t.Fatal(err)
	}
	if result.Data.Decision != "yes" || !result.FirstTry || result.UsedFallback {
		t.Fatalf("result = %#v", result)
	}
	if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("successful artifact was not consumed: %v", err)
	}
	requests := client.snapshot()
	if len(requests) != 1 || requests[0].Phase != PhaseMain {
		t.Fatalf("requests = %#v", requests)
	}
}

func TestDispatchParseFixUsesSameSessionAndWatcherDeduplicates(t *testing.T) {
	path := filepath.Join(t.TempDir(), "decision.json")
	input := testInput(path)
	input.MaxRetries = intPointer(0)
	input.ParseFixBudget = 1
	input.PreserveOnSuccess = true
	var mainSession string
	client := &recordingClient{}
	client.run = func(_ context.Context, request Request) error {
		switch request.Phase {
		case PhaseMain:
			mainSession = request.SessionID
			if err := os.WriteFile(request.OutputPath, []byte("{"), 0o600); err != nil {
				t.Fatal(err)
			}
			first := request.BetweenStepReminder()
			second := request.BetweenStepReminder()
			if !strings.Contains(first, "JSON Parse error: Expected '}'") || second != "" {
				t.Fatalf("watcher first=%q second=%q", first, second)
			}
		case PhaseParseFix:
			if request.SessionID != mainSession {
				t.Fatalf("parse fix moved sessions: %s -> %s", mainSession, request.SessionID)
			}
			if !strings.Contains(request.Reminder, "PARSE-FIX MODE (1/1)") {
				t.Fatalf("parse reminder = %q", request.Reminder)
			}
			writeDecision(t, request.OutputPath, testDecision{Decision: "yes", Reason: "fixed"})
		default:
			t.Fatalf("unexpected phase %s", request.Phase)
		}
		return nil
	}
	result, err := DispatchJSON(context.Background(), input, testDeps(client))
	if err != nil {
		t.Fatal(err)
	}
	if result.Data.Reason != "fixed" || !result.FirstTry {
		t.Fatalf("result = %#v", result)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("preserved artifact missing: %v", err)
	}
}

func TestDispatchSchemaFixAndFreshRetrySessionRules(t *testing.T) {
	t.Run("schema fix same session", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "decision.json")
		input := testInput(path)
		input.MaxRetries = intPointer(0)
		input.SchemaFixBudget = 1
		var session string
		client := &recordingClient{run: func(_ context.Context, request Request) error {
			switch request.Phase {
			case PhaseMain:
				session = request.SessionID
				writeDecision(t, request.OutputPath, testDecision{Decision: "maybe", Reason: ""})
			case PhaseSchemaFix:
				if request.SessionID != session {
					t.Fatal("schema fix did not continue in same session")
				}
				if !strings.Contains(request.Reminder, "  - decision: Invalid option") ||
					!strings.Contains(request.Reminder, "  - reason: Too small") {
					t.Fatalf("schema reminder = %q", request.Reminder)
				}
				writeDecision(t, request.OutputPath, testDecision{Decision: "no", Reason: "fixed"})
			}
			return nil
		}}
		result, err := DispatchJSON(context.Background(), input, testDeps(client))
		if err != nil || result.Data.Reason != "fixed" {
			t.Fatalf("result=%#v err=%v", result, err)
		}
	})

	t.Run("outer retry fresh session", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "decision.json")
		input := testInput(path)
		input.MaxRetries = intPointer(1)
		var first string
		client := &recordingClient{run: func(_ context.Context, request Request) error {
			if request.Phase != PhaseMain {
				t.Fatalf("unexpected phase %s", request.Phase)
			}
			if first == "" {
				first = request.SessionID
				return nil
			}
			if request.SessionID == first {
				t.Fatal("fresh retry reused child session")
			}
			if !strings.Contains(request.Reminder, "no output file at "+request.OutputPath) {
				t.Fatalf("retry reminder = %q", request.Reminder)
			}
			writeDecision(t, request.OutputPath, testDecision{Decision: "yes", Reason: "retry"})
			return nil
		}}
		result, err := DispatchJSON(context.Background(), input, testDeps(client))
		if err != nil || result.FirstTry || result.Data.Reason != "retry" {
			t.Fatalf("result=%#v err=%v", result, err)
		}
	})
}

func TestSchemaFixThatBreaksJSONKeepsStaleSchemaErrorForRetry(t *testing.T) {
	path := filepath.Join(t.TempDir(), "decision.json")
	input := testInput(path)
	input.MaxRetries = intPointer(1)
	input.SchemaFixBudget = 1
	client := &recordingClient{}
	call := 0
	client.run = func(_ context.Context, request Request) error {
		call++
		switch call {
		case 1:
			writeDecision(t, request.OutputPath, testDecision{Decision: "maybe", Reason: ""})
		case 2:
			if request.Phase != PhaseSchemaFix {
				t.Fatalf("second phase = %s", request.Phase)
			}
			if err := os.WriteFile(request.OutputPath, []byte("{"), 0o600); err != nil {
				t.Fatal(err)
			}
		case 3:
			if request.Phase != PhaseMain {
				t.Fatalf("third phase = %s", request.Phase)
			}
			if !strings.Contains(request.Reminder, "decision: Invalid option") ||
				!strings.Contains(request.Reminder, "reason: Too small") ||
				strings.Contains(request.Reminder, "JSON Parse error") {
				t.Fatalf("retry did not keep stale schema error: %q", request.Reminder)
			}
			writeDecision(t, request.OutputPath, testDecision{Decision: "yes", Reason: "retry"})
		default:
			t.Fatalf("unexpected call %d", call)
		}
		return nil
	}
	result, err := DispatchJSON(context.Background(), input, testDeps(client))
	if err != nil || result.FirstTry || result.Data.Reason != "retry" {
		t.Fatalf("result=%#v err=%v", result, err)
	}
}

func TestDispatchClientFailuresShareFallbackBranch(t *testing.T) {
	cases := map[string]func(context.Context, Request) error{
		"error": func(context.Context, Request) error { return errors.New("boom") },
		"panic": func(context.Context, Request) error { panic("boom") },
		"timeout": func(ctx context.Context, _ Request) error {
			<-ctx.Done()
			return ctx.Err()
		},
	}
	for name, run := range cases {
		t.Run(name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "decision.json")
			input := testInput(path)
			input.MaxRetries = intPointer(0)
			if name == "timeout" {
				ms := int64(1)
				input.TimeoutMS = &ms
			}
			result, err := DispatchJSON(
				context.Background(), input, testDeps(&recordingClient{run: run}),
			)
			if err != nil || !result.UsedFallback || result.Data.Reason != "fallback" {
				t.Fatalf("result=%#v err=%v", result, err)
			}
		})
	}
}

func TestDispatchWaitsForCanceledClientBeforeReadingArtifact(t *testing.T) {
	// A canceled prompt may still finish its context-aware cleanup and publish
	// an artifact. Dispatch must not inspect or unlink the file concurrently.
	previousProcs := runtime.GOMAXPROCS(1)
	defer runtime.GOMAXPROCS(previousProcs)

	path := filepath.Join(t.TempDir(), "decision.json")
	input := testInput(path)
	input.MaxRetries = intPointer(0)
	var expire context.CancelFunc
	client := &recordingClient{run: func(_ context.Context, request Request) error {
		expire()
		runtime.Gosched()
		writeDecision(t, request.OutputPath, testDecision{Decision: "yes", Reason: "completed cleanup"})
		return context.Canceled
	}}
	deps := testDeps(client)
	deps.TimeoutContext = func(
		ctx context.Context, _ time.Duration,
	) (context.Context, context.CancelFunc) {
		callCtx, cancel := context.WithCancel(ctx)
		expire = cancel
		return callCtx, cancel
	}

	result, err := DispatchJSON(context.Background(), input, deps)
	if err != nil || result.UsedFallback || result.Data.Reason != "completed cleanup" {
		t.Fatalf("result=%#v err=%v", result, err)
	}
}

func TestDispatchDeletesStaleFileBeforeFirstCall(t *testing.T) {
	path := filepath.Join(t.TempDir(), "decision.json")
	writeDecision(t, path, testDecision{Decision: "yes", Reason: "stale"})
	input := testInput(path)
	input.MaxRetries = intPointer(0)
	result, err := DispatchJSON(
		context.Background(), input,
		testDeps(&recordingClient{run: func(_ context.Context, _ Request) error {
			if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
				t.Fatalf("stale file visible to client: %v", err)
			}
			return nil
		}}),
	)
	if err != nil || !result.UsedFallback {
		t.Fatalf("result=%#v err=%v", result, err)
	}
}

func TestDispatchNoModelAndNoFallbackErrors(t *testing.T) {
	path := filepath.Join(t.TempDir(), "decision.json")
	input := testInput(path)
	deps := testDeps(&recordingClient{})
	deps.Resolver = ResolverFunc(func(baked.Tier) []string { return nil })
	result, err := DispatchJSON(context.Background(), input, deps)
	if err != nil || !result.UsedFallback {
		t.Fatalf("fallback result=%#v err=%v", result, err)
	}
	input.Fallback = nil
	_, err = DispatchJSON(context.Background(), input, deps)
	if err == nil || err.Error() != "agent-json: no model available for tier=high, agent=merger" {
		t.Fatalf("error = %v", err)
	}
}

func TestDispatchUnknownBakedAgentFailsBeforeClient(t *testing.T) {
	path := filepath.Join(t.TempDir(), "decision.json")
	input := testInput(path)
	input.Agent = "definitely-missing"
	_, err := DispatchJSON(context.Background(), input, testDeps(&recordingClient{}))
	if err == nil || err.Error() != "agent-json: baked agent not found: definitely-missing" {
		t.Fatalf("error = %v", err)
	}
}

func TestFormatSchemaErrorsLimitAndCompactRootPath(t *testing.T) {
	issues := []Issue{
		{Message: "root"},
		{Path: []string{"a", "0"}, Message: "bad"},
		{Path: []string{"c"}, Message: "worse"},
	}
	if got := FormatSchemaErrors(issues, 2); got != "  - (root): root\n  - a.0: bad" {
		t.Fatalf("FormatSchemaErrors = %q", got)
	}
	if got := CompactSchemaErrors(issues); got != ": root; a.0: bad; c: worse" {
		t.Fatalf("CompactSchemaErrors = %q", got)
	}
}

func TestToolSettingsMarshalPreservesSpreadOrder(t *testing.T) {
	settings := MergeTools([]ToolSetting{
		{Name: "custom", Enabled: true},
		{Name: "edit", Enabled: true},
		{Name: "custom", Enabled: false},
	})
	raw, err := json.Marshal(settings)
	if err != nil {
		t.Fatal(err)
	}
	want := `{"edit":true,"apply_patch":false,"task":false,"plandb":false,"todowrite":false,"custom":false}`
	if string(raw) != want {
		t.Fatalf("settings = %s", raw)
	}
}

func intPointer(value int) *int { return &value }

func TestInvokeClientHonorsParentCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	start := time.Now()
	err := invokeClient(ctx, ClientFunc(func(ctx context.Context, _ Request) error {
		<-ctx.Done()
		return ctx.Err()
	}), Request{}, time.Hour, nil)
	if !errors.Is(err, context.Canceled) || time.Since(start) > time.Second {
		t.Fatalf("err=%v elapsed=%s", err, time.Since(start))
	}
}

func TestInvokeClientBoundsNonCooperativeCancellation(t *testing.T) {
	release := make(chan struct{})
	defer close(release)
	timeout := 5 * time.Millisecond
	start := time.Now()
	done := make(chan error, 1)
	go func() {
		done <- invokeClient(context.Background(), ClientFunc(func(
			context.Context, Request,
		) error {
			<-release
			return nil
		}), Request{}, timeout, nil)
	}()
	select {
	case err := <-done:
		elapsed := time.Since(start)
		if !errors.Is(err, context.DeadlineExceeded) {
			t.Fatalf("invokeClient error = %v, want deadline exceeded", err)
		}
		if elapsed > 750*time.Millisecond {
			t.Fatalf("invokeClient exceeded timeout plus bounded grace: %s", elapsed)
		}
	case <-time.After(750 * time.Millisecond):
		t.Fatal("invokeClient wedged on a client that ignores cancellation")
	}
}

func TestDispatchLeavesArtifactOwnedByNonCooperativeClient(t *testing.T) {
	path := filepath.Join(t.TempDir(), "decision.json")
	input := testInput(path)
	input.MaxRetries = intPointer(0)
	timeoutMS := int64(5)
	input.TimeoutMS = &timeoutMS
	release := make(chan struct{})
	started := make(chan struct{})
	artifact := make(chan string, 1)
	client := &recordingClient{run: func(_ context.Context, request Request) error {
		if err := os.WriteFile(
			request.OutputPath,
			[]byte(`{"decision":"yes","reason":"still being written"}`),
			0o600,
		); err != nil {
			return err
		}
		artifact <- request.OutputPath
		close(started)
		<-release
		return nil
	}}

	type dispatchResult struct {
		result Result[testDecision]
		err    error
	}
	done := make(chan dispatchResult, 1)
	go func() {
		result, err := DispatchJSON(context.Background(), input, testDeps(client))
		done <- dispatchResult{result: result, err: err}
	}()
	<-started
	select {
	case got := <-done:
		if got.err != nil || !got.result.UsedFallback {
			t.Fatalf("dispatch result=%#v err=%v", got.result, got.err)
		}
		ownedPath := <-artifact
		if _, err := os.Stat(ownedPath); err != nil {
			t.Fatalf("live client's artifact was inspected or unlinked: %v", err)
		}
		close(release)
		deadline := time.Now().Add(time.Second)
		for ActiveOrphanedClients() != 0 && time.Now().Before(deadline) {
			time.Sleep(time.Millisecond)
		}
	case <-time.After(750 * time.Millisecond):
		t.Fatal("DispatchJSON wedged on a client that ignores cancellation")
	}
}

func TestDispatchIsolatesAndReapsOrphanedClientArtifact(t *testing.T) {
	// Validation contracts C6.1-C6.4: a timed-out first dispatch may publish
	// late, but its private artifact cannot corrupt the next dispatch. When it
	// eventually returns, the tracked orphan and its artifact are both reaped.
	path := filepath.Join(t.TempDir(), "decision.json")
	input := testInput(path)
	input.MaxRetries = intPointer(0)
	input.PreserveOnSuccess = true
	timeoutMS := int64(5)
	input.TimeoutMS = &timeoutMS

	baselineOrphans := ActiveOrphanedClients()
	firstStarted := make(chan string, 1)
	releaseFirst := make(chan struct{})
	var released atomic.Bool
	t.Cleanup(func() {
		if released.CompareAndSwap(false, true) {
			close(releaseFirst)
		}
	})
	var calls atomic.Int32
	client := &recordingClient{run: func(_ context.Context, request Request) error {
		switch calls.Add(1) {
		case 1:
			firstStarted <- request.OutputPath
			<-releaseFirst // deliberately ignore cancellation past the grace window
			return os.WriteFile(
				request.OutputPath,
				[]byte(`{"decision":"yes","reason":"stale first dispatch"}`),
				0o600,
			)
		case 2:
			return os.WriteFile(
				request.OutputPath,
				[]byte(`{"decision":"no","reason":"current second dispatch"}`),
				0o600,
			)
		default:
			return errors.New("unexpected client call")
		}
	}}

	type dispatchResult struct {
		result Result[testDecision]
		err    error
	}
	firstDone := make(chan dispatchResult, 1)
	go func() {
		result, err := DispatchJSON(context.Background(), input, testDeps(client))
		firstDone <- dispatchResult{result: result, err: err}
	}()
	firstArtifact := <-firstStarted
	select {
	case got := <-firstDone:
		if got.err != nil || !got.result.UsedFallback {
			t.Fatalf("first dispatch result=%#v err=%v", got.result, got.err)
		}
	case <-time.After(750 * time.Millisecond):
		t.Fatal("first dispatch did not enforce its deadline")
	}
	if got := ActiveOrphanedClients(); got != baselineOrphans+1 {
		t.Fatalf("active orphans = %d, want %d", got, baselineOrphans+1)
	}

	second, err := DispatchJSON(context.Background(), input, testDeps(client))
	if err != nil || second.UsedFallback || second.Data.Reason != "current second dispatch" {
		t.Fatalf("second dispatch result=%#v err=%v", second, err)
	}
	requests := client.snapshot()
	if len(requests) != 2 || requests[0].OutputPath == requests[1].OutputPath ||
		requests[1].OutputPath == path {
		t.Fatalf("dispatch artifact paths were not private: %#v", requests)
	}

	if released.CompareAndSwap(false, true) {
		close(releaseFirst)
	}
	deadline := time.Now().Add(time.Second)
	for ActiveOrphanedClients() != baselineOrphans && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	if got := ActiveOrphanedClients(); got != baselineOrphans {
		t.Fatalf("orphan was not reaped: active=%d baseline=%d", got, baselineOrphans)
	}
	if _, err := os.Stat(firstArtifact); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("orphan artifact was not discarded: %v", err)
	}
	raw, err := os.ReadFile(path)
	if err != nil || !strings.Contains(string(raw), "current second dispatch") {
		t.Fatalf("preserved artifact was corrupted: raw=%q err=%v", raw, err)
	}
}
