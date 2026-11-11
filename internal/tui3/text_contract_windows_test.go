package tui3

import (
	"path/filepath"
	"strings"
	"testing"
)

// Text contract C5: drive and UNC spellings navigate rather than filter; the
// volume root remains absolute when the final component is still being typed.
func TestTextContractFolderNativePaths(t *testing.T) {
	for _, tc := range []struct{ typed, dir, leaf string }{
		{`C:\x`, `C:\`, "x"},
		{`C:/x`, `C:\`, "x"},
		{`c:\work\pro`, `c:\work`, "pro"},
		{`\\server\share\x`, `\\server\share\`, "x"},
		{`\\server\share\x\`, `\\server\share\x`, ""},
		{`C:\`, `C:\`, ""},
	} {
		t.Run(tc.typed, func(t *testing.T) {
			var picker folderPick
			picker.start(nil, "")
			picker.filter.setText(tc.typed)
			picker.rank()
			if !picker.browsing {
				t.Fatal("native path was classified as search text")
			}
			picker.browseSync(func(path string) string { return path })
			if picker.cols.dir != filepath.Clean(tc.dir) {
				t.Fatalf("browsed %q, want %q", picker.cols.dir, tc.dir)
			}
			if tc.leaf != "" {
				dir, leaf := pathHead(tc.typed)
				if filepath.Clean(dir) != filepath.Clean(tc.dir) || leaf != tc.leaf {
					t.Fatalf("split = %q, %q", dir, leaf)
				}
			}
		})
	}
}

// Text contract C5: changing the drive letter's case keeps the held listing.
func TestTextContractFolderDriveCase(t *testing.T) {
	var picker folderPick
	picker.start(nil, "")
	picker.browseAt(`C:\work`, "")
	picker.filter.setText(`c:\work\`)
	picker.browseSync(func(path string) string { return path })
	if !strings.HasPrefix(picker.cols.dir, "C:") {
		t.Fatalf("drive case restarted navigation: %q", picker.cols.dir)
	}
}
