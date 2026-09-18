package main

// testfloor_test.go holds the floor itself down, from the outside.
//
// #1145 pinned CODEAF_PROFILE_DIR in the four tests reading had found, one at a
// time, and proposed the real stopper and did not do it: the package's floor —
// [isolateTestEnvironment] in testenv_test.go, which TestMain gives every test
// in this package — moved HOME and CODEAF_HOME into a temp root but answered an
// exported CODEAF_PROFILE_DIR as a deliberate pin and left it alone, and
// config.ProfilePath answers an exported profile before it falls back to the
// state root. So the floor covered every variable except the one that outranks
// both, and a harness that exports a profile at a live chat.log reached the
// whole package, not the four roads that had been named.
//
// A test that pins the variable itself cannot fail on a floor that does not
// clear it — that is exactly why the four pins from #1145 could not be the
// failing case any more — so this one runs the floor rather than a road: a
// child of this test binary, started with CODEAF_PROFILE_DIR exported at an
// empty directory standing in for the inherited profile. The child takes the
// real fault road (reportFault appends through
// config.ProfilePath(config.ProfileDir(), "chat.log"), the exact expression
// #1145 turned on) and asserts what the floor left it; the parent then reads
// the stand-in back. The assertions are on WHERE the write landed and on what
// the child inherited, never on a byte of any real profile, and the stand-in is
// a directory this test created — no real profile is touched, not even to
// check it.

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/Agent-Field/codeaf/internal/config"
	"github.com/Agent-Field/codeaf/internal/home"
)

// TestThePackageFloorClearsAProfileTheEnvironmentNamed is the class-stopper
// #1145 proposed, held down the way the fault case was: by running a test of
// this package under an inherited profile and looking at where its write
// landed. Any test of the package would do — the floor belongs to TestMain —
// so the child is a test whose whole job is to say what the floor left it.
func TestThePackageFloorClearsAProfileTheEnvironmentNamed(t *testing.T) {
	inherited := filepath.Join(t.TempDir(), "profile")
	if err := os.MkdirAll(inherited, 0o755); err != nil {
		t.Fatal(err)
	}
	// The child's HOME and state root, so that nothing it writes can reach a
	// real one even on a road this test did not think about. A closed
	// environment is the package's own idiom for a child (smokeEnv).
	own := t.TempDir()

	child := exec.Command(os.Args[0],
		"-test.run=^TestTheFloorLeftThisProcessNoInheritedProfile$", "-test.count=1")
	child.Env = []string{
		"HOME=" + own,
		"PATH=" + os.Getenv("PATH"),
		home.EnvVar + "=" + own,
		config.ProfileDirEnv + "=" + inherited,
		"CODEAF_TELEMETRY=off",
		"CODEAF_TELEMETRY_ENDPOINT=",
	}
	out, err := child.CombinedOutput()

	landed, readErr := os.ReadDir(inherited)
	if readErr != nil {
		t.Fatalf("read the inherited profile back: %v", readErr)
	}
	if len(landed) != 0 {
		names := make([]string, 0, len(landed))
		for _, entry := range landed {
			names = append(names, entry.Name())
		}
		t.Fatalf("a package test wrote into the profile the environment named despite the floor: %v\nthe child said:\n%s",
			names, out)
	}
	if err != nil {
		t.Fatalf("a package test failed under the floor with an inherited %s: %v\n%s", config.ProfileDirEnv, err, out)
	}
}

// TestTheFloorLeftThisProcessNoInheritedProfile is the child half of the guard
// above: its parent starts this binary with CODEAF_PROFILE_DIR exported at a
// stand-in profile, and the floor under TestMain is what stands between this
// test and that export.
//
// IT ALSO RUNS IN THE ORDINARY SUITE, as any Test function does, and that pass
// is worth having rather than suppressing: on a machine with nothing to inherit
// it reads the floor's own clear, so the suite says the floor holds here, and
// the child run above says it holds against an export. Neither writes outside
// the state root the floor provides.
func TestTheFloorLeftThisProcessNoInheritedProfile(t *testing.T) {
	// The road first, so a floor that leaks leaves its evidence in the stand-in
	// for the parent to read back even though the assertions below fail this
	// child on their own.
	reportFault(&bytes.Buffer{}, "runtime error: slice bounds out of range [:-1]",
		[]byte("goroutine 1 [running]:\nmain.runDo(...)\n"))

	if was := os.Getenv(config.ProfileDirEnv); was != "" {
		t.Fatalf("the floor left %s in this process's environment: %q", config.ProfileDirEnv, was)
	}
	// An empty [config.ProfileDir] is the ordinary answer and means the state
	// root's own profile, so the log belongs beside the state root the floor
	// provides and nowhere an export could have named.
	if _, err := os.Stat(home.Join("chat.log")); err != nil {
		t.Fatalf("the fault log did not land under the state root the floor provides: %v", err)
	}
}
