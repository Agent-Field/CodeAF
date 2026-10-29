package testutil_test

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/Agent-Field/codeaf/internal/testutil"
)

// On macOS an aliased TMPDIR, even with a trailing separator, becomes the
// physical folder, and later temporary folders are made under it.
func TestCanonicalizeTempRootsTMPDIRAlias(t *testing.T) {
	requireDarwin(t)
	root := physicalTempDir(t)
	alias := filepath.Join(root, "alias")
	if err := os.Symlink(root, alias); err != nil {
		t.Fatalf("create TMPDIR alias: %v", err)
	}
	t.Setenv("TMPDIR", alias+string(os.PathSeparator))
	t.Setenv("GOTMPDIR", "")

	for call := 1; call <= 2; call++ {
		if err := testutil.CanonicalizeTempRoots(); err != nil {
			t.Fatalf("canonicalize temp roots on call %d: %v", call, err)
		}
		if got := os.Getenv("TMPDIR"); got != root {
			t.Fatalf("TMPDIR on call %d = %q, want physical directory %q", call, got, root)
		}
	}
	child, err := os.MkdirTemp("", "x")
	if err != nil {
		t.Fatalf("create directory under canonical TMPDIR: %v", err)
	}
	if got := filepath.Dir(child); got != root {
		t.Fatalf("temporary directory parent = %q, want physical directory %q", got, root)
	}
}

// TMPDIR and GOTMPDIR are resolved separately and keep their own targets.
func TestCanonicalizeTempRootsSeparateAliases(t *testing.T) {
	requireDarwin(t)
	root := physicalTempDir(t)
	for _, name := range []string{"TMPDIR", "GOTMPDIR"} {
		target := filepath.Join(root, name)
		if err := os.Mkdir(target, 0o700); err != nil {
			t.Fatalf("create %s target: %v", name, err)
		}
		alias := target + "-alias"
		if err := os.Symlink(target, alias); err != nil {
			t.Fatalf("create %s alias: %v", name, err)
		}
		t.Setenv(name, alias)
	}
	if err := testutil.CanonicalizeTempRoots(); err != nil {
		t.Fatalf("canonicalize separate temp roots: %v", err)
	}
	for _, name := range []string{"TMPDIR", "GOTMPDIR"} {
		want := filepath.Join(root, name)
		if got := os.Getenv(name); got != want {
			t.Fatalf("%s = %q, want its separate target %q", name, got, want)
		}
	}
}

// Empty and unset roots stay exactly as they were.
func TestCanonicalizeTempRootsEmptyAndUnset(t *testing.T) {
	for _, unset := range []string{"TMPDIR", "GOTMPDIR"} {
		t.Run("unset "+unset, func(t *testing.T) {
			t.Setenv("TMPDIR", "")
			t.Setenv("GOTMPDIR", "")
			if err := os.Unsetenv(unset); err != nil {
				t.Fatalf("unset %s: %v", unset, err)
			}
			if err := testutil.CanonicalizeTempRoots(); err != nil {
				t.Fatalf("canonicalize empty and unset temp roots: %v", err)
			}
			for _, name := range []string{"TMPDIR", "GOTMPDIR"} {
				value, set := os.LookupEnv(name)
				wantSet := name != unset
				if value != "" || set != wantSet {
					t.Fatalf("LookupEnv(%q) = (%q, %t), want (%q, %t)", name, value, set, "", wantSet)
				}
			}
		})
	}
}

// An already physical root keeps its spelling byte for byte.
func TestCanonicalizeTempRootsPhysicalUnchanged(t *testing.T) {
	root := physicalTempDir(t)
	for _, suffix := range []string{"", string(os.PathSeparator), string(os.PathSeparator) + "."} {
		t.Run("suffix="+suffix, func(t *testing.T) {
			raw := root + suffix
			t.Setenv("TMPDIR", raw)
			t.Setenv("GOTMPDIR", raw)
			if err := testutil.CanonicalizeTempRoots(); err != nil {
				t.Fatalf("canonicalize physical temp roots: %v", err)
			}
			for _, name := range []string{"TMPDIR", "GOTMPDIR"} {
				if got := os.Getenv(name); got != raw {
					t.Fatalf("%s = %q, want byte-for-byte original %q", name, got, raw)
				}
			}
		})
	}
}

