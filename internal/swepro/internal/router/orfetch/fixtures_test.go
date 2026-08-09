package orfetch

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Agent-Field/swe-pro-go/internal/jscompat"
	"github.com/Agent-Field/swe-pro-go/internal/router/ratehead"
)

// Replays testdata/fixtures.json, produced by tools/fixtures/gen-orfetch.ts
// from the REAL src/router/openrouter-fetch.ts (plus adaptive.ts's
// isLikelyTimeout and openrouter-rate-headers.ts). The gate is BYTE equality
// between jscompat.Stringify(goResult) and the JSON.stringify the TS run
// recorded.
//
// The `fetch` cases replay against a virtual clock: the generator swapped
// global setTimeout/clearTimeout for a recorder and this test swaps
// timerFactory for the same thing, so the fixture's `ops` transcript pins arm
// order, every re-arm, and the double-clear the post-cancel pump performs.

type fixtureLine struct {
	Name     string `json:"name"`
	Fn       string `json:"fn"`
	ArgsJSON string `json:"args_json"`
	OutJSON  string `json:"out_json"`
}

// ── fn: constants ────────────────────────────────────────────────────────

type constantsOut struct {
	FirstContentDefaultMs jscompat.JSNumber `json:"FIRST_CONTENT_DEFAULT_MS"`
	ContentIdleDefaultMs  jscompat.JSNumber `json:"CONTENT_IDLE_DEFAULT_MS"`
}

// ── fn: classify ─────────────────────────────────────────────────────────

type classifyArgs struct {
	Text string `json:"text"`
}

type classifyOut struct {
	Text            string `json:"text"`
	IsLikelyTimeout bool   `json:"isLikelyTimeout"`
}

// isLikelyTimeoutMirror mirrors adaptive.ts's isLikelyTimeout for the half
// that this module's messages exercise: errorText(err).toLowerCase() then a
// substring scan of ["timeout", "timed out", "deadline exceeded"]. errorText
// of a cause-less Error is exactly err.message, so matching on the message is
// exact.
//
// The `(err as {name?}).name === "AbortError"` short-circuit has no Go
// analogue and is deliberately absent: openrouter-fetch.ts only ever aborts
// with `new Error(msg)`, so it can never produce an AbortError-named value
// itself. Modelling that branch belongs to the sibling adaptive port.
//
// This lives in the test, not the package: isLikelyTimeout is adaptive.ts's
// function, and orfetch must not pre-empt that package's surface.
func isLikelyTimeoutMirror(message string) bool {
	text := strings.ToLower(message)
	for _, needle := range []string{"timeout", "timed out", "deadline exceeded"} {
		if strings.Contains(text, needle) {
			return true
		}
	}
	return false
}

// ── fn: rateHeaders ──────────────────────────────────────────────────────

type rateArgs struct {
	Headers map[string]string `json:"headers"`
}

// The rateHeaders cases are the cross-check between this package's consumer
// contract and internal/router/ratehead's independent port of the same TS
// module: both are pinned against the REAL openrouter-rate-headers.ts here.
type rateOut struct {
	Info *ratehead.RateLimitInfo `json:"info"`
	Low  bool                    `json:"low"`
}

// ── fn: fetch ────────────────────────────────────────────────────────────

type planSpec struct {
	Status             int               `json:"status"`
	StatusText         string            `json:"statusText"`
	Headers            map[string]string `json:"headers"`
	Nobody             bool              `json:"nobody"`
	Throws             *string           `json:"throws"`
	ThrowsSignalReason bool              `json:"throwsSignalReason"`
}

type stepSpec struct {
	Kind    string  `json:"kind"`
	Text    string  `json:"text"`
	MS      float64 `json:"ms"`
	Message string  `json:"message"`
}

type fetchArgs struct {
	Env          map[string]string `json:"env"`
	Plan         planSpec          `json:"plan"`
	Caller       string            `json:"caller"`
	CallerReason *string           `json:"callerReason"`
	Steps        []stepSpec        `json:"steps"`
}

// recordedOp mirrors the generator's transcript entry. `ms` is omitted on
// clears exactly as the TS object literal omits it.
type recordedOp struct {
	Op string `json:"op"`
	ID int    `json:"id"`
	MS string `json:"ms,omitempty"`
}

type fetchOut struct {
	Armed           []string     `json:"armed"`
	Ops             []recordedOp `json:"ops"`
	BodyPresent     bool         `json:"bodyPresent"`
	Text            string       `json:"text"`
	Error           *string      `json:"error"`
	IsLikelyTimeout bool         `json:"isLikelyTimeout"`
	Limiter         []string     `json:"limiter"`
}

// ── virtual clock ────────────────────────────────────────────────────────

