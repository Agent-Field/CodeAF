//go:build windows

package exec

import (
	"path/filepath"
	"testing"
)

// K3 proves the exact offered shell command can read a native external scratch
// folder with spaces and an apostrophe, as well as the relative layout above.
func TestK3WindowsSpillNoticeReadsExternalScratch(t *testing.T) {
	loudCommandRoundTrip(t, workspace(t).WithScratch(filepath.Join(t.TempDir(), "saved output's folder")))
}
