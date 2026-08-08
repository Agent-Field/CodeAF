package rtk

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// stub installs the stand-in rtk somewhere and points the resolver at it.
func stub(t *testing.T, dir string) string {
	t.Helper()
	source, err := os.ReadFile(filepath.Join("testdata", "rtk"))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "rtk")
	if err := os.WriteFile(path, source, 0o700); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestResolutionOrderPrefersTheStatedChoice(t *testing.T) {
	chosen := stub(t, filepath.Join(t.TempDir(), "chosen"))
	onPath := stub(t, filepath.Join(t.TempDir(), "onpath"))
	home := t.TempDir()
	managed := stub(t, filepath.Join(home, ".aforge", "bin"))

	t.Setenv("HOME", home)
	t.Setenv("PATH", filepath.Dir(onPath))

	t.Setenv(EnvBinary, chosen)
	if tool, ok := Available(); !ok || tool.Path != chosen {
		t.Fatalf("with %s set, resolved %+v, want %s", EnvBinary, tool, chosen)
	}

	// "off" is a stated intention and outranks every installation.
	for _, spelling := range []string{"off", "OFF", " off "} {
		t.Setenv(EnvBinary, spelling)
		if tool, ok := Available(); ok {
			t.Fatalf("%s=%q resolved %s, want nothing", EnvBinary, spelling, tool.Path)
		}
	}

	// A named binary that is not there is a choice, not a hint to look further.
	t.Setenv(EnvBinary, filepath.Join(t.TempDir(), "absent"))
	if tool, ok := Available(); ok {
		t.Fatalf("a missing explicit path resolved %s, want nothing", tool.Path)
	}

	t.Setenv(EnvBinary, "")
	if tool, ok := Available(); !ok || tool.Path != onPath {
		t.Fatalf("with no choice, resolved %+v, want the one on PATH %s", tool, onPath)
	}

	t.Setenv("PATH", filepath.Join(t.TempDir(), "empty"))
	if tool, ok := Available(); !ok || tool.Path != managed {
		t.Fatalf("with nothing on PATH, resolved %+v, want the managed %s", tool, managed)
	}

	if err := os.Remove(managed); err != nil {
		t.Fatal(err)
	}
	if tool, ok := Available(); ok {
		t.Fatalf("with rtk nowhere, resolved %s, want nothing", tool.Path)
	}
}

func TestWrapAcceptsOnlyInspection(t *testing.T) {
	t.Setenv(EnvBinary, stub(t, t.TempDir()))
	tool, ok := Available()
	if !ok {
		t.Fatal("the stub did not resolve")
	}

	for _, want := range []struct {
		command string
		wrapped string
		class   Class
		why     string
	}{
		{"git status", "rtk git status", ClassRead, "a read-only git verb"},
		{"git log --oneline -5", "rtk git log --oneline -5", ClassRead, "a read-only git verb"},
		{"cat go.mod", "rtk read go.mod", ClassRead, "a file read"},
		{"ls -la", "rtk ls -la", ClassRead, "a listing"},
		{"go test ./...", "rtk go test ./...", ClassCheck, "a test run"},
		{"echo x && cat go.mod", "echo x && rtk read go.mod", ClassRead, "an untouched segment beside a read"},
		{"git status | grep m", "git status | rtk grep m", ClassRead, "a filter on the end of a pipe"},
		{"go test && go vet", "rtk go test && rtk go vet", ClassCheck, "two checks"},

		{"git add .", "", ClassNone, "git that writes"},
		{"git pull", "", ClassNone, "git that writes"},
		{"git branch -d topic", "", ClassNone, "git that writes"},
		{"pip install black", "", ClassNone, "a package manager"},
		{"ps aux", "", ClassNone, "a subcommand we have not vouched for"},
		{"sudo ls", "", ClassNone, "rtk behind sudo, which needs it on another PATH"},
		{"cat rtk", "", ClassNone, "a bare rtk token in an argument"},
		{"echo hi", "", ClassNone, "nothing rtk offered to rewrite"},
		{"nudged", "", ClassNone, "rtk's own advice, not a command"},
		{"same", "", ClassNone, "a rewrite that changed nothing"},
	} {
		wrapped, class := tool.Wrap(context.Background(), want.command)
		if class != want.class {
			t.Errorf("Wrap(%q) class = %v, want %v (%s)", want.command, class, want.class, want.why)
			continue
		}
		expected := want.wrapped
		if want.class == ClassNone {
			expected = want.command
		}
		if wrapped != expected {
			t.Errorf("Wrap(%q) = %q, want %q", want.command, wrapped, expected)
		}
	}

	// A mixed line is only as trustworthy as its weakest part: one read in it
	// means the whole line is re-runnable, so it is treated as a read.
	if _, class := tool.Wrap(context.Background(), "git status && go test"); class != ClassRead {
		t.Errorf("a read mixed with a check = %v, want ClassRead", class)
	}
}