type opRecorder struct {
	mu      sync.Mutex
	ops     []recordedOp
	seq     int
	timers  []*fakeTimer
	version int
}

type fakeTimer struct {
	rec     *opRecorder
	ordinal int
	ms      float64
	fn      func()
}

// Stop records unconditionally, like the generator's clearTimeout: TS issues
// clearTimeout on handles that already fired, and those calls are part of the
// transcript.
func (t *fakeTimer) Stop() {
	t.rec.mu.Lock()
	t.rec.ops = append(t.rec.ops, recordedOp{Op: "clear", ID: t.ordinal})
	t.rec.version++
	t.rec.mu.Unlock()
}

func (r *opRecorder) factory(ms float64, fn func()) Timer {
	r.mu.Lock()
	r.seq++
	timer := &fakeTimer{rec: r, ordinal: r.seq, ms: ms, fn: fn}
	r.timers = append(r.timers, timer)
	r.ops = append(r.ops, recordedOp{Op: "set", ID: timer.ordinal, MS: jscompat.FormatNumber(ms)})
	r.version++
	r.mu.Unlock()
	return timer
}

func (r *opRecorder) snapshot() []recordedOp {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]recordedOp, len(r.ops))
	copy(out, r.ops)
	return out
}

func (r *opRecorder) timersSnapshot() []*fakeTimer {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]*fakeTimer, len(r.timers))
	copy(out, r.timers)
	return out
}

func (r *opRecorder) armedSets() []string {
	armed := []string{}
	for _, op := range r.snapshot() {
		if op.Op == "set" {
			armed = append(armed, op.MS)
		}
	}
	return armed
}

func (r *opRecorder) bumpVersion() {
	r.mu.Lock()
	r.version++
	r.mu.Unlock()
}

func (r *opRecorder) currentVersion() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.version
}

// fireByMs runs the most recently armed timer with this delay — the same rule
// the generator's fireByMs uses. The callback is invoked outside the lock
// because it reaches back into the watchdogs.
func (r *opRecorder) fireByMs(t *testing.T, ms float64) {
	t.Helper()
	r.mu.Lock()
	var target *fakeTimer
	for i := len(r.timers) - 1; i >= 0; i-- {
		if r.timers[i].ms == ms {
			target = r.timers[i]
			break
		}
	}
	r.mu.Unlock()
	if target == nil {
		t.Fatalf("no armed timer with delay %v", ms)
	}
	target.fn()
}

// ── recording limiter ────────────────────────────────────────────────────

type recordingLimiter struct {
	mu    sync.Mutex
	calls []string
}

func (l *recordingLimiter) record(name string) {
	l.mu.Lock()
	l.calls = append(l.calls, name)
	l.mu.Unlock()
}

func (l *recordingLimiter) Acquire() <-chan struct{} {
	l.record("acquire")
	settled := make(chan struct{})
	close(settled)
	return settled
}
func (l *recordingLimiter) Release()        { l.record("release") }
func (l *recordingLimiter) OnSuccess()      { l.record("onSuccess") }
func (l *recordingLimiter) OnThrottle()     { l.record("onThrottle") }
func (l *recordingLimiter) OnLowRemaining() { l.record("onLowRemaining") }

func (l *recordingLimiter) snapshot() []string {
	l.mu.Lock()
	defer l.mu.Unlock()
	out := make([]string, len(l.calls))
	copy(out, l.calls)
	return out
}

// ── scripted upstream ────────────────────────────────────────────────────

// fakeBody is the Go twin of the generator's mock ReadableStream: chunks are
// queued (never blocking the pusher, like ReadableStream's internal queue),
// one chunk is handed over per Read, and an abort surfaces the cancel cause
// the way the mock errors the stream with signal.reason.
type fakeBody struct {
	feed chan []byte
	eof  chan struct{}
	ctx  context.Context

	eofOnce   sync.Once
	closeOnce sync.Once
	closed    chan struct{}
}

func newFakeBody(ctx context.Context) *fakeBody {
	return &fakeBody{feed: make(chan []byte, 64), eof: make(chan struct{}), ctx: ctx, closed: make(chan struct{})}
}

func (b *fakeBody) push(text string) { b.feed <- []byte(text) }

func (b *fakeBody) closeUpstream() { b.eofOnce.Do(func() { close(b.eof) }) }

func (b *fakeBody) Read(p []byte) (int, error) {
	select {
	case chunk := <-b.feed:
		return copy(p, chunk), nil
	case <-b.eof:
		return 0, io.EOF
	case <-b.ctx.Done():
		return 0, context.Cause(b.ctx)
	}
}

func (b *fakeBody) Close() error {
	b.closeOnce.Do(func() { close(b.closed) })
	return nil
}

