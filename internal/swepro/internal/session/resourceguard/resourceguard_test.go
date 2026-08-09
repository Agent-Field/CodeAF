package resourceguard_test

import (
	"errors"
	"math"
	"os"
	"path/filepath"
	"testing"

	"github.com/Agent-Field/swe-pro-go/internal/session/resourceguard"
)

func fixedFloor(raw string) resourceguard.EnvLookup {
	return func(string) (string, bool) { return raw, true }
}

func failingStatFS(string) (resourceguard.StatFSResult, error) {
	return resourceguard.StatFSResult{}, errors.New("statfs failed")
}

func TestCheckDiskEnvelopeUsesStatFS(t *testing.T) {
	dfCalled := false
	got := resourceguard.CheckDiskEnvelope("/workspace", resourceguard.CheckOptions{
		LookupEnv: fixedFloor("5"),
		StatFS: func(path string) (resourceguard.StatFSResult, error) {
			if path != "/workspace" {
				t.Fatalf("statfs path = %q", path)
			}
			return resourceguard.StatFSResult{Bavail: 2 * 1024 * 1024, Bsize: 4096}, nil
		},
		DF: func(string) (string, error) {
			dfCalled = true
			return "", nil
		},
	})
	if dfCalled {
		t.Fatal("df fallback called after successful statfs")
	}
	if float64(got.FreeBytes) != 8*1024*1024*1024 || float64(got.FreeGB) != 8 || !got.OK {
		t.Fatalf("unexpected envelope: %+v", got)
	}
}

func TestCheckDiskEnvelopeEqualityIsBelowFloor(t *testing.T) {
	got := resourceguard.CheckDiskEnvelope("/workspace", resourceguard.CheckOptions{
		LookupEnv: fixedFloor("5"),
		StatFS: func(string) (resourceguard.StatFSResult, error) {
			return resourceguard.StatFSResult{Bavail: 5, Bsize: 1024 * 1024 * 1024}, nil
		},
	})
	if got.OK {
		t.Fatalf("exactly-at-floor envelope is OK: %+v", got)
	}
}

func TestCheckDiskEnvelopeFallsBackToLastDFLine(t *testing.T) {
	got := resourceguard.CheckDiskEnvelope("odd path", resourceguard.CheckOptions{
		LookupEnv: fixedFloor("1"),
		StatFS:    failingStatFS,
		DF: func(path string) (string, error) {
			if path != "odd path" {
				t.Fatalf("df path = %q", path)
			}
			return "Filesystem 1024-blocks Used Available Capacity Mounted\n/dev/x 99 1 2097152 1% /mnt\n", nil
		},
	})
	if float64(got.FreeBytes) != 2*1024*1024*1024 || float64(got.FreeGB) != 2 || !got.OK {
		t.Fatalf("unexpected df envelope: %+v", got)
	}
}

func TestCheckDiskEnvelopeDFUsesJSWhitespaceAndNumber(t *testing.T) {
	got := resourceguard.CheckDiskEnvelope("/workspace", resourceguard.CheckOptions{
		LookupEnv: fixedFloor("0"),
		StatFS:    failingStatFS,
		DF: func(string) (string, error) {
			return "fs\u00a010\u20031\u202f0x10\u3000/mnt", nil
		},
	})
	if float64(got.FreeBytes) != 16*1024 || !got.OK {
		t.Fatalf("unexpected JS-coerced df envelope: %+v", got)
	}
}

func TestCheckDiskEnvelopeMeasurementFailureFailsOpen(t *testing.T) {
	got := resourceguard.CheckDiskEnvelope("/missing", resourceguard.CheckOptions{
		LookupEnv: fixedFloor("5"),
		StatFS:    failingStatFS,
		DF: func(string) (string, error) {
			return "", errors.New("df failed")
		},
	})
	if float64(got.FreeBytes) != -1 || float64(got.FreeGB) != -1 || !got.OK {
		t.Fatalf("measurement did not fail open: %+v", got)
	}
}

func TestCheckDiskEnvelopeRecoversPanickingMeasurements(t *testing.T) {
	got := resourceguard.CheckDiskEnvelope("/missing", resourceguard.CheckOptions{
		LookupEnv: fixedFloor("5"),
		StatFS: func(string) (resourceguard.StatFSResult, error) {
			panic("statfs panic")
		},
		DF: func(string) (string, error) {
			panic("df panic")
		},
	})
	if float64(got.FreeBytes) != -1 || !got.OK {
		t.Fatalf("panicking measurement did not fail open: %+v", got)
	}
}

func TestKeptBugDefaultDFAcceptsParseableOutputFromNonzeroExit(t *testing.T) {
	binDir := t.TempDir()
	dfPath := filepath.Join(binDir, "df")
	script := "#!/bin/sh\nprintf 'fs 9 1 2097152 1%% /mnt\\n'\nexit 9\n"
	if err := os.WriteFile(dfPath, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", binDir)
	got := resourceguard.CheckDiskEnvelope("/workspace", resourceguard.CheckOptions{
		LookupEnv: fixedFloor("1"),
		StatFS:    failingStatFS,
	})
	if float64(got.FreeGB) != 2 || !got.OK {
		t.Fatalf("nonzero df stdout was not accepted: %+v", got)
	}
}

func TestCheckDiskEnvelopePreservesNaNStatFSQuirk(t *testing.T) {
	got := resourceguard.CheckDiskEnvelope("/workspace", resourceguard.CheckOptions{
		LookupEnv: fixedFloor("5"),
		StatFS: func(string) (resourceguard.StatFSResult, error) {
			return resourceguard.StatFSResult{Bavail: math.NaN(), Bsize: 4096}, nil
		},
	})
	if !math.IsNaN(float64(got.FreeBytes)) || float64(got.FreeGB) != -1 || got.OK {
		t.Fatalf("NaN statfs semantics changed: %+v", got)
	}
}

func TestRealCheckDiskEnvelopeNeverThrows(t *testing.T) {
	got := resourceguard.CheckDiskEnvelope(t.TempDir())
	if float64(got.FloorGB) < 0 {
		t.Fatalf("invalid floor from real check: %+v", got)
	}
}
