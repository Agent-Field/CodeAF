//go:build !linux && !darwin

package session

import "testing"

// Private Apply is supported only on Linux and macOS, where ownership can be prepared.
func normalizeWorkspaceApplyFixtureMetadata(t *testing.T, path string) {
	t.Helper()
}
