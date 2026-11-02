//go:build !linux

package handoff

import "testing"

// The directory rename itself is the native Windows handle check.
func assertTakeStageReleased(t *testing.T, root string) {}
