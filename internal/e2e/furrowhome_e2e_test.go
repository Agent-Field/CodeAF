//go:build e2e

package e2e

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"
)

// The acceptance of #1769: a session launched with its own CODEAF_HOME, behind
// the host guard's throwaway HOME, must keep furrow's snapshot store inside
// the state root and leave the platform data root empty. Before the fix the
// store went wherever furrow's own default put it — the OS data root of the
// login, `~/Library/Application Support/dev.furrow.furrow` on macOS — so two
// profiles that shared a HOME shared one store, and test drives wrote into the
// developer's own store.
//
// One turn is the unit: the belt is assembled per turn, and the belt's furrow
// detection is the first status call, which opens (and therefore creates) the
// store eagerly even in a folder furrow was never attached to.
func TestTheFurrowStoreFollowsTheStateRoot(t *testing.T) {
	requireTmuxAndKey(t)
	home := newHome(t, nil)
	ws := newWorkspace(t, "furrow-home", false)
	r := start(t, "furrow_home", home, ws, 100, 32)

	r.lit("Answer with only these words joined by spaces: Furrow / store / check.")
	r.keys("Enter")
	r.waitFor(3*time.Minute, "Furrow store check")

	store := filepath.Join(home, "furrow", "store-v1")
	if _, err := os.Stat(store); err != nil {
		t.Fatalf("the snapshot store did not land under the state root: %v (CODEAF_HOME=%s)", err, home)
	}
	// The login the guard gave this launch is throwaway; furrow's default data
	// root there must not exist, or the store still escaped the profile.
	legacyRoot := furrowDefaultDataRoot(r.host.login)
	if _, err := os.Stat(legacyRoot); !os.IsNotExist(err) {
		t.Fatalf("the login's data root holds a furrow store: %s", legacyRoot)
	}
	t.Logf("store under the state root: %s\ndefault data root empty: %s", store, legacyRoot)
}

// furrowDefaultDataRoot is where furrow's own default would put its store on
// this platform: the directories crate's ProjectDirs::from("dev", "furrow",
// "furrow").data_dir(), verified against the pinned v0.1.0 source. The
// qualifier shapes only macOS; Linux uses the lowercase application name under
// the XDG data root (which the guard does not set, so the fallback applies).
func furrowDefaultDataRoot(login string) string {
	if runtime.GOOS == "darwin" {
		return filepath.Join(login, "Library", "Application Support", "dev.furrow.furrow")
	}
	return filepath.Join(login, ".local", "share", "furrow")
}
