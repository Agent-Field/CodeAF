//go:build windows

package session

import "testing"

// F1 and R2 reach all six readers on Windows without constructing an agent:
// each must see the writer's rows at a native path containing %20 and #.
func TestWindowsSessionPlanReadersNativePath(t *testing.T) {
	testSessionPlanReadersUseWriterURI(t)
}
