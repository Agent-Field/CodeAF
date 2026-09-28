//go:build !windows

package modelsdev

import (
	"context"
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (function roundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return function(request)
}

func fixture(t *testing.T) []byte {
	t.Helper()
	raw, err := os.ReadFile("testdata/catalog.json")
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

func response(request *http.Request, status int, body []byte) *http.Response {
	return &http.Response{
		StatusCode: status,
		Status:     http.StatusText(status),
		Body:       io.NopCloser(strings.NewReader(string(body))),
		Request:    request,
		Header:     make(http.Header),
	}
}

func TestCatalogProjectsCostsAndLimits(t *testing.T) {
	client, err := New(Options{
		CatalogPath: "testdata/catalog.json", CacheDir: t.TempDir(), DisableFetch: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	catalog, err := client.Get(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	model, err := catalog.Resolve("openrouter", "fixture/vendor-model")
	if err != nil {
		t.Fatal(err)
	}
	if model.Cost == nil || model.Cost.Input != 1.25 || model.Cost.Output != 4.5 ||
		model.Cost.Cache == nil || model.Cost.Cache.Read != 0.125 || model.Cost.Cache.Write != 1.5 ||
		model.Cost.ExperimentalOver200K == nil || model.Cost.ExperimentalOver200K.Input != 2.5 ||
		model.Limit.Context != 240_000 || model.Limit.Input == nil || *model.Limit.Input != 220_000 ||
		model.Limit.Output != 12_000 {
		t.Fatalf("projected model = %#v", model)
	}
	if !model.Capabilities.Attachment || !model.Capabilities.Reasoning ||
		!model.Capabilities.Temperature || !model.Capabilities.ToolCall ||
		!model.Capabilities.Input["text"] || !model.Capabilities.Input["image"] ||
		model.Capabilities.Input["audio"] || !model.Capabilities.Output["text"] ||
		model.Capabilities.Output["image"] {
		t.Fatalf("projected capabilities = %#v", model.Capabilities)
	}
	withoutCost, err := catalog.Resolve("openrouter", "fixture/no-cost")
	if err != nil {
		t.Fatal(err)
	}
	if withoutCost.Cost == nil || withoutCost.Cost.Input != 0 || withoutCost.Cost.Output != 0 {
		t.Fatalf("missing cost projection = %#v", withoutCost.Cost)
	}
	if withoutCost.Capabilities.Attachment || withoutCost.Capabilities.Reasoning ||
		!withoutCost.Capabilities.Temperature || !withoutCost.Capabilities.ToolCall ||
		withoutCost.Capabilities.Input["text"] || withoutCost.Capabilities.Output["text"] {
		t.Fatalf("missing modalities/default projection = %#v", withoutCost.Capabilities)
	}
	if _, err := catalog.Resolve("openrouter", "fixture/unknown"); err == nil {
		t.Fatal("unknown model unexpectedly resolved")
	}
	// An absent temperature flag must read as support: the request-side gate
	// drops a configured temperature on false, and many models.dev entries
	// omit the flag entirely.
	defaults := projectCapabilities(Model{})
	if defaults.Attachment || defaults.Reasoning || !defaults.Temperature || !defaults.ToolCall ||
		defaults.Input["text"] || defaults.Output["text"] {
		t.Fatalf("absent capability defaults = %#v", defaults)
	}
}

func TestGetFetchesOnceAndReusesFreshDiskCache(t *testing.T) {
	var mu sync.Mutex
	requests := 0
	transport := roundTripFunc(func(request *http.Request) (*http.Response, error) {
		mu.Lock()
		requests++
		mu.Unlock()
		if request.URL.String() != "https://catalog.example/api.json" {
			t.Fatalf("request URL = %s", request.URL)
		}
		if request.Header.Get("User-Agent") != "senior-dev/test-version" {
			t.Fatalf("User-Agent = %q", request.Header.Get("User-Agent"))
		}
		return response(request, http.StatusOK, fixture(t)), nil
	})
	options := Options{
		Source: "https://catalog.example", CacheDir: t.TempDir(),
		HTTPClient: &http.Client{Transport: transport}, Version: "test-version",
		Sleep: func(context.Context, time.Duration) error { return nil },
	}
	first, err := New(options)
	if err != nil {
		t.Fatal(err)
	}
	if first.options.LockTimeout != 5*time.Minute {
		t.Fatalf("default lock timeout = %v, want 5m", first.options.LockTimeout)
	}
	if _, err := first.Get(context.Background()); err != nil {
		t.Fatal(err)
	}
	if _, err := first.Get(context.Background()); err != nil {
		t.Fatal(err)
	}
	second, err := New(options)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := second.Get(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := second.Refresh(context.Background(), false); err != nil {
		t.Fatal(err)
	}
	mu.Lock()
	defer mu.Unlock()
	if requests != 1 {
		t.Fatalf("catalog requests = %d, want 1", requests)
	}
	if filepath.Base(first.CachePath()) != "models-871608c17971dbac7e10503763c16cb91f1a52f7.json" {
		t.Fatalf("custom-source cache path = %s", first.CachePath())
	}
}

func TestColdUnreachableCatalogReturnsErrorAndDisabledFetchReturnsEmpty(t *testing.T) {
	requests := 0
	client, err := New(Options{
		Source: "https://unreachable.example", CacheDir: t.TempDir(),
		HTTPClient: &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
			requests++
			return nil, errors.New("offline")
		})},
		Sleep: func(context.Context, time.Duration) error { return nil },
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := client.Get(context.Background()); err == nil || !strings.Contains(err.Error(), "offline") {
		t.Fatalf("cold unreachable error = %v", err)
	}
	if requests != 3 {
		t.Fatalf("transient fetch attempts = %d, want 3", requests)
	}

	disabled, err := New(Options{
		CacheDir: t.TempDir(), DisableFetch: true,
		HTTPClient: &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
			t.Fatal("disabled catalog performed network I/O")
			return nil, nil
		})},
	})
	if err != nil {
		t.Fatal(err)
	}
	catalog, err := disabled.Get(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(catalog) != 0 {
		t.Fatalf("disabled catalog = %#v", catalog)
	}
	if _, err := catalog.Resolve("openrouter", "fixture/vendor-model"); err == nil {
		t.Fatal("model resolved from disabled empty catalog")
	}
}

func TestStaleDiskCatalogWinsOverUnreachableNetwork(t *testing.T) {
	cacheDir := t.TempDir()
	cachePath := filepath.Join(cacheDir, "models.json")
	if err := os.WriteFile(cachePath, fixture(t), 0o644); err != nil {
		t.Fatal(err)
	}
	stale := time.Now().Add(-24 * time.Hour)
	if err := os.Chtimes(cachePath, stale, stale); err != nil {
		t.Fatal(err)
	}
	client, err := New(Options{
		CacheDir: cacheDir,
		HTTPClient: &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
			t.Fatal("initial Get fetched instead of using the stale disk catalog")
			return nil, errors.New("offline")
		})},
	})
	if err != nil {
		t.Fatal(err)
	}
	catalog, err := client.Get(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := catalog.Resolve("openrouter", "fixture/vendor-model"); err != nil {
		t.Fatal(err)
	}
}

func TestCatalogLockHonorsCancellationAndTimeout(t *testing.T) {
	cacheDir := t.TempDir()
	client, err := New(Options{
		CacheDir: cacheDir, LockTimeout: 30 * time.Millisecond, LockPoll: time.Millisecond,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(client.CachePath()), 0o755); err != nil {
		t.Fatal(err)
	}
	lock, err := os.OpenFile(client.CachePath()+".lock", os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	defer lock.Close()
	if err := syscall.Flock(int(lock.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		t.Fatal(err)
	}
	defer syscall.Flock(int(lock.Fd()), syscall.LOCK_UN) //nolint:errcheck

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := client.withFileLock(ctx, func() error { return nil }); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled lock error = %v", err)
	}
	started := time.Now()
	err = client.withFileLock(context.Background(), func() error { return nil })
	if err == nil || !strings.Contains(err.Error(), "Timed out waiting for lock: models-dev:") {
		t.Fatalf("timeout error = %v", err)
	}
	if elapsed := time.Since(started); elapsed > time.Second {
		t.Fatalf("short test timeout took %v", elapsed)
	}
}

func TestRefreshLockTimeoutKeepsMemoizedCatalog(t *testing.T) {
	client, err := New(Options{
		CatalogPath: "testdata/catalog.json", CacheDir: t.TempDir(),
		LockTimeout: 20 * time.Millisecond, LockPoll: time.Millisecond,
		HTTPClient: &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
			t.Fatal("timed-out refresh reached the network")
			return nil, nil
		})},
	})
	if err != nil {
		t.Fatal(err)
	}
	before, err := client.Get(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	lock, err := os.OpenFile(client.CachePath()+".lock", os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	defer lock.Close()
	if err := syscall.Flock(int(lock.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		t.Fatal(err)
	}
	defer syscall.Flock(int(lock.Fd()), syscall.LOCK_UN) //nolint:errcheck
	if err := client.Refresh(context.Background(), true); err == nil ||
		!strings.Contains(err.Error(), "Timed out waiting for lock") {
		t.Fatalf("refresh timeout error = %v", err)
	}
	after, err := client.Get(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(after) != len(before) {
		t.Fatalf("catalog size after timeout = %d, want %d", len(after), len(before))
	}
	if _, err := after.Resolve("openrouter", "fixture/vendor-model"); err != nil {
		t.Fatalf("memoized catalog lost after refresh timeout: %v", err)
	}
}
