//go:build windows

package plandb

import "testing"

// F1 and R2 prove both readers reach the writer on Windows even when the native
// directory and filename contain spaces, Unicode, %20 and #.
func TestWindowsPlanReadersNativePath(t *testing.T) {
	testPlanReadersUseWriterURI(t)
}
