package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/Agent-Field/codeaf/internal/config"
	"github.com/Agent-Field/codeaf/internal/home"
)

// TestAFaultTestKeepsItsFixtureOutOfAProfileItInherited is the leak, held down
// from the outside — by running the fault test itself and looking at where its
// write landed.
//
// reportFault appends through config.ProfilePath(config.ProfileDir(),
// "chat.log"), and it has to: the surface's running log and the crash's append
// are one file, so a profile that moved the first must move the second
// (fault.go's own comment says so). CODEAF_PROFILE_DIR is therefore the variable
// that decides where a fault test's fixture goes, and it OUTRANKS the two a test
// naturally reaches for — an exported profile wins over CODEAF_HOME and over
// HOME, which only answer where the state root is when no profile was named. A
// harness that exports it points every fault test in this package at somebody
// else's chat.log, and a fixture that reads as a genuine crash costs a false
// crash report.
//
// So the fault test runs here as a child process with CODEAF_PROFILE_DIR pointed
// at an empty directory standing in for that inherited profile, and the stand-in
// is then read. The assertion is on WHERE the file landed and never on a byte of
// its text, and the stand-in is a directory this test created — so no real
// profile is touched, not even to check it. The child's own assertions are the
// positive half: it fails by itself if its log did not land where it expected.
func TestAFaultTestKeepsItsFixtureOutOfAProfileItInherited(t *testing.T) {
	inherited := filepath.Join(t.TempDir(), "profile")
	if err := os.MkdirAll(inherited, 0o755); err != nil {
		t.Fatal(err)
	}
	// The child's HOME and state root, so that nothing it writes can reach a
	// real one even on a road this test did not think about. A closed
	// environment is the package's own idiom for a child (smokeEnv).
	own := t.TempDir()

	child := exec.Command(os.Args[0],
		"-test.run=^TestReportFaultSaysOneCalmThingAndLogsTheStack$", "-test.count=1")
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
		t.Fatalf("the fault test wrote into the profile the environment named instead of one of its own: %v\nthe child said:\n%s",
			names, out)
	}
	if err != nil {
		t.Fatalf("the fault test failed under an inherited %s: %v\n%s", config.ProfileDirEnv, err, out)
	}
}
