package codeaf

import (
	"bytes"
	"context"
	"errors"
	"math"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Agent-Field/aforge-v2/internal/swepro/internal/session/resourceguard"
)

func TestParsePortfolioSurface(t *testing.T) {
	defaults, err := parseArgs([]string{"portfolio", "ship", "it"})
	if err != nil {
		t.Fatal(err)
	}
	if defaults.Command != "portfolio" || defaults.Message != "ship it" ||
		!defaults.Hard || defaults.Attempts != 3 || !defaults.Reconcile ||
		defaults.Blast || defaults.EntryAgent != "" || defaults.Format != "json" {
		t.Fatalf("portfolio defaults = %#v", defaults)
	}

	got, err := parseArgs([]string{
		"portfolio", "do", "--dir", "/repo", "the", "work",
		"--high", "provider/high", "--low=provider/low", "--variant", "medium",
		"--format", "default", "--entryAgent", "coder", "--prReady",
		"--no-hard", "--blast", "--attempts", "9.8", "--no-reconcile",
	})
	if err != nil {
		t.Fatal(err)
	}
	if got.Command != "portfolio" || got.Message != "do the work" || got.Directory != "/repo" ||
		got.High != "provider/high" || got.Low != "provider/low" || got.Variant != "medium" ||
		got.Format != "default" || got.EntryAgent != "coder" || !got.PRReady || got.Hard ||
		!got.Blast || got.Attempts != 9.8 || got.Reconcile {
		t.Fatalf("parsed portfolio args = %#v", got)
	}

	nanArgs, err := parseArgs([]string{"portfolio", "--attempts", "not-a-number", "work"})
	if err != nil || !math.IsNaN(nanArgs.Attempts) || nanArgs.Message != "work" {
		t.Fatalf("NaN attempts = %#v, %v", nanArgs, err)
	}
}

func TestParsePortfolioRejectsInvalidUsageWithTSText(t *testing.T) {
	tests := []struct {
		argv []string
		want string
	}{
		{[]string{"portfolio", "--bogus", "work"}, "Unknown argument: bogus"},
		{[]string{"portfolio", "--format", "yaml", "work"}, "Invalid values:\n  Argument: format, Given: \"yaml\", Choices: \"default\", \"json\""},
		{[]string{"portfolio", "--attempts"}, "Not enough arguments following: attempts"},
		{[]string{"portfolio", "--frontier", "provider/frontier", "work"}, "Unknown argument: frontier"},
	}
	for _, test := range tests {
		_, err := parseArgs(test.argv)
		if err == nil || err.Error() != test.want {
			t.Errorf("parseArgs(%q) error = %v, want %q", test.argv, err, test.want)
		}
	}
}

func TestPortfolioGoldenFromHandDerivedArtifacts(t *testing.T) {
	workspace := t.TempDir()
	fixtureRoot := filepath.Join("testdata", "portfolio", "artifacts")
	for _, tag := range []string{"a", "b"} {
		destination := filepath.Join(workspace+".portfolio", tag)
		copyPortfolioFixture(t, filepath.Join(fixtureRoot, tag), destination)
	}

	args, err := parseArgs([]string{
		"portfolio", "--dir", workspace, "--attempts", "2", "implement behavior",
	})
	if err != nil {
		t.Fatal(err)
	}
	clock := []float64{0, 1500, 1500, 4000}
	requests := []portfolioChildRequest{}
	deps := portfolioDeps{
		NowMillis: func() float64 {
			value := clock[0]
			clock = clock[1:]
			return value
		},
		IsGitRepo: func(context.Context, string) bool { return true },
		CheckDisk: func(string) resourceguard.DiskEnvelope {
			return resourceguard.DiskEnvelope{FreeBytes: 20 << 30, FreeGB: 20, FloorGB: 5, OK: true}
		},
		Clone: func(_ context.Context, _, _ string) cloneResult {
			return cloneResult{OK: true, CoW: true}
		},
		ResetClean: func(context.Context, string) {},
		SpawnChild: func(_ context.Context, request portfolioChildRequest) (int, error) {
			requests = append(requests, request)
			return 0, nil
		},
	}
	var stdout, stderr bytes.Buffer
	if err := runPortfolio(context.Background(), args, &stdout, &stderr, deps); err != nil {
		t.Fatal(err)
	}

	normalize := func(value string) string {
		return strings.ReplaceAll(value, workspace, "<workspace>")
	}
	wantStdout := readPortfolioGolden(t, filepath.Join("testdata", "portfolio", "stdout.golden"))
	wantStderr := readPortfolioGolden(t, filepath.Join("testdata", "portfolio", "stderr.golden"))
	if got := normalize(stdout.String()); got != wantStdout {
		t.Fatalf("stdout mismatch\n got: %q\nwant: %q", got, wantStdout)
	}
	if got := normalize(stderr.String()); got != wantStderr {
		t.Fatalf("stderr mismatch\n got: %q\nwant: %q", got, wantStderr)
	}
	if len(requests) != 2 || containsArg(requests[0].Args, "--hard") ||
		!containsArg(requests[1].Args, "--hard") {
		t.Fatalf("child launch modes = %#v", requests)
	}
}

