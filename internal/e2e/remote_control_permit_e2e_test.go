//go:build e2e

package e2e

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/Agent-Field/codeaf/internal/pair"
)

// rcGrant writes the host's explicit permit row for one paired computer, before
// the host starts, and it is the only place this suite touches the host's own
// book of what each paired computer may do.
//
// THE ROW HAS TO BE EXPLICIT. A computer that is already on the roster when the
// host first reads its book is an older pairing, and an older pairing watches
// but cannot type, which is the right default and the wrong fixture for a
// scenario that is about typing. Writing the row first, through the host's own
// book rather than through a wire method, also keeps the scenarios honest about
// where that switch lives: on the computer that runs the chats and nowhere else.
//
// It is a file of its own because the book is a package the live-control work
// adds, and everything else in this suite stands without it.
func rcGrant(t *testing.T, hostHome, identityID, device string, watch, drive bool) {
	t.Helper()
	dir := filepath.Join(hostHome, "v3", "remote")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatalf("the host's remote folder: %v", err)
	}
	if err := pair.PermitsAt(filepath.Join(dir, "permits.json")).Set(identityID, device, &watch, &drive, "test"); err != nil {
		t.Fatalf("writing the permit for %s: %v", device, err)
	}
}

// rcStampJoin is what joining an identity does to a computer's own book, so that
// every computer it meets afterwards is a new pairing and not an older one: the
// computer is not asked, once, whether the others may type into its chats. The
// rig pairs by writing the files a pairing would leave, so it writes this one too;
// leaving it out would put the one-time question over every Home in the suite.
func rcStampJoin(t *testing.T, home, identityID string) {
	t.Helper()
	dir := filepath.Join(home, "v3", "remote")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatalf("a computer's remote folder: %v", err)
	}
	if err := pair.PermitsAt(filepath.Join(dir, "permits.json")).SnapshotLegacy(identityID, nil); err != nil {
		t.Fatalf("stamping the join in %s: %v", home, err)
	}
}
