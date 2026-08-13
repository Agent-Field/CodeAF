package exec

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Agent-Field/aforge-v2/internal/home"
)

// Toolchain caches are shared, and they are shared at a path that does not
// carry a session in it.
//
// Measured (audit-notes/headless-regression-audit.md §10, defect 5): every swe
// session took its own module cache under /tmp/codeaf-scratch/ses_*/go-mod, one
// of them pulled 329MB of a single dependency on its own, concurrent sessions
// each pulled it again, and the pod's 5GB root filesystem reached 100% mid-
// battery. The bytes are content-addressed — the same module at the same
// version is the same file for everybody — so the private copy bought no
// isolation at all.
func TestChildToolchainCachesAreSharedAcrossSessions(t *testing.T) {
	t.Setenv(home.EnvVar, t.TempDir())
	for _, name := range []string{
		"GOMODCACHE", "GOCACHE", "CARGO_TARGET_DIR", "npm_config_cache", "PIP_CACHE_DIR",
	} {
		t.Setenv(name, "")
		_ = os.Unsetenv(name)
	}
	worker := NewSWE(nil, "vendor/model", "key", "", 0)
	first := environmentMap(worker.environ(placeView(t.TempDir())))
	second := environmentMap(worker.environ(placeView(t.TempDir())))

	root := home.Join("cache", "toolchain")
	for name, leaf := range map[string]string{
		"GOMODCACHE": "go-mod", "GOCACHE": "go-build", "CARGO_TARGET_DIR": "cargo",
		"npm_config_cache": "npm", "PIP_CACHE_DIR": "pip",
	} {
		want := filepath.Join(root, leaf)
		if first[name] != want {
			t.Fatalf("%s = %q, want the shared cache at %q", name, first[name], want)
		}
		if second[name] != first[name] {
			t.Fatalf("%s differs between sessions: %q vs %q", name, first[name], second[name])
		}
		if info, err := os.Stat(want); err != nil || !info.IsDir() {
			t.Fatalf("%s was pointed at a directory that does not exist: %v", name, err)
		}
	}
	if strings.Contains(first["GOMODCACHE"], "ses_") {
		t.Fatalf("the module cache still carries a session in its path: %q", first["GOMODCACHE"])
	}
}

// An operator who has already said where a cache lives outranks the default. A
// machine with a warm GOMODCACHE, or an image that mounts one, has made the
// same decision better, and overwriting it would be this worker ignoring the
// answer it was given.
func TestAnOperatorsOwnCacheOutranksTheSharedDefault(t *testing.T) {
	t.Setenv(home.EnvVar, t.TempDir())
	mounted := t.TempDir()
	t.Setenv("GOMODCACHE", mounted)
	worker := NewSWE(nil, "vendor/model", "key", "", 0)
	environment := environmentMap(worker.environ(placeView(t.TempDir())))
	if environment["GOMODCACHE"] != mounted {
		t.Fatalf("GOMODCACHE = %q, want the operator's %q", environment["GOMODCACHE"], mounted)
	}
	if environment["GOCACHE"] == "" {
		t.Fatal("pinning one cache silently dropped the others")
	}
}

// A run killed before it can write a terminal line still spent what it spent.
//
// Measured (audit-notes/headless-regression-audit.md §10, defect 4): a leaf that
// worked for 893 seconds reported `swe: deadline after 0 cycles, $0.0000`, while
// a comparable run that reached its terminal line reported $0.0874 — a ~35×
// discrepancy in dollars-per-token between two runs of the same worker on the
// same model. Those lines are what the selection prompts read, so a silently
// tiny number teaches the ruler that this worker is nearly free.
func TestAControlledStopStillReportsWhatItSpent(t *testing.T) {
	probe := newSWEProbe(t, "hang")
	task := probe.task()
	// The cancel waits until the run has demonstrably read something off the
	// stream. Cancelling unconditionally raced the child's own start, and the
	// test then measured a run killed before it ever heard about the money —
	// which is a different thing from the defect it is named for.
	controlWhenItSpeaks(&task, ControlCancel)
	outcome, err := probe.worker.Run(context.Background(), task)
	if err != nil {
		t.Fatal(err)
	}
	if outcome.Usage.Cost != 0.0731 {
		t.Fatalf("cost = %v, want the spend the stream reported before the kill", outcome.Usage.Cost)
	}
	if outcome.Usage.PromptTokens != 90000 {
		t.Fatalf("tokens = %+v", outcome.Usage)
	}
	if !strings.Contains(outcome.Text, "$0.0731") {
		t.Fatalf("the measured line hides the spend: %q", outcome.Text)
	}
	// The number is real but it is not the engine's own total, and a measured
	// line nobody labelled is read downstream as measured truth.
	if !strings.Contains(outcome.Text, "estimated") {
		t.Fatalf("the stand-in figure is not labelled as one: %q", outcome.Text)
	}
}

// The engine's own terminal figure still wins whenever it has one, and it is
// not labelled an estimate.
func TestTheEnginesOwnTotalIsStillAuthoritative(t *testing.T) {
	probe := newSWEProbe(t, "pass")
	outcome, err := probe.worker.Run(context.Background(), probe.task())
	if err != nil {
		t.Fatal(err)
	}
	if outcome.Usage.Cost != 0.4212 {
		t.Fatalf("cost = %v, want the terminal event's figure", outcome.Usage.Cost)
	}
	if strings.Contains(outcome.Text, "estimated") {
		t.Fatalf("a reported total was labelled an estimate: %q", outcome.Text)
	}
}

func environmentMap(environ []string) map[string]string {
	out := make(map[string]string, len(environ))
	for _, entry := range environ {
		name, value, found := strings.Cut(entry, "=")
		if found {
			out[name] = value
		}
	}
	return out
}