func TestWrapRefusesMachineFormats(t *testing.T) {
	t.Setenv(EnvBinary, stub(t, t.TempDir()))
	tool, ok := Available()
	if !ok {
		t.Fatal("the stub did not resolve")
	}
	// The stub would rewrite the bare form; the flag is what stops it, before
	// rtk is even asked.
	for _, command := range []string{
		"git status --porcelain", "git status --name-only", "git status --format=%H",
		"git status --pretty=oneline", "git status -z", "git status --json",
	} {
		if wrapped, class := tool.Wrap(context.Background(), command); class != ClassNone || wrapped != command {
			t.Errorf("Wrap(%q) = %q/%v, want it left alone", command, wrapped, class)
		}
	}
}

func TestBanStopsRetryingABrokenRewrite(t *testing.T) {
	t.Setenv(EnvBinary, stub(t, t.TempDir()))
	tool, ok := Available()
	if !ok {
		t.Fatal("the stub did not resolve")
	}
	if _, class := tool.Wrap(context.Background(), "git status"); class != ClassRead {
		t.Fatalf("git status was not wrapped to begin with")
	}
	tool.Ban("git status")
	if wrapped, class := tool.Wrap(context.Background(), "git status"); class != ClassNone || wrapped != "git status" {
		t.Fatalf("after Ban, Wrap = %q/%v, want the plain command", wrapped, class)
	}
}

func TestOffMeansOffEverywhere(t *testing.T) {
	// The stub is on PATH and in the managed directory; nothing may resolve.
	home := t.TempDir()
	stub(t, filepath.Join(home, ".aforge", "bin"))
	t.Setenv("HOME", home)
	t.Setenv("PATH", filepath.Dir(stub(t, t.TempDir())))
	t.Setenv(EnvBinary, Off)

	if tool, ok := Available(); ok {
		t.Fatalf("off resolved %s", tool.Path)
	}
	// Bootstrap must not fetch behind an explicit refusal either; with no
	// network in tests, the proof is that it returns at once and installs
	// nothing.
	Bootstrap(context.Background())
	if _, err := os.Stat(filepath.Join(home, ".aforge", "bin", "rtk.new")); err == nil {
		t.Fatal("Bootstrap wrote something with rtk turned off")
	}
}

func TestFailedReadsOnlyRtkSideBreakage(t *testing.T) {
	if !Failed(127, "") {
		t.Error("exit 127 is rtk failing to run what it was handed")
	}
	if !Failed(1, "[rtk: No such file or directory (os error 2)]") {
		t.Error("rtk's own failure line was not recognised")
	}
	if Failed(1, "--- FAIL: TestThing") {
		t.Error("an ordinary test failure must not read as rtk breaking")
	}
	if Failed(0, "fine") {
		t.Error("a success must never read as a failure")
	}
}

func TestStripNudgeRemovesOnlyTheAdvertisement(t *testing.T) {
	output := "[rtk] /!\\ No hook installed — run `rtk init -g`\nreal output\n[rtk] again\ntail"
	if got, want := StripNudge(output), "real output\ntail"; got != want {
		t.Fatalf("StripNudge = %q, want %q", got, want)
	}
	if got := StripNudge("untouched\noutput"); got != "untouched\noutput" {
		t.Fatalf("StripNudge changed output that had no nudge: %q", got)
	}
}

func TestBootstrapNeverBlocksOnAnAlreadyResolvedTool(t *testing.T) {
	t.Setenv(EnvBinary, stub(t, t.TempDir()))
	// No network is reachable in a test, so this returning at all is the
	// property: an existing rtk short-circuits the fetch.
	Bootstrap(context.Background())
}

func TestEveryReleaseTargetIsNamedForAPlatformGoKnows(t *testing.T) {
	for platform, target := range targets {
		parts := strings.Split(platform, "/")
		if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
			t.Errorf("target key %q is not GOOS/GOARCH", platform)
		}
		if !strings.Contains(target, "-") {
			t.Errorf("target %q is not a Rust triple", target)
		}
	}
	// The platform this test runs on is one aforge is built for, so a missing
	// entry here would be a silent loss of the whole feature.
	if _, ok := targets[runtime.GOOS+"/"+runtime.GOARCH]; !ok {
		t.Errorf("no rtk release target for %s/%s", runtime.GOOS, runtime.GOARCH)
	}
}