type fakeDoer struct {
	plan planSpec
	body *fakeBody
}

func (d *fakeDoer) Do(req *http.Request) (*http.Response, error) {
	ctx := req.Context()
	if d.plan.Throws != nil {
		return nil, errors.New(*d.plan.Throws)
	}
	if d.plan.ThrowsSignalReason {
		return nil, context.Cause(ctx)
	}
	header := http.Header{}
	for name, value := range d.plan.Headers {
		header.Set(name, value)
	}
	response := &http.Response{
		Status:        strings.TrimSpace(jscompat.FormatNumber(float64(d.plan.Status)) + " " + d.plan.StatusText),
		StatusCode:    d.plan.Status,
		Proto:         "HTTP/1.1",
		ProtoMajor:    1,
		ProtoMinor:    1,
		Header:        header,
		ContentLength: -1,
	}
	if d.plan.Nobody {
		response.Body = http.NoBody
		return response, nil
	}
	d.body = newFakeBody(ctx)
	response.Body = d.body
	return response, nil
}

// ── env handling ─────────────────────────────────────────────────────────

var envKeys = []string{
	"CODEAF_OPENROUTER_IDLE_MS",
	"CODEAF_OPENROUTER_FIRST_CONTENT_MS",
	"CODEAF_OPENROUTER_CONTENT_IDLE_MS",
	"CODEAF_OPENROUTER_TOTAL_REQ_MS",
}

// applyEnv mirrors the generator's applyEnv: a key absent from the case is
// UNSET (not set to ""), because firstContentMs distinguishes undefined from
// "" in its guard even though both land on the default.
func applyEnv(t *testing.T, env map[string]string) {
	t.Helper()
	for _, key := range envKeys {
		saved, had := os.LookupEnv(key)
		key := key
		t.Cleanup(func() {
			if had {
				_ = os.Setenv(key, saved)
			} else {
				_ = os.Unsetenv(key)
			}
		})
		if value, ok := env[key]; ok {
			if err := os.Setenv(key, value); err != nil {
				t.Fatalf("setenv %s: %v", key, err)
			}
		} else if err := os.Unsetenv(key); err != nil {
			t.Fatalf("unsetenv %s: %v", key, err)
		}
	}
}

func awaitSignal(t *testing.T, ch <-chan struct{}, what string) {
	t.Helper()
	select {
	case <-ch:
	case <-time.After(2 * time.Second):
		t.Fatalf("timed out waiting for %s", what)
	}
}

func awaitDrainLength(t *testing.T, progress <-chan int, want int) {
	t.Helper()
	for {
		select {
		case got := <-progress:
			if got >= want {
				return
			}
		case <-time.After(2 * time.Second):
			t.Fatalf("timed out waiting for drained body length %d", want)
		}
	}
}

// ── the fetch case driver ────────────────────────────────────────────────

