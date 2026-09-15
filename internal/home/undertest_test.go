package home

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestNothingHereResolvesTheRootThisBinaryInherited is the law of undertest.go
// asked of the ambient environment: this test sets nothing, moves nothing and
// pins nothing, so what it reads is exactly what a test binary is handed when
// it starts. Against the tree before the gate, both of these named the state
// root of the person running the suite.
func TestNothingHereResolvesTheRootThisBinaryInherited(t *testing.T) {
	if got := Dir(); Contains(inherited, got) {
		t.Errorf("the state root of whoever ran this resolved to %q; a test binary is entitled to none of it", got)
	}
	if got := Join("v3", "tasks"); Contains(inherited, got) {
		t.Errorf("a file inside the state root of whoever ran this resolved to %q", got)
	}
	if got, want := Dir(), quarantine; got != want {
		t.Errorf("an unpinned test binary got %q, want the throwaway root %q", got, want)
	}
}

// TestTheThrowawayRootIsNobodysHome is the half that makes the redirection safe
// rather than merely different: a quarantine directory that happened to sit
// under the person's own root would be the same defect one level down.
func TestTheThrowawayRootIsNobodysHome(t *testing.T) {
	if Contains(inherited, quarantine) {
		t.Fatalf("the throwaway root %q is inside the inherited root %q", quarantine, inherited)
	}
	// Named for this process, so two packages running beside each other never
	// share one root — which is how session node journals collided (#187).
	if !strings.Contains(filepath.Base(quarantine), "codeaf-test-home-") {
		t.Errorf("the throwaway root %q does not say what it is", quarantine)
	}
}

// TestARootTheTestChoseIsTheRootItGets is why the gate can be this blunt: what
// is refused is a root NOBODY CHOSE. Every test in this repository that points
// CODEAF_HOME at a directory of its own, and every package that moves HOME for
// its whole run the way internal/session does, must keep working unchanged.
func TestARootTheTestChoseIsTheRootItGets(t *testing.T) {
	chosen := t.TempDir()
	t.Setenv(EnvVar, chosen)
	if got, want := Dir(), chosen; got != want {
		t.Errorf("a root the test chose gave %q, want %q", got, want)
	}

	// Moving HOME alone is the other way a package says where its state goes,
	// and it has to reach the same answer: internal/session moves HOME and NOT
	// CODEAF_HOME on purpose, so that its own tests keep their separate roots.
	t.Setenv(EnvVar, "")
	login := t.TempDir()
	t.Setenv("HOME", login)
	if got, want := Dir(), DefaultUnder(login); got != want {
		t.Errorf("a login the test moved gave %q, want %q", got, want)
	}
}

// TestTheWholeInheritedRootIsRefusedAndNothingBeside covers the two edges of
// the comparison: everything under the root a test binary was handed is
// refused, however deep, and a directory merely NAMED like it is not — which is
// the whole reason [Contains] compares path elements and not prefixes.
func TestTheWholeInheritedRootIsRefusedAndNothingBeside(t *testing.T) {
	restore := inherited
	t.Cleanup(func() { inherited = restore })
	pretend := filepath.Join(t.TempDir(), "state")
	inherited = pretend

	t.Setenv(EnvVar, pretend)
	if got := Dir(); got != quarantine {
		t.Errorf("the inherited root resolved to %q", got)
	}
	t.Setenv(EnvVar, filepath.Join(pretend, "deeper", "still"))
	if got := Dir(); got != quarantine {
		t.Errorf("a directory deep inside the inherited root resolved to %q", got)
	}
	t.Setenv(EnvVar, pretend+"-2")
	if got, want := Dir(), pretend+"-2"; got != want {
		t.Errorf("a sibling of the inherited root was mistaken for a child of it: got %q, want %q", got, want)
	}
}

// TestInheritedDirIsTheRootTheProcessStartedWith pins the one door out. The
// guards that count files under the person's real journal trees, and the e2e
// harness that copies their provider key, both need the ungated answer, and a
// gate with no way through would have turned those into silent no-ops.
func TestInheritedDirIsTheRootTheProcessStartedWith(t *testing.T) {
	t.Setenv(EnvVar, t.TempDir())
	if got, want := InheritedDir(), inherited; got != want {
		t.Errorf("the inherited root moved with the environment: got %q, want %q", got, want)
	}
}

// TestOutsideATestBinaryNothingChanges pins the half that cannot be observed
// from inside one: the product resolves its state root exactly as it always
// has, and the gate is one bool read on a path taken by every caller.
func TestOutsideATestBinaryNothingChanges(t *testing.T) {
	gate, root := underTest, inherited
	t.Cleanup(func() { underTest, inherited = gate, root })
	underTest = false

	login := t.TempDir()
	t.Setenv("HOME", login)
	t.Setenv(EnvVar, "")
	inherited = DefaultUnder(login)
	if got, want := Dir(), DefaultUnder(login); got != want {
		t.Errorf("the product's state root resolved to %q, want %q", got, want)
	}
	if got, want := Join("config.json"), filepath.Join(login, ".codeaf", "config.json"); got != want {
		t.Errorf("the product's config resolved to %q, want %q", got, want)
	}
	// And a process with no home at all still degrades to a relative directory
	// rather than to an error path no caller was written to handle.
	t.Setenv("HOME", "")
	if runtimeHasNoHome(t) {
		if got, want := Dir(), ".codeaf"; got != want {
			t.Errorf("a process with no home resolved to %q, want %q", got, want)
		}
	}
}

// runtimeHasNoHome reports whether clearing HOME actually leaves this platform
// without a home directory. It does on Unix; Windows answers from USERPROFILE,
// and a test that asserted otherwise would be asserting about the operating
// system rather than about this package.
func runtimeHasNoHome(t *testing.T) bool {
	t.Helper()
	base, err := os.UserHomeDir()
	return err != nil || strings.TrimSpace(base) == ""
}