func TestPortfolioMissingMessageAndMissingVerdict(t *testing.T) {
	t.Run("message", func(t *testing.T) {
		var stdout, stderr bytes.Buffer
		err := runCLI(context.Background(), []string{"portfolio"}, nil, &stdout, &stderr)
		var exit *cliExitError
		if !errors.As(err, &exit) || exit.code != 1 || stdout.Len() != 0 ||
			stderr.String() != "[codeaf] missing message — pass a prompt as positional arg\n" {
			t.Fatalf("missing message = err:%v stdout:%q stderr:%q", err, stdout.String(), stderr.String())
		}
	})

	t.Run("empty portfolio from NaN attempts", func(t *testing.T) {
		args, err := parseArgs([]string{"portfolio", "--attempts", "NaN", "work"})
		if err != nil {
			t.Fatal(err)
		}
		deps := portfolioDeps{
			IsGitRepo: func(context.Context, string) bool { return true },
		}
		var stdout, stderr bytes.Buffer
		err = runPortfolio(context.Background(), args, &stdout, &stderr, deps)
		var exit *cliExitError
		if !errors.As(err, &exit) || exit.code != 1 ||
			!strings.Contains(stdout.String(), "attempt  gate  coverage  cost  wall  suspect\n") ||
			!strings.Contains(stderr.String(), "up to NaN attempts") ||
			!strings.Contains(stderr.String(), "no attempts produced a result") {
			t.Fatalf("empty portfolio = err:%v stdout:%q stderr:%q", err, stdout.String(), stderr.String())
		}
	})

	t.Run("verdict", func(t *testing.T) {
		workspace := t.TempDir()
		args, err := parseArgs([]string{
			"portfolio", "--dir", workspace, "--attempts", "1", "work",
		})
		if err != nil {
			t.Fatal(err)
		}
		clock := []float64{0, 1000}
		deps := portfolioDeps{
			NowMillis: func() float64 { value := clock[0]; clock = clock[1:]; return value },
			IsGitRepo: func(context.Context, string) bool { return true },
			CheckDisk: func(string) resourceguard.DiskEnvelope {
				return resourceguard.DiskEnvelope{OK: true}
			},
			Clone: func(_ context.Context, _, destination string) cloneResult {
				if err := os.MkdirAll(destination, 0o755); err != nil {
					t.Fatal(err)
				}
				return cloneResult{OK: true, CoW: true}
			},
			ResetClean: func(context.Context, string) {},
			SpawnChild: func(context.Context, portfolioChildRequest) (int, error) { return 0, nil },
		}
		var stdout, stderr bytes.Buffer
		err = runPortfolio(context.Background(), args, &stdout, &stderr, deps)
		var exit *cliExitError
		if !errors.As(err, &exit) || exit.code != 1 {
			t.Fatalf("missing verdict exit = %v", err)
		}
		for _, fragment := range []string{
			"a        unknown  0         n/a   1.0s  SUSPECT",
			"Winning attempt: a (gate: unknown)",
			"[codeaf] portfolio: 1 attempt(s) launched; no attempt passed",
		} {
			if !strings.Contains(stdout.String()+stderr.String(), fragment) {
				t.Errorf("missing %q\nstdout:\n%s\nstderr:\n%s", fragment, stdout.String(), stderr.String())
			}
		}
	})
}

func readPortfolioGolden(t *testing.T, path string) string {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return strings.ReplaceAll(string(raw), "·", " ")
}

func copyPortfolioFixture(t *testing.T, source, destination string) {
	t.Helper()
	if err := filepath.WalkDir(source, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		relative, err := filepath.Rel(source, path)
		if err != nil {
			return err
		}
		target := filepath.Join(destination, relative)
		if entry.IsDir() {
			return os.MkdirAll(target, 0o755)
		}
		raw, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		return os.WriteFile(target, raw, 0o600)
	}); err != nil {
		t.Fatal(err)
	}
}

func containsArg(args []string, want string) bool {
	for _, value := range args {
		if value == want {
			return true
		}
	}
	return false
}
