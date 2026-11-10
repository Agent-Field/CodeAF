package main

import (
	"context"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/config"
	"github.com/Agent-Field/codeaf/internal/pool/judge"
	"github.com/Agent-Field/codeaf/internal/pool/outbox"
)

// A transport may return a response whose body does not observe cancellation.
// Releasing it after close exercises the bounded join without timing a sleep.
type poolHeldBody struct {
	io.Reader
	started chan struct{}
	release chan struct{}
	once    sync.Once
}

func (body *poolHeldBody) Read(p []byte) (int, error) {
	body.once.Do(func() { close(body.started) })
	<-body.release
	return body.Reader.Read(p)
}

func (*poolHeldBody) Close() error { return nil }

func poolNetworkFixture(t *testing.T) {
	t.Helper()
	t.Setenv("CODEAF_MODEL_POOL", "read")
	t.Setenv("CODEAF_MODEL_POOL_URL", filepath.Join(t.TempDir(), "missing-index"))
	t.Setenv("CODEAF_MODEL_POOL_MIRROR_URL", "")
	t.Setenv("CODEAF_TELEMETRY", "on")
	t.Setenv("DO_NOT_TRACK", "")
	t.Setenv("CODEAF_TELEMETRY_ENDPOINT", "https://pool.invalid/usage")
	t.Setenv("CODEAF_MODEL_POOL_SUBMIT_URL", "https://pool.invalid/submit")
}

// Include modification times so rewriting the same bytes after close also fails.
type poolFileState struct {
	Data string
	Time time.Time
}

func poolFiles(t *testing.T, root string) map[string]poolFileState {
	t.Helper()
	files := map[string]poolFileState{}
	err := filepath.WalkDir(root, func(path string, entry os.DirEntry, err error) error {
		if os.IsNotExist(err) {
			return nil
		}
		if err != nil || entry.IsDir() {
			return err
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		files[path] = poolFileState{Data: string(data), Time: info.ModTime()}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return files
}

func TestPoolCloseStopsRequestsAndKeepsUnsentRows(t *testing.T) {
	t.Run("push", testPoolPushCloseStopsRequestsAndKeepsUnsentRows)
	t.Run("scoring", testPoolCloseStopsSweepAndLandingRequests)
}

func testPoolPushCloseStopsRequestsAndKeepsUnsentRows(t *testing.T) {
	for _, status := range []int{http.StatusBadRequest, http.StatusOK} {
		t.Run(http.StatusText(status), func(t *testing.T) {
			poolNetworkFixture(t)
			proc := v3TestProcess(t)
			t.Setenv("CODEAF_MODEL_POOL", "on")
			pool := config.ProfilePath(proc.ProfileDir, "pool")
			box, err := outbox.Open(outboxPath(pool))
			if err != nil {
				t.Fatal(err)
			}
			for _, row := range []string{`{"sample":1}`, `{"sample":2}`} {
				if err := box.Append([]byte(row)); err != nil {
					t.Fatal(err)
				}
			}
			box.Close()
			body := &poolHeldBody{Reader: strings.NewReader(`{"error":"line 1: refused"}`), started: make(chan struct{}), release: make(chan struct{})}
			var release sync.Once
			free := func() { release.Do(func() { close(body.release) }) }
			var calls atomic.Int32
			var firstBatch, nextBatch string
			previous := http.DefaultTransport
			http.DefaultTransport = moneyTransport(func(request *http.Request) (*http.Response, error) {
				batch, err := io.ReadAll(request.Body)
				if err != nil {
					return nil, err
				}
				if calls.Add(1) == 1 {
					firstBatch = string(batch)
					return &http.Response{StatusCode: status, Header: make(http.Header), Body: body}, nil
				}
				nextBatch = string(batch)
				return &http.Response{StatusCode: http.StatusOK, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(""))}, nil
			})
			finished := make(chan struct{})
			t.Cleanup(func() { free(); <-finished; http.DefaultTransport = previous })
			held := poolErrandsStart(proc.ProfileDir)
			cfg := config.ModelPoolAt(proc.ProfileDir)
			held.goCtx("pool/push", func(ctx context.Context) {
				defer close(finished)
				poolPush(ctx, proc.ProfileDir, cfg, poolPushBudget)
			})
			select {
			case <-body.started:
			case <-time.After(5 * time.Second):
				t.Fatal("push never reached the relay response")
			}
			proc.closeAll()
			beforeCalls, beforeFiles := calls.Load(), poolFiles(t, pool)
			free()
			<-finished
			if got := calls.Load() - beforeCalls; got != 0 {
				t.Errorf("push started %d requests after close returned", got)
			}
			if !reflect.DeepEqual(beforeFiles, poolFiles(t, pool)) {
				t.Error("push wrote pool files after close returned")
			}
			box, err = outbox.Open(outboxPath(pool))
			if err != nil {
				t.Fatal(err)
			}
			if got := len(box.Pending()); got != 2 {
				t.Errorf("cancelled push kept %d rows, want both unsent rows", got)
			}
			if len(box.Dropped()) != 0 {
				t.Error("cancelled push dropped an unsent row")
			}
			box.Close()
			poolPush(context.Background(), proc.ProfileDir, cfg, poolPushBudget)
			if nextBatch != firstBatch {
				t.Error("next push did not retry the complete unsent batch")
			}
			box, err = outbox.Open(outboxPath(pool))
			if err != nil {
				t.Fatal(err)
			}
			defer box.Close()
			if len(box.Pending()) != 0 {
				t.Error("successful next push left rows pending")
			}
		})
	}
}

func TestPoolSendTransportRejectsCancelledRequest(t *testing.T) {
	poolNetworkFixture(t)
	proc := v3TestProcess(t)
	t.Setenv("CODEAF_MODEL_POOL", "on")
	held := poolErrandsStart(proc.ProfileDir)
	proc.closeAll()
	previous := http.DefaultTransport
	defer func() { http.DefaultTransport = previous }()
	calls := 0
	http.DefaultTransport = moneyTransport(func(*http.Request) (*http.Response, error) {
		calls++
		return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(""))}, nil
	})
	request, err := http.NewRequestWithContext(held.ctx, http.MethodPost, "https://pool.invalid/submit", nil)
	if err != nil {
		t.Fatal(err)
	}
	transport := poolSendTransport{profileDir: proc.ProfileDir, cfg: config.ModelPoolAt(proc.ProfileDir)}
	response, err := transport.RoundTrip(request)
	if response != nil {
		response.Body.Close()
	}
	if calls != 0 || err == nil {
		t.Fatalf("cancelled push reached transport %d times; error=%v", calls, err)
	}
}

