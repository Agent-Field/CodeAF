package identity_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/Agent-Field/codeaf/internal/identity"
	"github.com/Agent-Field/codeaf/internal/identity/identitytest"
)

// Staged or committed work in an owned chat's own repository keeps the home
// from joining, even when the file has gone from the folder and its name is
// nothing but spaces, which a trimmed listing used to read as no name at all.
func TestStagedOrCommittedWorkInAnOwnedRepositoryKeepsAHomeFromJoining(t *testing.T) {
	for _, name := range []string{" "} {
		for _, step := range []string{"staged", "committed"} {
			t.Run(step+" "+map[bool]string{true: "a blank name", false: name}[name == " "], func(t *testing.T) {
				home := t.TempDir()
				place := identitytest.OpenAndQuit(t, home, true)
				if ok, err := identity.Joinable(home); !ok || err != nil {
					t.Fatalf("the untouched opened home must join first: %v, %v", ok, err)
				}
				work := place.Workspace
				git := func(args ...string) {
					t.Helper()
					cmd := exec.Command("git", append([]string{"-c", "user.name=t", "-c", "user.email=t@t", "-c", "commit.gpgsign=false"}, args...)...)
					cmd.Dir = work
					if out, err := cmd.CombinedOutput(); err != nil {
						t.Fatalf("git %v: %v\n%s", args, err, out)
					}
				}
				if err := os.WriteFile(filepath.Join(work, name), []byte("keep my work"), 0o600); err != nil {
					t.Fatal(err)
				}
				git("add", "--", name)
				if step == "committed" {
					git("commit", "-q", "-m", "work")
				}
				// The file leaves the folder, so only Git still knows about it.
				if err := os.Rename(filepath.Join(work, name), filepath.Join(t.TempDir(), "moved")); err != nil {
					t.Fatal(err)
				}
				if ok, err := identity.Joinable(home); ok || err != nil {
					t.Fatalf("%s work named %q must keep the home from joining: %v, %v", step, name, ok, err)
				}
			})
		}
	}
}