func runFetchCase(t *testing.T, args fetchArgs) fetchOut {
	t.Helper()
	applyEnv(t, args.Env)

	recorder := &opRecorder{}
	t.Cleanup(SetTimerFactoryForTesting(recorder.factory))

	limiter := &recordingLimiter{}
	t.Cleanup(SetLimiterProvider(func() Limiter { return limiter }))

	doer := &fakeDoer{plan: args.Plan}
	t.Cleanup(SetDoerForTesting(doer))

	callerCtx := context.Background()
	var callerCancel context.CancelCauseFunc
	if args.Caller != "none" {
		callerCtx, callerCancel = context.WithCancelCause(context.Background())
		defer callerCancel(errors.New("test teardown"))
	}
	callerReason := "caller cancelled"
	if args.CallerReason != nil {
		callerReason = *args.CallerReason
	}
	if args.Caller == "pre-aborted" {
		callerCancel(errors.New(callerReason))
	}

	req, err := http.NewRequestWithContext(callerCtx, http.MethodPost,
		"https://openrouter.ai/api/v1/chat/completions", nil)
	if err != nil {
		t.Fatalf("build request: %v", err)
	}

	out := fetchOut{Armed: []string{}, Ops: []recordedOp{}, Limiter: []string{}}

	response, fetchErr := OpenRouterAimdFetch(req)
	// Everything is armed synchronously before the request is dispatched, so
	// the set-ops present now ARE the resolved env-knob table — on the throw
	// path too.
	out.Armed = recorder.armedSets()

	var (
		drainText     bytes.Buffer
		drainErr      error
		drainDone     = make(chan struct{})
		drainProgress = make(chan int, 64)
	)

	if fetchErr != nil {
		message := fetchErr.Error()
		out.Error = &message
		close(drainDone)
	} else {
		out.BodyPresent = response.Body != nil && response.Body != http.NoBody
		if !out.BodyPresent {
			close(drainDone)
		} else {
			go func() {
				defer close(drainDone)
				buf := make([]byte, 64*1024)
				for {
					n, err := response.Body.Read(buf)
					if n > 0 {
						drainText.Write(buf[:n])
						recorder.bumpVersion()
						drainProgress <- drainText.Len()
					}
					if err != nil {
						if err != io.EOF {
							drainErr = err
						}
						recorder.bumpVersion()
						return
					}
				}
			}()

			wantTextLen := 0
			bodyCancelled := false
			for _, step := range args.Steps {
				switch step.Kind {
				case "chunk":
					doer.body.push(step.Text)
					if bodyCancelled {
						awaitSignal(t, doer.body.closed, "post-cancel pump shutdown")
					} else {
						wantTextLen += len(step.Text)
						awaitDrainLength(t, drainProgress, wantTextLen)
					}
				case "close":
					doer.body.closeUpstream()
					awaitSignal(t, drainDone, "upstream EOF")
				case "fire":
					recorder.fireByMs(t, step.MS)
					awaitSignal(t, drainDone, "timer abort")
				case "callerAbort":
					callerCancel(errors.New(step.Message))
					awaitSignal(t, drainDone, "caller abort")
				case "cancelBody":
					_ = response.Body.Close()
					bodyCancelled = true
				default:
					t.Fatalf("unknown step kind %q", step.Kind)
				}
			}
		}
	}

	select {
	case <-drainDone:
	case <-time.After(2 * time.Second):
		t.Fatalf("drain did not finish")
	}

	out.Ops = recorder.snapshot()
	out.Text = drainText.String()
	if drainErr != nil {
		message := drainErr.Error()
		out.Error = &message
	}
	out.Limiter = limiter.snapshot()
	if out.Error != nil {
		out.IsLikelyTimeout = isLikelyTimeoutMirror(*out.Error)
	}
	return out
}

// ── replay ───────────────────────────────────────────────────────────────

func TestFixtureParity(t *testing.T) {
	file, err := os.Open("testdata/fixtures.json")
	if err != nil {
		t.Fatalf("open fixtures: %v", err)
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 0, 1<<20), 1<<24)
	cases := 0
	for scanner.Scan() {
		raw := bytes.TrimSpace(scanner.Bytes())
		if len(raw) == 0 {
			continue
		}
		var fixture fixtureLine
		if err := json.Unmarshal(raw, &fixture); err != nil {
			t.Fatalf("decode fixture line: %v", err)
		}
		cases++
		t.Run(fixture.Name, func(t *testing.T) {
			var result any
			switch fixture.Fn {
			case "constants":
				result = constantsOut{
					FirstContentDefaultMs: jscompat.JSNumber(FIRST_CONTENT_DEFAULT_MS),
					ContentIdleDefaultMs:  jscompat.JSNumber(CONTENT_IDLE_DEFAULT_MS),
				}
			case "classify":
				var args classifyArgs
				decodeArgs(t, fixture.ArgsJSON, &args)
				result = classifyOut{Text: args.Text, IsLikelyTimeout: isLikelyTimeoutMirror(args.Text)}
			case "rateHeaders":
				var args rateArgs
				decodeArgs(t, fixture.ArgsJSON, &args)
				header := http.Header{}
				for name, value := range args.Headers {
					header.Set(name, value)
				}
				info := ratehead.ParseRateLimitHeaders(header)
				result = rateOut{Info: info, Low: ratehead.IsLowRemaining(info)}
			case "fetch":
				var args fetchArgs
				decodeArgs(t, fixture.ArgsJSON, &args)
				result = runFetchCase(t, args)
			default:
				t.Fatalf("unknown fn %q", fixture.Fn)
			}

			encoded, err := jscompat.Stringify(result)
			if err != nil {
				t.Fatalf("stringify go result: %v", err)
			}
			if string(encoded) != fixture.OutJSON {
				t.Errorf("byte parity failure\n  fn:   %s\n  args: %s\n  want: %s\n  got:  %s",
					fixture.Fn, fixture.ArgsJSON, fixture.OutJSON, encoded)
			}
		})
	}
	if err := scanner.Err(); err != nil {
		t.Fatalf("scan fixtures: %v", err)
	}
	if cases < 30 {
		t.Fatalf("expected at least 30 fixture cases, got %d", cases)
	}
	t.Logf("replayed %d fixture cases", cases)
}

func decodeArgs(t *testing.T, raw string, into any) {
	t.Helper()
	if err := json.Unmarshal([]byte(raw), into); err != nil {
		t.Fatalf("decode args_json: %v", err)
	}
}
