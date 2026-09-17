//go:build !windows

package update

import "testing"

func TestUnixLaunchDoesNotResolveAnExecutableOnlyToCleanAWindowsBackup(t *testing.T) {
	called := false
	CleanupOldRunning(func() (string, error) {
		called = true
		return "", nil
	})
	if called {
		t.Fatal("the Unix launch resolved its executable for Windows cleanup")
	}
}
