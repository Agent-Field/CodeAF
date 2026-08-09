package isolation_test

import (
	"os"
	"testing"

	"github.com/Agent-Field/swe-pro-go/internal/session/isolation"
)

func TestResolveIsolationBackendFromEnv(t *testing.T) {
	t.Setenv("CODEAF_ISOLATION", " Furrow ")
	got := isolation.ResolveIsolationBackendFromEnv()
	if got.Backend != isolation.BackendFurrow || !got.Recognized || got.Raw != "Furrow" {
		t.Fatalf("unexpected resolution: %+v", got)
	}
}

func TestCowSupportProberIsInjectableAndMemoized(t *testing.T) {
	calls := 0
	prober := isolation.NewCowSupportProber(func(baseDir string) bool {
		calls++
		return baseDir == "first"
	})
	if !prober.Probe("first") {
		t.Fatal("first probe = false, want true")
	}
	if !prober.Probe("second") {
		t.Fatal("memoized probe = false, want cached true")
	}
	if calls != 1 {
		t.Fatalf("probe calls = %d, want 1", calls)
	}
}

func TestCowSupportProberNeverPanics(t *testing.T) {
	prober := isolation.NewCowSupportProber(func(string) bool {
		panic("probe exploded")
	})
	if prober.Probe(t.TempDir()) {
		t.Fatal("panicking probe = true, want false")
	}
}

func TestPackageCowProbeMemoizesFailure(t *testing.T) {
	isolation.ResetCowProbeCacheForTest()
	t.Cleanup(isolation.ResetCowProbeCacheForTest)

	missing := t.TempDir() + "/does-not-exist"
	if isolation.ProbeCowSupport(missing) {
		t.Fatal("probe in nonexistent base = true, want false")
	}
	if isolation.ProbeCowSupport(os.TempDir()) {
		t.Fatal("second probe did not retain cached false")
	}
}

func TestRealCowProbeReturnsBoolean(t *testing.T) {
	isolation.ResetCowProbeCacheForTest()
	t.Cleanup(isolation.ResetCowProbeCacheForTest)
	_ = isolation.ProbeCowSupport()
}
