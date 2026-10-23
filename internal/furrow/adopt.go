package furrow

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/Agent-Field/codeaf/internal/env"
	"github.com/Agent-Field/codeaf/internal/home"
)

// Logf is the one-line reporting door boot adoption uses when best effort was
// not enough. It matches log.Printf without making this package choose a log.
type Logf func(string, ...any)

// Adopt moves a furrow snapshot store that predates the state-root rule into
// the default profile once. Before codeaf started naming FURROW_DATA_DIR
// ([runBinary]), a store lived wherever furrow's own default put it — the OS
// data root of the login, `~/Library/Application Support/dev.furrow.furrow` on
// macOS, `~/.local/share/furrow` on Linux. Pointing every child at the state
// root's own store orphans months of sealed timelines and forks unless they
// are carried across, so the first start of the new layout moves them. A link
// left at the old root keeps an older binary — a resident of an earlier build
// still running, a downgraded copy — resolving the same store: the companion
// link home.Adopt leaves at the old state folder, here for the store.
//
// It is a boot operation, not store resolution: the real binary calls it from
// main, and the testing guard makes an accidental library call harmless in
// test binaries. It is default-profile only — a CODEAF_HOME the caller named
// is a deliberate separation, and the old store belongs to the profile it
// leaves behind, not to the disposable one. Best effort throughout: a store
// furrow has already opened at the new place wins, and every failure is
// reported on one line and forgotten, the way [home.Adopt] treats its own.
func Adopt(logf Logf) {
	if testing.Testing() {
		return
	}
	adoptProfile(logf)
}

func adoptProfile(logf Logf) {
	if _, set := env.Lookup(home.EnvVar); set {
		return
	}
	base, err := os.UserHomeDir()
	if err != nil {
		adoptionFailed(logf, err)
		return
	}
	adopt(base, logf, adoptOps{
		lstat:    os.Lstat,
		mkdirAll: os.MkdirAll,
		rename:   os.Rename,
		symlink:  os.Symlink,
	})
}

type adoptOps struct {
	lstat    func(string) (os.FileInfo, error)
	mkdirAll func(string, os.FileMode) error
	rename   func(string, string) error
	symlink  func(string, string) error
}

func adopt(base string, logf Logf, operations adoptOps) {
	current := home.Join("furrow", "store-v1")
	legacy := filepath.Join(platformDataRoot(base), "store-v1")
	if _, err := operations.lstat(current); err == nil {
		// A store already lives under the state root — this profile has been
		// used since the rule, or another process is mid-flight. Leave both.
		return
	} else if !os.IsNotExist(err) {
		adoptionFailed(logf, err)
		return
	}
	info, err := operations.lstat(legacy)
	if os.IsNotExist(err) {
		return
	}
	if err != nil {
		adoptionFailed(logf, err)
		return
	}
	if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return
	}
	if err := operations.mkdirAll(home.Join("furrow"), 0o700); err != nil {
		adoptionFailed(logf, err)
		return
	}
	if err := operations.rename(legacy, current); err != nil {
		adoptionFailed(logf, err)
		return
	}
	if logf != nil {
		logf("codeaf: moved furrow's snapshot store from %s to %s", legacy, current)
	}
	if operations.symlink != nil {
		// THE OLD BUILD KEEPS READING THE SAME STORE. A process of an older
		// binary still running — a resident that outlived an update — resolves
		// the OS data root and would otherwise open a fresh empty store there,
		// splitting the timelines between two stores nobody sees both of.
		// Redirect the old path to the new one; relative, like home.Adopt's,
		// so the pair survives the home directory moving. Best effort: the move
		// above is the adoption, this is its compatibility seam.
		target, err := filepath.Rel(filepath.Dir(legacy), current)
		if err != nil {
			target = current
		}
		if err := operations.symlink(target, legacy); err != nil {
			adoptionFailed(logf, err)
		}
	}
}

// platformDataRoot is where furrow's own default puts its data on this
// platform: the directories crate's ProjectDirs::from("dev", "furrow",
// "furrow").data_dir(), verified against the pinned v0.1.0 source's
// data_root(). The qualifier only shapes macOS, and the login's OS goes here
// because the legacy store was chosen by the OS the store ran on.
func platformDataRoot(base string) string {
	switch runtime.GOOS {
	case "darwin":
		return filepath.Join(base, "Library", "Application Support", "dev.furrow.furrow")
	default:
		// Linux: ~/.local/share/furrow, or $XDG_DATA_HOME when the person
		// moved the XDG data root (directories reads it before falling back).
		if xdg := os.Getenv("XDG_DATA_HOME"); filepath.IsAbs(xdg) {
			return filepath.Join(xdg, "furrow")
		}
		return filepath.Join(base, ".local", "share", "furrow")
	}
}

func adoptionFailed(logf Logf, err error) {
	if logf != nil {
		logf("codeaf: could not adopt furrow's former snapshot store: %v", err)
	}
}