// A root that does not exist stops the suite with the variable's name.
func TestCanonicalizeTempRootsMissingTarget(t *testing.T) {
	requireDarwin(t)
	root := physicalTempDir(t)
	for _, name := range []string{"TMPDIR", "GOTMPDIR"} {
		t.Run(name, func(t *testing.T) {
			t.Setenv("TMPDIR", root)
			t.Setenv("GOTMPDIR", "")
			missing := filepath.Join(root, "missing")
			t.Setenv(name, missing)
			err := testutil.CanonicalizeTempRoots()
			if err == nil {
				t.Fatalf("missing %s target %q was accepted", name, missing)
			}
			if !strings.Contains(err.Error(), name) {
				t.Fatalf("missing target error = %q, want variable name %q", err, name)
			}
			if !errors.Is(err, os.ErrNotExist) {
				t.Fatalf("missing target error = %v, want wrapped os.ErrNotExist", err)
			}
			if got := os.Getenv(name); got != missing {
				t.Fatalf("%s after error = %q, want original %q", name, got, missing)
			}
		})
	}
}

// A ".." after a link climbs out of the link's target, as the kernel resolves
// it, and never out of the folder that spells the link.
func TestCanonicalizeTempRootsFollowsLinksBeforeDotDot(t *testing.T) {
	requireDarwin(t)
	root := physicalTempDir(t)
	deep := filepath.Join(root, "other", "deep")
	for _, dir := range []string{deep, filepath.Join(root, "other", "T"), filepath.Join(root, "case", "T")} {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			t.Fatalf("create %s: %v", dir, err)
		}
	}
	if err := os.Symlink(deep, filepath.Join(root, "case", "link")); err != nil {
		t.Fatalf("create link: %v", err)
	}
	t.Setenv("TMPDIR", filepath.Join(root, "case", "link")+"/../T")
	t.Setenv("GOTMPDIR", "")
	if err := testutil.CanonicalizeTempRoots(); err != nil {
		t.Fatalf("canonicalize a root with a dot-dot after a link: %v", err)
	}
	if got, want := os.Getenv("TMPDIR"), filepath.Join(root, "other", "T"); got != want {
		t.Fatalf("TMPDIR = %q, want the folder the kernel reaches, %q", got, want)
	}
}

// Elsewhere the environment is never touched, whatever it holds: the
// temporary folder is not an alias there, and a suite must not fail on it.
func TestCanonicalizeTempRootsLeavesOtherSystemsAlone(t *testing.T) {
	if runtime.GOOS == "darwin" {
		t.Skip("macOS is the system whose temporary folder is an alias")
	}
	root := physicalTempDir(t)
	alias := filepath.Join(root, "alias")
	if err := os.Symlink(root, alias); err != nil {
		t.Fatalf("create alias: %v", err)
	}
	for _, raw := range []string{alias + string(os.PathSeparator), "relative/tmp", filepath.Join(root, "missing")} {
		t.Setenv("TMPDIR", raw)
		t.Setenv("GOTMPDIR", raw)
		if err := testutil.CanonicalizeTempRoots(); err != nil {
			t.Fatalf("canonicalize %q on %s: %v", raw, runtime.GOOS, err)
		}
		for _, name := range []string{"TMPDIR", "GOTMPDIR"} {
			if got := os.Getenv(name); got != raw {
				t.Fatalf("%s on %s = %q, want it untouched %q", name, runtime.GOOS, got, raw)
			}
		}
	}
}

func requireDarwin(t *testing.T) {
	t.Helper()
	if runtime.GOOS != "darwin" {
		t.Skip("only macOS rewrites its temporary roots")
	}
}

// physicalTempDir avoids caching a test's temporary parent before its environment changes.
func physicalTempDir(t *testing.T) string {
	t.Helper()
	root, err := os.MkdirTemp("", "codeaf-testutil-")
	if err != nil {
		t.Fatalf("create fixture root: %v", err)
	}
	t.Cleanup(func() {
		if err := os.RemoveAll(root); err != nil {
			t.Errorf("remove fixture root %q: %v", root, err)
		}
	})
	physical, err := filepath.EvalSymlinks(root)
	if err != nil {
		t.Fatalf("resolve fixture root: %v", err)
	}
	return physical
}
