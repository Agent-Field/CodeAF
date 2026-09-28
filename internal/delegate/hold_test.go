//go:build !windows

package delegate

import (
	"context"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"golang.org/x/sys/unix"

	"github.com/Agent-Field/codeaf/internal/filelock"
)

// THE PROGRAM IS HANDED ITS HOST'S HOLD ON ITS FOLDER. The launch passes the
// hold's open file as the child's descriptor 3 and names it in the child's
// environment, so the hold — a flock, which belongs to the open file — lasts
// until the program has gone too, even when its host is killed first.
func TestAProgramIsHandedItsHostsHoldOnItsFolder(t *testing.T) {
	hold, err := os.OpenFile(filepath.Join(t.TempDir(), "folder.lock"), os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	defer hold.Close()
	if err := filelock.Lock(hold, true, true); err != nil {
		t.Fatal(err)
	}
	seen := filepath.Join(t.TempDir(), "seen")
	t.Setenv("FAKE_SEEN", seen)
	script := fakeProgram(t, `echo "$`+HoldEnv+`" > "$FAKE_SEEN"; if [ -e /dev/fd/3 ]; then echo open >> "$FAKE_SEEN"; fi; `+terminalLine("pass", "done"))
	launch := fakeLaunch(t, script, t.TempDir(), "hold it", Ceilings{}, ModelAPI{})
	launch.Hold = hold
	if _, err := Run(context.Background(), launch, &recorder{}); err != nil {
		t.Fatal(err)
	}
	if got, _ := os.ReadFile(seen); strings.TrimSpace(string(got)) != "3\nopen" {
		t.Fatalf("the program saw %q, want descriptor 3 named and open", got)
	}
}

// A HANDED HOLD IS KEPT FOR THIS PROCESS AND NO PROCESS IT STARTS. A
// descriptor handed down is not closed on exec, so a dev server the program's
// shell left running would hold the folder for as long as it ran.
func TestAHandedHoldIsClosedOnExec(t *testing.T) {
	file, err := os.OpenFile(filepath.Join(t.TempDir(), "folder.lock"), os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	if _, err := unix.FcntlInt(file.Fd(), unix.F_SETFD, 0); err != nil {
		t.Fatal(err)
	}
	t.Setenv(HoldEnv, strconv.Itoa(int(file.Fd())))
	keepHold()
	flags, err := unix.FcntlInt(file.Fd(), unix.F_GETFD, 0)
	if err != nil || flags&unix.FD_CLOEXEC == 0 {
		t.Fatalf("the handed hold is inherited by the program's own children: flags %d, %v", flags, err)
	}
}