func testPoolCloseStopsSweepAndLandingRequests(t *testing.T) {
	for _, sweep := range []bool{true, false} {
		name := "landing"
		if sweep {
			name = "sweep"
		}
		t.Run(name, func(t *testing.T) {
			poolNetworkFixture(t)
			proc := v3TestProcess(t)
			pool := config.ProfilePath(proc.ProfileDir, "pool")
			if sweep {
				if err := writePendingLanding(proc.ProfileDir, "do", poolTestLanding()); err != nil {
					t.Fatal(err)
				}
			}
			body := &poolHeldBody{Reader: strings.NewReader(`{"score":88,"reason":"completed"}`), started: make(chan struct{}), release: make(chan struct{})}
			var release sync.Once
			free := func() { release.Do(func() { close(body.release) }) }
			var calls atomic.Int32
			client := &http.Client{Transport: moneyTransport(func(*http.Request) (*http.Response, error) {
				calls.Add(1)
				return &http.Response{StatusCode: http.StatusOK, Body: body}, nil
			})}
			ask := func(string) judge.Ask {
				return func(ctx context.Context, _, _ string) (string, error) {
					request, err := http.NewRequestWithContext(ctx, http.MethodPost, "https://pool.invalid/judge", nil)
					if err != nil {
						return "", err
					}
					response, err := client.Do(request)
					if err != nil {
						return "", err
					}
					defer response.Body.Close()
					data, err := io.ReadAll(response.Body)
					return string(data), err
				}
			}
			finished := make(chan struct{})
			t.Cleanup(func() { free(); <-finished })
			held := poolErrandsStart(proc.ProfileDir)
			work := func(ctx context.Context) {
				if sweep {
					poolJudgeSweepContext(ctx, proc.Settings, proc.ProfileDir, "", poolTestCatalog, ask, time.Now)
				} else {
					poolJudgeHookContext(ctx, proc.Settings, proc.ProfileDir, poolTestCatalog, ask, time.Now, "task")(poolTestLanding())
				}
			}
			held.goCtx("pool/"+name, func(ctx context.Context) { defer close(finished); work(ctx) })
			select {
			case <-body.started:
			case <-time.After(5 * time.Second):
				t.Fatal("scoring never reached the provider response")
			}
			proc.closeAll()
			beforeCalls, beforeFiles := calls.Load(), poolFiles(t, pool)
			free()
			<-finished
			// A late landing or queued sweep must also be refused by its owner.
			held.goCtx("pool/late", work)
			held.wg.Wait()
			if got := calls.Load() - beforeCalls; got != 0 {
				t.Errorf("scoring started %d requests after close returned", got)
			}
			if !reflect.DeepEqual(beforeFiles, poolFiles(t, pool)) {
				t.Error("scoring wrote pool files after close returned")
			}
		})
	}
}
